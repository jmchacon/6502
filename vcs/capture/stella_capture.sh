#!/usr/bin/env bash
# Headlessly run a cart in Stella and grab PNG snapshot(s) of the TIA output,
# for comparison against this repo's own capture tool (vcs/capture) via
# vcs/capture/align, vcs/capture/diff or vcs/capture/montage.
#
# Usage:
#   stella_capture.sh seq <rom.bin> <outdir> <num_frames> [timeout_secs]
#   stella_capture.sh single <rom.bin> <output.png> [seconds_to_run]
#
# 'seq' is the recommended mode: it toggles Stella's built-in per-frame
# continuous snapshot mode (Alt+Shift+S) immediately after launch, which is
# driven by Stella's own internal frame clock (EventHandler::poll() calls
# PNGLibrary::updateTime() once per emulated frame), not wall-clock time. It
# then polls until <num_frames> PNGs exist (or <timeout_secs> elapses) and
# copies them into <outdir> as 000001.png, 000002.png, ... in capture order.
# Because both this script and vcs/capture's own -outdir mode produce
# frame-ordered sequences, vcs/capture/align can search for the actual
# matching offset between them instead of assuming wall-clock timing lines
# up (it doesn't: the same nominal-time 'single' capture was observed to
# land on different frames run to run in this environment).
#
# 'single' is kept only for quick, informal spot checks. It waits a
# wall-clock number of seconds and presses F12 for one snapshot, which is
# NOT frame-accurate -- prefer 'seq' + vcs/capture/align for anything you
# intend to actually compare.
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
MODE="${1:?Usage: stella_capture.sh seq|single <rom.bin> <output> [...]}"
ROM="$2"

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

case "$MODE" in
seq)
    OUTDIR="${3:?Usage: stella_capture.sh seq <rom.bin> <outdir> <num_frames> [timeout_secs]}"
    NUM_FRAMES="${4:?Usage: stella_capture.sh seq <rom.bin> <outdir> <num_frames> [timeout_secs]}"
    TIMEOUT_SECS="${5:-30}"

    timeout "$((TIMEOUT_SECS + 10))" "$STELLA_BIN" \
        -video software -fullscreen 0 -ss1x 1 \
        -snapsavedir "$SNAPDIR" -snapname rom \
        "$ROM" >/dev/null 2>&1 &
    STELLA_PID=$!

    WIN="$(xdotool search --sync --name "Stella" | head -1 || true)"
    if [ -z "$WIN" ]; then
        echo "Could not find Stella window on $DISPLAY_NUM" >&2
        exit 1
    fi
    # Alt+Shift+S: toggle per-frame continuous snapshot mode.
    xdotool key --window "$WIN" alt+shift+s

    deadline=$((SECONDS + TIMEOUT_SECS))
    count=0
    while [ "$SECONDS" -lt "$deadline" ]; do
        count=$(find "$SNAPDIR" -name '*.png' | wc -l)
        [ "$count" -ge "$NUM_FRAMES" ] && break
        sleep 0.25
    done

    mkdir -p "$OUTDIR"
    i=0
    # Filenames are '<rom>_<hex-time>.png', monotonically increasing, so a
    # plain lexicographic sort is chronological/frame order.
    while IFS= read -r f; do
        i=$((i + 1))
        cp "$f" "$(printf '%s/%06d.png' "$OUTDIR" "$i")"
        [ "$i" -ge "$NUM_FRAMES" ] && break
    done < <(find "$SNAPDIR" -name '*.png' | sort)

    if [ "$i" -eq 0 ]; then
        echo "No snapshots produced" >&2
        exit 1
    fi
    if [ "$i" -lt "$NUM_FRAMES" ]; then
        echo "warning: only captured $i/$NUM_FRAMES frames before timeout" >&2
    fi
    echo "Saved $i frame(s) to $OUTDIR"
    ;;

single)
    OUT="${3:?Usage: stella_capture.sh single <rom.bin> <output.png> [seconds_to_run]}"
    RUN_SECS="${4:-4}"

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
    ;;

*)
    echo "Unknown mode '$MODE'; expected 'seq' or 'single'" >&2
    exit 1
    ;;
esac
