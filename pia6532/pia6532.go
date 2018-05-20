// Package pia6532 implements the complete state of a 6532 PIA
// as described in http://www.ionpool.net/arcade/gottlieb/technical/datasheets/R6532_datasheet.pdf
// and http://www.devili.iki.fi/pub/Commodore/docs/datasheets/CSG/6532-8102.zip
package pia6532

import (
	"errors"
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

// out holds the data for an 8 bit I/O port.
type out struct {
	data uint8
}

// Output implements the interface for io.PortOut8
func (o *out) Output() uint8 {
	return o.data
}

const (
	kEDGE_UNIMPLEMENTED edgeType = iota // Start of valid edge detect enumerations.
	kEDGE_POSITIVE                      // Positive edge detection
	kEDGE_NEGATIVE                      // Negative edge detection
	kEDGE_MAX                           // End of edge enumerations.
)

const (
	kREAD_PORT_A       = uint16(0x0000)
	kREAD_PORT_A_DDR   = uint16(0x0001)
	kREAD_PORT_B       = uint16(0x0002)
	kREAD_PORT_B_DDR   = uint16(0x0003)
	kREAD_TIMER_NO_INT = uint16(0x0004)
	kREAD_INT          = uint16(0x0005)
	kREAD_TIMER_INT    = uint16(0x000C)

	kWRITE_PORT_A            = uint16(0x0000)
	kWRITE_PORT_A_DDR        = uint16(0x0001)
	kWRITE_PORT_B            = uint16(0x0002)
	kWRITE_PORT_B_DDR        = uint16(0x0003)
	kWRITE_NEG_NO_INT        = uint16(0x0004)
	kWRITE_POS_NO_INT        = uint16(0x0005)
	kWRITE_NEG_INT           = uint16(0x0006)
	kWRITE_POS_INT           = uint16(0x0007)
	kWRITE_TIMER_1_NO_INT    = uint16(0x0014)
	kWRITE_TIMER_8_NO_INT    = uint16(0x0015)
	kWRITE_TIMER_64_NO_INT   = uint16(0x0016)
	kWRITE_TIMER_1024_NO_INT = uint16(0x0017)
	kWRITE_TIMER_1_INT       = uint16(0x001C)
	kWRITE_TIMER_8_INT       = uint16(0x001D)
	kWRITE_TIMER_64_INT      = uint16(0x001E)
	kWRITE_TIMER_1024_INT    = uint16(0x001F)

	kMASK_INT  = uint8(0x80)
	kMASK_EDGE = uint8(0x40)
	kMASK_NONE = uint8(0x00)
)

// PIA6532 implements all modes needed for a 6532 including internal RAM
// plus the I/O and interrupt modes.
type PIA6532 struct {
	tickDone             bool       // True if TickDone() was called before the current Tick() call
	portAOutput          *out       // The output of port A.
	shadowPortAOutput    uint8      // Shadow value for portAOutput to load on TickDone().
	portBOutput          *out       // The output of port B.
	shadowPortBOutput    uint8      // Shadow value for portBOutput to load on TickDone().
	portAInput           io.PortIn8 // Interface for installing an IO Port input. Set by user if input is to be provided on port A.
	portBInput           io.PortIn8 // Interface for installing an IO Port input. Set by user if input is to be provided on port B.
	ram                  memory.Ram // Interface to implementation RAM.
	holdPortA            uint8      // The most recent read in value that will be used as a comparison for edge triggering on PA7.
	portADDR             uint8      // Port A DDR register.
	shadowPortADDR       uint8      // Shadow value for portADDR to load on TickDone().
	portBDDR             uint8      // Port B DDR register.
	shadowPortBDDR       uint8      // Shadow value for portBDDR to load on TickDone().
	timer                uint8      // Current timer value.
	wroteTimer           bool       // Whether timer values were reset on a recent write.
	shadowTimer          uint8      // Shadow value for timer to load on TickDone().
	timerMult            uint16     // Timer value adjustment multiplier.
	shadowTimerMult      uint16     // Shadow value for timerMult to load on TickDone().
	timerMultCount       uint16     // The current countdown for timerMult.
	shadowTimerMultCount uint16     // Shadow value for timerMultCount to load on TickDone().
	timerExpired         bool       // Whether current timer countdown has hit the end.
	interrupt            bool       // Whether timer interrupts are raised or not.
	wroteInterrupt       bool       // If interrupt and interruptOn were written this cycle.
	shadowInterrupt      bool       // Shadow value for interrupt to load on TickDone().
	interruptOn          uint8      // Current interrupt state. Bit 7 == timer, bit 6 == edge.
	shadowInterruptOn    uint8      // Shadow value which determines interruptOn  on TickDone().
	edgeInterrupt        bool       // Whether edge detection triggers an interrupt.
	shadowEdgeInterrupt  bool       // Shadow value for edgeInterrupt to load on TickDone().
	edgeStyle            edgeType   // Which type of edge detection to use.
	shadowEdgeStyle      edgeType   // Shadow value for edgeStyle to load on TickDone().
}

// Init returns a full initialized 6532. If the irq receiver passed in is
// non-nil it will be used to raise interrupts based on timer/PA7 state.
func Init(portA io.PortIn8, portB io.PortIn8) *PIA6532 {
	p := &PIA6532{
		portAOutput: &out{},
		portBOutput: &out{},
		portAInput:  portA,
		portBInput:  portB,
		ram:         &piaRam{},
		tickDone:    true,
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
	p.tickDone = true
	p.portAOutput.data = 0x00
	p.shadowPortAOutput = 0x00
	p.holdPortA = 0x00
	p.portADDR = 0x00
	p.shadowPortADDR = 0x00
	p.portBOutput.data = 0x00
	p.shadowPortBOutput = 0x00
	p.portBDDR = 0x00
	p.shadowPortBDDR = 0x00
	p.timer = 0x00
	p.wroteTimer = false
	p.shadowTimer = 0x00
	p.timerMult = 0x0001
	p.shadowTimerMult = 0x0001
	p.timerMultCount = 0x0001
	p.shadowTimerMultCount = 0x0001
	p.timerExpired = false
	p.interrupt = false
	p.shadowInterrupt = false
	p.interruptOn = 0x00
	p.shadowInterruptOn = 0x00
	p.edgeInterrupt = false
	p.shadowEdgeInterrupt = false
	p.edgeStyle = kEDGE_NEGATIVE
	p.shadowEdgeStyle = kEDGE_NEGATIVE
}

// PortA returns an io.PortOut8 for getting the current output pins of Port A.
func (p *PIA6532) PortA() io.PortOut8 {
	return p.portAOutput
}

// PortB returns an io.PortOut8 for getting the current output pins of Port B.
func (p *PIA6532) PortB() io.PortOut8 {
	return p.portBOutput
}

// Read returns memory at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real reads are happening on clocked CPU Tick()'s.
func (p *PIA6532) Read(addr uint16, ram bool) uint8 {
	if ram {
		// Assumption is memory interface impl correctly deals with any aliasing.
		return p.ram.Read(addr)
	}
	// Strip to 5 bits for internal regs.
	addr &= 0x1F
	var ret, readA, readB uint8

	// For port A (which has no pullups) input reads show the input pins as masked by DDR but then
	// AND's the other pins (so grounding a pin set to output 1 will result in a 0).
	if p.portAInput != nil {
		readA = (p.portAOutput.data | ^p.portADDR) & p.portAInput.Input()
	}
	// For port B OR in any set output pins (but only those). This works due to the internal
	// pullups not resulting in a classic open collector AND like port A gets.
	if p.portBInput != nil {
		readB = (p.portBOutput.data | ^p.portBDDR) & (p.portBInput.Input() | p.portBDDR)
	}

	// There's a lot of aliasing due to don't care bits.
	switch addr {
	case 0x00, 0x08, 0x10, 0x18:
		ret = readA
	case 0x01, 0x09, 0x11, 0x19:
		ret = p.portADDR
	case 0x02, 0x0A, 0x12, 0x1A:
		ret = readB
	case 0x03, 0x0B, 0x13, 0x1B:
		ret = p.portBDDR
	case 0x04, 0x06, 0x14, 0x16:
		ret = p.timer
		p.shadowInterrupt = false
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_INT)
		p.wroteInterrupt = true
	case 0x05, 0x07, 0x0D, 0x0F, 0x15, 0x17, 0x1D, 0x1F:
		if p.interrupt {
			ret |= 0x80
		}
		if p.edgeInterrupt {
			ret |= 0x40
		}
		p.shadowEdgeInterrupt = false
		p.shadowInterrupt = p.interrupt
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_EDGE)
		p.wroteInterrupt = true
	case 0x0C, 0x0E, 0x1C, 0x1E:
		ret = p.timer
		p.shadowInterrupt = true
		p.shadowInterruptOn = p.interruptOn
		p.wroteInterrupt = true
	}
	return ret
}

