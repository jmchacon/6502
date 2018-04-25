package tia

import (
	"flag"
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
	testImageDir    = flag.String("test_image_dir", "", "If set will generate images from tests to this directory")
	testImageScaler = flag.Float64("test_image_scaler", 1.0, "The amount to rescale the output PNGs")
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
	done := false
	f := func(i *image.NRGBA) {
		if *testImageDir != "" {
			o, err := os.Create(filepath.Join(*testImageDir, "TestBackground.png"))
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
		done = true
	}
	ta, err := Init(&TIADef{
		Mode:      TIA_MODE_NTSC,
		FrameDone: f,
	})
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	const redIndex = 0x1B
	// Set background to red
	ta.Write(COLUBK, redIndex)
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
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}
	// Create a canonical image to compare against.
	want := image.NewNRGBA(image.Rect(0, 0, kNTSCWidth, kNTSCHeight))
	// First 40 lines should be black
	for h := 0; h < kNTSCTopBlank; h++ {
		for w := 0; w < kNTSCWidth; w++ {
			want.Set(w, h, kBlack)
		}
	}
	// Next N are black hblank but red otherwise.
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		for w := 0; w < kHblank; w++ {
			want.Set(w, h, kBlack)
		}
		for w := kHblank; w < kNTSCWidth; w++ {
			want.Set(w, h, kNTSC[redIndex])
		}
	}
	// Last N are black again.
	for h := kNTSCOverscanStart; h < kNTSCHeight; h++ {
		for w := 0; w < kNTSCWidth; w++ {
			want.Set(w, h, kBlack)
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		t.Errorf("Pictures differ: %v", diff)
	}
}
