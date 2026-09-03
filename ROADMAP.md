# Afterword Roadmap

Afterword is an AI meeting assistant with two ways to capture a meeting that
feed one shared pipeline:

1. **Desktop app** (this repository). Local microphone and system-audio
   capture, on-device transcription, no bot, no cloud dependency required.
   macOS, Windows, and Linux, with Linux packaged as a first-class release.
2. **Meeting bot** (separate service, not yet started). Joins Zoom, Google
   Meet, and Microsoft Teams calls the user did not host on their own machine,
   triggered by calendar integration rather than pasted links.

Both write into a shared backend: a Go API, Postgres for metadata,
S3-compatible object storage for recordings and transcripts, semantic and
keyword search over transcripts, and a billing plane that reuses mobile-money
billing patterns already built for a related project. A web dashboard sits on
top for cross-device access, search, sharing, and integrations.

Primary market first: developers and teams in Uganda and the wider African
region. The product must still work globally.

This document deliberately has no dates. Scope and ordering are stated;
estimates are not, because there is not enough information yet to make them
honest. Each phase lists the decisions that need founder input rather than an
engineer's assumption.

---

## Phase 0 (done): Rebrand

Fork of Meetily rebranded to Afterword on the `rebrand/afterword` branch:
manifests, bundle identifier, UI copy, docs, updater endpoint and signing key,
storage paths, and attribution. See [NOTICE.md](NOTICE.md).

Still open from Phase 0, for the founder:

- New icon and logo artwork. The app still ships Meetily's icons and the docs
  still show Meetily screenshots.
- Author `PRIVACY_POLICY.md`, `CONTRIBUTING.md`, and
  `BLUETOOTH_PLAYBACK_NOTICE.md`. In-app links already point at those paths in
  this repository; the pages are 404 until written.
- Back up the updater private key at `~/.tauri/afterword.key` and set a
  password on it. Losing it means shipped builds can never be updated.
- Pick a tagline.

---

## Phase 1: Stabilize the desktop app

**Depends on:** Phase 0.

**Goal:** A release a stranger can install on macOS, Windows, or Linux from a
GitHub Release page and use for a real meeting without reading build docs.

### Scope

**Linux as a packaged release, not build-from-source**

- Produce and test AppImage and `.deb` from the existing Tauri bundle targets.
  Add Flatpak (Flathub manifest) so users get updates through their store.
- Linux system-audio capture does not exist today. The generic path in
  `frontend/src-tauri/src/audio/capture/system.rs` returns "not yet implemented"
  on Linux and Windows. Implement PipeWire/PulseAudio monitor-source capture on
  Linux. Without this, Linux records the microphone only.
- Verify Whisper and Parakeet run on CPU-only Linux, and document the CUDA and
  Vulkan feature builds that already exist in `Cargo.toml`.
- The build fetches FFmpeg binaries from Zackriya's GitHub and the default
  Parakeet v3 model from a Meetily-hosted mirror. Re-host both under an
  Afterword-controlled location, or fetch upstream (FFmpeg static builds,
  Hugging Face) directly. Add checksum verification, which is missing today.

**Windows and macOS installers**

- Windows: confirm the WASAPI loopback path actually works end to end; the
  explicit fallback code errors on Windows, so system audio may be relying on
  cpal's host enumeration. Test on a machine with no NVIDIA GPU.
- Windows code signing: the existing hook expects DigiCert `smctl`. Decide on
  a signing provider before the first public release (see decisions).
- macOS: notarization is not configured (`signingIdentity: "-"` is ad-hoc).
  Set up Developer ID signing and notarization in CI.
- Set up GitHub Actions to build all three platforms, sign, generate
  `latest.json` with the existing `scripts/generate-update-manifest-github.js`,
  and publish a release. Test the in-app updater against a real release.

**Close obvious gaps and dead weight inherited from upstream**

- Dead code that confuses contributors: `audio/audio_v2/` (never compiled),
  `audio/core-old.rs`, `audio/recording_saver_old.rs`, `audio/stt.rs` (vendored
  from another project), `lib_old_complex.rs`, the parallel-processor command
  set nothing in the frontend calls, and the sample `app/notes/[id]` route.
  Remove them.
- `tauri-plugin-fs` and `tauri-plugin-log` are declared but never registered,
  while the capability file grants broad `fs:*` permissions. Either register or
  drop both and tighten the capabilities.
- Three `api_*_profile` commands and two "backend connection" commands still
  call the archived FastAPI on `localhost:5167`. Remove them and the stale
  ports in the CSP `connect-src`.
- Enable `PRAGMA foreign_keys` on the SQLite connection so `ON DELETE CASCADE`
  works (the `meeting_notes` table can currently orphan rows).
- The Gemini API key column exists in the schema but no repository or UI reads
  it. Finish or remove.
- Do-not-disturb detection is stubbed to always return false while the
  settings UI offers a "respect DND" toggle. Implement per platform or remove
  the toggle.
- API keys are stored in plaintext SQLite. Move them to the OS keychain
  (Keychain, Credential Manager, Secret Service) via a Tauri plugin. This
  matters more once accounts and sync exist.
