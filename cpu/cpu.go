// Package cpu defines the 6502 architecture and provides
// the methods needed to run the CPU and interface with it
// for emulation.
package cpu

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/jmchacon/6502/irq"
	"github.com/jmchacon/6502/memory"
)

// CPUType is an enumeration of the valid CPU types.
type CPUType int

const (
	CPU_UNIMPLMENTED CPUType = iota // Start of valid cpu enumerations.
	CPU_NMOS                        // Basic NMOS 6502 including undocumented opcodes.
	CPU_NMOS_RICOH                  // Ricoh version used in NES which is identical to NMOS except BCD mode is unimplmented.
	CPU_NMOS_6510                   // NMOS 6510 variant which includes I/O ports mapped at addresses 0x0 and 0x1
	CPU_CMOS                        // 65C02 CMOS version where undocumented opcodes are all explicit NOP.
	CPU_MAX                         // End of CPU enumerations.
)

// irqType is an enumeration of the valid IRQ types.
type irqType int

const (
	kIRQ_UNIMPLMENTED irqType = iota // Start of valid irq enumerations.
	kIRQ_NONE                        // No interrupt raised.
	kIRQ_IRQ                         // Standard IRQ signal.
	kIRQ_NMI                         // NMI signal.
	kIRQ_MAX                         // End of irq enumerations.
)

const (
	NMI_VECTOR   = uint16(0xFFFA)
	RESET_VECTOR = uint16(0xFFFC)
	IRQ_VECTOR   = uint16(0xFFFE)

	P_NEGATIVE  = uint8(0x80)
	P_OVERFLOW  = uint8(0x40)
	P_S1        = uint8(0x20) // Always 1
	P_B         = uint8(0x10) // Only set during BRK. Cleared on all other interrupts.
	P_DECIMAL   = uint8(0x8)
	P_INTERRUPT = uint8(0x4)
	P_ZERO      = uint8(0x2)
	P_CARRY     = uint8(0x1)

	NEGATIVE_ONE = uint8(0xFF)
)

type Chip struct {
	A                 uint8         // Accumulator register
	X                 uint8         // X register
	Y                 uint8         // Y register
	S                 uint8         // Stack pointer
	P                 uint8         // Status register
	PC                uint16        // Program counter
	tickDone          bool          // True if TickDone() was called before the current Tick() call
	irq               irq.Sender    // Interface for installing an IRQ sender.
	nmi               irq.Sender    // Interface for installing an NMI sender.
	rdy               irq.Sender    // Interface for installing a RDY handler. Technically not an interrupt source but signals the same (edge).
	cpuType           CPUType       // Must be between UNIMPLEMENTED and MAX from above.
	ram               memory.Ram    // Interface to implementation RAM.
	clock             time.Duration // If non-zero indicates the cycle time per Tick (sleeps after processing to delay).
	avgClock          time.Duration // Empirically determined average run time of an instruction (if clock is non-zero).
	avgTime           time.Duration // Empirically determined average time that time.Now() calls take.
	timeRuns          int           // The precomputed number of times to delay loop to meet the clock cycle above.
	timeNeedAdjust    bool          // If true adds one to timeRuns every other cycle to account for the fact it undershoots by default.
	timeAdjustCnt     float64       // The number of ticks we're off by (too slow) and need adjusting every so often.
	timerTicks        float64       // Number of ticks in this sequence before resetting.
	timerTicksReset   int           // At the tick we should reset our counting for adjustment.
	reset             bool          // Whether reset has occurred.
	op                uint8         // The current working opcode
	opVal             uint8         // The 1st byte argument after the opcode (all instructions have this).
	opTick            int           // Tick number for internal operation of opcode.
	opAddr            uint16        // Address computed during opcode to be used for read/write (indirect, etc modes).
	opDone            bool          // Stays false until the current opcode has completed all ticks.
	addrDone          bool          // Stays false until the current opcode has completed any addressing mode ticks.
	skipInterrupt     bool          // Skip interrupt processing on the next instruction.
	prevSkipInterrupt bool          // Previous instruction skipped interrupt processing (so we shouldn't).
	irqRaised         irqType       // Must be between UNIMPLEMENTED and MAX from above.
	runningInterrupt  bool          // Whether we're running an interrupt setup or an opcode.
	halted            bool          // If stopped due to a halt instruction
	haltOpcode        uint8         // Opcode that caused the halt
}

// A few custom error types to distinguish why the CPU stopped.

// InvalidCPUState represents an invalid CPU state in the emulator.
type InvalidCPUState struct {
	Reason string
}

// Error implements the interface for error types.
func (e InvalidCPUState) Error() string {
	return fmt.Sprintf("invalid CPU state: %s", e.Reason)
}

// HaltOpcode represents an opcode which halts the CPU.
type HaltOpcode struct {
	Opcode uint8
}

// Error implements the interface for error types.
func (e HaltOpcode) Error() string {
	return fmt.Sprintf("HALT(0x%.2X) executed", e.Opcode)
}

// ChipDef defines a 65xx processor.
type ChipDef struct {
	// Cpu is the distinct cpu type for this implementation (stock 6502, 6510, 65C02, etc).
	Cpu CPUType
	// Ram is the RAM interface for this implementation.
	Ram memory.Ram
	// Irq is an optional IRQ source to trigger the IRQ line.
	Irq irq.Sender
	// Nmi is an optional IRQ source to trigger the NMI line (acts as edge trigger even though real HW is level).
	Nmi irq.Sender
	// Rdy s an optional IRQ source to trigger the RDY line (which halts the CPU). This is not technically an IRQ but acts the same.
	Rdy irq.Sender
}

// Init will create a new 65XX CPU of the type requested and return it in powered on state.
// If irq/nmi/rdy are non-nil they will be checked on each Tick() call and interrupt/hold
// the processor accordingly.
// The memory passed in will also be powered on and reset.
func Init(cpu *ChipDef) (*Chip, error) {
	if cpu.Cpu <= CPU_UNIMPLMENTED || cpu.Cpu >= CPU_MAX {
		return nil, InvalidCPUState{fmt.Sprintf("CPU type valid %d is invalid", cpu.Cpu)}
	}
	p := &Chip{
		cpuType:  cpu.Cpu,
		ram:      cpu.Ram,
		irq:      cpu.Irq,
		tickDone: true,
		nmi:      cpu.Nmi,
		rdy:      cpu.Rdy,
	}
	p.PowerOn()
	return p, nil
}

// SetClock will take the given duration and compute the average delay for a fast operation
// (consecutive time.Now() calls). This will then determine the number of times to call that
// in a delay loop at the end of every instruction.
// Will return an error if the system cannot compute a way to sleep in the amount of time required.
// NOTE: This precomputes the delay for time.Now() so it takes some wall time to run per call.
// TODO(jchacon): Implement on amd64 in terms of rdtsc instead as this is approximate at best and still has a decent amount of jitter.
//                Or use golang.org/x/sys/unix and at least on unix use nanosleep calls (TBD windows?)
func (p *Chip) SetClock(clk time.Duration) error {
	p.clock = clk
	p.timeRuns = 0
	if clk != 0 {
		var tot int64
		// 10000000 calls should be sufficient to get a reasonable average.
		const runs = int64(10000000)
		for i := int64(0); i < runs; i++ {
			s := time.Now()
			diff := time.Now().Sub(s).Nanoseconds()
			tot += diff
		}
		p.avgTime = time.Duration(float64(tot / runs))
		// Now get the average clock cycles for an instruction
		var err error
		p.avgClock, err = getClockAverage()
		if err != nil {
			return err
		}
		if p.avgClock > p.clock {
			return InvalidCPUState{fmt.Sprintf("can't set clock to %s as average time.Now() delay is %s", p.clock, p.avgClock)}
		}
		p.timeRuns = int((p.clock - p.avgClock) / p.avgTime)
		// If we undershoot the desired clocks by more than 5% still then set things so
		// we sleep an extra amount every N ticks to average out. Assuming no one is
		// running the CPU for so few ticks this jitter is actually noticable.
		if float64(p.timeRuns)*float64(p.avgTime)/float64(p.clock-p.avgClock) < 0.95 {
			p.timeNeedAdjust = true
			d := int64(p.clock-p.avgClock) - (int64(p.timeRuns) * int64(p.avgTime))
			p.timeAdjustCnt = float64(p.avgTime) / float64(d)
			// Assuming the above number isn't integral so we'll do adjustments by 10x.
			// i.e. say it's 1.3. Since we can't add a sleep every 1.3 ticks we'll add 13 over 10 ticks
			//      instead by adding extra for the ones where we need to adjust by > 1. This gives
			//      much better jitter control than simply doing every other (as if often the case)
			//      which still undershoots by quite a bit.
			//      This could be even more accurate by going to more orders of magnitude but testing shows this
			//      is pretty good until we get to something accurate by measuring the cycle timer. See Tick() for impl.
			p.timerTicksReset = int(p.timeAdjustCnt * 10)
			p.timerTicks = 0
		}
	}
	return nil
}

type staticMemory struct {
	ret uint8 // Always return this value on reads. Write are ignored.
}

func (r *staticMemory) Read(addr uint16) uint8 {
	return r.ret
}
func (r *staticMemory) Write(addr uint16, val uint8) {}
func (r *staticMemory) PowerOn()                     {}

// getClockAverage will fire up a CPU internally to benchmark the average of N calls of NOP vs N calls of ADC (most expensive op)
// to return an average length of time it takes to run. Will return an error if something goes wrong.
func getClockAverage() (time.Duration, error) {
	var totElapsed time.Duration
	totCycles := 0
	// LDA #i and ADC a
	// Can assume LDA is likely close enough to average run time but we measure ADC to get something to average against.
	for _, test := range []uint8{0xA9, 0x6D} {
		got := 0
		r := &staticMemory{test}
		c, err := Init(&ChipDef{CPU_NMOS, r, nil, nil, nil})
		if err != nil {
			return 0, fmt.Errorf("getClockAverage init CPU: %v", err)
		}
		n := time.Now()
		// Execute 10 million cycles so we get a reasonable timediff.
		// Otherwise calling time.Now() too close to another call mostly shows
		// upwards of 10ns of overhead just for gathering time (depending on arch).
		// At this many instructions we're accurate to 5-6 decimal places so "good enough".
		for i := 0; i < 10000000; i++ {
			if err := c.Tick(); err != nil {
				return 0, fmt.Errorf("getClockAverage Tick: %v", err)
			}
			c.TickDone()
			got++
		}
		totElapsed += time.Now().Sub(n)
		totCycles += got
	}
	return time.Duration(float64(totElapsed) / float64(totCycles)), nil
}

