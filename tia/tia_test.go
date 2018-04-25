package tia

import (
	"flag"
	"fmt"
	"image"
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
	width    int
	height   int
	vsync    int
	vblank   int
	overscan int
}

func runAFrame(t *testing.T, ta *TIA, frame frameSpec) {
	now := time.Now()
	// Run tick enough times for a frame.
	for i := 0; i < frame.width*frame.height; i++ {
		if err := ta.Tick(); err != nil {
			t.Fatalf("Error on tick: %v", err)
		}
		// Turn off VSYNC after it's done.
		if i > frame.vsync*frame.width && ta.vsync {
			ta.Write(VSYNC, 0x00)
		}
		// Turn off VBLANK after it's done.
		if i > frame.vblank*frame.width && ta.vblank {
			ta.Write(VBLANK, 0x00)
		}
		// Turn VBLANK back on at the bottom.
		if i > frame.overscan*frame.width {
			ta.Write(VBLANK, kMASK_VBL_VBLANK)
		}
	}
	// Now trigger a VSYNC which should trigger callback.
	t.Logf("Total frame time: %s", time.Now().Sub(now))
	ta.Write(VSYNC, 0xFF)
}

func TestBackground(t *testing.T) {
	// There are 128 background colors. Let's do them all
	for cnt := 0; cnt < len(kNTSC); cnt++ {

		done := false
		f := func(i *image.NRGBA) {
			if *testImageDir != "" {
				for m := 0; m < *testFrameMultiplier; m++ {
					o, err := os.Create(filepath.Join(*testImageDir, fmt.Sprintf("TestBackground%.6d.png", (cnt**testFrameMultiplier)+m)))
					if err != nil {
						t.Fatal(err)
					}
					if *testImageScaler != 1.0 {
						d := image.NewNRGBA(image.Rect(0, 0, int(float64(i.Bounds().Max.X)**testImageScaler), int(float64(i.Bounds().Max.Y)**testImageScaler)))
						draw.NearestNeighbor.Scale(d, d.Bounds(), i, i.Bounds(), draw.Over, nil)
						i = d
					}
					defer o.Close()
					if err := png.Encode(o, i); err != nil {
						t.Fatal(err)
					}
				}
			}
			done = true
		}
		ta, err := Init(&TIADef{
			Mode:      TIA_MODE_NTSC,
			FrameDone: f,
		})
		if err != nil {
			t.Fatalf("Color %d: Can't Init: %v", cnt, err)
		}

		// Set background to current color
		ta.Write(COLUBK, uint8(cnt))
		// Turn on VBLANK and VSYNC
		ta.Write(VBLANK, kMASK_VBL_VBLANK)
		ta.Write(VSYNC, 0xFF)
		runAFrame(t, ta, frameSpec{
			width:    kNTSCWidth,
			height:   kNTSCHeight,
			vsync:    kVSYNCLines,
			vblank:   kNTSCTopBlank,
			overscan: kNTSCOverscanStart,
		})
		if !done {
			t.Fatalf("Color %d: Didn't trigger a VSYNC?\n%v", cnt, spew.Sdump(ta))
		}
		// Create a canonical image to compare against.
		want := image.NewNRGBA(image.Rect(0, 0, kNTSCWidth, kNTSCHeight))
		// First 40 lines should be black
		for h := 0; h < kNTSCTopBlank; h++ {
			for w := 0; w < kNTSCWidth; w++ {
				want.Set(w, h, kBlack)
			}
		}
		// Next N are black hblank but color otherwise.
		for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
			for w := 0; w < kHblank; w++ {
				want.Set(w, h, kBlack)
			}
			for w := kHblank; w < kNTSCWidth; w++ {
				want.Set(w, h, kNTSC[cnt])
			}
		}
		// Last N are black again.
		for h := kNTSCOverscanStart; h < kNTSCHeight; h++ {
			for w := 0; w < kNTSCWidth; w++ {
				want.Set(w, h, kBlack)
			}
		}
		if diff := deep.Equal(ta.picture, want); diff != nil {
			t.Errorf("Color %d: Pictures differ: %v", cnt, diff)
		}
	}
}
