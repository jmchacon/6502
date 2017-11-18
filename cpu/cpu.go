// Package cpu defines the 6502 architecture and provides
// the methods needed to run the CPU and interface with it
// for emulation.
package cpu

import (
	"fmt"

	"github.com/jmchacon/6502/memory"
)

const (
	CPU_UNIMPLMENTED = iota // Start of valid cpu enumerations
	CPU_NMOS                // Basic NMOS 6502 including undocumented opcodes.
	CPU_NMOS_RICOH          // Ricoh version used in NES which is identical to NMOS except BCD mode is unimplmented.
	CPU_NMOS_6510           // NMOS 6510 variant which includes I/O ports mapped at addresses 0x0 and 0x1
	CPU_CMOS                // 65C02 CMOS version where undocumented opcodes are all explicit NOP.
	CPU_MAX                 // End of CPU enumerations
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
)

type Processor struct {
	A          uint8  // Accumulator register
	X          uint8  // X register
	Y          uint8  // Y register
	S          uint8  // Stack pointer
	P          uint8  // Processor status register
	PC         uint16 // Program counter
	CpuType    int    // Must be between UNIMPLEMENTED and MAX from above.
	Ram        memory.Ram
	halted     bool  // If stopped due to a halt instruction
	haltOpcode uint8 // Opcode that caused the halt
}

// A few custom error types to distinguish why the CPU stopped

// UnimplementedOpcode represents a currently unimplmented opcode in the emulator
type UnimplementedOpcode struct {
	Opcode uint8
}

// Error implements the interface for error types
func (e UnimplementedOpcode) Error() string {
	return fmt.Sprintf("0x%.2X is an unimplemented opcode", e.Opcode)
}

// HaltOpcode represents an opcode which halts the CPU.
type HaltOpcode struct {
	Opcode uint8
}

// Error implements the interface for error types
func (e HaltOpcode) Error() string {
	return fmt.Sprintf("HALT(0x%.2X) executed", e.Opcode)
}

