// Package pia6532 implements the complete state of a 6532 PIA
// as described in http://www.ionpool.net/arcade/gottlieb/technical/datasheets/R6532_datasheet.pdf
// and http://www.devili.iki.fi/pub/Commodore/docs/datasheets/CSG/6532-8102.zip
package pia6532

import (
	"errors"
	"fmt"
	"math/rand"

	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/memory"
)

var (
	_ = memory.Bank(&Chip{})
	_ = memory.Bank(&ioRam{})
)

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

	kMASK_RAM        = uint16(0x7F)
	kMASK_RW         = uint16(0x1F)
	kMASK_INT_BIT    = uint16(0x08)
	kMASK_TIMER_MULT = uint16(0x07)

	kMASK_TIMER_MULT1    = uint16(0x04)
	kMASK_TIMER_MULT8    = uint16(0x05)
	kMASK_TIMER_MULT64   = uint16(0x06)
	kMASK_TIMER_MULT1024 = uint16(0x07)

	kTIMER_MULT1    = uint16(0x0001)
	kTIMER_MULT8    = uint16(0x0008)
	kTIMER_MULT64   = uint16(0x0040)
	kTIMER_MULT1024 = uint16(0x0400)

	kPA7 = uint8(0x80)
)

// ioRam is used as an abstraction for getting at the I/O portion of the PIA
// through a memory.Bank interface.
type ioRam struct {
	p          *Chip
	databusVal uint8 // The most recent val seen cross the databus (read or write).
}

// Chip implements all modes needed for a 6532 including internal RAM
// plus the I/O and interrupt modes.
type Chip struct {
	clocks               int  // Total number of clock cycles since start.
	debug                bool // If true Debug() emits output.
	tickDone             bool // True if TickDone() was called before the current Tick() call
	io                   *ioRam
	portAOutput          *out        // The output of port A.
	shadowPortAOutput    uint8       // Shadow value for portAOutput to load on TickDone().
	portBOutput          *out        // The output of port B.
	shadowPortBOutput    uint8       // Shadow value for portBOutput to load on TickDone().
	portAInput           io.PortIn8  // Interface for installing an IO Port input. Set by user if input is to be provided on port A.
	portBInput           io.PortIn8  // Interface for installing an IO Port input. Set by user if input is to be provided on port B.
	ram                  memory.Bank // Interface to implementation RAM.
	holdPortA            uint8       // The most recent read in value that will be used as a comparison for edge triggering on PA7.
	portADDR             uint8       // Port A DDR register.
	shadowPortADDR       uint8       // Shadow value for portADDR to load on TickDone().
	portBDDR             uint8       // Port B DDR register.
	shadowPortBDDR       uint8       // Shadow value for portBDDR to load on TickDone().
	timer                uint8       // Current timer value.
	wroteTimer           bool        // Whether timer values were reset on a recent write.
	shadowTimer          uint8       // Shadow value for timer to load on TickDone().
	timerMult            uint16      // Timer value adjustment multiplier.
	shadowTimerMult      uint16      // Shadow value for timerMult to load on TickDone().
	timerMultCount       uint16      // The current countdown for timerMult.
	shadowTimerMultCount uint16      // Shadow value for timerMultCount to load on TickDone().
	timerExpired         bool        // Whether current timer countdown has hit the end.
	interrupt            bool        // Whether timer interrupts are raised or not.
	wroteInterrupt       bool        // If interrupt and interruptOn were written this cycle.
	shadowInterrupt      bool        // Shadow value for interrupt to load on TickDone().
	interruptOn          uint8       // Current interrupt state. Bit 7 == timer, bit 6 == edge.
	shadowInterruptOn    uint8       // Shadow value which determines interruptOn  on TickDone().
	edgeInterrupt        bool        // Whether edge detection triggers an interrupt.
	shadowEdgeInterrupt  bool        // Shadow value for edgeInterrupt to load on TickDone().
	edgeStyle            edgeType    // Which type of edge detection to use.
	shadowEdgeStyle      edgeType    // Shadow value for edgeStyle to load on TickDone().
	parent               memory.Bank // If non-nil contains a pointer to a containing memory.Bank
	databusVal           uint8       // The most recent val seen cross the databus (read or write).
}

type ChipDef struct {
	// PortA is the I/O port for port A.
	PortA io.PortIn8

	// PortB is the I/O port for port B.
	PortB io.PortIn8

	// Debug if true wll emit output from Debug() calls
	Debug bool

	// Parent if non-nil defines a containing memory.Bank this chip is contained within.
	Parent memory.Bank
}

// Init returns a full initialized 6532. If the irq receiver passed in is
// non-nil it will be used to raise interrupts based on timer/PA7 state.
func Init(d *ChipDef) (*Chip, error) {
	p := &Chip{
		portAOutput: &out{},
		portBOutput: &out{},
		portAInput:  d.PortA,
		portBInput:  d.PortB,
		tickDone:    true,
		debug:       d.Debug,
		parent:      d.Parent,
	}
	var err error
	if p.ram, err = memory.New8BitRAMBank(0x80, p); err != nil {
		return nil, fmt.Errorf("can't initialize RAM: %v", err)
	}
	p.io = &ioRam{p, 0}
	p.PowerOn()
	return p, nil
}

