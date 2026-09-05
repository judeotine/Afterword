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
# put the repository deploy key at /home/afterword/.ssh/id_ed25519, mode 600
# (the first bootstrap run creates the user and the .ssh directory for you)

bash /path/to/deploy/bootstrap.sh
```

`deploy/bootstrap.sh` is idempotent; run it as often as you like. It:

1. installs `ca-certificates curl git gnupg fail2ban ufw unattended-upgrades`;
2. makes sure `/etc/docker/daemon.json` caps `json-file` logging at 10 MB × 3
   files. An existing file is **merged**, not replaced: `python3` (or `jq`)
   fills in only the missing keys, so your own settings and any log sizes you
   already chose survive. The previous file is copied to a timestamped `.bak`
   and the path is logged. If the file exists and neither tool is available,
   the script refuses and prints the keys to add rather than guessing. Docker
   is restarted only when the contents actually changed;
3. installs Docker Engine and the Compose plugin from Docker's own apt
   repository, and enables the service;
4. creates the `afterword` user, adds it to the `docker` group, and creates
   `~/.ssh` with mode 700;
5. sets `ufw` to deny inbound except 22/tcp, 80/tcp, 443/tcp and 443/udp
   (443/udp is HTTP/3), then enables it;
6. writes an `sshd` jail for fail2ban and enables the service;
7. turns on unattended security upgrades;
8. requires the deploy key, then clones or fast-forwards the repository into
   `/opt/afterword`;
9. **stops on the first run** after copying `deploy/.env.example` to
   `deploy/.env` and `deploy/api.env.example` to `deploy/api.env` (both mode
   600, owned by `afterword`) and telling you what to fill in;
10. on every later run: `docker compose pull`, `docker compose up -d`,
    `docker compose run --rm api /migrate up`, then polls
    `https://api.$DOMAIN/healthz` for up to five minutes.

Migrations run **after** the stack is up and **before** the health poll on
purpose: `/healthz` reports `migrations: "pending"` and 503 until they have
run, so an unmigrated API correctly refuses to call itself healthy.

Overridable with environment variables: `APP_USER`, `APP_DIR`, `REPO_URL`,
`REPO_BRANCH`, `DEPLOY_KEY`, `HEALTH_ATTEMPTS`, `HEALTH_INTERVAL`.

---

## 3. Configuration: two files, two audiences

| File | Read by | Holds |
|---|---|---|
| `deploy/.env` | Docker Compose itself, for `${...}` interpolation | Infrastructure: domain, image tag, database and MinIO credentials, bucket names, bot and backup settings |
| `deploy/api.env` | The `api` container only, as `env_file` | The API's own secrets and tuning: `JWT_SECRET`, SMTP credentials, Google OAuth secret, timeouts |

The split exists so that one file is not handed wholesale to every container.
`api.env` never reaches Postgres, MinIO, the bot or the backup job; `.env`
values that the API genuinely needs (`DATABASE_URL`, the base URLs, the S3
endpoint and keys) are passed to it explicitly in the compose file, so nothing
is duplicated between the two files. Both are mode 600, owned by `afterword`,
and never committed — `deploy/.gitignore` un-ignores only the example files.

Generate the secrets:

```bash
openssl rand -base64 48 | tr -d '\n='          # JWT_SECRET (48+ chars, minimum is 32)
openssl rand -base64 32 | tr -d '\n=/+'        # POSTGRES_PASSWORD
openssl rand -base64 32 | tr -d '\n=/+'        # MINIO_ROOT_PASSWORD
```

### `deploy/.env`