// PowerOn will reset the CPU to power on state which isn't well defined.
// Registers are random, stack is at random (though visual 6502 claims it's 0xFD due to a push P/PC in reset).
// and P is cleared with interrupts disabled and decimal mode random (for NMOS).
// The starting PC value is loaded from the reset vector.
// TODO(jchacon): See if any of this gets more defined on CMOS versions.
func (p *Chip) PowerOn() error {
	rand.Seed(time.Now().UnixNano())
	// This bit is always set.
	flags := P_S1
	// Randomize decimal state at startup for base NMOS types.
	if p.cpuType == CPU_NMOS || p.cpuType == CPU_NMOS_6510 {
		if rand.Float32() > 0.5 {
			flags |= P_DECIMAL
		}
	}

	// Randomize register contents
	p.A = uint8(rand.Intn(256))
	p.X = uint8(rand.Intn(256))
	p.Y = uint8(rand.Intn(256))
	p.S = uint8(rand.Intn(256))
	p.P = flags
	// Reset to get everything else setup.
	for {
		done, err := p.Reset()
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	return nil
}

// Reset is similar to PowerOn except the main registers are not touched. The stack is moved
// 3 bytes as if PC/P have been pushed. Flags are not disturbed except for interrupts being disabled
// and the PC is loaded from the reset vector. This takes 6 cycles once triggered.
// Will return true when reset is complete and errors if any occur.
func (p *Chip) Reset() (bool, error) {
	// If we haven't previously started a reset trigger it now
	if !p.reset {
		p.reset = true
		p.tickDone = false
		p.opTick = 0
	}
	p.opTick++
	switch {
	case p.opTick < 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("Reset: bad opTick: %d", p.opTick)}
	case p.opTick == 1:
		// Standard first tick reads current PC value
		_ = p.ram.Read(p.PC)
		// Disable interrupts
		p.P |= P_INTERRUPT
		// Reset other state now
		p.halted = false
		p.haltOpcode = 0x00
		p.irqRaised = kIRQ_NONE
		return false, nil
	case p.opTick >= 2 && p.opTick <= 4:
		// Most registers unaffected but stack acts like PC/P have been pushed so decrement by 3 bytes over next 3 ticks.
		p.S--
		return false, nil
	case p.opTick == 5:
		// Load PC from reset vector
		p.opVal = p.ram.Read(RESET_VECTOR)
		return false, nil
	}
	// case p.opTick == 6:
	p.PC = (uint16(p.ram.Read(RESET_VECTOR+1)) << 8) + uint16(p.opVal)
	p.reset = false
	p.opTick = 0
	p.tickDone = true
	return true, nil
}

// Tick runs a clock cycle through the CPU which may execute a new instruction or may be finishing
// an existing one. True is returned if the current instruction has finished.
// An error is returned if the instruction isn't implemented or otherwise halts the CPU.
// For an NMOS cpu on a taken branch and an interrupt coming in immediately after will cause one
// more instruction to be executed before the first interrupt instruction. This is accounted
// for by executing this instruction before handling the interrupt (whose state is cached).
func (p *Chip) Tick() error {
	if !p.tickDone {
		p.opDone = true
		return InvalidCPUState{"called Tick() without calling TickDone() at end of last cycle"}
	}
	p.tickDone = false

	// If RDY is held high we do nothing and just return (time doesn't advance in the CPU).
	// TODO(jchacon): Ok, this technically only works like this in combination with SYNC being held high as well.
	//                Otherwise it acts like a single step and continues after the next clock.
	//                But, the only use known right now was atari 2600 which tied SYNC high and RDY low at the same
	//                time so "good enough".
	if p.rdy != nil && p.rdy.Raised() {
		p.opDone = false
		return nil
	}

	// Institute delay up front since we can return in N places below.
	times := p.timeRuns
	if p.timeNeedAdjust {
		// Only add time if incrementing tick didn't jump by more than a single digit.
		// i.e. if we're at 1.3 we tick at 0, 1.3, 2.6, 3.9 but not 5.2 as a result.
		o := int(p.timerTicks) + 1
		p.timerTicks += p.timeAdjustCnt
		if o != int(p.timerTicks) {
			times++
		}
		if int(p.timerTicks) >= p.timerTicksReset {
			p.timerTicks = 0
		}
	}
	for i := 0; i < times; i++ {
		_ = time.Now()
	}
	if p.irqRaised < kIRQ_NONE || p.irqRaised >= kIRQ_MAX {
		p.opDone = true
		return InvalidCPUState{fmt.Sprintf("p.irqRaised is invalid: %d", p.irqRaised)}
	}
	// Fast path if halted. The PC won't advance. i.e. we just keep returning the same error.
	if p.halted {
		p.opDone = true
		return HaltOpcode{p.haltOpcode}
	}

	// Increment up front so we're not zero based per se. i.e. each new instruction then
	// starts at opTick == 1.
	p.opTick++

	// If we get a new interrupt while running one then NMI always wins until it's done.
	var irq, nmi bool
	if p.irq != nil {
		irq = p.irq.Raised()
	}
	if p.nmi != nil {
		nmi = p.nmi.Raised()
	}
	if irq || nmi {
		switch p.irqRaised {
		case kIRQ_NONE:
			p.irqRaised = kIRQ_IRQ
			if nmi {
				p.irqRaised = kIRQ_NMI
			}
		case kIRQ_IRQ:
			if nmi {
				p.irqRaised = kIRQ_NMI
			}
		}
	}

	switch {
	case p.opTick == 1:
		// If opTick is 1 it means we're starting a new instruction based on the PC value so grab the opcode now.
		p.op = p.ram.Read(p.PC)

		// Reset done state
		p.opDone = false
		p.addrDone = false

		// PC always advances on every opcode start except IRQ/HMI (unless we're skipping to run one more instruction).
		if p.irqRaised == kIRQ_NONE || p.skipInterrupt {
			p.PC++
			p.runningInterrupt = false
		}
		if p.irqRaised != kIRQ_NONE && !p.skipInterrupt {
			p.runningInterrupt = true
		}
		return nil
	case p.opTick == 2:
		// All instructions fetch the value after the opcode (though some like BRK/PHP/etc ignore it).
		// We keep it since some instructions such as absolute addr then require getting one
		// more byte. So cache at this stage since we no idea if it's needed.
		// NOTE: the PC doesn't increment here as that's dependent on addressing mode which will handle it.
		p.opVal = p.ram.Read(p.PC)

		// We've started a new instruction so no longer skipping interrupt processing.
		p.prevSkipInterrupt = false
		if p.skipInterrupt {
			p.skipInterrupt = false
			p.prevSkipInterrupt = true
		}
	case p.opTick > 8:
		// This is impossible on a 65XX as all instructions take no more than 8 ticks.
		// Technically documented instructions max at 7 ticks but a RMW indirect X/Y will take 8.
		p.opDone = true
		return InvalidCPUState{fmt.Sprintf("opTick %d too large (> 8)", p.opTick)}
	}

	var err error
	if p.runningInterrupt {
		addr := IRQ_VECTOR
		if p.irqRaised == kIRQ_NMI {
			addr = NMI_VECTOR
		}
		p.opDone, err = p.runInterrupt(addr, true)
	} else {
		p.opDone, err = p.processOpcode()
	}

	if p.halted {
		p.haltOpcode = p.op
		p.opDone = true
		return HaltOpcode{p.op}
	}
	if err != nil {
		// Still consider this a halt since it's an internal precondition check.
		p.haltOpcode = p.op
		p.halted = true
		p.opDone = true
		return err
	}
	if p.opDone {
		// So the next tick starts a new instruction
		// It'll handle doing start of instruction reset on state (which includes resetting p.opDone, p.addrDone).
		p.opTick = 0
		// If we're currently running one clear state so we don't loop trying to run it again.
		if p.runningInterrupt {
			p.irqRaised = kIRQ_NONE
		}
		p.runningInterrupt = false
	}
	return nil
}

// TickDone is to be called after all chips have run a given Tick() cycle in order to do post
// processing that's normally controlled by a clock interlocking all the chips. i.e. setups for
// latch loads that take effect on the start of the next cycle. i.e. this could have been
// implemented as PreTick in the same way. Including this in Tick() requires a specific
// ordering between chips in order to present a consistent view otherwise.
func (p *Chip) TickDone() {
	p.tickDone = true
}

func (p *Chip) InstructionDone() bool {
	return p.opDone
}

