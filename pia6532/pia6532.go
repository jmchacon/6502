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
	PortAInput     io.Port8   // Interface for installing an IO Port input.
	PortBInput     io.Port8   // Interface for installing an IO Port input.
	ram            memory.Ram // Interface to implementation RAM.
	portA          uint8      // Current held value in portA masked by DDR.
	holdPortA      uint8      // The most recent read in value that will transition to portA on next tick.
	portADDR       uint8      // Port A DDR register.
	portB          uint8      // Current held value in portB masked by DDR.
	holdPortB      uint8      // The most recent read in value that will transition to portB on next tick.
	portBDDR       uint8      // Port B DDR register.
	timer          uint8      // Current timer value.
	timerMult      uint8      // Timer value adjustment multiplier.
	timerMultCount uint8      // The current countdown for timerMult.
	timerExpired   bool       // Whether current timer countdown has hit the end.
	interrupt      bool       // Whether interrupts are raised or not.
	interruptOn    bool       // Current interrupt state
	edge           bool       // Edge detection for PA7
	edgeStyle      edgeType   // Which type of edge detection to use
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
	p.timerMult = 0x01
	p.timerMultCount = 0x01
	p.timerExpired = false
	p.interrupt = false
	p.interruptOn = false
	p.edge = false
	p.edgeStyle = kEDGE_NEGATIVE
}

// Read returns memory at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this.
func (p *PIA6532) Read(addr uint16, ram bool) uint8 {
	if ram {
		return p.ram.Read(addr)
	}
	return 0x00
}

// Write stores the valy at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this.
func (p *PIA6532) Write(addr uint16, ram bool, val uint8) {
	if ram {
		p.ram.Write(addr, val)
		return
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
	return p.portA & p.portADDR
}

// PortA returns the current value in port B to simulate the actual output pins of port B
// vs. the internal read which is handled in Read above.
// NOTE: This is by nature delayed one clock from data changing via the PIA calling
//       a registered Port8.Input() call due to mimicing actual hardware.
func (p *PIA6532) PortB() uint8 {
	// Mask for output pins only as set by DDR
	return p.portB & p.portBDDR
}

const (
	kPA7 = uint8(0x80)
)

// Tick does a single clock cycle on the chip which generally involves decrementing timers
// and updates port A and port B values.
func (p *PIA6532) Tick() {
	// If we're detecting edge changes on PA7 possible setup interrupts for that.
	if p.edge {
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
	}

	// Move held values up to visible values and possibly latch in new ones.
	// We don't mask for DDR here since output more easily masks and is handled above.
	p.portA = p.holdPortA
	p.portB = p.holdPortB
	if p.PortAInput != nil {
		p.holdPortA = p.PortAInput.Input()
	}
	if p.PortBInput != nil {
		p.holdPortB = p.PortBInput.Input()
	}

	// If we haven't expired do normal countdown based around the multiplier.
	if !p.timerExpired {
		p.timerMultCount--
		if p.timerMultCount == 0 {
			p.timerMultCount = p.timerMult
			p.timer--
		}
		if p.timer == 0 {
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