| Variable | Notes |
|---|---|
| `DOMAIN` | Bare domain, e.g. `afterword.app`. Caddy builds `api.`, `app.` and `s3.` from it. |
| `ACME_EMAIL` | Let's Encrypt expiry notices go here. |
| `TAG` | Image tag to run. The deploy workflow rewrites this line on every deploy. |
| `COMPOSE_PROFILES` | Which optional services `docker compose up -d` manages. Empty today; set it to `workers` when Phase C/E land, `web,workers` after Phase F. Without it, `up -d` silently leaves profiled services untouched and a deploy would never update them. |
| `POSTGRES_USER`/`_PASSWORD`/`_DB` | Must match `DATABASE_URL`; keep them in sync by hand. |
| `DATABASE_URL` | `postgres://USER:PASSWORD@postgres:5432/DB?sslmode=disable`. The hostname is the compose service; traffic never leaves the host, so `sslmode=disable` is correct here. |
| `LOG_LEVEL` | `info` in production. |
| `APP_BASE_URL`, `API_BASE_URL` | `https://app.$DOMAIN` and `https://api.$DOMAIN`. `APP_BASE_URL` is also the only allowed CORS origin. |
| `MINIO_ROOT_USER`/`_PASSWORD` | MinIO's admin credentials. They go to `minio`, `minio-init`, the transcribe worker and the backup mirror, which needs every bucket. The API does not get them. |
| `S3_ACCESS_KEY`/`S3_SECRET_KEY` | The API's own MinIO keys. `minio-init` creates a service account with exactly these credentials and a policy limited to `Get`/`Put`/`Delete`/`List` on the buckets in `S3_BUCKETS`, and the `api` service receives them instead of the root credentials. Change them here and re-run `docker compose up -d minio-init api`; the init container updates the existing account in place. |
| `S3_ENDPOINT` | **`https://s3.$DOMAIN`, not `http://minio:9000`.** See below. |
| `S3_REGION`, `S3_USE_SSL` | `us-east-1` and `true`, matching the endpoint above. |
| `S3_BUCKETS` | Space-separated list used by `minio-init` and by the backup job. |
| `PRIVACY_URL` | **Required by the bot image.** The consent announcement reads this URL to every participant, so it must describe *this* deployment. Compose refuses to render without it. |
| `BOT_WORKER_REPLICAS` | Bots per box. One on a 4 GB box; see section 7. |
| `BACKUP_*` | Offsite target. `BACKUP_PROVIDER` is an rclone S3 provider name (`Cloudflare` for R2, `Other` for B2's S3 API). `BACKUP_MAX_DELETE` (default 1000) caps how many objects one mirror may delete off site; see section 8 before raising it. |

### `deploy/api.env`

| Variable | Notes |
|---|---|
| `JWT_SECRET` | Minimum 32 characters. Rotating it signs every existing session out. |
| `DATABASE_MAX_CONNS` | 20 is right for a 4 GB box, and the API refuses to start below 10. The scheduler pins one pooled connection per running task for as long as it holds that task's leader lock; with five scheduled tasks, a pool of 10 leaves half of it for request traffic and for the tasks' own queries. A scheduler tick that cannot get a connection within 5s logs a warning and is skipped rather than blocking, so an undersized pool shows up in the log instead of as a stall. |
| `TRUSTED_PROXY_CIDRS` | `172.16.0.0/12` covers the default Docker bridge networks, so the API trusts Caddy's `X-Forwarded-For`. Without it every log line and rate limit sees Caddy's container IP. |
| `REQUEST_TIMEOUT`, `SHUTDOWN_TIMEOUT` | `30s` and `15s`. |
| `ABANDONED_UPLOAD_TTL` | `24h`. An hourly sweep deletes meetings still `pending` after this long and queues their audio and transcript objects for purge, so a client that asked for upload URLs and never finalized does not leave storage behind. Raise it if your users routinely upload multi-hour recordings over slow links. |
| `GOOGLE_CLIENT_ID`/`_SECRET` | Both or neither; the API refuses to start with only one. `GOOGLE_REDIRECT_URL` must equal `https://api.$DOMAIN/v1/auth/google/callback` and be registered in the Google console. |
| `EMAIL_SENDER` | `smtp` in production. `log` writes login codes to the container log and is development only. `SMTP_HOST` and `SMTP_FROM` become required when it is `smtp`. |
| `SMTP_FROM` | Quote it — `"Afterword <no-reply@example.com>"` — because the value contains spaces and angle brackets. Compose strips the quotes when it loads the file. |
| `SMS_SENDER` | `noop` until an SMS provider is wired up. |
| `S3_BUCKET_*` | Per-bucket names for the storage layer. Keep them in step with `S3_BUCKETS` in `.env`, which `minio-init` and `backup.sh` read directly. If the storage layer lands with different variable names, rename these and leave `S3_BUCKETS` alone. |

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

Every service is capped: memory, CPU, and container logs (`json-file`, 10 MB ×
3 files, from a shared YAML anchor). `/etc/docker/daemon.json` sets the same
caps as the daemon-wide default, so anything started outside compose is capped
too.

| Service | Image | Memory | CPU | Notes |
|---|---|---|---|---|
| `caddy` | `caddy:2.10-alpine` | 256m | 0.5 | The only service publishing ports: 80/tcp, 443/tcp, 443/udp. Automatic Let's Encrypt, HSTS, `nosniff`, referrer policy, JSON access logs to stdout. |
| `postgres` | `pgvector/pgvector:pg16` | 1g | 1.5 | `shm_size: 256m` so parallel query and larger sorts do not fall over. `pg_isready` healthcheck. |
| `minio` | pinned `RELEASE.2025-09-07…` | 256m | 1.0 | S3 API on 9000 behind Caddy; console on `127.0.0.1:9001` only. |
| `minio-init` | pinned `mc` release | 128m | 0.25 | Creates the buckets, sets them non-public, exits. Everything that needs storage waits for it to complete successfully. |
| `api` | `ghcr.io/judeotine/afterword-api:${TAG}` | 512m | 1.0 | Waits for Postgres and MinIO to be healthy. Ships two binaries: `/api` (default command) and `/migrate`. |
| `web` | `…/afterword-web:${TAG}` | 512m | 1.0 | Profile `web`. Off until Phase F; `app.$DOMAIN` returns 502 until then. |
| `transcribe-worker` | `…/afterword-transcribe-worker:${TAG}` | 1200m | 1.5 | Profile `workers`. Off until Phase C. |
| `bot-worker` | `…/afterword-bot:${TAG}` | 1000m | 1.5 | Profile `workers`. `shm_size: 512m` for Chromium. Scale with `--scale`. |
| `backup` | `…/afterword-backup:${TAG}` | 256m | 0.5 | Purpose-built image (Alpine 3.21 with `postgresql16-client`, `rclone`, `tzdata` and `backup.sh` baked in). Loops, running the backup at `BACKUP_HOUR_UTC` every night. |

`caddy` depends on `api` and `minio` with `service_started`, not
`service_healthy`: an unhealthy MinIO must not be able to take the edge — and
with it every certificate renewal and the API — offline.

Bring up a profile explicitly, or set `COMPOSE_PROFILES` in `.env` so every
`up -d` includes them:

```bash
docker compose --profile workers up -d
docker compose --profile web --profile workers up -d
```

`docker compose config` with no profile flag and an empty `COMPOSE_PROFILES`
shows only the always-on services; that is expected, not a missing service.

### Migrations

The API image contains a second binary at `/migrate` built from
`services/api/cmd/migrate`. It reads `DATABASE_URL` and `MIGRATIONS_DIR`
(default `/migrations`, where the image puts the checked-in migrations):

```bash
docker compose run --rm api /migrate up
docker compose run --rm api /migrate down 1
docker compose run --rm api /migrate down all
docker compose run --rm api /migrate version
docker compose run --rm api /migrate force 9
```

`force` sets `schema_migrations` to a version without running anything. It is
the escape hatch for a dirty schema and nothing else — see section 6.

**`down` past migration 0010 destroys every share token.** 0010 replaced the
plaintext `share_links.token` with `token_hash`; its down path copies the hash
back into `token`, because the plaintext is gone and cannot be recovered. Every
share URL already handed out stops working, and the stored value is a hash
masquerading as a token — running `up` again hashes it a second time. If you
must roll back across 0010, restore from a backup taken before it instead, and
tell anyone holding a share link to ask for a new one.

The same binary backs `make migrate-up`, `migrate-down`, `migrate-down-all`,
`migrate-version` and `migrate-force FORCE_VERSION=N` in `services/api`, so
development and production run one migrator, not two.

The image declares `CMD ["/api"]` rather than an `ENTRYPOINT` precisely so that
`docker compose run --rm api /migrate up` replaces the command. Plain
`docker run` behaviour is unchanged: with no arguments it starts the API.

### What `/healthz` actually proves

`GET /healthz` returns `{"status", "db", "migrations"}` and is the single
signal every automated check uses.

| `db` | `migrations` | Code | Meaning |
|---|---|---|---|
| `up` | `ok` | 200 | Database answers and the schema matches the migrations in this image. |
| `up` | `ahead` | 200 | The schema is newer than this image — normal during a rollback, which is why it is not a failure. |
| `up` | `pending` | 503 | Migrations have never been run (`schema_migrations` does not exist, or is empty), or the schema is behind this image. |
| `up` | `dirty` | 503 | A migration failed part way through. Needs a human; see section 6. |
| `up` | `unknown` | 503 | `schema_migrations` could not be read for some other reason — a permissions problem, or a driver error. |
| `down` | `unknown` | 503 | The database is unreachable. |
| `unknown` | `unknown` | 503 | The process started without a database pool at all. |

The expected schema version is stamped into the binary at image build time
(`-X …/internal/httpx.ExpectedSchemaVersion`, computed from the migration files
in the build context). When that stamp is absent — a local `go build`, or a
test — the comparison is skipped and only "applied and clean" is enforced.

### Why the `api` service has no container healthcheck

The API runs on `gcr.io/distroless/static-debian12`, which has no shell, no
`curl` and no `wget`, so a `HEALTHCHECK` cannot be expressed. `/healthz` is
checked from outside instead: `bootstrap.sh` and the deploy workflow poll
`https://api.$DOMAIN/healthz` and fail the deploy if it never answers, and the
uptime monitor in section 9 watches it continuously. Nothing in the compose
file uses `depends_on: api: service_healthy`.

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
   input verbatim) and the git ref to check out on the box;