func (p *Chip) processOpcode() (bool, error) {
	// Opcode matric taken from:
	// http://wiki.nesdev.com/w/index.php/CPU_unofficial_opcodes#Games_using_unofficial_opcodes
	//
	// NOTE: The above lists 0xAB as LAX #i but we call it OAL since it has odd behavior and needs
	//       it's own code compared to other LAX. See 6502-NMOS.extra.opcodes below.
	//
	// Description of undocumented opcodes:
	//
	// http://www.ffd2.com/fridge/docs/6502-NMOS.extra.opcodes
	// http://nesdev.com/6502_cpu.txt
	// http://visual6502.org/wiki/index.php?title=6502_Opcode_8B_(XAA,_ANE)
	//
	// Opcode descriptions/timing/etc:
	// http://obelisk.me.uk/6502/reference.html

	// Preset (just in case). There is no default below since all cases are covered.
	var err error
	err = InvalidCPUState{"Invalid CPU state"}

	switch p.op {
	case 0x00:
		// BRK #i
		p.opDone, err = p.iBRK()
	case 0x01:
		// ORA (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iORA)
	case 0x02:
		// HLT
		p.halted = true
	case 0x03:
		// SLO (d,x)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iSLO)
	case 0x04:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x05:
		// ORA d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iORA)
	case 0x06:
		// ASL d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iASL)
	case 0x07:
		// SLO d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iSLO)
	case 0x08:
		// PHP
		p.opDone, err = p.iPHP()
	case 0x09:
		// ORA #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iORA)
	case 0x0A:
		// ASL
		p.opDone, err = p.iASLAcc()
	case 0x0B:
		// ANC #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iANC)
	case 0x0C:
		// NOP a
		p.opDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
	case 0x0D:
		// ORA a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iORA)
	case 0x0E:
		// ASL a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iASL)
	case 0x0F:
		// SLO a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iSLO)
	case 0x10:
		// BPL *+r
		p.opDone, err = p.iBPL()
	case 0x11:
		// ORA (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iORA)
	case 0x12:
		// HLT
		p.halted = true
	case 0x13:
		// SLO (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iSLO)
	case 0x14:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x15:
		// ORA d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.iORA)
	case 0x16:
		// ASL d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iASL)
	case 0x17:
		// SLO d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iSLO)
	case 0x18:
		// CLC
		p.opDone, err = p.iCLC()
	case 0x19:
		// ORA a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iORA)
	case 0x1A:
		// NOP
		p.opDone, err = true, nil
	case 0x1B:
		// SLO a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iSLO)
	case 0x1C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x1D:
		// ORA a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.iORA)
	case 0x1E:
		// ASL a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iASL)
	case 0x1F:
		// SLO a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iSLO)
	case 0x20:
		// JSR a
		p.opDone, err = p.iJSR()
	case 0x21:
		// AND (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iAND)
	case 0x22:
		// HLT
		p.halted = true
	case 0x23:
		// RLA (d,x)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iRLA)
	case 0x24:
		// BIT d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iBIT)
	case 0x25:
		// AND d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iAND)
	case 0x26:
		// ROL d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iROL)
	case 0x27:
		// RLA d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iRLA)
	case 0x28:
		// PLP
		p.opDone, err = p.iPLP()
	case 0x29:
		// AND #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iAND)
	case 0x2A:
		// ROL
		p.opDone, err = p.iROLAcc()
	case 0x2B:
		// ANC #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iANC)
	case 0x2C:
		// BIT a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iBIT)
	case 0x2D:
		// AND a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iAND)
	case 0x2E:
		// ROL a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iROL)
	case 0x2F:
		// RLA a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iRLA)
	case 0x30:
		// BMI *+r
		p.opDone, err = p.iBMI()
	case 0x31:
		// AND (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iAND)
	case 0x32:
		// HLT
		p.halted = true
	case 0x33:
		// RLA (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iRLA)
	case 0x34:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x35:
		// AND d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.iAND)
	case 0x36:
		// ROL d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iROL)
	case 0x37:
		// RLA d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iRLA)
	case 0x38:
		// SEC
		p.opDone, err = p.iSEC()
	case 0x39:
		// AND a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iAND)
	case 0x3A:
		// NOP
		p.opDone, err = true, nil
	case 0x3B:
		// RLA a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iRLA)
	case 0x3C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x3D:
		// AND a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.iAND)
	case 0x3E:
		// ROL a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iROL)
	case 0x3F:
		// RLA a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iRLA)
	case 0x40:
		// RTI
		p.opDone, err = p.iRTI()
	case 0x41:
		// EOR (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iEOR)
	case 0x42:
		// HLT
		p.halted = true
	case 0x43:
		// SRE (d,x)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iSRE)
	case 0x44:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x45:
		// EOR d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iEOR)
	case 0x46:
		// LSR d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iLSR)
	case 0x47:
		// SRE d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iSRE)
	case 0x48:
		// PHA
		p.opDone, err = p.iPHA()
	case 0x49:
		// EOR #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iEOR)
	case 0x4A:
		// LSR
		p.opDone, err = p.iLSRAcc()
	case 0x4B:
		// ALR #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iALR)
	case 0x4C:
		// JMP a
		p.opDone, err = p.iJMP()
	case 0x4D:
		// EOR a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iEOR)
	case 0x4E:
		// LSR a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iLSR)
	case 0x4F:
		// SRE a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iSRE)
	case 0x50:
		// BVC *+r
		p.opDone, err = p.iBVC()
	case 0x51:
		// EOR (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iEOR)
	case 0x52:
		// HLT
		p.halted = true
	case 0x53:
		// SRE (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iSRE)
	case 0x54:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x55:
		// EOR d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.iEOR)
	case 0x56:
		// LSR d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iLSR)
	case 0x57:
		// SRE d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iSRE)
	case 0x58:
		// CLI
		p.opDone, err = p.iCLI()
	case 0x59:
		// EOR a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iEOR)
	case 0x5A:
		// NOP
		p.opDone, err = true, nil
	case 0x5B:
		// SRE a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iSRE)
	case 0x5C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x5D:
		// EOR a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.iEOR)
	case 0x5E:
		// LSR a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iLSR)
	case 0x5F:
		// SRE a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iSRE)
	case 0x60:
		// RTS
		p.opDone, err = p.iRTS()
	case 0x61:
		// ADC (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iADC)
	case 0x62:
		// HLT
		p.halted = true
	case 0x63:
		// RRA (d,x)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iRRA)
	case 0x64:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x65:
		// ADC d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iADC)
	case 0x66:
		// ROR d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iROR)
	case 0x67:
		// RRA d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iRRA)
	case 0x68:
		// PLA
		p.opDone, err = p.iPLA()
	case 0x69:
		// ADC #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iADC)
	case 0x6A:
		// ROR
		p.opDone, err = p.iRORAcc()
	case 0x6B:
		// ARR #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iARR)
	case 0x6C:
		// JMP (a)
		p.opDone, err = p.iJMPIndirect()
	case 0x6D:
		// ADC a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iADC)
	case 0x6E:
		// ROR a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iROR)
	case 0x6F:
		// RRA a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iRRA)
	case 0x70:
		// BVS *+r
		p.opDone, err = p.iBVS()
	case 0x71:
		// ADC (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iADC)
	case 0x72:
		// HLT
		p.halted = true
	case 0x73:
		// RRA (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iRRA)
	case 0x74:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x75:
		// ADC d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.iADC)
	case 0x76:
		// ROR d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iROR)
	case 0x77:
		// RRA d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iRRA)
	case 0x78:
		// SEI
		p.opDone, err = p.iSEI()
	case 0x79:
		// ADC a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iADC)
	case 0x7A:
		// NOP
		p.opDone, err = true, nil
	case 0x7B:
		// RRA a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iRRA)
	case 0x7C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x7D:
		// ADC a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.iADC)
	case 0x7E:
		// ROR a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iROR)
	case 0x7F:
		// RRA a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iRRA)
	case 0x80:
		// NOP #i
		p.opDone, err = p.addrImmediate(kLOAD_INSTRUCTION)
	case 0x81:
		// STA (d,x)
		p.opDone, err = p.storeInstruction(p.addrIndirectX, p.A)
	case 0x82:
		// NOP #i
		p.opDone, err = p.addrImmediate(kLOAD_INSTRUCTION)
	case 0x83:
		// SAX (d,x)
		p.opDone, err = p.storeInstruction(p.addrIndirectX, p.A&p.X)
	case 0x84:
		// STY d
		p.opDone, err = p.storeInstruction(p.addrZP, p.Y)
	case 0x85:
		// STA d
		p.opDone, err = p.storeInstruction(p.addrZP, p.A)
	case 0x86:
		// STX d
		p.opDone, err = p.storeInstruction(p.addrZP, p.X)
	case 0x87:
		// SAX d
		p.opDone, err = p.storeInstruction(p.addrZP, p.A&p.X)
	case 0x88:
		// DEY
		p.opDone, err = p.loadRegister(&p.Y, p.Y-1)
	case 0x89:
		// NOP #i
		p.opDone, err = p.addrImmediate(kLOAD_INSTRUCTION)
	case 0x8A:
		// TXA
		p.opDone, err = p.loadRegister(&p.A, p.X)
	case 0x8B:
		// XAA #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iXAA)
	case 0x8C:
		// STY a
		p.opDone, err = p.storeInstruction(p.addrAbsolute, p.Y)
	case 0x8D:
		// STA a
		p.opDone, err = p.storeInstruction(p.addrAbsolute, p.A)
	case 0x8E:
		// STX a
		p.opDone, err = p.storeInstruction(p.addrAbsolute, p.X)
	case 0x8F:
		// SAX a
		p.opDone, err = p.storeInstruction(p.addrAbsolute, p.A&p.X)
	case 0x90:
		// BCC *+d
		p.opDone, err = p.iBCC()
	case 0x91:
		// STA (d),y
		p.opDone, err = p.storeInstruction(p.addrIndirectY, p.A)
	case 0x92:
		// HLT
		p.halted = true
	case 0x93:
		// AHX (d),y
		p.opDone, err = p.iAHX(p.addrIndirectY)
	case 0x94:
		// STY d,x
		p.opDone, err = p.storeInstruction(p.addrZPX, p.Y)
	case 0x95:
		// STA d,x
		p.opDone, err = p.storeInstruction(p.addrZPX, p.A)
	case 0x96:
		// STX d,y
		p.opDone, err = p.storeInstruction(p.addrZPY, p.X)
	case 0x97:
		// SAX d,y
		p.opDone, err = p.storeInstruction(p.addrZPY, p.A&p.X)
	case 0x98:
		// TYA
		p.opDone, err = p.loadRegister(&p.A, p.Y)
	case 0x99:
		// STA a,y
		p.opDone, err = p.storeInstruction(p.addrAbsoluteY, p.A)
	case 0x9A:
		// TXS
		p.opDone, err, p.S = true, nil, p.X
	case 0x9B:
		// TAS a,y
		p.opDone, err = p.iTAS()
	case 0x9C:
		// SHY a,x
		p.opDone, err = p.iSHY(p.addrAbsoluteX)
	case 0x9D:
		// STA a,x
		p.opDone, err = p.storeInstruction(p.addrAbsoluteX, p.A)
	case 0x9E:
		// SHX a,y
		p.opDone, err = p.iSHX(p.addrAbsoluteY)
	case 0x9F:
		// AHX a,y
		p.opDone, err = p.iAHX(p.addrAbsoluteY)
	case 0xA0:
		// LDY #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.loadRegisterY)
	case 0xA1:
		// LDA (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.loadRegisterA)
	case 0xA2:
		// LDX #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.loadRegisterX)
	case 0xA3:
		// LAX (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iLAX)
	case 0xA4:
		// LDY d
		p.opDone, err = p.loadInstruction(p.addrZP, p.loadRegisterY)
	case 0xA5:
		// LDA d
		p.opDone, err = p.loadInstruction(p.addrZP, p.loadRegisterA)
	case 0xA6:
		// LDX d
		p.opDone, err = p.loadInstruction(p.addrZP, p.loadRegisterX)
	case 0xA7:
		// LAX d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iLAX)
	case 0xA8:
		// TAY
		p.opDone, err = p.loadRegister(&p.Y, p.A)
	case 0xA9:
		// LDA #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.loadRegisterA)
	case 0xAA:
		// TAX
		p.opDone, err = p.loadRegister(&p.X, p.A)
	case 0xAB:
		// OAL #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iOAL)
	case 0xAC:
		// LDY a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.loadRegisterY)
	case 0xAD:
		// LDA a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.loadRegisterA)
	case 0xAE:
		// LDX a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.loadRegisterX)
	case 0xAF:
		// LAX a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iLAX)
	case 0xB0:
		// BCS *+d
		p.opDone, err = p.iBCS()
	case 0xB1:
		// LDA (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.loadRegisterA)
	case 0xB2:
		// HLT
		p.halted = true
	case 0xB3:
		// LAX (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iLAX)
	case 0xB4:
		// LDY d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.loadRegisterY)
	case 0xB5:
		// LDA d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.loadRegisterA)
	case 0xB6:
		// LDX d,y
		p.opDone, err = p.loadInstruction(p.addrZPY, p.loadRegisterX)
	case 0xB7:
		// LAX d,y
		p.opDone, err = p.loadInstruction(p.addrZPY, p.iLAX)
	case 0xB8:
		// CLV
		p.opDone, err = p.iCLV()
	case 0xB9:
		// LDA a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.loadRegisterA)
	case 0xBA:
		// TSX
		p.opDone, err = p.loadRegister(&p.X, p.S)
	case 0xBB:
		// LAS a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iLAS)
	case 0xBC:
		// LDY a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.loadRegisterY)
	case 0xBD:
		// LDA a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.loadRegisterA)
	case 0xBE:
		// LDX a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.loadRegisterX)
	case 0xBF:
		// LAX a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iLAX)
	case 0xC0:
		// CPY #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.compareY)
	case 0xC1:
		// CMP (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.compareA)
	case 0xC2:
		// NOP #i
		p.opDone, err = p.addrImmediate(kLOAD_INSTRUCTION)
	case 0xC3:
		// DCP (d,X)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iDCP)
	case 0xC4:
		// CPY d
		p.opDone, err = p.loadInstruction(p.addrZP, p.compareY)
	case 0xC5:
		// CMP d
		p.opDone, err = p.loadInstruction(p.addrZP, p.compareA)
	case 0xC6:
		// DEC d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iDEC)
	case 0xC7:
		// DCP d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iDCP)
	case 0xC8:
		// INY
		p.opDone, err = p.loadRegister(&p.Y, p.Y+1)
	case 0xC9:
		// CMP #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.compareA)
	case 0xCA:
		// DEX
		p.opDone, err = p.loadRegister(&p.X, p.X-1)
	case 0xCB:
		// AXS #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iAXS)
	case 0xCC:
		// CPY a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.compareY)
	case 0xCD:
		// CMP a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.compareA)
	case 0xCE:
		// DEC a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iDEC)
	case 0xCF:
		// DCP a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iDCP)
	case 0xD0:
		// BNE *+r
		p.opDone, err = p.iBNE()
	case 0xD1:
		// CMP (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.compareA)
	case 0xD2:
		// HLT
		p.halted = true
	case 0xD3:
		// DCP (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iDCP)
	case 0xD4:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0xD5:
		// CMP d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.compareA)
	case 0xD6:
		// DEC d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iDEC)
	case 0xD7:
		// DCP d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iDCP)
	case 0xD8:
		// CLD
		p.opDone, err = p.iCLD()
	case 0xD9:
		// CMP a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.compareA)
	case 0xDA:
		// NOP
		p.opDone, err = true, nil
	case 0xDB:
		// DCP a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iDCP)
	case 0xDC:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0xDD:
		// CMP a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.compareA)
	case 0xDE:
		// DEC a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iDEC)
	case 0xDF:
		// DCP a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iDCP)
	case 0xE0:
		// CPX #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.compareX)
	case 0xE1:
		// SBC (d,x)
		p.opDone, err = p.loadInstruction(p.addrIndirectX, p.iSBC)
	case 0xE2:
		// NOP #i
		p.opDone, err = p.addrImmediate(kLOAD_INSTRUCTION)
	case 0xE3:
		// ISC (d,x)
		p.opDone, err = p.rmwInstruction(p.addrIndirectX, p.iISC)
	case 0xE4:
		// CPX d
		p.opDone, err = p.loadInstruction(p.addrZP, p.compareX)
	case 0xE5:
		// SBC d
		p.opDone, err = p.loadInstruction(p.addrZP, p.iSBC)
	case 0xE6:
		// INC d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iINC)
	case 0xE7:
		// ISC d
		p.opDone, err = p.rmwInstruction(p.addrZP, p.iISC)
	case 0xE8:
		// INX
		p.opDone, err = p.loadRegister(&p.X, p.X+1)
	case 0xE9:
		// SBC #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iSBC)
	case 0xEA:
		// NOP
		p.opDone, err = true, nil
	case 0xEB:
		// SBC #i
		p.opDone, err = p.loadInstruction(p.addrImmediate, p.iSBC)
	case 0xEC:
		// CPX a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.compareX)
	case 0xED:
		// SBC a
		p.opDone, err = p.loadInstruction(p.addrAbsolute, p.iSBC)
	case 0xEE:
		// INC a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iINC)
	case 0xEF:
		// ISC a
		p.opDone, err = p.rmwInstruction(p.addrAbsolute, p.iISC)
	case 0xF0:
		// BEQ *+d
		p.opDone, err = p.iBEQ()
	case 0xF1:
		// SBC (d),y
		p.opDone, err = p.loadInstruction(p.addrIndirectY, p.iSBC)
	case 0xF2:
		// HLT
		p.halted = true
	case 0xF3:
		// ISC (d),y
		p.opDone, err = p.rmwInstruction(p.addrIndirectY, p.iISC)
	case 0xF4:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0xF5:
		// SBC d,x
		p.opDone, err = p.loadInstruction(p.addrZPX, p.iSBC)
	case 0xF6:
		// INC d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iINC)
	case 0xF7:
		// ISC d,x
		p.opDone, err = p.rmwInstruction(p.addrZPX, p.iISC)
	case 0xF8:
		// SED
		p.opDone, err = p.iSED()
	case 0xF9:
		// SBC a,y
		p.opDone, err = p.loadInstruction(p.addrAbsoluteY, p.iSBC)
	case 0xFA:
		// NOP
		p.opDone, err = true, nil
	case 0xFB:
		// ISC a,y
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteY, p.iISC)
	case 0xFC:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0xFD:
		// SBC a,x
		p.opDone, err = p.loadInstruction(p.addrAbsoluteX, p.iSBC)
	case 0xFE:
		// INC a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iINC)
	case 0xFF:
		// ISC a,x
		p.opDone, err = p.rmwInstruction(p.addrAbsoluteX, p.iISC)
	}
	return p.opDone, err
}

