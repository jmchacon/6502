// Package cpu defines the 6502 architecture and provides
// the methods needed to run the CPU and interface with it
// for emulation.
package cpu

import (
	"fmt"
	"math/rand"

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

type Processor struct {
	A                 uint8   // Accumulator register
	X                 uint8   // X register
	Y                 uint8   // Y register
	S                 uint8   // Stack pointer
	P                 uint8   // Processor status register
	PC                uint16  // Program counter
	CPUType           CPUType // Must be between UNIMPLEMENTED and MAX from above.
	Ram               memory.Ram
	op                uint8   // The current working opcode
	opVal             uint8   // The 1st byte argument after the opcode (all instructions have this).
	opTick            int     // Tick number for internal operation of opcode.
	opAddr            uint16  // Address computed during opcode to be used for read/write (indirect, etc modes).
	opDone            bool    // Stays false until the current opcode has completed all ticks.
	addrDone          bool    // Stays false until the current opcode has completed any addressing mode ticks.
	skipInterrupt     bool    // Skip interrupt processing on the next instruction.
	prevSkipInterrupt bool    // Previous instruction skipped interrupt processing (so we shouldn't).
	irqRaised         irqType // Must be between UNIMPLEMENTED and MAX from above.
	runningInterrupt  bool    // Whether we're running an interrupt setup or an opcode.
	halted            bool    // If stopped due to a halt instruction
	haltOpcode        uint8   // Opcode that caused the halt
}

// A few custom error types to distinguish why the CPU stopped

// UnimplementedOpcode represents a currently unimplmented opcode in the emulator.
type UnimplementedOpcode struct {
	Opcode uint8
}

// Error implements the interface for error types.
func (e UnimplementedOpcode) Error() string {
	return fmt.Sprintf("0x%.2X is an unimplemented opcode", e.Opcode)
}

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

// Init will create a new CPU of the type requested and return it in powered on state.
// The memory passed in will also be powered on.
func Init(cpu CPUType, r memory.Ram) (*Processor, error) {
	if cpu <= CPU_UNIMPLMENTED || cpu >= CPU_MAX {
		return nil, fmt.Errorf("CPU type valid %d is invalid", cpu)
	}
	p := &Processor{
		CPUType: cpu,
		Ram:     r,
	}
	p.Ram.PowerOn()
	p.PowerOn()
	return p, nil
}

// PowerOn will reset the CPU to specific power on state. Registers are zero, stack is at 0xFD
// and P is cleared with interrupts disabled. The starting PC value is loaded from the reset
// vector.
func (p *Processor) PowerOn() {
	p.A = 0
	p.X = 0
	p.Y = 0
	p.S = 0x0
	// This bit is always set.
	p.P = P_S1
	p.Reset()
}

// Reset is similar to PowerOn except the main registers are not touched. The stack is moved
// 3 bytes as if PC/P have been pushed. Flags are not disturbed except for interrupts being disabled
// and the PC is loaded from the reset vector. This takes 6 cycles once triggered.
// TODO(jchacon): Implement correctly as a tick version.
func (p *Processor) Reset() {
	// Most registers unaffected but stack acts like PC/P have been pushed so decrement by 3 bytes.
	p.S -= 3
	// Disable interrupts
	p.P |= P_INTERRUPT
	// Load PC from reset vector
	p.PC = p.Ram.ReadAddr(RESET_VECTOR)
	p.halted = false
	p.haltOpcode = 0x00
	p.opTick = 0
	p.irqRaised = kIRQ_NONE
}

// Tick runs a clock cycle through the CPU which may execute a new instruction or may be finishing
// an existing one. True is returned if the current instruction has finished.
// An error is returned if the instruction isn't implemented or otherwise halts the CPU.
// For an NMOS cpu on a taken branch and an interrupt coming in immediately after will cause one
// more instruction to be executed before the first interrupt instruction. This is accounted
// for by executing this instruction before handling the interrupt (which is cached).
func (p *Processor) Tick(irq bool, nmi bool) (bool, error) {
	if p.irqRaised < kIRQ_NONE || p.irqRaised >= kIRQ_MAX {
		return true, InvalidCPUState{fmt.Sprintf("p.irqRaised is invalid: %d", p.irqRaised)}
	}
	// Fast path if halted. The PC won't advance. i.e. we just keep returning the same error.
	if p.halted {
		return true, HaltOpcode{p.haltOpcode}
	}

	// Increment up front so we're not zero based per se. i.e. each new instruction then
	// starts at opTick == 1.
	p.opTick++

	if irq || nmi {
		p.irqRaised = kIRQ_IRQ
		if nmi {
			p.irqRaised = kIRQ_NMI
		}
	}

	switch {
	case p.opTick == 1:
		// If opTick is 1 it means we're starting a new instruction based on the PC value so grab the opcode now.
		p.op = p.Ram.Read(p.PC)

		// Reset done state
		p.opDone = false
		p.addrDone = false

		// PC always advances on every opcode start except IRQ/HMI (unless we're skipping to run one more instruction).
		if p.irqRaised == kIRQ_NONE {
			p.PC++
			p.runningInterrupt = false
		}
		if p.irqRaised != kIRQ_NONE && !p.skipInterrupt {
			p.runningInterrupt = true
		}
		return false, nil
	case p.opTick == 2:
		// All instructions fetch the value after the opcode (though some like BRK/PHP/etc ignore it).
		// We keep it since some instructions such as absolute addr then require getting one
		// more byte. So cache at this stage since we no idea if it's needed.
		// NOTE: the PC doesn't increment here as that's dependent on addressing mode which will handle it.
		p.opVal = p.Ram.Read(p.PC)

		// We've started a new instruction so no longer skipping interrupt processing.
		if p.skipInterrupt {
			p.skipInterrupt = false
			p.prevSkipInterrupt = true
		}
	case p.opTick > 8:
		// This is impossible on a 65XX as all instructions take no more than 8 ticks.
		// Technically documented instructions max at 7 ticks but a RMW indirect X/Y will take 8.
		return true, InvalidCPUState{fmt.Sprintf("opTick %d too large (> 8)", p.opTick)}
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
		return true, HaltOpcode{p.op}
	}
	if err != nil {
		// Still consider this a halt since it's an internal precondition check.
		p.haltOpcode = p.op
		p.halted = true
		return true, err
	}
	if p.opDone {
		// So the next tick starts a new instruction
		// It'll handle doing start of instruction reset on state (which includes resetting p.opDone, p.addrDone).
		p.opTick = 0
		p.runningInterrupt = false
	}
	return p.opDone, nil
}

func (p *Processor) processOpcode() (bool, error) {
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

	var err error

	switch p.op {
	case 0x00:
		// BRK
		p.opDone, err = p.iBRK()
	case 0x01:
		// ORA (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x02:
		// HLT
		p.halted = true
	case 0x03:
		// SLO (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x04:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x05:
		// ORA d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x06:
		// ASL d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iASL(p.opVal, p.opAddr)
		}
	case 0x07:
		// SLO d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x08:
		// PHP
		p.opDone, err = p.iPHP()
	case 0x09:
		// ORA #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x0A:
		// ASL
		p.opDone, err = p.iASLAcc()
	case 0x0B:
		// ANC #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iANC(p.opVal, p.opAddr)
		}
	case 0x0C:
		// NOP a
		p.opDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
	case 0x0D:
		// ORA a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x0E:
		// ASL a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iASL(p.opVal, p.opAddr)
		}
	case 0x0F:
		// SLO a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x10:
		// BPL *+r
		p.opDone, err = p.iBPL()
	case 0x11:
		// ORA (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x12:
		// HLT
		p.halted = true
	case 0x13:
		// SLO (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x14:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x15:
		// ORA d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x16:
		// ASL d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iASL(p.opVal, p.opAddr)
		}
	case 0x17:
		// SLO d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x18:
		// CLC
		p.P &^= P_CARRY
		p.opDone, err = true, nil
	case 0x19:
		// ORA a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x1A:
		// NOP
		p.opDone, err = true, nil
	case 0x1B:
		// SLO a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x1C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x1D:
		// ORA a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A|p.opVal)
		}
	case 0x1E:
		// ASL a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iASL(p.opVal, p.opAddr)
		}
	case 0x1F:
		// SLO a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSLO(p.opVal, p.opAddr)
		}
	case 0x20:
		// JSR a
		p.opDone, err = p.iJSR()
	case 0x21:
		// AND (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x22:
		// HLT
		p.halted = true
	case 0x23:
		// RLA (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x24:
		// BIT d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iBIT(p.opVal, p.opAddr)
		}
	case 0x25:
		// AND d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x26:
		// ROL d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROL(p.opVal, p.opAddr)
		}
	case 0x27:
		// RLA d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x28:
		// PLP
		p.opDone, err = p.iPLP()
	case 0x29:
		// AND #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x2A:
		// ROL
		p.opDone, err = p.iROLAcc()
	case 0x2B:
		// ANC #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iANC(p.opVal, p.opAddr)
		}
	case 0x2C:
		// BIT a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iBIT(p.opVal, p.opAddr)
		}
	case 0x2D:
		// AND a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x2E:
		// ROL a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROL(p.opVal, p.opAddr)
		}
	case 0x2F:
		// RLA a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x30:
		// BMI *+r
		p.opDone, err = p.iBMI()
	case 0x31:
		// AND (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x32:
		// HLT
		p.halted = true
	case 0x33:
		// RLA (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x34:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x35:
		// AND d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x36:
		// ROL d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROL(p.opVal, p.opAddr)
		}
	case 0x37:
		// RLA d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x38:
		// SEC
		p.P |= P_CARRY
		p.opDone, err = true, nil
	case 0x39:
		// AND a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x3A:
		// NOP
		p.opDone, err = true, nil
	case 0x3B:
		// RLA a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x3C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x3D:
		// AND a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A&p.opVal)
		}
	case 0x3E:
		// ROL a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROL(p.opVal, p.opAddr)
		}
	case 0x3F:
		// RLA a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRLA(p.opVal, p.opAddr)
		}
	case 0x40:
		// RTI
		p.opDone, err = p.iRTI()
	case 0x41:
		// EOR (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x42:
		// HLT
		p.halted = true
	case 0x43:
		// SRE (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x44:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x45:
		// EOR d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x46:
		// LSR d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iLSR(p.opVal, p.opAddr)
		}
	case 0x47:
		// SRE d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x48:
		// PHA
		p.opDone, err = p.iPHA()
	case 0x49:
		// EOR #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x4A:
		// LSR
		p.opDone, err = p.iLSRAcc()
	case 0x4B:
		// ALR #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iALR(p.opVal, p.opAddr)
		}
	case 0x4C:
		// JMP a
		p.opDone, err = p.iJMP()
	case 0x4D:
		// EOR a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x4E:
		// LSR a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iLSR(p.opVal, p.opAddr)
		}
	case 0x4F:
		// SRE a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x50:
		// BVC *+r
		p.opDone, err = p.iBVC()
	case 0x51:
		// EOR (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x52:
		// HLT
		p.halted = true
	case 0x53:
		// SRE (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x54:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x55:
		// EOR d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x56:
		// LSR d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iLSR(p.opVal, p.opAddr)
		}
	case 0x57:
		// SRE d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x58:
		// CLI
		p.P &^= P_INTERRUPT
		p.opDone, err = true, nil
	case 0x59:
		// EOR a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x5A:
		// NOP
		p.opDone, err = true, nil
	case 0x5B:
		// SRE a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x5C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x5D:
		// EOR a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.A^p.opVal)
		}
	case 0x5E:
		// LSR a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iLSR(p.opVal, p.opAddr)
		}
	case 0x5F:
		// SRE a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iSRE(p.opVal, p.opAddr)
		}
	case 0x60:
		// RTS
		p.opDone, err = p.iRTS()
	case 0x61:
		// ADC (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x62:
		// HLT
		p.halted = true
	case 0x63:
		// RRA (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x64:
		// NOP d
		p.opDone, err = p.addrZP(kLOAD_INSTRUCTION)
	case 0x65:
		// ADC d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x66:
		// ROR d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROR(p.opVal, p.opAddr)
		}
	case 0x67:
		// RRA d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x68:
		// PLA
		p.opDone, err = p.iPLA()
	case 0x69:
		// ADC #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x6A:
		// ROR
		p.opDone, err = p.iRORAcc()
	case 0x6B:
		// ARR #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iARR(p.opVal, p.opAddr)
		}
	case 0x6C:
		// JMP (a)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirect(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.PC, p.opDone, err = p.opAddr, true, nil
		}
	case 0x6D:
		// ADC a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x6E:
		// ROR a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROR(p.opVal, p.opAddr)
		}
	case 0x6F:
		// RRA a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x70:
		// BVS *+r
		p.opDone, err = p.iBVS()
	case 0x71:
		// ADC (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x72:
		// HLT
		p.halted = true
	case 0x73:
		// RRA (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x74:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0x75:
		// ADC d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x76:
		// ROR d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROR(p.opVal, p.opAddr)
		}
	case 0x77:
		// RRA d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x78:
		// SEI
		p.P |= P_INTERRUPT
		p.opDone, err = true, nil
	case 0x79:
		// ADC a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x7A:
		// NOP
		p.opDone, err = true, nil
	case 0x7B:
		// RRA a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x7C:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0x7D:
		// ADC a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iADC(p.opVal, p.opAddr)
		}
	case 0x7E:
		// ROR a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iROR(p.opVal, p.opAddr)
		}
	case 0x7F:
		// RRA a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iRRA(p.opVal, p.opAddr)
		}
	case 0x80:
		// NOP #i
		p.opDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
	case 0x81:
		// STA (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x82:
		// NOP #i
		p.opDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
	case 0x83:
		// SAX (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A&p.X, p.opAddr)
		}
	case 0x84:
		// STY d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.Y, p.opAddr)
		}
	case 0x85:
		// STA d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x86:
		// STX d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.X, p.opAddr)
		}
	case 0x87:
		// SAX d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A&p.X, p.opAddr)
		}
	case 0x88:
		// DEY
		p.opDone, err = p.loadRegister(&p.Y, p.Y-1)
	case 0x89:
		// NOP #i
		p.opDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
	case 0x8A:
		// TXA
		p.opDone, err = p.loadRegister(&p.A, p.X)
	case 0x8B:
		// XAA #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iXXA(p.opVal, p.opAddr)
		}
	case 0x8C:
		// STY a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.Y, p.opAddr)
		}
	case 0x8D:
		// STA a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x8E:
		// STX a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.X, p.opAddr)
		}
	case 0x8F:
		// SAX a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A&p.X, p.opAddr)
		}
	case 0x90:
		// BCC *+d
		p.opDone, err = p.iBCC()
	case 0x91:
		// STA (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x92:
		// HLT
		p.halted = true
	case 0x94:
		// STY d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.Y, p.opAddr)
		}
	case 0x95:
		// STA d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x96:
		// STX d,y
		if !p.addrDone {
			p.addrDone, err = p.addrZPY(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.X, p.opAddr)
		}
	case 0x97:
		// SAX d,y
		if !p.addrDone {
			p.addrDone, err = p.addrZPY(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A&p.X, p.opAddr)
		}
	case 0x98:
		// TYA
		p.opDone, err = p.loadRegister(&p.A, p.Y)
	case 0x99:
		// STA a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0x9A:
		// TXS
		p.opDone, err, p.S = true, nil, p.X
	case 0x9D:
		// STA a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kSTORE_INSTRUCTION)
		} else {
			p.opDone, err = p.store(p.A, p.opAddr)
		}
	case 0xA0:
		// LDY #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.Y, p.opVal)
		}
	case 0xA1:
		// LDA (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xA2:
		// LDX #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.X, p.opVal)
		}
	case 0xA3:
		// LAX (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xA4:
		// LDY d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.Y, p.opVal)
		}
	case 0xA5:
		// LDA d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xA6:
		// LDX d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.X, p.opVal)
		}
	case 0xA7:
		// LAX d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xA8:
		// TAY
		p.opDone, err = p.loadRegister(&p.Y, p.A)
	case 0xA9:
		// LDA #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xAA:
		// TAX
		p.opDone, err = p.loadRegister(&p.X, p.A)
	case 0xAB:
		// OAL #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iOAL(p.opVal, p.opAddr)
		}
	case 0xAC:
		// LDY a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.Y, p.opVal)
		}
	case 0xAD:
		// LDA a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xAE:
		// LDX a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.X, p.opVal)
		}
	case 0xAF:
		// LAX a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xB0:
		// BCS *+d
		p.opDone, err = p.iBCS()
	case 0xB1:
		// LDA (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xB2:
		// HLT
		p.halted = true
	case 0xB3:
		// LAX (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xB4:
		// LDY d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.Y, p.opVal)
		}
	case 0xB5:
		// LDA d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xB6:
		// LDX d,y
		if !p.addrDone {
			p.addrDone, err = p.addrZPY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.X, p.opVal)
		}
	case 0xB7:
		// LAX d,y
		if !p.addrDone {
			p.addrDone, err = p.addrZPY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xB8:
		// CLV
		p.P &^= P_OVERFLOW
		p.opDone, err = true, nil
	case 0xB9:
		// LDA a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xBA:
		// TSX
		p.opDone, err = p.loadRegister(&p.X, p.S)
	case 0xBC:
		// LDY a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.Y, p.opVal)
		}
	case 0xBD:
		// LDA a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.A, p.opVal)
		}
	case 0xBE:
		// LDX a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {
			p.opDone, err = p.loadRegister(&p.X, p.opVal)
		}
	case 0xBF:
		// LAX a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iLAX(p.opVal, p.opAddr)
		}
	case 0xC0:
		// CPY #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.Y, p.opVal)
		}
	case 0xC1:
		// CMP (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xC2:
		// NOP #i
		p.opDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
	case 0xC3:
		// DCP (d,X)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xC4:
		// CPY d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.Y, p.opVal)
		}
	case 0xC5:
		// CMP d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xC6:
		// DEC d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal-1, p.opAddr)
		}
	case 0xC7:
		// DCP d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xC8:
		// INY
		p.opDone, err = p.loadRegister(&p.Y, p.Y+1)
	case 0xC9:
		// CMP #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xCA:
		// DEX
		p.opDone, err = p.loadRegister(&p.X, p.X-1)
	case 0xCB:
		// AXS #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iAXS(p.opVal, p.opAddr)
		}
	case 0xCC:
		// CPY a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.Y, p.opVal)
		}
	case 0xCD:
		// CMP a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xCE:
		// DEC a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal-1, p.opAddr)
		}
	case 0xCF:
		// DCP a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xD0:
		// BNE *+r
		p.opDone, err = p.iBNE()
	case 0xD1:
		// CMP (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xD2:
		// HLT
		p.halted = true
	case 0xD3:
		// DCP (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xD4:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0xD5:
		// CMP d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xD6:
		// DEC d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal-1, p.opAddr)
		}
	case 0xD7:
		// DCP d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xD8:
		// CLD
		p.P &^= P_DECIMAL
		p.opDone, err = true, nil
	case 0xD9:
		// CMP a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xDA:
		// NOP
		p.opDone, err = true, nil
	case 0xDB:
		// DCP a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xDC:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0xDD:
		// CMP a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.A, p.opVal)
		}
	case 0xDE:
		// DEC a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal-1, p.opAddr)
		}
	case 0xDF:
		// DCP a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iDCP(p.opVal, p.opAddr)
		}
	case 0xE0:
		// CPX #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.X, p.opVal)
		}
	case 0xE1:
		// SBC (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xE2:
		// NOP #i
		p.opDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
	case 0xE3:
		// ISC (d,x)
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xE4:
		// CPX d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.X, p.opVal)
		}
	case 0xE5:
		// SBC d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xE6:
		// INC d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal+1, p.opAddr)
		}
	case 0xE7:
		// ISC d
		if !p.addrDone {
			p.addrDone, err = p.addrZP(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xE8:
		// INX
		p.opDone, err = p.loadRegister(&p.X, p.X+1)
	case 0xE9:
		// SBC #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xEA:
		// NOP
		p.opDone, err = true, nil
	case 0xEB:
		// SBC #i
		if !p.addrDone {
			p.addrDone, err = p.addrImmmediate(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xEC:
		// CPX a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.compare(p.X, p.opVal)
		}
	case 0xED:
		// SBC a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xEE:
		// INC a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal+1, p.opAddr)
		}
	case 0xEF:
		// ISC a
		if !p.addrDone {
			p.addrDone, err = p.addrAbsolute(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xF0:
		// BEQ *+d
		p.opDone, err = p.iBEQ()
	case 0xF1:
		// SBC (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xF2:
		// HLT
		p.halted = true
	case 0xF3:
		// ISC (d),y
		if !p.addrDone {
			p.addrDone, err = p.addrIndirectY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xF4:
		// NOP d,x
		p.opDone, err = p.addrZPX(kLOAD_INSTRUCTION)
	case 0xF5:
		// SBC d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xF6:
		// INC d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal+1, p.opAddr)
		}
	case 0xF7:
		// ISC d,x
		if !p.addrDone {
			p.addrDone, err = p.addrZPX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xF8:
		// SED
		p.P |= P_DECIMAL
		p.opDone, err = true, nil
	case 0xF9:
		// SBC a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xFA:
		// NOP
		p.opDone, err = true, nil
	case 0xFB:
		// ISC a,y
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteY(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	case 0xFC:
		// NOP a,x
		p.opDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
	case 0xFD:
		// SBC a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kLOAD_INSTRUCTION)
		}
		if p.addrDone {

			p.opDone, err = p.iSBC(p.opVal, p.opAddr)
		}
	case 0xFE:
		// INC a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.storeWithFlags(p.opVal+1, p.opAddr)
		}
	case 0xFF:
		// ISC a,x
		if !p.addrDone {
			p.addrDone, err = p.addrAbsoluteX(kRMW_INSTRUCTION)
		} else {
			p.opDone, err = p.iISC(p.opVal, p.opAddr)
		}
	default:
		return true, UnimplementedOpcode{p.op}
	}
	return p.opDone, err
}

// zeroCheck sets the Z flag based on the register contents.
func (p *Processor) zeroCheck(reg uint8) {
	if reg == 0 {
		p.P |= P_ZERO
	} else {
		p.P &^= P_ZERO
	}
}

// negativeCheck sets the N flag based on the register contents.
func (p *Processor) negativeCheck(reg uint8) {
	if (reg & P_NEGATIVE) == 0x80 {
		p.P |= P_NEGATIVE
	} else {
		p.P &^= P_NEGATIVE
	}
}

// carryCheck sets the C flag if the result of an 8 bit ALU operation
// (passed as a 16 bit result) caused a carry out by generating a value >= 0x100.
// NOTE: normally this just means masking 0x100 but in some overflow cases for BCD
//       math the value can be 0x200 here so it's still a carry.g
func (p *Processor) carryCheck(res uint16) {
	if res >= 0x100 {
		p.P |= P_CARRY
	} else {
		p.P &^= P_CARRY
	}
}

// overflowCheck sets the V flag if the result of the ALU operation
// caused a two's complement sign change.
// Taken from http://www.righto.com/2012/12/the-6502-overflow-flag-explained.html
func (p *Processor) overflowCheck(reg uint8, arg uint8, res uint8) {
	// If the originals signs differ from the end sign bit
	if (reg^res)&(arg^res)&0x80 != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
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

// addrImmmediate implements immediate mode - #i
// returning the value in p.opVal
// NOTE: This has no W or RMW mode so the argument is ignored.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrImmmediate(instructionMode) (bool, error) {
	if p.opTick != 2 {
		return true, InvalidCPUState{fmt.Sprintf("addrImmmediate invalid opTick %d, not 2", p.opTick)}
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
func (p *Processor) addrZP(mode instructionMode) (bool, error) {
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
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 4:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrZPX implements Zero page plus X mode - d,x
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrZPX(mode instructionMode) (bool, error) {
	return p.addrZPXY(mode, p.X)
}

func (p *Processor) addrZPXY(mode instructionMode, reg uint8) (bool, error) {
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
		_ = p.Ram.Read(p.opAddr)
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
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 5:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrZPY implements Zero page plus Y mode - d,y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrZPY(mode instructionMode) (bool, error) {
	return p.addrZPXY(mode, p.Y)
}

// addrIndirectX implements Zero page indirect plus X mode - (d,x)
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrIndirectX(mode instructionMode) (bool, error) {
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
		_ = p.Ram.Read(p.opAddr)
		// Does this as a uint8 so it wraps as needed.
		p.opAddr = uint16(uint8(p.opVal + p.X))
		return false, nil
	case p.opTick == 4:
		// Read effective addr low byte.
		p.opVal = p.Ram.Read(p.opAddr)
		// Setup opAddr for next read and handle wrapping
		p.opAddr = uint16(uint8(p.opAddr&0x00FF) + 1)
		return false, nil
	case p.opTick == 5:
		p.opAddr = (uint16(p.Ram.Read(p.opAddr)) << 8) + uint16(p.opVal)
		done := false
		// For a store we're done since we have the address needed.
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 6:
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 7:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrIndirectY implements Zero page indirect plus Y mode - (d),y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrIndirectY(mode instructionMode) (bool, error) {
	switch {
	case p.opTick == 1 || p.opTick > 7:
		return true, InvalidCPUState{fmt.Sprintf("addrIndirectY invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// Already read the value but need to bump the PC
		p.opAddr = uint16(0x00FF & p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Read from the ZP addr to start building our pointer.
		p.opVal = p.Ram.Read(p.opAddr)
		// Setup opAddr for next read and handle wrapping
		p.opAddr = uint16(uint8(p.opAddr&0x00FF) + 1)
		return false, nil
	case p.opTick == 4:
		// Compute effective address and then add Y to it (possibly wrongly).
		p.opAddr = (uint16(p.Ram.Read(p.opAddr)) << 8) + uint16(p.opVal)
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
		p.opVal = p.Ram.Read(p.opAddr)

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
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 7:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrAbsolute implements absolute mode - a
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrAbsolute(mode instructionMode) (bool, error) {
	switch {
	case p.opTick == 1 || p.opTick > 5:
		return true, InvalidCPUState{fmt.Sprintf("addrAbsolute invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// opVal has already been read so start constructing the address
		p.opAddr = 0x00FF & uint16(p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		p.opVal = p.Ram.Read(p.PC)
		p.PC++
		p.opAddr |= (uint16(p.opVal) << 8)
		done := false
		if mode == kSTORE_INSTRUCTION {
			done = true
		}
		return done, nil
	case p.opTick == 4:
		// For load and RMW instructions
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 5:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrAbsoluteX implements absolute plus X mode - a,x
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrAbsoluteX(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("addrAbsoluteX invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// opVal has already been read so start constructing the address
		p.opAddr = 0x00FF & uint16(p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		p.opVal = p.Ram.Read(p.PC)
		p.PC++
		p.opAddr |= (uint16(p.opVal) << 8)
		// Add X but do it in a way which won't page wrap (if needed)
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0x00FF)+p.X)
		p.opVal = 0
		if a != (p.opAddr + uint16(p.X)) {
			// Signal for next phase we got it wrong.
			p.opVal = 1
		}
		p.opAddr = a
		return false, nil
	case p.opTick == 4:
		t := p.opVal
		p.opVal = p.Ram.Read(p.opAddr)
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
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 6:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrAbsoluteY implements absolute plus X mode - a,y
// returning the value in p.opVal and the address read in p.opAddr (so RW operations can do things without having to
// reread memory incorrectly to compute a storage address).
// If mode is RMW then another tick will occur that writes the read value back to the same address due to how
// the 6502 operates.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
// TODO(jchacon): This should be combined with addrAbsoluteX as it only differs by register.
func (p *Processor) addrAbsoluteY(mode instructionMode) (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("addrAbsoluteX invalid opTick: %d", p.opTick)}
	case p.opTick == 2:
		// opVal has already been read so start constructing the address
		p.opAddr = 0x00FF & uint16(p.opVal)
		p.PC++
		return false, nil
	case p.opTick == 3:
		p.opVal = p.Ram.Read(p.PC)
		p.PC++
		p.opAddr |= (uint16(p.opVal) << 8)
		// Add Y but do it in a way which won't page wrap (if needed)
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0x00FF)+p.Y)
		p.opVal = 0
		if a != (p.opAddr + uint16(p.Y)) {
			// Signal for next phase we got it wrong.
			p.opVal = 1
		}
		p.opAddr = a
		return false, nil
	case p.opTick == 4:
		t := p.opVal
		p.opVal = p.Ram.Read(p.opAddr)
		// Check the old opVal to see if it's non-zero. If so it means the Y addition
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
		// Optional (on load) in case adding Y went past a page boundary.
		p.opVal = p.Ram.Read(p.opAddr)
		done := true
		if mode == kRMW_INSTRUCTION {
			done = false
		}
		return done, nil
	case p.opTick == 6:
		p.Ram.Write(p.opAddr, p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// addrIndirect implements indirect mode - (a)
// Has the same signature computed as other addressing modes but only address is needed and mode is ignored.
// Returns error on invalid tick.
// The bool return value is true if this tick ends address processing.
func (p *Processor) addrIndirect(mode instructionMode) (bool, error) {
	// First 3 ticks are the same as an absolute address
	if p.opTick < 4 {
		return p.addrAbsolute(mode)
	}
	// TODO(jchacon): This accounts for CMOS differences but is one of the only instructions currently to do so.
	switch {
	case (p.CPUType != CPU_CMOS && p.opTick > 5) || p.opTick > 6:
		return true, InvalidCPUState{fmt.Sprintf("addrIndirect invalid opTick: %d", p.opTick)}
	case p.opTick == 4:
		// Read the low byte of the pointer and stash it in opVal
		p.opVal = p.Ram.Read(p.opAddr)
		return false, nil
	case p.opTick == 5:
		// Read the high byte. On NMOS and CMOS this tick reads the wrong address if there was a page wrap.
		a := (p.opAddr & 0xFF00) + uint16(uint8(p.opAddr&0xFF)+1)
		v := p.Ram.Read(a)
		if p.CPUType == CPU_CMOS {
			// Just do a normal +1 now for CMOS so tick 6 reads the correct address no matter what.
			// It may be a duplicate of this but that's fine.
			p.opAddr += 1
			return false, nil
		}
		p.opAddr = (uint16(v) << 8) + uint16(p.opVal)
		return true, nil
	case p.opTick == 6:
		v := p.Ram.Read(p.opAddr)
		p.opAddr = (uint16(v) << 8) + uint16(p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// loadRegister takes the val and inserts it into the register passed in. It then does
// Z and N checks against the new value.
// Always returns true and no error since this is a single tick operation.
func (p *Processor) loadRegister(reg *uint8, val uint8) (bool, error) {
	*reg = val
	p.zeroCheck(*reg)
	p.negativeCheck(*reg)
	return true, nil
}

// pushStack pushes the given byte onto the stack and adjusts the stack pointer accordingly.
func (p *Processor) pushStack(val uint8) {
	p.Ram.Write(0x0100+uint16(p.S), val)
	p.S--
}

// popStack pops the top byte off the stack and adjusts the stack pointer accordingly.
func (p *Processor) popStack() uint8 {
	p.S++
	return p.Ram.Read(0x0100 + uint16(p.S))
}

// branchNOP reads the next byte as the branch offset and increments the PC.
// Used for the 2rd tick when branches aren't taken.
func (p *Processor) branchNOP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("branchNOP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		p.PC++
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// performBranch does the heavy lifting for branching by
// computing the new PC and computing appropriate cycle costs.
// It returns true when the instruction is done and error if the tick
// becomes invalid.
func (p *Processor) performBranch() (bool, error) {
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
		_ = p.Ram.Read(p.PC)
		if p.PC == (p.opAddr + uint16(int16(int8(p.opVal)))) {
			return true, nil
		}
		return false, nil
	case p.opTick == 4:
		// Set correct PC value
		p.PC = p.opAddr + uint16(int16(int8(p.opVal)))
		// Always read the next opcode
		_ = p.Ram.Read(p.PC)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

const BRK = uint8(0x00)

// runInterrupt does all the heavy lifting for any interrupt processing.
// i.e. pushing values onto the stack and loading PC with the right address.
// Pass in the vector to be used for loading the PC (which means for BRK
// it can change if an NMI happens before we get to the load ticks).
// Returns true when complete (and PC is correct). Can return an error on an
// invalid tick count.
func (p *Processor) runInterrupt(addr uint16, irq bool) (bool, error) {
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
		if p.CPUType == CPU_CMOS {
			p.P &^= P_DECIMAL
		}
		p.P |= P_INTERRUPT
		p.pushStack(push)
		return false, nil
	case p.opTick == 6:
		p.opVal = p.Ram.Read(addr)
		return false, nil
	case p.opTick == 7:
		p.PC = (uint16(p.Ram.Read(addr+1)) << 8) + uint16(p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iADC implements the ADC/SBC instructions and sets all associated flags.
// For SBC (non BCD) simply ones-complement the arg before calling.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iADC(arg uint8, _ uint16) (bool, error) {
	// Pull the carry bit out which thankfully is the low bit so can be
	// used directly.
	carry := p.P & P_CARRY

	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.CPUType != CPU_NMOS_RICOH {
		// BCD details - http://6502.org/tutorials/decimal_mode.html
		// Also http://nesdev.com/6502_cpu.txt but it has errors
		aL := (p.A & 0x0F) + (arg & 0x0F) + carry
		// Low nibble fixup
		if aL >= 0x0A {
			aL = ((aL + 0x06) & 0x0f) + 0x10
		}
		sum := uint16(p.A&0xF0) + uint16(arg&0xF0) + uint16(aL)
		// High nibble fixup
		if sum >= 0xA0 {
			sum += 0x60
		}
		res := uint8(sum & 0xFF)
		seq := (p.A & 0xF0) + (arg & 0xF0) + aL
		bin := p.A + arg + carry
		p.overflowCheck(p.A, arg, seq)
		p.carryCheck(sum)
		// TODO(jchacon): CMOS gets N/Z set correctly and needs implementing.
		p.negativeCheck(seq)
		p.zeroCheck(bin)
		p.A = res
		return true, nil
	}

	// Otherwise do normal binary math.
	sum := p.A + arg + carry
	p.overflowCheck(p.A, arg, sum)
	// Yes, could do bit checks here like the hardware but
	// just treating as uint16 math is simpler to code.
	p.carryCheck(uint16(p.A) + uint16(arg) + uint16(carry))

	// Now set the accumulator so the other flag checks are against the result.
	p.loadRegister(&p.A, sum)
	return true, nil
}

// iASLAcc implements the ASL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Processor) iASLAcc() (bool, error) {
	p.carryCheck(uint16(p.A) << 1)
	p.loadRegister(&p.A, p.A<<1)
	return true, nil
}

// iASL implements the ASL instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iASL(val uint8, addr uint16) (bool, error) {
	new := val << 1
	p.Ram.Write(addr, new)
	p.carryCheck(uint16(val) << 1)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iBCC implements the BCC instruction and branches if C is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBCC() (bool, error) {
	if p.P&P_CARRY == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBCS implements the BCS instruction and branches if C is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBCS() (bool, error) {
	if p.P&P_CARRY != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBEQ implements the BEQ instruction and branches if Z is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBEQ() (bool, error) {
	if p.P&P_ZERO != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBIT implements the BIT instruction for AND'ing against A
// and setting N/V based on the value.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iBIT(val uint8, _ uint16) (bool, error) {
	p.zeroCheck(p.A & val)
	p.negativeCheck(val)
	// Copy V from bit 6
	if val&P_OVERFLOW != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
	return true, nil
}

// iBMI implements the BMI instructions and branches if N is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBMI() (bool, error) {
	if p.P&P_NEGATIVE != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBNE implements the BNE instructions and branches if Z is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBNE() (bool, error) {
	if p.P&P_ZERO == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBPL implements the BPL instructions and branches if N is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBPL() (bool, error) {
	if p.P&P_NEGATIVE == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBRK implements the BRK instruction and sets up and then calls the interrupt
// handler referenced at IRQ_VECTOR.
// Returns true when on the correct PC. Returns error on an invalid tick.
func (p *Processor) iBRK() (bool, error) {
	// PC comes from IRQ_VECTOR
	return p.runInterrupt(IRQ_VECTOR, false)
}

// iBVC implements the BVC instructions and branches if V is clear.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBVC() (bool, error) {
	if p.P&P_OVERFLOW == 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// iBVS implements the BVS instructions and branches if V is set.
// Returns true when the branch has set the correct PC. Returns error on an invalid tick.
func (p *Processor) iBVS() (bool, error) {
	if p.P&P_OVERFLOW != 0x00 {
		return p.performBranch()
	}
	return p.branchNOP()
}

// compare implements the logic for all CMP/CPX/CPY instructions and
// sets flags accordingly from the results.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) compare(reg uint8, val uint8) (bool, error) {
	p.zeroCheck(reg - val)
	p.negativeCheck(reg - val)
	// A-M done as 2's complement addition by ones complement and add 1
	// This way we get valid sign extension and a carry bit test.
	p.carryCheck(uint16(reg) + uint16(^val) + uint16(1))
	return true, nil
}

// iJMP implments the JMP instruction for jumping to a new address.
// Returns true when the PC is correct. Returns an error on an invalid tick.
func (p *Processor) iJMP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("JMP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// We've already read opVal which is the new PCL so increment the PC for the next tick.
		p.PC++
		return false, nil
	case p.opTick == 3:
		// Get the next bit of the PC and assemble it.
		p.PC = (uint16(p.Ram.Read(p.PC)) << 8) + uint16(p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iJSR implments the JSR instruction for jumping to a subroutine.
// Returns true when the PC is correct. Returns an error on an invalid tick.
func (p *Processor) iJSR() (bool, error) {
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
	case p.opTick == 6:
		p.PC = (uint16(p.Ram.Read(p.PC)) << 8) + uint16(p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iLSRAcc implements the LSR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Processor) iLSRAcc() (bool, error) {
	// Get bit0 from A but in a 16 bit value and then shift it up into
	// the carry position
	p.carryCheck(uint16(p.A&0x01) << 8)
	p.loadRegister(&p.A, p.A>>1)
	return true, nil
}

// iLSR implements the LSR instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iLSR(val uint8, addr uint16) (bool, error) {
	new := val >> 1
	p.Ram.Write(addr, new)
	// Get bit0 from orig but in a 16 bit value and then shift it up into
	// the carry position
	p.carryCheck(uint16(val&0x01) << 8)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iPHA implements the PHA instruction and pushs X onto the stack.
// Returns true when done. Returns error on an invalid tick.
func (p *Processor) iPHA() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("PHA invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		p.pushStack(p.A)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iPLA implements the PLA instruction and pops the stock into the accumulator.
// Returns true when done. Returns error on an invalid tick.
func (p *Processor) iPLA() (bool, error) {
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
	case p.opTick == 4:
		// The real read
		p.loadRegister(&p.A, p.popStack())
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iPHP implements the PHP instructions for pushing P onto the stacks.
// Returns true when done. Returns error on an invalid tick.
func (p *Processor) iPHP() (bool, error) {
	switch {
	case p.opTick <= 1 || p.opTick > 3:
		return true, InvalidCPUState{fmt.Sprintf("PHP invalid opTick %d", p.opTick)}
	case p.opTick == 2:
		// Nothing else happens here
		return false, nil
	case p.opTick == 3:
		push := p.P
		// This bit is always set no matter what.
		push |= P_S1

		// TODO(jchacon): Seems NMOS varieties always push B on with PHP but
		//                unsure on CMOS. Verify
		push |= P_B
		p.pushStack(push)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iPLP implements the PLP instruction and pops the stack into the flags.
// Returns true when done. Returns error on an invalid tick.
func (p *Processor) iPLP() (bool, error) {
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
	case p.opTick == 4:
		// The real read
		p.P = p.popStack()
		// The actual flags register always has S1 set to one
		p.P |= P_S1
		// And the B bit is never set in the register
		p.P &^= P_B
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iROLAcc implements the ROL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Processor) iROLAcc() (bool, error) {
	carry := p.P & P_CARRY
	p.carryCheck(uint16(p.A) << 1)
	p.loadRegister(&p.A, (p.A<<1)|carry)
	return true, nil
}

// iROL implements the ROL instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iROL(val uint8, addr uint16) (bool, error) {
	carry := p.P & P_CARRY
	new := (val << 1) | carry
	p.Ram.Write(addr, new)
	p.carryCheck(uint16(val) << 1)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iRORAcc implements the ROR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since accumulator mode is done on tick 2 and never returns an error.
func (p *Processor) iRORAcc() (bool, error) {
	carry := (p.P & P_CARRY) << 7
	// Just see if carry is set or not.
	p.carryCheck((uint16(p.A) << 8) & 0x0100)
	p.loadRegister(&p.A, (p.A>>1)|carry)
	return true, nil
}

// iROR implements the ROR instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iROR(val uint8, addr uint16) (bool, error) {
	carry := (p.P & P_CARRY) << 7
	new := (val >> 1) | carry
	p.Ram.Write(addr, new)
	// Just see if carry is set or not.
	p.carryCheck((uint16(val) << 8) & 0x0100)
	p.zeroCheck(new)
	p.negativeCheck(new)
	return true, nil
}

// iRTI implements the RTI instruction and pops the flags and PC off the stack for returning from an interrupt.
// Returns true when done. Returns error on an invalid tick.
func (p *Processor) iRTI() (bool, error) {
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
	case p.opTick == 6:
		// PCH
		p.PC = (uint16(p.popStack()) << 8) + uint16(p.opVal)
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iRTS implements the RTS instruction and pops the PC off the stack adding one to it.
func (p *Processor) iRTS() (bool, error) {
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
	case p.opTick == 6:
		// Read the current PC and then get it incremented for the next instruction.
		_ = p.Ram.Read(p.PC)
		p.PC++
		return true, nil
	}
	return true, InvalidCPUState{"Impossible"}
}

// iSBC implements the SBC instruction for both binary and BCD modes (if implemented) and sets all associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iSBC(arg uint8, _ uint16) (bool, error) {
	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.CPUType != CPU_NMOS_RICOH {
		// Pull the carry bit out which thankfully is the low bit so can be
		// used directly.
		carry := p.P & P_CARRY

		// BCD details - http://6502.org/tutorials/decimal_mode.html
		// Also http://nesdev.com/6502_cpu.txt but it has errors
		aL := int8(p.A&0x0F) - int8(arg&0x0F) + int8(carry) - 1
		// Low nibble fixup
		if aL < 0 {
			aL = ((aL - 0x06) & 0x0F) - 0x10
		}
		sum := int16(p.A&0xF0) - int16(arg&0xF0) + int16(aL)
		// High nibble fixup
		if sum < 0x0000 {
			sum -= 0x60
		}
		res := uint8(sum & 0xFF)

		// Do normal binary math to set C,N,Z
		b := p.A + ^arg + carry
		p.overflowCheck(p.A, ^arg, b)
		p.negativeCheck(b)
		// Yes, could do bit checks here like the hardware but
		// just treating as uint16 math is simpler to code.
		p.carryCheck(uint16(p.A) + uint16(^arg) + uint16(carry))
		p.zeroCheck(b)
		p.A = res
		return true, nil
	}

	// Otherwise binary mode is just ones complement the arg and ADC.
	return p.iADC(^arg, 0x0000)
}

// iALR implements the undocumented opcode for ALR. This does AND #i and then LSR setting all associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iALR(arg uint8, _ uint16) (bool, error) {
	p.loadRegister(&p.A, p.A&arg)
	return p.iLSRAcc()
}

// iANC implements the undocumented opcode for ANC. This does AND #i and then sets carry based on bit 7 (sign extend).
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iANC(arg uint8, _ uint16) (bool, error) {
	p.loadRegister(&p.A, p.A&arg)
	p.carryCheck(uint16(p.A) << 1)
	return true, nil
}

// iARR implements the undocumented opcode for ARR. This does And #i and then ROR except some flags are set differently.
// Implemented as described in http://nesdev.com/6502_cpu.txt
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iARR(arg uint8, _ uint16) (bool, error) {
	t := p.A & arg
	a := p.A
	p.loadRegister(&p.A, t)
	p.iRORAcc()
	// Flags are different based on BCD or not (since the ALU acts different).
	if p.P&P_DECIMAL != 0x00 {
		// If bit 6 changed state between original A and AND operation set V.
		if (t^a)&0x40 != 0x00 {
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
	} else {
		// C is bit 6
		p.carryCheck(uint16(p.A) << 2)
		// V is bit 5 ^ bit 6
		if ((p.A&0x40)>>6)^((p.A^0x20)>>5) != 0x00 {
			p.P |= P_OVERFLOW
		} else {
			p.P &^= P_OVERFLOW
		}
	}
	return true, nil
}

// iAXS implements the undocumented opcode for AXS. (A AND X) - arg (no borrow) setting all associated flags post SBC.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iAXS(arg uint8, _ uint16) (bool, error) {
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
	p.iSBC(arg, 0x000)
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
func (p *Processor) iLAX(arg uint8, _ uint16) (bool, error) {
	p.loadRegister(&p.A, arg)
	p.loadRegister(&p.X, arg)
	return true, nil
}

// iDCP implements the undocumented opcode for DCP. This decrements the given address and then does a CMP with A setting associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iDCP(val uint8, addr uint16) (bool, error) {
	p.Ram.Write(addr, val-1)
	return p.compare(p.A, val-1)
}

// iISC implements the undocumented opcode for ISC. This increments the given address and then does an SBC with setting associated flags.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iISC(val uint8, addr uint16) (bool, error) {
	p.Ram.Write(addr, val+1)
	return p.iSBC(val+1, addr)
}

// iSLO implements the undocumented opcode for SLO. This does an ASL on the given address and then OR's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iSLO(val uint8, addr uint16) (bool, error) {
	p.Ram.Write(addr, val<<1)
	p.carryCheck(uint16(val) << 1)
	p.loadRegister(&p.A, (val<<1)|p.A)
	return true, nil
}

// iRLA implements the undocumented opcode for RLA. This does a ROL on the given address and then AND's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iRLA(val uint8, addr uint16) (bool, error) {
	n := val<<1 | (p.P & P_CARRY)
	p.Ram.Write(addr, n)
	p.carryCheck(uint16(val) << 1)
	p.loadRegister(&p.A, n&p.A)
	return true, nil
}

// iSRE implements the undocumented opcode for SRE. This does a LSR on the given address and then EOR's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iSRE(val uint8, addr uint16) (bool, error) {
	p.Ram.Write(addr, val>>1)
	// Old bit 0 becomes carry
	p.carryCheck(uint16(val) << 8)
	p.loadRegister(&p.A, (val>>1)^p.A)
	return true, nil
}

// iRRA implements the undocumented opcode for RRA. This does a ROR on the given address and then ADC's it against A. Sets flags and carry.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iRRA(val uint8, addr uint16) (bool, error) {
	n := ((p.P & P_CARRY) << 7) | val>>1
	p.Ram.Write(addr, n)
	// Old bit 0 becomes carry
	p.carryCheck((uint16(val) << 8) & 0x0100)
	return p.iADC(n, addr)
}

// iXAA implements the undocumented opcode for XAA. We'll go with http://visual6502.org/wiki/index.php?title=6502_Opcode_8B_(XAA,_ANE)
// for implementation and pick 0xEE as the constant.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iXXA(val uint8, _ uint16) (bool, error) {
	p.loadRegister(&p.A, (p.A|0xEE)&p.X&val)
	return true, nil
}

// iOAL implements the undocumented opcode for OAL. This one acts a bit randomly. It somtimes does XAA and sometimes
// does A=X=A&val.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) iOAL(val uint8, _ uint16) (bool, error) {
	if rand.Float32() >= 0.5 {
		return p.iXXA(val, 0x0000)
	}
	v := p.A & val
	p.loadRegister(&p.A, v)
	p.loadRegister(&p.X, v)
	return true, nil
}

// store implements the STA/STX/STY instruction for storing a value (from a register) in RAM.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) store(val uint8, addr uint16) (bool, error) {
	p.Ram.Write(addr, val)
	return true, nil
}

// storeWithFlags stores the val to the given addr and also sets Z/N flags accordingly.
// Generally used to implmenet INC/DEC.
// Always returns true since this takes one tick and never returns an error.
func (p *Processor) storeWithFlags(val uint8, addr uint16) (bool, error) {
	p.zeroCheck(val)
	p.negativeCheck(val)
	return p.store(val, addr)
}
