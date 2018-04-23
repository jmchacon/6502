package tia

import (
	"image"
	"testing"
)

func TestRam(t *testing.T) {
	f := func(i *image.NRGBA) {}
	ta, err := Init(&TIADef{
		Mode:      TIA_MODE_NTSC,
		FrameDone: f,
	})
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	// Make sure RAM works for the basic 128 addresses including aliasing.
	for i := uint16(0x0000); i < 0xFFFF; i++ {
		ta.Write(i, uint8(i))
		if got, want := ta.Read(i), uint8(i); got != want {
			t.Errorf("Bad Write/Read cycle for RAM: Wrote %.2X to %.4X but got %.2X on read", want, i, got)
		}
	}
}