// zeroCheck sets the Z flag based on the register contents.
func (p *Chip) zeroCheck(reg uint8) {
	p.P &^= P_ZERO
	if reg == 0 {
		p.P |= P_ZERO
	}
}

// negativeCheck sets the N flag based on the register contents.
func (p *Chip) negativeCheck(reg uint8) {
	p.P &^= P_NEGATIVE
	if (reg & P_NEGATIVE) == 0x80 {
		p.P |= P_NEGATIVE
	}
}

// carryCheck sets the C flag if the result of an 8 bit ALU operation
// (passed as a 16 bit result) caused a carry out by generating a value >= 0x100.
// NOTE: normally this just means masking 0x100 but in some overflow cases for BCD
//       math the value can be 0x200 here so it's still a carry.
func (p *Chip) carryCheck(res uint16) {
	p.P &^= P_CARRY
	if res >= 0x100 {
		p.P |= P_CARRY
	}
}

// overflowCheck sets the V flag if the result of the ALU operation
// caused a two's complement sign change.
// Taken from http://www.righto.com/2012/12/the-6502-overflow-flag-explained.html
func (p *Chip) overflowCheck(reg uint8, arg uint8, res uint8) {
	p.P &^= P_OVERFLOW
	// If the originals signs differ from the end sign bit
	if (reg^res)&(arg^res)&0x80 != 0x00 {
		p.P |= P_OVERFLOW
	}
}

// instructionMode is an enumeration indicating the type of instruction being processed.
// Used below in addressing modes.
type instructionMode int

const (
	kLOAD_INSTRUCTION instructionMode = iota
	kRMW_INSTRUCTION
	kSTORE_INSTRUCTION
)

// addrImmediate implements immediate mode - #i
// returning the value in p.opVal
// NOTE: This has no W or RMW mode so the argument is ignored.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrImmediate(instructionMode) (bool, error) {
	if p.opTick != 2 {
		return true, InvalidCPUState{fmt.Sprintf("addrImmediate invalid opTick %d, not 2", p.opTick)}
	}
	// This mode consumed the opVal so increment the PC.
	p.PC++
	return true, nil
}

