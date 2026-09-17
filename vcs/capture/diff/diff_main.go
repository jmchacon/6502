// Command diff quantitatively compares two rendered frame PNGs (e.g. this
// repo's vcs/capture output against a Stella snapshot from
// vcs/capture/stella_capture.sh) and reports how closely they match.
//
// The two captures won't always be the exact same size (Stella
// auto-detects visible frame height per ROM, this implementation always
// crops to the nominal NTSC/PAL height), so comparison is done over the
// overlapping top-left region of both images and any size mismatch is
// reported rather than treated as a hard failure.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"log"
	"math"
	"os"

	"github.com/jmchacon/6502/vcs/capture/internal/imgutil"
)

var (
	out       = flag.String("out", "", "Optional path to write a heatmap PNG visualizing per-pixel differences")
	threshold = flag.Float64("threshold", 2.0, "Exit non-zero if the percentage of differing pixels exceeds this value")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		log.Fatal("usage: diff [-out heatmap.png] [-threshold pct] <img1.png> <img2.png>")
	}

	a := imgutil.LoadPNG(args[0])
	b := imgutil.LoadPNG(args[1])

	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	bw, bh := b.Bounds().Dx(), b.Bounds().Dy()
	w, h := imgutil.OverlapSize(a, b)
	if aw != bw || ah != bh {
		fmt.Fprintf(os.Stderr, "warning: size mismatch %dx%d vs %dx%d; comparing overlapping %dx%d region\n", aw, ah, bw, bh, w, h)
	}

	var heatmap draw.Image
	if *out != "" {
		heatmap = image.NewNRGBA(image.Rect(0, 0, w, h))
	}

	amin, bmin := a.Bounds().Min, b.Bounds().Min
	var (
		total     int
		differing int
		sumDistSq float64
		maxDist   float64
	)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dist := imgutil.ColorDistance(a.At(amin.X+x, amin.Y+y), b.At(bmin.X+x, bmin.Y+y))
			total++
			if dist > 0 {
				differing++
			}
			sumDistSq += dist * dist
			if dist > maxDist {
				maxDist = dist
			}
			if heatmap != nil {
				heatmap.Set(x, y, imgutil.HeatColor(dist))
			}
		}
	}

	pct := 0.0
	rmse := 0.0
	if total > 0 {
		pct = 100 * float64(differing) / float64(total)
		rmse = math.Sqrt(sumDistSq / float64(total))
	}

	fmt.Printf("compared: %dx%d (%d pixels)\n", w, h, total)
	fmt.Printf("differing pixels: %d (%.2f%%)\n", differing, pct)
	fmt.Printf("RMSE color distance: %.2f (max %.2f, scale 0-441)\n", rmse, maxDist)

	if heatmap != nil {
		imgutil.SavePNG(*out, heatmap)
		fmt.Printf("heatmap written to %s\n", *out)
	}

	if pct > *threshold {
		fmt.Printf("FAIL: %.2f%% of pixels differ, exceeds threshold %.2f%%\n", pct, *threshold)
		os.Exit(1)
	}
	fmt.Println("PASS")
}