// Write stores the valy at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real writes are happening on clocked CPU Tick()'s.
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
		// Mask for output pins only as set by DDR
		// Any bits set as input are held to 1's on reads.
		p.shadowPortAOutput = (val & p.portADDR) | ^p.portADDR
	case 0x01, 0x09, 0x11, 0x19:
		p.shadowPortADDR = val
	case 0x02, 0x0A, 0x12, 0x1A:
		p.shadowPortBOutput = (val & p.portBDDR) | ^p.portBDDR
	case 0x03, 0x0B, 0x13, 0x1B:
		p.shadowPortBDDR = val
	case 0x04, 0x0C:
		p.shadowEdgeStyle = kEDGE_NEGATIVE
		p.shadowEdgeInterrupt = false
	case 0x05, 0x0D:
		p.shadowEdgeStyle = kEDGE_POSITIVE
		p.shadowEdgeInterrupt = false
	case 0x06, 0x0E:
		p.shadowEdgeStyle = kEDGE_NEGATIVE
		p.shadowEdgeInterrupt = true
	case 0x07, 0x0F:
		p.shadowEdgeStyle = kEDGE_POSITIVE
		p.shadowEdgeInterrupt = true
	case 0x14, 0x15, 0x16, 0x17, 0x1C, 0x1D, 0x1E, 0x1F:
		// All of these are timer setups with variations based on specific bits.
		p.wroteTimer = true
		p.wroteInterrupt = true
		p.shadowTimer = val
		p.shadowInterrupt = false
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_INT)
		if (addr & 0x08) != 0x00 {
			p.shadowInterrupt = true
		}
		p.shadowTimerMult = 0x0001
		p.shadowTimerMultCount = 0x0001
		switch addr & 0x07 {
		case 0x05:
			p.shadowTimerMult = 0x0008
			p.shadowTimerMultCount = 0x0008
		case 0x06:
			p.shadowTimerMult = 0x0040
			p.shadowTimerMultCount = 0x0040
		case 0x07:
			p.shadowTimerMult = 0x0400
			p.shadowTimerMultCount = 0x0400
		}
	}
}

