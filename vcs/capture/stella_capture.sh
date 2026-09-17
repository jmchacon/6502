#!/usr/bin/env bash
# Headlessly run a cart in Stella and grab a PNG snapshot of the TIA output,
# for comparison against this repo's own capture tool (vcs/capture) via
# vcs/capture/diff or vcs/capture/montage.
#
# Usage: stella_capture.sh <rom.bin> <output.png> [seconds_to_run]
#
# Requires:
#   - Xvfb and xdotool (apt install xvfb xdotool)
#   - A built Stella binary. Stella isn't vendored in this repo; check it
#     out and build it separately (https://github.com/stella-emu/stella),
#     then point STELLA_BIN at the resulting binary, e.g.:
#       git clone https://github.com/stella-emu/stella
#       cd stella && git checkout release-6.1-beta2  # current master needs SDL3
#       ./configure && make -j$(nproc)
#       export STELLA_BIN="$PWD/stella"
#   release-6.1-beta2 is the newest tagged release still on SDL2; current
#   master requires SDL3, which isn't packaged on many distros yet.

set -euo pipefail

STELLA_BIN="${STELLA_BIN:?Set STELLA_BIN to a built Stella binary (see script header for how to build one)}"
ROM="$1"
OUT="$2"
RUN_SECS="${3:-4}"

if [ ! -x "$STELLA_BIN" ]; then
    echo "Stella binary not found/executable at $STELLA_BIN" >&2
    exit 1
fi
if [ ! -f "$ROM" ]; then
    echo "ROM not found: $ROM" >&2
    exit 1
fi

WORKDIR="$(mktemp -d)"
SNAPDIR="$WORKDIR/snaps"
mkdir -p "$SNAPDIR"
DISPLAY_NUM=":$(( (RANDOM % 400) + 100 ))"

cleanup() {
    [ -n "${STELLA_PID:-}" ] && kill "$STELLA_PID" 2>/dev/null || true
    [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null || true
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

Xvfb "$DISPLAY_NUM" -screen 0 640x480x24 >/dev/null 2>&1 &
XVFB_PID=$!
sleep 1

export DISPLAY="$DISPLAY_NUM"

# -ss1x: raw TIA snapshot, no scaling/filters, for pixel-accurate comparison.
# -sssingle: overwrite a single file instead of numbering snapshots.
timeout "$((RUN_SECS + 10))" "$STELLA_BIN" \
    -video software -fullscreen 0 \
    -ss1x 1 -sssingle 1 \
    -snapsavedir "$SNAPDIR" -snapname rom \
    "$ROM" >/dev/null 2>&1 &
STELLA_PID=$!

sleep "$RUN_SECS"

WIN="$(xdotool search --name "Stella" | head -1 || true)"
if [ -z "$WIN" ]; then
    echo "Could not find Stella window on $DISPLAY_NUM" >&2
    exit 1
fi
xdotool key --window "$WIN" F12
sleep 1

SNAP="$(find "$SNAPDIR" -name '*.png' | head -1 || true)"
if [ -z "$SNAP" ]; then
    echo "No snapshot produced" >&2
    exit 1
fi
mkdir -p "$(dirname "$OUT")"
cp "$SNAP" "$OUT"
echo "Saved $OUT"
