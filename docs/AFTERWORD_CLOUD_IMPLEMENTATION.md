# Afterword Cloud: implementation plan

This document is the brief for the Claude Code sessions that build the hosted
side of Afterword. It describes the product Afterword becomes, the
architecture, the data model, the billing model, and a phased task list with
acceptance criteria. Facts about the current codebase are as of `main` at
2026-09-03 (Phase 1 hardening merged, bot scaffold present, CI green).

Positioning in one sentence: a Fathom-class meeting notetaker that is cheaper
to run and to buy, pays by the minute through mobile money, keeps the
local-first desktop app, and is built for teams in Uganda and the wider
African market first.

Standing rules for every session:

- New code carries no comments. Names and tests explain intent.
- Everything runs on one cheap VPS with Docker Compose. No managed queues,
  managed databases, or serverless. Scale later by adding boxes.
- Every API query is scoped by workspace. Consent before the bot records is
  mandatory. The MIT notice for the Meetily code stays.

---

## 1. Product decisions already made

| Decision | Value |
|---|---|
| Business model | Proprietary SaaS. The GitHub repo is private. `LICENSE.md` and `NOTICE.md` keep the Meetily attribution; nothing else in the product says "open source". |
| Pricing | Credits, pay-as-you-go minutes. Free monthly allowance, then top-ups. Teams share a pooled balance. No per-user minimums. |
| Payments | "Nylon Pay", the founder's existing payment module from a related project (mobile money and cards), behind an adapter interface. Real endpoints and webhook format are plugged in when its docs or repo path are provided. |
| Hosting | The cheapest credible VPS: one Ubuntu 24.04 box, 2 vCPU, 4 GB RAM, 40 GB disk (Hetzner CX22 class, about EUR 4 per month; any provider with the same shape works). Docker Compose runs Caddy (TLS), the Go API, the web app, Postgres 16 with pgvector, MinIO, and the workers. Off-site backups to a cheap object store (Backblaze B2 or Cloudflare R2). |
| Capture | Desktop app (local, free, unlimited) and the meeting bot (cloud, metered). |
| Desktop app | Keep the current UI. Add account, credits, teams, sharing, sync. Remove "open source / contribute" copy. |

---

## 2. What Fathom offers and where Afterword wins

Fathom (fathom.ai, checked 2026-09-03):

| Plan | Price | Notes |
|---|---|---|
| Free | $0 | Unlimited recordings and transcripts, bot or bot-free capture, instant summaries, clips, playlists, search |
| Premium | $20/user/mo ($16 annual) | Advanced summaries, action items, conversational assistant, custom bot |
| Team | $19/user/mo ($15 annual), 2-user minimum | Global search, playlists, comments, folders, keyword alerts |
| Business | $34/user/mo ($25 annual), 2-user minimum | CRM field sync, deal view, coaching scorecards, custom summaries |
| Enterprise | custom | SSO/SCIM, retention controls |

Supported: Zoom, Google Meet, Teams. Integrations: HubSpot, Salesforce, Slack, Notion, Asana, Zapier, public API, MCP. SOC 2 Type II, GDPR, HIPAA.

Afterword's edges, each mapped to tasks below:

1. **Cheaper for the target market.** Credits priced in local currency, bought in one-off mobile-money payments, no per-user minimum, no annual commitment.
2. **Local-first is free and real.** The desktop app transcribes and summarises on-device at no cost and works offline.
3. **Bandwidth-aware.** Transcript-first sync, resumable audio upload, Opus audio in storage.
4. **Your model, your data.** Local model, hosted model, or bring-your-own key. No training on customer audio.
5. **Team features without a Team tier.** Shared library, folders, comments, share links, keyword alerts for every workspace; the metered resource is minutes, not seats.
6. **Africa-first.** Phone-number sign-in, WhatsApp share targets, Luganda and Swahili summaries through the existing summary-language pipeline, a VPS located close to the market.

Parity items that must exist: action items, Ask-your-meetings chat, clips, playlists, global search, comments, folders, Slack/Notion/HubSpot, calendar auto-join, public API.

---

## 3. Architecture

```
                   VPS (Docker Compose, one host)
 +----------------------------------------------------------------------+
 |  caddy (443/80, auto TLS)                                            |
 |    -> api   (Go, /v1 REST, webhooks, cron, job dispatcher)           |
 |    -> web   (Next.js)                                                |
 |    -> minio console (admin only)                                     |
 |  postgres 16 + pgvector          minio (S3 API, local disk)          |
 |  transcribe-worker (afterword-transcribe CLI + summariser)           |
 |  bot-worker x N   (Chromium + PulseAudio, from bot/Dockerfile)       |
 |  backup (nightly pg_dump + minio mirror -> B2/R2)                    |
 +----------------------------------------------------------------------+
        ^ desktop app sync (HTTPS)        ^ browsers (web app)
```