2. builds and pushes `afterword-api` and `afterword-backup` to GHCR, plus
   `afterword-transcribe-worker` and `afterword-web` **if** their Dockerfiles
   exist (detected after checkout, because a job-level `hashFiles` runs before
   any checkout and always returns empty);
3. SSHes to the box with `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`, fetches tags and
   checks out **the exact ref being deployed** (`git checkout --detach`), not
   whatever `main` happens to be;
4. rewrites the `TAG=` line in `deploy/.env` atomically (write to a temp file in
   the same directory, `chmod 600`, `mv`), pulls, brings the stack up, runs
   `/migrate up`, and polls `/healthz` for five minutes. Every step is checked:
   a failed pull, a failed `up`, or a failed migration aborts immediately
   instead of letting the health poll pass against the container that is still
   running;
5. on failure it first asks `/migrate version`. If the schema is **dirty** it
   refuses to roll back and prints the manual procedure. Otherwise it
   re-deploys the tag in `deploy/.last_tag`. Either way the run fails;
6. `deploy/.last_tag` is written **only after** the new tag has passed the
   health check, so it always names a version that actually served traffic.

Required repository secrets: `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`. The job
uses the `production` environment, so you can add a required reviewer there.

**Rollback rolls back images, not schema.** The previous tag is redeployed and
`/migrate up` is a no-op against the newer schema, which the old image reports
as `migrations: "ahead"` and still serves. Keep migrations backward-compatible
for one release: add columns before writing them, drop them a release later.

