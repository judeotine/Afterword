#!/usr/bin/env bash
# Bring up the display and audio plumbing the bot needs, then run the service.
#
# Both daemons must be up before the first job: Chromium needs a display, and
# `pactl`/`parecord` need a running PulseAudio. Failing here is much better than
# failing per-job with a confusing "connection refused" from pactl.
set -euo pipefail

export DISPLAY="${DISPLAY:-:99}"

# PulseAudio refuses to run as root, so the image runs as pwuser. Say so plainly
# rather than limping on into per-job failures.
if [ "$(id -u)" = "0" ]; then
  echo "afterword-bot: refusing to start as root; PulseAudio will not run as root." >&2
  echo "afterword-bot: run the image as the non-root 'pwuser' user." >&2
  exit 1
fi

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/runtime-$(id -u)}"
mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"

wait_for() {
  local what="$1" attempts="$2"
  shift 2
  for _ in $(seq 1 "$attempts"); do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  echo "afterword-bot: $what did not become ready in time" >&2
  return 1
}

Xvfb "$DISPLAY" -screen 0 1280x720x24 -nolisten tcp &
wait_for "Xvfb on $DISPLAY" 40 xdpyinfo -display "$DISPLAY"

pulseaudio --start --exit-idle-time=-1
wait_for "PulseAudio" 40 pactl info

exec "$@"