- `services/api` (Go 1.23, chi, pgx + sqlc, golang-migrate, zerolog). One binary that also runs the job dispatcher and scheduled tasks (monthly credit grants, calendar polling, retention sweeps). Jobs live in a Postgres `jobs` table claimed with `FOR UPDATE SKIP LOCKED`; no external queue.
- `services/web` (Next.js 14 app router; reuses components from `frontend/src/components` that do not depend on Tauri).
- `services/transcribe-worker`: container built from `crates/afterword-core` with the `afterword-transcribe` CLI and a small Go runner that claims `transcribe` and `summarise` jobs, moves files through MinIO, and writes results. CPU Whisper `base` or `small` on the VPS; a GPU box can be added later for `large-v3-turbo`.
- `bot/` (existing TypeScript service) becomes the bot worker: claims `bot.join` jobs from the API, records, uploads to MinIO, enqueues transcription. One 4 GB box runs one or two concurrent bots; more boxes run more.
- `frontend/` desktop app gains an `AccountProvider`, a sync queue, credits and team UI.
- `deploy/` holds `docker-compose.yml`, `Caddyfile`, `.env.example`, backup scripts, and a `bootstrap.sh` that turns a fresh Ubuntu box into a running stack.

Everything stays in this monorepo. `services/` and `deploy/` are new.

### 3.1 Auth

- Email OTP and phone OTP (SMS provider behind an interface; Africa's Talking or Twilio unless Nylon Pay's family offers SMS). Google OAuth second.
- Sessions: 15-minute JWT plus 30-day refresh token. Desktop stores the refresh token in the OS keychain through the existing `SecretStore`; web uses an httpOnly cookie.
- Workspaces from day one: `users`, `workspaces`, `memberships(role: owner|admin|member)`. Personal use is a workspace of one.

### 3.2 Data model (Postgres)

```
users(id, email, phone, name, created_at)
workspaces(id, name, slug, bot_name, retention_days, created_at)
memberships(workspace_id, user_id, role, created_at)
meetings(id, workspace_id, owner_user_id, title, source: desktop|bot|import,
         platform: meet|zoom|teams|null, started_at, duration_s,
         consent_state, visibility: private|workspace|link, folder_id,
         audio_object, transcript_object, status, created_at)
transcript_segments(id, meeting_id, seq, speaker, start_s, end_s, text,
         tsv tsvector generated always, embedding vector(768) null)
summaries(id, meeting_id, template_id, language, markdown, model,
         action_items jsonb, created_at)
folders(id, workspace_id, name, parent_id)
comments(id, meeting_id, user_id, at_s, body, created_at)
clips(id, meeting_id, start_s, end_s, title, object, share_token)
share_links(id, meeting_id, token, permission: view|comment, expires_at)
keyword_alerts(id, workspace_id, user_id, phrase, channel)
bot_jobs(id, workspace_id, meeting_url, platform, scheduled_at, status,
         worker_id, minutes_used, consent_announced_at, error)
calendar_connections(id, user_id, provider, refresh_token_enc, auto_join_rule)
jobs(id, kind, payload jsonb, run_at, attempts, locked_by, locked_at,
         status, last_error)
credit_ledger(id, workspace_id, delta_minutes, reason: grant|purchase|
         bot_usage|transcribe_usage|summary_usage|ask_usage|refund|adjust,
         ref_id, created_at)
credit_packs(id, name, minutes, price_minor, currency, active)
payments(id, workspace_id, provider, provider_ref, amount_minor, currency,
         minutes, status: pending|paid|failed|refunded, raw jsonb, created_at)
api_tokens(id, workspace_id, hash, scopes, last_used_at)
devices(id, user_id, name, platform, last_sync_at)
audit_log(id, workspace_id, actor_user_id, action, target, at)
```

Rules: every query takes `workspace_id`; membership is checked in middleware and again in the repository layer. Migrations are checked in. Balances are computed as `SUM(delta_minutes)` with a debit that fails when the result would go below zero, inside one transaction.

### 3.3 Storage