**When the schema is dirty.** golang-migrate marks `schema_migrations.dirty`
when a migration fails half way. Nothing automatic will touch the database in
that state. On the box, in `/opt/afterword/deploy`:

```bash
docker compose run --rm api /migrate version
docker compose exec postgres psql -U afterword -d afterword
# inspect what the failed migration did and finish or undo it by hand
docker compose run --rm api /migrate force <last-good-version>
docker compose run --rm api /migrate up
curl -fsS https://api.$DOMAIN/healthz
```

Take a backup (`docker compose exec backup /usr/local/bin/backup.sh --once`)
before you start, and prefer restoring last night's dump over hand-editing if
the failed migration wrote data.

### Upgrading an existing box to the two-file configuration

Boxes bootstrapped before the `.env` / `api.env` split have one file, and the
`api` service now expects two. The deploy workflow checks for `deploy/api.env`
**before** it touches `.env`, `.last_tag` or any container: if the file is
missing it prints this procedure and stops, leaving the running stack exactly
as it was. Do the migration once, by hand, as the `afterword` user:

```bash
cd /opt/afterword/deploy
cp api.env.example api.env && chmod 600 api.env
```

Then move these values out of `.env` and into `api.env`, keeping your real
secrets rather than the example placeholders:

`JWT_SECRET`, `DATABASE_MAX_CONNS`, `TRUSTED_PROXY_CIDRS`, `REQUEST_TIMEOUT`,
`SHUTDOWN_TIMEOUT`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
`GOOGLE_REDIRECT_URL`, `EMAIL_SENDER`, `SMTP_HOST`, `SMTP_PORT`,
`SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_STARTTLS`, `SMS_SENDER`,
and the four `S3_BUCKET_*` names.

