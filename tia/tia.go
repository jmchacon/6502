// Package tia implements the TIA chip used in an atari 2600 for display/sound.
package tia

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/jmchacon/6502/io"
)

const (
	kCXM0P  = iota // Collision bits for M0 and P0/P1 stored in bits 6/7.
	kCXM1P         // Collision bits for M1 and P0/P1 stored in bits 6/7.
	kCXP0FB        // Collision bits for P0/PF and P0/BL stored in bits 6/7.
	kCXP1FB        // Collision bits for P1/PF and P1/BL stored in bits 6/7.
	kCXM0FB        // Collision bits for M0/PF and M0/BL stored in bits 6/7.
	kCXM1FB        // Collision bits for M1/PF and M1/BL stored in bits 6/7.
	kCXBLPF        // Collision bit for BL/PF stored in bit 7.
	kCXPPMM        // Collision bits for P0/P1 and M0/M1 stored in bits 6/7.
)

const (
	// An NTSC TIA Frame is 228x262 though visible area is only 160x192 due to overscan
	// and hblank regions.
	kNTSCWidth  = 228
	kNTSCHeight = 262

	// A PAL/SECAM TIA Frame is 228x312 though visible area is only 160x228 due to overscan
	// and hblank regions.
	kPALWidth  = 228
	kPALHeight = 312

	// Indexes for accessing player/missle and color arrays.
	kMissle0    = 0
	kMissle1    = 1
	kPlayer0    = 0
	kPlayer1    = 1
	kPlayfield  = 2
	kBackground = 3

	// Always 68 hblank clocks
	kHblank = 68

	kMASK_READ = uint8(0xC0) // Only D7/6 defined on the bus for reads.

	kMASK_VSYNC = uint8(0x02) // Trigger bit for VSYNC (others ignored).

	kMASK_VBL_VBLANK      = uint8(0x02)
	kMASK_VBL_I45_LATCHES = uint8(0x40)
	kMASK_VBL_I0I3_GROUND = uint8(0x80)

	kMASK_NUSIZ_MISSILE = uint8(0x30)

	kMissle1Clock = 1
	kMissle2Clock = 2
	kMissle4Clock = 4
	kMissle8Clock = 8

	kMASK_NUSIZ_PLAYER = uint8(0x07)

	kMASK_REFPX = uint8(0x08)

	kMASK_PF0 = uint8(0xF0)

	kMASK_AUDC = uint8(0x0F)

	kMASK_AUDF = uint8(0x1F)

	kMASK_AUDV = uint8(0x0F)

	kMASK_ENAMB = uint8(0x02) // Missle and ball enable use the same mask

	kShiftNmHM = 4

	kMASK_HM_NEG         = uint8(0x08) // If this bit is set, sign extend
	kMASK_HM_SIGN_EXTEND = uint8(0xF0)

	kMASK_VDEL = uint8(0x01)

	kMASK_RESMP = uint8(0x02)
)

type playerCntWidth int

const (
	kPlayerOne playerCntWidth = iota
	kPlayerTwoClose
	kPlayerTwoMed
	kPlayerThreeClose
	kPlayerTwoWide
	kPlayerDouble
	kPlayerThreeMed
	kPlayerQuad
)

// http://problemkaputt.de/2k6specs.htm has audio descriptions.

type audioStyle int

const (
	kAudioOff audioStyle = iota // Also is a tone for SECAM
	kAudio4Bit
	kAudioDiv154Bit
	kAudio5Bit4Bit
	kAudioDiv2Pure
	kAudioDiv31Pure
	kAudio5BitDiv2
	kAudio9Bit
	kAudio5Bit
	kAudioLast4One
	kAudioDiv6Pure
	kAudioDiv93Pure
	kAudio5BitDiv6
)

// TIA implements all modes needed for a TIA including sound.
type TIA struct {
	mode TIAMode
	// NOTE: Collision bits are stored as they are expected to return to
	//       avoid lots of shifting and masking if stored in a uint16.
	//       But store as an array so they can be easily reset.
	collision               [8]uint8          // Collission bits (see constants below for various ones).
	inputPorts              [6]io.PortIn1     // If non-nil defines the input port for the given paddle/joystick.
	ioPortGnd               func()            // If non-nil is called when I0-3 are grounded via VBLANK.7.
	outputLatches           [2]bool           // The output latches (if used) for ports 4/5.
	rdy                     bool              // If true then RDY out (which should go to RDY on cpu) is signaled high via Raised().
	vsync                   bool              // If true in VSYNC mode.
	vblank                  bool              // If true in VBLANK mode.
	latches                 bool              // If true then I4/I5 in latch mode.
	groundInput             bool              // If true then I0-I3 are grounded and always return 0.
	missileWidth            [2]int            // Width of missles in pixels (1,2,4,8).
	playerCntWidth          [2]playerCntWidth // Player 0,1 count and width (see enum).
	colors                  [4]*color.NRGBA   // Player 0,1, playfield and background color.
	reflectPlayers          [2]bool           // Player 0,1 reflection bits.
	playfield               [3]uint8          // PF0-3 regs.
	hPos                    int               // Current horizontal position.
	vPos                    int               // Current vertical position.
	playerPos               [2]int            // Player 0,1 horizontal start pos.
	misslePos               [2]int            // Missle 0,1 horizontal start pos.
	ballPos                 int               // Ball horizontal start pos.
	audioControl            [2]audioStyle     // Audio style for channels 0 and 1.
	audioDivide             [2]uint8          // Audio divisors for channels 0 and 1.
	audioVolume             [2]uint8          // Audio volume for channels 0 and 1.
	playerGraphic           [2]uint8          // The player graphics for player 0 and 1.
	missleEnabled           [2]bool           // Whether the missle is enabled or not.
	ballEnabled             bool              // Whether the ball is enabled or not.
	horizontalMotionPlayers [2]uint8          // Horizontal motion for players.
	horizontalMotionMissles [2]uint8          // Horizontal motion for missles.
	horizontalMotionBall    uint8             // Horizontal motion for ball.
	verticalDelayPlayers    [2]bool           // Whether to delay player 0,1 by one line.
	veritcalDelayBall       bool              // Whether to delay ball by one line.
	missleLockedPlayer      [2]bool           // Whether the missle is locked to it's player (and disabled).
	hmove                   bool              // Whether HMOVE has been triggered in the last 24 clocks.
	picture                 *image.NRGBA      // The in memory representation of a single frame.
	frameDone               func(*image.NRGBA)
}

