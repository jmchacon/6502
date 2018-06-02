// Package tia implements the TIA chip used in an atari 2600 for display/sound.
package tia

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"time"

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
	// Convention for constants:
	//
	// All caps - uint8 register locations/values/masks
	// Mixed case - Integer constants used for computing screen locations/offsets.

	// All screens are the same width and same visible drawing area.
	kWidth   = 228
	kVisible = 160

	// An NTSC TIA Frame is 228x262 though visible area is only 160x192 due to overscan
	// and hblank regions.
	kNTSCWidth         = kWidth
	kNTSCPictureStart  = kHblank
	kNTSCPictureMiddle = kNTSCPictureStart + ((kNTSCWidth - kNTSCPictureStart) / 2)
	kNTSCHeight        = 262
	kNTSCVBLANKLines   = 37 // Doesn't include VSYNC.
	kNTSCFrameLines    = 192
	kNTSCOverscanLines = 30
	kNTSCTopBlank      = kNTSCVBLANKLines + kVSYNCLines
	kNTSCOverscanStart = kNTSCTopBlank + kNTSCFrameLines

	// A PAL/SECAM TIA Frame is 228x312 though visible area is only 160x228 due to overscan
	// and hblank regions.
	kPALWidth         = kWidth
	kPALPictureStart  = kHblank
	kPALPictureMiddle = kPALPictureStart + ((kPALWidth - kPALPictureStart) / 2)
	kPALHeight        = 312
	kPALVBLANKLines   = 45 // Doesn't include VSYNC.
	kPALFrameLines    = 228
	kPALOverscanLines = 36
	kPALTopBlank      = kPALVBLANKLines + kVSYNCLines
	kPALOverscanStart = kPALTopBlank + kPALFrameLines

	// All implementations do the same VSYNC lines
	kVSYNCLines = 3

	// Always 68 hblank clocks
	kHblank = 68

	// Strip to 4 bits for internal regs.
	kMASK_READ = uint16(0x0F)

	// Mask for XOR to flip D7.
	kMASK_D7 = uint8(0x80)

	// Strip to 6 bits for internal regs.
	kMASK_WRITE = uint16(0x3F)

	kMASK_READ_OUTPUT = uint8(0xC0) // Only D7/6 defined on the bus for reads.

	kMASK_VSYNC     = uint8(0x02) // Trigger bit for VSYNC (others ignored).
	kMASK_VSYNC_OFF = uint8(0x00)

	kMASK_VBL_VBLANK      = uint8(0x02)
	kMASK_VBL_I45_LATCHES = uint8(0x40)
	kMASK_VBL_I0I3_GROUND = uint8(0x80)
	kMASK_VBL_VBLANK_OFF  = uint8(0x00)

	kPORT_OUTPUT = uint8(0x80)

	kMASK_NUSIZ_MISSILE = uint8(0x30)

	kMASK_NUSIZ_PLAYER = uint8(0x07)

	kMASK_REFPX = uint8(0x08)

	kMASK_PF0 = uint8(0xF0)

	kPF0Pixels = 16
	kPF1Pixels = 32
	kPF2Pixels = 32

	kMASK_AUDC = uint8(0x0F)

	kMASK_AUDF = uint8(0x1F)

	kMASK_AUDV = uint8(0x0F)

	kMASK_ENAMB = uint8(0x02) // Missile and ball enable use the same mask

	kShiftNmHM = 4

	kMASK_VDEL = uint8(0x01)

	kMASK_RESMP = uint8(0x02)

	kMASK_REF        = uint8(0x01)
	kMASK_SCORE      = uint8(0x02)
	kMASK_PFP        = uint8(0x04)
	kMASK_BALL_SIZE  = uint8(0x30)
	kMASK_REF_OFF    = uint8(0x00)
	kMASK_SCORE_OFF  = uint8(0x00)
	kMASK_PFP_NORMAL = uint8(0x00)

	// These are shifted down for easier comparisons against the clock.
	// The actual values stored in the upper nibble on write.
	kShiftWidth      = 4
	kBALL_WIDTH_1    = uint8(0x00)
	kBALL_WIDTH_2    = uint8(0x10)
	kBALL_WIDTH_4    = uint8(0x20)
	kBALL_WIDTH_8    = uint8(0x30)
	kMISSILE_WIDTH_1 = uint8(0x00)
	kMISSILE_WIDTH_2 = uint8(0x10)
	kMISSILE_WIDTH_4 = uint8(0x20)
	kMISSILE_WIDTH_8 = uint8(0x30)

	kMASK_NUSIZ_PLAYER_ONE         = uint8(0x00)
	kMASK_NUSIZ_PLAYER_TWO_CLOSE   = uint8(0x01)
	kMASK_NUSIZ_PLAYER_TWO_MED     = uint8(0x02)
	kMASK_NUSIZ_PLAYER_THREE_CLOSE = uint8(0x03)
	kMASK_NUSIZ_PLAYER_TWO_WIDE    = uint8(0x04)
	kMASK_NUSIZ_PLAYER_DOUBLE      = uint8(0x05)
	kMASK_NUSIZ_PLAYER_THREE_MED   = uint8(0x06)
	kMASK_NUSIZ_PLAYER_QUAD        = uint8(0x07)

	kMASK_AUDIO_OFF             = uint8(0x00)
	kMASK_AUDIO_4BIT            = uint8(0x01)
	kMASK_AUDIO_DIV15_4BIT      = uint8(0x02)
	kMASK_AUDIO_5BIT_4BIT       = uint8(0x03)
	kMASK_AUDIO_DIV2_PURE       = uint8(0x04)
	kMASK_AUDIO_DIV2_PURE_COPY  = uint8(0x05)
	kMASK_AUDIO_DIV31_PURE      = uint8(0x06)
	kMASK_AUDIO_DIV31_PURE_COPY = uint8(0x0A)
	kMASK_AUDIO_5BIT_DIV2       = uint8(0x07)
	kMASK_AUDIO_9BIT            = uint8(0x08)
	kMASK_AUDIO_5BIT            = uint8(0x09)
	kMASK_AUDIO_LAST4_ONE       = uint8(0x0B)
	kMASK_AUDIO_DIV6_PURE       = uint8(0x0C)
	kMASK_AUDIO_DIV6_PURE_COPY  = uint8(0x0D)
	kMASK_AUDIO_DIV93_PURE      = uint8(0x0E)
	kMASK_AUDIO_5BIT_DIV6       = uint8(0x0F)

	// Index positions in playerXGraphic for old and new slots.
	kOld = 0
	kNew = 1

	kCLOCK_RESET = uint8(156)

	kCLEAR_MOTION    = uint8(0x08)
	kCLEAR_COLLISION = uint8(0x00)

	kPLAYFIELD_CHECK_BIT = uint32(0x0001)

	kHMOVE_COUNTER_RESET = uint8(0x0F) // The initial ripple counter value (D7 inverted and shifted down)

	// Mask bits to determine H1 vs H2 clock. Match below to determine specific phase.
	kMASK_Hx_CLOCK = uint8(0x03)

	kMASK_H1_CLOCK = uint8(0x01)
	kMASK_H2_CLOCK = uint8(0x03)

	kMASK_HMOVE_DONE = uint8(0x0F)

	kMOVE_LEFT7  = uint8(0x70)
	kMOVE_LEFT6  = uint8(0x60)
	kMOVE_LEFT5  = uint8(0x50)
	kMOVE_LEFT4  = uint8(0x40)
	kMOVE_LEFT3  = uint8(0x30)
	kMOVE_LEFT2  = uint8(0x20)
	kMOVE_LEFT1  = uint8(0x10)
	kMOVE_NONE   = uint8(0x00)
	kMOVE_RIGHT1 = uint8(0xF0)
	kMOVE_RIGHT2 = uint8(0xE0)
	kMOVE_RIGHT3 = uint8(0xD0)
	kMOVE_RIGHT4 = uint8(0xC0)
	kMOVE_RIGHT5 = uint8(0xB0)
	kMOVE_RIGHT6 = uint8(0xA0)
	kMOVE_RIGHT7 = uint8(0x90)
	kMOVE_RIGHT8 = uint8(0x80)
)