Delete those lines from `.env`, and add the variables introduced since:
`COMPOSE_PROFILES=` (empty unless you run workers), `BACKUP_MAX_DELETE=1000`,
and `S3_ACCESS_KEY`/`S3_SECRET_KEY` for the API's scoped MinIO account (any
new key and a generated secret; `minio-init` creates the account on the next
`up`). Add `ABANDONED_UPLOAD_TTL=24h` to `api.env` while you are there. Quote `SMTP_FROM` if it contains spaces. Then check
your work before re-running the deploy:

```bash
docker compose config -q
docker compose config | grep -A2 'SMTP_FROM\|JWT_SECRET'
```

`docker compose config -q` fails loudly on anything still missing, which is
much cheaper than finding out during a deploy.

### By hand

```bash
cd /opt/afterword && git fetch --tags origin && git checkout --detach cloud-v0.3.0
cd deploy
sed -i 's/^TAG=.*/TAG=0.3.0/' .env
docker compose pull && docker compose up -d
docker compose run --rm api /migrate up
curl -fsS https://api.$DOMAIN/healthz
```

---

## 7. What actually fits on 4 GB

Be honest about the budget. The declared limits are:

| Always on | | Optional | |
|---|---|---|---|
| `postgres` | 1024m | `transcribe-worker` | 1200m |
| `api` | 512m | `bot-worker` | 1000m |
| `caddy` | 256m | `web` | 512m |
| `minio` | 256m | | |
| `backup` | 256m | | |
| **subtotal** | **~2.3 GB** | | |

A 4 GB box has roughly 3.6 GB usable after the kernel and the Docker daemon.
The always-on set leaves about 1.3 GB, which is **one** optional service at a
time — the transcribe worker *or* one bot, not both, and not with the web app
alongside them. Running everything in the table needs 8 GB or a second box.
Treat these limits as a budget, not a target: a bot that hits its 1000m cap is
OOM-killed and the meeting is lost, so give bots their own box before you go
looking for headroom here.

Practical shapes:

- **4 GB, no bots.** Default profile plus `transcribe-worker`. Desktop capture
  and cloud transcription work; the bot does not.
- **4 GB, one bot.** Default profile plus `bot-worker`, `COMPOSE_PROFILES=workers`
  and `BOT_WORKER_REPLICAS=1`, with transcription left to the desktop app.
- **8 GB.** Everything, one bot and one transcribe worker, with room to spare.
- **Two boxes** (recommended past one concurrent meeting): primary runs the
  always-on set plus the transcribe worker; a second box runs bots only.

Bots on a second box:

1. Provision another Ubuntu box and run `bootstrap.sh` on it.
2. In its `deploy/.env`, keep `API_BASE_URL=https://api.$DOMAIN` and the same
   `PRIVACY_URL` and `BOT_NAME`, and set `COMPOSE_PROFILES=workers`.
3. Start only the workers there: `docker compose up -d bot-worker`.
4. Leave `caddy`, `postgres`, `minio`, `api` and `backup` stopped on that box.
   The bot worker reaches the API over `https://api.$DOMAIN`, so nothing needs
   a private network.

More bots on one box, only if the RAM is genuinely there:

```bash
docker compose --profile workers up -d --scale bot-worker=2
```

Postgres and MinIO stay on the primary box. When they become the bottleneck,
move MinIO's data to R2 or B2 — the code only speaks S3, so that is an
`S3_ENDPOINT` and credentials change.

---

## 8. Backups and the restore drill

The `backup` container runs `backup.sh --loop`, which sleeps until
`BACKUP_HOUR_UTC` (default 03:00 UTC) and then, every night:

