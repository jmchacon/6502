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
	// Turn on VBLANK and VSYNC
	ta.Write(VBLANK, kMASK_VBL_VBLANK)
	ta.Write(VSYNC, 0xFF)
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
		picStart int
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
			picStart: kNTSCPictureStart,
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
			picStart: kPALPictureStart,
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
			picStart: kPALPictureStart,
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
			p := pic{
				w:        test.width,
				h:        test.height,
				vblank:   test.vblank,
				overscan: test.overscan,
				picStart: test.picStart,
				b:        test.colors[cnt],
			}
			want := createCanonicalImage(p)
			if diff := deep.Equal(ta.picture, want); diff != nil {
				t.Errorf("%s: Color %d: Pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", test.name, test.width, cnt, diff)
			}
		}
	}
}

type pic struct {
	w        int
	h        int
	vblank   int
	overscan int
	picStart int
	b        *color.NRGBA
}

// createCanonicalImage sets up a boxed canonical image (i.e. hblank, vblank and overscan areas).
// Then it fills in the background color everywhere else.
// Callers will need to fill in the visible area with expected values.
func createCanonicalImage(p pic) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, p.w, p.h))
	// First 40 lines should be black
	for i := 0; i < p.vblank; i++ {
		paint(0, p.w, i, img, kBlack)
	}
	// In between lines have black hblank areas and background otherwise.
	for i := p.vblank; i < p.overscan; i++ {
		paint(0, p.picStart, i, img, kBlack)
		paint(p.picStart, p.w, i, img, p.b)
	}

	// Last N are black again.
	for i := p.overscan; i < p.h; i++ {
		paint(0, p.w, i, img, kBlack)
	}
	return img
}

// cl defines a horizontal range to paint.
type cl struct {
	start int
	stop  int // One past (so loop can be < stop)
}