// Index references for TIA.color
const (
	kPlayer0Color = iota
	kPlayer1Color
	kPlayfieldColor
	kBackgroundColor
)

// TIAMode is the enumeration for TIA output mode (NTSC, etc).
type TIAMode int

const (
	TIA_MODE_UNIMPLEMENTED TIAMode = iota
	TIA_MODE_NTSC
	TIA_MODE_PAL
	TIA_MODE_SECAM
	TIA_MODE_MAX
)

type TIADef struct {
	// Mode defines the TV mode for this TIA (NTSC, PAL, SECAM)
	Mode TIAMode
	// Port0 is the 1 bit input for paddle 0.
	Port0 io.PortIn1
	// Port1 is the 1 bit input for paddle 1.
	Port1 io.PortIn1
	// Port2 is the 1 bit input for paddle 2.
	Port2 io.PortIn1
	// Port3 is the 1 bit input for paddle 3.
	Port3 io.PortIn1
	// Port4 is the 1 bit input for joystick 0 (trigger).
	Port4 io.PortIn1
	// Port5 is the 1 bit input for joystick 1 (trigger).
	Port5 io.PortIn1
	// IoPortGnd is an optional function which will be called when Ports 0-3 are grounded via VBLANK.7.
	IoPortGnd func()
	// FrameDone is an non-optional function which will be called on VSYNC transitions from low->high.
	// This will pass the current rendered frame for output/analysis/etc.
	// Non-optional because otherwise what's the point of rendering frames that can't be used?
	FrameDone func(*image.NRGBA)
}

// Init returns a full initialized TIA.
func Init(def *TIADef) (*TIA, error) {
	if def.Mode <= TIA_MODE_UNIMPLEMENTED || def.Mode >= TIA_MODE_MAX {
		return nil, fmt.Errorf("TIA mode is invalid: %d", def.Mode)
	}
	if def.FrameDone == nil {
		return nil, errors.New("FrameDone must be non-nil")
	}
	w := kNTSCWidth
	h := kNTSCHeight
	if def.Mode != TIA_MODE_NTSC {
		w = kPALWidth
		h = kPALHeight
	}
	t := &TIA{
		mode:       def.Mode,
		inputPorts: [6]io.PortIn1{def.Port0, def.Port1, def.Port2, def.Port3, def.Port4, def.Port5},
		picture:    image.NewNRGBA(image.Rect(0, 0, w, h)),
		frameDone:  def.FrameDone,
		vsync:      true, // start in VSYNC mode.
	}
	t.PowerOn()
	return t, nil
}

// PowerOn performs a full power-on/reset for the TIA.
func (t *TIA) PowerOn() {
}

// out holds the data for a 1 bit I/O port.
type out struct {
	data bool
}

// Output implements the interface for io.PortOut1
func (o *out) Output() bool {
	return o.data
}

// NOTE: a lot of details for below come from
//
// http://problemkaputt.de/2k6specs.htm
//
// and the Stella PDF:
//
// https://atarihq.com/danb/files/stella.pdf

// Raised implements the irq.Sender interface for determining RDY (effectivly an interrupt)
// state when called. An implementation tying this to a receiver can tie them together for
// halting the CPU as needed.
func (t *TIA) Raised() bool {
	return t.rdy
}

