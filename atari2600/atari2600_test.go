package atari2600

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/tia"
)

var (
	testImageDir = flag.String("test_image_dir", "", "If set will generate images from tests to this directory")
	testDebug    = flag.Bool("test_debug", false, "If true will emit full CPU/TIA/PIA debugging while running")
)

const testDir = "../testdata"

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

func TestCarts(t *testing.T) {
	diff := &swtch{false}
	game := &swtch{false}
	color := &swtch{true}
	done := false

	tests := []struct {
		name     string
		filename string
	}{
		// NOTE: to run these tests one must get legit cart images for the below
		//       and put them in testDir manually (they aren't checked in).
		{
			name:     "Combat",
			filename: "combat.bin",
		},
		{
			name:     "SpaceInvaders",
			filename: "spcinvad.bin",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(testDir, test.filename)
			rom, err := ioutil.ReadFile(file)
			if err != nil {
				t.Fatalf("%s: can't read %s: %v", test.name, file, err)
			}

			a, err := Init(&VCSDef{
				Mode:       tia.TIA_MODE_NTSC,
				Difficulty: [2]io.PortIn1{diff, diff},
				ColorBW:    color,
				GameSelect: game,
				Reset:      color,
				FrameDone:  generateImage(t, test.name, 3600, &done),
				Rom:        []uint8(rom),
				Debug:      *testDebug,
			})
			if err != nil {
				t.Fatalf("%s: can't init VCS: %v", test.name, err)
			}
			for {
				if err := a.Tick(); err != nil {
					t.Fatalf("Tick error: %v", err)
				}
				if done {
					break
				}
			}
		})
	}
}

// curry some things and return a valid image callback for the TIA on frame end.
func generateImage(t *testing.T, name string, max int, done *bool) func(i *image.NRGBA) {
	cnt := 0
	now := time.Now()
	return func(i *image.NRGBA) {
		df := time.Now().Sub(now)
		bad := ""
		if df > 16600*time.Microsecond {
			bad = "BAD"
		}
		t.Logf("Frame: %d took %s %s\n", cnt, time.Now().Sub(now), bad)
		cnt++
		o, err := os.Create(filepath.Join(*testImageDir, fmt.Sprintf("%s%.6d.png", name, cnt)))
		if err != nil {
			t.Fatalf("Can't open output file %s%.6d.png: %v", t.Name(), cnt, err)
		}
		defer o.Close()
		if err := png.Encode(o, i); err != nil {
			t.Fatalf("Can't PNG encode for file %s%.6d.png: %v", t.Name(), cnt, err)
		}
		now = time.Now()
		if cnt == max {
			*done = true
		}
	}
}
