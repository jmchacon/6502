package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"sync"
	"time"

	"github.com/jmchacon/6502/atari2600"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/tia"
	"github.com/veandco/go-sdl2/sdl"
)

var (
	debug       = flag.Bool("debug", false, "If true will emit full CPU/TIA/PIA debugging while running")
	cart        = flag.String("cart", "", "Path to cart image to load")
	scale       = flag.Int("scale", 1, "Scale factor to render screen")
	port        = flag.Int("port", 6060, "Port to run HTTP server for pprof")
	advance     = flag.Bool("advance", true, "If true the game select will be toggled to advance the play screen")
	advanceRate = flag.Int("advance_rate", 60, "After how many frames to toggle the game select")
	mode        = flag.String("mode", "NTSC", "Either NTSC, PAL or SECAM (case insensitive) to determine video mode")
)

type swtch struct {
	b bool
}

func (s *swtch) Input() bool {
	return s.b
}

type fastImage struct {
	surface *sdl.Surface
	data    []byte
}

func (f *fastImage) Set(x, y int, c color.Color) {
	// Calculate and poke the values in directly which avoids a call to Convert
	// that Surface.Set does which chews measurable CPU because of GC'ing color.Color
	i := int32(y)*f.surface.Pitch + int32(x)*int32(f.surface.Format.BytesPerPixel)
	// These may come in either way so type switch accordingly.
	if _, ok := c.(color.RGBA); ok {
		f.data[i+0] = c.(color.RGBA).R
		f.data[i+1] = c.(color.RGBA).G
		f.data[i+2] = c.(color.RGBA).B
		f.data[i+3] = c.(color.RGBA).A
	} else {
		f.data[i+0] = c.(*color.RGBA).R
		f.data[i+1] = c.(*color.RGBA).G
		f.data[i+2] = c.(*color.RGBA).B
		f.data[i+3] = c.(*color.RGBA).A
	}
}

func (f *fastImage) ColorModel() color.Model {
	return f.surface.ColorModel()
}

func (f *fastImage) Bounds() image.Rectangle {
	return f.surface.Bounds()
}

func (f *fastImage) At(x, y int) color.Color {
	return f.surface.At(x, y)
}

func main() {
	flag.Parse()

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

	go func() {
		log.Println(http.ListenAndServe(fmt.Sprintf("localhost:%d", *port), nil))
	}()
	var window *sdl.Window
	fi := &fastImage{}

	sdl.Main(func() {
		var wg sync.WaitGroup
		wg.Add(1)
		sdl.Do(func() {
			if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
				log.Fatalf("Can't init SDL: %v", err)
			}

			var err error
			window, err = sdl.CreateWindow("test", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, int32(w**scale), int32(h**scale), sdl.WINDOW_SHOWN)
			if err != nil {
				log.Fatalf("Can't create window: %v", err)
			}
			fi.surface, err = window.GetSurface()
			if err != nil {
				log.Fatalf("Can't get window surface: %v", err)
			}
			fi.data = fi.surface.Pixels()
			wg.Done()
		})

		game := &swtch{false}

		// Luckily carts are so tiny by modern standards we just read it in.
		// TODO(jchacon): Add a sanity check here for size.
		rom, err := ioutil.ReadFile(*cart)
		if err != nil {
			log.Fatalf("Can't load rom: %v from path: %s", err, *cart)
		}
		wg.Wait()
		defer func() {
			window.Destroy()
			sdl.Quit()
		}()

		now := time.Now()
		var tot, cnt time.Duration
		a, err := atari2600.Init(&atari2600.VCSDef{
			Mode:        tiaMode,
			Difficulty:  [2]io.PortIn1{&swtch{false}, &swtch{false}},
			ColorBW:     &swtch{true},
			GameSelect:  game,
			Reset:       &swtch{false},
			Image:       fi,
			ScaleFactor: *scale,
			FrameDone: func(draw.Image) {
				sdl.Do(func() {
					df := time.Now().Sub(now)
					tot += df
					cnt++
					if *advance && int(cnt)%*advanceRate == 0 {
						game.b = !game.b
					}
					fmt.Printf("Frame took %s average %s\n", df, tot/cnt)
					window.UpdateSurface()
					now = time.Now()
				})
			},
			Rom:   []uint8(rom),
			Debug: *debug,
		})
		if err != nil {
			log.Fatalf("Can't init VCS: %v", err)
		}
		for {
			if err := a.Tick(); err != nil {
				log.Fatalf("Tick error: %v", err)
			}
		}
	})
}
