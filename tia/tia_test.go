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
	// Turn on VBLANK and VSYNC and run a tick to implement it.
	ta.Write(VSYNC, 0x00)
	if err := ta.Tick(); err != nil {
		t.Fatalf("Error on tick: %v", err)
	}
	ta.TickDone()

	ta.Write(VBLANK, kMASK_VBL_VBLANK)
	ta.Write(VSYNC, kMASK_VSYNC)
	if err := ta.Tick(); err != nil {
		t.Fatalf("Error on tick: %v", err)
	}
	ta.TickDone()

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
	black  = uint8(0x00)
)

var (
	// Missile callbacks for 1,2,4,8 sized missiles. Always sets a single regular player.
	missile0Width1 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_1)
	}
	missile0Width2 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_2)
	}
	missile0Width4 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_4)
	}
	missile0Width8 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMISSILE_WIDTH_8)
	}
	missile1Width1 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_1)
	}
	missile1Width2 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_2)
	}
	missile1Width4 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_4)
	}
	missile1Width8 = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMISSILE_WIDTH_8)
	}

	// Missile movement callbacks
	missile0Move8 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT8)
	}
	missile0Move7 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT7)
	}
	missile0Move6 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT6)
	}
	missile0Move5 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT5)
	}
	missile0Move4 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT4)
	}
	missile0Move3 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT3)
	}
	missile0Move2 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT2)
	}
	missile0Move1 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_RIGHT1)
	}
	missile0MoveNone = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_NONE)
	}
	missile0MoveLeft1 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT1)
	}
	missile0MoveLeft2 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT2)
	}
	missile0MoveLeft3 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT3)
	}
	missile0MoveLeft4 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT4)
	}
	missile0MoveLeft5 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT5)
	}
	missile0MoveLeft6 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT6)
	}
	missile0MoveLeft7 = func(x, y int, ta *TIA) {
		ta.Write(HMM0, kMOVE_LEFT7)
	}
	missile1Move8 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT8)
	}
	missile1Move7 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT7)
	}
	missile1Move6 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT6)
	}
	missile1Move5 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT5)
	}
	missile1Move4 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT4)
	}
	missile1Move3 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT3)
	}
	missile1Move2 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT2)
	}
	missile1Move1 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_RIGHT1)
	}
	missile1MoveNone = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_NONE)
	}
	missile1MoveLeft1 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT1)
	}
	missile1MoveLeft2 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT2)
	}
	missile1MoveLeft3 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT3)
	}
	missile1MoveLeft4 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT4)
	}
	missile1MoveLeft5 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT5)
	}
	missile1MoveLeft6 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT6)
	}
	missile1MoveLeft7 = func(x, y int, ta *TIA) {
		ta.Write(HMM1, kMOVE_LEFT7)
	}

	// Player move callbacks. Just need 1 each
	player0Move8 = func(x, y int, ta *TIA) {
		ta.Write(HMP0, kMOVE_RIGHT8)
	}
	player1Move8 = func(x, y int, ta *TIA) {
		ta.Write(HMP1, kMOVE_RIGHT8)
	}

	// Ball callbacks for 1,2,4,8 sized balls.
	// We always have reflection of playfield and score mode on for the ball tests.
	ballWidth1 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_1|kMASK_REF|kMASK_SCORE)
	}
	ballWidth2 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_2|kMASK_REF|kMASK_SCORE)
	}
	ballWidth4 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_4|kMASK_REF|kMASK_SCORE)
	}
	ballWidth8 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_8|kMASK_REF|kMASK_SCORE)
	}
	// Variants of the above with PFP also enabled.
	ballWidthPFP1 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_1|kMASK_PFP|kMASK_REF|kMASK_SCORE)
	}
	ballWidthPFP2 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_2|kMASK_PFP|kMASK_REF|kMASK_SCORE)
	}
	ballWidthPFP4 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_4|kMASK_PFP|kMASK_REF|kMASK_SCORE)
	}
	ballWidthPFP8 = func(x, y int, ta *TIA) {
		ta.Write(CTRLPF, kBALL_WIDTH_8|kMASK_PFP|kMASK_REF|kMASK_SCORE)
	}

	// Ball movement callbacks
	ballMove8 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT8)
	}
	ballMove7 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT7)
	}
	ballMove6 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT6)
	}
	ballMove5 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT5)
	}
	ballMove4 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT4)
	}
	ballMove3 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT3)
	}
	ballMove2 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT2)
	}
	ballMove1 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_RIGHT1)
	}
	ballMoveNone = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_NONE)
	}
	ballMoveLeft1 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT1)
	}
	ballMoveLeft2 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT2)
	}
	ballMoveLeft3 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT3)
	}
	ballMoveLeft4 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT4)
	}
	ballMoveLeft5 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT5)
	}
	ballMoveLeft6 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT6)
	}
	ballMoveLeft7 = func(x, y int, ta *TIA) {
		ta.Write(HMBL, kMOVE_LEFT7)
	}

	hmclr = func(x, y int, ta *TIA) {
		// Any value strobes it.
		ta.Write(HMCLR, 0x00)
	}

	hmove = func(x, y int, ta *TIA) {
		// Any value strobes it.
		ta.Write(HMOVE, 0x00)
	}

	// Turn the ball on and off.
	ballOn = func(x, y int, ta *TIA) {
		ta.Write(ENABL, kMASK_ENAMB)
	}
	ballOff = func(x, y int, ta *TIA) {
		ta.Write(ENABL, 0x00)
	}

	// Turn the 2 missiles on and off.
	missile0On = func(x, y int, ta *TIA) {
		ta.Write(ENAM0, kMASK_ENAMB)
	}
	missile1On = func(x, y int, ta *TIA) {
		ta.Write(ENAM1, kMASK_ENAMB)
	}
	missile0Off = func(x, y int, ta *TIA) {
		ta.Write(ENAM0, 0x00)
	}
	missile1Off = func(x, y int, ta *TIA) {
		ta.Write(ENAM1, 0x00)
	}

	// Vertical delay on.
	ballVerticalDelay = func(x, y int, ta *TIA) {
		ta.Write(VDELBL, kMASK_VDEL)
	}
	player0VerticalDelay = func(x, y int, ta *TIA) {
		ta.Write(VDELP0, kMASK_VDEL)
	}
	player0VerticalDelayOff = func(x, y int, ta *TIA) {
		ta.Write(VDELP0, 0x00)
	}
	player1VerticalDelay = func(x, y int, ta *TIA) {
		ta.Write(VDELP1, kMASK_VDEL)
	}
	player1VerticalDelayOff = func(x, y int, ta *TIA) {
		ta.Write(VDELP1, 0x00)
	}

	// Reset ball position. Should start painting 4 pixels later than this immmediately.
	ballReset = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESBL, 0x00)
	}

	// Reset missiles position. Should start painting 4 pixels later than this immediately.
	missile0Reset = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESM0, 0x00)
	}
	missile1Reset = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESM1, 0x00)
	}

	// Reset player positions. Should start painting 5 pixels later than this but skip a line.
	player0Reset = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESP0, 0x00)
	}
	player1Reset = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RESP1, 0x00)
	}

	// Set the player1 bitmask which also triggers vertical delay copies for GRP0 and the ball (if VDEL is enabled).
	// Set to all 0's here since otherwise this will paint the player at the expense of the ball since there's no
	// player enable (just whether pixels match).
	// Include a player0 variant as well to turn off player painting.
	player0SetClear = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x00)
	}
	player1SetClear = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x00)
	}

	player0Reflect = func(x, y int, ta *TIA) {
		ta.Write(REFP0, kMASK_REFPX)
	}
	player1Reflect = func(x, y int, ta *TIA) {
		ta.Write(REFP1, kMASK_REFPX)
	}
	player0ReflectClear = func(x, y int, ta *TIA) {
		ta.Write(REFP0, 0x00)
	}
	player1ReflectClear = func(x, y int, ta *TIA) {
		ta.Write(REFP1, 0x00)
	}

	// 2 player sprites which needs to get enabled on successive lines in order to be fully rendered.
	// The player0 version:
	//
	//   **
	//   **
	//  ****
	//  *  *
	//  ****
	// **  **
	// **  **
	//**    **
	//
	// The player1 version is inverted:
	//
	//**    **
	// **  **
	// **  **
	//  ****
	//  *  *
	//  ****
	//   **
	//   **
	player0Line0 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x18)
	}
	player0Line1 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x18)
	}
	player0Line2 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x3C)
	}
	player0Line3 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x24)
	}
	player0Line4 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x3C)
	}
	player0Line5 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x66)
	}
	player0Line6 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x66)
	}
	player0Line7 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0xC3)
	}
	player1Line0 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0xC3)
	}
	player1Line1 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x66)
	}
	player1Line2 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x66)
	}
	player1Line3 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x3C)
	}
	player1Line4 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x24)
	}
	player1Line5 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x3C)
	}
	player1Line6 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x18)
	}
	player1Line7 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x18)
	}

	// A graphic for reflection testing
	//
	// Image:
	//
	//       **
	//      **
	//  *****
	// ****
	// ****
	//  *****
	//      **
	//       **
	player0ReflectLine0 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0xC0)
	}
	player0ReflectLine1 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x60)
	}
	player0ReflectLine2 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x3E)
	}
	player0ReflectLine3 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x0F)
	}
	player0ReflectLine4 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x0F)
	}
	player0ReflectLine5 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x3E)
	}
	player0ReflectLine6 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0x60)
	}
	player0ReflectLine7 = func(x, y int, ta *TIA) {
		ta.Write(GRP0, 0xC0)
	}
	player1ReflectLine0 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0xC0)
	}
	player1ReflectLine1 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x60)
	}
	player1ReflectLine2 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x3E)
	}
	player1ReflectLine3 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x0F)
	}
	player1ReflectLine4 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x0F)
	}
	player1ReflectLine5 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x3E)
	}
	player1ReflectLine6 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0x60)
	}
	player1ReflectLine7 = func(x, y int, ta *TIA) {
		ta.Write(GRP1, 0xC0)
	}

	// Various incarnations of playerX sizing and missile sizing.

	// Single players, various sizes.
	player0Single = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_ONE)
	}
	player1Single = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_ONE)
	}
	player0Double = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_DOUBLE)
	}
	player1Double = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_DOUBLE)
	}
	player0Quad = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_QUAD)
	}
	player1Quad = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_QUAD)
	}

	// 2 close players, different missile widths.
	player0TwoClose1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_1)
	}
	player0TwoClose2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_2)
	}
	player0TwoClose4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_4)
	}
	player0TwoClose8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_8)
	}
	player1TwoClose1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_1)
	}
	player1TwoClose2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_2)
	}
	player1TwoClose4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_4)
	}
	player1TwoClose8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_CLOSE|kMISSILE_WIDTH_8)
	}

	// 2 medium players, different missile widths
	player0TwoMed1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_1)
	}
	player0TwoMed2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_2)
	}
	player0TwoMed4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_4)
	}
	player0TwoMed8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_8)
	}
	player1TwoMed1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_1)
	}
	player1TwoMed2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_2)
	}
	player1TwoMed4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_4)
	}
	player1TwoMed8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_MED|kMISSILE_WIDTH_8)
	}

	// 3 close players, different missile widths.
	player0ThreeClose1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_1)
	}
	player0ThreeClose2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_2)
	}
	player0ThreeClose4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_4)
	}
	player0ThreeClose8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_8)
	}
	player1ThreeClose1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_1)
	}
	player1ThreeClose2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_2)
	}
	player1ThreeClose4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_4)
	}
	player1ThreeClose8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_CLOSE|kMISSILE_WIDTH_8)
	}

	// 2 wide players, different missile widths.
	player0TwoWide1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_1)
	}
	player0TwoWide2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_2)
	}
	player0TwoWide4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_4)
	}
	player0TwoWide8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_8)
	}
	player1TwoWide1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_1)
	}
	player1TwoWide2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_2)
	}
	player1TwoWide4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_4)
	}
	player1TwoWide8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_TWO_WIDE|kMISSILE_WIDTH_8)
	}

	// 3 medium players, different missile widths
	player0ThreeMed1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_1)
	}
	player0ThreeMed2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_2)
	}
	player0ThreeMed4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_4)
	}
	player0ThreeMed8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ0, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_8)
	}
	player1ThreeMed1Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_1)
	}
	player1ThreeMed2Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_2)
	}
	player1ThreeMed4Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_4)
	}
	player1ThreeMed8Missile = func(x, y int, ta *TIA) {
		ta.Write(NUSIZ1, kMASK_NUSIZ_PLAYER_THREE_MED|kMISSILE_WIDTH_8)
	}

	// Reset missileX position to the middle of playerX.
	missile0ResetPlayer = func(x, y int, ta *TIA) {
		ta.Write(RESMP0, kMASK_RESMP)
	}
	missile0ResetPlayerOff = func(x, y int, ta *TIA) {
		ta.Write(RESMP0, 0x00)
	}
	missile1ResetPlayer = func(x, y int, ta *TIA) {
		ta.Write(RESMP1, kMASK_RESMP)
	}
	missile1ResetPlayerOff = func(x, y int, ta *TIA) {
		ta.Write(RESMP1, 0x00)
	}

	rsync = func(x, y int, ta *TIA) {
		// Any value works, including 0's. Just need to hit the address.
		ta.Write(RSYNC, 0x00)
	}
)

