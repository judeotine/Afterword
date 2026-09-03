# Afterword meeting bot

A small service that sends a notetaker into meetings the user cannot record on
their own machine. It joins the call in a headless Chromium, **announces that
the meeting is being recorded**, captures the call audio through a PulseAudio
null sink, and transcribes the result with `afterword-transcribe` — the same
Rust pipeline the desktop app uses, so the transcript JSON has the identical
`TranscriptSegment` shape (`id`, `text`, `audio_start_time`, `audio_end_time`,
`duration`, `display_time`, `confidence`, `sequence_id`).

Google Meet is the only platform implemented today. Zoom and Teams URLs are
recognised and answered with a clear "not yet supported" error.

## Consent policy

Recording without telling people is not a feature we ship.

- The worker calls `platform.announceConsent()` **before** the recorder starts.
  The two calls sit next to each other in `src/worker.ts` and must stay in that
  order; a change that starts the recorder first is a bug, not an optimisation.
- The message names the bot, the person it is attending for, what is shared
  with them, a privacy-policy link, and how to object (`src/consent.ts`).
- The bot joins muted, with its camera off, under a name that says what it is.
- **Do not deploy this externally until the disclosure flow has been reviewed.**
  Jurisdictions differ on one-party vs all-party consent, and the in-meeting
  notice is only part of it — see ROADMAP.md, Phase 4 ("Meeting bot service"),
  for the consent, retention and calendar-OAuth work that is still outstanding.

## Local run

Requires Node 22, pnpm 10, PulseAudio (`pactl`, `parecord`) and the
`afterword-transcribe` binary on `PATH`.

```bash
pnpm install
pnpm exec playwright install chromium   # no --with-deps on macOS
pnpm dev                                # http://localhost:8787
```

Schedule a meeting:

```bash
curl -sS -X POST http://localhost:8787/jobs \
  -H 'content-type: application/json' \
  -d '{
        "meetingUrl": "https://meet.google.com/abc-defg-hij",
        "onBehalfOf": "Jude Otine",
        "startAt": "2026-09-03T14:00:00Z"
      }'
# => 201 {"id":"<uuid>","status":"scheduled"}

curl -sS http://localhost:8787/jobs/<uuid>      # job record + artifact paths
curl -sS -X DELETE http://localhost:8787/jobs/<uuid>   # leave the meeting now
curl -sS http://localhost:8787/healthz
```

A job moves through `scheduled → joining → recording → transcribing → done`,
or to `failed` / `cancelled`. When it is `done` the record carries
`artifacts.wav` and `artifacts.transcript`.

## Docker

The image bundles PulseAudio, Xvfb and the Rust CLI. The build context is the
**repository root**, because stage 1 compiles `afterword-transcribe` from the
Cargo workspace:

```bash
docker build -f bot/Dockerfile -t afterword-bot .
# or, from bot/:
docker compose up --build
```

`docker-compose.yml` publishes port 8787 and mounts `./recordings` and
`./models`.

## Building afterword-transcribe locally

From the repository root:

```bash
cargo build --release -p afterword-core --bin afterword-transcribe
export TRANSCRIBE_BIN="$PWD/target/release/afterword-transcribe"
```

The CLI is invoked as
`afterword-transcribe --input <wav> --out <dir> --engine <whisper|parakeet> --model <name> --models-dir <dir>`
and writes `transcripts.json` plus `metadata.json` into `<dir>`. Exit code 2
means the audio could not be decoded, 3 means the model is missing.

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8787` | HTTP port for the job API |
| `BOT_NAME` | `Afterword Notetaker` | Display name the bot joins under |
| `RECORDINGS_DIR` | `./recordings` | Where `<job id>/meeting.wav` and transcripts are written |
| `MAX_MEETING_MINUTES` | `180` | Hard stop for a single meeting |
| `ALONE_TIMEOUT_SECONDS` | `120` | Leave after being the only participant this long |
| `TRANSCRIBE_BIN` | `afterword-transcribe` | Path to the Rust transcription CLI |
| `TRANSCRIBE_ENGINE` | `whisper` | `whisper` or `parakeet` |
| `TRANSCRIBE_MODEL` | `base` | Model name passed to the CLI |
| `MODELS_DIR` | `./models` | Model directory passed to the CLI |
| `PRIVACY_URL` | Afterword `PRIVACY_POLICY.md` on GitHub | Link included in the consent notice |
| `HEADLESS` | `true` | Run Chromium headless (`false` to watch it locally) |
| `PULSE_SINK_NAME` | `afterword_sink` | Name of the PulseAudio null sink |

## Layout

```
src/
  index.ts            Fastify API: POST /jobs, GET /jobs/:id, DELETE /jobs/:id, GET /healthz
  scheduler.ts        In-memory job store, timers, worker protocol parsing (pure + testable)
  worker.ts           One meeting per process: join → consent → record → transcribe
                      (dependencies injected, so the ordering rules are unit-tested)
  config.ts           zod-validated environment
  consent.ts          The consent notice
  transcribe.ts       afterword-transcribe invocation
  errors.ts           NotImplementedError, AdmissionTimeoutError, UnsupportedPlatformError
  audio/pulse.ts      Null sink + parecord
  platforms/          types.ts (interface + detectPlatform), meet.ts, zoom.ts, teams.ts
  calendar/           CalendarSource interface + Google stub (Phase 4)
test/                 vitest unit tests + a Playwright smoke test over a fake Meet page
```

## Tests

```bash
pnpm lint    # tsc --noEmit over src/ and test/
pnpm test    # vitest: pure logic + the Meet adapter smoke test
```

The smoke test serves `test/fixtures/fake-meet.html` — a page carrying the same
aria-labels and texts the Meet adapter looks for — and drives
`join`/`announceConsent`/`participantCount`/`leave` against it with Chromium.
The `?prejoinDelay=600` variant renders the join controls late, proving the
adapter waits for the SPA instead of probing once and giving up.

## Limitations

- **Google Meet only.** Zoom and Teams adapters exist but throw
  `NotImplementedError`; the API answers 501 for their URLs.
- **In-memory job store.** Restarting the service forgets every job and orphans
  running workers. Persistence lands with the Phase 2 backend.
- **No calendar integration.** `calendar/google.ts` is a stub; jobs are created
  over HTTP only.
- **Selectors will break.** Google changes the Meet DOM regularly. Every
  selector lives in `MEET_SELECTORS` in `src/platforms/meet.ts`; the smoke test
  fixture must be updated alongside it.
- **Linux/container audio.** `pactl`/`parecord` are required. On macOS the
  service runs and schedules jobs, but recording needs the Docker image (or a
  Linux host) with PulseAudio.
- **No authentication.** The API is unauthenticated; run it on a private
  network only.
