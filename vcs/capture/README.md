# Emulator comparison tools

Tools for running a cart through this repo's Atari 2600 implementation and
through [Stella](https://github.com/stella-emu/stella), then comparing the
rendered output. None of this is wired into `go test`; it's for manual
side-by-side debugging against a reference implementation.

**Recommended workflow: capture frame sequences from both, then let `align`
find the matching offset (step 3).** Wall-clock timing does not reliably
line up two independent emulator processes to the same frame — confirmed
here by running Stella's single-shot capture twice with the same nominal
delay and getting two different frames back. Single-frame capture (`-out`
below, and `stella_capture.sh single`) is only for quick, informal spot
checks.

## 1. Capture a frame sequence from this implementation

```sh
go run ./vcs/capture -cart game.bin -mode NTSC -frames 300 -outdir mine_seq -interval 1
```

Headless (no SDL/display). By default crops to the visible picture area and
doubles horizontal resolution to correct pixel aspect ratio, matching how a
TV and other emulators frame the picture; pass `-crop=false` for the full
raw TIA signal including blanking/overscan. `-outdir`/`-interval 1` writes
every frame as a numbered PNG (`-out` instead writes just the final frame,
for quick spot checks).

## 2. Capture a frame sequence from Stella

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
./vcs/capture/stella_capture.sh seq game.bin stella_seq 300
```

This runs Stella headlessly under a virtual X display and toggles Stella's
own built-in per-frame continuous snapshot mode (Alt+Shift+S) right after
launch. That mode is driven by Stella's internal frame clock
(`EventHandler::poll()` calls `PNGLibrary::updateTime()` once per emulated
frame), not wall-clock time, so unlike a single timed screenshot it produces
a frame-accurate, reproducible sequence. It polls until 300 PNGs exist (or a
timeout) and copies them into `stella_seq/` in capture order.

## 3. Align and compare

`vcs/capture`'s sequence and `stella_capture.sh seq`'s sequence each start
counting from their own "frame 1" (ROM launch vs. the moment continuous
snapshot mode was toggled on), so expect a fixed constant offset between
them, not frame-for-frame equality:

```sh
go run ./vcs/capture/align -out heatmap.png mine_seq stella_seq
```

Searches a range of offsets (`-max-offset`, default ±60 frames) using cheap
per-frame luminance signatures (robust to small palette differences between
implementations, which would otherwise swamp the search), reports the best
offset and the mean RMSE color distance across all aligned pairs, and (with
`-out`) writes a heatmap for the single closest-matching pair.

For one already-known-aligned pair (e.g. two single-shot captures, or one
specific frame pulled from each sequence):

```sh
go run ./vcs/capture/diff -out heatmap.png mine.png stella.png
```

Reports percentage of differing pixels and RMSE color distance, and (with
`-out`) writes a black/red/yellow heatmap PNG of where the two frames
diverge. Exits non-zero if the differing-pixel percentage exceeds
`-threshold` (default 2%). The two captures aren't always pixel-identical in
size — Stella auto-detects visible frame height per ROM, this
implementation always uses the nominal NTSC/PAL height — so both `diff` and
`align` compare the overlapping top-left region and warn on mismatch rather
than failing outright.

For a plain visual side-by-side instead of a diff:

```sh
go run ./vcs/capture/montage -out compare.png mine.png stella.png
```
