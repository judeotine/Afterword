# Releasing Afterword

This document describes how to cut a release of the Afterword desktop app. Releases
are built and published automatically by `.github/workflows/release.yml` whenever a
`v*` tag is pushed (or the workflow is run manually via `workflow_dispatch`).

## 1. Bump the version

The app version must be updated in three places, kept in sync:

- `frontend/package.json` — `"version"`
- `frontend/src-tauri/tauri.conf.json` — `"version"`
- `frontend/src-tauri/Cargo.toml` — `[package] version`

All three should use the same semantic version, e.g. `0.5.0`.

Commit the version bump on `main` (or the release branch) before tagging.

## 2. Tag the release

```bash
git tag v0.5.0
git push origin v0.5.0
```

Pushing a tag matching `v*` triggers `.github/workflows/release.yml`, which builds
the desktop app for:

- macOS (Apple Silicon) — `aarch64-apple-darwin`
- macOS (Intel) — `x86_64-apple-darwin`
- Windows — `windows-latest`
- Linux — `ubuntu-22.04`

For each target, the workflow builds the `llama-helper` sidecar binary, copies it to
`frontend/src-tauri/binaries/llama-helper-<target-triple>[.exe]`, and then runs
[`tauri-apps/tauri-action`](https://github.com/tauri-apps/tauri-action) to build and
bundle the app and attach the artifacts to a **draft** GitHub Release named
`Afterword v<version>`. Review the draft release and publish it manually once the
artifacts look correct.

The `.github/workflows/bot.yml` workflow also builds and pushes the bot's Docker
image (`ghcr.io/<owner>/afterword-bot`) on the same tag push, tagged with both the
release version and the commit SHA.

## 3. Required secrets

Configure these as repository (or organization) secrets before running a release:

| Secret | Used by | Purpose |
| --- | --- | --- |
| `TAURI_SIGNING_PRIVATE_KEY` | `release.yml` | Private key used to sign updater artifacts (`latest.json` + bundle signatures) so the in-app updater can verify releases against the `pubkey` in `tauri.conf.json`. Required — the build fails without it since `createUpdaterArtifacts: true`. |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | `release.yml` | Password protecting the above private key. Required alongside it. |
| `GITHUB_TOKEN` | `release.yml`, `bot.yml` | Automatically provided by GitHub Actions. Used by `tauri-action` to create/update the draft release and upload artifacts, and by `bot.yml` to authenticate to GHCR. No manual setup needed. |
| `APPLE_CERTIFICATE` | `release.yml` (macOS) | Base64-encoded `.p12` code-signing certificate for macOS app signing. Optional — if unset, the macOS build is ad-hoc signed (a warning is printed in the workflow log) and will not pass Gatekeeper/notarization on other machines. |
| `APPLE_CERTIFICATE_PASSWORD` | `release.yml` (macOS) | Password for the `.p12` certificate above. |
| `APPLE_SIGNING_IDENTITY` | `release.yml` (macOS) | Signing identity string (e.g. `Developer ID Application: ...`) matching the certificate. |
| `APPLE_ID` | `release.yml` (macOS) | Apple ID used for notarization. |
| `APPLE_PASSWORD` | `release.yml` (macOS) | App-specific password for the Apple ID above. |
| `APPLE_TEAM_ID` | `release.yml` (macOS) | Apple Developer Team ID. |
| `DIGICERT_KEYPAIR_ALIAS` | `release.yml` (Windows) | Keypair alias for DigiCert KeyLocker signing, consumed by `frontend/src-tauri/scripts/sign-windows.ps1` via the `signCommand` in `tauri.conf.json`. Optional — if unset, `sign-windows.ps1` no-ops and the Windows build is left unsigned (a warning is printed in the workflow log). |

All of the Apple and DigiCert secrets are optional in the sense that the workflow
will still complete without them, but the resulting artifacts will be unsigned for
that platform. Set them before cutting a production release intended for public
distribution.

## 4. Where `latest.json` ends up

Because `createUpdaterArtifacts: true` is set in `tauri.conf.json` and
`includeUpdaterJson: true` is passed to `tauri-action`, the release workflow
generates an updater manifest (`latest.json`) and attaches it directly to the
GitHub Release created for the tag, alongside the platform bundles (`.dmg`, `.msi`,
`.AppImage`, `.deb`, etc.) and their `.sig` signature files.

The app's updater config (`frontend/src-tauri/tauri.conf.json` →
`plugins.updater.endpoints`) points at:

```
https://github.com/judeotine/Afterword/releases/latest/download/latest.json
```

so once the draft release is published, installed copies of Afterword will
discover the new version automatically via that URL.

## 5. Local testing only: `generate-update-manifest-github.js`

`scripts/generate-update-manifest-github.js` predates the automated release
workflow and manually builds a `latest.json` from local bundle output. It is no
longer part of the release process — `tauri-action`'s `includeUpdaterJson: true`
now generates and uploads `latest.json` automatically in CI. Keep using the script
only for local testing of the updater manifest format (e.g. verifying a locally
built bundle produces the expected JSON) before pushing a release tag.
