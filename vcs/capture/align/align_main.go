// Command align finds the best frame-offset alignment between two capture
// sequences (e.g. this repo's vcs/capture -outdir output and
// vcs/capture/stella_capture.sh's "seq" mode output) and reports how well
// they match once aligned.
//
// Both capture tools produce frame-ordered sequences, but each starts
// counting from its own "frame 1" (ROM launch for vcs/capture, the moment
// Stella's continuous snapshot mode was toggled on for stella_capture.sh),
// so a fixed constant offset between the two is expected. align searches
// for that offset by comparing cheap per-frame luminance signatures (robust
// to the kind of small palette differences between implementations that
// would otherwise swamp a real alignment search) rather than assuming the
// sequences already line up.
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/jmchacon/6502/vcs/capture/internal/imgutil"
)

var (
	maxOffset = flag.Int("max-offset", 60, "Search offsets in [-max-offset, max-offset] frames")
	out       = flag.String("out", "", "Optional path to write a heatmap PNG for the best-aligned frame pair")
	sigGridX  = flag.Int("sig-grid-x", 32, "Signature grid width used for the cheap alignment search")
	sigGridY  = flag.Int("sig-grid-y", 24, "Signature grid height used for the cheap alignment search")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		log.Fatal("usage: align [-max-offset N] [-out heatmap.png] <dirA> <dirB>")
	}

	aFiles := sortedPNGs(args[0])
	bFiles := sortedPNGs(args[1])
	if len(aFiles) == 0 || len(bFiles) == 0 {
		log.Fatalf("need PNGs in both directories, got %d and %d", len(aFiles), len(bFiles))
	}

	var aSigs, bSigs [][]float64
	for _, f := range aFiles {
		aSigs = append(aSigs, imgutil.Signature(imgutil.LoadPNG(f), *sigGridX, *sigGridY))
	}
	for _, f := range bFiles {
		bSigs = append(bSigs, imgutil.Signature(imgutil.LoadPNG(f), *sigGridX, *sigGridY))
	}

	bestOffset, bestScore := 0, -1.0
	for offset := -*maxOffset; offset <= *maxOffset; offset++ {
		score, n := 0.0, 0
		for i, aSig := range aSigs {
			j := i + offset
			if j < 0 || j >= len(bSigs) {
				continue
			}
			score += imgutil.SignatureDistance(aSig, bSigs[j])
			n++
		}
		if n == 0 {
			continue
		}
		score /= float64(n)
		if bestScore < 0 || score < bestScore {
			bestScore, bestOffset = score, offset
		}
	}
	if bestScore < 0 {
		log.Fatal("no overlapping frames found within -max-offset; try a larger -max-offset")
	}

	fmt.Printf("best offset: B[i] matches A[i%+d]  (mean luminance-signature distance %.2f)\n", bestOffset, bestScore)

	// Report full-resolution RMSE across every aligned pair at that offset,
	// and remember the single closest-matching pair for an optional heatmap.
	var (
		totalRMSE    float64
		pairs        int
		bestPairRMSE = -1.0
		bestPairAIdx int
		bestPairBIdx int
	)
	for i := range aFiles {
		j := i + bestOffset
		if j < 0 || j >= len(bFiles) {
			continue
		}
		a := imgutil.LoadPNG(aFiles[i])
		b := imgutil.LoadPNG(bFiles[j])
		rmse, _, _ := imgutil.RMSE(a, b)
		totalRMSE += rmse
		pairs++
		if bestPairRMSE < 0 || rmse < bestPairRMSE {
			bestPairRMSE, bestPairAIdx, bestPairBIdx = rmse, i, j
		}
	}
	fmt.Printf("aligned pairs compared: %d, mean RMSE color distance: %.2f (scale 0-441)\n", pairs, totalRMSE/float64(pairs))
	fmt.Printf("closest pair: %s <-> %s (RMSE %.2f)\n", aFiles[bestPairAIdx], bFiles[bestPairBIdx], bestPairRMSE)

	if *out != "" {
		a := imgutil.LoadPNG(aFiles[bestPairAIdx])
		b := imgutil.LoadPNG(bFiles[bestPairBIdx])
		w, h := imgutil.OverlapSize(a, b)
		heatmap := image.NewNRGBA(image.Rect(0, 0, w, h))
		amin, bmin := a.Bounds().Min, b.Bounds().Min
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dist := imgutil.ColorDistance(a.At(amin.X+x, amin.Y+y), b.At(bmin.X+x, bmin.Y+y))
				heatmap.Set(x, y, imgutil.HeatColor(dist))
			}
		}
		imgutil.SavePNG(*out, heatmap)
		fmt.Printf("heatmap for closest pair written to %s\n", *out)
	}
}

func sortedPNGs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("Can't read %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files
}