// Read returns values based on the given address. The address is masked to 4 bits internally
// (so aliasing across the 6 address pins).
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real reads are happening on clocked CPU Tick()'s.
func (t *TIA) Read(addr uint16) uint8 {
	// Strip to 4 bits for internal regs.
	addr &= 0x0F
	var ret uint8
	switch addr {
	case 0x00:
		ret = t.collision[kCXM0P]
	case 0x01:
		ret = t.collision[kCXM1P]
	case 0x02:
		ret = t.collision[kCXP0FB]
	case 0x03:
		ret = t.collision[kCXP1FB]
	case 0x04:
		ret = t.collision[kCXM0FB]
	case 0x05:
		ret = t.collision[kCXM1FB]
	case 0x06:
		ret = t.collision[kCXBLPF]
	case 0x07:
		ret = t.collision[kCXPPMM]
	case 0x08, 0x09, 0x0A, 0x0B:
		idx := int(addr) - 0x08
		if !t.groundInput && t.inputPorts[idx] != nil && t.inputPorts[idx].Input() {
			ret = 0x80
		}
	case 0x0C, 0x0D:
		idx := int(addr) - 0x0C
		if t.latches {
			if t.outputLatches[idx] {
				ret = 0x80
				break
			}
		}
		if t.inputPorts[idx+4] != nil && t.inputPorts[idx+4].Input() {
			ret = 0x80
		}
	default:
		// Couldn't find a definitive answer what happens on
		// undefined addresses so pull them all high.
		ret = 0xFF
	}
	// Apply read mask before returning.
	return ret & kMASK_READ
}

// Write stores the value at the given address. The address is masked to 6 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real writes are happening on clocked CPU Tick()'s.
func (t *TIA) Write(addr uint16, val uint8) {
	// Strip to 6 bits for internal regs
	addr &= 0x3F

	switch addr {
	case 0x00:
		// VSYNC
		l := false
		if (val & kMASK_VSYNC) == kMASK_VSYNC {
			l = true
		}
		// If transitioning low->high assume end of frame and do callback and reset
		// coordinates.
		if l && !t.vsync {
			t.frameDone(t.picture)
			t.hPos = 0
			t.vPos = 0
		}
		t.vsync = l
	case 0x01:
		// VBLANK
		t.vblank = false
		if (val & kMASK_VBL_VBLANK) == kMASK_VBL_VBLANK {
			t.vblank = true
		}
		l := false
		if (val & kMASK_VBL_I45_LATCHES) == kMASK_VBL_I45_LATCHES {
			l = true
		}
		// If we're turning latches off (low) then they go high for later.
		if !l && t.latches {
			t.outputLatches[0] = true
			t.outputLatches[1] = true
		}
		t.latches = l
		t.groundInput = false
		if (val * kMASK_VBL_I0I3_GROUND) == kMASK_VBL_I0I3_GROUND {
			t.groundInput = true
			if t.ioPortGnd != nil {
				t.ioPortGnd()
			}
		}
	case 0x02:
		// WSYNC
		t.rdy = true
	case 0x03:
		// RSYNC
		t.hPos = 0
	case 0x04, 0x05:
		// NUSIZ0 and NUSIZ1
		idx := int(addr) - 0x04
		switch val & kMASK_NUSIZ_MISSILE {
		case 0x00:
			t.missileWidth[idx] = kMissle1Clock
		case 0x10:
			t.missileWidth[idx] = kMissle2Clock
		case 0x20:
			t.missileWidth[idx] = kMissle4Clock
		case 0x30:
			t.missileWidth[idx] = kMissle8Clock
		}
		switch val & kMASK_NUSIZ_PLAYER {
		case 0x00:
			t.playerCntWidth[idx] = kPlayerOne
		case 0x01:
			t.playerCntWidth[idx] = kPlayerTwoClose
		case 0x02:
			t.playerCntWidth[idx] = kPlayerTwoMed
		case 0x03:
			t.playerCntWidth[idx] = kPlayerThreeClose
		case 0x04:
			t.playerCntWidth[idx] = kPlayerTwoWide
		case 0x05:
			t.playerCntWidth[idx] = kPlayerDouble
		case 0x06:
			t.playerCntWidth[idx] = kPlayerThreeMed
		case 0x07:
			t.playerCntWidth[idx] = kPlayerQuad
		}
	case 0x06, 0x07, 0x08, 0x09:
		// COLUP0, COLUP1, COLUPF, COLUBK
		idx := int(addr) - 0x06
		t.colors[idx] = decodeColor(t.mode, val)
	case 0x0B, 0x0C:
		// REFP0, REFP1
		idx := int(addr) - 0x0B
		t.reflectPlayers[idx] = false
		if (val & kMASK_REFPX) == kMASK_REFPX {
			t.reflectPlayers[idx] = true
		}
	case 0x0D, 0x0E, 0x0F:
		// PF0, PF1, PF2
		idx := int(addr) - 0x0D
		// PF0 only cares about some bits.
		if addr == 0x0D {
			val &= kMASK_PF0
		}
		t.playfield[idx] = val
	case 0x10, 0x11:
		// RESP0, RESP1
		idx := int(addr) - 0x10
		t.playerPos[idx] = t.hPos
	case 0x12, 0x13:
		// RESM0, RESM1
		idx := int(addr) - 0x12
		t.misslePos[idx] = t.hPos
	case 0x14:
		t.ballPos = t.hPos
	case 0x15, 0x16:
		// AUDC0, AUDC1
		idx := int(addr) - 0x15
		// Only care about bottom bits
		val &= kMASK_AUDC
		switch val {
		case 0x00:
			t.audioControl[idx] = kAudioOff
		case 0x01:
			t.audioControl[idx] = kAudio4Bit
		case 0x02:
			t.audioControl[idx] = kAudioDiv154Bit
		case 0x03:
			t.audioControl[idx] = kAudio5Bit4Bit
		case 0x04, 0x05:
			t.audioControl[idx] = kAudioDiv2Pure
		case 0x06, 0x0A:
			t.audioControl[idx] = kAudioDiv31Pure
		case 0x07:
			t.audioControl[idx] = kAudio5BitDiv2
		case 0x08:
			t.audioControl[idx] = kAudio9Bit
		case 0x09:
			t.audioControl[idx] = kAudio5Bit
		case 0x0B:
			t.audioControl[idx] = kAudioLast4One
		case 0x0C, 0x0D:
			t.audioControl[idx] = kAudioDiv6Pure
		case 0x0E:
			t.audioControl[idx] = kAudioDiv93Pure
		case 0x0F:
			t.audioControl[idx] = kAudio5BitDiv6
		}
	case 0x17, 0x18:
		// AUDF0, AUDF1
		idx := int(addr) - 0x17
		// Only use certain bits.
		val &= kMASK_AUDF
		t.audioDivide[idx] = val
	case 0x19, 0x1A:
		// AUDV0, AUDV1
		idx := int(addr) - 0x19
		// Only use certain bits.
		val &= kMASK_AUDV
		t.audioVolume[idx] = val
	case 0x1B, 0x1C:
		// GRP0, GRP1
		idx := int(addr) - 0x1B
		t.playerGraphic[idx] = val
	case 0x1D, 0x1E:
		// ENAM0, ENAM1
		idx := int(addr) - 0x1D
		t.missleEnabled[idx] = false
		if (val & kMASK_ENAMB) == kMASK_ENAMB {
			t.missleEnabled[idx] = true
		}
	case 0x1F:
		// ENABL
		t.ballEnabled = false
		if (val & kMASK_ENAMB) == kMASK_ENAMB {
			t.ballEnabled = true
		}
	case 0x20, 0x21, 0x22, 0x23, 0x24:
		// HMP0, HMP1, HMM0, HMM1, HMBL
		// This only appears in the high bits but we want to convert it to a signed
		// 2's complement value for later
		val >>= kShiftNmHM
		if (val & kMASK_HM_NEG) == kMASK_HM_NEG {
			val |= kMASK_HM_SIGN_EXTEND
		}
		switch addr {
		case 0x20, 0x21:
			idx := int(addr) - 0x20
			t.horizontalMotionPlayers[idx] = val
		case 0x22, 0x23:
			idx := int(addr) - 0x22
			t.horizontalMotionMissles[idx] = val
		case 0x24:
			t.horizontalMotionBall = val
		}
	case 0x25, 0x26:
		// VDELP0, VDELP1
		idx := int(addr) - 0x25
		t.verticalDelayPlayers[idx] = false
		if (val & kMASK_VDEL) == kMASK_VDEL {
			t.verticalDelayPlayers[idx] = true
		}
	case 0x27:
		// VDELBL
		t.veritcalDelayBall = false
		if (val & kMASK_VDEL) == kMASK_VDEL {
			t.veritcalDelayBall = true
		}
	case 0x28, 0x29:
		// RESMP0, RESMP1
		idx := int(addr) - 0x28
		t.missleLockedPlayer[idx] = false
		if (val & kMASK_RESMP) == kMASK_RESMP {
			t.missleLockedPlayer[idx] = true
		}
	case 0x2A:
		// HMOVE
		t.hmove = true
	case 0x2B:
		// HMCLR
		t.horizontalMotionPlayers[0] = 0x00
		t.horizontalMotionPlayers[1] = 0x00
		t.horizontalMotionMissles[0] = 0x00
		t.horizontalMotionMissles[1] = 0x00
		t.horizontalMotionBall = 0x00
	case 0x2C:
		// CXCLR
		for i := range t.collision {
			t.collision[i] = 0x00
		}
	default:
		// These are undefined and do nothing.
	}
}

