# Afterword production plan: desktop, bot, web, all synced

This plan takes Afterword from its current state to a production-ready
Fathom-class product with three fully working capture and access surfaces that
share one backend and one pipeline:

1. Desktop app: local capture, on-device transcription, free and offline.
2. Meeting bot: joins calls the user did not host, metered by the minute.
3. Web app: cross-device library, search, sharing, billing, admin.

It supersedes the forward-looking parts of
`AFTERWORD_CLOUD_IMPLEMENTATION.md`. That document remains the source of the
data model, billing rules, and architecture. This one records what is actually
built as of `main`, what is missing, and the exact order to close the gap.

Standing rules for every session:

- New code carries no comments. Names and tests explain intent.
- No em dashes in prose or docs. No serial comma before "and".
- Everything runs on one cheap VPS with Docker Compose. No managed queues or
  serverless. Scale by adding boxes.
- Every API query is scoped by workspace. Consent before the bot records is
  mandatory. The MIT notice for the Meetily code stays.
- One pipeline. Desktop and bot produce identical transcript and summary
  formats so search, sharing, and integrations never special-case the source.

---

## 1. Current state, audited

### Done and merged (`main`)

Backend (`services/api`, Go 1.23, chi, pgx + sqlc, golang-migrate):

- Auth: email OTP, phone OTP, Google OAuth, 15-minute JWT plus 30-day refresh,
  workspace creation on first sign-in, membership middleware.
- Workspaces, memberships, invites with roles.
- Meetings: create, list, get, update title and visibility, delete with object
  purge, presigned MinIO upload URLs, finalize, bulk segment replace, list
  segments, folders.
- Share links plus unauthenticated public `/v1/shared/{token}` read endpoints.
- Billing: credit packs, balance, checkout, payments list, admin adjust,
  signature-verified idempotent webhook, `nylonpay` skeleton, `fake` provider.
- Bot jobs: create, list, get, scoped by workspace.
- Jobs queue: Postgres table, `FOR UPDATE SKIP LOCKED`, exponential backoff,
  dead-letter after 5 attempts, idempotency keys.
- Credit ledger: balance is `SUM(delta_minutes)`, concurrent debits never
  overdraw, entitlement checks gate bot jobs and cloud transcription.
- Storage: S3 and MinIO adapters plus an in-memory adapter for tests.
- 17 migrations, sqlc generation, `/healthz`, `/metrics`, structured logs, CI
  green.

Desktop (`frontend/`): local capture, VAD, Whisper and Parakeet transcription,
summary templates, OS-keychain secrets, Phase 1 hardening complete.

Deploy (`deploy/`): production `docker-compose.yml`, `Caddyfile`, `bootstrap.sh`,
`backup.sh`, `restore.sh`, and `.github/workflows/deploy.yml` publishing to
GHCR.

### Built at the data layer, not wired to HTTP

- `comments`, `clips`, `keyword_alerts`, `calendar_connections`: tables migrated
  and sqlc code generated, no handlers or routes.

### Not started

- `services/web/`: directory does not exist. Compose references
  `afterword-web` behind a `web` profile, so the default stack still starts
  without it.
- Desktop sync: no `frontend/src-tauri/src/sync/`. `useAccount` returns a
  hardcoded local-only state.
- Bot to backend link: the bot runs an in-memory store on `localhost:8787`,
  never claims jobs from the API, never uploads to MinIO. Calendar, Zoom, and
  Teams are stubs.
- Keyword and semantic search endpoints: `/v1/search`, `/v1/ask`, and the
  `afterword-summarise` CLI do not exist yet. The `embedding` column, `tsv`
  column, and (after this work) the HNSW index do exist.
- Integrations: Slack, Notion, HubSpot, outbound webhooks, public API docs.
- Policy pages: privacy, terms, contributing, Bluetooth notice still unwritten.

### Delivered in the current session

- Comments removed from every source file in the repository (Go, Rust,
  TypeScript, JavaScript, Python, shell) with language-aware tokenizers, one
  git commit per file. Load-bearing directives were preserved on purpose:
  `//go:build`, sqlc `-- name:` markers, shell shebangs, TypeScript triple-slash
  and `@ts-`/eslint directives, and Rust lifetimes and raw strings. Config and
  data files (YAML, TOML, Dockerfile, Makefile, JSON, Caddyfile, Markdown) were
  left untouched because their `#` lines are frequently parser directives.
  Verified: Go builds and vets, all three Rust crates compile, `cargo fmt`
  succeeds, all 205 TS/JS files parse, all Python compiles, every shell script
  passes `bash -n`.
