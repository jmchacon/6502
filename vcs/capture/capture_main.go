// Command capture runs a headless Atari 2600 cart for a given number of
// frames and dumps rendered frame(s) as PNG. It uses no SDL/display and is
// meant for automated screenshot comparisons (e.g. against other emulators).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmchacon/6502/atari2600"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/tia"
)

// Standard visible-picture framing, matching what a TV (and other emulators
// like Stella) actually display: horizontal blanking and vertical
// VSYNC/VBLANK/overscan are stripped out. Horizontal color clocks are
// doubled to correct the pixel aspect ratio, matching Stella's default
// snapshot framing.
const (
	kHblank    = 68
	kPicWidth  = 160
	kNTSCTop   = 40 // VSYNC(3) + VBLANK(37)
	kNTSCLines = 192
	kPALTop    = 48 // VSYNC(3) + VBLANK(45)
	kPALLines  = 228
)

var (
	cart        = flag.String("cart", "", "Path to cart image to load")
	mode        = flag.String("mode", "NTSC", "Either NTSC, PAL or SECAM (case insensitive) to determine video mode")
	frames      = flag.Int("frames", 120, "Number of frames to run before capturing")
	out         = flag.String("out", "", "Output PNG path for the frame at the end of the run. Either this or -outdir must be set.")
	outdir      = flag.String("outdir", "", "If set, write every captured frame (per -interval) as a numbered PNG into this directory instead of a single -out file")
	interval    = flag.Int("interval", 1, "When using -outdir, only save every Nth frame")
	crop        = flag.Bool("crop", true, "If true, crop to the visible picture area and double horizontal resolution to correct pixel aspect ratio (matches Stella's default snapshot framing). If false, dumps the full raw TIA frame including blanking/overscan.")
	advance     = flag.Bool("advance", true, "If true the game select will be toggled periodically to advance attract/title screens")
	advanceRate = flag.Int("advance_rate", 60, "After how many frames to toggle the game select when -advance is true")
)

type swtch struct {
	b bool
}

func (s *swtch) Input() bool {
	return s.b
}

func main() {
	flag.Parse()

	if *cart == "" {
		log.Fatal("-cart is required")
	}
	if *out == "" && *outdir == "" {
		log.Fatal("one of -out or -outdir is required")
	}

	vidMode := strings.ToUpper(*mode)
	var tiaMode tia.TIAMode
	var h, w int
	switch vidMode {
	case "NTSC":
		tiaMode = tia.TIA_MODE_NTSC
		h = tia.NTSCHeight
		w = tia.NTSCWidth
	case "PAL":
		tiaMode = tia.TIA_MODE_PAL
		h = tia.PALHeight
		w = tia.PALWidth
	case "SECAM":
		tiaMode = tia.TIA_MODE_SECAM
		h = tia.SECAMHeight
		w = tia.SECAMWidth
	default:
		log.Fatalf("Invalid video mode %q - Must be NTSC, PAL or SECAM\n", vidMode)
	}

	picTop, picLines := kNTSCTop, kNTSCLines
	if vidMode != "NTSC" {
		picTop, picLines = kPALTop, kPALLines
	}

	rom, err := ioutil.ReadFile(*cart)
	if err != nil {
		log.Fatalf("Can't load rom: %v from path: %s", err, *cart)
	}

	if *outdir != "" {
		if err := os.MkdirAll(*outdir, 0o755); err != nil {
			log.Fatalf("Can't create outdir %s: %v", *outdir, err)
		}
	}

	game := &swtch{false}
	image := image.NewNRGBA(image.Rect(0, 0, w, h))

	cnt := 0
	base := strings.TrimSuffix(filepath.Base(*cart), filepath.Ext(*cart))
	done := false

	frameDone := func(i draw.Image) {
		cnt++
		if *advance && cnt%*advanceRate == 0 {
			game.b = !game.b
		}
		if *outdir != "" && cnt%*interval == 0 {
			p := filepath.Join(*outdir, fmt.Sprintf("%s%.6d.png", base, cnt))
			savePNG(p, i, picTop, picLines)
		}
		if cnt >= *frames {
			if *out != "" {
				savePNG(*out, i, picTop, picLines)
			}
			done = true
		}
	}

	a, err := atari2600.Init(&atari2600.VCSDef{
		Mode:       tiaMode,
		Difficulty: [2]io.PortIn1{&swtch{false}, &swtch{false}},
		Joysticks: [2]*atari2600.Joystick{
			{
				Up:     &swtch{true},
				Down:   &swtch{true},
				Left:   &swtch{true},
				Right:  &swtch{true},
				Button: &swtch{true},
			},
			{
				Up:     &swtch{true},
				Down:   &swtch{true},
				Left:   &swtch{true},
				Right:  &swtch{true},
				Button: &swtch{true},
			},
		},
		ColorBW:    &swtch{true},
		GameSelect: game,
		Reset:      &swtch{false},
		Image:      image,
		FrameDone:  frameDone,
		Rom:        []uint8(rom),
	})
	if err != nil {
		log.Fatalf("Can't init VCS: %v", err)
	}

	for !done {
		if err := a.Tick(); err != nil {
			log.Fatalf("Tick error: %v", err)
		}
	}
}

func savePNG(path string, i draw.Image, picTop, picLines int) {
	img := image.Image(i)
	if *crop {
		img = cropAndScale(i, picTop, picLines)
	}
	o, err := os.Create(path)
	if err != nil {
		log.Fatalf("Can't open output file %s: %v", path, err)
	}
	defer o.Close()
	if err := png.Encode(o, img); err != nil {
		log.Fatalf("Can't PNG encode for file %s: %v", path, err)
	}
}

// cropAndScale extracts the visible picture area from a raw TIA frame and
// doubles it horizontally (nearest neighbor) to correct the pixel aspect
// ratio, so output roughly matches Stella's default snapshot framing.
func cropAndScale(i draw.Image, picTop, picLines int) image.Image {
	out := image.NewNRGBA(image.Rect(0, 0, kPicWidth*2, picLines))
	for y := 0; y < picLines; y++ {
		for x := 0; x < kPicWidth; x++ {
			c := i.At(kHblank+x, picTop+y)
			out.Set(x*2, y, c)
			out.Set(x*2+1, y, c)
		}
	}
	return out
}