1. `pg_dump --format=custom --compress=9` into the `backup-staging` volume;
2. verifies the dump with `pg_restore --list` before trusting it;
3. `rclone copyto` the dump to
   `offsite:$BACKUP_BUCKET/$BACKUP_PREFIX/postgres/postgres-<stamp>.dump` and
   confirms it is listable off site;
4. **deletes the local dump.** The staging volume is a work area, not an
   archive; retention lives off site, where a disk failure cannot reach it;
5. mirrors each bucket in `S3_BUCKETS` to
   `offsite:…/objects/<bucket>`, with three guards (see below);
6. prunes off-site dumps and soft-deleted objects older than
   `BACKUP_RETENTION_DAYS` (30).

Every step is checked. If any one fails, the run logs `backup failed` and —
in `--loop` mode — waits for the next window rather than reporting success. A
truncated dump is never uploaded and never announced as complete.

The database steps are strictly sequential: a failed dump or upload stops the
run immediately. Object mirroring is not. Each bucket is mirrored on its own,
a failure is logged and counted, and the remaining buckets are still mirrored
and the off-site prune still runs, so one broken bucket cannot quietly skip the
others or let retention drift. The run then reports failure overall.

### The mirror guards

`rclone sync` makes the destination match the source, so a bug that empties
MinIO would, unguarded, empty the only off-site copy on the next run. Three
things prevent that:

- **Empty-source refusal.** If a bucket lists zero objects but the off-site
  copy is not empty, the run fails loudly instead of syncing. If both are
  empty — a fresh deployment — it logs and moves on.
- **`--max-delete $BACKUP_MAX_DELETE`** (default 1000) aborts a sync that would
  delete more than that many objects. This is a tripwire, not a policy: a
  legitimate large sweep — a retention run that expires a year of audio, or a
  workspace deleting thousands of meetings — will trip it, and that bucket will
  then **fail every night** until you raise `BACKUP_MAX_DELETE` in `deploy/.env`
  and restart the backup container. Confirm the deletions were intended, raise
  it for one run, then put it back.
- **`--backup-dir …/deleted/<stamp>/<bucket>`** turns every deletion into a
  dated soft-delete, kept for `BACKUP_RETENTION_DAYS`. A wrong sync is
  recoverable for 30 days.

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
`pg_isready`; pulls the named dump from off site if it is not already staged;
runs `pg_restore --clean --if-exists --exit-on-error`; optionally syncs the
object buckets back into MinIO with `--objects`; and leaves the stack stopped
so you can inspect before starting it.

It reuses the `backup` image, so the `pg_restore` doing the work is the same
major version as the `pg_dump` that made the file, and no package is installed
at restore time.

Note what a restore does **not** undo: it replaces database contents, but if
the dump predates a migration the running image expects, `/healthz` will say
`migrations: "pending"` — run `/migrate up` afterwards. Record the drill result
(date, dump used, time to restore) so the number is known before an incident.

---

## 9. Monitoring

- **Uptime check.** Point any free monitor (UptimeRobot, Better Stack, a cron
  on another box) at `https://api.$DOMAIN/healthz` every minute, expecting HTTP
  200. The body says which half is wrong: `"db":"down"` is an unreachable
  database, `"migrations":"pending"` is an un-migrated one, `"dirty"` needs the
  procedure in section 6. A TCP check would miss all three.
- **Metrics.** `GET /metrics` on the api container serves Prometheus text
  (`http_requests_total`, `http_request_duration_seconds`,
  `http_requests_in_flight`, `afterword_api_build_info`, plus Go runtime
  metrics). It is not exposed through Caddy. Scrape it from a container on the
  same network, or add a `/metrics` route behind basic auth in the Caddyfile if
  you need it externally.
- **Logs.** Everything logs JSON to stdout. Both the compose file and
  `/etc/docker/daemon.json` cap them at 10 MB × 3 files per container, so logs
  cannot fill the disk on their own. `docker compose logs -f <service>` reads
  them.
- **TLS.** Caddy renews automatically. `docker compose logs caddy | grep -i
  certificate` after any DNS change.
- **Backups.** `docker compose logs backup --since 24h` should show a
  `backup complete` line every morning. A `backup failed` line, or no line at
  all, is an incident.