// addrZP implements Zero page mode - d
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrZP(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 4:
		return true, InvalidCPUState{fmt.Sprintf("addrZP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Already read the value but need to bump the PC
		p.opAddr = uint16(0x00FF & p.opVal)
		p.PC++
		done := false
		// For a store we're done since we have the address needed.
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 3:
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 4:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// addrZPX implements Zero page plus X mode - d,x
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrZPX(mode instructionMode) (bool, error) {
	return p.addrZPXY(mode, p.X)
}

// addrZPY implements Zero page plus Y mode - d,y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrZPY(mode instructionMode) (bool, error) {
	return p.addrZPXY(mode, p.Y)
}

// addrZPXY implements the details for addrZPX and addrZPY since they only differ based on the register used.
// See those functions for arg/return specifics.
func (p *Chip) addrZPXY(mode instructionMode, reg uint8) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 5:
		return true, InvalidCPUState{fmt.Sprintf("addrZPXY invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// Already read the value but need to bump the PC
		p.opAddr = uint16(0x00FF & p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Read from the ZP addr and then add the register for the real read later.
		_ = p.ram.Read(p.opAddr)
		// Does this as a uint8 so it wraps as needed.
		p.opAddr = uint16(uint8(p.opVal + reg))
		done := false
		// For a store we're done since we have the address needed.
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 4:
		// Now read from the final address.
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 5:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// addrIndirectX implements Zero page indirect plus X mode - (d,x)
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrIndirectX(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 7:
		return true, InvalidCPUState{fmt.Sprintf("addrIndirectX invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// Already read the value but need to bump the PC
		p.opAddr = uint16(0x00FF & p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Read from the ZP addr. We'll add the X register as well for the real read next.
		_ = p.ram.Read(p.opAddr)
		// Does this as a uint8 so it wraps as needed.
		p.opAddr = uint16(uint8(p.opVal + p.X))
		return false, nil
	case p.opTick == 4:
		// Read effective addr low byte.
		p.opVal = p.ram.Read(p.opAddr)
		// Setup opAddr for next read and handle wrapping
		p.opAddr = uint16(uint8(p.opAddr&0x00FF) + 1)
		return false, nil
	case p.opTick == 5:
		p.opAddr = (uint16(p.ram.Read(p.opAddr)) << 8) + uint16(p.opVal)
		done := false
		// For a store we're done since we have the address needed.
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 6:
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 7:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// addrIndirectY implements Zero page indirect plus Y mode - (d),y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrIndirectY(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 7:
		return true, InvalidCPUState{fmt.Sprintf("addrIndirectY invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// Already read the value but need to bump the PC
		p.opAddr = uint16(0x00FF & p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Read from the ZP addr to start building our pointer.
		p.opVal = p.ram.Read(p.opAddr)
		// Setup opAddr for next read and handle wrapping
		p.opAddr = uint16(uint8(p.opAddr&0x00FF) + 1)
		return false, nil
	case p.opTick == 4:
		// Compute effective address and then add Y to it (possibly wrongly).
		p.opAddr = (uint16(p.ram.Read(p.opAddr)) << 8) + uint16(p.opVal)
		// Add Y but do it in a way which won't page wrap (if needed)
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0xFF)+p.Y)
		p.opVal = 0
		if a != (p.opAddr + uint16(p.Y)) {
			// Signal for next phase we got it wrong.
			p.opVal = 1
		}
		p.opAddr = a
		return false, nil
	case p.opTick == 5:
		t := p.opVal
		p.opVal = p.ram.Read(p.opAddr)

		// Check old opVal to see if it's non-zero. If so it means the Y addition
		// crosses a page boundary and we'll have to fixup.
		// For a load operation that means another tick to read the correct
		// address.
		// For RMW it doesn't matter (we always do the extra tick).
		// For Store we're done. Just fixup p.opAddr so the return value is correct.
		done := true
		if t != 0 {
			p.opAddr += 0x0100
			if mode == kLOAD_INSTRUCTION {
				done = false
			}
		}
		// For RMW it doesn't matter, we tick again.
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 6:
		// Optional (on load) in case adding Y went past a page boundary.
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 7:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// addrAbsolute implements absolute mode - a
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrAbsolute(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 5:
		return true, InvalidCPUState{fmt.Sprintf("addrAbsolute invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// opVal has already been read so start constructing the address
		p.opAddr = 0x00FF & uint16(p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		p.opVal = p.ram.Read(p.PC)
		p.PC++
		p.opAddr |= (uint16(p.opVal) << 8)
		done := false
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 4:
		// For load and RMW instructions
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 5:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// addrAbsoluteX implements absolute plus X mode - a,x
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrAbsoluteX(mode instructionMode) (bool, error) {
	return p.addrAbsoluteXY(mode, p.X)
}

// addrAbsoluteY implements absolute plus X mode - a,y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Chip) addrAbsoluteY(mode instructionMode) (bool, error) {
	return p.addrAbsoluteXY(mode, p.Y)
}

// addrAbsoluteXY implements the details for addrAbsoluteX and addrAbsoluteY since they only differ based on the register used.
// See those functions for arg/return specifics.
func (p *Chip) addrAbsoluteXY(mode instructionMode, reg uint8) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("addrAbsoluteX invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// opVal has already been read so start constructing the address
		p.opAddr = 0x00FF & uint16(p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		p.opVal = p.ram.Read(p.PC)
		p.PC++
		p.opAddr |= (uint16(p.opVal) << 8)
		// Add X but do it in a way which won't page wrap (if needed)
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0x00FF)+reg)
		p.opVal = 0
		if a != (p.opAddr + uint16(reg)) {
			// Signal for next phase we got it wrong.
			p.opVal = 1
		}
		p.opAddr = a
		return false, nil
	case p.opTick == 4:
		t := p.opVal
		p.opVal = p.ram.Read(p.opAddr)
		// Check old opVal to see if it's non-zero. If so it means the X addition
		// crosses a page boundary and we'll have to fixup.
		// For a load operation that means another tick to read the correct
		// address.
		// For RMW it doesn't matter (we always do the extra tick).
		// For Store we're done. Just fixup p.opAddr so the return value is correct.
		done := true
		if t != 0 {
			p.opAddr += 0x0100
			if mode == kLOAD_INSTRUCTION {
				done = false
			}
		}
		// For RMW it doesn't matter, we tick again.
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 5:
		// Optional (on load) in case adding X went past a page boundary.
		p.opVal = p.ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	}
	// case p.opTick == 6:
	p.ram.Write(p.opAddr, p.opVal)
	return true, nil
}

// loadRegister takes the val and inserts it into the register passed in. It then does
// Z and N checks against the new value.
// Always returns true and no error since this is a single tick operation.
func (p *Chip) loadRegister(reg *uint8, val uint8) (bool, error) {
	*reg = val
	p.zeroCheck(*reg)
	p.negativeCheck(*reg)
	return true, nil
}

// loadRegisterA is the curried version of loadRegister that uses p.opVal and A implicitly.
// This way it can be used as the opFunc argument during load/rmw instructions.
// Always returns true and no error since this is a single tick operation.
func (p *Chip) loadRegisterA() (bool, error) {
	p.loadRegister(&p.A, p.opVal)
	return true, nil
}

// loadRegisterX is the curried version of loadRegister that uses p.opVal and X implicitly.
// This way it can be used as the opFunc argument during load/rmw instructions.
// Always returns true and no error since this is a single tick operation.
func (p *Chip) loadRegisterX() (bool, error) {
	return p.loadRegister(&p.X, p.opVal)
}

// loadRegisterY is the curried version of loadRegister that uses p.opVal and Y implicitly.
// This way it can be used as the opFunc argument during load/rmw instructions.
// Always returns true and no error since this is a single tick operation.
func (p *Chip) loadRegisterY() (bool, error) {
	return p.loadRegister(&p.Y, p.opVal)
}

// pushStack pushes the given byte onto the stack and adjusts the stack pointer accordingly.
func (p *Chip) pushStack(val uint8) {
	p.ram.Write(0x0100+uint16(p.S), val)
	p.S--
}

// popStack pops the top byte off the stack and adjusts the stack pointer accordingly.
func (p *Chip) popStack() uint8 {
	p.S++
	return p.ram.Read(0x0100 + uint16(p.S))
}

// branchNOP reads the next byte as the branch offset and increments the PC.
// Used for the 2rd tick when branches aren't taken.
func (p *Chip) branchNOP() (bool, error) {
	if p.opTick <= 1 || p.opTick > 3 {
		return true, InvalidCPUState{fmt.Sprintf("branchNOP invalid opTick %d", p.opTick)}
	}
	p.PC++
	return true, nil
}

// performBranch does the heavy lifting for branching by
// computing the new PC and computing appropriate cycle costs.
// It returns true when the instruction is done and error if the tick
// becomes invalid.
func (p *Chip) performBranch() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 4:
		return true, InvalidCPUState{fmt.Sprintf("performBranch invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Increment the PC
		p.PC++
		return false, nil
	case p.opTick == 3:
		// We only skip if the last instruction didn't. This way a branch always doesn't prevent interrupt processing
		// since real silicon this is what happens (just a delay in the pipelining).
		if !p.prevSkipInterrupt {
			p.skipInterrupt = true
		}
		// Per http://www.6502.org/tutorials/6502opcodes.html
		// the wrong page is defined as the a different page than
		// the next byte after the jump. i.e. current PC at the moment.

		// Now compute the new PC but possibly wrong page.
		// Stash the old one in p.opAddr so we can use in tick 4 if needed.
		p.opAddr = p.PC
		p.PC = (p.PC & 0xFF00) + uint16(uint8(p.PC&0x00FF)+p.opVal)
		// It always triggers a bus read of the PC.
		_ = p.ram.Read(p.PC)
		if p.PC == (p.opAddr + uint16(int16(int8(p.opVal)))) {
			return true, nil
		}
		return false, nil
	}
	// case p.opTick == 4:
	// Set correct PC value
	p.PC = p.opAddr + uint16(int16(int8(p.opVal)))
	// Always read the next opcode
	_ = p.ram.Read(p.PC)
	return true, nil
}

const BRK = uint8(0x00)

// runInterrupt does all the heavy lifting for any interrupt processing.
// i.e. pushing values onto the stack and loading PC with the right address.
// Pass in the vector to be used for loading the PC (which means for BRK
// it can change if an NMI happens before we get to the load ticks).
// Returns true when complete (and PC is correct). Can return an error on an
// invalid tick count.
func (p *Chip) runInterrupt(addr uint16, irq bool) (bool, error) {
	switch {
	case p.opTick < 1 || p.opTick > 7:
		return true, InvalidCPUState{fmt.Sprintf("runInterrupt invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// Increment the PC on a non IRQ (i.e. BRK) since that changes where returns happen.
		if !irq {
			p.PC++
		}
		return false, nil
	case p.opTick == 3:
		p.pushStack(uint8((p.PC & 0xFF00) >> 8))
		return false, nil
	case p.opTick == 4:
		p.pushStack(uint8(p.PC & 0xFF))
		return false, nil
	case p.opTick == 5:
		push := p.P
		// S1 is always set
		push |= P_S1
		// B always set unless this triggered due to IRQ
		push |= P_B
		if irq {
			push &^= P_B
		}
		if p.cpuType == CPU_CMOS {
			p.P &^= P_DECIMAL
		}
		p.P |= P_INTERRUPT
		p.pushStack(push)
		return false, nil
	case p.opTick == 6:
		p.opVal = p.ram.Read(addr)
		return false, nil
	}
	// case p.opTick == 7:
	p.PC = (uint16(p.ram.Read(addr+1)) << 8) + uint16(p.opVal)
	// If we didn't previously skip an interrupt from processing make sure we execute the first instruction of
	// a handler before firing again.
	if irq && !p.prevSkipInterrupt {
		p.skipInterrupt = true
	}
	return true, nil
}

// iADC implements the ADC/SBC instructions and sets all associated flags.
// For SBC (non BCD) simply ones-complement p.opVal before calling.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iADC() (bool, error) {
	// Pull the carry bit out which thankfully is the low bit so can be
	// used directly.
	carry := p.P & P_CARRY

	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.cpuType != CPU_NMOS_RICOH {
		// BCD details - http://6502.org/tutorials/decimal_mode.html
		// Also http://nesdev.com/6502_cpu.txt but it has errors
		aL := (p.A & 0x0F) + (p.opVal & 0x0F) + carry
		// Low nibble fixup
		if aL >= 0x0A {
			aL = ((aL + 0x06) & 0x0f) + 0x10
		}
		sum := uint16(p.A&0xF0) + uint16(p.opVal&0xF0) + uint16(aL)
		// High nibble fixup
		if sum >= 0xA0 {
			sum += 0x60
		}
		res := uint8(sum & 0xFF)
		seq := (p.A & 0xF0) + (p.opVal & 0xF0) + aL
		bin := p.A + p.opVal + carry
		p.overflowCheck(p.A, p.opVal, seq)
		p.carryCheck(sum)
		// TODO(jchacon): CMOS gets N/Z set correctly and needs implementing.
		p.negativeCheck(seq)
		p.zeroCheck(bin)
		p.A = res
		return true, nil
	}

	// Otherwise do normal binary math.
	sum := p.A + p.opVal + carry
	p.overflowCheck(p.A, p.opVal, sum)
	// Yes, could do bit checks here like the hardware but
	// just treating as uint16 math is simpler to code.
	p.carryCheck(uint16(p.A) + uint16(p.opVal) + uint16(carry))

	// Now set the accumulator so the other flag checks are against the result.
	p.loadRegister(&p.A, sum)
	return true, nil
}

// iASLAcc implements the ASL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Chip) iASLAcc() (bool, error) {
	p.carryCheck(uint16(p.A) << 1)
	p.loadRegister(&p.A, p.A<<1)
	return true, nil
}

// iASL implements the ASL instruction on the given memory location in p.opAddr.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iASL() (bool, error) {
	new := p.opVal << 1
	p.ram.Write(p.opAddr, new)
	p.carryCheck(uint16(p.opVal) << 1)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iBCC implements the BCC instruction and branches if C is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBCC() (bool, error) {
	if p.P&P_CARRY == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBCS implements the BCS instruction and branches if C is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBCS() (bool, error) {
	if p.P&P_CARRY != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBEQ implements the BEQ instruction and branches if Z is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBEQ() (bool, error) {
	if p.P&P_ZERO != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBIT implements the BIT instruction for AND'ing against A
// and setting N/V based on the value.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iBIT() (bool, error) {
	p.zeroCheck(p.A & p.opVal)
	p.negativeCheck(p.opVal)
	// Copy V from bit 6
	p.P &^= P_OVERFLOW
	if p.opVal&P_OVERFLOW != 0x00 {
		p.P |= P_OVERFLOW
	}
	return true, nil
}

// iBMI implements the BMI instructions and branches if N is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBMI() (bool, error) {
	if p.P&P_NEGATIVE != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBNE implements the BNE instructions and branches if Z is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBNE() (bool, error) {
	if p.P&P_ZERO == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBPL implements the BPL instructions and branches if N is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBPL() (bool, error) {
	if p.P&P_NEGATIVE == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBRK implements the BRK instruction and sets up and then calls the interrupt
// handler referenced at IRQ_VECTOR (normally).
// Returns true when on the correct PC. Returns error on an invalid tick.
func (p *Chip) iBRK() (bool, error) {
	// Basically this is the same code as an interrupt handler so can change
	// change if interrupt state changes on a per tick basis. i.e. we might
	// push P with P_B set but go to NMI vector on the right timing.
	// PC comes from IRQ_VECTOR normally unless we've raised an NMI
	vec := IRQ_VECTOR
	if p.irqRaised == kIRQ_NMI {
		vec = NMI_VECTOR
	}
	itr := false
	if p.irqRaised != kIRQ_NONE {
		itr = true
	}
	done, err := p.runInterrupt(vec, itr)
	if done {
		// Eat any pending interrupt since BRK is special.
		p.irqRaised = kIRQ_NONE
	}
	return done, err
}

// iBVC implements the BVC instructions and branches if V is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBVC() (bool, error) {
	if p.P&P_OVERFLOW == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBVS implements the BVS instructions and branches if V is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Chip) iBVS() (bool, error) {
	if p.P&P_OVERFLOW != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// compare implements the logic for all CMP/CPX/CPY instructions and
// sets flags accordingly from the results.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) compare(reg uint8, val uint8) (bool, error) {
	p.zeroCheck(reg - val)
	p.negativeCheck(reg - val)
	// A-M done as 2's complement addition by ones complement and add 1
	// This way we get valid sign extension and a carry bit test.
	p.carryCheck(uint16(reg) + uint16(^val) + uint16(1))
	return true, nil
}

// compareA is a curried version of compare that references A and uses p.opVal for the value.
// This way it can be used as the opFunc in loadInstruction.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) compareA() (bool, error) {
	return p.compare(p.A, p.opVal)
}

// compareX is a curried version of compare that references X and uses p.opVal for the value.
// This way it can be used as the opFunc in loadInstruction.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) compareX() (bool, error) {
	return p.compare(p.X, p.opVal)
}

// compareY is a curried version of compare that references Y and uses p.opVal for the value.
// This way it can be used as the opFunc in loadInstruction.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) compareY() (bool, error) {
	return p.compare(p.Y, p.opVal)
}

// iJMP implments the JMP instruction for jumping to a new address.
// Doesn't use addressing mode functions since it's technically not a load/rmw/store
// instruction so doesn't fit exactly.
// Returns true when the PC is correct. Returns an error on an invalid tick.
func (p *Chip) iJMP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("JMP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// We've already read opVal which is the new PCL so increment the PC for the next tick.
		p.PC++
		return false, nil
	}
	// case p.opTick == 3:
	// Get the next bit of the PC and assemble it.
	v := p.ram.Read(p.PC)
	p.opAddr = (uint16(v) << 8) + uint16(p.opVal)
	p.PC = p.opAddr
	return true, nil
}

// iJMPIndirect implements the indirect JMP instruction for jumping through a pointer to a new address.
// Assumes address is in p.opAddr correctly.
// Returns true when the PC is correct. Returns an error on an invalid tick.
func (p *Chip) iJMPIndirect() (bool, error) {
	// First 3 ticks are the same as an absolute address
	if p.opTick < 4 {
		return p.addrAbsolute(kLOAD_INSTRUCTION)
	}
	switch {
	case (p.cpuType != CPU_CMOS && p.opTick > 5) || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("iJMPIndirect invalid opTick: %d", p.opTick)}
	case p.opTick == 4:
		// Read the low byte of the pointer and stash it in opVal
		p.opVal = p.ram.Read(p.opAddr)
		return false, nil
	case p.opTick == 5:
		// Read the high byte. On NMOS and CMOS this tick reads the wrong address if there was a page wrap.
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0xFF)+1)
		v := p.ram.Read(a)
		if p.cpuType == CPU_CMOS {
			// Just do a normal +1 now for CMOS so tick 6 reads the correct address no matter what.
			// It may be a duplicate of this but that's fine.
			p.opAddr += 1
			return false, nil
		}
		p.opAddr = (uint16(v) << 8) + uint16(p.opVal)
		p.PC = p.opAddr
		return true, nil
	}
	// case p.opTick == 6:
	v := p.ram.Read(p.opAddr)
	p.opAddr = (uint16(v) << 8) + uint16(p.opVal)
	p.PC = p.opAddr
	return true, nil
}

// iJSR implments the JSR instruction for jumping to a subroutine.
// Returns true when the PC is correct. Returns an error on an invalid tick.
func (p *Chip) iJSR() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("JSR invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing happens here except to make the PC correct.
		// NOTE: This means the PC pushed below is actually pointing in the middle of
		//       the address. RTS handles this by adding one to the popped PC value.
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Not 100% sure what happens on this cycle.
		// Per http://nesdev.com/6502_cpu.txt we read the current stack
		// value because there needs to be a tick to make S correct.
		p.S--
		_ = p.popStack()
		return false, nil
	case p.opTick == 4:
		p.pushStack(uint8((p.PC & 0xFF00) >> 8))
		return false, nil
	case p.opTick == 5:
		p.pushStack(uint8(p.PC & 0xFF))
		return false, nil
	}
	// case p.opTick == 6:
	p.PC = (uint16(p.ram.Read(p.PC)) << 8) + uint16(p.opVal)
	return true, nil
}

// iLSRAcc implements the LSR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Chip) iLSRAcc() (bool, error) {
	// Get bit0 from A but in a 16 bit value and then shift it up into
	// the carry position
	p.carryCheck(uint16(p.A&0x01) << 8)
	p.loadRegister(&p.A, p.A>>1)
	return true, nil
}

// iLSR implements the LSR instruction on p.opAddr.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iLSR() (bool, error) {
	new := p.opVal >> 1
	p.ram.Write(p.opAddr, new)
	// Get bit0 from orig but in a 16 bit value and then shift it up into
	// the carry position
	p.carryCheck(uint16(p.opVal&0x01) << 8)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iPHA implements the PHA instruction and pushs X onto the stack.
// Returns true when done. Returns error on an invalid tick.
func (p *Chip) iPHA() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("PHA invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	}
	// case p.opTick == 3:
	p.pushStack(p.A)
	return true, nil
}

// iPLA implements the PLA instruction and pops the stock into the accumulator.
// Returns true when done. Returns error on an invalid tick.
func (p *Chip) iPLA() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 4:
		return true, InvalidCPUState{fmt.Sprintf("PLA invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		// A read of the current stack happens while the CPU is incrementing S.
		// Since our popStack does both of these together on this cycle it's just
		// a throw away read.
		p.S--
		_ = p.popStack()
		return false, nil
	}
	// case p.opTick == 4:
	// The real read
	p.loadRegister(&p.A, p.popStack())
	return true, nil
}

// iPHP implements the PHP instructions for pushing P onto the stacks.
// Returns true when done. Returns error on an invalid tick.
func (p *Chip) iPHP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("PHP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	}
	// case p.opTick == 3:
	push := p.P
	// This bit is always set no matter what.
	push |= P_S1

	// PHP always sets this bit where-as IRQ/NMI won't.
	push |= P_B
	p.pushStack(push)
	return true, nil
}

// iPLP implements the PLP instruction and pops the stack into the flags.
// Returns true when done. Returns error on an invalid tick.
func (p *Chip) iPLP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 4:
		return true, InvalidCPUState{fmt.Sprintf("PLP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		// A read of the current stack happens while the CPU is incrementing S.
		// Since our popStack does both of these together on this cycle it's just
		// a throw away read.
		p.S--
		_ = p.popStack()
		return false, nil
	}
	// case p.opTick == 4:
	// The real read
	p.P = p.popStack()
	// The actual flags register always has S1 set to one
	p.P |= P_S1
	// And the B bit is never set in the register
	p.P &^= P_B
	return true, nil
}

// iROLAcc implements the ROL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Chip) iROLAcc() (bool, error) {
	carry := p.P & P_CARRY
	p.carryCheck(uint16(p.A) << 1)
	p.loadRegister(&p.A, (p.A<<1)|carry)
	return true, nil
}

// iROL implements the ROL instruction on p.opAddr.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iROL() (bool, error) {
	carry := p.P & P_CARRY
	new := (p.opVal << 1) | carry
	p.ram.Write(p.opAddr, new)
	p.carryCheck(uint16(p.opVal) << 1)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iRORAcc implements the ROR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Chip) iRORAcc() (bool, error) {
	carry := (p.P & P_CARRY) << 7
	// Just see if carry is set or not.
	p.carryCheck((uint16(p.A) << 8) & 0x0100)
	p.loadRegister(&p.A, (p.A>>1)|carry)
	return true, nil
}

// iROR implements the ROR instruction on p.opAddr.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iROR() (bool, error) {
	carry := (p.P & P_CARRY) << 7
	new := (p.opVal >> 1) | carry
	p.ram.Write(p.opAddr, new)
	// Just see if carry is set or not.
	p.carryCheck((uint16(p.opVal) << 8) & 0x0100)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iRTI implements the RTI instruction and pops the flags and PC off the stack for returning from an interrupt.
// Returns true when done. Returns error on an invalid tick.
func (p *Chip) iRTI() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("RTI invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		// A read of the current stack happens while the CPU is incrementing S.
		// Since our popStack does both of these together on this cycle it's just
		// a throw away read.
		p.S--
		_ = p.popStack()
		return false, nil
	case p.opTick == 4:
		// The real read for P
		p.P = p.popStack()
		// The actual flags register always has S1 set to one
		p.P |= P_S1
		// And the B bit is never set in the register
		p.P &^= P_B
		return false, nil
	case p.opTick == 5:
		// PCL
		p.opVal = p.popStack()
		return false, nil
	}
	// case p.opTick == 6:
	// PCH
	p.PC = (uint16(p.popStack()) << 8) + uint16(p.opVal)
	return true, nil
}

// iRTS implements the RTS instruction and pops the PC off the stack adding one to it.
func (p *Chip) iRTS() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("RTS invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		// A read of the current stack happens while the CPU is incrementing S.
		// Since our popStack does both of these together on this cycle it's just
		// a throw away read.
		p.S--
		_ = p.popStack()
		return false, nil
	case p.opTick == 4:
		// PCL
		p.opVal = p.popStack()
		return false, nil
	case p.opTick == 5:
		// PCH
		p.PC = (uint16(p.popStack()) << 8) + uint16(p.opVal)
		return false, nil
	}
	// case p.opTick == 6:
	// Read the current PC and then get it incremented for the next instruction.
	_ = p.ram.Read(p.PC)
	p.PC++
	return true, nil
}

// iSBC implements the SBC instruction for both binary and BCD modes (if implemented) and sets all associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSBC() (bool, error) {
	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.cpuType != CPU_NMOS_RICOH {
		// Pull the carry bit out which thankfully is the low bit so can be
		// used directly.
		carry := p.P & P_CARRY

		// BCD details - http://6502.org/tutorials/decimal_mode.html
		// Also http://nesdev.com/6502_cpu.txt but it has errors
		aL := int8(p.A&0x0F) - int8(p.opVal&0x0F) + int8(carry) - 1
		// Low nibble fixup
		if aL < 0 {
			aL = ((aL - 0x06) & 0x0F) - 0x10
		}
		sum := int16(p.A&0xF0) - int16(p.opVal&0xF0) + int16(aL)
		// High nibble fixup
		if sum < 0x0000 {
			sum -= 0x60
		}
		res := uint8(sum & 0xFF)

		// Do normal binary math to set C,N,Z
		b := p.A + ^p.opVal + carry
		p.overflowCheck(p.A, ^p.opVal, b)
		p.negativeCheck(b)
		// Yes, could do bit checks here like the hardware but
		// just treating as uint16 math is simpler to code.
		p.carryCheck(uint16(p.A) + uint16(^p.opVal) + uint16(carry))
		p.zeroCheck(b)
		p.A = res
		return true, nil
	}

	// Otherwise binary mode is just ones complement p.opVal and ADC.
	p.opVal = ^p.opVal
	return p.iADC()
}