- MinIO on the VPS behind the S3 API; the code only ever speaks S3, so moving to R2 or B2 later is a config change.
- Buckets: `audio`, `transcripts`, `clips`, `exports`. Pre-signed URLs for upload and download; the API never proxies media.
- Audio stored as Opus 24 kbps (FFmpeg in the worker) to keep a 40 GB disk useful for a long time. Retention per workspace (default 365 days).
- Nightly `pg_dump` and MinIO mirror to B2 or R2 with 30-day retention; a restore script that is actually tested in G4.

### 3.4 Jobs

- `jobs` table, `FOR UPDATE SKIP LOCKED`, exponential backoff, dead-letter after 5 attempts, visible in an admin page. Idempotency keys on every job. The `bot_jobs` row is the source of truth for what the UI shows.
- Scheduled work runs from the API process with a leader lock in Postgres (`pg_try_advisory_lock`): monthly grants, calendar polling every 5 minutes, retention sweep nightly.

### 3.5 Credits and billing

- Bot minute = 1 credit. Cloud transcription minute of uploaded audio = 1 credit. Desktop on-device transcription = 0. Hosted summary = 0.1 credit per transcript minute, rounded up per meeting; bring-your-own-key summaries = 0. Ask query = 1 credit.
- Free grant per workspace per month (proposal 300, expiring; founder decision). Purchased credits do not expire.
- Purchase flow: `POST /v1/billing/checkout {pack_id}` creates a pending `payments` row and asks the provider to start a payment (mobile-money push or card redirect). Provider webhook `POST /v1/billing/webhooks/{provider}` is signature-verified and idempotent on `provider_ref`; on success it inserts a `credit_ledger` purchase row.
- Packs live in `credit_packs`, so prices change without a deploy.
- Entitlement: the bot refuses to join when the balance is below the estimated minutes (default 60); cloud transcription checks before enqueue; the desktop app reads a signed balance snapshot and keeps local features working offline.
- Provider interface (Go):
  ```go
  type PaymentProvider interface {
      StartPayment(ctx context.Context, req StartPaymentRequest) (StartPaymentResponse, error)
      VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error)
      Refund(ctx context.Context, providerRef string, amount Money) error
  }
  ```
  `nylonpay` implements it once its docs exist; `fake` drives tests and staging.

### 3.6 Search and Ask

- Keyword: `websearch_to_tsquery` over `transcript_segments.tsv`, ranked, filtered by workspace, folder, date, participant.
- Semantic: embeddings per 30 to 60 s chunk in `pgvector`, hybrid ranking by reciprocal rank fusion. Embedding provider behind an interface (hosted API by default; local model later).
- Ask: retrieval over the workspace's segments plus the chosen LLM (hosted default or the user's own key). Answers cite meeting and timestamp. Metered.

### 3.7 Desktop app changes (keep the UI, add surfaces)

- `AccountProvider`: sign in, workspace switcher, balance in the sidebar footer, "Top up" opens the web checkout in the system browser.
- Sync queue in Rust (`frontend/src-tauri/src/sync/`): SQLite `sync_queue` table, uploader with backoff, resumable audio upload, status events. Conflict rule: desktop owns transcript text; server owns titles edited on the web, sharing, comments.
- Teams: pick the workspace a meeting syncs to; visibility control per meeting.
- Sharing: creates a `share_links` row and copies the URL; WhatsApp and email targets.
- Copy: the About screen loses the open-source block and GitHub button and gains plan and credits; onboarding gets an optional sign-in step (skip keeps local-only mode).
- "Send the notetaker": takes a meeting link or a calendar event, shows the credit estimate, creates a bot job, shows status.

### 3.8 Bot completion

- Jobs persisted through the API; workers claim and report status.
- Upload to MinIO on leave, enqueue `transcribe`, delete local files.
- Calendar auto-join: Google Calendar first, Microsoft 365 second; poll every 5 minutes; per-calendar and per-event opt-in; join one minute before start.
- Zoom and Teams adapters behind the existing `MeetingPlatform` interface with fixture pages like the Meet one.
- Consent announcement mandatory and logged; hosts can remove the bot and the job records it.
- Custom bot name and avatar per workspace.

### 3.9 Web app

- Library with folders, workspace and personal views.
- Meeting page: player synced to transcript, summary editor (BlockNote reused), action items, comments at timestamps, clips, share.
- Search and Ask pages.
- Workspace settings: members and roles, bot name, retention, integrations, credits and payment history, API tokens.
- Public share page by token.

### 3.10 Integrations

- Slack summary posts and keyword alerts; Notion page per meeting; HubSpot first CRM; outbound signed webhooks; public API with tokens; MCP server later.