func paint(start, stop, h int, i *image.NRGBA, cl *color.NRGBA) {
	for w := start; w < stop; w++ {
		i.Set(w, h, cl)
	}
}
func TestPlayfield(t *testing.T) {

	done := false
	cnt := 0
	ta, err := Init(&TIADef{
		Mode:      TIA_MODE_NTSC,
		FrameDone: generateImage(t, t.Name()+"Regular", &cnt, &done),
	})
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	const (
		yellow = uint8(0x0F)
		red    = uint8(0x1B)
		blue   = uint8(0x42)
		green  = uint8(0x5A)
	)
	t.Logf("\nyellow: %v\nred: %v\nblue: %v\ngreen: %v", kNTSC[yellow], kNTSC[red], kNTSC[blue], kNTSC[green])

	// Set background to yellow - 0x0F (and left shift it to act as a color value).
	ta.Write(COLUBK, yellow<<1)
	// Set player0 to red (0x1B) and player1 to blue (0x42) and again left shift.
	ta.Write(COLUP0, red<<1)
	ta.Write(COLUP1, blue<<1)
	// Finally set playfield to green (0x5A) and again left shift.
	ta.Write(COLUPF, green<<1)

	// Write all ones into the 3 PF registers so we generate a bar.
	ta.Write(PF0, 0xFF)
	ta.Write(PF1, 0xFF)
	ta.Write(PF2, 0xFF)
	// Make playfield reflect
	ta.Write(CTRLPF, kMASK_REF)

	callback := func(i int) {
		// Unless we're past line 10 (visible) and before the last 10 lines.
		// (the index is 0 based whereas the constants are line counts). This
		// gets called before line rendering starts so checking on +10 means 10
		// rows are done.
		if i == kNTSCTopBlank+10 {
			ta.Write(PF1, 0x00)
			ta.Write(PF2, 0x00)
		}
		if i == kNTSCOverscanStart-10 {
			ta.Write(PF1, 0xFF)
			ta.Write(PF2, 0xFF)
		}
	}
	m := make(map[int]func(int))
	m[kNTSCTopBlank+10] = callback
	m[kNTSCOverscanStart-10] = callback
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	p := pic{
		w:        kNTSCWidth,
		h:        kNTSCHeight,
		vblank:   kNTSCTopBlank,
		overscan: kNTSCOverscanStart,
		picStart: kNTSCPictureStart,
		b:        kNTSC[yellow],
	}

	want := createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10, h >= kNTSCOverscanStart-10:
			// First 10 and last 10 rows are solid green.
			paint(kHblank, kNTSCWidth, h, want, kNTSC[green])
		default:
			// Everything else is first kPF0Pixels pixels green and last kPF0Pixels pixels green.
			// Remember, PF0 is only 4 bits but that's 4 pixels per bit when on screen.
			// The rest are background (yellow).
			paint(kNTSCPictureStart, kNTSCPictureStart+kPF0Pixels, h, want, kNTSC[green])
			paint(kNTSCWidth-kPF0Pixels, kNTSCWidth, h, want, kNTSC[green])
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Box pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

	// Turn off reflection
	ta.Write(CTRLPF, 0x00)
	cnt++
	done = false
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	want = createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10, h >= kNTSCOverscanStart-10:
			// First 10 and last 10 rows are solid green.
			paint(kNTSCPictureStart, kNTSCWidth, h, want, kNTSC[green])
		default:
			// Everything else is first kPF0Pixels pixels green then kPF0Pixels after mid screen (visible).
			// Remember, PF0 is only 4 bits but that's 4 pixels per bit when on screen.
			// The rest are background (yellow).
			paint(kNTSCPictureStart, kNTSCPictureStart+kPF0Pixels, h, want, kNTSC[green])
			paint(kNTSCPictureMiddle, kNTSCPictureMiddle+kPF0Pixels, h, want, kNTSC[green])
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Non reflected box pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

	// Set PF0/PF1/PF2 to alternating patterns which should cause 2 double pixels due to decoding reversals.
	ta.Write(PF0, 0xA0)
	ta.Write(PF1, 0x55)
	ta.Write(PF2, 0x55)
	// The regular pattern:
	//
	// PF0:            PF1:                            PF2:
	// 00001111000011110000111100001111000011110000111111110000111100001111000011110000
	//
	// And reflected:
	//
	// PF2:                            PF1:                            PF0:
	// 00001111000011110000111100001111111100001111000011110000111100001111000011110000

	// Turn reflection back on for this run.
	ta.Write(CTRLPF, kMASK_REF)
	cnt++
	done = false
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	want = createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10:
			// First 10 rows are all alternating pattern with reflection.
			// First half (always non-reflected) and 2nd half reflected.
			for _, g := range []cl{{4, 8}, {12, 16}, {20, 24}, {28, 32}, {36, 40}, {44, 52}, {56, 60}, {64, 68}, {72, 76}, {84, 88}, {92, 96}, {100, 104}, {108, 116}, {120, 124}, {128, 132}, {136, 140}, {144, 148}, {152, 156}} {
				paint(kNTSCPictureStart+g.start, kNTSCPictureStart+g.stop, h, want, kNTSC[green])
			}
		case h >= kNTSCOverscanStart-10:
			// Last 10 rows are solid green. Except edges are PF0 so stippled.
			paint(kNTSCPictureStart, kNTSCWidth, h, want, kNTSC[green])
			// Yes this is the opposite of the above logic (colors swapped).
			for _, y := range []cl{{0, 4}, {8, 12}, {148, 152}, {156, 160}} {
				paint(kNTSCPictureStart+y.start, kNTSCPictureStart+y.stop, h, want, kNTSC[yellow])
			}
		default:
			// Rest are all yellow except green PF0 stippled edges.
			for _, g := range []cl{{4, 8}, {12, 16}, {144, 148}, {152, 156}} {
				paint(kNTSCPictureStart+g.start, kNTSCPictureStart+g.stop, h, want, kNTSC[green])
			}
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Reflected pattern pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

	// For the next frame have to reset PF values as our callbacks reset PF1/2 above (it's why the bottom lines were solid).
	ta.Write(PF0, 0xA0)
	ta.Write(PF1, 0x55)
	ta.Write(PF2, 0x55)
	// Now turn reflection off which should move the bit doubling slightly.
	ta.Write(CTRLPF, 0x00)
	cnt++
	done = false
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	want = createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10:
			// First 10 rows are all alternating pattern with reflection.
			// First half (always non-reflected) and 2nd half non-reflected.
			for _, g := range []cl{{4, 8}, {12, 16}, {20, 24}, {28, 32}, {36, 40}, {44, 52}, {56, 60}, {64, 68}, {72, 76}, {84, 88}, {92, 96}, {100, 104}, {108, 112}, {116, 120}, {124, 132}, {136, 140}, {144, 148}, {152, 156}} {
				paint(kNTSCPictureStart+g.start, kNTSCPictureStart+g.stop, h, want, kNTSC[green])
			}
		case h >= kNTSCOverscanStart-10:
			// Last 10 rows are solid green. Except edges are PF0 so stippled. And PF0 isn't reflected this time so mid-screen.
			paint(kNTSCPictureStart, kNTSCWidth, h, want, kNTSC[green])
			// Yes this is the opposite of the above logic (colors swapped).
			for _, y := range []cl{{0, 4}, {8, 12}, {80, 84}, {88, 92}} {
				paint(kNTSCPictureStart+y.start, kNTSCPictureStart+y.stop, h, want, kNTSC[yellow])
			}
		default:
			for _, g := range []cl{{4, 8}, {12, 16}, {84, 88}, {92, 96}} {
				paint(kNTSCPictureStart+g.start, kNTSCPictureStart+g.stop, h, want, kNTSC[green])
			}
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Non reflected pattern pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

	// Now do score mode which should change colors
	ta.Write(PF0, 0xFF)
	ta.Write(PF1, 0xFf)
	ta.Write(PF2, 0xFF)
	// Leave reflection off so the middle bar can be seen as the transition point.
	// But, turn on score mode.
	ta.Write(CTRLPF, kMASK_SCORE)
	cnt++
	done = false
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	want = createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10, h >= kNTSCOverscanStart-10:
			// First and last 10 rows are half red, half blue.
			// Write all red first and then we'll replace with green for playfield bits.
			paint(kNTSCPictureStart, kNTSCPictureMiddle, h, want, kNTSC[red])
			paint(kNTSCPictureMiddle, kNTSCWidth, h, want, kNTSC[blue])
		default:
			// Rest are all yellow except red or blue PF0 blocks (which is in the middle for the repeat due to no relfection).
			paint(kNTSCPictureStart, kNTSCPictureStart+kPF0Pixels, h, want, kNTSC[red])
			paint(kNTSCPictureMiddle, kNTSCPictureMiddle+kPF0Pixels, h, want, kNTSC[blue])
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Score mode box pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

	// Test score mode changing mid screen. Say after 20 lines and turn back on at bottom -20.
	callback2 := func(i int) {
		ta.Write(CTRLPF, 0x00)
	}
	callback3 := func(i int) {
		ta.Write(CTRLPF, kMASK_SCORE)
	}
	m[kNTSCTopBlank+20] = callback2
	m[kNTSCOverscanStart-20] = callback3
	cnt++
	done = false
	runAFrame(t, ta, frameSpec{
		width:     kNTSCWidth,
		height:    kNTSCHeight,
		vsync:     kVSYNCLines,
		vblank:    kNTSCTopBlank,
		overscan:  kNTSCOverscanStart,
		callbacks: m,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	want = createCanonicalImage(p)
	for h := kNTSCTopBlank; h < kNTSCOverscanStart; h++ {
		switch {
		case h < kNTSCTopBlank+10, h >= kNTSCOverscanStart-10:
			// First and last 10 rows are half red, half blue.
			// Write all red first and then we'll replace with green for playfield bits.
			paint(kNTSCPictureStart, kNTSCPictureMiddle, h, want, kNTSC[red])
			paint(kNTSCPictureMiddle, kNTSCWidth, h, want, kNTSC[blue])
		case h < kNTSCTopBlank+20, h >= kNTSCOverscanStart-20:
			// The next 10 are all yellow except red or blue PF0 blocks (which is in the middle for the repeat due to no relfection).
			paint(kNTSCPictureStart, kNTSCPictureStart+kPF0Pixels, h, want, kNTSC[red])
			paint(kNTSCPictureMiddle, kNTSCPictureMiddle+kPF0Pixels, h, want, kNTSC[blue])
		default:
			// The rest are green PF0 blocks in place of red/blue as above.
			paint(kNTSCPictureStart, kNTSCPictureStart+kPF0Pixels, h, want, kNTSC[green])
			paint(kNTSCPictureMiddle, kNTSCPictureMiddle+kPF0Pixels, h, want, kNTSC[green])
		}
	}
	if diff := deep.Equal(ta.picture, want); diff != nil {
		x := generateImage(t, "Error", &cnt, &done)
		x(want)
		t.Errorf("Score mode with transition pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", kNTSCWidth, diff)
	}

}
