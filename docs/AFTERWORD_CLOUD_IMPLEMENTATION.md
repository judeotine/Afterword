# Afterword Cloud: implementation plan

This document is written to be handed to Claude Code as the brief for the next
sessions. It describes the product Afterword becomes, the architecture, the
data model, the billing model, and a phased task list with acceptance criteria.
Facts about the current codebase are as of `main` at 2026-09-03 (Phase 1
hardening merged, bot scaffold present, CI green on Linux and macOS).

Positioning in one sentence: a Fathom-class meeting notetaker that is cheaper
to run and to buy, pays by the minute through mobile money, keeps the
local-first desktop app, and is built for teams in Uganda and the wider
African market first.

---

## 1. Product decisions already made

| Decision | Value |
|---|---|
| Business model | Proprietary SaaS. The GitHub repo is private. The MIT notice for the Meetily code stays in `LICENSE.md` and `NOTICE.md`; nothing else about "open source" appears in the product. |
| Pricing | Credits, pay-as-you-go minutes. Free monthly allowance, then top-ups. Teams share a pooled balance. No per-user minimums. |
| Payments | "Nylon Pay", the founder's existing payment module from a related project (mobile money and cards). Integrated behind an adapter interface; its real endpoints and webhook format are plugged in when the module's docs or repo path are provided. |
| Hosting | GCP: Cloud Run (API, web, transcription and bot workers), Cloud SQL Postgres, Cloud Storage, Secret Manager, Cloud Tasks, Pub/Sub. One region to start (see open decisions). |
| Capture | Desktop app (local, free, unlimited) and the meeting bot (cloud, metered). The bot is the way to record meetings you did not host on your own machine. |
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

Afterword's edges, each of which maps to a concrete task below:

1. **Cheaper for the target market.** Credits priced in local currency, bought in one-off mobile-money payments, no per-user minimum, no annual commitment. A team of five paying only for the minutes they use should cost a fraction of Fathom Team.
2. **Local-first is free and real.** The desktop app transcribes and summarises on-device at no cost; Fathom's desktop capture still sends audio to their cloud. Offline meetings work.
3. **Bandwidth-aware.** Transcript-first sync, resumable audio upload, low-bitrate audio. Fathom assumes a good connection.
4. **Your model, your data.** Users can pick a local model, a cloud model, or bring their own API key; Afterword never trains on customer audio and says so in the product.
5. **Team features without the Team tier.** Shared library, folders, comments, sharing links, and keyword alerts are available to any workspace; the metered resource is bot and cloud minutes, not seats.
6. **Africa-first operations.** GCP region close to East Africa (see open decisions), phone-number sign-in, WhatsApp share targets, local-language summaries (Luganda, Swahili) via the summary language pipeline that already exists.

Parity items (must exist to be credible): action items, Ask-your-meetings chat, clips, playlists, global search, comments, folders, Slack/Notion/HubSpot, calendar auto-join, public API.

---

## 3. Architecture

```
                 +----------------------+        +--------------------+
Desktop app ---> |  Go API (Cloud Run)  | <----> |  Web app (Next.js) |
(Tauri, local)   |  /v1 REST + webhooks |        |  Cloud Run         |
                 +----------+-----------+        +--------------------+
                            |
        +-------------------+--------------------+------------------+
        |                   |                    |                  |
  Cloud SQL Postgres   Cloud Storage       Pub/Sub + Cloud Tasks   Secret Manager
  (metadata, ledger,   (audio, transcripts,  (jobs: transcribe,
   pgvector search)     clips, exports)       summarise, bot)
                            |
        +-------------------+--------------------+
        |                                        |
  Transcription worker (Cloud Run job,      Bot worker (Cloud Run job or
  afterword-transcribe + summariser)        GKE Autopilot pool: Chromium,
                                            PulseAudio, uploads WAV to GCS)
```

Services and repos:

- `services/api` (Go 1.23, chi or gin, sqlc, golang-migrate). Single deployable.
- `services/web` (Next.js 14 app router, reuses components from `frontend/src/components` where they do not depend on Tauri; shadcn already in use).
- `services/transcribe-worker` (Rust container built from `crates/afterword-core` with the `afterword-transcribe` CLI plus a small Go or Rust wrapper that pulls jobs, downloads audio from GCS, writes results back). CPU Whisper `base`/`small` on Cloud Run; GPU (`L4`) Cloud Run service for `large-v3-turbo` when volume justifies it.
- `bot/` (existing TypeScript service) becomes the bot worker image. Gains job persistence via the API, GCS upload, calendar auto-join, Zoom and Teams adapters.
- `frontend/` desktop app gains an `AccountProvider`, sync queue, credits and team UI.
- `infra/` Terraform for GCP, GitHub Actions deploy workflows.

