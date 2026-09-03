# Afterword

The summary that comes after the conversation.

Afterword is a local-first AI meeting assistant. The desktop app records your
microphone and system audio, transcribes on-device with Whisper or Parakeet,
and writes structured summaries with a local model or the LLM provider you
choose. No bot joins your call and nothing has to leave your machine.

Afterword is proprietary software and this repository is private.


## Status

Early. The desktop app builds and runs on macOS, with Windows and Linux builds
inherited from upstream. See [ROADMAP.md](ROADMAP.md) for where this is going:
packaged Linux releases, a shared backend and web dashboard, a calendar-driven
meeting bot, and billing.

## Repository layout

| Path | What it is |
| --- | --- |
| `frontend/` | Tauri 2 desktop app: Next.js UI in `src/`, Rust core in `src-tauri/` |
| `crates/afterword-core/` | Tauri-free transcription pipeline shared by the desktop app and the bot; also builds the `afterword-transcribe` CLI |
| `bot/` | Meeting bot service that joins calls the user did not host and transcribes them |
| `llama-helper/` | Sidecar binary that runs local GGUF summary models via llama.cpp |
| `docs/` | Build and architecture docs |
| `scripts/` | Release and testing helpers (update manifest, transcript injection) |
| `packaging/flatpak/` | Flatpak manifest and metadata for the Linux release |
| `backend/` | Archived upstream Python/FastAPI backend. Unsupported, kept for reference |

## Meeting bot

`bot/` is a separate service, not part of the desktop app, for meetings the
user did not host on their own machine (currently Google Meet only). It
always announces that it is recording before it starts, and must not be
deployed externally until the consent flow has had legal review. See
[bot/README.md](bot/README.md) for setup, the job API, and the full consent
policy.

## Building

Prerequisites: Node 18+, pnpm, Rust stable, CMake. On macOS you also need the
Xcode Command Line Tools.

```bash
cd frontend
pnpm install
pnpm tauri:dev        # development build with auto-detected GPU backend
pnpm tauri:build      # production bundle
```

Platform details, GPU backends, and Linux packaging notes are in
[docs/BUILDING.md](docs/BUILDING.md) and [docs/GPU_ACCELERATION.md](docs/GPU_ACCELERATION.md).

### Development

From `frontend/`: `pnpm lint`, `pnpm test`. From the repo root:
`cargo test --workspace` (covers `frontend/src-tauri`,
`crates/afterword-core`, and `llama-helper`). From `bot/`: `pnpm lint`,
`pnpm test`.

## License

Afterword is proprietary software and this repository is private. Afterword
began as a fork of Meetily by Zackriya Solutions, released under the MIT
License, and keeps the required MIT attribution and copyright notices in
[LICENSE.md](LICENSE.md) — see
[NOTICE.md](NOTICE.md) for what has changed since the fork.
