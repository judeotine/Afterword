# Afterword

The summary that comes after the conversation.

Afterword is a local-first AI meeting assistant. The desktop app records your
microphone and system audio, transcribes on-device with Whisper or Parakeet,
and writes structured summaries with a local model or the LLM provider you
choose. No bot joins your call and nothing has to leave your machine.


## Status

Early. The desktop app builds and runs on macOS, with Windows and Linux builds
inherited from upstream. See [ROADMAP.md](ROADMAP.md) for where this is going:
packaged Linux releases, a shared backend and web dashboard, a calendar-driven
meeting bot, and billing.

## Repository layout

| Path | What it is |
| --- | --- |
| `frontend/` | Tauri 2 desktop app: Next.js UI in `src/`, Rust core in `src-tauri/` |
| `llama-helper/` | Sidecar binary that runs local GGUF summary models via llama.cpp |
| `docs/` | Build and architecture docs |
| `scripts/` | Release and testing helpers (update manifest, transcript injection) |
| `backend/` | Archived upstream Python/FastAPI backend. Unsupported, kept for reference |

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

## License

MIT. See [LICENSE.md](LICENSE.md).
