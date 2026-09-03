#!/usr/bin/env bash
# Bring up the audio and display plumbing the bot needs, then run the service.
set -euo pipefail

# A null sink needs a running daemon; --exit-idle-time=-1 keeps it alive while
# no client is connected.
pulseaudio --start --exit-idle-time=-1 --disallow-exit || true

# Chromium is far better behaved with a real display than with --headless for
# WebRTC, so give it a virtual one.
Xvfb "${DISPLAY:-:99}" -screen 0 1280x720x24 -nolisten tcp &
export DISPLAY="${DISPLAY:-:99}"

exec "$@"