### 3.11 Deployment on the VPS

- `deploy/docker-compose.yml`: caddy, api, web, postgres, minio, transcribe-worker, bot-worker (scale with `--scale bot-worker=N`), backup. Named volumes for Postgres and MinIO. Resource limits so one runaway bot cannot starve the API.
- `deploy/Caddyfile`: `api.<domain>` and `app.<domain>` with automatic Let's Encrypt.
- `deploy/bootstrap.sh`: installs Docker, creates the `afterword` user, sets up the firewall (22, 80, 443 only), fail2ban, unattended upgrades, clones or pulls the repo, writes `.env` from `.env.example`, runs `docker compose up -d`, runs migrations.
- `deploy/backup.sh` and `deploy/restore.sh` with a documented monthly restore drill.
- GitHub Actions `deploy.yml`: on tag or manual dispatch, build images to GHCR (the bot image already goes there), SSH to the VPS with a deploy key, `docker compose pull && up -d`, run migrations, health check, roll back on failure.
- Observability without paid services: structured JSON logs to files with logrotate, `GET /healthz` and `/metrics` (Prometheus format) scraped by a tiny Grafana Alloy or plain uptime monitoring; Sentry free tier for the web and desktop apps.
- Security baseline: TLS only, secrets only in `.env` (mode 600) and never in the repo, signed URLs, rate limits on auth and checkout, audit log, `DELETE /v1/meetings/:id` purges objects, retention documented.

Sizing: a 2 vCPU / 4 GB box comfortably runs API, web, Postgres, MinIO, one transcribe worker with Whisper `base`, and one bot at a time. When two bots must overlap regularly, add a second box for bot workers only; the compose file already separates them.

---

## 4. Phased task list

Each phase fits one or two Claude Code sessions. Each task names files and acceptance criteria. Conventions: one branch per phase, tests before merge, CI green, plan mode before large changes, no comments in new code.

### Phase A: Backend foundation (`services/api`, `deploy/`)

A1. Scaffold the Go module: chi router, config from env, zerolog, sqlc with pgx, golang-migrate, `GET /healthz`, `GET /metrics`, Dockerfile (distroless), `deploy/docker-compose.dev.yml` with Postgres 16 (pgvector image) and MinIO, Makefile targets (`run`, `test`, `migrate`, `sqlc`, `lint` with golangci-lint). Acceptance: `go test ./...` green; container starts and answers `/healthz`; `docker compose -f deploy/docker-compose.dev.yml up` gives a working local stack.
A2. Migrations for section 3.2 and sqlc queries for every table; a `jobs` package implementing claim, complete, fail with backoff, dead-letter. Acceptance: migrate up and down cleanly; jobs package has concurrency tests with two claimants.
A3. Auth: email OTP, phone OTP (SMS provider interface with a fake), Google OAuth, JWT plus refresh, workspace creation on first sign-in, membership middleware. Acceptance: integration tests for sign-up, refresh, revoke; every other route returns 401 without a token and 403 across workspaces.
A4. Meetings and transcripts API: create meeting, pre-signed upload URLs (MinIO), finalise upload, list, get, update title and visibility, delete with object purge, segments bulk insert, folders. Acceptance: contract tests against MinIO in compose; objects removed on delete.
A5. `deploy/`: production compose, Caddyfile, bootstrap, backup and restore scripts, `deploy.yml`. Acceptance: `bootstrap.sh` run on a fresh Ubuntu 24.04 VM (Multipass or a throwaway VPS) yields a healthy stack over HTTPS; restore drill documented and executed once.

### Phase B: Credits and Nylon Pay

B1. Ledger, balances, packs, monthly grant scheduled task, admin adjust endpoint. Acceptance: property tests that the balance equals the ledger sum; concurrent debits never overdraw.
B2. `PaymentProvider` interface, `fake` provider, `nylonpay` skeleton configured by env (base URL, keys, webhook secret). Acceptance: checkout creates a pending payment and returns the provider's push handle or redirect; webhook marks paid exactly once.
B3. Entitlement middleware for bot and transcription routes. Acceptance: below-balance requests are rejected with an error that includes the top-up URL.
B4. Web checkout and payment history pages; desktop "Top up" opens them in the system browser.
Founder input needed before B2 is finished: Nylon Pay docs or repo path, webhook signature scheme, currencies, refund API, SMS capability.

### Phase C: Transcription and summary workers