Everything stays in this monorepo. `services/` is new.

### 3.1 Auth

- Email OTP and phone OTP (SMS through the same provider family as Nylon Pay if it offers messaging; otherwise Twilio/Africa's Talking). Google OAuth second.
- Sessions: short-lived JWT (15 min) plus refresh token (30 days) stored in the desktop app's OS keychain (the `SecretStore` from Phase 1) and in an httpOnly cookie on the web.
- Workspaces from day one: `users`, `workspaces`, `memberships(role: owner|admin|member)`. Personal use is a workspace of one.

### 3.2 Data model (Postgres)

```
users(id, email, phone, name, created_at)
workspaces(id, name, slug, plan, created_at)
memberships(workspace_id, user_id, role, created_at)
meetings(id, workspace_id, owner_user_id, title, source: desktop|bot|import,
         platform: meet|zoom|teams|null, started_at, duration_s,
         consent_state, visibility: private|workspace|link, folder_id,
         audio_object, transcript_object, status, created_at)
transcript_segments(id, meeting_id, seq, speaker, start_s, end_s, text,
         tsv tsvector generated, embedding vector(768) null)
summaries(id, meeting_id, template_id, language, markdown, model,
         action_items jsonb, created_at)
folders(id, workspace_id, name, parent_id)
comments(id, meeting_id, user_id, at_s, body, created_at)
clips(id, meeting_id, start_s, end_s, title, object, share_token)
share_links(id, meeting_id, token, permission: view|comment, expires_at)
keyword_alerts(id, workspace_id, user_id, phrase, channel)
bot_jobs(id, workspace_id, meeting_url, platform, scheduled_at, status,
         worker_id, minutes_used, consent_announced_at, error)
calendar_connections(id, user_id, provider, refresh_token_ref, auto_join_rule)
credit_ledger(id, workspace_id, delta_minutes, reason: grant|purchase|
         bot_usage|transcribe_usage|refund|adjust, ref_id, created_at)
credit_balances(workspace_id, minutes) -- materialised from ledger
payments(id, workspace_id, provider: nylonpay, provider_ref, amount, currency,
         minutes, status: pending|paid|failed|refunded, raw jsonb, created_at)
api_tokens(id, workspace_id, hash, scopes, last_used_at)
devices(id, user_id, name, platform, last_sync_at)
```

Rules: every query is scoped by `workspace_id`; row-level checks in the API layer, not just the UI. Migrations checked in. `pgvector` enabled in Cloud SQL.

### 3.3 Storage

- Buckets: `afterword-audio-{env}`, `afterword-transcripts-{env}`, `afterword-clips-{env}`. Uniform bucket-level access, CMEK optional later.
- Pre-signed (V4) upload and download URLs from the API. The API never proxies media.
- Audio kept as the desktop app's `audio.mp4` (AAC) or the bot's WAV converted to Opus 24 kbps for storage (FFmpeg in the worker). Retention policy per workspace (default 12 months; founder decision).

### 3.4 Jobs

- Pub/Sub topics: `transcribe`, `summarise`, `bot.join`, `notify`. Cloud Tasks for scheduled bot joins (calendar events) so retries and timing are managed.
- Idempotency keys on every job; a job table row is the source of truth for status shown in the UI.

### 3.5 Credits and billing

- One minute of bot recording = 1 credit. One minute of cloud transcription of uploaded audio = 1 credit. Desktop on-device transcription = 0 credits. Summaries with the hosted model = 0.1 credit per minute of transcript (rounded up per meeting); bring-your-own-key summaries = 0.
- Free grant: N credits per workspace per month (proposal: 300; founder decision). Grants expire monthly; purchased credits do not expire.
- Purchase flow: web or desktop opens `POST /v1/billing/checkout {pack}` which creates a `payments` row and calls the Nylon Pay adapter to start a mobile-money or card payment; Nylon Pay's webhook hits `POST /v1/billing/webhooks/nylonpay`, verified by signature, which marks the payment paid and inserts a `credit_ledger` purchase row. Idempotent on `provider_ref`.
- Packs (proposal, priced in UGX with USD equivalents shown): 100, 500, 2000 minutes. Founder sets prices; the code stores packs in a table so prices change without a deploy.
- Entitlement checks: the bot refuses to join when the workspace balance is below the meeting's estimated minutes (default 60) unless "allow overdraft to zero" is on; cloud transcription checks before enqueue. The desktop app reads a signed balance snapshot and works offline for local features regardless.
- Adapter interface (Go):
  ```go
  type PaymentProvider interface {
      StartPayment(ctx, req StartPaymentRequest) (StartPaymentResponse, error) // returns redirect URL or USSD push handle
      VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error)
      Refund(ctx, providerRef string, amount Money) error
  }
  ```
  `nylonpay` implements it. Until the real endpoints are known, a `fake` provider drives tests and the staging environment.

### 3.6 Search and Ask

- Keyword: Postgres full-text on `transcript_segments.tsv` with `websearch_to_tsquery`, ranked, filtered by workspace, folder, date, participant.
- Semantic: embeddings per segment chunk (30 to 60 s of speech) stored in `pgvector`; hybrid ranking (reciprocal rank fusion of keyword and vector). Embedding model: a hosted embedding API for the managed service, with a local option for the desktop app's own index later.
- "Ask Afterword": retrieval over the workspace's segments plus the chosen LLM (hosted default, or the user's own key). Answers cite meeting and timestamp and deep-link into the player. Metered like summaries.