func decodeColor(mode TIAMode, val uint8) *color.NRGBA {
	// Limit to 128 values
	val &= 0x7F
	var out *color.NRGBA
	switch mode {
	case TIA_MODE_NTSC:
		out = kNTSC[int(val)]
	case TIA_MODE_PAL:
		out = kPAL[int(val)]
	case TIA_MODE_SECAM:
		out = kSECAM[int(val)]
	default:
		panic(fmt.Sprintf("Impossible mode: %d", mode))
	}
	return out
}

// Tick does a single clock cycle on the chip which usually is running 3x the
// speed of a CPU. It's up to implementations to run these at whatever rates are
// needed and add delay for total cycle time needed.
// Every tick involves a pixel change to the display.
func (t *TIA) Tick() error {
	// Most of this is a giant state machine where certain things take priority.
	var c *color.NRGBA
	switch {
	case t.vsync, t.vblank, t.hPos < kHblank:
		// Always black
		c = kBlack
	default:
		c = t.colors[kBackground]
	}
	// Start of line always resets the rdy line.
	if t.hPos == 0 {
		t.rdy = false
	}
	// Every tick outputs a pixel
	//	fmt.Printf("Setting %d,%d to %+v\n", t.hPos, t.vPos, c)
	t.picture.Set(t.hPos, t.vPos, c)
	t.hPos++
	// Wrap on the end of line. Up to CPU code to count lines and trigger vPos reset.
	if t.hPos >= t.picture.Bounds().Max.X {
		t.hPos = 0
		t.vPos++
	}
	return nil
}

