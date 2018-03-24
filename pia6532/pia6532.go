// Package pia6532 implements the complete state of a 6532 PIA
// as described in http://www.ionpool.net/arcade/gottlieb/technical/datasheets/R6532_datasheet.pdf
// and http://www.devili.iki.fi/pub/Commodore/docs/datasheets/CSG/6532-8102.zip
package pia6532

import (
	"fmt"

	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/memory"
)

// piaRam is the memory for the 6532 implemented according to the memory interface.
// Technically not needed but easier to debug.
type piaRam struct {
	// Only has 128 bytes of RAM
	addr [128]uint8
}

// Read implements the interface for memory. Address is clipped to 7 bits.
func (r *piaRam) Read(addr uint16) uint8 {
	return r.addr[addr&0x7F]
}

// Write implements the interface for memory. Address is clipped to 7 bits.
func (r *piaRam) Write(addr uint16, val uint8) {
	r.addr[addr&0x7F] = val
}

// Reset implements the interface for memory.
func (r *piaRam) Reset() {
}

// PowerOn implements the interface for memory and zero's out the RAM.
func (r *piaRam) PowerOn() {
	for i := range r.addr {
		r.addr[i] = 0x00
	}
	r.Reset()
}

type edgeType int

const (
	kEDGE_UNIMPLMENTED edgeType = iota // Start of valid edge detect enumerations.
	kEDGE_POSITIVE                     // Positive edge detection
	kEDGE_NEGATIVE                     // Negative edge detection
	kEDGE_MAX                          // End of edge enumerations.
)

// PIA6532 implements all modes needed for a 6532 including internal RAM
// plus the I/O and interrupt modes.
type PIA6532 struct {
	PortAInput     io.Port8   // Interface for installing an IO Port input to be updated on Tick().
	PortBInput     io.Port8   // Interface for installing an IO Port input to be updated on Tick().
	ram            memory.Ram // Interface to implementation RAM.
	portA          uint8      // Current held value in portA masked by DDR.
	holdPortA      uint8      // The most recent read in value that will transition to portA on next tick.
	portADDR       uint8      // Port A DDR register.
	portB          uint8      // Current held value in portB masked by DDR.
	holdPortB      uint8      // The most recent read in value that will transition to portB on next tick.
	portBDDR       uint8      // Port B DDR register.
	timer          uint8      // Current timer value.
	timerMult      uint16     // Timer value adjustment multiplier.
	timerMultCount uint16     // The current countdown for timerMult.
	timerExpired   bool       // Whether current timer countdown has hit the end.
	interrupt      bool       // Whether interrupts are raised or not.
	interruptOn    bool       // Current interrupt state.
	edgeInterrupt  bool       // Whether edge detection triggers an interrupt.
	edgeStyle      edgeType   // Which type of edge detection to use.
}

// Init returns a full initialized 6532. If the irq receiver passed in is
// non-nil it will be used to raise interrupts based on timer/PA7 state.
func Init() *PIA6532 {
	p := &PIA6532{
		ram: &piaRam{},
	}
	p.ram.PowerOn()
	p.PowerOn()
	return p
}

// PowerOn performs a full power-on/reset for the 6532.
func (p *PIA6532) PowerOn() {
	p.Reset()
}

// Reset does a soft reset on the 6532 based on holding RES low on the chip.
// This takes one cycle to complete so not integrated with Tick.
func (p *PIA6532) Reset() {
	p.portA = 0x00
	p.holdPortA = 0x00
	p.portADDR = 0x00
	p.portB = 0x00
	p.holdPortB = 0x00
	p.portBDDR = 0x00
	p.timer = 0x00
	p.timerMult = 0x0001
	p.timerMultCount = 0x0001
	p.timerExpired = false
	p.interrupt = false
	p.interruptOn = false
	p.edgeInterrupt = false
	p.edgeStyle = kEDGE_NEGATIVE
}

// Read returns memory at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this.
func (p *PIA6532) Read(addr uint16, ram bool) uint8 {
	if ram {
		// Assumption is memory interface impl correctly deals with any aliasing.
		return p.ram.Read(addr)
	}
	// Strip to 5 bits for internal regs.
	addr &= 0x1F
	var ret uint8

	// There's a lot of aliasing due to don't care bits.
	switch addr {
	case 0x00, 0x08, 0x10, 0x18:
		ret = p.PortA()
	case 0x01, 0x09, 0x11, 0x19:
		ret = p.portADDR
	case 0x02, 0x0A, 0x12, 0x1A:
		ret = p.PortB()
	case 0x03, 0x0B, 0x13, 0x1B:
		ret = p.portBDDR
	case 0x04, 0x06, 0x14, 0x16:
		ret = p.timer
		p.interruptOn = false
		p.interrupt = false
	case 0x05, 0x07, 0x0D, 0x0F, 0x15, 0x17, 0x1D, 0x1F:
		if p.interrupt {
			ret |= 0x80
		}
		if p.edgeInterrupt {
			ret |= 0x40
		}
		p.edgeInterrupt = false
	case 0x0C, 0x0E, 0x1C, 0x1E:
		ret = p.timer
		p.interrupt = true
	}
	return ret
}

