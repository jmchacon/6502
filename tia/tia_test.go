package tia

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
	ta.Write(VSYNC, 0xFF)
	for i := 0; i < frame.height; i++ {
		if cb := frame.vcallbacks[i]; cb != nil {
			cb(i, ta)
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
			// TODO(jchacon): add logic to randomly run this before or after Tick so we can verify order doesn't matter. CPU's on the same clock should have the same effects regardless.
			if cb := frame.hvcallbacks[i][j]; cb != nil {
				cb(j, i, ta)
			}
			if err := ta.Tick(); err != nil {
				t.Fatalf("Error on tick: %v", err)
			}
			ta.TickDone()
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
		ta.Write(CTRLPF, 0x00)
	}
	pfCallback3 := func(i int, ta *TIA) {
		ta.Write(CTRLPF, kMASK_SCORE)
	}

	// Ball callbacks for 1,2,4,8 sized balls at visible pixel 80 and 5 lines printed for each.
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

	// Turn the ball on and off.
	ballOn := func(y, x int, ta *TIA) {
		ta.Write(ENABL, kMASK_ENAMB)
	}
	ballOff := func(y, x int, ta *TIA) {
		ta.Write(ENABL, 0x00)
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
			name:   "BallOffButWidthsChange",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				kNTSCTopBlank:      {0: ballWidth1},
				kNTSCTopBlank + 10: {0: ballWidth2},
				kNTSCTopBlank + 20: {0: ballWidth4},
				kNTSCTopBlank + 30: {0: ballWidth8},
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
			name:   "BallOnWidthsChange",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
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
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 10,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 81, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 25,
					stop:        kNTSCTopBlank + 30,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 82, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 45,
					stop:        kNTSCTopBlank + 50,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 70,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]}},
				},
			},
		},
		{
			name:   "BallOnWidthsChangeScreenEdge",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth1},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 155: ballReset},
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
					// All of these should be green (playfield color) since score mode shouldn't be changing
					// the ball drawing color.
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 10,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					// 2 pixels writes on on right edge and then wraps to next scanline for a single one.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap).
					start:       kNTSCTopBlank + 25,
					stop:        kNTSCTopBlank + 30,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 25,
					stop:        kNTSCTopBlank + 30,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 1, kNTSC[green]}},
				},
				{
					// 4 pixels writes on on right edge and then wraps to next scanline for three more.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap).
					start:       kNTSCTopBlank + 45,
					stop:        kNTSCTopBlank + 50,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 45,
					stop:        kNTSCTopBlank + 50,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 3, kNTSC[green]}},
				},
				{
					// 8 pixels writes on on right edge and then wraps to next scanline for three more.
					// It'll clip the last row since we turn the ball off on that one (so the last wrap).
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 70,
					horizontals: []horizontal{{kNTSCPictureStart + 159, kNTSCPictureStart + 160, kNTSC[green]}},
				},
				{
					// But...It turns on for the first row since wrap around while off on the previous row still counts.
					start:       kNTSCTopBlank + 65,
					stop:        kNTSCTopBlank + 70,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 7, kNTSC[green]}},
				},
			},
		},
		{
			name:   "BallOnWidthsAndDisableMidWrite",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:      {0: ballWidth8},
				kNTSCTopBlank + 3:  {kNTSCPictureStart + 76: ballReset},
				kNTSCTopBlank + 5:  {0: ballOn, kNTSCPictureStart + 85: ballOff},
				kNTSCTopBlank + 7:  {0: ballOn},
				kNTSCTopBlank + 8:  {0: ballOff},
				kNTSCTopBlank + 20: {0: ballOn, 4: ballWidth4, kNTSCPictureStart + 85: ballWidth8, kNTSCPictureStart + 95: ballOff},
				kNTSCTopBlank + 22: {0: ballOn},
				kNTSCTopBlank + 23: {0: ballOff},
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
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 6,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 86, kNTSC[green]}},
				},
				{
					start:       kNTSCTopBlank + 7,
					stop:        kNTSCTopBlank + 8,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]}},
				},
				{
					start: kNTSCTopBlank + 20,
					stop:  kNTSCTopBlank + 21,
					horizontals: []horizontal{
						{kNTSCPictureStart + 80, kNTSCPictureStart + 84, kNTSC[green]},
						{kNTSCPictureStart + 86, kNTSCPictureStart + 88, kNTSC[green]},
					},
				},
				{
					start:       kNTSCTopBlank + 22,
					stop:        kNTSCTopBlank + 23,
					horizontals: []horizontal{{kNTSCPictureStart + 80, kNTSCPictureStart + 88, kNTSC[green]}},
				},
			},
		},
		{
			name:   "BallOnResetHblank",
			pfRegs: [3]uint8{0xFF, 0x00, 0x00},
			hvcallbacks: map[int]map[int]func(int, int, *TIA){
				// Simulate ball control happening in hblank.
				kNTSCTopBlank:     {0: ballWidth8},
				kNTSCTopBlank + 3: {0: ballReset},
				kNTSCTopBlank + 5: {0: ballOn},
				kNTSCTopBlank + 7: {0: ballOff},
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
					start:       kNTSCTopBlank + 5,
					stop:        kNTSCTopBlank + 7,
					horizontals: []horizontal{{kNTSCPictureStart, kNTSCPictureStart + 8, kNTSC[green]}},
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
			ctrl := uint8(0x00)
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
				for h := s.start; h < s.stop; h++ {
					for _, hz := range s.horizontals {
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
		ta.Write(VSYNC, 0xFF)
		for i := 0; i < frame.height; i++ {
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
					b.Fatalf("Error on tick: %v", err)
				}
				ta.TickDone()
			}
		}
		ta.Write(VSYNC, 0xFF)
		if !done {
			b.Fatalf("Didn't trigger a VSYNC?\n%v", spew.Sdump(ta))
		}
	}
	d := time.Now().Sub(n)
	b.Logf("%d runs at total time %s and %s time per run", kRuns, d, d/kRuns)

}