func TestDrawing(t *testing.T) {
	// Emit these so it's easier to debug if there's a diff.
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
			name: "BallMissileOnWidthsChangeScreenEdge",
			// No columns on this test to verify edge missiles work.
			pfRegs: [3]uint8{0x00, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
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
				// Use PFP mode so we can detect the ball in the edges vs player colors.
				kNTSCTopBlank:      {0: ballWidthPFP8, 8: missile0Width8, 17: missile1Width8},
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
				// Use PFP mode so we can detect the ball in the edges vs player colors.
				kNTSCTopBlank:      {0: ballWidthPFP8, 8: missile0Width8, 17: missile1Width8},
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
					// Except for this first one where PF wins since we're in score mode and P0 wins over P1.
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 67,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]}},
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
			name:       "BallOnWidthsChangeVerticalDelay",
			pfRegs:     [3]uint8{0xFF, 0x00, 0x00},
			vcallbacks: map[int]func(int, *TIA){},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in vblank.
				kNTSCTopBlank - 10: {0: ballVerticalDelay},
				kNTSCTopBlank:      {0: ballWidth1},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset},
				kNTSCTopBlank + 5:  {0: ballOn},
				kNTSCTopBlank + 10: {9: ballOff},
				kNTSCTopBlank + 20: {0: ballWidth2},
				kNTSCTopBlank + 25: {0: ballOn},
				kNTSCTopBlank + 26: {0: player1SetClear},
				kNTSCTopBlank + 30: {9: ballOff},
				kNTSCTopBlank + 40: {0: ballWidth4},
				kNTSCTopBlank + 44: {0: player1SetClear},
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
		{
			name: "MissileLockedToPlayer",
			// No columns on this test to verify edge missiles work.
			pfRegs: [3]uint8{0x00, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank:       {8: missile0Width8, 17: missile1Width8},
				kNTSCTopBlank + 3:   {0: missile0Reset, 8: missile0On, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 5:   {0: missile0ResetPlayer},
				kNTSCTopBlank + 7:   {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 9:   {0: missile0Off},
				kNTSCTopBlank + 13:  {0: missile1Reset, 8: missile1On, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 15:  {0: missile1ResetPlayer},
				kNTSCTopBlank + 17:  {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 19:  {0: missile1Off},
				kNTSCTopBlank + 23:  {0: missile0Reset, 8: missile0On, 11: player0TwoClose8Missile, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 25:  {0: missile0ResetPlayer},
				kNTSCTopBlank + 27:  {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 29:  {0: missile0Off},
				kNTSCTopBlank + 33:  {0: missile1Reset, 8: missile1On, 11: player1TwoClose8Missile, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 35:  {0: missile1ResetPlayer},
				kNTSCTopBlank + 37:  {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 39:  {0: missile1Off},
				kNTSCTopBlank + 43:  {0: missile0Reset, 8: missile0On, 11: player0TwoMed8Missile, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 45:  {0: missile0ResetPlayer},
				kNTSCTopBlank + 47:  {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 49:  {0: missile0Off},
				kNTSCTopBlank + 53:  {0: missile1Reset, 8: missile1On, 11: player1TwoMed8Missile, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 55:  {0: missile1ResetPlayer},
				kNTSCTopBlank + 57:  {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 59:  {0: missile1Off},
				kNTSCTopBlank + 63:  {0: missile0Reset, 8: missile0On, 11: player0ThreeClose8Missile, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 65:  {0: missile0ResetPlayer},
				kNTSCTopBlank + 67:  {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 69:  {0: missile0Off},
				kNTSCTopBlank + 73:  {0: missile1Reset, 8: missile1On, 11: player1ThreeClose8Missile, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 75:  {0: missile1ResetPlayer},
				kNTSCTopBlank + 77:  {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 79:  {0: missile1Off},
				kNTSCTopBlank + 83:  {0: missile0Reset, 8: missile0On, 11: player0TwoWide8Missile, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 85:  {0: missile0ResetPlayer},
				kNTSCTopBlank + 87:  {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 89:  {0: missile0Off},
				kNTSCTopBlank + 93:  {0: missile1Reset, 8: missile1On, 11: player1TwoWide8Missile, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 95:  {0: missile1ResetPlayer},
				kNTSCTopBlank + 97:  {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 99:  {0: missile1Off},
				kNTSCTopBlank + 103: {0: missile0Reset, 8: missile0On, 11: player0ThreeMed8Missile, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 105: {0: missile0ResetPlayer},
				kNTSCTopBlank + 107: {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 109: {0: missile0Off},
				kNTSCTopBlank + 113: {0: missile1Reset, 8: missile1On, 11: player1ThreeMed8Missile, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 115: {0: missile1ResetPlayer},
				kNTSCTopBlank + 117: {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 119: {0: missile1Off},
				kNTSCTopBlank + 123: {0: missile0Reset, 8: missile0On, 11: player0Double, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 125: {0: missile0ResetPlayer},
				kNTSCTopBlank + 127: {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 129: {0: missile0Off},
				kNTSCTopBlank + 133: {0: missile1Reset, 8: missile1On, 11: player1Double, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 135: {0: missile1ResetPlayer},
				kNTSCTopBlank + 137: {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 139: {0: missile1Off},
				kNTSCTopBlank + 143: {0: missile0Reset, 8: missile0On, 11: player0Quad, kNTSCPictureStart + 76: player0Reset},
				kNTSCTopBlank + 145: {0: missile0ResetPlayer},
				kNTSCTopBlank + 147: {79: missile0ResetPlayerOff},
				kNTSCTopBlank + 149: {0: missile0Off},
				kNTSCTopBlank + 153: {0: missile1Reset, 8: missile1On, 11: player1Quad, kNTSCPictureStart + 76: player1Reset},
				kNTSCTopBlank + 155: {0: missile1ResetPlayer},
				kNTSCTopBlank + 157: {79: missile1ResetPlayerOff},
				kNTSCTopBlank + 159: {0: missile1Off},
			},
			scanlines: []scanline{
				{
					// A regular 8 width missile should show up (single copy).
					start:       kNTSCTopBlank + 3,
					stop:        kNTSCTopBlank + 5,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]}},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start.
					start:       kNTSCTopBlank + 7,
					stop:        kNTSCTopBlank + 9,
					horizontals: []horizontal{{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]}},
				},
				{
					// Same thing for missile1 as a single copy.
					start:       kNTSCTopBlank + 13,
					stop:        kNTSCTopBlank + 15,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 17,
					stop:        kNTSCTopBlank + 19,
					horizontals: []horizontal{{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]}},
				},
				{
					// A regular 8 width missile should show up with 2 copies (close).
					start: kNTSCTopBlank + 23,
					stop:  kNTSCTopBlank + 25,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 16, kNTSCPictureStart + 24, kNTSC[red]},
					},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start with 2 copies again.
					start: kNTSCTopBlank + 27,
					stop:  kNTSCTopBlank + 29,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[red]},
					},
				},
				{
					// Same thing for missile1 with 2 copies (close).
					start: kNTSCTopBlank + 33,
					stop:  kNTSCTopBlank + 35,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]},
						{kNTSCPictureStart + 16, kNTSCPictureStart + 24, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 37,
					stop:  kNTSCTopBlank + 39,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[blue]},
					},
				},
				{
					// A regular 8 width missile should show up with 2 copies (med).
					start: kNTSCTopBlank + 43,
					stop:  kNTSCTopBlank + 45,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[red]},
					},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start with 2 copies again.
					start: kNTSCTopBlank + 47,
					stop:  kNTSCTopBlank + 49,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[red]},
					},
				},
				{
					// Same thing for missile1 with 2 copies (med).
					start: kNTSCTopBlank + 53,
					stop:  kNTSCTopBlank + 55,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 57,
					stop:  kNTSCTopBlank + 59,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[blue]},
					},
				},
				{
					// A regular 8 width missile should show up with 3 copies (close).
					start: kNTSCTopBlank + 63,
					stop:  kNTSCTopBlank + 65,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 16, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[red]},
					},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start with 2 copies again.
					start: kNTSCTopBlank + 67,
					stop:  kNTSCTopBlank + 69,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[red]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[red]},
					},
				},
				{
					// Same thing for missile1 with 3 copies (close).
					start: kNTSCTopBlank + 73,
					stop:  kNTSCTopBlank + 75,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]},
						{kNTSCPictureStart + 16, kNTSCPictureStart + 24, kNTSC[blue]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 77,
					stop:  kNTSCTopBlank + 79,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 108, kNTSC[blue]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[blue]},
					},
				},
				{
					// A regular 8 width missile should show up with 2 copies (wide).
					start: kNTSCTopBlank + 83,
					stop:  kNTSCTopBlank + 85,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 72, kNTSC[red]},
					},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start with 2 copies again.
					start: kNTSCTopBlank + 87,
					stop:  kNTSCTopBlank + 89,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]},
						{kNTSCPictureStart + 148, kNTSCPictureStart + 156, kNTSC[red]},
					},
				},
				{
					// Same thing for missile1 with 2 copies (wide).
					start: kNTSCTopBlank + 93,
					stop:  kNTSCTopBlank + 95,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 72, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 97,
					stop:  kNTSCTopBlank + 99,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 148, kNTSCPictureStart + 156, kNTSC[blue]},
					},
				},
				{
					// A regular 8 width missile should show up with 3 copies (med).
					start: kNTSCTopBlank + 103,
					stop:  kNTSCTopBlank + 105,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[red]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 72, kNTSC[red]},
					},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 4 over from the player start with 2 copies again.
					start: kNTSCTopBlank + 107,
					stop:  kNTSCTopBlank + 109,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[red]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[red]},
						{kNTSCPictureStart + 148, kNTSCPictureStart + 156, kNTSC[red]},
					},
				},
				{
					// Same thing for missile1 with 3 copies (med).
					start: kNTSCTopBlank + 113,
					stop:  kNTSCTopBlank + 115,
					horizontals: []horizontal{
						{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[blue]},
						{kNTSCPictureStart + 32, kNTSCPictureStart + 40, kNTSC[blue]},
						{kNTSCPictureStart + 64, kNTSCPictureStart + 72, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 117,
					stop:  kNTSCTopBlank + 119,
					horizontals: []horizontal{
						{kNTSCPictureStart + 84, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 116, kNTSCPictureStart + 124, kNTSC[blue]},
						{kNTSCPictureStart + 148, kNTSCPictureStart + 156, kNTSC[blue]},
					},
				},
				{
					// A regular 1 width missile should show up (single copy) but player is doubled.
					start:       kNTSCTopBlank + 123,
					stop:        kNTSCTopBlank + 125,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[red]}},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 9 (resets on both counter 4 clocks) over from the player start.
					start:       kNTSCTopBlank + 127,
					stop:        kNTSCTopBlank + 129,
					horizontals: []horizontal{{kNTSCPictureStart + 89, kNTSCPictureStart + 90, kNTSC[red]}},
				},
				{
					// Same thing for missile1 as a single copy with doubled player.
					start:       kNTSCTopBlank + 133,
					stop:        kNTSCTopBlank + 135,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 137,
					stop:        kNTSCTopBlank + 139,
					horizontals: []horizontal{{kNTSCPictureStart + 89, kNTSCPictureStart + 90, kNTSC[blue]}},
				},
				{
					// A regular 1 width missile should show up (single copy) but player is quad.
					start:       kNTSCTopBlank + 143,
					stop:        kNTSCTopBlank + 145,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[red]}},
				},
				{
					// Now it should disappear until we disable locking here. Then it should be 19 (resets on all counter 4 clocks) over from the player start.
					start:       kNTSCTopBlank + 147,
					stop:        kNTSCTopBlank + 149,
					horizontals: []horizontal{{kNTSCPictureStart + 97, kNTSCPictureStart + 98, kNTSC[red]}},
				},
				{
					// Same thing for missile1 as a single copy with doubled player.
					start:       kNTSCTopBlank + 153,
					stop:        kNTSCTopBlank + 155,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[blue]}},
				},
				{
					start:       kNTSCTopBlank + 157,
					stop:        kNTSCTopBlank + 159,
					horizontals: []horizontal{{kNTSCPictureStart + 97, kNTSCPictureStart + 98, kNTSC[blue]}},
				},
			},
		},
		{
			name: "PlayerDraws",
			// No columns on this test to verify drawing works.
			pfRegs: [3]uint8{0x00, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank: {0: player0Single, 8: player1Single, 10: player0ReflectClear, 12: player1ReflectClear},
				// For one line set the pixels right after reset to verify main image doesn't paint on that line.
				kNTSCTopBlank + 3:  {0: player0Reset, 1: player0Line0, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line0},
				kNTSCTopBlank + 4:  {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 5:  {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 6:  {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 7:  {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 8:  {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 9:  {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 10: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 11: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 12: {0: player0SetClear, 8: player1SetClear, 10: player0TwoClose1Missile, 11: player1TwoClose1Missile},
				kNTSCTopBlank + 14: {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 15: {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 16: {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 17: {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 18: {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 19: {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 20: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 21: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 22: {0: player0SetClear, 8: player1SetClear, 10: player0Double, 11: player1Double},
				kNTSCTopBlank + 24: {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 25: {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 26: {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 27: {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 28: {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 29: {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 30: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 31: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 32: {0: player0SetClear, 8: player1SetClear, 10: player0Quad, 11: player1Quad},
				kNTSCTopBlank + 34: {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 35: {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 36: {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 37: {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 38: {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 39: {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 40: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 41: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 42: {0: player0SetClear, 8: player1SetClear, 10: player0Single, 11: player1Quad},
				kNTSCTopBlank + 44: {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 45: {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 46: {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 47: {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 48: {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 49: {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 50: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 51: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 52: {0: player0SetClear, 8: player1SetClear, 10: player0TwoClose1Missile, 11: player1TwoClose1Missile},
				kNTSCTopBlank + 54: {0: player0Reset, 1: player0Line0, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line0},
				kNTSCTopBlank + 55: {0: player0Reset, 1: player0Line1, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line1},
				kNTSCTopBlank + 56: {0: player0Reset, 1: player0Line2, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line2},
				kNTSCTopBlank + 57: {0: player0Reset, 1: player0Line3, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line3},
				kNTSCTopBlank + 58: {0: player0Reset, 1: player0Line4, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line4},
				kNTSCTopBlank + 59: {0: player0Reset, 1: player0Line5, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line5},
				kNTSCTopBlank + 60: {0: player0Reset, 1: player0Line6, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line6},
				kNTSCTopBlank + 61: {0: player0Reset, 1: player0Line7, kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line7},
				kNTSCTopBlank + 62: {0: player0SetClear, 8: player1SetClear, 10: player0TwoClose1Missile, 11: player1TwoClose1Missile},
				kNTSCTopBlank + 64: {0: player0ReflectLine0, 8: player1ReflectLine0, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 65: {0: player0ReflectLine1, 8: player1ReflectLine1, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 66: {0: player0ReflectLine2, 8: player1ReflectLine2, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 67: {0: player0ReflectLine3, 8: player1ReflectLine3, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 68: {0: player0ReflectLine4, 8: player1ReflectLine4, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 69: {0: player0ReflectLine5, 8: player1ReflectLine5, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 70: {0: player0ReflectLine6, 8: player1ReflectLine6, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 71: {0: player0ReflectLine7, 8: player1ReflectLine7, kNTSCPictureStart + 15: player0Reflect, kNTSCPictureStart + 95: player1Reflect, kNTSCPictureStart + 150: player0ReflectClear, kNTSCPictureStart + 151: player1ReflectClear},
				kNTSCTopBlank + 72: {0: player0SetClear, 8: player1SetClear, 10: player0Single, 11: player1Single},
				kNTSCTopBlank + 74: {0: player0Line0, 8: player1Line0, kNTSCPictureStart + 5: player0ReflectLine0, kNTSCPictureStart + 85: player1ReflectLine1},
				kNTSCTopBlank + 75: {0: player0SetClear, 8: player1SetClear, 10: player0Single, 11: player1Single},
				kNTSCTopBlank + 80: {0: player0Line0, 1: player1SetClear, 2: player0ReflectLine0},
				// Load it again after vertical delay to prove it doesn't change old (which we're drawing now).
				kNTSCTopBlank + 81: {0: player0VerticalDelay, 2: player0ReflectLine0},
				kNTSCTopBlank + 82: {kNTSCPictureStart + 4: player0VerticalDelayOff},
				kNTSCTopBlank + 83: {0: player1Line0, 1: player0SetClear, 2: player1ReflectLine2},
				// Load it again after vertical delay to prove it doesn't change old (which we're drawing now).
				kNTSCTopBlank + 84:  {0: player1VerticalDelay, 2: player1ReflectLine2},
				kNTSCTopBlank + 85:  {kNTSCPictureStart + 84: player1VerticalDelayOff},
				kNTSCTopBlank + 86:  {0: player0SetClear, 8: player1SetClear, 90: player0Move8, 91: player1Move8},
				kNTSCTopBlank + 87:  {8: hmove},
				kNTSCTopBlank + 92:  {10: player0TwoClose1Missile, 11: player1TwoClose1Missile},
				kNTSCTopBlank + 94:  {0: player0Line0, 8: player1Line0},
				kNTSCTopBlank + 95:  {0: player0Line1, 8: player1Line1},
				kNTSCTopBlank + 96:  {0: player0Line2, 8: player1Line2},
				kNTSCTopBlank + 97:  {0: player0Line3, 8: player1Line3},
				kNTSCTopBlank + 98:  {0: player0Line4, 8: player1Line4},
				kNTSCTopBlank + 99:  {0: player0Line5, 8: player1Line5},
				kNTSCTopBlank + 100: {0: player0Line6, 8: player1Line6},
				kNTSCTopBlank + 101: {0: player0Line7, 8: player1Line7},
				kNTSCTopBlank + 102: {0: player0SetClear, 8: player1SetClear},
			},
			scanlines: []scanline{
				{
					// Each character will appear over the next 8 lines.
					// Even though resets appear normal remmber players shift by 1 more pixel.
					// So a reset in HBLANK means painting from Start+1 to Start+8, etc.
					start: kNTSCTopBlank + 4,
					stop:  kNTSCTopBlank + 5,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 83, kNTSC[blue]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 89, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 5,
					stop:  kNTSCTopBlank + 6,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 6,
					stop:  kNTSCTopBlank + 7,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 7,
					stop:  kNTSCTopBlank + 8,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 8,
					stop:  kNTSCTopBlank + 9,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 87, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 9,
					stop:  kNTSCTopBlank + 10,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 10,
					stop:  kNTSCTopBlank + 11,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 86, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 11,
					stop:  kNTSCTopBlank + 12,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 3, kNTSC[red]},
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 86, kNTSC[blue]},
					},
				},
				{
					// Same as above with 2 copies (close).
					start: kNTSCTopBlank + 14,
					stop:  kNTSCTopBlank + 15,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 20, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 83, kNTSC[blue]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 89, kNTSC[blue]},
						{kNTSCPictureStart + 97, kNTSCPictureStart + 99, kNTSC[blue]},
						{kNTSCPictureStart + 103, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 15,
					stop:  kNTSCTopBlank + 16,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 20, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 16,
					stop:  kNTSCTopBlank + 17,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 19, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 17,
					stop:  kNTSCTopBlank + 18,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 19, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 18,
					stop:  kNTSCTopBlank + 19,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 19, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 84, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 19,
					stop:  kNTSCTopBlank + 20,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 20,
					stop:  kNTSCTopBlank + 21,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 86, kNTSC[blue]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 102, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 21,
					stop:  kNTSCTopBlank + 22,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 3, kNTSC[red]},
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 17, kNTSCPictureStart + 19, kNTSC[red]},
						{kNTSCPictureStart + 23, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 84, kNTSCPictureStart + 86, kNTSC[blue]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 102, kNTSC[blue]},
					},
				},
				{
					// Same as first (single copy) but double width. No need to test other
					// NUSIZx versions as missile tests did all of them.
					start: kNTSCTopBlank + 24,
					stop:  kNTSCTopBlank + 25,
					horizontals: []horizontal{
						{kNTSCPictureStart + 7, kNTSCPictureStart + 11, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 85, kNTSC[blue]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 97, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 25,
					stop:  kNTSCTopBlank + 26,
					horizontals: []horizontal{
						{kNTSCPictureStart + 7, kNTSCPictureStart + 11, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 95, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 26,
					stop:  kNTSCTopBlank + 27,
					horizontals: []horizontal{
						{kNTSCPictureStart + 5, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 83, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 95, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 27,
					stop:  kNTSCTopBlank + 28,
					horizontals: []horizontal{
						{kNTSCPictureStart + 5, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 11, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 28,
					stop:  kNTSCTopBlank + 29,
					horizontals: []horizontal{
						{kNTSCPictureStart + 5, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 93, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 29,
					stop:  kNTSCTopBlank + 30,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 11, kNTSCPictureStart + 15, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 30,
					stop:  kNTSCTopBlank + 31,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 11, kNTSCPictureStart + 15, kNTSC[red]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 91, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 31,
					stop:  kNTSCTopBlank + 32,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 5, kNTSC[red]},
						{kNTSCPictureStart + 13, kNTSCPictureStart + 17, kNTSC[red]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 91, kNTSC[blue]},
					},
				},
				{
					// Same as first (single copy) but quad width.
					start: kNTSCTopBlank + 34,
					stop:  kNTSCTopBlank + 35,
					horizontals: []horizontal{
						{kNTSCPictureStart + 13, kNTSCPictureStart + 21, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 89, kNTSC[blue]},
						{kNTSCPictureStart + 105, kNTSCPictureStart + 113, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 35,
					stop:  kNTSCTopBlank + 36,
					horizontals: []horizontal{
						{kNTSCPictureStart + 13, kNTSCPictureStart + 21, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 109, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 36,
					stop:  kNTSCTopBlank + 37,
					horizontals: []horizontal{
						{kNTSCPictureStart + 9, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 109, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 37,
					stop:  kNTSCTopBlank + 38,
					horizontals: []horizontal{
						{kNTSCPictureStart + 9, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 38,
					stop:  kNTSCTopBlank + 39,
					horizontals: []horizontal{
						{kNTSCPictureStart + 9, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 39,
					stop:  kNTSCTopBlank + 40,
					horizontals: []horizontal{
						{kNTSCPictureStart + 5, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 29, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 40,
					stop:  kNTSCTopBlank + 41,
					horizontals: []horizontal{
						{kNTSCPictureStart + 5, kNTSCPictureStart + 13, kNTSC[red]},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 29, kNTSC[red]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 41,
					stop:  kNTSCTopBlank + 42,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 25, kNTSCPictureStart + 33, kNTSC[red]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[blue]},
					},
				},
				{
					// Same as first (single copy) but quad width player0. Make sure we didn't tie these together.
					start: kNTSCTopBlank + 44,
					stop:  kNTSCTopBlank + 45,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 89, kNTSC[blue]},
						{kNTSCPictureStart + 105, kNTSCPictureStart + 113, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 45,
					stop:  kNTSCTopBlank + 46,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 109, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 46,
					stop:  kNTSCTopBlank + 47,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 109, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 47,
					stop:  kNTSCTopBlank + 48,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 48,
					stop:  kNTSCTopBlank + 49,
					horizontals: []horizontal{
						{kNTSCPictureStart + 3, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 93, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 49,
					stop:  kNTSCTopBlank + 50,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 50,
					stop:  kNTSCTopBlank + 51,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 4, kNTSC[red]},
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 51,
					stop:  kNTSCTopBlank + 52,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 3, kNTSC[red]},
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 93, kNTSCPictureStart + 101, kNTSC[blue]},
					},
				},
				{
					// We're setup for 2 copies here but resetting in front of the main one which should be suppressed.
					start: kNTSCTopBlank + 54,
					stop:  kNTSCTopBlank + 55,
					horizontals: []horizontal{
						{kNTSCPictureStart + 20, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 97, kNTSCPictureStart + 99, kNTSC[blue]},
						{kNTSCPictureStart + 103, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 55,
					stop:  kNTSCTopBlank + 56,
					horizontals: []horizontal{
						{kNTSCPictureStart + 20, kNTSCPictureStart + 22, kNTSC[red]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 56,
					stop:  kNTSCTopBlank + 57,
					horizontals: []horizontal{
						{kNTSCPictureStart + 19, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 57,
					stop:  kNTSCTopBlank + 58,
					horizontals: []horizontal{
						{kNTSCPictureStart + 19, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 58,
					stop:  kNTSCTopBlank + 59,
					horizontals: []horizontal{
						{kNTSCPictureStart + 19, kNTSCPictureStart + 23, kNTSC[red]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 100, kNTSC[blue]},
						{kNTSCPictureStart + 102, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 59,
					stop:  kNTSCTopBlank + 60,
					horizontals: []horizontal{
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 103, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 60,
					stop:  kNTSCTopBlank + 61,
					horizontals: []horizontal{
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 22, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 102, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 61,
					stop:  kNTSCTopBlank + 62,
					horizontals: []horizontal{
						{kNTSCPictureStart + 17, kNTSCPictureStart + 19, kNTSC[red]},
						{kNTSCPictureStart + 23, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 100, kNTSCPictureStart + 102, kNTSC[blue]},
					},
				},
				{
					// 2 copies but reflected versions
					start: kNTSCTopBlank + 64,
					stop:  kNTSCTopBlank + 65,
					horizontals: []horizontal{
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 17, kNTSCPictureStart + 19, kNTSC[red]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 89, kNTSC[blue]},
						{kNTSCPictureStart + 97, kNTSCPictureStart + 99, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 65,
					stop:  kNTSCTopBlank + 66,
					horizontals: []horizontal{
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 66,
					stop:  kNTSCTopBlank + 67,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 19, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 67,
					stop:  kNTSCTopBlank + 68,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 5, kNTSC[red]},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 85, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 68,
					stop:  kNTSCTopBlank + 69,
					horizontals: []horizontal{
						{kNTSCPictureStart + 1, kNTSCPictureStart + 5, kNTSC[red]},
						{kNTSCPictureStart + 21, kNTSCPictureStart + 25, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 85, kNTSC[blue]},
						{kNTSCPictureStart + 101, kNTSCPictureStart + 105, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 69,
					stop:  kNTSCTopBlank + 70,
					horizontals: []horizontal{
						{kNTSCPictureStart + 2, kNTSCPictureStart + 7, kNTSC[red]},
						{kNTSCPictureStart + 19, kNTSCPictureStart + 24, kNTSC[red]},
						{kNTSCPictureStart + 82, kNTSCPictureStart + 87, kNTSC[blue]},
						{kNTSCPictureStart + 99, kNTSCPictureStart + 104, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 70,
					stop:  kNTSCTopBlank + 71,
					horizontals: []horizontal{
						{kNTSCPictureStart + 6, kNTSCPictureStart + 8, kNTSC[red]},
						{kNTSCPictureStart + 18, kNTSCPictureStart + 20, kNTSC[red]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
						{kNTSCPictureStart + 98, kNTSCPictureStart + 100, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 71,
					stop:  kNTSCTopBlank + 72,
					horizontals: []horizontal{
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 17, kNTSCPictureStart + 19, kNTSC[red]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 89, kNTSC[blue]},
						{kNTSCPictureStart + 97, kNTSCPictureStart + 99, kNTSC[blue]},
					},
				},
				{
					// Basic test that changing graphics mid paint shows up as expected.
					start: kNTSCTopBlank + 74,
					stop:  kNTSCTopBlank + 75,
					horizontals: []horizontal{
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
						{kNTSCPictureStart + 81, kNTSCPictureStart + 83, kNTSC[blue]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[blue]},
					},
				},
				{
					// Vertical delay test to ensure old and new registers picked up as expected.
					start: kNTSCTopBlank + 80,
					stop:  kNTSCTopBlank + 81,
					horizontals: []horizontal{
						// Current "new" copy in P0.
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
					},
				},
				{
					start: kNTSCTopBlank + 81,
					stop:  kNTSCTopBlank + 82,
					horizontals: []horizontal{
						// "old" copy from original sets.
						{kNTSCPictureStart + 4, kNTSCPictureStart + 6, kNTSC[red]},
					},
				},
				{
					start: kNTSCTopBlank + 82,
					stop:  kNTSCTopBlank + 83,
					horizontals: []horizontal{
						// Change delay mid paint should bring "new" back in.
						{kNTSCPictureStart + 4, kNTSCPictureStart + 5, kNTSC[red]},
						{kNTSCPictureStart + 7, kNTSCPictureStart + 9, kNTSC[red]},
					},
				},
				{
					// Player 1 version of Vertical delay test.
					start: kNTSCTopBlank + 83,
					stop:  kNTSCTopBlank + 84,
					horizontals: []horizontal{
						// Current "new" copy in P0.
						{kNTSCPictureStart + 82, kNTSCPictureStart + 87, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 84,
					stop:  kNTSCTopBlank + 85,
					horizontals: []horizontal{
						// "old" copy from original sets.
						{kNTSCPictureStart + 81, kNTSCPictureStart + 83, kNTSC[blue]},
						{kNTSCPictureStart + 87, kNTSCPictureStart + 89, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 85,
					stop:  kNTSCTopBlank + 86,
					horizontals: []horizontal{
						// Change delay mid paint should bring "new" back in.
						{kNTSCPictureStart + 81, kNTSCPictureStart + 83, kNTSC[blue]},
						{kNTSCPictureStart + 85, kNTSCPictureStart + 87, kNTSC[blue]},
					},
				},
				{
					// Same as 2 copies (close) but with an HMOVE.
					// One comb line due to HBLANK.
					start:       kNTSCTopBlank + 87,
					stop:        kNTSCTopBlank + 88,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kBlack}},
				},
				{
					start: kNTSCTopBlank + 94,
					stop:  kNTSCTopBlank + 95,
					horizontals: []horizontal{
						{kNTSCPictureStart + 12, kNTSCPictureStart + 14, kNTSC[red]},
						{kNTSCPictureStart + 28, kNTSCPictureStart + 30, kNTSC[red]},
						{kNTSCPictureStart + 89, kNTSCPictureStart + 91, kNTSC[blue]},
						{kNTSCPictureStart + 95, kNTSCPictureStart + 97, kNTSC[blue]},
						{kNTSCPictureStart + 105, kNTSCPictureStart + 107, kNTSC[blue]},
						{kNTSCPictureStart + 111, kNTSCPictureStart + 113, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 95,
					stop:  kNTSCTopBlank + 96,
					horizontals: []horizontal{
						{kNTSCPictureStart + 12, kNTSCPictureStart + 14, kNTSC[red]},
						{kNTSCPictureStart + 28, kNTSCPictureStart + 30, kNTSC[red]},
						{kNTSCPictureStart + 90, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 94, kNTSCPictureStart + 96, kNTSC[blue]},
						{kNTSCPictureStart + 106, kNTSCPictureStart + 108, kNTSC[blue]},
						{kNTSCPictureStart + 110, kNTSCPictureStart + 112, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 96,
					stop:  kNTSCTopBlank + 97,
					horizontals: []horizontal{
						{kNTSCPictureStart + 11, kNTSCPictureStart + 15, kNTSC[red]},
						{kNTSCPictureStart + 27, kNTSCPictureStart + 31, kNTSC[red]},
						{kNTSCPictureStart + 90, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 94, kNTSCPictureStart + 96, kNTSC[blue]},
						{kNTSCPictureStart + 106, kNTSCPictureStart + 108, kNTSC[blue]},
						{kNTSCPictureStart + 110, kNTSCPictureStart + 112, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 97,
					stop:  kNTSCTopBlank + 98,
					horizontals: []horizontal{
						{kNTSCPictureStart + 11, kNTSCPictureStart + 12, kNTSC[red]},
						{kNTSCPictureStart + 14, kNTSCPictureStart + 15, kNTSC[red]},
						{kNTSCPictureStart + 27, kNTSCPictureStart + 28, kNTSC[red]},
						{kNTSCPictureStart + 30, kNTSCPictureStart + 31, kNTSC[red]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 95, kNTSC[blue]},
						{kNTSCPictureStart + 107, kNTSCPictureStart + 111, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 98,
					stop:  kNTSCTopBlank + 99,
					horizontals: []horizontal{
						{kNTSCPictureStart + 11, kNTSCPictureStart + 15, kNTSC[red]},
						{kNTSCPictureStart + 27, kNTSCPictureStart + 31, kNTSC[red]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 92, kNTSC[blue]},
						{kNTSCPictureStart + 94, kNTSCPictureStart + 95, kNTSC[blue]},
						{kNTSCPictureStart + 107, kNTSCPictureStart + 108, kNTSC[blue]},
						{kNTSCPictureStart + 110, kNTSCPictureStart + 111, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 99,
					stop:  kNTSCTopBlank + 100,
					horizontals: []horizontal{
						{kNTSCPictureStart + 10, kNTSCPictureStart + 12, kNTSC[red]},
						{kNTSCPictureStart + 14, kNTSCPictureStart + 16, kNTSC[red]},
						{kNTSCPictureStart + 26, kNTSCPictureStart + 28, kNTSC[red]},
						{kNTSCPictureStart + 30, kNTSCPictureStart + 32, kNTSC[red]},
						{kNTSCPictureStart + 91, kNTSCPictureStart + 95, kNTSC[blue]},
						{kNTSCPictureStart + 107, kNTSCPictureStart + 111, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 100,
					stop:  kNTSCTopBlank + 101,
					horizontals: []horizontal{
						{kNTSCPictureStart + 10, kNTSCPictureStart + 12, kNTSC[red]},
						{kNTSCPictureStart + 14, kNTSCPictureStart + 16, kNTSC[red]},
						{kNTSCPictureStart + 26, kNTSCPictureStart + 28, kNTSC[red]},
						{kNTSCPictureStart + 30, kNTSCPictureStart + 32, kNTSC[red]},
						{kNTSCPictureStart + 92, kNTSCPictureStart + 94, kNTSC[blue]},
						{kNTSCPictureStart + 108, kNTSCPictureStart + 110, kNTSC[blue]},
					},
				},
				{
					start: kNTSCTopBlank + 101,
					stop:  kNTSCTopBlank + 102,
					horizontals: []horizontal{
						{kNTSCPictureStart + 9, kNTSCPictureStart + 11, kNTSC[red]},
						{kNTSCPictureStart + 15, kNTSCPictureStart + 17, kNTSC[red]},
						{kNTSCPictureStart + 25, kNTSCPictureStart + 27, kNTSC[red]},
						{kNTSCPictureStart + 31, kNTSCPictureStart + 33, kNTSC[red]},
						{kNTSCPictureStart + 92, kNTSCPictureStart + 94, kNTSC[blue]},
						{kNTSCPictureStart + 108, kNTSCPictureStart + 110, kNTSC[blue]},
					},
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

	// Now setup cleanly.
	ta, err := setup(t, t.Name(), TIA_MODE_NTSC, &cnt, &done)
	if err != nil {
		t.Errorf("Can't Init: %v", err)
	}
	// Calling Tick twice should give an error.
	if err := ta.Tick(); err != nil {
		t.Errorf("Error on tick: %v", err)
	}
	if err := ta.Tick(); err == nil {
		t.Error("Didn't get an error on 2 ticks in a row?")
	}

	// These next few aren't technically errors but they are undefined state.

	// Addresses 2D-3F aren't defined for writes and shouldn't change the TIA state.
	origTIA := ta
	for i := uint16(0x2D); i <= 0x3F; i++ {
		ta.Write(i, 0xFF)
		if diff := deep.Equal(origTIA, ta); diff != nil {
			t.Errorf("At write address %.4X TIA state changed unexpectedly: %v", i, diff)
		}
	}

	// Addresses E and F aren't defined for reads.
	for i := uint16(0x0E); i <= 0x0F; i++ {
		if got, want := ta.Read(i), kMASK_READ_OUTPUT; got != want {
			t.Errorf("At read address %.4X read %.2X instead of %.2X as expected", i, got, want)
		}
		if diff := deep.Equal(origTIA, ta); diff != nil {
			t.Errorf("At read address %.4X TIA state changed unexpectedly: %v", i, diff)
		}
	}

}

func TestRsync(t *testing.T) {
	// This is similar to TestDrawing but we're checking state holds over
	// between frames which isn't possible in the previous harness. No reason
	// to extend it to handle that for a one-shot case.
	done := false
	cnt := 0
	ta, err := setup(t, "", TIA_MODE_NTSC, &cnt, &done)
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	// Write the PF regs.
	ta.Write(PF0, 0xFF)
	ta.Write(PF1, 0x00)
	ta.Write(PF2, 0x00)
	// Make playfield reflect and score mode.
	ta.Write(CTRLPF, kMASK_REF|kMASK_SCORE)

	hvcallbacks := map[int]map[int]func(int, int, *TIA){
		kNTSCTopBlank:      {0: ballWidth8},
		kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset},
		kNTSCTopBlank + 5:  {0: ballOn},
		kNTSCTopBlank + 10: {9: ballOff},
	}

	// Run the actual frame based on the callbacks for when to change rendering.
	runAFrame(t, ta, frameSpec{
		width:       kNTSCWidth,
		height:      kNTSCHeight,
		vsync:       kVSYNCLines,
		vblank:      kNTSCTopBlank,
		overscan:    kNTSCOverscanStart,
		hvcallbacks: hvcallbacks,
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
	scanlines := []scanline{
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
			// Green column for 5 lines.
			start:       kNTSCTopBlank + 5,
			stop:        kNTSCTopBlank + 10,
			horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]}},
		},
	}
	drawWant := func() {
		// Loop over each scanline and for that range run each horizontal paint request.
		for _, s := range scanlines {
			if s.stop <= s.start || s.start < 0 || s.start > kNTSCHeight || s.stop > kNTSCHeight {
				t.Fatalf("Invalid scanline %v in scanlines: %v", spew.Sdump(s), spew.Sdump(scanlines))
			}
			for h := s.start; h < s.stop; h++ {
				for _, hz := range s.horizontals {
					if hz.stop <= hz.start || hz.start < 0 || hz.start > kNTSCWidth || hz.stop > kNTSCWidth {
						t.Fatalf("Invalid horizontal %v in scanline: %v", spew.Sdump(hz), spew.Sdump(s))
					}
					paint(hz.start, hz.stop, h, want, hz.cl)
				}
			}
		}
	}
	df := func() {
		if diff := deep.Equal(ta.picture, want); diff != nil {
			// Emit the canonical so we can visually compare if needed.
			generateImage(t, "Error"+t.Name(), &cnt, &done)(want)
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
			generateImage(t, "Diff"+t.Name(), &cnt, &done)(d)
			t.Errorf("Pictures %d differ. For image data divide by 4 to get a pixel offset and then by %d to get row\n%v", cnt, kNTSCWidth, diff)
		}
	}

	drawWant()
	df()

	// Render another frame with RSYNC on some lines that should leave previous frame in place.
	// This also loses cycles and moves sprites and as a side effect hvcallbacks indexes are off as well after that.
	done = false
	cnt++
	hvcallbacks[kNTSCTopBlank][0] = ballWidth4
	// This should cause a ball to paint on line 46 from 80-83 and pixels 84-87 remain from previous frame (RSYNC skips). Everything skips right 76 after this.
	hvcallbacks[kNTSCTopBlank+6] = map[int]func(int, int, *TIA){kNTSCPictureStart + 80: rsync}
	// Technically this resets the ball at kNTSCPictureStart+86 (or clock 154) since we lost 76 clocks above.
	// In the same way the RSYNC will happen at kNTSCPictureStart+148 (or clock 216) and expire at 219 so shifts everything right by 8.
	hvcallbacks[kNTSCTopBlank+8] = map[int]func(int, int, *TIA){kNTSCPictureStart + 10: ballReset, kNTSCPictureStart + 72: rsync}

	// Reset the colors around for screen 2 so we can easily pick out what didn't get overwritten and skipped by RSYNC.
	// Playfield/Ball to black
	ta.Write(COLUPF, black<<1)
	// Set background to blue
	ta.Write(COLUBK, blue<<1)
	// Set player0 to yellow and player 1 to red
	ta.Write(COLUP0, yellow<<1)
	ta.Write(COLUP1, red<<1)

	// Need to turn VSYNC off and render a tick so everything resets.
	// Otherwise the VSYNC at the end of frame and beginning don't
	// actually reset on the 2nd one (as intended).
	ta.Write(VSYNC, 0x00)
	if err := ta.Tick(); err != nil {
		t.Fatalf("Error on tick: %v", err)
	}
	ta.TickDone()

	// This technically will run over by 76 + 8 pixels and paint a last line we should be able to see. Due to how VSYNC gets latched
	// we always emit some pixels on the next line. In our case 1 since we trigger VSYNC immediately on end of frame. A real 2600 would
	// like do STA WSYNC; STA VSYNC and actually draw 9 pixels of hblank which are fine since they would be there in VSYNC/VBLANK anyways.
	runAFrame(t, ta, frameSpec{
		width:       kNTSCWidth,
		height:      kNTSCHeight,
		vsync:       kVSYNCLines,
		vblank:      kNTSCTopBlank,
		overscan:    kNTSCOverscanStart,
		hvcallbacks: hvcallbacks,
	})
	if !done {
		t.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
	}

	p.b = kNTSC[blue]
	want = createCanonicalImage(p)
	// Need new scanlines since we swapped colors around.
	scanlines = []scanline{
		{
			// Fill in the columns first.
			start: kNTSCTopBlank,
			stop:  kNTSCOverscanStart,
			horizontals: []horizontal{
				{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[yellow]},
				{kNTSCWidth - kPF0Pixels, kNTSCWidth, kNTSC[red]},
			},
		},
		{
			// Black 4 width ball on 2 line
			start:       kNTSCTopBlank + 5,
			stop:        kNTSCTopBlank + 7,
			horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[black]}},
		},
		{
			// Left over green ball on line 6.
			// The rest of line 6 is leftover background and PF from last frame.
			start: kNTSCTopBlank + 6,
			stop:  kNTSCTopBlank + 7,
			horizontals: []horizontal{
				{kNTSCPictureStart + 84, kNTSCPictureStart + 88, kNTSC[green]},
				{kNTSCPictureStart + 88, kNTSCPictureStart + 144, kNTSC[yellow]},
				{kNTSCPictureStart + 144, kNTSCPictureStart + 160, kNTSC[blue]},
			},
		},
		{
			// The ball moved right 76 pixels so should be against right edge now but it won't
			// print because we're in score mode and the playfield is on so P1 color takes precedence.
			// Technically don't need to draw this (columns did above) but leaving it for clarity.
			start:       kNTSCTopBlank + 7,
			stop:        kNTSCTopBlank + 8,
			horizontals: []horizontal{{kNTSCPictureStart + 156, kNTSCPictureStart + 160, kNTSC[red]}},
		},
		{
			// Now it moves to normal position post reset (see above on pixel shifts).
			// Then RSYNC near the end leaves the last 8 pixels from the previous frame.
			start: kNTSCTopBlank + 8,
			stop:  kNTSCTopBlank + 9,
			horizontals: []horizontal{
				{kNTSCPictureStart + 90, kNTSCPictureStart + 94, kNTSC[black]},
				{kNTSCPictureStart + 152, kNTSCPictureStart + 160, kNTSC[blue]},
			},
		},
		{
			// Finally ball shifts 8 to the right.
			start:       kNTSCTopBlank + 9,
			stop:        kNTSCTopBlank + 10,
			horizontals: []horizontal{{kNTSCPictureStart + 98, kNTSCPictureStart + 102, kNTSC[black]}},
		},
		{
			// Last line is special since we ticked off more clocks than needed so the TIA just keeps painting till
			// VSYNC hits. In this case it's 76 + 8 + 1 (due to VSYNC clock delay).
			start: kNTSCOverscanStart,
			stop:  kNTSCOverscanStart + 1,
			horizontals: []horizontal{
				// Yellow column extended one line.
				{kNTSCPictureStart, kNTSCPictureStart + kPF0Pixels, kNTSC[yellow]},
				// One blue background pixel for VSYNC clock.
				{kNTSCPictureStart + kPF0Pixels, kNTSCPictureStart + kPF0Pixels + 1, kNTSC[blue]},
			},
		},
	}

	drawWant()
	df()

}

func TestCollision(t *testing.T) {
	clearCollision := func(x, y int, ta *TIA) {
		// Anything written should trigger it.
		ta.Write(CXCLR, 0x00)
	}
	verifyNoCollision := func(x, y int, ta *TIA) {
		// In this case we'll just reach inside of TIA directly to check state.
		// Other tests use Read correctly.
		for i, c := range ta.collision {
			if got, want := c, kCLEAR_COLLISION; got != want {
				t.Errorf("Index %d of t.collision has collision bits %.2X when it should have none at (%d,%d)", i, got, x, y)
			}
		}
	}

	noCollisionM0P := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0P), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionM0P0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionM1P := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1P), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionM1P1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionPPMM := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXPPMM), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionPPMM: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionMissile0FB := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0FB), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionMissile0FB: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionMissile1FB := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1FB), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionMissile1FB: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionPlayer0FB := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP0FB), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionPlayer0FB: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	noCollisionPlayer1FB := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP1FB), kCLEAR_COLLISION; got != want {
			t.Errorf("noCollisionPlayer1FB: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}

	missile0Player0 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0P), kMASK_CX_M0P0; got != want {
			t.Errorf("missile0Player0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile0Player1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0P), kMASK_CX_M0P1; got != want {
			t.Errorf("missile0Player1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile1Player0 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1P), kMASK_CX_M1P0; got != want {
			t.Errorf("missile1Player0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile1Player1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1P), kMASK_CX_M1P1; got != want {
			t.Errorf("missile1Player0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile0Playfield := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0FB), kMASK_CX_M0PF; got != want {
			t.Errorf("missile0Playfield: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile1Playfield := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1FB), kMASK_CX_M1PF; got != want {
			t.Errorf("missile1Playfield: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	missile0Missile1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXPPMM), kMASK_CX_M0M1; got != want {
			t.Errorf("missile0Missile1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	player0Playfield := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP0FB), kMASK_CX_P0PF; got != want {
			t.Errorf("player0Playfield: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	player1Playfield := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP1FB), kMASK_CX_P1PF; got != want {
			t.Errorf("player1Playfield: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	player0Player1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXPPMM), kMASK_CX_P0P1; got != want {
			t.Errorf("player0Player1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}

	ballPlayfield := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXBLPF), kMASK_CX_BLPF; got != want {
			t.Errorf("ballPlayfield: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	ballMissile0 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM0FB), kMASK_CX_M0BL; got != want {
			t.Errorf("ballMissile0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	ballMissile1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXM1FB), kMASK_CX_M1BL; got != want {
			t.Errorf("ballMissile1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	ballPlayer0 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP0FB), kMASK_CX_P0BL; got != want {
			t.Errorf("ballPlayer0: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}
	ballPlayer1 := func(x, y int, ta *TIA) {
		if got, want := ta.Read(CXP1FB), kMASK_CX_P1BL; got != want {
			t.Errorf("ballPlayer1: Got incorrect collision. Got %.2X and want %.2X at (%d,%d)", got, want, x, y)
		}
	}

	tests := []struct {
		name        string
		hvcallbacks map[int]map[int]func(int, int, *TIA) // for runAFrame hvcallbacks and checking state
	}{
		{
			name: "MissilePlayerPlayfieldCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Normally collision clear gets done in HBLANK.
				kNTSCTopBlank - 1: {0: clearCollision, 1: verifyNoCollision},
				kNTSCTopBlank:     {0: player0TwoClose4Missile, 1: player1TwoClose4Missile, 10: player0SetClear, 12: player1SetClear},
				// When initially setting up the player it won't emit so no collisions should take place but missile ones should.
				kNTSCTopBlank + 3: {0: player0Reset, 1: player0Line2, 3: missile0Reset, 4: missile0On, kNTSCPictureStart: verifyNoCollision, kNTSCPictureStart + 1: missile0Playfield, kNTSCPictureStart + 4: noCollisionM0P},
				// Now the player should collide as well with missile/playfield.
				kNTSCTopBlank + 4: {kNTSCPictureStart + 4: missile0Player0, kNTSCPictureStart + 5: player0Playfield, kNTSCPictureStart + 8: player0SetClear, kNTSCPictureStart + 9: missile0Off, kNTSCPictureStart + 100: clearCollision},
				// Same for player1 as above.
				kNTSCTopBlank + 5: {kNTSCPictureStart + 76: player1Reset, kNTSCPictureStart + 77: player1Line2, kNTSCPictureStart + 78: missile1Reset, kNTSCPictureStart + 79: missile1On, kNTSCPictureStart + 82: verifyNoCollision, kNTSCPictureStart + 83: missile1Playfield, kNTSCPictureStart + 84: noCollisionM1P},
				kNTSCTopBlank + 6: {kNTSCPictureStart + 84: missile1Player1, kNTSCPictureStart + 85: player1Playfield, kNTSCPictureStart + 88: player1SetClear, kNTSCPictureStart + 89: missile1Off, kNTSCPictureStart + 100: clearCollision},
				// Now missile0, player 1.
				kNTSCTopBlank + 7: {0: player1Reset, 1: player1Line2, 3: missile0Reset, 4: missile0On, kNTSCPictureStart: verifyNoCollision, kNTSCPictureStart + 1: missile0Playfield, kNTSCPictureStart + 4: noCollisionM0P},
				kNTSCTopBlank + 8: {kNTSCPictureStart + 4: missile0Player1, kNTSCPictureStart + 5: player1Playfield, kNTSCPictureStart + 8: player1SetClear, kNTSCPictureStart + 9: missile0Off, kNTSCPictureStart + 100: clearCollision},
				// Now missile1, player 0.
				kNTSCTopBlank + 9:  {0: player0Reset, 1: player0Line2, 3: missile1Reset, 4: missile1On, kNTSCPictureStart: verifyNoCollision, kNTSCPictureStart + 1: missile1Playfield, kNTSCPictureStart + 4: noCollisionM1P},
				kNTSCTopBlank + 10: {kNTSCPictureStart + 4: missile1Player0, kNTSCPictureStart + 5: player0Playfield, kNTSCPictureStart + 8: player0SetClear, kNTSCPictureStart + 9: missile1Off, kNTSCPictureStart + 100: clearCollision},
			},
		},
		{
			name: "MissileMissileCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank - 1: {0: clearCollision, 1: verifyNoCollision},
				// Setup a long missile and a 1 pixel one and thn offset resets by 4 pixels.
				kNTSCTopBlank:     {0: missile0Width8, 1: missile1Width1, 3: missile0Reset, 4: missile0On, 5: missile1Off, kNTSCPictureStart: missile1Reset, kNTSCPictureStart + 8: noCollisionPPMM},
				kNTSCTopBlank + 1: {0: missile1On, kNTSCPictureStart: noCollisionPPMM, kNTSCPictureStart + 1: noCollisionPPMM, kNTSCPictureStart + 2: noCollisionPPMM, kNTSCPictureStart + 3: noCollisionPPMM, kNTSCPictureStart + 4: noCollisionPPMM, kNTSCPictureStart + 5: missile0Missile1},
			},
		},
		{
			name: "PlayerPLayerCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank - 1: {0: clearCollision, 1: verifyNoCollision},
				// Set player graphics up but offset player1 to reset at the edge so it'll start on position 73 (and collide).
				kNTSCTopBlank:     {0: player0Single, 1: player1Single, 2: player0Reset, 3: player0Line0, 4: player1Line0, kNTSCPictureStart: player1Reset, kNTSCPictureStart + 8: noCollisionPPMM},
				kNTSCTopBlank + 1: {kNTSCPictureStart: noCollisionPPMM, kNTSCPictureStart + 1: noCollisionPPMM, kNTSCPictureStart + 2: noCollisionPPMM, kNTSCPictureStart + 3: noCollisionPPMM, kNTSCPictureStart + 4: noCollisionPPMM, kNTSCPictureStart + 5: noCollisionPPMM, kNTSCPictureStart + 6: player0Player1},
			},
		},
		{
			name: "BallPlayfieldCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simple. Just enabled the ball and it'll collide with the playfield. We'll reset it at the edge so it starts a bit later.
				kNTSCTopBlank: {0: ballOff, kNTSCPictureStart: ballReset, kNTSCPictureStart + 1: ballOn, kNTSCPictureStart + 2: verifyNoCollision, kNTSCPictureStart + 3: verifyNoCollision, kNTSCPictureStart + 4: verifyNoCollision, kNTSCPictureStart + 5: ballPlayfield},
			},
		},
		{
			name: "MissileBallCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank - 1: {0: clearCollision, 1: verifyNoCollision},
				// Setup a long missile0 and a 1 pixel one and thn offset resets by 4 pixels. Move this outside the playfield column.
				kNTSCTopBlank:     {0: missile0Width8, 5: missile1Off, kNTSCPictureStart + 16: missile0Reset, kNTSCPictureStart + 17: missile0On, kNTSCPictureStart + 20: ballReset, kNTSCPictureStart + 25: noCollisionMissile0FB},
				kNTSCTopBlank + 1: {0: ballOn, kNTSCPictureStart + 20: noCollisionMissile0FB, kNTSCPictureStart + 21: noCollisionMissile0FB, kNTSCPictureStart + 22: noCollisionMissile0FB, kNTSCPictureStart + 23: noCollisionMissile0FB, kNTSCPictureStart + 24: noCollisionMissile0FB, kNTSCPictureStart + 25: ballMissile0, kNTSCPictureStart + 100: ballOff},
				// Now do the same for missile1.
				kNTSCTopBlank + 2: {0: missile1Width8, 5: missile0Off, kNTSCPictureStart + 16: missile1Reset, kNTSCPictureStart + 17: missile1On, kNTSCPictureStart + 20: ballReset, kNTSCPictureStart + 25: noCollisionMissile1FB},
				kNTSCTopBlank + 3: {0: ballOn, kNTSCPictureStart + 20: noCollisionMissile1FB, kNTSCPictureStart + 21: noCollisionMissile1FB, kNTSCPictureStart + 22: noCollisionMissile1FB, kNTSCPictureStart + 23: noCollisionMissile1FB, kNTSCPictureStart + 24: noCollisionMissile1FB, kNTSCPictureStart + 25: ballMissile1},
			},
		},
		{
			name: "PlayerBallCollision",
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank - 1: {0: clearCollision, 1: verifyNoCollision},
				// Set players outside of the columns and initially reset the ball so it overlaps player0.
				kNTSCTopBlank: {0: player0Single, 1: player1Single, 3: player0Line0, 4: player1Line7, kNTSCPictureStart + 15: player0Reset, kNTSCPictureStart + 20: ballReset, kNTSCPictureStart + 25: noCollisionPlayer0FB, kNTSCPictureStart + 35: player1Reset, kNTSCPictureStart + 36: clearCollision, kNTSCPictureStart + 45: noCollisionPlayer1FB},
				// Now turn on the ball and check collisions and then setup for player1.
				kNTSCTopBlank + 1: {0: ballOn, kNTSCPictureStart + 20: noCollisionPlayer0FB, kNTSCPictureStart + 21: noCollisionPlayer0FB, kNTSCPictureStart + 22: noCollisionPlayer0FB, kNTSCPictureStart + 23: noCollisionPlayer0FB, kNTSCPictureStart + 24: noCollisionPlayer0FB, kNTSCPictureStart + 25: ballPlayer0, kNTSCPictureStart + 26: ballOff, kNTSCPictureStart + 40: ballReset},
				// Now do player1.
				kNTSCTopBlank + 2: {0: ballOn, kNTSCPictureStart + 40: noCollisionPlayer1FB, kNTSCPictureStart + 41: noCollisionPlayer1FB, kNTSCPictureStart + 42: noCollisionPlayer1FB, kNTSCPictureStart + 43: noCollisionPlayer1FB, kNTSCPictureStart + 44: noCollisionPlayer1FB, kNTSCPictureStart + 45: ballPlayer1},
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

			// Write the PF regs so we get a left column and a middle one and set some basic stuff.
			ta.Write(PF0, 0xFF)
			ta.Write(PF1, 0x00)
			ta.Write(PF2, 0x00)
			ta.Write(CTRLPF, 0x00)

			// Run the actual frame based on the callbacks for when to change rendering.
			runAFrame(t, ta, frameSpec{
				width:       kNTSCWidth,
				height:      kNTSCHeight,
				vsync:       kVSYNCLines,
				vblank:      kNTSCTopBlank,
				overscan:    kNTSCOverscanStart,
				hvcallbacks: test.hvcallbacks,
			})
			if !done {
				t.Fatalf("%s: didn't trigger a VSYNC?\n%v", test.name, spew.Sdump(ta))
			}
		})
	}
}

func TestWsync(t *testing.T) {
	done := false
	cnt := 0
	ta, err := setup(t, t.Name(), TIA_MODE_NTSC, &cnt, &done)
	if err != nil {
		t.Fatalf("Can't Init: %v", err)
	}

	if ta.Raised() {
		t.Error("Raised is already set before WSYNC?")
	}
	if err := ta.Tick(); err != nil {
		t.Errorf("Error on tick: %v", err)
	}
	// Any value will do.
	ta.Write(WSYNC, 0x00)
	if ta.Raised() {
		t.Error("Raised is already set Tick/TickDone?")
	}
	ta.TickDone()
	if !ta.Raised() {
		t.Error("Not raised after WSYNC?")
	}
	for {
		// Stop when we see hblank is happening.
		if ta.hblank {
			break
		}
		if err := ta.Tick(); err != nil {
			t.Errorf("Error on tick %d: %v", cnt, err)
		}
		if !ta.Raised() {
			t.Errorf("Raised dropped before hblank at %d?", cnt)
		}
		ta.TickDone()
	}

	// Run one more tick.
	if err := ta.Tick(); err != nil {
		t.Errorf("Error on tick: %v", err)
	}
	ta.TickDone()
	if ta.Raised() {
		t.Errorf("Raised still after hblank?\n%v", spew.Sdump(ta))
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
		if err := ta.Tick(); err != nil {
			b.Fatalf("Error on tick: %v", err)
		}
		ta.TickDone()

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