// PowerOn implements the memory interface for ram.
// It performs a full power-on/reset for the 6532.
func (p *Chip) PowerOn() {
	// Allowed to initialize the RAM since we own it directly.
	p.ram.PowerOn()
	p.Reset()
}

// Reset implements the memory interface for ram.
// It does a soft reset on the 6532 based on holding RES low on the chip.
// This takes one cycle to complete so not integrated with Tick.
func (p *Chip) Reset() {
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
	p.timer = uint8(rand.Intn(256))
	p.wroteTimer = false
	p.shadowTimer = p.timer
	// Evidently the real hardware starts up in this mode
	// which some implementation depend on to loop watching for
	// a zero crossing without bothering to program the chip first.
	p.timerMult = kTIMER_MULT1024
	p.shadowTimerMult = kTIMER_MULT1024
	p.timerMultCount = kTIMER_MULT1024 - 1
	p.shadowTimerMultCount = kTIMER_MULT1024 - 1
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
func (p *Chip) PortA() io.PortOut8 {
	return p.portAOutput
}

// PortB returns an io.PortOut8 for getting the current output pins of Port B.
func (p *Chip) PortB() io.PortOut8 {
	return p.portBOutput
}

// IO returns a memory.Bank which interfaces to the I/O portion of the PIA.
func (p *Chip) IO() memory.Bank {
	return p.io
}

// Read implements the interface for memory.Bank and gives access to the RAM
// portion of the PIA. Use IO() to get an inteface to the I/O section.
func (p *Chip) Read(addr uint16) uint8 {
	val := p.read(addr, true)
	p.databusVal = val
	return val
}

// Write implements the interface for memory.Bank and gives access to the RAM
// portion of the PIA. Use IO() to get an inteface to the I/O section.
func (p *Chip) Write(addr uint16, val uint8) {
	p.databusVal = val
	p.write(addr, true, val)
}

// Parent implements the interface for returning a possible parent memory.Bank.
func (p *Chip) Parent() memory.Bank {
	return p.parent
}

// DatabusVal returns the most recent seen databus item.
func (p *Chip) DatabusVal() uint8 {
	return p.databusVal
}

// Read implements the interface for memory.Bank and gives access to the I/O
// portion of the PIA.
func (i *ioRam) Read(addr uint16) uint8 {
	val := i.p.read(addr, false)
	i.databusVal = val
	return val
}

// Write implements the interface for memory.Bank and gives access to the I/O
// portion of the PIA.
func (i *ioRam) Write(addr uint16, val uint8) {
	i.databusVal = val
	i.p.write(addr, false, val)
}

func (i *ioRam) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank.
func (i *ioRam) Parent() memory.Bank {
	return i.p
}

// DatabusVal returns the most recent seen databus item.
func (i *ioRam) DatabusVal() uint8 {
	return i.databusVal
}

// read returns memory at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real reads are happening on clocked CPU Tick()'s.
func (p *Chip) read(addr uint16, ram bool) uint8 {
	if ram {
		// Assumption is memory interface impl correctly deals with any aliasing.
		return p.ram.Read(addr)
	}
	// Strip to 5 bits for internal regs.
	addr &= kMASK_RW
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
	case kREAD_PORT_A, 0x08, 0x10, 0x18:
		ret = readA
	case kREAD_PORT_A_DDR, 0x09, 0x11, 0x19:
		ret = p.portADDR
	case kREAD_PORT_B, 0x0A, 0x12, 0x1A:
		ret = readB
	case kREAD_PORT_B_DDR, 0x0B, 0x13, 0x1B:
		ret = p.portBDDR
	case kREAD_TIMER_NO_INT, 0x06, 0x14, 0x16:
		ret = p.timer
		p.shadowInterrupt = false
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_INT)
		p.wroteInterrupt = true
	case kREAD_INT, 0x07, 0x0D, 0x0F, 0x15, 0x17, 0x1D, 0x1F:
		if p.interrupt {
			ret |= kMASK_INT
		}
		if p.edgeInterrupt {
			ret |= kMASK_EDGE
		}
		p.shadowEdgeInterrupt = false
		p.shadowInterrupt = p.interrupt
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_EDGE)
		p.wroteInterrupt = true
	case kREAD_TIMER_INT, 0x0E, 0x1C, 0x1E:
		ret = p.timer
		p.shadowInterrupt = true
		p.shadowInterruptOn = p.interruptOn
		p.wroteInterrupt = true
	}
	return ret
}