### 3.7 Desktop app changes (keep the UI, add surfaces)

- `AccountProvider`: sign in, workspace switcher, balance display in the sidebar footer, "Top up" button that opens the web checkout.
- Sync queue (Rust): after a meeting is saved locally, enqueue upload of metadata and transcript segments; audio upload is a separate, cancellable, resumable step (GCS resumable upload). Status shown per meeting. Conflict rule: desktop is source of truth for transcript text; server for titles edited on the web, sharing, and comments.
- Teams: choose which workspace a meeting syncs to; workspace members see it in the web library per its visibility.
- Sharing: "Share" on a meeting creates a `share_links` row and copies the URL; WhatsApp and email share targets.
- Copy changes: About screen loses the "Open source" block and GitHub button; gains account, plan, and credits. Onboarding gains an optional sign-in step (skip keeps local-only mode).
- Bot from the desktop: "Send the notetaker" button that takes a meeting link or picks from the connected calendar; shows job status and credits estimate.

### 3.8 Bot completion

- Persist jobs through the API instead of the in-memory map; workers claim jobs from Pub/Sub, report status via `PATCH /v1/bot-jobs/:id`.
- Upload WAV to GCS on leave; enqueue `transcribe`; delete local files.
- Calendar auto-join: Google Calendar and Microsoft 365 OAuth, polling every 5 minutes for events with conferencing links, per-calendar and per-event opt-in, join 1 minute before start.
- Zoom and Teams adapters behind the existing `MeetingPlatform` interface.
- Consent: the announcement is mandatory and logged (`consent_announced_at`); participants get a link to the privacy policy; hosts can remove the bot and the job records that.
- Custom bot name and avatar per workspace (Fathom Premium parity).

### 3.9 Web app

- Library: list, filters, folders, workspace and personal views.
- Meeting page: audio player synced to transcript, summary (BlockNote editor reused), action items, comments anchored at timestamps, clips, share.
- Search page and Ask page.
- Workspace settings: members and roles, bot name, retention, integrations, credits and payments history, API tokens.
- Public share page: read-only meeting view by token, optional comment.

### 3.10 Integrations

- Slack: post summary and action items to a channel; keyword alerts to DM.
- Notion: page per meeting into a chosen database.
- HubSpot first CRM: log meeting and summary on contacts and deals matched by attendee email.
- Outbound webhooks (`meeting.summarised`, `bot.job.completed`) with signed payloads; public API with tokens; MCP server later (Fathom has one).

### 3.11 GCP deployment