---

## 10. When the disk fills

40 GB goes to Postgres, MinIO objects, and Docker images, in that order of
surprise. Container logs are capped, so they are no longer a likely cause.
Symptoms: Postgres refuses writes, uploads 500, the API looks healthy but
nothing persists.

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
3. Check the staging volume: `backup.sh` deletes each dump after uploading it,
   so a pile of `postgres-*.dump` files in the `backup-staging` volume means
   uploads have been failing. Fix the off-site credentials first, then
   `docker compose exec backup sh -c 'rm -f /backups/postgres-*.dump'`.
4. Apply retention: the point of per-workspace retention (default 365 days) is
   to bound `audio`, which dominates MinIO. Shorten it for the noisiest
   workspaces, then let the retention sweep delete the objects.
5. Only then resize the volume or the box. Growing the disk with the provider
   and rebooting is a five-minute job and cheaper than a bad prune.

Prevention: alert at 75% used. Audio is stored as 24 kbps Opus, so roughly
10 MB per recorded hour — the disk should last a long time unless retention is
never applied or images are never pruned.

### Bounding what an upload URL can write

A pre-signed `PUT` cannot express a size *range*: S3 and MinIO can pin an exact
`Content-Length` into the signature or nothing at all. So the size has to come
from the client. `POST /v1/meetings` and `POST /v1/meetings/{id}/upload-urls`
take an optional `size_bytes`; when it is present the API validates it against
`S3_MAX_AUDIO_MB`, signs exactly that length into the audio `PUT`, and echoes it
back as `upload.size_bytes` so the client knows the contract. An upload of any
other length is then refused by MinIO before a byte is stored.

When `size_bytes` is omitted the old behaviour stands: the URL is unbounded,
`upload.max_bytes` advertises the ceiling as advice, and `S3_MAX_AUDIO_MB` and
`S3_MAX_TRANSCRIPT_MB` are only checked at finalize, after the object has
landed — an over-sized object is refused and left for the purge sweep, but it
was already written to the disk once.

For those clients, a bucket quota is the ceiling, and MinIO enforces it at
write time:

```bash
docker compose exec minio mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
docker compose exec minio mc quota set local/audio --size 30GB
docker compose exec minio mc quota set local/transcripts --size 2GB
```

Size the audio quota to the disk you are willing to lose, not to one upload.
The hourly abandoned-upload sweep (`ABANDONED_UPLOAD_TTL`) reclaims whatever a
client wrote and never finalized, whether or not it declared a size.

---

## 11. Security baseline

- TLS everywhere; Caddy redirects 80 to 443 and sends HSTS with a one-year
  max-age. Do not enable HSTS preload submission until the domain is settled.
- Only Caddy publishes ports. Everything else talks over the compose network.
  MinIO's console is loopback-only.
- Secrets live only in `deploy/.env` and `deploy/api.env` (both mode 600) and
  in GitHub Actions secrets. `api.env` goes to the API container and nowhere
  else. Nothing secret is committed.
- The API holds a scoped MinIO service account (`S3_ACCESS_KEY`/`S3_SECRET_KEY`),
  not the root credentials: it can read, write and delete inside the four
  buckets and do nothing else — no admin API, no new buckets, no other keys.
- Revoking a share link stops new reads, not reads already in flight. A
  pre-signed download URL issued before the revocation stays valid until it
  expires, for up to `S3_DOWNLOAD_TTL` (default 15m), because the signature is
  checked by MinIO and never reaches the API. Treat a leaked recording as
  leaked for that window; shorten `S3_DOWNLOAD_TTL` if that window is too long
  for you.
- `ufw` denies inbound except 22, 80 and 443. Harden SSH further by disabling
  password authentication in `/etc/ssh/sshd_config`.
- fail2ban bans an IP for an hour after five failed SSH attempts.
- Unattended security upgrades are on; reboot the box during a quiet window
  when `/var/run/reboot-required` appears.
- Rotate `JWT_SECRET`, the Postgres password, the MinIO credentials and the
  deploy key on a schedule; each rotation is an edit plus
  `docker compose up -d`, except the Postgres password, which also needs
  `ALTER ROLE` inside the database.