var (
	// Constant needed for vblank/vsync/hblank:
	kBlack = &color.NRGBA{0x00, 0x00, 0x00, 0xFF}

	// Using values from
	// http://www.randomterrain.com/atari-2600-memories-tia-color-charts.html
	kNTSC = [128]*color.NRGBA{
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Grey
		&color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF},
		&color.NRGBA{0x39, 0x39, 0x39, 0xFF},
		&color.NRGBA{0x5B, 0x5B, 0x5B, 0xFF},
		&color.NRGBA{0x7E, 0x7E, 0x7E, 0xFF},
		&color.NRGBA{0xA2, 0xA2, 0xA2, 0xFF},
		&color.NRGBA{0xC7, 0xC7, 0xC7, 0xFF},
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF},
		&color.NRGBA{0x1D, 0x00, 0x00, 0xFF}, // Gold
		&color.NRGBA{0x3E, 0x1E, 0x00, 0xFF},
		&color.NRGBA{0x61, 0x41, 0x00, 0xFF},
		&color.NRGBA{0x86, 0x65, 0x00, 0xFF},
		&color.NRGBA{0xAB, 0x8A, 0x00, 0xFF},
		&color.NRGBA{0xCF, 0xB1, 0x00, 0xFF},
		&color.NRGBA{0xF4, 0xD7, 0x00, 0xFF},
		&color.NRGBA{0xF9, 0xFF, 0x29, 0xFF},
		&color.NRGBA{0x3D, 0x00, 0x00, 0xFF}, // Orange
		&color.NRGBA{0x67, 0x00, 0x00, 0xFF},
		&color.NRGBA{0x8F, 0x17, 0x00, 0xFF},
		&color.NRGBA{0xB7, 0x3F, 0x00, 0xFF},
		&color.NRGBA{0xDE, 0x65, 0x00, 0xFF},
		&color.NRGBA{0xFF, 0x8A, 0x00, 0xFF},
		&color.NRGBA{0xFF, 0xB4, 0x0C, 0xFF},
		&color.NRGBA{0xFF, 0xE3, 0x65, 0xFF},
		&color.NRGBA{0x4F, 0x00, 0x00, 0xFF}, // Bright orange
		&color.NRGBA{0x7F, 0x00, 0x00, 0xFF},
		&color.NRGBA{0xA7, 0x00, 0x00, 0xFF},
		&color.NRGBA{0xD0, 0x06, 0x00, 0xFF},
		&color.NRGBA{0xF8, 0x3C, 0x0F, 0xFF},
		&color.NRGBA{0xFF, 0x66, 0x44, 0xFF},
		&color.NRGBA{0xFF, 0x94, 0x78, 0xFF},
		&color.NRGBA{0xFF, 0xC2, 0xB8, 0xFF},
		&color.NRGBA{0x4C, 0x00, 0x00, 0xFF}, // Pink
		&color.NRGBA{0x7B, 0x00, 0x15, 0xFF},
		&color.NRGBA{0xA6, 0x00, 0x38, 0xFF},
		&color.NRGBA{0xCE, 0x00, 0x5B, 0xFF},
		&color.NRGBA{0xF7, 0x00, 0x7E, 0xFF},
		&color.NRGBA{0xFF, 0x44, 0xA4, 0xFF},
		&color.NRGBA{0xFF, 0x76, 0xD4, 0xFF},
		&color.NRGBA{0xFF, 0xAA, 0xF5, 0xFF},
		&color.NRGBA{0x36, 0x00, 0x4A, 0xFF}, // Purple
		&color.NRGBA{0x63, 0x00, 0x67, 0xFF},
		&color.NRGBA{0x8E, 0x00, 0x8C, 0xFF},
		&color.NRGBA{0xB5, 0x00, 0xB1, 0xFF},
		&color.NRGBA{0xDE, 0x00, 0xD7, 0xFF},
		&color.NRGBA{0xFF, 0x20, 0xFE, 0xFF},
		&color.NRGBA{0xFF, 0x6C, 0xF5, 0xFF},
		&color.NRGBA{0xFF, 0xA8, 0xF6, 0xFF},
		&color.NRGBA{0x26, 0x00, 0x84, 0xFF}, // Purple-blue
		&color.NRGBA{0x47, 0x00, 0xA4, 0xFF},
		&color.NRGBA{0x6B, 0x00, 0xCB, 0xFF},
		&color.NRGBA{0x90, 0x00, 0xF3, 0xFF},
		&color.NRGBA{0xB3, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0xD8, 0x4E, 0xFF, 0xFF},
		&color.NRGBA{0xFE, 0x82, 0xFE, 0xFF},
		&color.NRGBA{0xFF, 0xB5, 0xF8, 0xFF},
		&color.NRGBA{0x24, 0x00, 0x93, 0xFF}, // Blue
		&color.NRGBA{0x34, 0x00, 0xC0, 0xFF},
		&color.NRGBA{0x4A, 0x00, 0xE7, 0xFF},
		&color.NRGBA{0x63, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0x7D, 0x43, 0xFF, 0xFF},
		&color.NRGBA{0x9B, 0x79, 0xFF, 0xFF},
		&color.NRGBA{0xBE, 0xA7, 0xFF, 0xFF},
		&color.NRGBA{0xE3, 0xD4, 0xFF, 0xFF},
		&color.NRGBA{0x1A, 0x00, 0x73, 0xFF}, // Blue
		&color.NRGBA{0x29, 0x00, 0xAD, 0xFF},
		&color.NRGBA{0x30, 0x00, 0xD9, 0xFF},
		&color.NRGBA{0x3C, 0x3E, 0xFF, 0xFF},
		&color.NRGBA{0x44, 0x72, 0xFF, 0xFF},
		&color.NRGBA{0x5B, 0x9F, 0xFF, 0xFF},
		&color.NRGBA{0x77, 0xCD, 0xFF, 0xFF},
		&color.NRGBA{0x9A, 0xF9, 0xFF, 0xFF},
		&color.NRGBA{0x03, 0x08, 0x3B, 0xFF}, // Light blue
		&color.NRGBA{0x01, 0x2A, 0x6F, 0xFF},
		&color.NRGBA{0x00, 0x4D, 0xA4, 0xFF},
		&color.NRGBA{0x00, 0x73, 0xCB, 0xFF},
		&color.NRGBA{0x00, 0x99, 0xF2, 0xFF},
		&color.NRGBA{0x00, 0xC1, 0xFF, 0xFF},
		&color.NRGBA{0x00, 0xED, 0xFF, 0xFF},
		&color.NRGBA{0x5A, 0xFF, 0xFF, 0xFF},
		&color.NRGBA{0x00, 0x22, 0x03, 0xFF}, // Turquoise
		&color.NRGBA{0x00, 0x47, 0x28, 0xFF},
		&color.NRGBA{0x00, 0x6D, 0x59, 0xFF},
		&color.NRGBA{0x00, 0x92, 0x7C, 0xFF},
		&color.NRGBA{0x00, 0xB7, 0xA1, 0xFF},
		&color.NRGBA{0x00, 0xDE, 0xC7, 0xFF},
		&color.NRGBA{0x00, 0xFF, 0xED, 0xFF},
		&color.NRGBA{0x45, 0xFF, 0xFF, 0xFF},
		&color.NRGBA{0x00, 0x27, 0x04, 0xFF}, // Green blue
		&color.NRGBA{0x00, 0x4F, 0x08, 0xFF},
		&color.NRGBA{0x00, 0x77, 0x11, 0xFF},
		&color.NRGBA{0x00, 0x9E, 0x2F, 0xFF},
		&color.NRGBA{0x00, 0xC5, 0x4F, 0xFF},
		&color.NRGBA{0x00, 0xEC, 0x71, 0xFF},
		&color.NRGBA{0x00, 0xFF, 0x95, 0xFF},
		&color.NRGBA{0x5F, 0xFF, 0xB7, 0xFF},
		&color.NRGBA{0x00, 0x24, 0x03, 0xFF}, // Green
		&color.NRGBA{0x00, 0x4B, 0x06, 0xFF},
		&color.NRGBA{0x00, 0x72, 0x05, 0xFF},
		&color.NRGBA{0x00, 0x99, 0x07, 0xFF},
		&color.NRGBA{0x00, 0xC0, 0x10, 0xFF},
		&color.NRGBA{0x00, 0xE7, 0x2B, 0xFF},
		&color.NRGBA{0x3D, 0xFF, 0x4A, 0xFF},
		&color.NRGBA{0x9B, 0xFF, 0x67, 0xFF},
		&color.NRGBA{0x00, 0x17, 0x01, 0xFF}, // Yellow green
		&color.NRGBA{0x00, 0x3A, 0x01, 0xFF},
		&color.NRGBA{0x13, 0x5E, 0x00, 0xFF},
		&color.NRGBA{0x3C, 0x84, 0x00, 0xFF},
		&color.NRGBA{0x5F, 0xAB, 0x00, 0xFF},
		&color.NRGBA{0x83, 0xD2, 0x00, 0xFF},
		&color.NRGBA{0xA8, 0xF9, 0x03, 0xFF},
		&color.NRGBA{0xD8, 0xFF, 0x2E, 0xFF},
		&color.NRGBA{0x1E, 0x00, 0x00, 0xFF}, // Orange green
		&color.NRGBA{0x3F, 0x1E, 0x00, 0xFF},
		&color.NRGBA{0x62, 0x41, 0x00, 0xFF},
		&color.NRGBA{0x87, 0x65, 0x00, 0xFF},
		&color.NRGBA{0xAC, 0x8A, 0x00, 0xFF},
		&color.NRGBA{0xD1, 0xB1, 0x00, 0xFF},
		&color.NRGBA{0xF7, 0xD7, 0x00, 0xFF},
		&color.NRGBA{0xF9, 0xff, 0x29, 0xFF},
		&color.NRGBA{0x3E, 0x00, 0x00, 0xFF}, // Light orange
		&color.NRGBA{0x68, 0x00, 0x00, 0xFF},
		&color.NRGBA{0x90, 0x16, 0x00, 0xFF},
		&color.NRGBA{0xB8, 0x3F, 0x00, 0xFF},
		&color.NRGBA{0xDF, 0x63, 0x00, 0xFF},
		&color.NRGBA{0xFF, 0x8A, 0x00, 0xFF},
		&color.NRGBA{0xFF, 0xB4, 0x0F, 0xFF},
		&color.NRGBA{0xFF, 0xE3, 0x66, 0xFF},
	}
	kPAL = [128]*color.NRGBA{
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Grey
		&color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF},
		&color.NRGBA{0x39, 0x39, 0x39, 0xFF},
		&color.NRGBA{0x5B, 0x5B, 0x5B, 0xFF},
		&color.NRGBA{0x7E, 0x7E, 0x7E, 0xFF},
		&color.NRGBA{0xA2, 0xA2, 0xA2, 0xFF},
		&color.NRGBA{0xC7, 0xC7, 0xC7, 0xFF},
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF},
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Grey
		&color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF},
		&color.NRGBA{0x39, 0x39, 0x39, 0xFF},
		&color.NRGBA{0x5B, 0x5B, 0x5B, 0xFF},
		&color.NRGBA{0x7E, 0x7E, 0x7E, 0xFF},
		&color.NRGBA{0xA2, 0xA2, 0xA2, 0xFF},
		&color.NRGBA{0xC7, 0xC7, 0xC7, 0xFF},
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF},
		&color.NRGBA{0x23, 0x00, 0x00, 0xFF}, // Gold
		&color.NRGBA{0x44, 0x1A, 0x00, 0xFF},
		&color.NRGBA{0x68, 0x3C, 0x00, 0xFF},
		&color.NRGBA{0x8E, 0x60, 0x00, 0xFF},
		&color.NRGBA{0xB3, 0x84, 0x00, 0xFF},
		&color.NRGBA{0xD7, 0xAB, 0x00, 0xFF},
		&color.NRGBA{0xFE, 0xD1, 0x00, 0xFF},
		&color.NRGBA{0xFA, 0xFD, 0x38, 0xFF},
		&color.NRGBA{0x00, 0x24, 0x01, 0xFF}, // Green
		&color.NRGBA{0x00, 0x4B, 0x02, 0xFF},
		&color.NRGBA{0x00, 0x71, 0x05, 0xFF},
		&color.NRGBA{0x00, 0x99, 0x07, 0xFF},
		&color.NRGBA{0x00, 0xC0, 0x06, 0xFF},
		&color.NRGBA{0x00, 0xE6, 0x09, 0xFF},
		&color.NRGBA{0x4E, 0xFF, 0x25, 0xFF},
		&color.NRGBA{0xA2, 0xFF, 0x48, 0xFF},
		&color.NRGBA{0x41, 0x00, 0x00, 0xFF}, // Bright Orange
		&color.NRGBA{0x6C, 0x00, 0x00, 0xFF},
		&color.NRGBA{0x95, 0x0F, 0x00, 0xFF},
		&color.NRGBA{0xBC, 0x39, 0x00, 0xFF},
		&color.NRGBA{0xE2, 0x5E, 0x00, 0xFF},
		&color.NRGBA{0xFF, 0x83, 0x15, 0xFF},
		&color.NRGBA{0xFF, 0xAE, 0x4F, 0xFF},
		&color.NRGBA{0xFF, 0xDE, 0x80, 0xFF},
		&color.NRGBA{0x00, 0x28, 0x01, 0xFF}, // Green
		&color.NRGBA{0x00, 0x50, 0x03, 0xFF},
		&color.NRGBA{0x00, 0x79, 0x06, 0xFF},
		&color.NRGBA{0x00, 0x9F, 0x0B, 0xFF},
		&color.NRGBA{0x00, 0xC7, 0x23, 0xFF},
		&color.NRGBA{0x00, 0xEE, 0x41, 0xFF},
		&color.NRGBA{0x00, 0xFF, 0x62, 0xFF},
		&color.NRGBA{0x65, 0xFF, 0x8B, 0xFF},
		&color.NRGBA{0x4F, 0x00, 0x00, 0xFF}, // Pink
		&color.NRGBA{0x7E, 0x00, 0x00, 0xFF},
		&color.NRGBA{0xA6, 0x00, 0x11, 0xFF},
		&color.NRGBA{0xCF, 0x00, 0x38, 0xFF},
		&color.NRGBA{0xF8, 0x2F, 0x5D, 0xFF},
		&color.NRGBA{0xFF, 0x5C, 0x82, 0xFF},
		&color.NRGBA{0xFF, 0x8B, 0xAE, 0xFF},
		&color.NRGBA{0xFF, 0xB8, 0xD8, 0xFF},
		&color.NRGBA{0x00, 0x24, 0x01, 0xFF}, // Green blue
		&color.NRGBA{0x00, 0x4A, 0x07, 0xFF},
		&color.NRGBA{0x00, 0x72, 0x29, 0xFF},
		&color.NRGBA{0x00, 0x98, 0x49, 0xFF},
		&color.NRGBA{0x00, 0xBE, 0x6C, 0xFF},
		&color.NRGBA{0x00, 0xE6, 0x8F, 0xFF},
		&color.NRGBA{0x00, 0xFF, 0xB5, 0xFF},
		&color.NRGBA{0x41, 0xFF, 0xE0, 0xFF},
		&color.NRGBA{0x49, 0x00, 0x25, 0xFF}, // Pink Purple
		&color.NRGBA{0x78, 0x00, 0x4D, 0xFF},
		&color.NRGBA{0xA3, 0x00, 0x70, 0xFF},
		&color.NRGBA{0xCD, 0x00, 0x96, 0xFF},
		&color.NRGBA{0xF6, 0x00, 0xBB, 0xFF},
		&color.NRGBA{0xFF, 0x29, 0xE1, 0xFF},
		&color.NRGBA{0xFF, 0x6A, 0xFD, 0xFF},
		&color.NRGBA{0xFF, 0xA8, 0xFD, 0xFF},
		&color.NRGBA{0x00, 0x0F, 0x2B, 0xFF}, // Light blue
		&color.NRGBA{0x00, 0x33, 0x50, 0xFF},
		&color.NRGBA{0x00, 0x59, 0x77, 0xFF},
		&color.NRGBA{0x00, 0x7D, 0x9D, 0xFF},
		&color.NRGBA{0x00, 0xA2, 0xC2, 0xFF},
		&color.NRGBA{0x00, 0xC8, 0xE9, 0xFF},
		&color.NRGBA{0x00, 0xEF, 0xFF, 0xFF},
		&color.NRGBA{0x54, 0xFF, 0xFF, 0xFF},
		&color.NRGBA{0x36, 0x00, 0x66, 0xFF}, // Purple
		&color.NRGBA{0x63, 0x00, 0x93, 0xFF},
		&color.NRGBA{0x8C, 0x00, 0x8A, 0xFF},
		&color.NRGBA{0x83, 0x00, 0xE1, 0xFF},
		&color.NRGBA{0xDC, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0xFF, 0x23, 0xFE, 0xFF},
		&color.NRGBA{0xFF, 0x6A, 0xFD, 0xFF},
		&color.NRGBA{0xFF, 0xA8, 0xFD, 0xFF},
		&color.NRGBA{0x18, 0x00, 0x6C, 0xFF}, // Blue
		&color.NRGBA{0x20, 0x00, 0x96, 0xFF},
		&color.NRGBA{0x22, 0x25, 0xBF, 0xFF},
		&color.NRGBA{0x2D, 0x4F, 0xE5, 0xFF},
		&color.NRGBA{0x3E, 0x77, 0xFF, 0xFF},
		&color.NRGBA{0x51, 0xA3, 0xFF, 0xFF},
		&color.NRGBA{0x6E, 0xD1, 0xFF, 0xFF},
		&color.NRGBA{0x90, 0xFD, 0xFF, 0xFF},
		&color.NRGBA{0x27, 0x00, 0x90, 0xFF}, // Purple
		&color.NRGBA{0x47, 0x00, 0xBF, 0xFF},
		&color.NRGBA{0x68, 0x00, 0xE7, 0xFF},
		&color.NRGBA{0x8A, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0xAB, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0xCF, 0x55, 0xFF, 0xFF},
		&color.NRGBA{0xF5, 0x88, 0xFE, 0xFF},
		&color.NRGBA{0xFF, 0xB7, 0xFE, 0xFF},
		&color.NRGBA{0x24, 0x00, 0x92, 0xFF}, // Light blue
		&color.NRGBA{0x33, 0x00, 0xC0, 0xFF},
		&color.NRGBA{0x47, 0x00, 0xE7, 0xFF},
		&color.NRGBA{0x5F, 0x00, 0xFF, 0xFF},
		&color.NRGBA{0x77, 0x49, 0xFF, 0xFF},
		&color.NRGBA{0x95, 0x7E, 0xFF, 0xFF},
		&color.NRGBA{0xB8, 0xAB, 0xFF, 0xFF},
		&color.NRGBA{0xDC, 0xD8, 0xFF, 0xFF},
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Grey
		&color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF},
		&color.NRGBA{0x39, 0x39, 0x39, 0xFF},
		&color.NRGBA{0x5B, 0x5B, 0x5B, 0xFF},
		&color.NRGBA{0x7E, 0x7E, 0x7E, 0xFF},
		&color.NRGBA{0xA2, 0xA2, 0xA2, 0xFF},
		&color.NRGBA{0xC7, 0xC7, 0xC7, 0xFF},
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF},
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Grey
		&color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF},
		&color.NRGBA{0x39, 0x39, 0x39, 0xFF},
		&color.NRGBA{0x5B, 0x5B, 0x5B, 0xFF},
		&color.NRGBA{0x7E, 0x7E, 0x7E, 0xFF},
		&color.NRGBA{0xA2, 0xA2, 0xA2, 0xFF},
		&color.NRGBA{0xC7, 0xC7, 0xC7, 0xFF},
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF},
	}
	kSECAM = [128]*color.NRGBA{ // Same repeated every 8
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
		&color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // Black
		&color.NRGBA{0x4E, 0x00, 0xFE, 0xFF}, // Blue
		&color.NRGBA{0xFF, 0x00, 0x6F, 0xFF}, // Red
		&color.NRGBA{0xFF, 0x00, 0xFE, 0xFF}, // Purple
		&color.NRGBA{0x00, 0xFF, 0x08, 0xFF}, // Green
		&color.NRGBA{0x2C, 0xFF, 0xFF, 0xFF}, // Turquoise
		&color.NRGBA{0x77, 0xFE, 0x27, 0xFF}, // Yellow
		&color.NRGBA{0xED, 0xED, 0xED, 0xFF}, // Light Grey
	}
)