type hMoveState int

const (
	kHmoveStateNotRunning = iota
	kHmoveStateRunning
	kHmoveStateStart
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
// NOTE: Most of the state is also contained in a set of shadow register
//       as loads that occur during a clock cycle can't affect output that
//       is also happening on that cycle. So these are cached until TickDone()
//       to simulate register outputs for the next cycle.
type TIA struct {
	mode     TIAMode
	tickDone bool // True if TickDone() was called before the current Tick() call.
	h        int  // Height of picture.
	w        int  // Width of picture.
	center   bool // Whether or not painting is past center.
	// NOTE: Collision bits are stored as they are expected to return to
	//       avoid lots of shifting and masking if stored in a uint16.
	//       But store as an array so they can be easily reset.
	collision                     [8]uint8          // Collission bits (see constants below for various ones).
	inputPorts                    [6]io.PortIn1     // If non-nil defines the input port for the given paddle/joystick.
	ioPortGnd                     func()            // If non-nil is called when I0-3 are grounded via VBLANK.7.
	outputLatches                 [2]bool           // The output latches (if used) for ports 4/5.
	rdy                           bool              // If true then RDY out (which should go to RDY on cpu) is signaled high via Raised().
	shadowRdy                     bool              // Shadow reg for RDY set in TickDone().
	vsync                         bool              // If true in VSYNC mode.
	shadowVsync                   bool              // Shadow reg for VSYNC set in TickDone().
	vblank                        bool              // If true in VBLANK mode.
	shadowVblank                  bool              // Shadow reg for VBLANK.
	hblank                        bool              // If true in HBLANK mode.
	rsync                         bool              // If true RSYNC was triggered and the hClock should reset.
	frameReset                    bool              // Is true we should reset the frame state.
	lateHblank                    bool              // It true don't disable HBLANK initially but pause 8 more pixels (for HMOVE).
	latches                       bool              // If true then I4/I5 in latch mode.
	groundInput                   bool              // If true then I0-I3 are grounded and always return 0.
	missileWidth                  [2]uint8          // Width of missiles in pixels (1,2,4,8).
	shadowMissileWidth            [2]uint8          // Shadow regs of missileWidth to load on TickDone().
	playerCntWidth                [2]playerCntWidth // Player 0,1 count and width (see enum).
	shadowPlayerCntWidth          [2]playerCntWidth // Shadow regs of playerCntWidth to load on TickDone().
	colors                        [4]*color.NRGBA   // Player 0,1, playfield and background color.
	shadowColors                  [4]*color.NRGBA   // Shadow regs of colors to load on TickDone().
	reflectPlayers                [2]bool           // Player 0,1 reflection bits.
	shadowReflectPlayers          [2]bool           // Shadow regs of reflectPlayers to load on TickDone().
	playfield                     [3]uint8          // PF0-3 regs.
	shadowPlayfield               [3]uint8          // Shadow regs of playfield to load on TickDone().
	pf                            uint32            // The non-reflected 20 bits of the playfield shifted correctly.
	pfReflect                     uint32            // The reflected 20 bits of the playfield shifted correctly.
	hPos                          int               // Current horizontal position.
	vPos                          int               // Current vertical position.
	hClock                        uint8             // Horizontal clock which wraps at kWidth.
	playerClock                   [2]uint8          // Player 0,1 clock current values. Only runs during visible portion and wraps at kVisible.
	playerReset                   [2]bool           // // Indicates a player was reset and clock should be changed during TickDone().
	missileClock                  [2]uint8          // Missile 0,1 clock current values. Only runs during visible portion and wraps at kVisible.
	missileReset                  [2]bool           // Indicates a missile was reset and clock should be changed during TickDone().
	ballClock                     uint8             // Ball clock current value. Only runs during visible portion and wraps at kVisible.
	ballReset                     bool              // Indicates ball was reset and clock should be changed during TickDone().
	audioControl                  [2]audioStyle     // Audio style for channels 0 and 1.
	audioDivide                   [2]uint8          // Audio divisors for channels 0 and 1.
	audioVolume                   [2]uint8          // Audio volume for channels 0 and 1.
	player0Graphic                [2]uint8          // The player graphics for player 0 (new and old).
	shadowPlayer0Graphic          [2]uint8          // Shadow reg for player0Graphic to load on TickDone().
	player1Graphic                [2]uint8          // The player graphics for player 1 (new and old).
	shadowPlayer1Graphic          [2]uint8          // Shadow reg for player1Graphic to load on TickDone().
	missileEnabled                [2]bool           // Whether the missile is enabled or not.
	shadowMissileEnabled          [2]bool           // Shadows regs for missileEnabled to load on TickDone().
	ballEnabled                   [2]bool           // Whether the ball is enabled or not. (new and old).
	shadowBallEnabled             [2]bool           // Shadow regs for ballEnabled to load on TickDone().
	horizontalMotionPlayer        [2]uint8          // Horizontal motion for players.
	shadowHorizontalMotionPlayer  [2]uint8          // Shadow regs for horizontalMotionPlayers to load on TickDone().
	horizontalMotionMissile       [2]uint8          // Horizontal motion for missiles.
	shadowHorizontalMotionMissile [2]uint8          // Shadow regs for horizontalMotionMissiles to load on TickDone().
	horizontalMotionBall          uint8             // Horizontal motion for ball.
	shadowHorizontalMotionBall    uint8             // Shadow reg for horizontalMotionBall to load on TickDone().
	verticalDelayPlayer           [2]bool           // Whether to delay player 0,1 by one line.
	shadowVerticalDelayPlayer     [2]bool           // Shadow regs for verticalDelayPlayers to load on TickDone().
	verticalDelayBall             bool              // Whether to delay ball by one line.
	shadowVerticalDelayBall       bool              // Shadow reg for verticalDelayBall to load on TickDone().
	missileLockedPlayer           [2]bool           // Whether the missile is locked to it's player (and disabled).
	shadowMissileLockedPlayer     [2]bool           // Shadow regs for missileLockedPlayer to load on TickDone().
	hmove                         hMoveState        // Whether HMOVE has been triggered or is currently running.
	shadowHmove                   hMoveState        // Shadow reg for hmove.
	hmoveCounter                  uint8             // Ripple counter moving through HMOVE states (15).
	hmovePlayerActive             [2]bool           // Whether HMOVE has completed for the given player.
	hmoveMissileActive            [2]bool           // Whether HMOVE has completed for the given missile.
	hmoveBallActive               bool              // Whether HMOVe has completed for the ball.
	picture                       *image.NRGBA      // The in memory representation of a single frame.
	frameDone                     func(*image.NRGBA)
	reflectPF                     bool  // Whether PF registers reflect or not.
	shadowReflectPF               bool  // Shadow reg for reflectPF to load on TickDone().
	scoreMode                     bool  // If true, use score mode (left PF gets P0 color, right gets P1).
	shadowScoreMode               bool  // Shadow reg for scoreMode to load on TickDone().
	playfieldPriority             bool  // If true playfield has priority over player pixels (player goes behind PF).
	shadowPlayfieldPriority       bool  // Shadow reg for playfieldPriority to load on TickDone().
	ballWidth                     uint8 // Width of ball in pixels (1,2,4,8).
	shadowBallWidth               uint8 // Shadow reg for ballWidth to load on TickDone().
}

// Index references for TIA.colors. These line up with ordering of write registers for each
// (for easy decoding).
const (
	kPlayer0Color = iota
	kPlayer1Color
	kPlayfieldColor
	kBackgroundColor

	kMissile0Color = kPlayer0Color
	kMissile1Color = kPlayer1Color
	kBallColor     = kPlayfieldColor
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
	// The player/missile/ball drawing only happens during visible pixels. But..the start locations
	// aren't defined so we randomize them somewhere on the line. Makes sure that users (and tests)
	// don't assume left edge or anything.
	rand.Seed(time.Now().UnixNano())
	t := &TIA{
		mode:         def.Mode,
		h:            h,
		w:            w,
		tickDone:     true,
		inputPorts:   [6]io.PortIn1{def.Port0, def.Port1, def.Port2, def.Port3, def.Port4, def.Port5},
		picture:      image.NewNRGBA(image.Rect(0, 0, w, h)),
		frameDone:    def.FrameDone,
		vsync:        true, // start in VSYNC mode.
		playerClock:  [2]uint8{uint8(rand.Intn(kVisible)), uint8(rand.Intn(kVisible))},
		missileClock: [2]uint8{uint8(rand.Intn(kVisible)), uint8(rand.Intn(kVisible))},
		ballClock:    uint8(rand.Intn(kVisible)),
		ballWidth:    0x01,
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

// Constants for referencing addresses by well known conventions

const (
	// Read side definitions

	CXM0P  = uint16(0x00)
	CXM1P  = uint16(0x01)
	CXP0FB = uint16(0x02)
	CXP1FB = uint16(0x03)
	CXM0FB = uint16(0x04)
	CXM1FB = uint16(0x05)
	CXBLPF = uint16(0x06)
	CXPPMM = uint16(0x07)
	INPT0  = uint16(0x08)
	INPT1  = uint16(0x09)
	INPT2  = uint16(0x0A)
	INPT3  = uint16(0x0B)
	INPT4  = uint16(0x0C)
	INPT5  = uint16(0x0D)

	// Write side definition

	VSYNC  = uint16(0x00)
	VBLANK = uint16(0x01)
	WSYNC  = uint16(0x02)
	RSYNC  = uint16(0x03)
	NUSIZ0 = uint16(0x04)
	NUSIZ1 = uint16(0x05)
	COLUP0 = uint16(0x06)
	COLUP1 = uint16(0x07)
	COLUPF = uint16(0x08)
	COLUBK = uint16(0x09)
	CTRLPF = uint16(0x0A)
	REFP0  = uint16(0x0B)
	REFP1  = uint16(0x0C)
	PF0    = uint16(0x0D)
	PF1    = uint16(0x0E)
	PF2    = uint16(0x0F)
	RESP0  = uint16(0x10)
	RESP1  = uint16(0x11)
	RESM0  = uint16(0x12)
	RESM1  = uint16(0x13)
	RESBL  = uint16(0x14)
	AUDC0  = uint16(0x15)
	AUDC1  = uint16(0x16)
	AUDF0  = uint16(0x17)
	AUDF1  = uint16(0x18)
	AUDV0  = uint16(0x19)
	AUDV1  = uint16(0x1A)
	GRP0   = uint16(0x1B)
	GRP1   = uint16(0x1C)
	ENAM0  = uint16(0x1D)
	ENAM1  = uint16(0x1E)
	ENABL  = uint16(0x1F)
	HMP0   = uint16(0x20)
	HMP1   = uint16(0x21)
	HMM0   = uint16(0x22)
	HMM1   = uint16(0x23)
	HMBL   = uint16(0x24)
	VDELP0 = uint16(0x25)
	VDELP1 = uint16(0x26)
	VDELBL = uint16(0x27)
	RESMP0 = uint16(0x28)
	RESMP1 = uint16(0x29)
	HMOVE  = uint16(0x2A)
	HMCLR  = uint16(0x2B)
	CXCLR  = uint16(0x2C)
)

// Read returns values based on the given address. The address is masked to 4 bits internally
// (so aliasing across the 6 address pins).
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real reads are happening on clocked CPU Tick()'s.
func (t *TIA) Read(addr uint16) uint8 {
	// Strip to 4 bits for internal regs.
	addr &= kMASK_READ
	var ret uint8
	switch addr {
	case CXM0P:
		ret = t.collision[kCXM0P]
	case CXM1P:
		ret = t.collision[kCXM1P]
	case CXP0FB:
		ret = t.collision[kCXP0FB]
	case CXP1FB:
		ret = t.collision[kCXP1FB]
	case CXM0FB:
		ret = t.collision[kCXM0FB]
	case CXM1FB:
		ret = t.collision[kCXM1FB]
	case CXBLPF:
		ret = t.collision[kCXBLPF]
	case CXPPMM:
		ret = t.collision[kCXPPMM]
	case INPT0, INPT1, INPT2, INPT3:
		idx := int(addr - INPT0)
		if !t.groundInput && t.inputPorts[idx] != nil && t.inputPorts[idx].Input() {
			ret = kPORT_OUTPUT
		}
	case INPT4, INPT5:
		idx := int(addr - INPT4)
		if t.latches {
			if t.outputLatches[idx] {
				ret = kPORT_OUTPUT
				break
			}
		}
		if t.inputPorts[idx+4] != nil && t.inputPorts[idx+4].Input() {
			ret = kPORT_OUTPUT
		}
	default:
		// Couldn't find a definitive answer what happens on
		// undefined addresses so pull them all high.
		ret = 0xFF
	}
	// Apply read mask before returning.
	return ret & kMASK_READ_OUTPUT
}

// Write stores the value at the given address. The address is masked to 6 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real writes are happening on clocked CPU Tick()'s.
func (t *TIA) Write(addr uint16, val uint8) {
	// Strip to 6 bits for internal regs.
	addr &= kMASK_WRITE

	switch addr {
	case VSYNC:
		l := false
		if (val & kMASK_VSYNC) == kMASK_VSYNC {
			l = true
		}
		// If transitioning low->high assume end of frame and do callback and reset
		// coordinates. Can set vsync here since it doesn't trigger anything directly.
		if l && !t.vsync {
			t.frameReset = true
		}
		t.shadowVsync = l
	case VBLANK:
		t.shadowVblank = false
		if (val & kMASK_VBL_VBLANK) == kMASK_VBL_VBLANK {
			t.shadowVblank = true
		}
		// The latches can happen immediately since there's no clocking here.
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
	case WSYNC:
		t.shadowRdy = true
	case RSYNC:
		t.rsync = true
	case NUSIZ0, NUSIZ1:
		idx := int(addr - NUSIZ0)
		// Mask off the missile width and shift down and use that to shift a value in.
		t.shadowMissileWidth[idx] = 1 << ((val & kMASK_NUSIZ_MISSILE) >> kShiftWidth)

		switch val & kMASK_NUSIZ_PLAYER {
		case kMASK_NUSIZ_PLAYER_ONE:
			t.shadowPlayerCntWidth[idx] = kPlayerOne
		case kMASK_NUSIZ_PLAYER_TWO_CLOSE:
			t.shadowPlayerCntWidth[idx] = kPlayerTwoClose
		case kMASK_NUSIZ_PLAYER_TWO_MED:
			t.shadowPlayerCntWidth[idx] = kPlayerTwoMed
		case kMASK_NUSIZ_PLAYER_THREE_CLOSE:
			t.shadowPlayerCntWidth[idx] = kPlayerThreeClose
		case kMASK_NUSIZ_PLAYER_TWO_WIDE:
			t.shadowPlayerCntWidth[idx] = kPlayerTwoWide
		case kMASK_NUSIZ_PLAYER_DOUBLE:
			t.shadowPlayerCntWidth[idx] = kPlayerDouble
		case kMASK_NUSIZ_PLAYER_THREE_MED:
			t.shadowPlayerCntWidth[idx] = kPlayerThreeMed
		case kMASK_NUSIZ_PLAYER_QUAD:
			t.shadowPlayerCntWidth[idx] = kPlayerQuad
		}
	case COLUP0, COLUP1, COLUPF, COLUBK:
		idx := int(addr - COLUP0)
		t.shadowColors[idx] = decodeColor(t.mode, val)
	case CTRLPF:
		t.shadowReflectPF = false
		if (val & kMASK_REF) == kMASK_REF {
			t.shadowReflectPF = true
		}
		t.shadowScoreMode = false
		if (val & kMASK_SCORE) == kMASK_SCORE {
			t.shadowScoreMode = true
		}
		t.shadowPlayfieldPriority = false
		if (val & kMASK_PFP) == kMASK_PFP {
			t.shadowPlayfieldPriority = true
		}
		// Mask off the ball width and shift down and use that to shift a value in.
		t.shadowBallWidth = 1 << ((val & kMASK_BALL_SIZE) >> kShiftWidth)
	case REFP0, REFP1:
		idx := int(addr - REFP0)
		t.shadowReflectPlayers[idx] = false
		if (val & kMASK_REFPX) == kMASK_REFPX {
			t.shadowReflectPlayers[idx] = true
		}
	case PF0, PF1, PF2:
		idx := int(addr - PF0)
		// PF0 only cares about some bits.
		if addr == PF0 {
			val &= kMASK_PF0
		}
		t.shadowPlayfield[idx] = val
	case RESP0, RESP1:
		idx := int(addr - RESP0)
		t.playerReset[idx] = true
	case RESM0, RESM1:
		idx := int(addr - RESM0)
		t.missileReset[idx] = true
	case RESBL:
		t.ballReset = true
	case AUDC0, AUDC1:
		idx := int(addr - AUDC0)
		// Only care about bottom bits
		val &= kMASK_AUDC
		switch val {
		case kMASK_AUDIO_OFF:
			t.audioControl[idx] = kAudioOff
		case kMASK_AUDIO_4BIT:
			t.audioControl[idx] = kAudio4Bit
		case kMASK_AUDIO_DIV15_4BIT:
			t.audioControl[idx] = kAudioDiv154Bit
		case kMASK_AUDIO_5BIT_4BIT:
			t.audioControl[idx] = kAudio5Bit4Bit
		case kMASK_AUDIO_DIV2_PURE, kMASK_AUDIO_DIV2_PURE_COPY:
			t.audioControl[idx] = kAudioDiv2Pure
		case kMASK_AUDIO_DIV31_PURE, kMASK_AUDIO_DIV31_PURE_COPY:
			t.audioControl[idx] = kAudioDiv31Pure
		case kMASK_AUDIO_5BIT_DIV2:
			t.audioControl[idx] = kAudio5BitDiv2
		case kMASK_AUDIO_9BIT:
			t.audioControl[idx] = kAudio9Bit
		case kMASK_AUDIO_5BIT:
			t.audioControl[idx] = kAudio5Bit
		case kMASK_AUDIO_LAST4_ONE:
			t.audioControl[idx] = kAudioLast4One
		case kMASK_AUDIO_DIV6_PURE, kMASK_AUDIO_DIV6_PURE_COPY:
			t.audioControl[idx] = kAudioDiv6Pure
		case kMASK_AUDIO_DIV93_PURE:
			t.audioControl[idx] = kAudioDiv93Pure
		case kMASK_AUDIO_5BIT_DIV6:
			t.audioControl[idx] = kAudio5BitDiv6
		}
	case AUDF0, AUDF1:
		idx := int(addr - AUDF0)
		// Only use certain bits.
		val &= kMASK_AUDF
		t.audioDivide[idx] = val
	case AUDV0, AUDV1:
		idx := int(addr - AUDV0)
		// Only use certain bits.
		val &= kMASK_AUDV
		t.audioVolume[idx] = val
	case GRP0:
		t.shadowPlayer0Graphic[kNew] = val
		// Always copies new to old for other player on load (vertical delay).
		t.shadowPlayer1Graphic[kOld] = t.shadowPlayer1Graphic[kNew]
	case GRP1:
		t.shadowPlayer1Graphic[kNew] = val
		// Always copies new to old for other player on load (vertical delay)
		t.shadowPlayer0Graphic[kOld] = t.shadowPlayer0Graphic[kNew]
		// Loading GRP1 also copies new->old for ball enabled too for vertical delay.
		t.shadowBallEnabled[kOld] = t.shadowBallEnabled[kNew]
	case ENAM0, ENAM1:
		idx := int(addr - ENAM0)
		t.shadowMissileEnabled[idx] = false
		if (val & kMASK_ENAMB) == kMASK_ENAMB {
			t.shadowMissileEnabled[idx] = true
		}
	case ENABL:
		t.shadowBallEnabled[kNew] = false
		if (val & kMASK_ENAMB) == kMASK_ENAMB {
			t.shadowBallEnabled[kNew] = true
		}
	case HMP0, HMP1, HMM0, HMM1, HMBL:
		// Flip bit 7 so this can be used as a comparision
		// against a 15->0 counter more easily. HMOVE runs -8->7 right to left
		// ranges but the real hardware treats it as 0-15 by inverting D7 internally.
		val ^= kMASK_D7

		// This only appears in the high bits but we want it in the lower
		// bits for easy checks later.
		val >>= kShiftNmHM
		switch addr {
		case HMP0, HMP1:
			idx := int(addr - HMP0)
			t.shadowHorizontalMotionPlayer[idx] = val
		case HMM0, HMM1:
			idx := int(addr - HMM0)
			t.shadowHorizontalMotionMissile[idx] = val
		case HMBL:
			t.shadowHorizontalMotionBall = val
		}
	case VDELP0, VDELP1:
		idx := int(addr - VDELP0)
		t.shadowVerticalDelayPlayer[idx] = false
		if (val & kMASK_VDEL) == kMASK_VDEL {
			t.shadowVerticalDelayPlayer[idx] = true
		}
	case VDELBL:
		t.shadowVerticalDelayBall = false
		if (val & kMASK_VDEL) == kMASK_VDEL {
			t.shadowVerticalDelayBall = true
		}
	case RESMP0, RESMP1:
		idx := int(addr - RESMP0)
		t.shadowMissileLockedPlayer[idx] = false
		if (val & kMASK_RESMP) == kMASK_RESMP {
			t.shadowMissileLockedPlayer[idx] = true
		}
	case HMOVE:
		t.shadowHmove = kHmoveStateStart
	case HMCLR:
		t.shadowHorizontalMotionPlayer[0] = kCLEAR_MOTION
		t.shadowHorizontalMotionPlayer[1] = kCLEAR_MOTION
		t.shadowHorizontalMotionMissile[0] = kCLEAR_MOTION
		t.shadowHorizontalMotionMissile[1] = kCLEAR_MOTION
		t.shadowHorizontalMotionBall = kCLEAR_MOTION
	case CXCLR:
		for i := range t.collision {
			t.collision[i] = kCLEAR_COLLISION
		}
	default:
		// These are undefined and do nothing.
	}
}

func decodeColor(mode TIAMode, val uint8) *color.NRGBA {
	// val is only 7 bits but left shifted so fix that
	// to use as an index.
	val >>= 1
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

var reverse8Bit = []uint8{
	0x00, 0x08, 0x04, 0x0C, 0x02, 0x0A, 0x06, 0x0E, 0x01, 0x09, 0x05, 0x0D, 0x03, 0x0B, 0x07, 0x0F,
}

// reverse takes a 8 bit value and reverses the bit order.
func reverse(n uint8) uint8 {
	// Reverse the top and bottom nibble then swap them.
	return (reverse8Bit[n&0x0F] << 4) | reverse8Bit[n>>4]
}

// playfieldOn will return true if the current pixel should have a playfield
// bit displayed. Up to caller to determine priority with this vs ball/player/missile.
func (t *TIA) playfieldOn() bool {
	// If we're not out of HBLANK there's nothing to do
	pos := t.hPos - kHblank
	if pos < 0 {
		return false
	}
	pfBit := uint(pos / 4)
	rightSide := false
	if pfBit > 19 {
		pfBit -= 20
		rightSide = true
	}
	// Adjust into 19-0 range so can be used as a shift below.
	pfBit = 19 - pfBit

	// Now we have a 0-19 value and which side of the screen we're on.

	pf := t.pf
	if t.reflectPF && rightSide {
		pf = t.pfReflect
	}
	if (pf>>pfBit)&kPLAYFIELD_CHECK_BIT == kPLAYFIELD_CHECK_BIT {
		return true
	}
	return false
}

// generatePF should be called anytime PFx registers are changed. The bit pattern
// for both regular and reflected patterns are generated and stored.
func (t *TIA) generatePF() {
	// Assemble PF0/1/2 into a single uint32.
	// Just use lookup tables since this is simpler than bit manipulating every bit.
	// Have to assemble this on every load since the PF regs are allowed to change mid scanline.

	// Reflected version (may not be used if reflection isn't on).
	pf0 := t.playfield[0] >> 4
	pf1 := reverse(t.playfield[1])
	pf2 := t.playfield[2]
	t.pfReflect = (uint32(pf2) << 12) | (uint32(pf1) << 4) | uint32(pf0)

	pf0 = reverse(t.playfield[0])
	pf1 = t.playfield[1]
	pf2 = reverse(t.playfield[2])
	t.pf = (uint32(pf0) << 16) | (uint32(pf1) << 8) | uint32(pf2)
}

// ballOn will return true if the current pixel should have a ball
// bit displayed. Up to caller to determine priority with this vs playfield/player/missile.
func (t *TIA) ballOn() bool {
	// Thankfully the clocks to do this line up with widths.
	if t.ballClock < t.ballWidth {
		// Vertical delay determines old (when on) or new slot (when not) for determining whether to output or not.
		if (t.verticalDelayBall && t.ballEnabled[kOld]) || (!t.verticalDelayBall && t.ballEnabled[kNew]) {
			return true
		}
	}
	return false
}

// Tick does a single clock cycle on the chip which usually is running 3x the
// speed of a CPU. It's up to implementations to run these at whatever rates are
// needed and add delay for total cycle time needed.
// Every tick involves a pixel change to the display.
func (t *TIA) Tick() error {
	if !t.tickDone {
		return errors.New("called Tick() without calling TickDone() at end of last cycle")
	}
	t.tickDone = false

	// Do HMOVE calculations if needed. Can modify state of t.hmove here since it only moves
	// forward through states and an HMOVE would just reset it later so there's no conflicts.
	// Do this in terms of H1/H2 derived clocks since that's how the hardware works.
	// NOTE: Nothing stops this from running outside of hblank which is how the real hardware
	//       does it too. Right shifts are a side effect of the extra hblank comb and clocks
	//       not running when expected.
	if (t.hClock & kMASK_Hx_CLOCK) == kMASK_H1_CLOCK {
		if t.hmove == kHmoveStateStart {
			t.hmove = kHmoveStateRunning
		}
		// The rest of H1 is handled in TickDone() below (see comments).
	}
	if (t.hClock & kMASK_Hx_CLOCK) == kMASK_H2_CLOCK {
		// Only need 15 decrements to hit all states and then it stops until reset happens.
		if t.hmoveCounter != 0x00 {
			t.hmoveCounter--
		}
		if t.hmove == kHmoveStateRunning {
			t.hmove = kHmoveStateNotRunning
			// Don't reset unless the current counter has expired.
			// The hardware does this by only letting SEC reset the counter
			// when it's rippled all the way down to zero.
			if t.hmoveCounter == 0x00 {
				t.hmoveCounter = kHMOVE_COUNTER_RESET
			}
			// Always reset the latch states since SEC has come through
			// by H2 (see schematics).
			t.hmovePlayerActive[0] = true
			t.hmovePlayerActive[1] = true
			t.hmoveMissileActive[0] = true
			t.hmoveMissileActive[1] = true
			t.hmoveBallActive = true
		}
	}

	// Most of this is a giant state machine where certain things take priority.
	// Luckily the TIA itself is so primitive in nature it doesn't actually mutate
	// internal state except for the collision registers on a given cycle. i.e. once
	// registers are set things simply paint the same way every line.
	var c *color.NRGBA
	switch {
	case t.vsync, t.vblank, t.hblank:
		// Always black
		c = kBlack
	case t.ballOn():
		c = t.colors[kBallColor]
	case t.playfieldOn():
		c = t.colors[kPlayfieldColor]
		if t.scoreMode {
			c = t.colors[kPlayer0Color]
			// If we're past visible center use the other player color.
			if t.center {
				c = t.colors[kPlayer1Color]
			}
		}
	default:
		c = t.colors[kBackgroundColor]
	}
	// Every tick outputs a pixel
	t.picture.Set(t.hPos, t.vPos, c)
	return nil
}

func (t *TIA) resetClock(reset *bool, clock *uint8) {
	if *reset {
		*clock = kCLOCK_RESET
		// Technically the sprite does end up on kCLOCK_RESET during hblank
		// since there's a clock before actual pixels show up that bleeds
		// off the start sequence. But that means the clock runs from pixel
		// 64-223 since it's actually a divide by 4 clock and each tick there
		// is really setting up the pixel output for the one after. The real
		// clock running at 228 ticks 4x faster so each state gets replicated
		// 4 times. That's just annoying vs thinking in terms of visible pixels (68-227).
		// So, just correct for this one case here and the rest "just works"
		// since resetting outside of hblank sets the clocks in a pattern
		// that correctly runs off the start bits.
		if t.hblank {
			*clock = (*clock + 4) % kVisible
		}
		*reset = false
	}
}

func (t *TIA) checkHmove(h uint8, a *bool) {
	// Do compares here and set done bits accordingly. Could get stuck
	// and never set done if the register changed mid-stream and we
	// hit the stopping condition with a partial match.
	// NOTE: By compare we mean "all bits are different between counter and comparison".
	//       This is due to how the hardware runs a counter from 15->0 but the HMx
	//       registers (with D7 flipped) are effectively a 0-15 count of how many
	//       compares to pass though (which implies an extra clock to that sprite).
	//       The hardware also does with with XOR and is needed here to mimic
	//       the behavior that a mid-counter write of the right HMx value means
	//       compare passes forever once the counter is at 0.
	if t.hmoveCounter^h == kMASK_HMOVE_DONE {
		*a = false
	}
}

func (t *TIA) moveClock(a bool, c *uint8) {
	if a {
		if t.hblank {
			// Only do this during hblank. Any other time MOTCLK and this enable
			// create the same signal so no extra clocks end up generated.
			*c = (*c + 1) % kVisible
		}
	}
}

// TickDone is to be called after all chips have run a given Tick() cycle in order to do post
// processing that's normally controlled by a clock interlocking all the chips. i.e. setups for
// flip-flop loads that take effect on the start of the next cycle. i.e. this could have been
// implemented as PreTick in the same way. Including this in Tick() requires a specific
// ordering between chips in order to present a consistent view otherwise.
func (t *TIA) TickDone() {
	t.tickDone = true

	t.rdy = t.shadowRdy

	// Do latch work first before advancing things.

	// These could update at the same time as compares are happening.
	t.horizontalMotionPlayer = t.shadowHorizontalMotionPlayer
	t.horizontalMotionMissile = t.shadowHorizontalMotionMissile
	t.horizontalMotionBall = t.shadowHorizontalMotionBall

	// Run the H1 clock here since the latched (i.e. immediate) state of the
	// HMx registers is needed.
	if (t.hClock & kMASK_Hx_CLOCK) == kMASK_H1_CLOCK {
		t.checkHmove(t.horizontalMotionPlayer[0], &t.hmovePlayerActive[0])
		t.checkHmove(t.horizontalMotionPlayer[1], &t.hmovePlayerActive[1])
		t.checkHmove(t.horizontalMotionMissile[0], &t.hmoveMissileActive[0])
		t.checkHmove(t.horizontalMotionMissile[1], &t.hmoveMissileActive[1])
		t.checkHmove(t.horizontalMotionBall, &t.hmoveBallActive)

		// Now adjust clocks if needed (i.e. still active).
		// NOTE: we can adjust clocks directly since the only outside way to do
		//       this is as a reset which is handled in TickDone().
		t.moveClock(t.hmovePlayerActive[0], &t.playerClock[0])
		t.moveClock(t.hmovePlayerActive[1], &t.playerClock[1])
		t.moveClock(t.hmoveMissileActive[0], &t.missileClock[0])
		t.moveClock(t.hmoveMissileActive[1], &t.missileClock[1])
		t.moveClock(t.hmoveBallActive, &t.ballClock)
	}

	// Check for reset first since it still needs to advance also.
	t.resetClock(&t.ballReset, &t.ballClock)
	t.resetClock(&t.missileReset[0], &t.missileClock[0])
	t.resetClock(&t.missileReset[1], &t.missileClock[1])
	t.resetClock(&t.playerReset[0], &t.playerClock[0])
	t.resetClock(&t.playerReset[1], &t.playerClock[1])

	// Check missile locking now so we can reset missile clocks if needed.
	t.missileLockedPlayer = t.shadowMissileLockedPlayer
	if t.missileLockedPlayer[0] {
		// See comments in resetClock.
		t.missileClock[0] = (kCLOCK_RESET + 4) % kVisible
		// This being on always forces the missile enable off.
		t.missileEnabled[0] = false
	}
	if t.missileLockedPlayer[1] {
		// See comments in resetClock.
		t.missileClock[1] = (kCLOCK_RESET + 4) % kVisible
		// This being on always forces the missile enable off.
		t.missileEnabled[1] = false
	}

	// Advance the clocks (and wrap it) if during visible.
	if !t.hblank {
		t.playerClock[0] = (t.playerClock[0] + 1) % kVisible
		t.playerClock[1] = (t.playerClock[1] + 1) % kVisible
		t.missileClock[0] = (t.missileClock[0] + 1) % kVisible
		t.missileClock[1] = (t.missileClock[1] + 1) % kVisible
		t.ballClock = (t.ballClock + 1) % kVisible
	}

	// Also wrao the main clock as needed. All state triggering happens here.
	t.hClock = (t.hClock + 1) % kWidth

	t.hPos = (t.hPos + 1) % kWidth

	// Wrap on the end of line. Up to CPU code to count lines and trigger vPos reset.
	// vPos is strictly for debugging.
	if t.hPos == 0 && !t.rsync {
		t.vPos++
	}

	// Check rsync for pulling back to beginning of line.
	if t.rsync {
		t.hPos = 0
		t.rsync = false
	}

	// Frame reset means everything goes back to upper left.
	if t.frameReset {
		t.frameDone(t.picture)
		t.hPos = 0
		t.vPos = 0
		t.hClock = 0x00
		t.frameReset = false
	}

	t.vsync = t.shadowVsync
	t.vblank = t.shadowVblank

	// Note that all of the values here are post increment (i.e. what the next run
	// is using). Lines up with documented clocks easier.
	switch t.hClock {
	case 0:
		t.hblank = true
		t.lateHblank = false
		t.center = false
	case 68:
		if !t.lateHblank {
			t.hblank = false
		}
	case 76:
		// By here we always reset.
		t.hblank = false
	case 148:
		t.center = true
	}

	// Start of line always resets the rdy line. We do this on the end of the previous line
	// so that the next Tick() both this and the CPU would execute during the same clock.
	// Otherwise if we want until the actual Tick that starts with hPos == 0 the CPU will
	// always be off by one TIA clock. In the actual hardware this happens because the
	// control for it is a latch, not a flip-flop so as soon as "start hblank signal"
	// happens this line resets. That technically happens at the far right side of the screen
	// as the beam has to be off as it resets back to the left side.
	if t.hPos == 0 {
		t.rdy = false
	}

	if t.shadowHmove != kHmoveStateNotRunning {
		t.hmove = t.shadowHmove
		t.shadowHmove = kHmoveStateNotRunning
		t.lateHblank = true
	}

	origReflectPF := t.reflectPF
	origPlayfield := t.playfield

	t.missileWidth = t.shadowMissileWidth
	t.playerCntWidth = t.shadowPlayerCntWidth
	t.colors = t.shadowColors
	t.reflectPF = t.shadowReflectPF
	t.scoreMode = t.shadowScoreMode
	t.playfieldPriority = t.shadowPlayfieldPriority
	t.ballWidth = t.shadowBallWidth
	t.reflectPlayers = t.shadowReflectPlayers
	t.playfield = t.shadowPlayfield
	// No reason to recompute this unless it changes.
	if (t.playfield != origPlayfield) || (t.reflectPF != origReflectPF) {
		t.generatePF()
	}

	t.player0Graphic = t.shadowPlayer0Graphic
	t.player1Graphic = t.shadowPlayer1Graphic
	t.ballEnabled = t.shadowBallEnabled
	t.missileEnabled = t.shadowMissileEnabled
	t.verticalDelayPlayer = t.shadowVerticalDelayPlayer
	t.verticalDelayBall = t.shadowVerticalDelayBall
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
		&color.NRGBA{0xF9, 0xFF, 0x29, 0xFF},
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
