# Notice

Afterword is a fork of **Meetily** by Zackriya Solutions:
https://github.com/Zackriya-Solutions/meeting-minutes

Meetily is released under the MIT License. The original copyright notice is
retained in [LICENSE.md](LICENSE.md) alongside the Afterword copyright line, as
the MIT License requires. Afterword is an independent project and is not
affiliated with or endorsed by Zackriya Solutions.

## What has changed from upstream so far

- Product renamed to Afterword across the desktop app: window title, tray,
  notifications, onboarding and settings copy, About screen, docs, and build
  scripts.
- Bundle identifier changed from `com.meetily.ai` to `com.afterword.app`;
  Cargo package renamed `meetily` to `afterword`; npm package renamed
  `meetily` to `afterword`.
- Auto-updater now points at this repository's GitHub releases and uses a new
  signing key. Builds of Afterword will not receive Meetily updates.
- The upstream PostHog analytics project key was removed. Analytics stays
  opt-in and is a no-op unless `AFTERWORD_POSTHOG_KEY` is provided at build
  time.
- Default per-user storage paths renamed (`afterword-recordings`,
  `Afterword` data directories, `AfterwordRecoveryDB`). The legacy database
  importer still recognises data from a previous upstream Meetily install.
- The archived Python/FastAPI backend under `backend/` is kept unmodified for
  historical reference only. It is not part of the supported product.
- Added `crates/afterword-core`, a Tauri-free crate holding the shared audio
  and transcription pipeline, plus an `afterword-transcribe` CLI built from
  it. The desktop app's Rust core (`app_lib`) now depends on this crate
  instead of containing that code directly.
- Added `bot/`, a meeting-bot service (not present in upstream Meetily) that
  joins meetings the user did not host locally and transcribes them with the
  same `afterword-transcribe` CLI. See `bot/README.md` for its scope and
  consent policy.

## Still carrying upstream artwork and hosted assets

- App icons, logos, and screenshots under `frontend/src-tauri/icons/`,
  `frontend/public/`, and `docs/` are still Meetily's artwork and need to be
  replaced.
- The build downloads prebuilt FFmpeg binaries from
  `github.com/Zackriya-Solutions/ffmpeg-binaries`, and the default Parakeet v3
  speech model is fetched from a Meetily-hosted mirror. Both are tracked as
  roadmap items to re-host.