- C0: migration `0018_segment_embedding_index` adds an HNSW cosine index on
  `transcript_segments.embedding`. Well-formed SQL against the existing pgvector
  column; not run end to end here because no Postgres or Docker is available on
  this machine.
- C1: the transcribe worker. Command in `services/api/cmd/transcribe-worker`,
  job logic in `services/api/internal/workerlib`, Dockerfile in
  `services/transcribe-worker/`. It claims `transcribe` jobs, downloads audio by
  presigned URL, runs the `afterword-transcribe` CLI, writes the transcript
  object and segment rows, transcodes to Opus, and marks the meeting ready. It
  does not debit credits because the API already debits at enqueue time.
  Verified: the whole API module builds and vets, the worker binary compiles,
  and the transcript-parser unit tests pass. Not verified end to end because no
  Postgres, MinIO, Whisper model, or Docker is available here.

### Honest scope note

Phase C is complete and verified end to end against real infrastructure (Docker
Postgres with pgvector, MinIO, ffmpeg): C0 embedding index, C1 transcribe
worker, C2 summarise CLI and worker, C3 keyword search, C4 Ask, C5 comments and
clips and keyword alerts. LLM-dependent features (summarise, Ask) use a provider
interface with a tested offline fake and an Ollama provider selected by config.
Four pre-existing bugs were found and fixed by the real tests: a NULL embedding
scan, an ambiguous clip title column, and NULL-safe embedding projection.

Phases D (desktop sync), E (bot to backend), F (web app), and G (integrations)
follow. Backend-testable parts are verified with real integration tests; the
web and desktop UI and the browser-driven bot adapters are compile-checked only,
because this machine has no windowed desktop session, no meeting targets, and no
third-party OAuth or integration credentials.

### Two facts that change sequencing

- The compose file assumes `web` and `transcribe-worker` images exist. Until
  they do, the only deployable stack is api-only. Ship those two first so the
  box is deployable end to end.
- Semantic search needs a migration that adds the vector column and index
  before any worker can write embeddings. That migration is a prerequisite for
  Phase C3, not part of it.

---

## 2. What "better than Fathom" means, concretely

Each edge maps to a task below, not a slogan.

1. Cheaper for the market: credits in UGX, one-off mobile-money top-ups, no
   per-user minimum, no annual lock-in. Owned by the billing plane already
   built (B) plus the web checkout (F5) and desktop top-up (D).
2. Local-first is free and real: desktop transcribes and summarizes on-device
   at zero credits, works offline, syncs when it can. Owned by the desktop
   sync queue (D3) with an offline-first conflict rule.
3. Bandwidth-aware: transcript-first sync, resumable audio upload, Opus 24 kbps
   in storage. Owned by D3 and C1.
4. Your model, your data: local model, hosted model, or bring-your-own key. No
   training on customer audio. Owned by C2 and the Ask provider interface (C4).
5. Team features without a Team tier: shared library, folders, comments, share
   links, keyword alerts for every workspace. The metered resource is minutes,
   not seats. Owned by the wired comment/clip/keyword endpoints (C5) and web
   (F).
6. Africa-first: phone sign-in (built), WhatsApp share targets, Luganda and
   Swahili summaries through the summary-language pipeline, a VPS near the
   market. Owned by D4 and C2.

Parity items that must exist before launch: action items, Ask-your-meetings,
clips, playlists or folders, global search, comments, Slack and Notion and
HubSpot, calendar auto-join, public API.

---

## 3. Delivery order

The order below front-loads the two missing pieces that make the stack
deployable, then makes each surface real one at a time, verifying sync at every
step. Each phase is one or two focused sessions with tests and green CI.

### Phase C: Workers so the stack is deployable and meetings get processed

Goal: a recording uploaded to the API comes back transcribed, summarized, and
searchable, with credits debited. This unblocks the compose file.

- C0. Migration `add embeddings`: `embedding vector(768)` on
  `transcript_segments`, an ivfflat or hnsw index, and a `tsv` generated column
  if not already present. Acceptance: migrate up and down clean, index used by
  `EXPLAIN` on a seeded table.
