# Deploying Afterword Cloud on one VPS

Everything in `deploy/` targets a single Ubuntu 24.04 box running Docker
Compose. There are no managed queues, no managed database, and nothing
serverless. Scaling happens by adding boxes, not tiers.

The files carry no comments by project rule. This document is where the
reasoning lives.

---

## 1. What you need before you start

| Item | Value |
|---|---|
| Server | Ubuntu 24.04, 2 vCPU, 4 GB RAM, 40 GB disk (Hetzner CX22 class, ~EUR 4/month). Any provider with the same shape works. Pick a region close to your users. |
| Domain | One domain you control, with the three records below. |
| Offsite storage | A Backblaze B2 or Cloudflare R2 bucket for backups, plus an access key pair scoped to it. |
| SMTP | Any transactional sender (Postmark, SES, Resend, Mailgun). Login codes go out over it. |
| GitHub deploy key | A read-only SSH key on the private repository, installed on the box at `/home/afterword/.ssh/id_ed25519`. |
| GHCR access | The box must be able to pull `ghcr.io/judeotine/afterword-*`. For a private package, run `docker login ghcr.io` once as the `afterword` user with a read:packages token. |

### DNS records

All three are `A` records (add `AAAA` too if the box has IPv6) pointing at the
server's public IP. Create them **before** the first boot: Caddy asks Let's
Encrypt for certificates on startup and needs the names to resolve.

| Name | Serves |
|---|---|
| `api.example.com` | The Go API. |
| `app.example.com` | The Next.js web app (Phase F; returns 502 until then). |
| `s3.example.com` | MinIO's S3 endpoint. Pre-signed upload and download URLs point here, so it must be reachable from the public internet. |

The MinIO console is deliberately **not** published. It binds to
`127.0.0.1:9001` on the host only; reach it with an SSH tunnel
(`ssh -L 9001:127.0.0.1:9001 afterword@your-box`) and open
`http://127.0.0.1:9001`.

---

## 2. First boot

```bash
ssh root@your-box
install -d -m 0700 /home/afterword/.ssh          # after the first run creates the user
# put the repository deploy key at /home/afterword/.ssh/id_ed25519, mode 600

curl -fsSL https://raw.githubusercontent.com/... /deploy/bootstrap.sh -o bootstrap.sh
# or clone by hand once and run deploy/bootstrap.sh from the checkout
bash bootstrap.sh
```

`deploy/bootstrap.sh` is idempotent; run it as often as you like. It:

1. installs `ca-certificates curl git gnupg fail2ban ufw unattended-upgrades`;
2. installs Docker Engine and the Compose plugin from Docker's own apt
   repository, and enables the service;
3. creates the `afterword` user, adds it to the `docker` group, and creates
   `~/.ssh` with mode 700;
4. sets `ufw` to deny inbound except 22/tcp, 80/tcp, 443/tcp and 443/udp
   (443/udp is HTTP/3), then enables it;
5. writes an `sshd` jail for fail2ban and enables the service;
6. turns on unattended security upgrades;
7. requires the deploy key, then clones or fast-forwards the repository into
   `/opt/afterword`;
8. **stops on the first run** after copying `deploy/.env.example` to
   `deploy/.env` (mode 600, owned by `afterword`) and tells you to fill it in;
9. on every later run: `docker compose pull`, `docker compose up -d`,
   `docker compose run --rm api /migrate up`, then polls
   `https://api.$DOMAIN/healthz` for up to five minutes.

Migrations run **after** the stack is up and **before** the health poll on
purpose: `/healthz` reads a row that only exists once migrations have run, so
an unmigrated API correctly reports itself degraded.

Overridable with environment variables: `APP_USER`, `APP_DIR`, `REPO_URL`,
`REPO_BRANCH`, `DEPLOY_KEY`, `HEALTH_ATTEMPTS`, `HEALTH_INTERVAL`.

---

## 3. Filling in `deploy/.env`

`deploy/.env` is the only secret store on the box. Mode 600, owned by
`afterword`, never committed (`deploy/.gitignore` covers it). Docker Compose
reads it for interpolation, and the `api` and `transcribe-worker` services also
load it wholesale as `env_file`.

Generate the secrets:

