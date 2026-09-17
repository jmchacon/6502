# Emulator comparison tools

Tools for running a cart through this repo's Atari 2600 implementation and
through [Stella](https://github.com/stella-emu/stella), then comparing the
rendered output. None of this is wired into `go test`; it's for manual
side-by-side debugging against a reference implementation.

## 1. Capture from this implementation

```sh
go run ./vcs/capture -cart game.bin -mode NTSC -frames 300 -out mine.png
```

Headless (no SDL/display). By default crops to the visible picture area and
doubles horizontal resolution to correct pixel aspect ratio, matching how a
TV and other emulators frame the picture; pass `-crop=false` for the full
raw TIA signal including blanking/overscan. See `-help` for frame count,
outputting a whole sequence (`-outdir`/`-interval`), etc.

## 2. Capture from Stella

Stella isn't vendored here. Build it once:

```sh
git clone https://github.com/stella-emu/stella
cd stella && git checkout release-6.1-beta2  # current master needs SDL3
./configure && make -j$(nproc)
export STELLA_BIN="$PWD/stella"
```

(`release-6.1-beta2` is the newest tagged release still on SDL2, which is
what's packaged on most distros; current `master` requires SDL3.)

Then, with `Xvfb` and `xdotool` installed:

```sh
./vcs/capture/stella_capture.sh game.bin stella.png
```

This runs Stella headlessly under a virtual X display, waits a few seconds,
sends F12 to trigger Stella's own pixel-accurate TIA snapshot, and copies it
out.

## 3. Compare

```sh
go run ./vcs/capture/diff -out heatmap.png mine.png stella.png
```

Reports percentage of differing pixels and RMSE color distance, and (with
`-out`) writes a black/red/yellow heatmap PNG of where the two frames
diverge. Exits non-zero if the differing-pixel percentage exceeds
`-threshold` (default 2%). The two captures aren't always pixel-identical in
size — Stella auto-detects visible frame height per ROM, this
implementation always uses the nominal NTSC/PAL height — so `diff` compares
the overlapping top-left region and warns on mismatch rather than failing
outright.

For a plain visual side-by-side instead of a diff:

```sh
go run ./vcs/capture/montage -out compare.png mine.png stella.png
```
