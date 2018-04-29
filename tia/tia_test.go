package tia

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-test/deep"
	"golang.org/x/image/draw"
)

var (
	testImageDir        = flag.String("test_image_dir", "", "If set will generate images from tests to this directory")
	testImageScaler     = flag.Float64("test_image_scaler", 1.0, "The amount to rescale the output PNGs")
	testFrameMultiplier = flag.Int("test_frame_multiplier", 1, "The number of frames to multiply for output to slow down final frame rates. i.e. 30 == 2 fps for viewing")
)

type frameSpec struct {
	width     int
	height    int
	vsync     int
	vblank    int
	overscan  int
	callbacks map[int]func(int) // Optional mapping of scan lines to callbacks on each line (setting player/PF/etc registers possibly different).
}

func runAFrame(t *testing.T, ta *TIA, frame frameSpec) {
	now := time.Now()
	// Run tick enough times for a frame.
	for i := 0; i < frame.height; i++ {
		if cb := frame.callbacks[i]; cb != nil {
			cb(i)
		}
		// Turn off VSYNC after it's done.
		if i >= frame.vsync && ta.vsync {
			ta.Write(VSYNC, 0x00)
		}
		// Turn off VBLANK after it's done.
		if i >= frame.vblank && ta.vblank {
			ta.Write(VBLANK, 0x00)
		}
		// Turn VBLANK back on at the bottom.
		if i >= frame.overscan {
			ta.Write(VBLANK, kMASK_VBL_VBLANK)
		}
		for j := 0; j < frame.width; j++ {
			if err := ta.Tick(); err != nil {
				t.Fatalf("Error on tick: %v", err)
			}
		}
	}
	// Now trigger a VSYNC which should trigger callback.
	t.Logf("Total frame time: %s", time.Now().Sub(now))
	ta.Write(VSYNC, 0xFF)
}

// curry a bunch of args and return a valid image callback for the TIA on frame end.
func generateImage(t *testing.T, name string, cnt *int, done *bool) func(i *image.NRGBA) {
	return func(i *image.NRGBA) {
		if *testImageDir != "" {
			n := i
			if *testImageScaler != 1.0 {
				d := image.NewNRGBA(image.Rect(0, 0, int(float64(i.Bounds().Max.X)**testImageScaler), int(float64(i.Bounds().Max.Y)**testImageScaler)))
				draw.NearestNeighbor.Scale(d, d.Bounds(), i, i.Bounds(), draw.Over, nil)
				n = d
			}
			for m := 0; m < *testFrameMultiplier; m++ {
				o, err := os.Create(filepath.Join(*testImageDir, fmt.Sprintf("%s%.6d.png", name, (*cnt**testFrameMultiplier)+m)))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				defer o.Close()
				if err := png.Encode(o, n); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			}
			// Without this we generate too much garbage and OOM during a test.
			n = nil
		}
		*done = true
	}
}

func TestBackground(t *testing.T) {
	tests := []struct {
		name     string
		mode     TIAMode
		colors   *[128]*color.NRGBA
		width    int
		height   int
		vsync    int
		vblank   int
		overscan int
	}{
		{
			name:     "NTSC",
			mode:     TIA_MODE_NTSC,
			colors:   &kNTSC,
			width:    kNTSCWidth,
			height:   kNTSCHeight,
			vsync:    kVSYNCLines,
			vblank:   kNTSCTopBlank,
			overscan: kNTSCOverscanStart,
		},
		{
			name:     "PAL",
			mode:     TIA_MODE_PAL,
			colors:   &kPAL,
			width:    kPALWidth,
			height:   kPALHeight,
			vsync:    kVSYNCLines,
			vblank:   kPALTopBlank,
			overscan: kPALOverscanStart,
		},
		{
			name:     "SECAM",
			mode:     TIA_MODE_SECAM,
			colors:   &kSECAM,
			width:    kPALWidth,
			height:   kPALHeight,
			vsync:    kVSYNCLines,
			vblank:   kPALTopBlank,
			overscan: kPALOverscanStart,
		},
	}

	for _, test := range tests {
		// There are a lot of background colors. Let's do them all
		for cnt := 0; cnt < len(*test.colors); cnt++ {
			done := false
			ta, err := Init(&TIADef{
				Mode:      test.mode,
				FrameDone: generateImage(t, t.Name()+test.name, &cnt, &done),
			})
			if err != nil {
				t.Fatalf("%s: Color %d: Can't Init: %v", test.name, cnt, err)
			}

			// Set background to current color (and left shift it to act as a color value).
			ta.Write(COLUBK, uint8(cnt)<<1)
			// Turn on VBLANK and VSYNC
			ta.Write(VBLANK, kMASK_VBL_VBLANK)
			ta.Write(VSYNC, 0xFF)
			runAFrame(t, ta, frameSpec{
				width:     test.width,
				height:    test.height,
				vsync:     test.vsync,
				vblank:    test.vblank,
				overscan:  test.overscan,
				callbacks: make(map[int]func(int)),
			})
			if !done {
				t.Fatalf("%s: Color %d: Didn't trigger a VSYNC?\n%v", test.name, cnt, spew.Sdump(ta))
			}
			// Create a canonical image to compare against.
			want := image.NewNRGBA(image.Rect(0, 0, test.width, test.height))
			// First 40 lines should be black
			for h := 0; h < test.vblank; h++ {
				for w := 0; w < test.width; w++ {
					want.Set(w, h, kBlack)
				}
			}
			// Next N are black hblank but color otherwise.
			for h := test.vblank; h < test.overscan; h++ {
				for w := 0; w < kHblank; w++ {
					want.Set(w, h, kBlack)
				}
				for w := kHblank; w < test.width; w++ {
					want.Set(w, h, test.colors[cnt])
				}
			}
			// Last N are black again.
			for h := test.overscan; h < test.height; h++ {
				for w := 0; w < kNTSCWidth; w++ {
					want.Set(w, h, kBlack)
				}
			}
			if diff := deep.Equal(ta.picture, want); diff != nil {
				t.Errorf("%s: Color %d: Pictures differ: %v", test.name, cnt, diff)
			}
		}
	}
}

func TestPlayfield(t *testing.T) {
	// Test regular playfield color (draw a box 3 lines on top and bottom)
	// Test reflection (double box with no right side)
	// Test score (box but player0/1 colors instead for each side)
	done := false
	cnt := 0
	ta, err := Init(&TIADef{
		Mode:      TIA_MODE_NTSC,
		FrameDone: generateImage(t, t.Name()+"Regular", &cnt, &done),
	})
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	// Set background to yellow - 0x0F (and left shift it to act as a color value).
	ta.Write(COLUBK, uint8(0x0F)<<1)
	// Set player0 to red (0x1B) and player1 to blue (0x42) and again left shift.
	ta.Write(COLUP0, uint8(0x1B)<<1)
	ta.Write(COLUP1, uint8(0x42)<<1)
	// Finally set playfield to green (0x5A) and again left shift.
	ta.Write(COLUPF, uint8(0x5A)<<1)

	// Write all ones into the 3 PF registers so we generate a bar.
	ta.Write(PF0, 0xFF)
	ta.Write(PF1, 0xFF)
	ta.Write(PF2, 0xFF)
}