```bash
openssl rand -base64 48 | tr -d '\n='          # JWT_SECRET (48+ chars, minimum is 32)
openssl rand -base64 32 | tr -d '\n=/+'        # POSTGRES_PASSWORD
openssl rand -base64 32 | tr -d '\n=/+'        # MINIO_ROOT_PASSWORD
```

| Variable | Notes |
|---|---|
| `DOMAIN` | Bare domain, e.g. `afterword.app`. Caddy builds `api.`, `app.` and `s3.` from it. |
| `ACME_EMAIL` | Let's Encrypt expiry notices go here. |
| `TAG` | Image tag to run. The deploy workflow rewrites this line on every deploy. |
| `POSTGRES_USER`/`_PASSWORD`/`_DB` | Must match `DATABASE_URL`; keep them in sync by hand. |
| `DATABASE_URL` | `postgres://USER:PASSWORD@postgres:5432/DB?sslmode=disable`. The hostname is the compose service; traffic never leaves the host, so `sslmode=disable` is correct here. |
| `DATABASE_MAX_CONNS` | 20 is right for a 4 GB box. |
| `JWT_SECRET` | Rotating it signs every existing session out. |
| `APP_BASE_URL`, `API_BASE_URL` | `https://app.$DOMAIN` and `https://api.$DOMAIN`. `APP_BASE_URL` is also the only allowed CORS origin. |
| `TRUSTED_PROXY_CIDRS` | `172.16.0.0/12` covers the default Docker bridge networks, so the API trusts Caddy's `X-Forwarded-For`. Without it every log line and rate limit sees Caddy's container IP. |
| `GOOGLE_CLIENT_ID`/`_SECRET` | Both or neither; the API refuses to start with only one. `GOOGLE_REDIRECT_URL` must equal `https://api.$DOMAIN/v1/auth/google/callback` and be registered in the Google console. |
| `EMAIL_SENDER` | `smtp` in production. `log` writes login codes to the container log and is development only. `SMTP_HOST` and `SMTP_FROM` become required when it is `smtp`. |
| `SMS_SENDER` | `noop` until an SMS provider is wired up. |
| `MINIO_ROOT_USER`/`_PASSWORD` | MinIO's admin credentials. `S3_ACCESS_KEY`/`S3_SECRET_KEY` must match them until a scoped MinIO service account exists. |
| `S3_ENDPOINT` | **`https://s3.$DOMAIN`, not `http://minio:9000`.** See below. |
| `S3_USE_SSL` | `true`, matching the endpoint above. |
| `S3_BUCKETS` | Space-separated list used by `minio-init` and by the backup job. Keep it in step with the `S3_BUCKET_*` names. |
| `PRIVACY_URL` | **Required by the bot image.** The consent announcement reads this URL to every participant, so it must describe *this* deployment. Compose refuses to render without it. |
| `BOT_WORKER_REPLICAS` | Bots per box. One on a 4 GB box; see section 7. |
| `BACKUP_*` | Offsite target. `BACKUP_PROVIDER` is an rclone S3 provider name (`Cloudflare` for R2, `Other` for B2's S3 API). |

The four `S3_BUCKET_*` variables anticipate the storage layer's per-bucket
names. If that layer lands with different variable names, rename them here and
keep `S3_BUCKETS` — which `minio-init` and `backup.sh` read directly — listing
the same four buckets.

### Why `S3_ENDPOINT` is the public URL

A pre-signed URL's signature covers the `Host` header. If the API signed URLs
for `minio:9000`, the browser would send them to `s3.example.com` and MinIO
would reject the signature — and `minio:9000` is not resolvable from a browser
anyway. Pointing the API at `https://s3.$DOMAIN` makes the signature match what
the client actually sends. The cost is that the API's own S3 calls take one
extra hop through Caddy. If a future change adds a separate
"public endpoint for signing" variable, set that to `https://s3.$DOMAIN` and
`S3_ENDPOINT` back to `http://minio:9000`.

---

## 4. What runs, and what it is limited to

| Service | Image | Memory | CPU | Notes |
|---|---|---|---|---|
| `caddy` | `caddy:2.10-alpine` | 256m | 0.5 | The only service publishing ports: 80/tcp, 443/tcp, 443/udp. Automatic Let's Encrypt, HSTS, `nosniff`, referrer policy, JSON access logs to stdout. |
| `postgres` | `pgvector/pgvector:pg16` | 1g | 1.5 | `shm_size: 256m` so parallel query and larger sorts do not fall over. `pg_isready` healthcheck. |
| `minio` | pinned `RELEASE.2025-09-07…` | 256m | 1.0 | S3 API on 9000 behind Caddy; console on `127.0.0.1:9001` only. |
| `minio-init` | pinned `mc` release | 128m | 0.25 | Creates the buckets, sets them non-public, exits. Everything that needs storage waits for it to complete successfully. |
| `api` | `ghcr.io/judeotine/afterword-api:${TAG}` | 512m | 1.0 | Waits for Postgres and MinIO to be healthy. Ships two binaries: `/api` (default command) and `/migrate`. |
| `web` | `…/afterword-web:${TAG}` | 512m | 1.0 | Profile `web`. Off until Phase F; `app.$DOMAIN` returns 502 until then. |
| `transcribe-worker` | `…/afterword-transcribe-worker:${TAG}` | 1500m | 1.5 | Profile `workers`. Off until Phase C. |
| `bot-worker` | `…/afterword-bot:${TAG}` | 1200m | 1.5 | Profile `workers`. `shm_size: 1gb` for Chromium. Scale with `--scale`. |
| `backup` | `alpine:3.21` | 256m | 0.5 | Installs `postgresql16-client` and `rclone` at start, then loops, running `deploy/backup.sh` at `BACKUP_HOUR_UTC` every night. |

Bring up a profile explicitly:

```bash
docker compose --profile workers up -d
docker compose --profile web --profile workers up -d
```

`docker compose config` without a profile flag shows only the always-on
services; that is expected, not a missing service.

### Migrations

The API image contains a second binary at `/migrate` built from
`services/api/cmd/migrate`. It reads `DATABASE_URL` and `MIGRATIONS_DIR`
(default `/migrations`, where the image puts the checked-in migrations) and
takes three commands:

```bash
docker compose run --rm api /migrate up
docker compose run --rm api /migrate down 1
docker compose run --rm api /migrate version
```

The image declares `CMD ["/api"]` rather than an `ENTRYPOINT` precisely so that
`docker compose run --rm api /migrate up` replaces the command. Plain
`docker run` behaviour is unchanged: with no arguments it starts the API.

### Proxy headers

Caddy already sends `X-Forwarded-For`, `X-Forwarded-Proto` and
`X-Forwarded-Host` on every `reverse_proxy` — declaring them again only earns a
warning in the log — so the Caddyfile adds just `X-Real-IP`. The API decides
which of those to trust from `TRUSTED_PROXY_CIDRS`; with that unset it ignores
forwarded headers entirely and every client looks like Caddy.

`s3.$DOMAIN` deliberately does **not** rewrite `Host`: a pre-signed URL's
signature covers it, and rewriting it would make every upload fail with a
signature mismatch. `flush_interval -1` disables response buffering so large
downloads stream rather than being held in Caddy's memory.

### Why the `api` service has no container healthcheck

The API runs on `gcr.io/distroless/static-debian12`, which has no shell, no
`curl` and no `wget`, so a `HEALTHCHECK` cannot be expressed. `GET /healthz` is
checked from outside instead: `bootstrap.sh` and the deploy workflow poll
`https://api.$DOMAIN/healthz` and fail the deploy if it never answers, and the
uptime monitor in section 9 watches it continuously. Nothing in the compose
file uses `depends_on: api: service_healthy`.

---

## 5. Day-to-day operations

```bash
cd /opt/afterword/deploy

docker compose ps
docker compose logs -f api
docker compose logs -f caddy
docker compose exec postgres psql -U afterword -d afterword

docker compose restart api
docker compose down            # stops everything, keeps volumes
```

Run these as the `afterword` user. Volumes (`postgres-data`, `minio-data`,
`caddy-data`, and the worker volumes) survive `down`; only
`docker compose down -v` destroys data, and there is never a good reason to
type that on the production box.

---

## 6. Updating

### Automatic

Push a tag:

```bash
git tag cloud-v0.3.0
git push origin cloud-v0.3.0
```

`.github/workflows/deploy.yml` then:

1. resolves the image tag (`cloud-v0.3.0` → `0.3.0`, or the `workflow_dispatch`
   input verbatim);
2. builds and pushes `afterword-api` to GHCR, plus `afterword-transcribe-worker`
   and `afterword-web` **if** their Dockerfiles exist (detected after checkout,
   because a job-level `hashFiles` runs before any checkout and always returns
   empty);
3. SSHes to the box with `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`, fast-forwards
   the repository, rewrites the `TAG=` line in `deploy/.env`, pulls, brings the
   stack up, runs `/migrate up`, and polls `/healthz` for five minutes;
4. on failure, re-deploys the tag recorded in `deploy/.last_tag` and still
   fails the run.

Required repository secrets: `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`. Optional
repository variable: `DEPLOY_BRANCH` (defaults to `main`). The job uses the
`production` environment, so you can add a required reviewer there.

**Rollback rolls back images, not schema.** The previous tag is redeployed and
`/migrate up` is a no-op against the newer schema. Keep migrations
backward-compatible for one release: add columns before writing them, drop them
a release later. If a migration itself must be undone, do it deliberately with
`/migrate down 1` and a fresh deploy.

### By hand

```bash
cd /opt/afterword && git pull --ff-only
cd deploy
sed -i 's/^TAG=.*/TAG=0.3.0/' .env
docker compose pull && docker compose up -d
docker compose run --rm api /migrate up
curl -fsS https://api.$DOMAIN/healthz
```

---

## 7. Scaling

On one 4 GB box the practical ceiling is: API, web, Postgres, MinIO, one
transcribe worker on Whisper `base`, and **one** bot at a time. Chromium plus
PulseAudio is the expensive part.

More bots on the same box (only if you have RAM to spare):

```bash
docker compose --profile workers up -d --scale bot-worker=2
```

Bots on a second box, which is the recommended shape past one concurrent
meeting:

1. Provision another Ubuntu box and run `bootstrap.sh` on it.
2. In its `deploy/.env`, keep `API_BASE_URL=https://api.$DOMAIN` and the same
   `PRIVACY_URL` and `BOT_NAME`, and set the storage and database values to the
   primary box's public endpoints.
3. Start only the workers there:
   `docker compose --profile workers up -d bot-worker`.
4. Leave `caddy`, `postgres`, `minio` and `api` stopped on that box. The bot
   worker reaches the API over `https://api.$DOMAIN`, so nothing needs a
   private network.

Postgres and MinIO stay on the primary box. When they become the bottleneck,
move MinIO's data to R2 or B2 — the code only speaks S3, so that is an
`S3_ENDPOINT` and credentials change.

---

## 8. Backups and the restore drill

The `backup` service runs `deploy/backup.sh --loop`, which sleeps until
`BACKUP_HOUR_UTC` (default 03:00 UTC) and then, every night:

1. `pg_dump --format=custom --compress=9` into the `backup-staging` volume;
2. verifies the dump with `pg_restore --list` before trusting it;
3. `rclone copyto` the dump to
   `offsite:$BACKUP_BUCKET/$BACKUP_PREFIX/postgres/postgres-<stamp>.dump`;
4. `rclone sync` each bucket in `S3_BUCKETS` to
   `offsite:$BACKUP_BUCKET/$BACKUP_PREFIX/objects/<bucket>`;
5. deletes local dumps and offsite dumps older than `BACKUP_RETENTION_DAYS`
   (30). Object mirrors are a sync, not a history: a deleted object is gone
   offsite at the next run.

Run one on demand:

```bash
docker compose exec backup /usr/local/bin/backup.sh --once
```

### Restore drill — run this monthly, on a scratch box, before you need it

```bash
cd /opt/afterword/deploy
./restore.sh --list
./restore.sh --dump postgres-20260901T030000Z.dump --objects --yes
docker compose up -d
curl -fsS https://api.$DOMAIN/healthz
docker compose run --rm api /migrate version
```

`restore.sh` refuses to do anything without `--yes`. It stops `caddy`, `api`,
`web`, the workers and `backup`; starts Postgres alone and waits for
`pg_isready`; pulls the named dump from offsite if it is not already staged;
runs `pg_restore --clean --if-exists --exit-on-error`; optionally syncs the
object buckets back into MinIO with `--objects`; and leaves the stack stopped so
you can inspect before starting it. It does not start the stack for you.

Note what a restore does **not** undo: it replaces database contents, but if the
dump predates a migration the running image expects, run `/migrate up`
afterwards. Record the drill result (date, dump used, time to restore) so the
number is known before an incident.

---

## 9. Monitoring

- **Uptime check.** Point any free monitor (UptimeRobot, Better Stack, a cron
  on another box) at `https://api.$DOMAIN/healthz` every minute, expecting HTTP
  200 and `"status":"ok"`. A 503 with `"db":"down"` means Postgres is
  unreachable or unmigrated — the process is alive, so a TCP check would miss
  it.
- **Metrics.** `GET /metrics` on the api container serves Prometheus text
  (`http_requests_total`, `http_request_duration_seconds`,
  `http_requests_in_flight`, `afterword_api_build_info`, plus Go runtime
  metrics). It is not exposed through Caddy. Scrape it from a container on the
  same network, or add a `/metrics` route behind basic auth in the Caddyfile if
  you need it externally.
- **Logs.** Everything logs JSON to stdout; `docker compose logs` and the
  journal hold them. Cap them so they cannot fill the disk by setting Docker's
  default in `/etc/docker/daemon.json`:

  ```json
  {"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}
  ```

  then `systemctl restart docker`.
- **TLS.** Caddy renews automatically. `docker compose logs caddy | grep -i
  certificate` after any DNS change.
- **Backups.** `docker compose logs backup --since 24h` should show a
  "backup complete" line every morning. No line is an incident.

---

## 10. When the disk fills

40 GB goes to Postgres, MinIO objects, Docker images, and container logs, in
that order of surprise. Symptoms: Postgres refuses writes, uploads 500, the API
looks healthy but nothing persists.

Triage:

```bash
df -h
docker system df
du -sh /var/lib/docker/volumes/* | sort -h | tail
```

Recover, cheapest first:

1. `docker image prune -af` — old tags accumulate on every deploy. Usually the
   biggest single win.
2. `docker builder prune -af` — only if anything was ever built on the box.
3. Truncate container logs (or set the `daemon.json` cap above, which prevents
   the problem):
   `truncate -s 0 /var/lib/docker/containers/*/*-json.log`.
4. Clear the staging dumps: they live in the `backup-staging` volume and are
   already offsite —
   `docker compose exec backup sh -c 'rm -f /backups/postgres-*.dump'`.
5. Apply retention: the point of per-workspace retention (default 365 days) is
   to bound `audio`, which dominates MinIO. Shorten it for the noisiest
   workspaces, then let the retention sweep delete the objects.
6. Only then resize the volume or the box. Growing the disk with the provider
   and rebooting is a five-minute job and cheaper than a bad prune.

Prevention: alert at 75% used. Audio is stored as 24 kbps Opus, so roughly
10 MB per recorded hour — the disk should last a long time unless retention is
never applied or images are never pruned.

---

## 11. Security baseline

- TLS everywhere; Caddy redirects 80 to 443 and sends HSTS with a one-year
  max-age. Do not enable HSTS preload submission until the domain is settled.
- Only Caddy publishes ports. Everything else talks over the compose network.
  MinIO's console is loopback-only.
- Secrets live only in `deploy/.env` (mode 600) and in GitHub Actions secrets.
  Nothing secret is committed; `deploy/.gitignore` un-ignores only the two
  example files.
- `ufw` denies inbound except 22, 80 and 443. Harden SSH further by disabling
  password authentication in `/etc/ssh/sshd_config`.
- fail2ban bans an IP for an hour after five failed SSH attempts.
- Unattended security upgrades are on; reboot the box during a quiet window
  when `/var/run/reboot-required` appears.
- Rotate `JWT_SECRET`, the Postgres password, the MinIO credentials and the
  deploy key on a schedule; each rotation is an `.env` edit plus
  `docker compose up -d`, except the Postgres password, which also needs
  `ALTER ROLE` inside the database.
