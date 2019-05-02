package main

import (
	"flag"
	"image/draw"
	"io/ioutil"
	"log"

	"github.com/jmchacon/6502/atari2600"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/tia"
	"github.com/veandco/go-sdl2/sdl"
)

var (
	debug = flag.Bool("debug", false, "If true will emit full CPU/TIA/PIA debugging while running")
	cart  = flag.String("cart", "", "Path to cart image to load")
)

type swtch struct {
	b bool
}

func (s *swtch) Input() bool {
	return s.b
}

type swap struct {
	b     bool
	cnt   int
	reset int
}

func (s *swap) Input() bool {
	s.cnt--
	if s.cnt == 0 {
		s.b = !s.b
		s.cnt = s.reset
	}
	return s.b
}

func main() {
	flag.Parse()
	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		log.Fatalf("Can't init SDL: %v", err)
	}
	defer sdl.Quit()

	window, err := sdl.CreateWindow("test", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, tia.NTSCWidth, tia.NTSCHeight, sdl.WINDOW_SHOWN)
	if err != nil {
		log.Fatalf("Can't create window: %v", err)
	}
	defer window.Destroy()

	surface, err := window.GetSurface()
	if err != nil {
		log.Fatalf("Can't get window surface: %v", err)
	}

	diff := &swtch{false}
	game := &swtch{false}
	color := &swtch{true}

	// Luckily carts are so tiny by modern standards we just read it in.
	// TODO(jchacon): Add a sanity check here for size.
	rom, err := ioutil.ReadFile(*cart)
	if err != nil {
		log.Fatalf("Can't load rom: %v from path: %s", err, cart)
	}

	a, err := atari2600.Init(&atari2600.VCSDef{
		Mode:       tia.TIA_MODE_NTSC,
		Difficulty: [2]io.PortIn1{diff, diff},
		ColorBW:    color,
		GameSelect: game,
		Reset:      color,
		Image:      surface,
		FrameDone:  func(draw.Image) { window.UpdateSurface() },
		Rom:        []uint8(rom),
		Debug:      *debug,
	})
	if err != nil {
		log.Fatalf("Can't init VCS: %v", err)
	}
	for {
		if err := a.Tick(); err != nil {
			log.Fatalf("Tick error: %v", err)
		}
	}
}
