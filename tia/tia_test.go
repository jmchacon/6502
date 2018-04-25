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
)

var (
	testImageDir = flag.String("test_image_dir", "", "If set will generate images from tests to this directory")
)

func TestBackground(t *testing.T) {
	done := false
	f := func(i *image.NRGBA) {
		if *testImageDir != "" {
			o, err := os.Create(filepath.Join(*testImageDir, "TestBackground.png"))
			if err != nil {
				t.Fatal(err)
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
	now := time.Now()
	// Run tick enough times for a frame.
	for i := 0; i < kNTSCWidth*kNTSCHeight; i++ {
		if err := ta.Tick(); err != nil {
			t.Fatalf("Error on tick: %v", err)
		}
		// Turn off VSYNC after it's done.
		if i > kVSYNCLines*kNTSCWidth && ta.vsync {
			ta.Write(VSYNC, 0x00)
		}
		// Turn off VBLANK after it's done.
		if i > kNTSCTopBlank*kNTSCWidth && ta.vblank {
			ta.Write(VBLANK, 0x00)
		}
		// Turn VBLANK back on at the bottom.
		if i > kNTSCOverscanStart*kNTSCWidth {
			ta.Write(VBLANK, kMASK_VBL_VBLANK)
		}
	}
	// Now trigger a VSYNC which should trigger callback.
	t.Logf("Total frame time: %s", time.Now().Sub(now))
	ta.Write(VSYNC, 0xFF)
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