// iALR implements the undocumented opcode for ALR. This does AND #i (p.opVal) and then LSR setting all associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iALR() (bool, error) {
	p.loadRegister(&p.A, p.A&p.opVal)
	return p.iLSRAcc()
}

// iANC implements the undocumented opcode for ANC. This does AND #i (p.opVal) and then sets carry based on bit 7 (sign extend).
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iANC() (bool, error) {
	p.loadRegister(&p.A, p.A&p.opVal)
	p.carryCheck(uint16(p.A) << 1)
	return true, nil
}

// iARR implements the undocumented opcode for ARR. This does AND #i (p.opVal) and then ROR except some flags are set differently.
// Implemented as described in http://nesdev.com/6502_cpu.txt
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iARR() (bool, error) {
	t := p.A & p.opVal
	p.loadRegister(&p.A, t)
	p.iRORAcc()
	// Flags are different based on BCD or not (since the ALU acts different).
	if p.P&P_DECIMAL != 0x00 {
		// If bit 6 changed state between AND output and rotate outut then set V.
		if (t^p.A)&0x40 != 0x00 {
			p.P |= P_OVERFLOW
		} else {
			p.P &^= P_OVERFLOW
		}
		// Now do possible odd BCD fixups and set C
		ah := t >> 4
		al := t & 0x0F
		if (al + (al & 0x01)) > 5 {
			p.A = (p.A & 0xF0) | ((p.A + 6) & 0x0F)
		}
		if (ah + (ah & 1)) > 5 {
			p.P |= P_CARRY
			p.A += 0x60
		} else {
			p.P &^= P_CARRY
		}
		return true, nil
	}
	// C is bit 6
	p.carryCheck((uint16(p.A) << 2) & 0x0100)
	// V is bit 5 ^ bit 6
	if ((p.A&0x40)>>6)^((p.A&0x20)>>5) != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
	return true, nil
}