// Write stores the valy at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this.
func (p *PIA6532) Write(addr uint16, ram bool, val uint8) {
	if ram {
		// Assumption is memory interface impl correctly deals with any aliasing.
		p.ram.Write(addr, val)
		return
	}
	// Strip to 5 bits for internal regs
	addr &= 0x1F

	// There's a lot of aliasing due to don't care bits.
	switch addr {
	case 0x00, 0x08, 0x10, 0x18:
		p.portA = val
	case 0x01, 0x09, 0x11, 0x19:
		p.portADDR = val
	case 0x02, 0x0A, 0x12, 0x1A:
		p.portB = val
	case 0x03, 0x0B, 0x13, 0x1B:
		p.portBDDR = val
	case 0x04, 0x0C:
		p.edgeStyle = kEDGE_NEGATIVE
		p.edgeInterrupt = false
	case 0x05, 0x0D:
		p.edgeStyle = kEDGE_POSITIVE
		p.edgeInterrupt = false
	case 0x06, 0x0E:
		p.edgeStyle = kEDGE_NEGATIVE
		p.edgeInterrupt = true
	case 0x07, 0x0F:
		p.edgeStyle = kEDGE_POSITIVE
		p.edgeInterrupt = true
	case 0x14, 0x15, 0x16, 0x17, 0x1C, 0x1D, 0x1E, 0x1F:
		// All of these are timer setups with variations based on specific bits.
		p.timer = val
		p.interrupt = false
		if (addr & 0x08) != 0x00 {
			p.interrupt = true
		}
		p.timerMult = 0x0001
		p.timerMultCount = 0x0001
		switch addr & 0x07 {
		case 0x05:
			p.timerMult = 0x0008
			p.timerMultCount = 0x0008
		case 0x06:
			p.timerMult = 0x0040
			p.timerMultCount = 0x0040
		case 0x07:
			p.timerMult = 0x0400
			p.timerMultCount = 0x0400
		}
	}
}

// Raised implements the irq.Sender interface for determining interrupt state when called.
// An implementation tying this to a receiver can tie this together.
func (p *PIA6532) Raised() bool {
	return p.interrupt
}

// PortA returns the current value in port A to simulate the actual output pins of port A
// vs. the internal read which is handled in Read above.
// NOTE: This is by nature delayed one clock from data changing via the PIA calling
//       a registered Port8.Input() call due to mimicing actual hardware.
func (p *PIA6532) PortA() uint8 {
	// Mask for output pins only as set by DDR
	a := p.portA & p.portADDR
	// Any bits set as input are held to 1's on reads.
	a |= ^p.portADDR
	return a
}

// PortA returns the current value in port B to simulate the actual output pins of port B
// vs. the internal read which is handled in Read above.
// NOTE: This is by nature delayed one clock from data changing via the PIA calling
//       a registered Port8.Input() call due to mimicing actual hardware.
func (p *PIA6532) PortB() uint8 {
	// Mask for output pins only as set by DDR
	b := p.portB & p.portBDDR
	// Any bits set as input are held to 1's on reads.
	b |= ^p.portBDDR
	return b
}

const (
	kPA7 = uint8(0x80)
)

// Tick does a single clock cycle on the chip which generally involves decrementing timers
// and updates port A and port B values.
func (p *PIA6532) Tick() {
	// If we're detecting edge changes on PA7 possibly setup interrupts for that.
	switch p.edgeStyle {
	case kEDGE_POSITIVE:
		if p.interrupt && (p.portA&kPA7) == 0x00 && (p.holdPortA&kPA7) != 0x00 {
			p.interruptOn = true
		}
	case kEDGE_NEGATIVE:
		if p.interrupt && (p.portA&kPA7) != 0x00 && (p.holdPortA&kPA7) == 0x00 {
			p.interruptOn = true
		}
	default:
		panic(fmt.Sprintf("Impossible edge state: %d", p.edgeStyle))
	}

	// Move held values up to visible values and possibly latch in new ones.
	p.portA = p.holdPortA
	p.portB = p.holdPortB
	// Only set the bits marked as input (which are 0 in DDR so necessary invert below).
	if p.PortAInput != nil {
		p.holdPortA = p.PortAInput.Input() & (^p.portADDR)
	}
	if p.PortBInput != nil {
		p.holdPortB = p.PortBInput.Input() & (^p.portBDDR)
	}

	// If we haven't expired do normal countdown based around the multiplier.
	if !p.timerExpired {
		p.timerMultCount--
		if p.timerMultCount == 0x0000 {
			p.timerMultCount = p.timerMult
			p.timer--
		}
		if p.timer == 0x00 {
			p.timerExpired = true
		}
		// Even if we just expired it takes one more tick before we free run and possibly set interrupts.
		return
	}
	// If we expired the timer free runs (and wraps around) until the timer value gets reset.
	p.timer--
	if p.interrupt {
		p.interruptOn = true
	}
}
