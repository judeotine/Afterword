# Testing Linux System-Audio Capture

Afterword captures system audio on Linux by recording the **monitor source of the
default sink** through the PulseAudio client API (`libpulse`). PipeWire users go
through the same path via `pipewire-pulse`, its PulseAudio compatibility layer.
The microphone continues to be captured with cpal/ALSA.

This is a **manual** checklist: it needs a real sound server, real speakers and a
real browser, so it cannot run in CI.

- Backend: `frontend/src-tauri/src/audio/capture/pulse_monitor.rs`
- Wiring: `frontend/src-tauri/src/audio/stream.rs` (`create_pulse_monitor_stream`)
- Device entry: `System Audio (PulseAudio/PipeWire)` (constant
  `LINUX_SYSTEM_AUDIO_DEVICE_NAME` in `audio/devices/configuration.rs`)

---

## 0. Prerequisites

```bash
sudo apt install libpulse-dev libasound2-dev   # build-time headers
pactl info                                     # must print server info
```

`pactl info` tells you which server you are on:

- `Server Name: pulseaudio` → classic PulseAudio
- `Server Name: PulseAudio (on PipeWire x.y.z)` → PipeWire + `pipewire-pulse`

Confirm a monitor source exists and note its name:

```bash
pactl list short sources | grep monitor
# e.g. 51  alsa_output.pci-0000_00_1f.3.analog-stereo.monitor  ...
```

Sanity check the monitor **before** touching Afterword — play something audible,
then:

```bash
# Record 5 s of what the speakers are playing
parec --device=@DEFAULT_MONITOR@ --format=float32le --rate=48000 --channels=1 \
  --file-format=wav /tmp/monitor-check.wav &
sleep 5; kill %1
ffprobe -hide_banner /tmp/monitor-check.wav
ffmpeg -hide_banner -i /tmp/monitor-check.wav -af volumedetect -f null - 2>&1 | grep mean_volume
```

`mean_volume` well below `-91 dB` means the monitor produced digital silence — fix
that first (wrong default sink, sink suspended, or you are muted), because
Afterword reads exactly the same source.

---

## 1. Device list

1. Start the app: `./dev-gpu.sh` (or `pnpm run tauri:dev` from `frontend/`).
2. Open the device selector.
3. **Expect:** an output/system entry named exactly
   `System Audio (PulseAudio/PipeWire)`, listed before any
   `... .monitor (System Audio)` ALSA entries, and selected by default.

This entry is synthetic: it is never resolved through cpal, so it appears even
when cpal cannot see a monitor device.

---

## 2. Record with system audio

1. Play something with speech in Firefox (a YouTube video, a podcast) at a normal
   volume through the **default** sink.
2. In Afterword, select the microphone plus `System Audio (PulseAudio/PipeWire)`
   and start recording.
3. Speak into the microphone a few times so both sources are present.
4. **Expect in the terminal logs:**
   ```
   🎵 Stream: Using PulseAudio/PipeWire monitor backend for system audio
   🔊 Pulse: connecting to source '@DEFAULT_MONITOR@' (48000 Hz, 1 ch, f32le)
   ✅ Pulse: connected to source '@DEFAULT_MONITOR@'
   ✅ Pulse: capture thread started for '@DEFAULT_MONITOR@'
   ```
5. **Expect in the UI:** the system-audio level meter moves with the browser
   audio, and transcript lines appear for the browser speech, not only for you.
6. While recording, confirm the sound server sees the client:
   ```bash
   pactl list source-outputs | grep -A6 -i afterword
   # application.name = "Afterword", stream name "system audio"
   ```
7. Stop the recording. **Expect:**
   ```
   Aborting PulseAudio monitor task...
   ⚠️ Pulse: capture thread ended for '@DEFAULT_MONITOR@'
   ```
   and no hang on stop (the capture thread is joined; this takes < ~100 ms).

---

## 3. Verify the saved file

Recordings land in `~/Documents/afterword-recordings/<Meeting Name>/audio.mp4`.
The mic and system streams are **mixed into a single mono track** by the audio
pipeline, so the check is that the track exists, has the expected duration, and
actually contains the browser audio.

```bash
REC=~/Documents/afterword-recordings/<Meeting_Name>/audio.mp4

# Track present, and long enough
ffprobe -hide_banner -show_entries stream=index,codec_name,channels,sample_rate \
        -show_entries format=duration -of default=noprint_wrappers=1 "$REC"

# Not silence
ffmpeg -hide_banner -i "$REC" -af volumedetect -f null - 2>&1 | grep -E "mean_volume|max_volume"

# Listen: the browser audio must be audible alongside your voice
ffplay -hide_banner -autoexit "$REC"
```

**Pass criteria**

- [ ] Exactly one audio stream, duration ≈ the recording length.
- [ ] `mean_volume` is clearly above the silence floor (not `-91 dB`).
- [ ] Playback contains both the browser audio and the microphone.
- [ ] The meeting transcript contains words that were only spoken by the browser.

---

## 4. Repeat on both server variants

Run sections 1–3 on each of:

- [ ] **PipeWire** (`pactl info` shows `PulseAudio (on PipeWire ...)`) — the common
      default on Fedora 34+, Ubuntu 22.10+, Arch.
- [ ] **Classic PulseAudio** (`pactl info` shows `Server Name: pulseaudio`) — e.g.
      Ubuntu 22.04, Debian 12.

Behaviour must be identical; `@DEFAULT_MONITOR@` is understood by both.

Extra checks worth doing once per variant:

- [ ] **Switch the default sink while recording** (`pactl set-default-sink ...`).
      The stream stays on the sink it started on; recording must keep working and
      must not crash. Restart the recording to follow the new sink.
- [ ] **No sound server running** (`systemctl --user stop pipewire-pulse` or
      `pulseaudio -k`): starting a recording must fail gracefully with
      *"Failed to open PulseAudio monitor source '@DEFAULT_MONITOR@' … PulseAudio/PipeWire
      may not be running, or libpulse is missing."*, and microphone-only recording
      must still work.

---

## 5. Flatpak note

A Flatpak sandbox has no access to the sound server unless the socket is shared.
The manifest (and any `flatpak run` override) must include:

```
--socket=pulseaudio
```

Without it, `Simple::new` fails with a connection-refused error and system audio
is silently unavailable while the microphone may still work through the same
socket.

`packaging/flatpak/com.afterword.app.yml` already lists it under `finish-args`;
verify the built app kept it:

```bash
flatpak info --show-permissions com.afterword.app | grep -i pulseaudio
```

Note that `--socket=pulseaudio` grants **both** playback and monitor recording:
there is no finer-grained portal for monitor capture, so the checklist above is
the only way to confirm it works in the sandbox. See [FLATPAK.md](FLATPAK.md) for
the rest of the packaging setup.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `Failed to open PulseAudio monitor source` | server not running, or no socket in the sandbox | start `pipewire-pulse`/`pulseaudio`; add `--socket=pulseaudio` |
| Recording works but system track is silent | wrong default sink, or the app plays to another sink | `pactl set-default-sink <sink>`, restart the recording |
| Silence only when the screen is idle | sink suspended by `module-suspend-on-idle` | keep audio playing, or unload the module while testing |
| Build fails with `pkg-config … libpulse` | headers missing | `sudo apt install libpulse-dev` |