// Raised implements the irq.Sender interface for determining interrupt state when called.
// An implementation tying this to a receiver can tie this together.
func (p *PIA6532) Raised() bool {
	return (p.interruptOn & (kMASK_INT | kMASK_EDGE)) != 0x00
}

const (
	kPA7 = uint8(0x80)
)

func (p *PIA6532) edgeDetect(newA uint8, oldA uint8) error {
	// If we're detecting edge changes on PA7 possibly setup interrupts for that.
	switch p.edgeStyle {
	case kEDGE_POSITIVE:
		if p.edgeInterrupt && (newA&kPA7) == 0x00 && (oldA&kPA7) != 0x00 {
			p.interruptOn |= kMASK_EDGE
		}
	case kEDGE_NEGATIVE:
		if p.edgeInterrupt && (newA&kPA7) != 0x00 && (oldA&kPA7) == 0x00 {
			p.interruptOn |= kMASK_EDGE
		}
	default:
		return fmt.Errorf("impossible edge state: %d", p.edgeStyle)
	}
	return nil
}

// Tick does a single clock cycle on the chip which generally involves decrementing timers
// and updates port A and port B values.
func (p *PIA6532) Tick() error {
	if !p.tickDone {
		return errors.New("called Tick() without calling TickDone() at end of last cycle")
	}
	p.tickDone = false

	var newA uint8
	// We always trigger on an edge transition here.
	if p.portAInput != nil {
		// Mask for input pins.
		newA = p.portAInput.Input() & (^p.portADDR)
	}

	if err := p.edgeDetect(newA, p.holdPortA); err != nil {
		return err
	}

	// Move new values into hold for next timer eval.
	p.holdPortA = newA

	return nil
}

// TickDone is to be called after all chips have run a given Tick() cycle in order to do post
// processing that's normally controlled by a clock interlocking all the chips. i.e. setups for
// latch loads that take effect on the start of the next cycle. i.e. this could have been
// implemented as PreTick in the same way. Including this in Tick() requires a specific
// ordering between chips in order to present a consistent view otherwise.
func (p *PIA6532) TickDone() {
	// Deal with port A edge detection.
	old := p.portAOutput.data
	p.portAOutput.data = p.shadowPortAOutput
	// This can only change the edge bit so the reset below doesn't change that.
	p.edgeDetect(old, p.shadowPortAOutput)

	// Port B data
	p.portBOutput.data = p.shadowPortBOutput

	// Port A/B DDR
	p.portADDR = p.shadowPortADDR
	p.portBDDR = p.shadowPortBDDR

	// Interrupt styles
	p.edgeStyle = p.shadowEdgeStyle
	p.edgeInterrupt = p.shadowEdgeInterrupt

	// Timer runs in TickDone() so that reads always see a consistent timer value during a given
	// cycle.

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
	} else {
		// If we expired the timer free runs (and wraps around) until the timer value gets reset.
		p.timer--
		if p.interrupt {
			p.interruptOn |= kMASK_INT
		}
	}

	// Now deal with interrupt state. This means a timer ticking 00->FF on the same cycle it gets a reset never emits
	// an interrupt.
	if p.wroteInterrupt {
		p.interrupt = p.shadowInterrupt
		p.interruptOn = p.shadowInterruptOn
		p.wroteInterrupt = false
	}

	// Deal with timer resets.
	if p.wroteTimer {
		p.timer = p.shadowTimer
		p.timerMult = p.shadowTimerMult
		p.timerMultCount = p.shadowTimerMultCount
		p.wroteTimer = false
	}

	p.tickDone = true
}
