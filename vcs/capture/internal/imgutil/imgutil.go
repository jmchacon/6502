// Package imgutil holds small helpers shared by the vcs/capture comparison
// tools (diff, align, montage) for loading PNGs and measuring how far apart
// two frames are.
package imgutil

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

// LoadPNG decodes a PNG file, exiting the program via log.Fatalf on error
// (all of these tools are short-lived CLI commands where that's the right
// failure mode).
func LoadPNG(path string) image.Image {
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

// SavePNG encodes img as a PNG file, exiting the program via log.Fatalf on
// error.
func SavePNG(path string, img image.Image) {
	o, err := os.Create(path)
	if err != nil {
		log.Fatalf("Can't create %s: %v", path, err)
	}
	defer o.Close()
	if err := png.Encode(o, img); err != nil {
		log.Fatalf("Can't encode %s: %v", path, err)
	}
}

// ColorDistance returns the Euclidean distance between two colors in 8-bit
// RGB space: 0 == identical, ~441 == max possible distance (black vs white).
func ColorDistance(a, b color.Color) float64 {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	dr := float64(ar>>8) - float64(br>>8)
	dg := float64(ag>>8) - float64(bg>>8)
	db := float64(ab>>8) - float64(bb>>8)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// OverlapSize returns the largest region common to both images, anchored at
// each image's own origin. Captures being compared aren't always identical
// sizes (e.g. Stella auto-detects visible frame height per ROM, this
// implementation always uses the nominal NTSC/PAL height), so callers
// should compare only this region rather than assuming equal dimensions.
func OverlapSize(a, b image.Image) (w, h int) {
	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	bw, bh := b.Bounds().Dx(), b.Bounds().Dy()
	w, h = aw, ah
	if bw < w {
		w = bw
	}
	if bh < h {
		h = bh
	}
	return w, h
}

// RMSE computes the root-mean-square color distance between two images over
// their overlapping region, along with the number of pixels compared and how
// many differed at all.
func RMSE(a, b image.Image) (rmse float64, total, differing int) {
	w, h := OverlapSize(a, b)
	amin, bmin := a.Bounds().Min, b.Bounds().Min
	var sumSq float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dist := ColorDistance(a.At(amin.X+x, amin.Y+y), b.At(bmin.X+x, bmin.Y+y))
			total++
			if dist > 0 {
				differing++
			}
			sumSq += dist * dist
		}
	}
	if total == 0 {
		return 0, 0, 0
	}
	return math.Sqrt(sumSq / float64(total)), total, differing
}

// Signature reduces an image to a small grid of average-luminance values,
// for cheaply comparing many frame pairs (e.g. searching for the best
// alignment offset between two capture sequences) without doing a full
// per-pixel diff on every candidate pair. Luminance rather than full color
// is deliberate: it still tracks scene/shape changes but is robust to the
// kind of small per-implementation palette differences that would otherwise
// swamp a real alignment search.
func Signature(img image.Image, gx, gy int) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	sig := make([]float64, gx*gy)
	if w == 0 || h == 0 {
		return sig
	}
	counts := make([]int, gx*gy)
	for y := 0; y < h; y++ {
		cy := y * gy / h
		for x := 0; x < w; x++ {
			cx := x * gx / w
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lum := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			idx := cy*gx + cx
			sig[idx] += lum
			counts[idx]++
		}
	}
	for i, c := range counts {
		if c > 0 {
			sig[i] /= float64(c)
		}
	}
	return sig
}

// SignatureDistance returns the RMS difference between two signatures
// produced by Signature.
func SignatureDistance(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.Inf(1)
	}
	var sumSq float64
	for i := range a {
		d := a[i] - b[i]
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(a)))
}

// HeatColor maps a color distance (0-441) onto a black -> red -> yellow
// heatmap color, for visualizing where two frames diverge.
func HeatColor(dist float64) color.Color {
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
