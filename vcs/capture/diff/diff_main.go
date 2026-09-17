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
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
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

	a := loadPNG(args[0])
	b := loadPNG(args[1])

	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	bw, bh := b.Bounds().Dx(), b.Bounds().Dy()
	w, h := aw, ah
	if bw < w {
		w = bw
	}
	if bh < h {
		h = bh
	}
	if aw != bw || ah != bh {
		fmt.Fprintf(os.Stderr, "warning: size mismatch %dx%d vs %dx%d; comparing overlapping %dx%d region\n", aw, ah, bw, bh, w, h)
	}

	var heatmap draw.Image
	if *out != "" {
		heatmap = image.NewNRGBA(image.Rect(0, 0, w, h))
	}

	var (
		total     int
		differing int
		sumDistSq float64
		maxDist   float64
	)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ca := a.At(a.Bounds().Min.X+x, a.Bounds().Min.Y+y)
			cb := b.At(b.Bounds().Min.X+x, b.Bounds().Min.Y+y)
			dist := colorDistance(ca, cb)
			total++
			if dist > 0 {
				differing++
			}
			sumDistSq += dist * dist
			if dist > maxDist {
				maxDist = dist
			}
			if heatmap != nil {
				heatmap.Set(x, y, heatColor(dist))
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
		savePNG(*out, heatmap)
		fmt.Printf("heatmap written to %s\n", *out)
	}

	if pct > *threshold {
		fmt.Printf("FAIL: %.2f%% of pixels differ, exceeds threshold %.2f%%\n", pct, *threshold)
		os.Exit(1)
	}
	fmt.Println("PASS")
}

// colorDistance returns the Euclidean distance between two colors in 8-bit
// RGB space (0 == identical, ~441 == max possible distance, e.g. black vs white).
func colorDistance(a, b color.Color) float64 {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	dr := float64(ar>>8) - float64(br>>8)
	dg := float64(ag>>8) - float64(bg>>8)
	db := float64(ab>>8) - float64(bb>>8)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// heatColor maps a color distance onto a black -> red -> yellow heatmap.
func heatColor(dist float64) color.Color {
	t := dist / 441.0
	if t > 1 {
		t = 1
	}
	return color.NRGBA{
		R: uint8(255 * clamp01(t*2)),
		G: uint8(255 * clamp01(t*2-1)),
		B: 0,
		A: 255,
	}
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func loadPNG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("Can't open %s: %v", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatalf("Can't decode %s: %v", path, err)
	}
	return img
}

func savePNG(path string, img image.Image) {
	o, err := os.Create(path)
	if err != nil {
		log.Fatalf("Can't create %s: %v", path, err)
	}
	defer o.Close()
	if err := png.Encode(o, img); err != nil {
		log.Fatalf("Can't encode %s: %v", path, err)
	}
}