C1. `services/transcribe-worker`: claims `transcribe` jobs, pulls audio from MinIO, runs `afterword-transcribe`, uploads `transcripts.json`, inserts segments, converts audio to Opus, debits credits. Acceptance: end-to-end test with a WAV fixture through compose.
C2. Summariser using the templates in `frontend/src-tauri/templates/*.json` through a new `afterword-summarise` CLI in `crates/afterword-core` so desktop and cloud stay identical. Acceptance: same markdown as the desktop app for the same transcript.
C3. Embeddings job and `GET /v1/search`. Acceptance: relevance tests on a fixture library.
C4. `POST /v1/ask` with citations and metering.

### Phase D: Desktop app account, sync, credits, teams, sharing

D1. Remove open-source copy: About screen block and GitHub button, onboarding "Report issues on GitHub" link, README wording; add a plan-and-credits card driven by a `useAccount` hook that reports local-only mode until D2 lands; a single `SUPPORT_URL` constant for support links.
D2. `AccountProvider`, sign-in dialog, keychain-stored refresh token via `SecretStore`, workspace switcher.
D3. Rust sync queue with resumable audio upload and status events. Acceptance: kill the app mid-upload, restart, upload completes; offline leaves local features untouched.
D4. Sharing and visibility controls; WhatsApp and email targets.
D5. "Send the notetaker" flow with credit estimate and job status.
D6. Onboarding sign-in step (skippable); balance in the sidebar.

### Phase E: Bot completion (`bot/`)

E1. API-backed jobs claimed by workers; status via API. Acceptance: existing tests pass with a fake API.
E2. MinIO upload on leave, `transcribe` enqueue, local cleanup.
E3. Calendar connections and auto-join (Google first).
E4. Zoom and Teams adapters with fixture pages.
E5. Custom bot name and avatar; consent logging; host removal handling.
E6. Compose service definition with resource limits; documented scaling to a second box.

### Phase F: Web app (`services/web`)

F1. Scaffold with auth, workspace context, and the desktop app's design tokens.
F2. Library, folders, meeting page with player, transcript, summary editor, action items.
F3. Comments, clips (server-side FFmpeg cut), share links, public share page.
F4. Search and Ask pages.
F5. Workspace settings: members, roles, bot settings, retention, integrations, credits and payments, API tokens.

### Phase G: Integrations and launch

G1. Slack, Notion, HubSpot, outbound webhooks, public API docs.
G2. Keyword alerts.
G3. Privacy policy and terms pages; the bot's `PRIVACY_URL` points at them.
G4. Launch checklist: staging soak with real Meet calls, load test, backup and restore drill, incident runbook, uptime monitor.

---

## 5. Pricing proposal (for the founder to confirm)

| Item | Proposal |
|---|---|
| Free grant | 300 credits per workspace per month, expiring |
| Packs | 100 credits UGX 15,000; 500 credits UGX 60,000; 2,000 credits UGX 200,000 (roughly USD 4, 16, 54) |
| Metering | Bot minute 1; cloud transcription minute 1; hosted summary 0.1 per transcript minute; Ask query 1; desktop on-device features free; bring-your-own-key summaries free |
| Team | Pooled balance, unlimited members, roles free |

Worked comparison: a five-person team running 40 hours of bot-recorded meetings a month uses 2,400 credits, about UGX 240,000 (USD 65). Fathom Team for five is USD 95 per month billed annually, USD 1,140 up front. The VPS running it costs about EUR 4 to 8 per month.

---

## 6. Open decisions for the founder

1. VPS provider and location (Hetzner Falkenstein or Helsinki is cheapest; a Johannesburg or Nairobi provider gives lower latency at a higher price). Domain name for `api.` and `app.`.
2. Nylon Pay: docs, sandbox credentials, webhook format, refund support, SMS capability.
3. Free grant size and pack prices (section 5).
4. Retention default and whether audio is stored by default or transcript-only.
5. Which calendar first (Google assumed), which CRM first (HubSpot assumed).
6. Legal review of recording-consent rules for launch markets before the bot is offered outside internal testing.

---

## 7. How to run this with Claude Code

Start a session per phase. Suggested opening prompt:

> Read `docs/AFTERWORD_CLOUD_IMPLEMENTATION.md`. We are doing Phase A. Enter plan mode, list the files you will create, then implement A1 through A5 with tests, keeping CI green. No comments in new code. Do not start other phases.

Guardrails to repeat each session: no secrets in the repo; every endpoint scoped by workspace; consent before recording stays mandatory; keep the MIT notice; no comments in new code; ask before anything that changes billing amounts or deletes customer data.