- Parakeet, the default engine, ignores the language setting silently. Surface
  that in the UI, and default non-English users to Whisper.
- Notification consent is auto-granted at startup while analytics is opt-in.
  Make both opt-in during onboarding.

**Testing baseline**

- There is one frontend test file (Bun) and scattered Rust unit tests. Add a
  CI job that runs `cargo test`, `pnpm build`, and a headless smoke test of the
  packaged app on each OS.

### Founder decisions

- Which Linux distributions and formats are "supported" for the first
  release: AppImage + `.deb` only, or Flatpak from day one?
- Windows signing: pay for an EV/OV certificate, use Azure Trusted Signing, or
  ship unsigned with a SmartScreen warning for the first releases?
- Keep the "Import Audio & Retranscribe" beta flag, or graduate it to stable?
- Whether to keep the OpenRouter/Groq/Anthropic/OpenAI cloud providers in a
  product positioned as local-first, or hide them behind an "advanced" setting.

---

## Phase 2: Backend

**Depends on:** Phase 1 releases so there is a stable client to sync from.
Can be designed in parallel with Phase 1.

**Goal:** Users can sign in from the desktop app, and meetings recorded
locally appear in their account. Local-only use keeps working with no account.

### Scope

**Go API service**

- Single Go module, HTTP/JSON API, versioned under `/v1`. OpenAPI spec kept in
  the repo and used to generate the desktop client.
- Auth: email + magic link or passwordless OTP first (phone-number OTP is
  worth considering for the primary market), OAuth (Google, GitHub) second.
  Sessions as short-lived JWT plus refresh token stored in the desktop app's
  OS keychain.
- Organisations and members from the start, even if the first UI is
  single-user. Retrofitting multi-tenancy later is far more expensive than
  adding an `org_id` column now.

**Postgres schema**

- `users`, `orgs`, `org_members`, `meetings` (owner, org, source =
  `desktop|bot`, started_at, duration, consent state), `transcript_segments`
  (meeting, speaker, start/end seconds, text), `summaries` (meeting, template,
  language, markdown, model used), `recordings` (object-storage key, codec,
  size, checksum), `api_tokens`, `devices`.
- Row-level access by `org_id`. Migrations via `golang-migrate` or `goose`,
  checked into the repo.

**Object storage**

- S3-compatible API only, so it runs on AWS S3, Cloudflare R2, MinIO, or a
  local-region provider. Pre-signed upload and download URLs; the API never
  proxies media bytes.
- Recordings uploaded as the existing `audio.mp4` (AAC). Transcripts uploaded
  as JSON alongside the segment rows in Postgres.

**Sync from the desktop app**

- Opt-in per meeting and per account. Default remains local-only.
- Desktop app gets a sync queue: after a recording finishes and the SQLite
  save completes, enqueue an upload job (metadata, transcript segments,
  optional audio). Retry with backoff; survive app restarts; visible status in
  the sidebar.
- Conflict rule: the desktop app is the source of truth for transcript text;
  the server is the source of truth for sharing, titles edited on the web, and
  summaries regenerated on the web. Last-write-wins per field with
  `updated_at`, no CRDTs.
- Bandwidth matters in the primary market. Transcript-only sync must work on
  a bad connection; audio upload is a separate, cancellable, resumable step.

**Operations**

- Containerised, single `docker compose` for self-hosting (API + Postgres +
  MinIO). Managed deployment target chosen by the founder.
- Structured logs, request metrics, and error tracking from the first
  deploy.

### Founder decisions

- Hosting region and provider. Latency and data-residency expectations for
  Ugandan and East African users argue for a provider with an African region
  or edge presence.
- Whether audio recordings sync by default or transcript-only by default.
  This is a privacy positioning decision as much as a bandwidth one.
- Self-hostable from day one (adds packaging and docs work) or managed-only
  first?
- Data retention defaults and whether deletion is hard delete or soft delete
  with a grace period.

---

## Phase 3: Search and dashboard

**Depends on:** Phase 2 (accounts, meetings and transcripts in Postgres and
object storage).

**Goal:** Find any moment from any meeting by meaning or by keyword, from a
browser, and share it.

### Scope

**Search**

- Keyword search: Postgres full-text search (`tsvector` on transcript
  segments) replaces the current `LIKE '%q%'` scan in the desktop app for
  synced meetings. The desktop app should also get SQLite FTS5 for local-only
  users.
- Semantic search: embed transcript segments (chunked to roughly 30 to 60
  seconds of speech) and store vectors in `pgvector` in the same Postgres.
  Hybrid ranking: keyword and vector results fused, then filtered by org,
  date, participant. Avoid a separate vector database until Postgres is the
  bottleneck.
- Embedding provider behind an interface: a hosted API for the managed
  service, and a local model (via the existing `llama-helper` or ONNX) for
  self-hosters and the desktop app's local index.
- Search results return the meeting, the timestamp, the matching segment, and
  a deep link that opens the recording at that moment.

**Web dashboard**

- Meeting library: list, filter, open. Transcript view with audio playback
  synced to segments. Summary view with the same markdown editing the desktop
  app has (BlockNote can be reused).
