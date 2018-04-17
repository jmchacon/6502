// Package tia implements the TIA chip used in an atari 2600 for display/sound.
package tia

import "github.com/jmchacon/6502/io"

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
	kMASK_READ = uint8(0xC0) // Only D7/6 defined on the bus for reads.

	kMASK_VSYNC = uint8(0x02) // Trigger bit for VSYNC (others ignored).

	kMASK_VBL_VBLANK      = uint8(0x02)
	kMASK_VBL_I45_LATCHES = uint8(0x40)
	kMASK_VBL_I0I3_GROUND = uint8(0x80)

	kMASK_NUSIZ_MISSILE = uint8(0x30)
	kNUSIZ_SHIFT        = 4
	kMASK_NUSIZ_PLAYER  = uint8(0x07)
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

// TIA implements all modes needed for a TIA including sound.
type TIA struct {
	// NOTE: Collision bits are stored as they are expected to return to
	//       avoid lots of shifting and masking if stored in a uint16.
	//       But store as an array so they can be easily reset.
	collision      [8]uint8          // Collission bits (see constants below for various ones).
	inputPorts     [6]io.PortIn1     // If non-nil defines the input port for the given paddle/joystick.
	ioPortGnd      func()            // If non-nil is called when I0-3 are grounded via VBLANK.7.
	outputLatches  [2]bool           // The output latches (if used) for ports 4/5.
	rdy            bool              // If true then RDY out (which should go to RDY on cpu) is signaled high via Raised().
	vsync          bool              // If true in VSYNC mode.
	vblank         bool              // If true in VBLANK mode.
	latches        bool              // If true then I4/I5 in latch mode.
	groundInput    bool              // If true then I0-I3 are grounded and always return 0.
	horizontalCnt  int               // Current horizontal position (including hblank).
	missileWidth   [2]int            // Width of missles in pixels (1,2,4,8).
	playerCntWidth [2]playerCntWidth // Player 0,1 count and width (see enum).
}

type TiaDef struct {
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
}

// Init returns a full initialized TIA.
func Init(def *TiaDef) *TIA {
	t := &TIA{
		inputPorts: [6]io.PortIn1{def.Port0, def.Port1, def.Port2, def.Port3, def.Port4, def.Port5},
	}
	t.PowerOn()
	return t
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
// state when called. An implementation tying this to a receiver can tie this together.
func (t *TIA) Raised() bool {
	return t.rdy
}

// Read returns memory at the given address. The address is masked to 4 bits internally
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

// Write stores the valy at the given address. The address is masked to 6 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real writes are happening on clocked CPU Tick()'s.
func (t *TIA) Write(addr uint16, val uint8) {
	// Strip to 6 bits for internal regs
	addr &= 0x3F

	switch addr {
	case 0x00:
		// VSYNC
		t.vsync = false
		if (val & kMASK_VSYNC) != 0x00 {
			t.vsync = true
		}
	case 0x01:
		// VBLANK
		t.vblank = false
		if (val & kMASK_VBL_VBLANK) != 0x00 {
			t.vblank = true
		}
		l := false
		if (val & kMASK_VBL_I45_LATCHES) != 0x00 {
			l = true
		}
		// If we're resetting t.latches they go high
		if l && !t.latches {
			t.outputLatches[0] = true
			t.outputLatches[1] = true
		}
		t.latches = l
		t.groundInput = false
		if (val * kMASK_VBL_I0I3_GROUND) != 0x00 {
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
		t.horizontalCnt = 0
	case 0x04, 0x05:
		// NUSIZ0 and NUSIZ1
		idx := int(addr) - 0x04
		switch (val & kMASK_NUSIZ_MISSILE) >> kNUSIZ_SHIFT {
		case 0x00:
			t.missileWidth[idx] = 1
		case 0x01:
			t.missileWidth[idx] = 2
		case 0x02:
			t.missileWidth[idx] = 4
		case 0x03:
			t.missileWidth[idx] = 8
		default:
			panic("Impossible missle value from mask")
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
		default:
			panic("Impossible player value from mask")
		}
	case 0x2C:
		// CXCLR
		for i := range t.collision {
			t.collision[i] = 0x00
		}

	default:
		// These are undefined and go nowhere.
	}
}

// Tick does a single clock cycle on the chip which usually is running 3x the
// speed of a CPU. It's up to implementations to run these at whatever rates are
// needed and add delay for total cycle time needed.
// Every tick involves some form of graphics update/state change.
func (t *TIA) Tick() error {
	return nil
}
