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
	"sync"
	"time"

	"github.com/jmchacon/6502/atari2600"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/tia"
	"github.com/veandco/go-sdl2/sdl"
)

var (
	debug = flag.Bool("debug", false, "If true will emit full CPU/TIA/PIA debugging while running")
	cart  = flag.String("cart", "", "Path to cart image to load")
	scale = flag.Int("scale", 1, "Scale factor to render screen")
	port  = flag.Int("port", 6060, "Port to run HTTP server for pprof")
)

type swtch struct {
	b bool
}

func (s *swtch) Input() bool {
	return s.b
}

type toggle struct {
	b          bool
	cnt        int
	resetTrue  int
	resetFalse int
	total      int
	stop       int
}

func (s *toggle) Input() bool {
	if s.total > s.stop {
		return s.b
	}
	s.cnt--
	if s.cnt == 0 {
		s.cnt = s.resetFalse
		if s.b {
			s.cnt = s.resetTrue
		}
		s.b = !s.b
		s.total++
	}
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
			window, err = sdl.CreateWindow("test", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, int32(tia.NTSCWidth**scale), int32(tia.NTSCHeight**scale), sdl.WINDOW_SHOWN)
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

		game := &toggle{
			cnt:        1000,
			resetTrue:  60,
			resetFalse: 1800,
			stop:       16,
		}

		// Luckily carts are so tiny by modern standards we just read it in.
		// TODO(jchacon): Add a sanity check here for size.
		rom, err := ioutil.ReadFile(*cart)
		if err != nil {
			log.Fatalf("Can't load rom: %v from path: %s", err, cart)
		}
		wg.Wait()
		defer func() {
			window.Destroy()
			sdl.Quit()
		}()

		now := time.Now()
		var tot, cnt time.Duration
		a, err := atari2600.Init(&atari2600.VCSDef{
			Mode:        tia.TIA_MODE_NTSC,
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