- Sharing: per-meeting share link with view/comment permissions, scoped to
  the org or public with an expiring token.
- Account and org settings, API tokens, device list.
- Framework: Next.js is already in the repo and the team knows it; reuse
  components from `frontend/src/components` where they do not depend on Tauri.

### Founder decisions

- Which embedding model and provider for the managed service, and whether
  self-hosters get a local model by default (quality vs. cost vs. privacy).
- Whether the desktop app gets semantic search over local-only meetings in
  this phase, or only synced meetings are searchable semantically.
- Public share links: allowed at all in the first version?

---

## Phase 4: Meeting bot service

**Depends on:** Phase 2 (the bot writes into the same backend) and Phase 3
(users need a place to see bot-recorded meetings that were never on their
machine).

**Goal:** Afterword can attend a Zoom, Google Meet, or Teams meeting on the
user's behalf, with every participant told they are being recorded.

### Scope

**Bot service (separate repository)**

- A self-hosted service that launches a headless browser session per meeting
  (the Recall.ai / Vexa model), joins with a visible display name such as
  "Afterword Notetaker", captures the meeting's audio stream, and streams it to
  the same transcription pipeline used by the desktop app.
- One container per active meeting, orchestrated by a scheduler that scales
  workers with the calendar queue. Start with Google Meet (most tractable),
  then Zoom, then Teams.
- Reuse the Rust transcription and summarisation code by packaging it as a
  server-side worker, so desktop and bot produce identical transcripts and
  summaries.

**Calendar-triggered auto-join**

- Google Calendar and Microsoft 365 OAuth. The service watches upcoming
  events with conferencing links and joins at start time. Users choose
  per-calendar and per-event whether the bot attends. No manual link pasting
  as the primary flow.

**Recording-consent and disclosure flow (blocks external release)**

- Before the bot joins, the host is shown what will happen and confirms.
- On join, the bot posts a chat message and, where the platform allows, a
  spoken or visual notice that the meeting is being recorded by Afterword on
  behalf of the named user, with a link to the privacy policy.
- Participants who object: document what the host can do (remove the bot) and
  provide a per-meeting "stop and delete" control in the dashboard.
- Consent state stored on the meeting record. The bot does not ship to anyone
  outside internal testing until this flow exists and has been reviewed
  against the recording-consent laws of the launch markets.

### Founder decisions

- Self-hosted bot infrastructure first, or a managed fleet first? Self-hosted
  is cheaper to start and fits the local-first story; managed is what most
  paying teams will actually want.
- Which platform ships first. The recommendation above is Google Meet, but
  the primary market's usage split (Meet vs. Zoom vs. Teams vs. WhatsApp
  calls) should decide this.
- Bot display name and whether users can customise it.
- Legal review of consent requirements for Uganda, Kenya, Nigeria, South
  Africa, the EU, and the US before external release.

---

## Phase 5: Billing and integrations

**Depends on:** Phase 2 (accounts) for billing; Phase 3 (dashboard) and
Phase 4 (bot minutes as a metered resource) for the full pricing model.

**Goal:** Afterword can be paid for, from the primary market, and fits into
the tools teams already use.

### Scope

**Subscription billing**

- Reuse the existing mobile-money billing pattern (MTN MoMo, Airtel Money
  flows already built for the related project) for the primary market, plus
  card billing (Stripe or Paystack/Flutterwave) for global users.
- Metered resources: bot meeting minutes, cloud transcription minutes, cloud
  storage. The desktop app with local models stays free.
- Entitlements checked server-side; the desktop app reads a signed
  entitlement token so it works offline for the grace period.
- Invoices, receipts, and failed-payment handling. Mobile-money payments are
  often one-off rather than recurring mandates, so the plan model must
  support "pay for 30 days" as well as auto-renewing subscriptions.

**Integrations**

- Slack: post the summary and action items to a channel after each meeting.
- Notion: create a page per meeting in a chosen database.
- CRM: log meetings and summaries against contacts and deals. Start with one
  CRM and make the integration layer generic (webhook-style event
  `meeting.summarised` with a signed payload) so others are cheap to add.
- Outbound webhooks and a public API token so teams can build their own.

**Shareable meeting clips**

- Select a time range in the dashboard, cut the audio (FFmpeg server-side)
  with the matching transcript, and produce a share link or download.

### Founder decisions

- Pricing tiers and what is free forever. In particular: is the desktop app
  with cloud sync free, or is sync the first paid feature?
- Which mobile-money providers and which card processor to launch with.
- Which CRM first. HubSpot has the broadest reach; the primary market may
  favour something else.
- Whether clips are audio-only or also include a generated caption video.

---

## Cross-cutting principles

- **Local-first stays real.** Every phase must leave the no-account,
  no-network desktop experience working. Cloud features are additive.
- **One pipeline.** Desktop and bot must produce the same transcript and
  summary formats so search, sharing, and integrations never special-case the
  source.
- **Bandwidth-aware.** Assume a poor connection. Transcripts first, audio
  later and resumable.
- **Consent before convenience.** Nothing that records other people ships
  without a disclosure flow.
