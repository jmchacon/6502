package tia

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
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
	width       int
	height      int
	vsync       int
	vblank      int
	overscan    int
	vcallbacks  map[int]func(int, *TIA)              // Optional mapping of scan lines to callbacks at beginning of specified line (setting player/PF/etc registers possibly different).
	hvcallbacks map[int]map[int]func(int, int, *TIA) // Optional mapping of scan line and horizontal positions to callbacks at any point on the screen.
}

// setup creates a basic TIA object and initializes all the colors to known contrasting values.
func setup(t *testing.T, name string, mode TIAMode, cnt *int, done *bool) (*TIA, error) {
	n := strings.Split(t.Name(), "/")[0]
	ta, err := Init(&TIADef{
		Mode:      mode,
		FrameDone: generateImage(t, n+name, cnt, done),
	})
	if err != nil {
		return nil, err
	}
	t.Logf("Test: %s", name)

	// Set background to yellow - 0x0F (and left shift it to act as a color value).
	ta.Write(COLUBK, yellow<<1)
	// Set player0 to red (0x1B) and player1 to blue (0x42) and again left shift.
	ta.Write(COLUP0, red<<1)
	ta.Write(COLUP1, blue<<1)
	// Finally set playfield to green (0x5A) and again left shift.
	ta.Write(COLUPF, green<<1)

	return ta, nil
}

