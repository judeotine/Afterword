# Flatpak packaging

This directory documents the Flatpak packaging for Afterword. The manifest,
desktop entry, and AppStream metainfo live in
[`packaging/flatpak/`](../packaging/flatpak/):

- `com.afterword.app.yml` — flatpak-builder manifest
- `com.afterword.app.desktop` — desktop entry
- `com.afterword.app.metainfo.xml` — AppStream metainfo

The Flatpak build does **not** compile Afterword from source. It downloads
the `.deb` produced for each GitHub Release, unpacks it, and repackages the
already-built binary and sidecars into the Flatpak sandbox. This keeps the
Flatpak in lockstep with the same release artifact that Debian/Ubuntu users
install, instead of maintaining a second build path.

## Building locally

Prerequisites (Fedora/Debian package names shown; adjust for your distro):

```bash
# Fedora
sudo dnf install flatpak flatpak-builder

# Debian/Ubuntu
sudo apt install flatpak flatpak-builder
```

Add Flathub and the GNOME 46 runtime/SDK if you don't already have them:

```bash
flatpak remote-add --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo
flatpak install flathub org.gnome.Platform//46 org.gnome.Sdk//46
```

Then, from the repository root:

```bash
flatpak-builder --user --install --force-clean build-dir packaging/flatpak/com.afterword.app.yml
```

This downloads the `.deb` referenced in the manifest's `sources`, verifies
its sha256, unpacks it, and installs the app into your user Flatpak
installation. Run it with:

```bash
flatpak run com.afterword.app
```

To build without installing (e.g. to just check the manifest is valid and
the sources fetch/build correctly), drop `--install`:

```bash
flatpak-builder --force-clean build-dir packaging/flatpak/com.afterword.app.yml
```

## Updating the sha256 after each release

The manifest pins the exact `.deb` for the current version by URL **and**
sha256, so a stale or tampered file can't be substituted silently. After
`release.yml` publishes a new GitHub Release:

1. Update the `url` in `packaging/flatpak/com.afterword.app.yml` to point at
   the new tag/filename, e.g.:
   ```
   https://github.com/judeotine/Afterword/releases/download/v<version>/afterword_<version>_amd64.deb
   ```
2. Compute the sha256 of the published asset. Either:
   - Take it from the release workflow's checksum output (if `release.yml`
     publishes a `SHA256SUMS`/checksums file alongside the artifacts), or
   - Download the asset and hash it yourself:
     ```bash
     curl -LO https://github.com/judeotine/Afterword/releases/download/v<version>/afterword_<version>_amd64.deb
     sha256sum afterword_<version>_amd64.deb
     ```
3. Replace the placeholder/old `sha256:` value in the manifest with that
   digest.
4. Bump the `<release version="..." date="...">` entry in
   `com.afterword.app.metainfo.xml` to match.
5. Rebuild locally (see above) to confirm the manifest still resolves and
   installs cleanly before committing/submitting.

## Flathub submission steps

Flathub packages are built from a manifest hosted in a dedicated repo under
the `flathub` GitHub org, reviewed via pull request. Rough process:

1. Fork [flathub/flathub](https://github.com/flathub/flathub).
2. Create a new branch named after the app id, e.g. `new-pr/com.afterword.app`.
3. Add `com.afterword.app.yml`, `com.afterword.app.desktop`, and
   `com.afterword.app.metainfo.xml` (copied from `packaging/flatpak/` in
   this repo, with the sha256 kept up to date per the section above) to
   that branch.
4. Open a pull request against `flathub/flathub` from that branch. This
   triggers Flathub's automated build/lint checks (manifest validation,
   `appstreamcli validate`, desktop-file-validate, etc.).
5. Address reviewer feedback from the Flathub maintainers — required
   reviews typically check: correct `finish-args` (least-privilege
   sandboxing), valid AppStream metadata with real screenshots, a
   working icon, and that the app builds reproducibly from the pinned
   source.
6. Once approved and merged, Flathub creates a build repository
   (`flathub/com.afterword.app`) that becomes the source of truth for
   future updates — subsequent releases are made by pushing manifest
   updates (e.g. the sha256 bump above) to that repo directly, either
   manually or via an automated version-bump PR.

See Flathub's own
[App Submission docs](https://docs.flathub.org/docs/for-app-authors/submission)
for the current, authoritative checklist, since requirements evolve.

## Known limitations

- **Sandboxed system-audio capture**: system audio (the "record what's
  playing" side of meeting capture) depends on a PulseAudio/PipeWire
  monitor source being reachable from inside the sandbox. The manifest
  grants `--socket=pulseaudio` for this; on a pure PipeWire system this
  works through PipeWire's PulseAudio compatibility layer, but there is no
  additional Flatpak portal for enumerating/selecting monitor sources, so
  device selection is whatever PulseAudio/PipeWire exposes over that
  socket. `--socket=wayland` and `--socket=fallback-x11` cover video/UI as
  usual; screen-recording-style portals (e.g. `xdg-desktop-portal`
  `ScreenCast`) are not requested because Afterword captures audio, not
  video, and doesn't need them.
- **Notifications and tray icon**: these rely on `--talk-name=org.freedesktop.Notifications`
  and `--talk-name=org.kde.StatusNotifierWatcher` being available on the
  host session bus. Desktop environments without a StatusNotifierWatcher
  implementation (some minimal window manager setups) may not show the
  tray icon even though the app still runs.
- **Recordings/model storage**: the sandbox is granted `--filesystem=xdg-videos`
  (default recordings location, `~/Videos/afterword-recordings`) and
  `--filesystem=xdg-documents`, but not broad home directory access. Users
  who redirect the recordings or model directory outside of
  `~/Videos`/`~/Documents` will need to grant additional filesystem access
  (e.g. via Flatseal) for those custom locations to work.
- **GPU acceleration**: `--device=dri` exposes the host GPU for hardware
  acceleration (WebKit rendering and, where supported, Whisper GPU
  inference), but CUDA-specific acceleration is not available inside the
  GNOME runtime's driver stack — Flatpak builds should be expected to fall
  back to CPU or Vulkan rather than CUDA.
- **No separate backend**: like the `.deb`/AppImage builds, the Flatpak
  packages the Tauri desktop app only; it does not package or depend on
  the archived FastAPI backend under `backend/`.