// iAXS implements the undocumented opcode for AXS. (A AND X) - p.opVal (no borrow) setting all associated flags post SBC.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iAXS() (bool, error) {
	// Save A off to restore later
	a := p.A
	p.loadRegister(&p.A, p.A&p.X)
	// Carry is always set
	p.P |= P_CARRY
	// Save D & V state since it's always ignored for this but needs to keep values.
	d := p.P & P_DECIMAL
	v := p.P & P_OVERFLOW
	// Clear D so SBC never uses BCD mode (we'll reset it later from saved state).
	p.P &^= P_DECIMAL
	p.iSBC()
	// Clear V now in case SBC set it so we can properly restore it below.
	p.P &^= P_OVERFLOW
	// Save A in a temp so we can load registers in the right order to set flags (based on X, not old A)
	x := p.A
	p.loadRegister(&p.A, a)
	p.loadRegister(&p.X, x)
	// Restore D & V from our initial state.
	p.P |= d | v
	return true, nil
}

// iLAX implements the undocumented opcode for LAX. This loads A and X with the same value and sets all associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iLAX() (bool, error) {
	p.loadRegister(&p.A, p.opVal)
	p.loadRegister(&p.X, p.opVal)
	return true, nil
}

// iDCP implements the undocumented opcode for DCP. This decrements p.opAddr and then does a CMP with A setting associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iDCP() (bool, error) {
	p.opVal -= 1
	p.ram.Write(p.opAddr, p.opVal)
	return p.compareA()
}