func runAFrame(t *testing.T, ta *TIA, frame frameSpec) {
	now := time.Now()
	// Run tick enough times for a frame.
	// Turn on VBLANK and VSYNC
	ta.Write(VBLANK, kMASK_VBL_VBLANK)
	ta.Write(VSYNC, kMASK_VSYNC)
	for i := 0; i < frame.height; i++ {
		if cb := frame.vcallbacks[i]; cb != nil {
			cb(i, ta)
		}
		// Turn off VSYNC after it's done.
		if i >= frame.vsync && ta.vsync {
			ta.Write(VSYNC, kMASK_VSYNC_OFF)
		}
		// Turn off VBLANK after it's done.
		if i >= frame.vblank && ta.vblank {
			ta.Write(VBLANK, kMASK_VBL_VBLANK_OFF)
		}
		// Turn VBLANK back on at the bottom.
		if i >= frame.overscan {
			ta.Write(VBLANK, kMASK_VBL_VBLANK)
		}
		for j := 0; j < frame.width; j++ {
			// Randomize order callbacks run to verify Tick() order doesn't matter.
			// NOTE: This means the time reported below is going to be off since rand calls
			//       take measurable time.
			before := true
			if rand.Float32() > 0.5 {
				before = false
			}
			cb := frame.hvcallbacks[i][j]
			if before && cb != nil {
				cb(j, i, ta)
			}
			if err := ta.Tick(); err != nil {
				t.Fatalf("Error on tick: %v", err)
			}
			if !before && cb != nil {
				cb(j, i, ta)
			}
			ta.TickDone()
		}
	}
	// Now trigger a VSYNC which should trigger callback.
	t.Logf("Total frame time: %s", time.Now().Sub(now))
	ta.Write(VSYNC, kMASK_VSYNC)
	if err := ta.Tick(); err != nil {
		t.Fatalf("Error on tick: %v", err)
	}
	ta.TickDone()
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
			ta, err := setup(t, test.name, test.mode, &cnt, &done)
			if err != nil {
				t.Fatalf("%s: Color %d: Can't Init: %v", test.name, cnt, err)
			}

			// Set background to current color (and left shift it to act as a color value).
			ta.Write(COLUBK, uint8(cnt)<<1)
			runAFrame(t, ta, frameSpec{
				width:      test.width,
				height:     test.height,
				vsync:      test.vsync,
				vblank:     test.vblank,
				overscan:   test.overscan,
				vcallbacks: make(map[int]func(int, *TIA)),
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

// paint is a helper for writing a set of pixels in a certain color in a range to the image.
// The draw package could be used but for small images where we're doing small horizontal
// ranges this is simpler.
func paint(start, stop, h int, i *image.NRGBA, cl *color.NRGBA) {
	for w := start; w < stop; w++ {
		i.Set(w, h, cl)
	}
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

// horizontal defines a horizontal range to paint.
type horizontal struct {
	start int
	stop  int // One past (so loop can be < stop)
	cl    *color.NRGBA
}

// scanline defines a set of definitions for a range of scanlines
// that are all identical.
type scanline struct {
	start       int
	stop        int
	horizontals []horizontal
}

type cl struct {
	start int
	stop  int
}

const (
	yellow = uint8(0x0F)
	red    = uint8(0x1B)
	blue   = uint8(0x42)
	green  = uint8(0x5A)
)

func TestDrawing(t *testing.T) {
	t.Logf("\nyellow: %v\nred: %v\nblue: %v\ngreen: %v", kNTSC[yellow], kNTSC[red], kNTSC[blue], kNTSC[green])

	// Standard callback we use on all playfield only tests.
	pfCallback := func(i int, ta *TIA) {
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

	// Only used below in a couple of specific playfield test.
	pfCallback2 := func(i int, ta *TIA) {
		// Turn off everything - score, reflection and set, priorty normal and ball width 1.
		ta.Write(CTRLPF, kMASK_REF_OFF|kMASK_SCORE_OFF|kMASK_PFP_NORMAL|kBALL_WIDTH_1)
	}
	pfCallback3 := func(i int, ta *TIA) {
		ta.Write(CTRLPF, kMASK_SCORE)
	}

	// Missile callbacks for 1,2,4,8 sized missiles. Always sets a single regular player.
	missile0Width1 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_1)
	}
	missile0Width2 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_2)
	}
	missile0Width4 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_4)
	}
	missile0Width8 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_8)
	}
	missile1Width1 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_1)
	}
	missile1Width2 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_2)
	}
	missile1Width4 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_4)
	}
	missile1Width8 := func(y, x int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_8)
	}

	// Missile movement callbacks
	missile0Move8 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT8)
	}
	missile0Move7 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT7)
	}
	missile0Move6 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT6)
	}
	missile0Move5 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT5)
	}
	missile0Move4 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT4)
	}
	missile0Move3 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT3)
	}
	missile0Move2 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT2)
	}
	missile0Move1 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT1)
	}
	missile0MoveNone := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_NONE)
	}
	missile0MoveLeft1 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT1)
	}
	missile0MoveLeft2 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT2)
	}
	missile0MoveLeft3 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT3)
	}
	missile0MoveLeft4 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT4)
	}
	missile0MoveLeft5 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT5)
	}
	missile0MoveLeft6 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT6)
	}
	missile0MoveLeft7 := func(y, x int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT7)
	}
	missile1Move8 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT8)
	}
	missile1Move7 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT7)
	}
	missile1Move6 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT6)
	}
	missile1Move5 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT5)
	}
	missile1Move4 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT4)
	}
	missile1Move3 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT3)
	}
	missile1Move2 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT2)
	}
	missile1Move1 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT1)
	}
	missile1MoveNone := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_NONE)
	}
	missile1MoveLeft1 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT1)
	}
	missile1MoveLeft2 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT2)
	}
	missile1MoveLeft3 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT3)
	}
	missile1MoveLeft4 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT4)
	}
	missile1MoveLeft5 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT5)
	}
	missile1MoveLeft6 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT6)
	}
	missile1MoveLeft7 := func(y, x int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT7)
	}

	// Ball callbacks for 1,2,4,8 sized balls.
	// We always have reflection of playfield and score mode on for the ball tests.
	ballWidth1 := func(y, x int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_1|kMASK_REF|kMASK_SCORE)
	}
	ballWidth2 := func(y, x int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_2|kMASK_REF|kMASK_SCORE)
	}
	ballWidth4 := func(y, x int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_4|kMASK_REF|kMASK_SCORE)
	}
	ballWidth8 := func(y, x int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_8|kMASK_REF|kMASK_SCORE)
	}

	// Ball movement callbacks
	ballMove8 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT8)
	}
	ballMove7 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT7)
	}
	ballMove6 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT6)
	}
	ballMove5 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT5)
	}
	ballMove4 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT4)
	}
	ballMove3 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT3)
	}
	ballMove2 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT2)
	}
	ballMove1 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT1)
	}
	ballMoveNone := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_NONE)
	}
	ballMoveLeft1 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT1)
	}
	ballMoveLeft2 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT2)
	}
	ballMoveLeft3 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT3)
	}
	ballMoveLeft4 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT4)
	}
	ballMoveLeft5 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT5)
	}
	ballMoveLeft6 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT6)
	}
	ballMoveLeft7 := func(y, x int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT7)
	}

	hmclr := func(y, x int, ta *TIA) {
		// Any value strobes it.
		ta.Write(HMCLR, 0x00)
	}

	hmove := func(y, x int, ta *TIA) {
		// Any value strobes it.
		ta.Write(HMOVE, 0x00)
	}

	// Turn the ball on and off.
	ballOn := func(y, x int, ta *TIA) {
		ta.Write(ENABL, kMASK_ENAMB)
	}
	ballOff := func(y, x int, ta *TIA) {
		ta.Write(ENABL, 0x00)
	}

	// Turn the 2 missiles on and off.
	missile0On := func(y, x int, ta *TIA) {
		ta.Write(ENAM0, kMASK_ENAMB)
	}
	missile1On := func(y, x int, ta *TIA) {
		ta.Write(ENAM1, kMASK_ENAMB)
	}
	missile0Off := func(y, x int, ta *TIA) {
		ta.Write(ENAM0, 0x00)
	}
	missile1Off := func(y, x int, ta *TIA) {
		ta.Write(ENAM1, 0x00)
	}

	// Vertical delay on.
	ballVerticalDelay := func(y int, ta *TIA) {
		ta.Write(VDELBL, kMASK_VDEL)
	}

	// Reset ball position. Should start painting 4 pixels later than this.
	ballReset := func(y, x int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESBL, 0x00)
	}

	// Reset missiles position. Should start painting 4 pixels later than this.
	missile0Reset := func(y, x int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESM0, 0x00)
	}
	missile1Reset := func(y, x int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESM1, 0x00)
	}

	// Set the player1 bitmask which also triggers vertical delay copies for GRP0 and the ball.
	player1Set := func(y int, ta *TIA) {
		ta.Write(GRP1, 0xFF)
	}

	tests := []struct {
		name        string
		pfRegs      [3]uint8 // Initial state for PFx regs (assuming was set during vblank).
		reflect     bool
		score       bool
		vcallbacks  map[int]func(int, *TIA)              // for runAFrame vcallbacks.
		hvcallbacks map[int]map[int]func(int, int, *TIA) // for runAFrame hvcallbacks
		scanlines   []scanline                           // for generating the canonical image for verification.
	}{
		{
			name:    "PlayfieldBox",
			pfRegs:  [3]uint8{0xFF, 0xFF, 0xFF},
			reflect: true,
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			scanlines: []scanline{
				// First 10 and last 10 rows are solid green.
				{
					start:       kNTSCTopBlank,
					stop:        kNTSCTopBlank + 10,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCWidth, kNTSC[green]}},
				},
				{
					start:       kNTSCOverscanStart - 10,
					stop:        kNTSCOverscanStart,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCWidth, kNTSC[green]}},
				},
				// Everything else is first kPF0Pixels pixels green and last kPF0Pixels pixels green.
				// Remember, PF0 is only 4 bits but that's 4 pixels per bit when on screen.
				// The rest are background (yellow).
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[green]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[green]},
					},
				},
			},
		},
		{
			// Box without reflection on.
			name:   "PlayfieldWindow",
			pfRegs: [3]uint8{0xFF, 0xFF, 0xFF},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			scanlines: []scanline{
				// First 10 are solid green.
				{
					start:       kNTSCTopBlank,
					stop:        kNTSCTopBlank + 10,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCWidth, kNTSC[green]}},
				},
				// Everything else is first kPF0Pixels pixels green then kPF0Pixels after mid screen (visible).
				// Remember, PF0 is only 4 bits but that's 4 pixels per bit when on screen.
				// The rest are background (yellow).
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[green]},
						{kNTSCPictureMiddle, kNTSCPictureMiddle + kPF0Pixels, kNTSC[green]},
					},
				},
				// Last 10 are solid green.
				{
					start:       kNTSCOverscanStart - 10,
					stop:        kNTSCOverscanStart,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCWidth, kNTSC[green]}},
				},
			},
		},
		{
			name: "PlayFieldAlternating-reflected",
			// Set PF0/PF1/PF2 to alternating patterns which should cause 2 double pixels due to decoding reversals.
			// The regular pattern:
			//
			// PF0:            PF1:                            PF2:
			// 00001111000011110000111100001111000011110000111111110000111100001111000011110000
			//
			// And reflected:
			//
			// PF2:                            PF1:                            PF0:
			// 00001111000011110000111100001111111100001111000011110000111100001111000011110000
			pfRegs: [3]uint8{0xA0, 0x55, 0x55},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			reflect: true,
			scanlines: []scanline{
				// First 10 rows are all alternating pattern with reflection.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 8, kNTSC[green]},
						{kNTSCPictureStart + 12, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 20, kNTSCPictureStart + 24, kNTSC[green]},
						{kNTSCPictureStart + 28, kNTSCPictureStart + 32, kNTSC[green]},
						{kNTSCPictureStart + 36, kNTSCPictureStart + 40, kNTSC[green]},
						{kNTSCPictureStart + 44, kNTSCPictureStart + 52, kNTSC[green]},
						{kNTSCPictureStart + 56, kNTSCPictureStart + 60, kNTSC[green]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 68, kNTSC[green]},
						{kNTSCPictureStart + 72, kNTSCPictureStart + 76, kNTSC[green]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 92, kNTSCPictureStart + 96, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 104, kNTSC[green]},
						{kNTSCPictureStart + 108, kNTSCPictureStart + 116, kNTSC[green]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 124, kNTSC[green]},
						{kNTSCPictureStart + 128, kNTSCPictureStart + 132, kNTSC[green]},
						{kNTSCPictureStart + 136, kNTSCPictureStart + 140, kNTSC[green]},
						{kNTSCPictureStart + 144, kNTSCPictureStart + 148, kNTSC[green]},
						{kNTSCPictureStart + 152, kNTSCPictureStart + 156, kNTSC[green]},
					},
				},
				// The rest are background (yellow) except edges are green PF0 stippled edges.
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 8, kNTSC[green]},
						{kNTSCPictureStart + 12, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 144, kNTSCPictureStart + 148, kNTSC[green]},
						{kNTSCPictureStart + 152, kNTSCPictureStart + 156, kNTSC[green]},
					},
				},
				// Last 10 rows are solid green. Except edges are PF0 so stippled.
				// Paint green first and then fill back in the yellow as needed.
				{
					start: kNTSCOverscanStart - 10,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCWidth, kNTSC[green]},
						{kNTSCPictureStart + 8, kNTSCPictureStart + 12, kNTSC[yellow]},
						{kNTSCPictureStart + 148, kNTSCPictureStart + 152, kNTSC[yellow]},
						{kNTSCPictureStart + 156, kNTSCPictureStart + 160, kNTSC[yellow]},
					},
				},
			},
		},
		{
			name:   "PlayFieldAlternating-not-reflected",
			pfRegs: [3]uint8{0xA0, 0x55, 0x55},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			scanlines: []scanline{
				// First 10 rows are all alternating pattern with reflection.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 8, kNTSC[green]},
						{kNTSCPictureStart + 12, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 20, kNTSCPictureStart + 24, kNTSC[green]},
						{kNTSCPictureStart + 28, kNTSCPictureStart + 32, kNTSC[green]},
						{kNTSCPictureStart + 36, kNTSCPictureStart + 40, kNTSC[green]},
						{kNTSCPictureStart + 44, kNTSCPictureStart + 52, kNTSC[green]},
						{kNTSCPictureStart + 56, kNTSCPictureStart + 60, kNTSC[green]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 68, kNTSC[green]},
						{kNTSCPictureStart + 72, kNTSCPictureStart + 76, kNTSC[green]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 92, kNTSCPictureStart + 96, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 104, kNTSC[green]},
						{kNTSCPictureStart + 108, kNTSCPictureStart + 112, kNTSC[green]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 120, kNTSC[green]},
						{kNTSCPictureStart + 124, kNTSCPictureStart + 132, kNTSC[green]},
						{kNTSCPictureStart + 136, kNTSCPictureStart + 140, kNTSC[green]},
						{kNTSCPictureStart + 144, kNTSCPictureStart + 148, kNTSC[green]},
						{kNTSCPictureStart + 152, kNTSCPictureStart + 156, kNTSC[green]},
					},
				},
				// The rest are background (yellow) except edges are green PF0 stippled edges.
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 8, kNTSC[green]},
						{kNTSCPictureStart + 12, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 92, kNTSCPictureStart + 96, kNTSC[green]},
					},
				},
				// Last 10 rows are solid green. Except edges are PF0 so stippled.
				// Paint green first and then fill back in the yellow as needed.
				{
					start: kNTSCOverscanStart - 10,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCWidth, kNTSC[green]},
						{kNTSCPictureStart + 8, kNTSCPictureStart + 12, kNTSC[yellow]},
						{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[yellow]},
						{kNTSCPictureStart + 88, kNTSCPictureStart + 92, kNTSC[yellow]},
					},
				},
			},
		},
		{
			name:   "PlayfieldScoreMode-not-reflected",
			pfRegs: [3]uint8{0xFF, 0xFF, 0xFF},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			score: true,
			scanlines: []scanline{
				// First and last 10 rows are half red, half blue.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
				// Rest are all yellow except red or blue PF0 blocks (which is now reflected).
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCPictureMiddle + kPF0Pixels, kNTSC[blue]},
					},
				},
				// Last 10 rows are the same as first 10.
				{
					start: kNTSCOverscanStart - 10,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "PlayfieldScoreMode-reflected",
			pfRegs: [3]uint8{0xFF, 0xFF, 0xFF},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCOverscanStart - 10: pfCallback,
			},
			reflect: true,
			score:   true,
			scanlines: []scanline{
				// First and last 10 rows are half red, half blue.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
				// Rest are all yellow except red or blue PF0 blocks (which is in the middle for the repeat due to no relfection).
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				// Last 10 rows are the same as first 10.
				{
					start: kNTSCOverscanStart - 10,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "PlayfieldScoreMode-transition-no-reflect",
			pfRegs: [3]uint8{0xFF, 0xFF, 0xFF},
			vcallbacks: map[int]func(int, *TIA){
				kNTSCTopBlank + 10:      pfCallback,
				kNTSCTopBlank + 20:      pfCallback2,
				kNTSCOverscanStart - 20: pfCallback3,
				kNTSCOverscanStart - 10: pfCallback,
			},
			score: true,
			scanlines: []scanline{
				// First and last 10 rows are half red, half blue.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
				// Next 10 have red/blue blocks on sides/middle.
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCTopBlank + 20,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCPictureMiddle + kPF0Pixels, kNTSC[blue]},
					},
				},
				// The rest are green PF0 blocks in place of red/blue as above.
				{
					start: kNTSCTopBlank + 20,
					stop:  kNTSCOverscanStart - 20,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[green]},
						{kNTSCPictureMiddle, kNTSCPictureMiddle + kPF0Pixels, kNTSC[green]},
					},
				},
				// The 10 before we get to the final 10 have red/blue blocks on sides/middle.
				{
					start: kNTSCOverscanStart - 20,
					stop:  kNTSCOverscanStart - 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCPictureMiddle + kPF0Pixels, kNTSC[blue]},
					},
				},
				// Last 10 rows are the same as first 10.
				{
					start: kNTSCOverscanStart - 10,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureMiddle, kNTSC[red]},
						{kNTSCPictureMiddle, kNTSCWidth, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "BallMissileOffButWidthsChange",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank:      {0: ballWidth1},
				kNTSCTopBlank + 10: {0: ballWidth2},
				kNTSCTopBlank + 20: {0: ballWidth4},
				kNTSCTopBlank + 30: {0: ballWidth8},
				kNTSCTopBlank + 40: {0: missile0Width8, 8: missile1Width8},
				kNTSCTopBlank + 50: {0: missile0Width4, 8: missile1Width4},
				kNTSCTopBlank + 60: {0: missile0Width2, 8: missile1Width2},
				kNTSCTopBlank + 70: {0: missile0Width1, 8: missile1Width1},
			},
			scanlines: []scanline{
				// Every line is red left and blue right columns each PF0 sized.
				// i.e. no ball should show up.
				{
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "BallMissileOnWidthsChange",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth1, 8: missile0Width1, 17: missile1Width1},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset, kNTSCPictureStart + 96: missile0Reset, kNTSCPictureStart + 116: missile1Reset},
				kNTSCTopBlank + 5:  {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 10: {9: ballOff, 8: missile0Off, 17: missile1Off},
				kNTSCTopBlank + 20: {0: ballWidth2, 8: missile0Width2, 17: missile1Width2},
				kNTSCTopBlank + 25: {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 30: {9: ballOff, 8: missile0Off, 17: missile1Off},
				kNTSCTopBlank + 40: {0: ballWidth4, 8: missile0Width4, 17: missile1Width4},
				kNTSCTopBlank + 45: {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 50: {0: ballOff, 8: missile0Off, 17: missile1Off},
				kNTSCTopBlank + 60: {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 65: {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 70: {0: ballOff, 8: missile0Off, 17: missile1Off},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start: kNTSCTopBlank + 5,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 81, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 101, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 121, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 25,
					stop:  kNTSCTopBlank + 30,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 82, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 102, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 122, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 45,
					stop:  kNTSCTopBlank + 50,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 104, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 124, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 65,
					stop:  kNTSCTopBlank + 70,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 128, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "BallMissileOnWidthsChangeScreenEdge",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth1, 8: missile0Width1, 17: missile1Width1},
				kNTSCTopBlank + 1:  {kNTSCPictureStart + 155: missile0Reset},
				kNTSCTopBlank + 2:  {kNTSCPictureStart + 155: missile1Reset},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 155: ballReset},
				kNTSCTopBlank + 5:  {0: ballOn},
				kNTSCTopBlank + 7:  {0: missile0On, 9: ballOff},
				kNTSCTopBlank + 9:  {0: missile1On, 9: missile0Off},
				kNTSCTopBlank + 11: {9: missile1Off},
				kNTSCTopBlank + 20: {0: ballWidth2, 8: missile0Width2, 17: missile1Width2},
				kNTSCTopBlank + 25: {0: ballOn},
				kNTSCTopBlank + 27: {0: missile0On, 9: ballOff},
				kNTSCTopBlank + 29: {0: missile1On, 9: missile0Off},
				kNTSCTopBlank + 31: {9: missile1Off},
				kNTSCTopBlank + 40: {0: ballWidth4, 8: missile0Width4, 17: missile1Width4},
				kNTSCTopBlank + 45: {0: ballOn},
				kNTSCTopBlank + 47: {0: missile0On, 9: ballOff},
				kNTSCTopBlank + 49: {0: missile1On, 9: missile0Off},
				kNTSCTopBlank + 51: {9: missile1Off},
				kNTSCTopBlank + 60: {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 65: {0: ballOn},
				kNTSCTopBlank + 67: {0: missile0On, 9: ballOff},
				kNTSCTopBlank + 69: {0: missile1On, 9: missile0Off},
				kNTSCTopBlank + 71: {9: missile1Off},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// All of these should be green for the ball (playfield color) since score mode shouldn't be changing
					// the ball drawing color. Missile colors will match players which means in some cases for wrapping
					// we don't see missile bits for one but do for the other. That should suffice to prove things.
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 7,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 7,
					stop:        kNTSCTopBlank + 9,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 9,
					stop:        kNTSCTopBlank + 11,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[blue]}},
				},
				{
					// 1 pixel writes on on right edge and then wraps to next scanline for a single one.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap doesn't happen).
					start:       kNTSCTopBlank + 25,
					stop:        kNTSCTopBlank + 27,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 27,
					stop:        kNTSCTopBlank + 29,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 29,
					stop:        kNTSCTopBlank + 31,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[blue]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 25,
					stop:        kNTSCTopBlank + 27,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 27,
					stop:        kNTSCTopBlank + 29,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 29,
					stop:        kNTSCTopBlank + 31,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[blue]}},
				},
				{
					// 1 pixel writes on on right edge and then wraps to next scanline for three more.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap doesn't happen).
					start:       kNTSCTopBlank + 45,
					stop:        kNTSCTopBlank + 47,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 47,
					stop:        kNTSCTopBlank + 49,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 49,
					stop:        kNTSCTopBlank + 51,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[blue]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 45,
					stop:        kNTSCTopBlank + 47,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 3, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 47,
					stop:        kNTSCTopBlank + 49,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 3, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 49,
					stop:        kNTSCTopBlank + 51,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 3, kNTSC[blue]}},
				},
				{
					// 1 pixel writes on on right edge and then wraps to next scanline for seven more.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap doesn't write).
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 67,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 67,
					stop:        kNTSCTopBlank + 69,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 69,
					stop:        kNTSCTopBlank + 71,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[blue]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 67,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 7, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 67,
					stop:        kNTSCTopBlank + 69,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 7, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 69,
					stop:        kNTSCTopBlank + 71,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 7, kNTSC[blue]}},
				},
			},
		},
		{
			name:   "BallMissileOnWidthsAndDisableMidWrite",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset, kNTSCPictureStart + 96: missile0Reset, kNTSCPictureStart + 116: missile1Reset},
				kNTSCTopBlank + 5:  {0: ballOn, 8: missile0On, 17: missile1On, kNTSCPictureStart + 85: ballOff, kNTSCPictureStart + 105: missile0Off, kNTSCPictureStart + 125: missile1Off},
				kNTSCTopBlank + 7:  {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 8:  {0: ballOff, 8: missile0Off, 17: missile1Off},
				kNTSCTopBlank + 20: {0: ballOn, 4: ballWidth4, 8: missile0On, 9: missile0Width4, 17: missile1On, 18: missile1Width4, kNTSCPictureStart + 85: ballWidth8, kNTSCPictureStart + 95: ballOff, kNTSCPictureStart + 105: missile0Width8, kNTSCPictureStart + 115: missile0Off, kNTSCPictureStart + 125: missile1Width8, kNTSCPictureStart + 135: missile1Off},
				kNTSCTopBlank + 22: {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 23: {0: ballOff, 8: missile0Off, 17: missile1Off},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start: kNTSCTopBlank + 5,
					stop:  kNTSCTopBlank + 6,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 86, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 106, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 126, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 7,
					stop:  kNTSCTopBlank + 8,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 128, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 20,
					stop:  kNTSCTopBlank + 21,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[green]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 104, kNTSC[red]},
						{kNTSCPictureStart + 106, kNTSCPictureStart + 108, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 124, kNTSC[blue]},
						{kNTSCPictureStart + 126, kNTSCPictureStart + 128, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 22,
					stop:  kNTSCTopBlank + 23,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[red]},
						{kNTSCPictureStart + 120, kNTSCPictureStart + 128, kNTSC[blue]},
					},
				},
			},
		},
		{
			name: "BallMissileOnWidthsAndResetNTime",
			// No columns on this test to verify edge missiles work.
			pfRegs: [3]uint8{0x00, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:     {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3: {kNTSCPictureStart: ballReset, kNTSCPictureStart + 10: missile0Reset, kNTSCPictureStart + 70: missile1Reset},
				kNTSCTopBlank + 5: {0: ballOn, 8: missile0On, 17: missile1On},
				kNTSCTopBlank + 6: {0: ballOff, 8: missile0Off, 17: missile1Off},
				kNTSCTopBlank + 7: {0: ballOn, 8: missile0On, 17: missile1On, kNTSCPictureStart + 20: ballReset, kNTSCPictureStart + 30: missile0Reset, kNTSCPictureStart + 40: ballReset, kNTSCPictureStart + 50: missile0Reset, kNTSCPictureStart + 80: ballReset, kNTSCPictureStart + 90: missile0Reset, kNTSCPictureStart + 100: missile1Reset, kNTSCPictureStart + 120: missile1Reset, kNTSCPictureStart + 140: missile1Reset},
				kNTSCTopBlank + 9: {0: ballOff, 8: missile0Off, 17: missile1Off},
			},
			scanlines: []scanline{
				{
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start: kNTSCTopBlank + 5,
					stop:  kNTSCTopBlank + 6,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 12, kNTSC[green]},
						{kNTSCPictureStart + 14, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 74, kNTSCPictureStart + 82, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 7,
					stop:  kNTSCTopBlank + 8,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 12, kNTSC[green]},
						{kNTSCPictureStart + 24, kNTSCPictureStart + 32, kNTSC[green]},
						{kNTSCPictureStart + 44, kNTSCPictureStart + 52, kNTSC[green]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[green]},
						{kNTSCPictureStart + 14, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 34, kNTSCPictureStart + 42, kNTSC[red]},
						{kNTSCPictureStart + 54, kNTSCPictureStart + 62, kNTSC[red]},
						{kNTSCPictureStart + 94, kNTSCPictureStart + 102, kNTSC[red]},
						{kNTSCPictureStart + 74, kNTSCPictureStart + 82, kNTSC[blue]},
						{kNTSCPictureStart + 104, kNTSCPictureStart + 112, kNTSC[blue]},
						{kNTSCPictureStart + 124, kNTSCPictureStart + 132, kNTSC[blue]},
						{kNTSCPictureStart + 144, kNTSCPictureStart + 152, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 8,
					stop:  kNTSCTopBlank + 9,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[green]},
						{kNTSCPictureStart + 94, kNTSCPictureStart + 102, kNTSC[red]},
						{kNTSCPictureStart + 144, kNTSCPictureStart + 152, kNTSC[blue]},
					},
				},
			},
		},
		{
			name: "BallMissileOnResetHblankAndFarEdge",
			// No columns on this test to verify edge missiles work.
			pfRegs: [3]uint8{0x00, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball/missile control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3:  {0: ballReset, 8: ballOn},
				kNTSCTopBlank + 5:  {0: ballOn},
				kNTSCTopBlank + 7:  {0: ballOff, kNTSCWidth - 13: ballOn, kNTSCWidth - 12: ballReset},
				kNTSCTopBlank + 9:  {0: ballOff},
				kNTSCTopBlank + 11: {0: missile0Reset, 8: missile0On},
				kNTSCTopBlank + 13: {0: missile0On},
				kNTSCTopBlank + 15: {0: missile0Off, kNTSCWidth - 13: missile0On, kNTSCWidth - 12: missile0Reset},
				kNTSCTopBlank + 17: {0: missile0Off},
				kNTSCTopBlank + 19: {0: missile1Reset, 8: missile1On},
				kNTSCTopBlank + 21: {0: missile1On},
				kNTSCTopBlank + 23: {0: missile1Off, kNTSCWidth - 13: missile1On, kNTSCWidth - 12: missile1Reset},
				kNTSCTopBlank + 25: {0: missile1Off},
			},
			scanlines: []scanline{
				{
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start:       kNTSCTopBlank + 3,
					stop:        kNTSCTopBlank + 7,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 11,
					stop:        kNTSCTopBlank + 15,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 19,
					stop:        kNTSCTopBlank + 23,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]}},
				},
				{
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start:       kNTSCTopBlank + 7,
					stop:        kNTSCTopBlank + 9,
					horizontals: []horizontal{{kNTSCWidth - 8, kNTSCWidth, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 15,
					stop:        kNTSCTopBlank + 17,
					horizontals: []horizontal{{kNTSCWidth - 8, kNTSCWidth, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 23,
					stop:        kNTSCTopBlank + 25,
					horizontals: []horizontal{{kNTSCWidth - 8, kNTSCWidth, kNTSC[blue]}},
				},
			},
		},
		{
			name:   "BallMissileOnMove",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3:  {0: ballReset, kNTSCPictureStart + 56: missile0Reset, kNTSCPictureStart + 106: missile1Reset},
				kNTSCTopBlank + 5:  {0: ballOn, 8: missile0On, 17: missile1On, 200: ballMove8, 208: missile0Move8, 216: missile1Move8},
				kNTSCTopBlank + 6:  {8: hmove, 200: ballMove7, 208: missile0Move7, 216: missile1Move7},
				kNTSCTopBlank + 7:  {8: hmove, 200: ballMove6, 208: missile0Move6, 216: missile1Move6},
				kNTSCTopBlank + 8:  {8: hmove, 200: ballMove5, 208: missile0Move5, 216: missile1Move5},
				kNTSCTopBlank + 9:  {8: hmove, 200: ballMove4, 208: missile0Move4, 216: missile1Move4},
				kNTSCTopBlank + 10: {8: hmove, 200: ballMove3, 208: missile0Move3, 216: missile1Move3},
				kNTSCTopBlank + 11: {8: hmove, 200: ballMove2, 208: missile0Move2, 216: missile1Move2},
				kNTSCTopBlank + 12: {8: hmove, 200: ballMove1, 208: missile0Move1, 216: missile1Move1},
				kNTSCTopBlank + 13: {8: hmove, 200: ballMoveNone, 208: missile0MoveNone, 216: missile1MoveNone},
				kNTSCTopBlank + 15: {8: hmove, 200: ballMoveLeft1, 208: missile0MoveLeft1, 216: missile1MoveLeft1},
				kNTSCTopBlank + 16: {8: hmove, 200: ballMoveLeft2, 208: missile0MoveLeft2, 216: missile1MoveLeft2},
				kNTSCTopBlank + 17: {8: hmove, 200: ballMoveLeft3, 208: missile0MoveLeft3, 216: missile1MoveLeft3},
				kNTSCTopBlank + 18: {8: hmove, 200: ballMoveLeft4, 208: missile0MoveLeft4, 216: missile1MoveLeft4},
				kNTSCTopBlank + 19: {8: hmove, 200: ballMoveLeft5, 208: missile0MoveLeft5, 216: missile1MoveLeft5},
				kNTSCTopBlank + 20: {8: hmove, 200: ballMoveLeft6, 208: missile0MoveLeft6, 216: missile1MoveLeft6},
				kNTSCTopBlank + 21: {8: hmove, 200: ballMoveLeft7, 208: missile0MoveLeft7, 216: missile1MoveLeft7},
				kNTSCTopBlank + 22: {8: hmove},
				kNTSCTopBlank + 23: {8: hmove},
				kNTSCTopBlank + 25: {0: ballOff, 8: missile0Off, 17: missile1Off},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// The ball should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start: kNTSCTopBlank + 5,
					stop:  kNTSCTopBlank + 6,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[green]},
						{kNTSCPictureStart + 60, kNTSCPictureStart + 68, kNTSC[red]},
						{kNTSCPictureStart + 110, kNTSCPictureStart + 118, kNTSC[blue]},
					},
				},
				{
					// The rest of these executed HMOVE so they get the extended hblank comb.
					start: kNTSCTopBlank + 6,
					stop:  kNTSCTopBlank + 7,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 8, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 68, kNTSCPictureStart + 76, kNTSC[red]},
						{kNTSCPictureStart + 118, kNTSCPictureStart + 126, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 7,
					stop:  kNTSCTopBlank + 8,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 15, kNTSCPictureStart + 23, kNTSC[green]},
						{kNTSCPictureStart + 75, kNTSCPictureStart + 83, kNTSC[red]},
						{kNTSCPictureStart + 125, kNTSCPictureStart + 133, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 8,
					stop:  kNTSCTopBlank + 9,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 29, kNTSC[green]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 89, kNTSC[red]},
						{kNTSCPictureStart + 131, kNTSCPictureStart + 139, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 9,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 26, kNTSCPictureStart + 34, kNTSC[green]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 94, kNTSC[red]},
						{kNTSCPictureStart + 136, kNTSCPictureStart + 144, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCTopBlank + 11,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 30, kNTSCPictureStart + 38, kNTSC[green]},
						{kNTSCPictureStart + 90, kNTSCPictureStart + 98, kNTSC[red]},
						{kNTSCPictureStart + 140, kNTSCPictureStart + 148, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 11,
					stop:  kNTSCTopBlank + 12,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 33, kNTSCPictureStart + 41, kNTSC[green]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[red]},
						{kNTSCPictureStart + 143, kNTSCPictureStart + 151, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 12,
					stop:  kNTSCTopBlank + 13,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 35, kNTSCPictureStart + 43, kNTSC[green]},
						{kNTSCPictureStart + 95, kNTSCPictureStart + 103, kNTSC[red]},
						{kNTSCPictureStart + 145, kNTSCPictureStart + 153, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 13,
					stop:  kNTSCTopBlank + 14,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 36, kNTSCPictureStart + 44, kNTSC[green]},
						{kNTSCPictureStart + 96, kNTSCPictureStart + 104, kNTSC[red]},
						{kNTSCPictureStart + 146, kNTSCPictureStart + 154, kNTSC[blue]},
					},
				},
				{
					// No comb on middle line (no HMOVE).
					start: kNTSCTopBlank + 14,
					stop:  kNTSCTopBlank + 15,
					horizontals: []horizontal{
						{kNTSCPictureStart + 36, kNTSCPictureStart + 44, kNTSC[green]},
						{kNTSCPictureStart + 96, kNTSCPictureStart + 104, kNTSC[red]},
						{kNTSCPictureStart + 146, kNTSCPictureStart + 154, kNTSC[blue]},
					},
				},
				{
					// Didn't move but did have HMOVE so comb again.
					start: kNTSCTopBlank + 15,
					stop:  kNTSCTopBlank + 16,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 36, kNTSCPictureStart + 44, kNTSC[green]},
						{kNTSCPictureStart + 96, kNTSCPictureStart + 104, kNTSC[red]},
						{kNTSCPictureStart + 146, kNTSCPictureStart + 154, kNTSC[blue]},
					},
				},
				{
					// Now they start decreasing.
					start: kNTSCTopBlank + 16,
					stop:  kNTSCTopBlank + 17,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 35, kNTSCPictureStart + 43, kNTSC[green]},
						{kNTSCPictureStart + 95, kNTSCPictureStart + 103, kNTSC[red]},
						{kNTSCPictureStart + 145, kNTSCPictureStart + 153, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 17,
					stop:  kNTSCTopBlank + 18,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 33, kNTSCPictureStart + 41, kNTSC[green]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[red]},
						{kNTSCPictureStart + 143, kNTSCPictureStart + 151, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 18,
					stop:  kNTSCTopBlank + 19,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 30, kNTSCPictureStart + 38, kNTSC[green]},
						{kNTSCPictureStart + 90, kNTSCPictureStart + 98, kNTSC[red]},
						{kNTSCPictureStart + 140, kNTSCPictureStart + 148, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 19,
					stop:  kNTSCTopBlank + 20,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 26, kNTSCPictureStart + 34, kNTSC[green]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 94, kNTSC[red]},
						{kNTSCPictureStart + 136, kNTSCPictureStart + 144, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 20,
					stop:  kNTSCTopBlank + 21,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 29, kNTSC[green]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 89, kNTSC[red]},
						{kNTSCPictureStart + 131, kNTSCPictureStart + 139, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 21,
					stop:  kNTSCTopBlank + 22,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 15, kNTSCPictureStart + 23, kNTSC[green]},
						{kNTSCPictureStart + 75, kNTSCPictureStart + 83, kNTSC[red]},
						{kNTSCPictureStart + 125, kNTSCPictureStart + 133, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 22,
					stop:  kNTSCTopBlank + 23,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 8, kNTSCPictureStart + 16, kNTSC[green]},
						{kNTSCPictureStart + 68, kNTSCPictureStart + 76, kNTSC[red]},
						{kNTSCPictureStart + 118, kNTSCPictureStart + 126, kNTSC[blue]},
					},
				},
				{
					// Note that we can't move left 8 so we end up not quite where we started.
					// Also note that comb takes precedence here so we only emit 1 ball pixel
					// as the rest are hidden in hblank.
					start: kNTSCTopBlank + 23,
					stop:  kNTSCTopBlank + 24,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{kNTSCPictureStart + 8, kNTSCPictureStart + 9, kNTSC[green]},
						{kNTSCPictureStart + 61, kNTSCPictureStart + 69, kNTSC[red]},
						{kNTSCPictureStart + 111, kNTSCPictureStart + 119, kNTSC[blue]},
					},
				},
				{
					// No HMOVE so no comb.
					start: kNTSCTopBlank + 24,
					stop:  kNTSCTopBlank + 25,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 9, kNTSC[green]},
						{kNTSCPictureStart + 61, kNTSCPictureStart + 69, kNTSC[red]},
						{kNTSCPictureStart + 111, kNTSCPictureStart + 119, kNTSC[blue]},
					},
				},
			},
		},
		{
			name:   "BallMissileOnMoveOutsideNormalSpec",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth8, 8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3:  {0: ballReset},
				kNTSCTopBlank + 5:  {0: ballOn, 200: ballMoveLeft7},
				kNTSCTopBlank + 6:  {224: hmove}, // Right edge wrap so comb doesn't trigger.
				kNTSCTopBlank + 9:  {0: ballOff},
				kNTSCTopBlank + 10: {148: ballReset}, // Put it in the center.
				kNTSCTopBlank + 11: {0: ballOn, 68: hmove},
				kNTSCTopBlank + 13: {0: ballOff},
				kNTSCTopBlank + 15: {0: ballOn, 8: hmove, 45: hmclr},
				kNTSCTopBlank + 17: {0: ballOff, 200: ballMoveLeft5},
				kNTSCTopBlank + 19: {0: ballOn, 8: hmove, 49: hmclr},
				kNTSCTopBlank + 23: {0: ballOff},
				kNTSCTopBlank + 33: {0: missile0Reset},
				kNTSCTopBlank + 35: {0: missile0On, 200: missile0MoveLeft7},
				kNTSCTopBlank + 36: {224: hmove}, // Right edge wrap so comb doesn't trigger.
				kNTSCTopBlank + 39: {0: missile0Off},
				kNTSCTopBlank + 40: {148: missile0Reset}, // Put it in the center.
				kNTSCTopBlank + 41: {0: missile0On, 68: hmove},
				kNTSCTopBlank + 43: {0: missile0Off},
				kNTSCTopBlank + 45: {0: missile0On, 8: hmove, 45: hmclr},
				kNTSCTopBlank + 47: {0: missile0Off, 200: missile0MoveLeft5},
				kNTSCTopBlank + 49: {0: missile0On, 8: hmove, 49: hmclr},
				kNTSCTopBlank + 53: {0: missile0Off},
				kNTSCTopBlank + 63: {0: missile1Reset},
				kNTSCTopBlank + 65: {0: missile1On, 200: missile1MoveLeft7},
				kNTSCTopBlank + 66: {224: hmove}, // Right edge wrap so comb doesn't trigger.
				kNTSCTopBlank + 69: {0: missile1Off},
				kNTSCTopBlank + 70: {148: missile1Reset}, // Put it in the center.
				kNTSCTopBlank + 71: {0: missile1On, 68: hmove},
				kNTSCTopBlank + 73: {0: missile1Off},
				kNTSCTopBlank + 75: {0: missile1On, 8: hmove, 45: hmclr},
				kNTSCTopBlank + 77: {0: missile1Off, 200: missile1MoveLeft5},
				kNTSCTopBlank + 79: {0: missile1On, 8: hmove, 49: hmclr},
				kNTSCTopBlank + 83: {0: missile1Off},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// First 2 lines show green bar.
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 7,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[green]}},
				},
				{
					// Next 2 should show the ball shifted 15 pixels left which is actually all the way on the right side now due to wrap.
					// We also shouldn't end up with a comb either since extended hblank didn't trigger (well it did and then immediately
					// turned off on wrap).
					start:       kNTSCTopBlank + 7,
					stop:        kNTSCTopBlank + 9,
					horizontals: []horizontal{{kNTSCWidth - 15, kNTSCWidth - 7, kNTSC[green]}},
				},
				{
					// These next 2 shouldn't move. We position in the center and draw then HMOVE but no clocks should roll off.
					// We also shouldn't end up with a comb either since extended hblank didn't trigger.
					start:       kNTSCTopBlank + 11,
					stop:        kNTSCTopBlank + 13,
					horizontals: []horizontal{{152, 160, kNTSC[green]}},
				},
				{
					// This should draw the same bar as before and then again not move it since HMCLR happened
					// on the clock where these would stop (i.e. all bits different).
					// But...the first once still gets the comb.
					start: kNTSCTopBlank + 15,
					stop:  kNTSCTopBlank + 16,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{152, 160, kNTSC[green]},
					},
				},
				{
					start:       kNTSCTopBlank + 16,
					stop:        kNTSCTopBlank + 17,
					horizontals: []horizontal{{152, 160, kNTSC[green]}},
				},
				{
					// Bit more interesting. Start moving but clear register right after we pass
					// the point where it would stop. This means during hblank we shift the block
					// 17 pixels left each time and no comb. On the first line this is a left shift
					// 8 due to the comb and never matching (so one more shift).
					// The 17 happens because there's just enough room for that many H1 clocks
					// inside of normal hblank.
					start: kNTSCTopBlank + 19,
					stop:  kNTSCTopBlank + 20,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{144, 152, kNTSC[green]},
					},
				},
				{
					start:       kNTSCTopBlank + 20,
					stop:        kNTSCTopBlank + 21,
					horizontals: []horizontal{{127, 135, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 21,
					stop:        kNTSCTopBlank + 22,
					horizontals: []horizontal{{110, 118, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 22,
					stop:        kNTSCTopBlank + 23,
					horizontals: []horizontal{{93, 101, kNTSC[green]}},
				},
				{
					// These are exactly like the ball but 30 lines later and missile0 color.
					start:       kNTSCTopBlank + 35,
					stop:        kNTSCTopBlank + 37,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 37,
					stop:        kNTSCTopBlank + 39,
					horizontals: []horizontal{{kNTSCWidth - 15, kNTSCWidth - 7, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 41,
					stop:        kNTSCTopBlank + 43,
					horizontals: []horizontal{{152, 160, kNTSC[red]}},
				},
				{
					start: kNTSCTopBlank + 45,
					stop:  kNTSCTopBlank + 46,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{152, 160, kNTSC[red]},
					},
				},
				{
					start:       kNTSCTopBlank + 46,
					stop:        kNTSCTopBlank + 47,
					horizontals: []horizontal{{152, 160, kNTSC[red]}},
				},
				{
					start: kNTSCTopBlank + 49,
					stop:  kNTSCTopBlank + 50,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{144, 152, kNTSC[red]},
					},
				},
				{
					start:       kNTSCTopBlank + 50,
					stop:        kNTSCTopBlank + 51,
					horizontals: []horizontal{{127, 135, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 51,
					stop:        kNTSCTopBlank + 52,
					horizontals: []horizontal{{110, 118, kNTSC[red]}},
				},
				{
					start:       kNTSCTopBlank + 52,
					stop:        kNTSCTopBlank + 53,
					horizontals: []horizontal{{93, 101, kNTSC[red]}},
				},
				{
					// These are exactly like the ball but 60 lines later and missile1 color.
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 67,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 67,
					stop:        kNTSCTopBlank + 69,
					horizontals: []horizontal{{kNTSCWidth - 15, kNTSCWidth - 7, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 71,
					stop:        kNTSCTopBlank + 73,
					horizontals: []horizontal{{152, 160, kNTSC[blue]}},
				},
				{
					start: kNTSCTopBlank + 75,
					stop:  kNTSCTopBlank + 76,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{152, 160, kNTSC[blue]},
					},
				},
				{
					start:       kNTSCTopBlank + 76,
					stop:        kNTSCTopBlank + 77,
					horizontals: []horizontal{{152, 160, kNTSC[blue]}},
				},
				{
					start: kNTSCTopBlank + 79,
					stop:  kNTSCTopBlank + 80,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack},
						{144, 152, kNTSC[blue]},
					},
				},
				{
					start:       kNTSCTopBlank + 80,
					stop:        kNTSCTopBlank + 81,
					horizontals: []horizontal{{127, 135, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 81,
					stop:        kNTSCTopBlank + 82,
					horizontals: []horizontal{{110, 118, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 82,
					stop:        kNTSCTopBlank + 83,
					horizontals: []horizontal{{93, 101, kNTSC[blue]}},
				},
			},
		},
		{
			name:   "BallOnWidthsChangeVerticalDelay",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			vcallbacks: map[int]func(int, *TIA){
				// Simulate ball control happening in hblank/vblank.
				kNTSCTopBlank - 10: ballVerticalDelay,
				kNTSCTopBlank + 26: player1Set, // Triggers ball delay copies.
				kNTSCTopBlank + 44: player1Set, // Triggers ball delay copies.
			},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth1},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset},
				kNTSCTopBlank + 5:  {0: ballOn},
				kNTSCTopBlank + 10: {9: ballOff},
				kNTSCTopBlank + 20: {0: ballWidth2},
				kNTSCTopBlank + 25: {0: ballOn},
				kNTSCTopBlank + 30: {9: ballOff},
				kNTSCTopBlank + 40: {0: ballWidth4},
				kNTSCTopBlank + 45: {0: ballOn},
				kNTSCTopBlank + 50: {0: ballOff},
				kNTSCTopBlank + 60: {0: ballWidth8},
				kNTSCTopBlank + 65: {0: ballOn},
				kNTSCTopBlank + 70: {0: ballOff},
			},
			scanlines: []scanline{
				{
					// Fill in the columns first.
					start: kNTSCTopBlank,
					stop:  kNTSCOverscanStart,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[red]},
						{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[blue]},
					},
				},
				{
					// Will partially draw the 2 pixel wide one starting a line late but then ignores off.
					start:       kNTSCTopBlank + 26,
					stop:        kNTSCTopBlank + 40,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 82, kNTSC[green]}},
				},
				{
					// Obeys new size but keeps drawing until a new copy happens at line 44 with off state.
					// Ignores the future changes since never gets turned back on.
					start:       kNTSCTopBlank + 40,
					stop:        kNTSCTopBlank + 44,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[green]}},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			done := false
			cnt := 0
			ta, err := setup(t, test.name, TIA_MODE_NTSC, &cnt, &done)
			if err != nil {
				t.Fatalf("%s: can't Init: %v", test.name, err)
			}

			// Write the PF regs.
			ta.Write(PF0, test.pfRegs[0])
			ta.Write(PF1, test.pfRegs[1])
			ta.Write(PF2, test.pfRegs[2])
			// Make playfield reflect and score mode possibly.
			ctrl := kMASK_REF_OFF | kMASK_SCORE_OFF
			if test.reflect {
				ctrl |= kMASK_REF
			}
			if test.score {
				ctrl |= kMASK_SCORE
			}
			ta.Write(CTRLPF, ctrl)

			// Run the actual frame based on the callbacks for when to change rendering.
			runAFrame(t, ta, frameSpec{
				width:       kNTSCWidth,
				height:      kNTSCHeight,
				vsync:       kVSYNCLines,
				vblank:      kNTSCTopBlank,
				overscan:    kNTSCOverscanStart,
				vcallbacks:  test.vcallbacks,
				hvcallbacks: test.hvcallbacks,
			})
			if !done {
				t.Fatalf("%s: didn't trigger a VSYNC?\n%v", test.name, spew.Sdump(ta))
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
			// Loop over each scanline and for that range run each horizontal paint request.
			// This looks worse than it is as in general there are 1-3 horizontals for
			// a given scanline and there's only 192-228 visible of those.
			for _, s := range test.scanlines {
				if s.stop <= s.start || s.start < 0 || s.start > kNTSCHeight || s.stop > kNTSCHeight {
					t.Fatalf("%s: invalid scanline %v in scanlines: %v", test.name, spew.Sdump(s), spew.Sdump(test.scanlines))
				}
				for h := s.start; h < s.stop; h++ {
					for _, hz := range s.horizontals {
						if hz.stop <= hz.start || hz.start < 0 || hz.start > kNTSCWidth || hz.stop > kNTSCWidth {
							t.Fatalf("%s: invalid horizontal %v in scanline: %v", test.name, spew.Sdump(hz), spew.Sdump(s))
						}
						paint(hz.start, hz.stop, h, want, hz.cl)
					}
				}
			}
			if diff := deep.Equal(ta.picture, want); diff != nil {
				// Emit the canonical so we can visually compare if needed.
				generateImage(t, "Error"+test.name, &cnt, &done)(want)

				// Also generate a diff picture.
				d := image.NewNRGBA(image.Rect(0, 0, kNTSCWidth, kNTSCHeight))
				for x := 0; x < kNTSCWidth; x++ {
					for y := 0; y < kNTSCHeight; y++ {
						gotC := ta.picture.At(x, y).(color.NRGBA)
						wantC := want.At(x, y).(color.NRGBA)
						diffC := kBlack
						// Set diff color to bright red always. Setting it to the XOR
						// values makes for some hard to distinguish colors sometimes.
						if ((gotC.R ^ wantC.R) != 0x00) ||
							((gotC.G ^ wantC.G) != 0x00) ||
							((gotC.B ^ wantC.B) != 0x00) {
							diffC = kNTSC[red]
						}
						d.Set(x, y, diffC)
					}
				}
				generateImage(t, "Diff"+test.name, &cnt, &done)(d)
				t.Errorf("%s: pictures differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", test.name, kNTSCWidth, diff)
			}
		})
	}
}