- Terraform in `infra/`: project, VPC connector, Cloud SQL (Postgres 16, private IP, pgvector), three buckets, Pub/Sub topics, Cloud Tasks queue, Secret Manager secrets, Cloud Run services (`api`, `web`), Cloud Run jobs (`transcribe-worker`, `bot-worker`), Artifact Registry, Cloud Scheduler for the calendar poller and monthly credit grants, IAM service accounts with least privilege.
- Environments: `staging` and `prod`, separate projects.
- CI/CD: GitHub Actions `deploy.yml` builds images to Artifact Registry with Workload Identity Federation (no long-lived keys), runs migrations as a Cloud Run job, then deploys. The existing `bot.yml` image build moves under this.
- Observability: Cloud Logging with structured JSON, Cloud Monitoring alerts on 5xx rate, job failure rate, credit webhook failures; Sentry for the web and desktop apps (there is a Sentry plugin available to Claude Code).
- Security baseline: TLS everywhere, secrets only in Secret Manager, signed URLs, rate limits on auth and checkout, audit log table for admin actions, data deletion endpoint (`DELETE /v1/meetings/:id` purges objects), documented retention. SOC 2 is a later, paid effort; design so nothing blocks it.

---

## 4. Phased task list

Each phase is sized for one or two Claude Code sessions. Each task names its files and its acceptance criteria. Use the existing conventions: one branch per phase, tests before merge, CI must be green, plan mode before large changes.

### Phase A: Backend foundation (`services/api`, `infra/`)

A1. Scaffold Go module with chi, sqlc, golang-migrate, zerolog, OpenAPI spec at `services/api/openapi.yaml` generated from handlers. Acceptance: `go test ./...` green; `GET /healthz`; Dockerfile builds; `docker compose` with Postgres for local dev.
A2. Migrations for the data model in section 3.2 (pgvector enabled). Acceptance: migrate up and down cleanly; sqlc queries for every table.
A3. Auth: email OTP, phone OTP (provider interface with a fake), Google OAuth, JWT plus refresh, workspace creation on first sign-in. Acceptance: integration tests for sign-up, refresh, revoke; every other endpoint returns 401 without a token.
A4. Meetings and transcripts API: create meeting, request upload URLs, finalise upload, list, get, update title and visibility, delete with object purge. Acceptance: contract tests; objects removed from a fake GCS in tests.
A5. Terraform for staging (section 3.11) plus `deploy.yml`. Acceptance: `terraform plan` clean; API reachable on a Cloud Run URL; migrations job runs.

### Phase B: Credits and Nylon Pay

B1. Ledger and balances: tables, `GET /v1/billing/balance`, monthly grant job, admin adjust endpoint. Acceptance: property tests that balance always equals sum of ledger; concurrent debits cannot overdraw.
B2. `PaymentProvider` interface, `fake` provider, `nylonpay` provider skeleton with config for base URL, keys, webhook secret. Acceptance: checkout creates a pending payment and returns the provider's redirect or push handle; webhook marks paid exactly once.
B3. Entitlement middleware for bot and transcription endpoints. Acceptance: requests below balance are rejected with a clear error that includes the top-up URL.
B4. Web checkout and payment history pages; desktop "Top up" opens the web page in the system browser.
Input needed from the founder before B2 can be finished: Nylon Pay API docs or repo path, webhook signature scheme, supported currencies, refund API.

### Phase C: Transcription and summary workers

C1. `services/transcribe-worker`: container with `afterword-transcribe` (already built by `bot/Dockerfile` stage 1), a small runner that pulls `transcribe` jobs, downloads audio from GCS, runs the CLI, uploads `transcripts.json`, inserts segments, debits credits. Acceptance: end-to-end test with a WAV fixture through a local Pub/Sub emulator.
C2. Summariser job reusing the prompt templates in `frontend/src-tauri/templates/*.json` (port `summary/processor.rs` logic to the worker or call the Rust code through a second CLI `afterword-summarise` in `crates/afterword-core`; prefer the CLI so desktop and cloud stay identical). Acceptance: same markdown for the same transcript as the desktop app.
C3. Embeddings job and hybrid search endpoint `GET /v1/search?q=`. Acceptance: relevance tests on a fixture library.
C4. Ask endpoint `POST /v1/ask` with citations. Acceptance: answers include meeting id and timestamp for each claim; metering recorded.

### Phase D: Desktop app account, sync, credits, teams, sharing