// iISC implements the undocumented opcode for ISC. This increments the value at p.opAddr and then does an SBC with setting associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iISC() (bool, error) {
	p.opVal += 1
	p.ram.Write(p.opAddr, p.opVal)
	return p.iSBC()
}

// iSLO implements the undocumented opcode for SLO. This does an ASL on p.opAddr and then OR's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSLO() (bool, error) {
	p.ram.Write(p.opAddr, p.opVal<<1)
	p.carryCheck(uint16(p.opVal) << 1)
	p.loadRegister(&p.A, (p.opVal<<1)|p.A)
	return true, nil
}

// iRLA implements the undocumented opcode for RLA. This does a ROL on p.opAddr address and then AND's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iRLA() (bool, error) {
	n := p.opVal<<1 | (p.P & P_CARRY)
	p.ram.Write(p.opAddr, n)
	p.carryCheck(uint16(p.opVal) << 1)
	p.loadRegister(&p.A, n&p.A)
	return true, nil
}

// iSRE implements the undocumented opcode for SRE. This does a LSR on p.opAddr and then EOR's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSRE() (bool, error) {
	p.ram.Write(p.opAddr, p.opVal>>1)
	// Old bit 0 becomes carry
	p.carryCheck(uint16(p.opVal) << 8)
	p.loadRegister(&p.A, (p.opVal>>1)^p.A)
	return true, nil
}

// iRRA implements the undocumented opcode for RRA. This does a ROR on p.opAddr and then ADC's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iRRA() (bool, error) {
	n := ((p.P & P_CARRY) << 7) | p.opVal>>1
	p.ram.Write(p.opAddr, n)
	// Old bit 0 becomes carry
	p.carryCheck((uint16(p.opVal) << 8) & 0x0100)
	p.opVal = n
	return p.iADC()
}

// iXAA implements the undocumented opcode for XAA. We'll go with http://visual6502.org/wiki/index.php?title=6502_Opcode_8B_(XAA,_ANE)
// for implementation and pick 0xEE as the constant. According to VICE this may break so might need to change it to 0xFF
// https://sourceforge.net/tracker/?func=detail&aid=2110948&group_id=223021&atid=1057617
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iXAA() (bool, error) {
	p.loadRegister(&p.A, (p.A|0xEE)&p.X&p.opVal)
	return true, nil
}

// iOAL implements the undocumented opcode for OAL. This one acts a bit randomly. It somtimes does XAA and sometimes
// does A=X=A&val.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iOAL() (bool, error) {
	if rand.Float32() >= 0.5 {
		return p.iXAA()
	}
	v := p.A & p.opVal
	p.loadRegister(&p.A, v)
	p.loadRegister(&p.X, v)
	return true, nil
}

// store implements the STA/STX/STY instruction for storing a value (from a register) in RAM.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) store(val uint8, addr uint16) (bool, error) {
	p.ram.Write(addr, val)
	return true, nil
}

// storeWithFlags stores the val to the given addr and also sets Z/N flags accordingly.
// Generally used to implmenet INC/DEC.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) storeWithFlags(val uint8, addr uint16) (bool, error) {
	p.zeroCheck(val)
	p.negativeCheck(val)
	return p.store(val, addr)
}

// iCLV implements the CLV instruction clearing the V status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iCLV() (bool, error) {
	p.P &^= P_OVERFLOW
	return true, nil
}

// iCLD implements the CLD instruction clearing the D status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iCLD() (bool, error) {
	p.P &^= P_DECIMAL
	return true, nil
}

// iCLC implements the CLC instruction clearing the C status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iCLC() (bool, error) {
	p.P &^= P_CARRY
	return true, nil
}

// iCLI implements the CLI instruction clearing the I status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iCLI() (bool, error) {
	p.P &^= P_INTERRUPT
	return true, nil
}

// iSED implements the SED instruction setting the D status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSED() (bool, error) {
	p.P |= P_DECIMAL
	return true, nil
}

// iSEC implements the SEC instruction setting the C status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSEC() (bool, error) {
	p.P |= P_CARRY
	return true, nil
}

// iSEI implements the SEI instruction setting the I status bit.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iSEI() (bool, error) {
	p.P |= P_INTERRUPT
	return true, nil
}

// iORA implements the ORA instruction which ORs p.opVal with A.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iORA() (bool, error) {
	return p.loadRegister(&p.A, p.A|p.opVal)
}

// iAND implements the AND instruction which ANDs p.opVal with A.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iAND() (bool, error) {
	return p.loadRegister(&p.A, p.A&p.opVal)
}

// iEOR implements the EOR instruction which EORs p.opVal with A.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iEOR() (bool, error) {
	return p.loadRegister(&p.A, p.A^p.opVal)
}

// iDEC implements the DEC instruction by decrementing the value (p.opVal) at p.opAddr.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iDEC() (bool, error) {
	return p.storeWithFlags(p.opVal-1, p.opAddr)
}

// iINC implements the INC instruction by incrementing the value (p.opVal) at p.opAddr.
// Always returns true since this takes one tick and never returns an error.
func (p *Chip) iINC() (bool, error) {
	return p.storeWithFlags(p.opVal+1, p.opAddr)
}

// iAHX implements the undocumented AHX instruction based on the addressing mode passed in.
// The value stored is (A & X & (ADDR_HI + 1))
// Returns true when complete and any error.
func (p *Chip) iAHX(addrFunc func(instructionMode) (bool, error)) (bool, error) {
	// This is a store but we can't use storeInstruction since it depends on knowing p.opAddr
	// for the final computed value so we have to do the addressing mode ourselves.
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kSTORE_INSTRUCTION)
		return false, err
	}
	val := p.A & p.X & uint8((p.opAddr>>8)+1)
	return p.store(val, p.opAddr)
}

// iSHY implements the undocumented AHX instruction based on the addressing mode passed in.
// The value stored is (Y & (ADDR_HI + 1))
// Returns true when complete and any error.
func (p *Chip) iSHY(addrFunc func(instructionMode) (bool, error)) (bool, error) {
	// This is a store but we can't use storeInstruction since it depends on knowing p.opAddr
	// for the final computed value so we have to do the addressing mode ourselves.
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kSTORE_INSTRUCTION)
		return false, err
	}
	val := p.Y & uint8((p.opAddr>>8)+1)
	return p.store(val, p.opAddr)
}

// iSHX implements the undocumented AHX instruction based on the addressing mode passed in.
// The value stored is (X & (ADDR_HI + 1))
// Returns true when complete and any error.
func (p *Chip) iSHX(addrFunc func(instructionMode) (bool, error)) (bool, error) {
	// This is a store but we can't use storeInstruction since it depends on knowing p.opAddr
	// for the final computed value so we have to do the addressing mode ourselves.
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kSTORE_INSTRUCTION)
		return false, err
	}
	val := p.X & uint8((p.opAddr>>8)+1)
	return p.store(val, p.opAddr)
}

// iTAS implements the undocumented TAS instruction which only has one addressing more.
// This does the same operations as AHX above but then also sets S = A&X
// Returns true when complete and any error.
func (p *Chip) iTAS() (bool, error) {
	p.S = p.A & p.X
	return p.iAHX(p.addrAbsoluteY)
}

// iLAS implements the undocumented LAS instruction.
// This take opVal and ANDs it with S and then stores that in A,X,S setting flags accordingly.
// Always returns true because it cannot error.
func (p *Chip) iLAS() (bool, error) {
	p.S = p.S & p.opVal
	p.loadRegister(&p.X, p.S)
	p.loadRegister(&p.A, p.S)
	return true, nil
}

// loadInstruction abstracts all load instruction opcodes. The address mode function is used to get the proper values loaded into p.opAddr and p.opVal.
// Then on the same tick this is done the opFunc is called to load the appropriate register.
// Returns true when complete and any error.
func (p *Chip) loadInstruction(addrFunc func(instructionMode) (bool, error), opFunc func() (bool, error)) (bool, error) {
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kLOAD_INSTRUCTION)
	}
	if err != nil {
		return true, err
	}
	if p.addrDone {
		return opFunc()
	}
	return false, nil
}

// rmwInstruction abstracts all rmw instruction opcodes. The address mode function is used to get the proper values loaded into p.opAddr and p.opVal.
// This assumes the address mode function also handle the extra write rmw instructions perform.
// Then on the next tick the opFunc is called to perform the final write operation.
// Returns true when complete and any error.
func (p *Chip) rmwInstruction(addrFunc func(instructionMode) (bool, error), opFunc func() (bool, error)) (bool, error) {
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kRMW_INSTRUCTION)
		return false, err
	}
	return opFunc()
}

// storeInstruction abstracts all store instruction opcodes. The address mode function is used to get the proper values loaded into p.opAddr and p.opVal.
// Then on the next tick the val passed is stored to p.opAddr.
// Returns true when complete and any error.
func (p *Chip) storeInstruction(addrFunc func(instructionMode) (bool, error), val uint8) (bool, error) {
	var err error
	if !p.addrDone {
		p.addrDone, err = addrFunc(kSTORE_INSTRUCTION)
		return false, err
	}
	return p.store(val, p.opAddr)
}