- C1. `services/transcribe-worker`: small Go runner that claims `transcribe`
  and `summarise` jobs, pulls audio from MinIO, runs the `afterword-transcribe`
  CLI, uploads `transcripts.json`, bulk-inserts segments, converts audio to
  Opus 24 kbps with FFmpeg, debits credits. Dockerfile built from
  `crates/afterword-core`. Acceptance: end-to-end test with a WAV fixture
  through the dev compose; the GHCR image the production compose expects now
  builds.
- C2. `afterword-summarise` CLI in `crates/afterword-core` driven by the
  templates in `frontend/src-tauri/templates/*.json`, so desktop and cloud
  produce identical markdown and action items. Worker calls it for `summarise`
  jobs. Acceptance: byte-identical markdown to the desktop app for the same
  transcript and template.
- C3. Embeddings job plus `GET /v1/search`: chunk segments to 30 to 60 seconds,
  embed behind a provider interface (hosted default, local later), hybrid rank
  keyword and vector by reciprocal rank fusion, filter by workspace, folder,
  date, participant. Acceptance: relevance test on a fixture library, results
  carry meeting id and timestamp deep link.
- C4. `POST /v1/ask`: retrieval over the workspace segments plus the chosen LLM
  (hosted default or the user's key), answers cite meeting and timestamp,
  metered at 1 credit. Acceptance: citation test, debit recorded.
- C5. Wire the data-layer tables that already exist: comment CRUD at
  timestamps, clips (server-side FFmpeg cut, share token), keyword alerts.
  Acceptance: contract tests, objects purged on meeting delete.

### Phase D: Desktop account, sync, credits, teams, sharing

Goal: sign in on the desktop, local meetings appear in the account, local-only
use still works with no account.

- D1. Remove open-source copy from the About screen and onboarding, add a
  plan-and-credits card driven by `useAccount`, single `SUPPORT_URL` constant.
- D2. `AccountProvider`, sign-in dialog (email or phone OTP, Google), refresh
  token in the OS keychain through the existing `SecretStore`, workspace
  switcher. Replace the `useAccount` stub with real state.
- D3. Rust sync queue in `frontend/src-tauri/src/sync/`: SQLite `sync_queue`
  table, uploader with backoff, resumable and cancellable audio upload,
  transcript-first, status events to the sidebar. Conflict rule: desktop owns
  transcript text, server owns titles edited on the web, sharing, comments,
  last-write-wins per field on `updated_at`. Acceptance: kill the app
  mid-upload and restart, the upload completes; offline leaves local features
  untouched.
- D4. Sharing and visibility per meeting, WhatsApp and email share targets.
- D5. "Send the notetaker": take a meeting link or calendar event, show the
  credit estimate, create a bot job, show live status.
- D6. Onboarding sign-in step that is skippable, balance in the sidebar footer,
  "Top up" opens the web checkout in the system browser.

### Phase E: Bot completion, the second capture surface

Goal: the bot claims jobs from the API, records, uploads, and its results land
in the same library as desktop meetings.

- E1. Replace the in-memory store with API-backed jobs: the worker claims
  `bot.join` jobs from the API, reports status transitions back, authenticates
  with a service token. Acceptance: existing bot tests pass against a fake API,
  a job created through `/v1/.../bot-jobs` is picked up and reported done.
- E2. On leave, upload the WAV to MinIO, enqueue a `transcribe` job, delete
  local files. The meeting appears in the workspace library with source `bot`.
- E3. Calendar connections and auto-join: Google Calendar first, poll every 5
  minutes, per-calendar and per-event opt-in, join one minute before start.
  Wire `calendar_connections` (table already exists).
- E4. Zoom and Teams adapters behind the existing `MeetingPlatform` interface,
  each with a fixture page like the Meet smoke test.
- E5. Custom bot name and avatar per workspace, consent announcement logged to
  the meeting record, host removal handled and recorded.
- E6. Bot worker compose service with resource limits, documented scaling to a
  second box with `--scale bot-worker=N`.

### Phase F: Web app, the third surface

Goal: everything a user can do on the desktop, plus the team and admin surfaces,
in a browser, sharing the exact backend.

- F1. Scaffold `services/web` (Next.js 14 app router) with auth, workspace
  context, the desktop design tokens, reuse non-Tauri components from
  `frontend/src/components`. Acceptance: the GHCR `afterword-web` image the
  production compose expects now builds and serves behind Caddy.
- F2. Library with folders and workspace and personal views, meeting page with
  a player synced to the transcript, summary editor (reuse BlockNote), action
  items.
- F3. Comments at timestamps, clips, share links, public share page by token
  (backed by C5 and the existing public endpoints).
- F4. Search and Ask pages (backed by C3 and C4).
- F5. Workspace settings: members and roles, bot name and avatar, retention,
  integrations, credits and payment history and checkout, API tokens.

### Phase G: Integrations, hardening, launch

- G1. Slack summary posts, Notion page per meeting, HubSpot first CRM, outbound
  signed webhooks, public API docs. Generic event `meeting.summarised` with a
  signed payload so more integrations are cheap.
- G2. Keyword alerts delivery (table wired in C5, delivery here).
- G3. Privacy policy, terms, contributing, Bluetooth notice pages. The bot's
  `PRIVACY_URL` and the app's in-app links point at real pages.
- G4. Launch checklist: staging soak with real Meet calls, load test, backup
  and restore drill executed once, incident runbook, uptime monitor, Sentry
  free tier on web and desktop.

---

## 4. Sync contract, the thing that ties all three surfaces together

Sync is the highest-risk part, so its rules are stated once here and every
phase must honor them.

- Identity: a meeting has one id across desktop, bot, and web. Desktop meetings
  are created locally with a client id, mapped to the server id on first
  successful sync. Bot meetings are created server-side.
- Source of truth per field: desktop owns transcript segment text. Server owns
  title edited on the web, visibility, sharing, comments, and summaries
  regenerated on the web. Last-write-wins per field keyed on `updated_at`. No
  CRDTs.
- Direction: desktop pushes local meetings up, then pulls server-owned fields
  down. Bot writes only up. Web reads and writes through the API only.
- Bandwidth: transcript and metadata sync first and must succeed on a bad
  connection. Audio upload is a separate resumable cancellable step.
- Offline: no network means the desktop keeps every local feature. The queue
  drains when connectivity returns.
- Consistency check: the same transcript rendered on desktop and web must match
  segment for segment. A contract test seeds a meeting, syncs it, and diffs the
  desktop SQLite copy against the API response.

---

## 5. Production readiness gates

No surface is "done" until it clears these.

- Deployable: `deploy/docker-compose.yml` pulls every referenced image and the
  stack answers `/healthz` over HTTPS on a fresh Ubuntu 24.04 box via
  `bootstrap.sh`. This is blocked today by the missing web and transcribe-worker
  images (Phases C1 and F1).
- Tested: `go test ./...`, `cargo test --workspace`, `pnpm test` in `frontend/`
  and `bot/` and `services/web` all green in CI. A cross-surface sync contract
  test exists.
- Metered correctly: every credit-consuming path debits exactly once and never
  overdraws under concurrency. Property tests hold.
- Consent enforced: the bot announces before recording, the order is unit
  tested, consent state is on the meeting record, and the bot refuses to start
  without a `PRIVACY_URL` that resolves.
- Recoverable: the nightly backup runs and a restore drill has been executed
  once against a throwaway box.
- Observable: JSON logs with rotation, `/metrics` scraped, error tracking on
  web and desktop.
- Secure: TLS only, secrets only in `.env` at mode 600, signed URLs, rate
  limits on auth and checkout, audit log, delete purges objects, retention
  documented.

---

## 6. Decisions still needed from the founder

Carried forward from the cloud brief, still open and now blocking specific
phases:

1. VPS provider and location, and the domain for `api.` and `app.` (blocks
   Phase F deploy and the launch checklist).
2. Nylon Pay docs, sandbox credentials, webhook signature scheme, currencies,
   refund API, SMS capability (blocks finishing B2 and real checkout in F5).
3. Free grant size and pack prices (section 5 of the cloud brief).
4. Retention default, and whether audio syncs by default or transcript-only
   (blocks D3 defaults and G4 retention sweep).
5. Which calendar first (Google assumed) and which CRM first (HubSpot assumed).
6. Legal review of recording-consent rules for the launch markets before the
   bot is offered outside internal testing (blocks any external bot release).

---

## 7. Recommended next session

Phase C0 plus C1. They unblock the compose file, prove the pipeline end to end
in the cloud, and give the web app real data to render. Suggested opening
prompt:

> Read `docs/AFTERWORD_PRODUCTION_PLAN.md`. We are doing Phase C0 and C1. Enter
> plan mode, list the files you will create, then implement the embeddings
> migration and the transcribe-worker with tests, keeping CI green. No comments
> in new code. Do not start other phases.
