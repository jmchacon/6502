package tia

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func TestBackground(t *testing.T) {
	done := false
	f := func(i *image.NRGBA) {
		o, err := os.Create("image.png")
		if err != nil {
			t.Fatal(err)
		}
		defer o.Close()
		if err := png.Encode(o, i); err != nil {
			t.Fatal(err)
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

	// Set background to red
	ta.Write(0x09, 0x1B)
	// Turn on VBLANK
	ta.Write(0x01, kMASK_VBL_VBLANK)
	// Run tick enough times for a frame.
	for i := 0; i < kNTSCWidth*kNTSCHeight; i++ {
		if err := ta.Tick(); err != nil {
			t.Fatalf("Error on tick: %v", err)
		}
		// Turn off VSYNC after 3 lines
		if i > 3*kNTSCWidth && ta.vsync {
			ta.Write(0x00, 0x00)
		}
		// Turn off VLANK after 37 more lines.
		if i > 40*kNTSCWidth && ta.vblank {
			ta.Write(0x01, 0x00)
		}
		// Turn VBLANK back on at the bottom.
		if i > 232*kNTSCWidth {
			ta.Write(0x01, kMASK_VBL_VBLANK)
		}
	}
	// Now trigger a VSYNC which should trigger callback.
	ta.Write(0x00, 0xFF)
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}
}