// write stores the value at the given address which is either the RAM (if ram is true) or
// internal registers. For RAM the address is masked to 7 bits and internal addresses
// are masked to 5 bits.
// NOTE: This isn't tied to the clock so it's possible to read/write more than one
//       item per cycle. Integration is expected to coordinate clocks as needed to control this
//       since it's assumed real writes are happening on clocked CPU Tick()'s.
func (p *Chip) write(addr uint16, ram bool, val uint8) {
	if ram {
		// Assumption is memory interface impl correctly deals with any aliasing.
		p.ram.Write(addr, val)
		return
	}
	// Strip to 5 bits for internal regs
	addr &= kMASK_RW

	// There's a lot of aliasing due to don't care bits.
	switch addr {
	case kWRITE_PORT_A, 0x08, 0x10, 0x18:
		// Mask for output pins only as set by DDR
		// Any bits set as input are held to 1's on reads.
		p.shadowPortAOutput = (val & p.portADDR) | ^p.portADDR
	case kWRITE_PORT_A_DDR, 0x09, 0x11, 0x19:
		p.shadowPortADDR = val
	case kWRITE_PORT_B, 0x0A, 0x12, 0x1A:
		p.shadowPortBOutput = (val & p.portBDDR) | ^p.portBDDR
	case kWRITE_PORT_B_DDR, 0x0B, 0x13, 0x1B:
		p.shadowPortBDDR = val
	case kWRITE_NEG_NO_INT, 0x0C:
		p.shadowEdgeStyle = kEDGE_NEGATIVE
		p.shadowEdgeInterrupt = false
	case kWRITE_POS_NO_INT, 0x0D:
		p.shadowEdgeStyle = kEDGE_POSITIVE
		p.shadowEdgeInterrupt = false
	case kWRITE_NEG_INT, 0x0E:
		p.shadowEdgeStyle = kEDGE_NEGATIVE
		p.shadowEdgeInterrupt = true
	case kWRITE_POS_INT, 0x0F:
		p.shadowEdgeStyle = kEDGE_POSITIVE
		p.shadowEdgeInterrupt = true
	case kWRITE_TIMER_1_NO_INT, kWRITE_TIMER_8_NO_INT, kWRITE_TIMER_64_NO_INT, kWRITE_TIMER_1024_NO_INT, kWRITE_TIMER_1_INT, kWRITE_TIMER_8_INT, kWRITE_TIMER_64_INT, kWRITE_TIMER_1024_INT:
		// All of these are timer setups with variations based on specific bits.
		p.wroteTimer = true
		p.wroteInterrupt = true
		p.shadowTimer = val
		p.shadowInterrupt = false
		p.shadowInterruptOn = (p.interruptOn &^ kMASK_INT)
		if (addr & kMASK_INT_BIT) == kMASK_INT_BIT {
			p.shadowInterrupt = true
		}
		switch addr & kMASK_TIMER_MULT {
		case kMASK_TIMER_MULT1:
			p.shadowTimerMult = kTIMER_MULT1
			p.shadowTimerMultCount = kTIMER_MULT1
		case kMASK_TIMER_MULT8:
			p.shadowTimerMult = kTIMER_MULT8
			p.shadowTimerMultCount = kTIMER_MULT8
		case kMASK_TIMER_MULT64:
			p.shadowTimerMult = kTIMER_MULT64
			p.shadowTimerMultCount = kTIMER_MULT64
		case kMASK_TIMER_MULT1024:
			p.shadowTimerMult = kTIMER_MULT1024
			p.shadowTimerMultCount = kTIMER_MULT1024
		}
	}
}

// Raised implements the irq.Sender interface for determining interrupt state when called.
// An implementation tying this to a receiver can tie this together.
func (p *Chip) Raised() bool {
	return (p.interruptOn & (kMASK_INT | kMASK_EDGE)) != 0x00
}

func (p *Chip) edgeDetect(newA uint8, oldA uint8) error {
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
func (p *Chip) Tick() error {
	p.clocks++
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
func (p *Chip) TickDone() {
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
		// When the multiplier resets we decrement the timer.
		// This allows it to run at p.timer == 0x00 until the
		// multiplier is done.
		if p.timerMultCount == p.timerMult {
			p.timer--
		}
		p.timerMultCount--
		if p.timerMultCount == 0x0000 {
			p.timerMultCount = p.timerMult
		}
		// Even if we just expired it takes one more tick before we free run and possibly set interrupts.
		if p.timer == 0xFF {
			p.timerExpired = true
			if p.interrupt {
				p.interruptOn |= kMASK_INT
			}
		}
	} else {
		// If we expired the timer free runs (and wraps around) until the timer value gets reset.
		p.timer--
		if p.interrupt {
			p.interruptOn |= kMASK_INT
		}
	}

	// Deal with timer resets.
	if p.wroteTimer {
		p.timer = p.shadowTimer
		p.timerMult = p.shadowTimerMult
		p.timerMultCount = p.shadowTimerMultCount
		p.wroteTimer = false
		p.timerExpired = false
	}

	// Now deal with interrupt state. This means a timer ticking 00->FF on the same cycle it gets a reset never emits
	// an interrupt.
	if p.wroteInterrupt {
		p.interrupt = p.shadowInterrupt
		p.interruptOn = p.shadowInterruptOn
		p.wroteInterrupt = false
	}

	p.tickDone = true
}

func (p *Chip) Debug() string {
	if p.debug {
		return fmt.Sprintf("%.6d timer: %.2X mult: %.4X multCount: %.4X expired: %t\n", p.clocks, p.timer, p.timerMult, p.timerMultCount, p.timerExpired)
	}
	return ""
}
