// Command montage places two or more PNG images side by side into a single
// PNG for quick visual comparison (e.g. this Go VCS implementation's output
// next to Stella's for the same cart).
package main

import (
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
)

var out = flag.String("out", "montage.png", "Output PNG path")

const (
	gap    = 8
	border = 1
)

func main() {
	flag.Parse()
	paths := flag.Args()
	if len(paths) < 2 {
		log.Fatal("usage: montage -out montage.png <img1.png> <img2.png> [more...]")
	}

	var imgs []image.Image
	maxH := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			log.Fatalf("Can't open %s: %v", p, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			log.Fatalf("Can't decode %s: %v", p, err)
		}
		imgs = append(imgs, img)
		if h := img.Bounds().Dy(); h > maxH {
			maxH = h
		}
	}

	totalW := border
	for _, img := range imgs {
		totalW += img.Bounds().Dx() + gap + border
	}
	out2 := image.NewNRGBA(image.Rect(0, 0, totalW, maxH+2*border))
	// Fill background with a mid gray so both light and dark frames are visible.
	gray := image.NewUniform(color.NRGBA{R: 96, G: 96, B: 96, A: 255})
	draw.Draw(out2, out2.Bounds(), gray, image.Point{}, draw.Src)

	x := border
	for _, img := range imgs {
		r := image.Rect(x, border, x+img.Bounds().Dx(), border+img.Bounds().Dy())
		draw.Draw(out2, r, img, img.Bounds().Min, draw.Src)
		x += img.Bounds().Dx() + gap
	}

	o, err := os.Create(*out)
	if err != nil {
		log.Fatalf("Can't create %s: %v", *out, err)
	}
	defer o.Close()
	if err := png.Encode(o, out2); err != nil {
		log.Fatalf("Can't encode %s: %v", *out, err)
	}
}