D1. Remove open-source copy: `frontend/src/components/About.tsx` (open-source block, GitHub button), README and onboarding mentions; replace with account and credits blocks. Keep `NOTICE.md` and `LICENSE.md`.
D2. `AccountProvider` and sign-in dialog; keychain-stored refresh token via the existing `SecretStore`; workspace switcher.
D3. Rust sync queue in `frontend/src-tauri/src/sync/`: table `sync_queue` in SQLite, uploader with backoff, resumable audio upload, status events to the UI. Acceptance: kill the app mid-upload, restart, upload completes; airplane mode leaves local features untouched.
D4. Sharing and visibility controls on the meeting page; WhatsApp and email targets.
D5. "Send the notetaker" flow that creates a bot job and shows the credit estimate.
D6. Onboarding sign-in step (skippable), balance in the sidebar.

### Phase E: Bot completion (`bot/`)

E1. Replace the in-memory job store with API-backed jobs; worker claims from Pub/Sub; status via API. Acceptance: existing 62 tests still pass; new tests use a fake API.
E2. GCS upload on leave, `transcribe` job enqueue, local cleanup.
E3. Calendar connections and auto-join (Google first). Acceptance: fake calendar in tests schedules a join one minute before start.
E4. Zoom and Teams adapters with fixture pages like the Meet one.
E5. Custom bot name and avatar; consent logging; host removal handling.
E6. Cloud Run job or GKE Autopilot deployment with the Chromium and PulseAudio image already built by CI.

### Phase F: Web app (`services/web`)

F1. Scaffold Next.js app with auth, workspace context, and the design tokens from the desktop app so it feels like one product.
F2. Library, folders, meeting page with player and transcript, summary editor (BlockNote, reused), action items.
F3. Comments, clips (server-side FFmpeg cut), share links and public share page.
F4. Search and Ask pages.
F5. Workspace settings: members, roles, bot settings, retention, integrations, credits and payments, API tokens.

### Phase G: Integrations and launch

G1. Slack, Notion, HubSpot, outbound webhooks, public API docs.
G2. Keyword alerts.
G3. Privacy policy and terms pages served by the web app; the bot's `PRIVACY_URL` points at them.
G4. Launch checklist: staging soak with real Meet calls, load test of the API, backup and restore drill for Cloud SQL, incident runbook, status page.

---

## 5. Pricing proposal (for the founder to confirm)

| Item | Proposal |
|---|---|
| Free grant | 300 credits per workspace per month, expiring |
| Packs | 100 credits UGX 15,000; 500 credits UGX 60,000; 2,000 credits UGX 200,000 (roughly USD 4, 16, 54) |
| Metering | Bot minute 1 credit; cloud transcription minute 1 credit; hosted summary 0.1 credit per transcript minute; Ask query 1 credit; desktop on-device features free; bring-your-own-key summaries free |
| Team | Pooled balance, unlimited members, roles free |

Worked comparison: a five-person team running 40 hours of bot-recorded meetings a month uses 2,400 credits, about UGX 240,000 (USD 65) total. Fathom Team for five is USD 95 per month billed annually, USD 1,140 up front.

---

## 6. Open decisions for the founder

1. GCP region: `europe-west1` (Belgium) has the best latency to East Africa among full-featured regions today; `africa-south1` (Johannesburg) offers data residency in Africa but fewer services (check Cloud Run GPU and pgvector-capable Cloud SQL availability at build time). Pick one.
2. Nylon Pay: docs, sandbox credentials, webhook format, refund support, SMS capability.
3. Free grant size and pack prices (section 5).
4. Retention default and whether audio is stored by default or transcript-only.
5. Which calendar first (Google assumed), which CRM first (HubSpot assumed).
6. Bot infrastructure: Cloud Run jobs (simplest) versus a GKE Autopilot pool (cheaper at volume). Start with Cloud Run jobs.
7. Legal review of recording-consent rules for launch markets before the bot is offered outside internal testing.

---

## 7. How to run this with Claude Code

Start a session per phase. Suggested opening prompt:

> Read `docs/AFTERWORD_CLOUD_IMPLEMENTATION.md`. We are doing Phase A. Enter plan mode, list the files you will create, then implement A1 through A5 with tests, keeping CI green. Do not start other phases.

Guardrails to repeat each session: no secrets in the repo; every endpoint scoped by workspace; consent before recording stays mandatory; keep the MIT notice; ask before anything that changes billing amounts or deletes customer data.