// Init will create a new CPU of the type requested and return it in powered on state.
// The memory passed in will also be powered on.
func Init(cpu int, r memory.Ram) (*Processor, error) {
	if cpu <= CPU_UNIMPLMENTED || cpu >= CPU_MAX {
		return nil, fmt.Errorf("CPU type valid %d is invalid", cpu)
	}
	p := &Processor{
		// TODO(jchacon): This isn't checked anywhere yet and should be done for NMOS specific
		//                behaviors such as indirect JMP on a page boundary and extra cycles
		//                for page boundaries, etc.
		CpuType: cpu,
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
	// These 2 bits are always set.
	p.P = P_S1 | P_B
	p.Reset()
}

// Reset is similar to PowerOn except the main registers are not touched. The stack is moved
// 3 bytes as if PC/P have been pushed. Flags are not disturbed except for interrupts being disabled
// and the PC is loaded from the reset vector.
func (p *Processor) Reset() {
	// Most registers unaffected but stack acts like PC/P have been pushed so decrement by 3 bytes.
	p.S -= 3
	// Disable interrupts
	p.P |= P_INTERRUPT
	// Load PC from reset vector
	p.PC = p.Ram.ReadAddr(RESET_VECTOR)
	p.halted = false
	p.haltOpcode = 0x0
}

// Step executes the next instruction and returns the cycles it took to run. An error is returned
// if the instruction isn't implemented or otherwise halts the CPU.
func (p *Processor) Step() (int, error) {
	// Fast path if halted. The PC won't advance. i.e. we just keep returning the same error.
	if p.halted {
		return 0, HaltOpcode{p.haltOpcode}
	}
	// Everything takes at least 2 cycles
	cycles := 2
	op := p.Ram.Read(p.PC)
	p.PC++
	// Opcode matric taken from:
	// http://wiki.nesdev.com/w/index.php/CPU_unofficial_opcodes#Games_using_unofficial_opcodes
	switch op {
	case 0x02:
		p.halted = true
	case 0x04:
		// NOP
	case 0x0C:
		// NOP
	case 0x12:
		p.halted = true
	case 0x14:
		// NOP
	case 0x1A:
		// NOP
	case 0x1C:
		// NOP
	case 0x22:
		p.halted = true
	case 0x32:
		p.halted = true
	case 0x34:
		// NOP
	case 0x3A:
		// NOP
	case 0x3C:
		// NOP
	case 0x42:
		p.halted = true
	case 0x44:
		// NOP
	case 0x52:
		p.halted = true
	case 0x54:
		// NOP
	case 0x5A:
		// NOP
	case 0x5C:
		// NOP
	case 0x62:
		p.halted = true
	case 0x64:
		// NOP
	case 0x65:
		// ADC d
		p.ADC(p.Ram.ReadZP(p.Ram.Read(p.PC)))
		p.PC++
		cycles = 3
	case 0x69:
		// ADC #i
		p.ADC(p.Ram.Read(p.PC))
		p.PC++
	case 0x72:
		p.halted = true
	case 0x74:
		// NOP
	case 0x75:
		// ADC d,x
		p.ADC(p.Ram.ReadZP(p.Ram.Read(p.PC) + p.X))
		p.PC++
		cycles = 4
	case 0x7A:
		// NOP
	case 0x7C:
		// NOP
	case 0x80:
		// NOP
	case 0x81:
		// STA (d,x)
		p.Ram.Write(p.Ram.ReadZPAddr(p.Ram.Read(p.PC)+p.X), p.A)
		p.PC++
		cycles = 6
	case 0x82:
		// NOP
	case 0x84:
		// STY d
		p.Ram.WriteZP(p.Ram.Read(p.PC), p.Y)
		p.PC++
		cycles = 3
	case 0x85:
		// STA d
		p.Ram.WriteZP(p.Ram.Read(p.PC), p.A)
		p.PC++
		cycles = 3
	case 0x86:
		// STX d
		p.Ram.WriteZP(p.Ram.Read(p.PC), p.X)
		p.PC++
		cycles = 3
	case 0x88:
		// DEY
		p.Y--
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0x89:
		// NOP
	case 0x8A:
		// TXA
		p.A = p.X
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0x8C:
		// STY a
		p.Ram.Write(p.Ram.ReadAddr(p.PC), p.Y)
		p.PC += 2
		cycles = 4
	case 0x8D:
		// STA a
		p.Ram.Write(p.Ram.ReadAddr(p.PC), p.A)
		p.PC += 2
		cycles = 4
	case 0x8E:
		// STX a
		p.Ram.Write(p.Ram.ReadAddr(p.PC), p.X)
		p.PC += 2
		cycles = 4
	case 0x91:
		// STA (d),y
		p.Ram.Write(p.Ram.ReadZPAddr(p.Ram.Read(p.PC))+uint16(p.Y), p.A)
		p.PC++
		cycles = 6
	case 0x92:
		p.halted = true
	case 0x94:
		// STY d,x
		p.Ram.WriteZP(p.Ram.Read(p.PC)+p.X, p.Y)
		p.PC++
		cycles = 4
	case 0x95:
		// STA d,x
		p.Ram.WriteZP(p.Ram.Read(p.PC)+p.X, p.A)
		p.PC++
		cycles = 4
	case 0x96:
		// STX d,y
		p.Ram.WriteZP(p.Ram.Read(p.PC)+p.Y, p.X)
		p.PC++
		cycles = 4
	case 0x98:
		// TYA
		p.A = p.Y
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0x99:
		// STA a,y
		p.Ram.Write(p.Ram.ReadAddr(p.PC)+uint16(p.Y), p.A)
		p.PC += 2
		cycles = 5
	case 0x9D:
		// STA a,x
		p.Ram.Write(p.Ram.ReadAddr(p.PC)+uint16(p.X), p.A)
		p.PC += 2
		cycles = 5
	case 0xA0:
		// LDY #i
		p.Y = p.Ram.Read(p.PC)
		p.PC++
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xA1:
		// LDA (d,x)
		p.A = p.Ram.Read(p.Ram.ReadZPAddr(p.Ram.Read(p.PC) + p.X))
		p.PC++
		cycles = 6
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xA2:
		// LDX #i
		p.X = p.Ram.Read(p.PC)
		p.PC++
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xA4:
		// LDY d
		p.Y = p.Ram.ReadZP(p.Ram.Read(p.PC))
		p.PC++
		cycles = 3
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xA5:
		// LDA d
		p.A = p.Ram.ReadZP(p.Ram.Read(p.PC))
		p.PC++
		cycles = 3
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xA6:
		// LDX d
		p.X = p.Ram.ReadZP(p.Ram.Read(p.PC))
		p.PC++
		cycles = 3
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xA8:
		// TAY
		p.Y = p.A
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xA9:
		// LDA #i
		p.A = p.Ram.Read(p.PC)
		p.PC++
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xAA:
		// TAX
		p.X = p.A
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xAC:
		// LDY a
		p.Y = p.Ram.Read(p.Ram.ReadAddr(p.PC))
		p.PC += 2
		cycles = 4
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xAD:
		// LDA a
		p.A = p.Ram.Read(p.Ram.ReadAddr(p.PC))
		p.PC += 2
		cycles = 4
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xAE:
		// LDX a
		p.X = p.Ram.Read(p.Ram.ReadAddr(p.PC))
		p.PC += 2
		cycles = 4
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xB1:
		// LDA (d),y
		addr := p.Ram.ReadZPAddr(p.Ram.Read(p.PC))
		p.A = p.Ram.Read(addr + uint16(p.Y))
		p.PC++
		cycles = p.AdjustCycles(5, addr, p.Y)
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xB2:
		p.halted = true
	case 0xB4:
		// LDY d,x
		p.Y = p.Ram.ReadZP(p.Ram.Read(p.PC) + p.X)
		p.PC++
		cycles = 4
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xB5:
		// LDA d,x
		p.A = p.Ram.ReadZP(p.Ram.Read(p.PC) + p.X)
		p.PC++
		cycles = 4
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xB6:
		// LDX d,y
		p.X = p.Ram.ReadZP(p.Ram.Read(p.PC) + p.Y)
		p.PC++
		cycles = 4
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xB9:
		// LDA a,y
		addr := p.Ram.ReadAddr(p.PC)
		p.A = p.Ram.Read(addr + uint16(p.Y))
		p.PC += 2
		cycles = p.AdjustCycles(4, addr, p.Y)
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xBC:
		// LDY a,x
		addr := p.Ram.ReadAddr(p.PC)
		p.Y = p.Ram.Read(addr + uint16(p.X))
		p.PC += 2
		cycles = p.AdjustCycles(4, addr, p.X)
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xBD:
		// LDA a,x
		addr := p.Ram.ReadAddr(p.PC)
		p.A = p.Ram.Read(addr + uint16(p.X))
		p.PC += 2
		cycles = p.AdjustCycles(4, addr, p.X)
		p.ZeroCheck(p.A)
		p.NegativeCheck(p.A)
	case 0xBE:
		// LDX a,y
		addr := p.Ram.ReadAddr(p.PC)
		p.X = p.Ram.Read(addr + uint16(p.Y))
		p.PC += 2
		cycles = p.AdjustCycles(4, addr, p.Y)
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xC2:
		// NOP
	case 0xC8:
		// INY
		p.Y++
		p.ZeroCheck(p.Y)
		p.NegativeCheck(p.Y)
	case 0xCA:
		// DEX
		p.X--
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xD2:
		p.halted = true
	case 0xD4:
		// NOP
	case 0xDA:
		// NOP
	case 0xDC:
		// NOP
	case 0xE2:
		// NOP
	case 0xE8:
		// INX
		p.X++
		p.ZeroCheck(p.X)
		p.NegativeCheck(p.X)
	case 0xEA:
		// NOP
	case 0xF2:
		p.halted = true
	case 0xF4:
		// NOP
	case 0xFA:
		// NOP
	case 0xFC:
		// NOP
	default:
		return 0, UnimplementedOpcode{op}
	}
	if p.halted {
		p.haltOpcode = op
		return 0, HaltOpcode{op}
	}
	return cycles, nil
}

func (p *Processor) AdjustCycles(cycles int, addr uint16, reg uint8) int {
	// If we cross a page boundary on an NMOS we have to adjust cycles by one
	if (addr&0xFF + uint16(reg)) > 0x00FF {
		cycles++
	}
	return cycles
}

func (p *Processor) ZeroCheck(reg uint8) {
	if reg == 0 {
		p.P |= P_ZERO
	} else {
		p.P &^= P_ZERO
	}
}

func (p *Processor) NegativeCheck(reg uint8) {
	if (reg & 0x80) == 0x80 {
		p.P |= P_NEGATIVE
	} else {
		p.P &^= P_NEGATIVE
	}
}

func (p *Processor) ADC(arg uint8) {
	// TODO(jchacon): Implement BCD mode
	// Yes, could do bit checks here like the hardware but
	// just treating as uint16 math is simpler to code.
	carry := p.P & P_CARRY
	new := uint16(p.A) + uint16(arg) + uint16(carry)
	sum := p.A + arg + carry
	if new > 0xFF {
		p.P |= P_CARRY
	} else {
		p.P &^= P_CARRY
	}
	// If the originals signs differ from the end sign bit
	if (p.A^sum)&(arg^sum)&0x80 != 0x0 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
	p.A = sum
	p.ZeroCheck(p.A)
	p.NegativeCheck(p.A)
}