func TestErrorStates(t *testing.T) {
	cnt := 0
	done := false
	// No FrameDone callback should be an error
	if _, err := Init(&TIADef{
		Mode:      TIA_MODE_NTSC,
		FrameDone: nil,
	}); err == nil {
		t.Error("FrameDone was nil, no error?")
	}

	// Invalid mode.
	if _, err := Init(&TIADef{
		Mode:      TIA_MODE_UNIMPLEMENTED,
		FrameDone: generateImage(t, t.Name(), &cnt, &done),
	}); err == nil {
		t.Errorf("Didn't get an error for mode: %v", TIA_MODE_UNIMPLEMENTED)
	}
}

func BenchmarkFrameRender(b *testing.B) {
	done := false
	ta, err := Init(&TIADef{
		Mode: TIA_MODE_NTSC,
		FrameDone: func(i *image.NRGBA) {
			done = true
		},
	})
	if err != nil {
		b.Fatalf("Can't Init: %v", err)
	}
	// Set background to yellow - 0x0F (and left shift it to act as a color value).
	ta.Write(COLUBK, yellow<<1)
	// Set player0 to red (0x1B) and player1 to blue (0x42) and again left shift.
	ta.Write(COLUP0, red<<1)
	ta.Write(COLUP1, blue<<1)
	// Finally set playfield to green (0x5A) and again left shift.
	ta.Write(COLUPF, green<<1)

	// Write the PF regs.
	ta.Write(PF0, 0xFF)
	ta.Write(PF1, 0x00)
	ta.Write(PF2, 0x00)
	// Make playfield reflect and score mode on.
	ta.Write(CTRLPF, kMASK_REF|kMASK_SCORE)

	frame := frameSpec{
		width:    kNTSCWidth,
		height:   kNTSCHeight,
		vsync:    kVSYNCLines,
		vblank:   kNTSCTopBlank,
		overscan: kNTSCOverscanStart,
	}
	n := time.Now()
	const kRuns = 10000
	// Now generate 10,000 frames. Even at 4-5ms per frame that's only 50s or so.
	for i := 0; i < kRuns; i++ {
		done = false
		// Inlined version of runAFrame:

		// Run tick enough times for a frame.
		// Turn on VBLANK and VSYNC
		ta.Write(VBLANK, kMASK_VBL_VBLANK)
		ta.Write(VSYNC, kMASK_VSYNC)
		for i := 0; i < frame.height; i++ {
			// Turn off VSYNC after it's done.
			if i >= frame.vsync && ta.vsync {
				ta.Write(VSYNC, kMASK_VSYNC_OFF)
			}
			// Turn off VBLANK after it's done.
			if i >= frame.vblank && ta.vblank {
				ta.Write(VBLANK, kMASK_VBL_VBLANK_OFF)
			}
			// Turn VBLANK back on at the bottom.
			if i >= frame.overscan {
				ta.Write(VBLANK, kMASK_VBL_VBLANK)
			}
			for j := 0; j < frame.width; j++ {
				if err := ta.Tick(); err != nil {
					b.Fatalf("Error on tick: %v", err)
				}
				ta.TickDone()
			}
		}
		ta.Write(VSYNC, kMASK_VSYNC)
		if err := ta.Tick(); err != nil {
			b.Fatalf("Error on tick: %v", err)
		}
		ta.TickDone()
		if !done {
			b.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
		}
	}
	d := time.Now().Sub(n)
	b.Logf("%d runs at total time %s and %s time per run", kRuns, d, d/kRuns)

}
