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
	//
	// Opcode descriptions/timing/etc:
	// http://obelisk.me.uk/6502/reference.html
	switch op {
	case 0x02:
		p.halted = true
	case 0x04:
		// NOP
	case 0x06:
		// ASL d
		p.ASL(&cycles, uint16(p.AddrZP(&cycles)))
	case 0x0A:
		// ASL
		p.ASLAcc(&cycles)
	case 0x0C:
		// NOP
	case 0x0E:
		// ASL a
		p.ASL(&cycles, p.AddrAbsolute(&cycles))
	case 0x10:
		// BPL *+r
		p.BPL(&cycles)
	case 0x12:
		p.halted = true
	case 0x14:
		// NOP
	case 0x16:
		// ASL d,x
		p.ASL(&cycles, uint16(p.AddrZPX(&cycles)))
	case 0x1A:
		// NOP
	case 0x1C:
		// NOP
	case 0x1E:
		// ASL a,x
		p.ASL(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x21:
		// AND (d,x)
		p.LoadRegister(&p.A, p.A&p.AddrIndirectXVal(&cycles))
	case 0x22:
		p.halted = true
	case 0x24:
		// BIT d
		p.BIT(p.AddrZPVal(&cycles))
	case 0x25:
		// AND d
		p.LoadRegister(&p.A, p.A&p.AddrZPVal(&cycles))
	case 0x29:
		// AND #i
		p.LoadRegister(&p.A, p.A&p.AddrImmediateVal(&cycles))
	case 0x2C:
		// BIT a
		p.BIT(p.AddrAbsoluteVal(&cycles))
	case 0x2D:
		// AND a
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteVal(&cycles))
	case 0x30:
		// BMI *+r
		p.BMI(&cycles)
	case 0x31:
		// AND (d),y
		p.LoadRegister(&p.A, p.A&p.AddrIndirectYVal(&cycles, true))
	case 0x32:
		p.halted = true
	case 0x34:
		// NOP
	case 0x35:
		// AND d,x
		p.LoadRegister(&p.A, p.A&p.AddrZPXVal(&cycles))
	case 0x39:
		// AND a,y
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteYVal(&cycles, true))
	case 0x3A:
		// NOP
	case 0x3C:
		// NOP
	case 0x3D:
		// AND a,x
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteXVal(&cycles, true))
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
	case 0x61:
		// ADC (d,x)
		p.ADC(p.AddrIndirectXVal(&cycles))
	case 0x62:
		p.halted = true
	case 0x64:
		// NOP
	case 0x65:
		// ADC d
		p.ADC(p.AddrZPVal(&cycles))
	case 0x69:
		// ADC #i
		p.ADC(p.AddrImmediateVal(&cycles))
	case 0x6D:
		// ADC a
		p.ADC(p.AddrAbsoluteVal(&cycles))
	case 0x71:
		// ADC (d),y
		p.ADC(p.AddrIndirectYVal(&cycles, true))
	case 0x72:
		p.halted = true
	case 0x74:
		// NOP
	case 0x75:
		// ADC d,x
		p.ADC(p.AddrZPXVal(&cycles))
	case 0x79:
		// ADC a,y
		p.ADC(p.AddrAbsoluteYVal(&cycles, true))
	case 0x7A:
		// NOP
	case 0x7C:
		// NOP
	case 0x7D:
		// ADC a,x
		p.ADC(p.AddrAbsoluteXVal(&cycles, true))
	case 0x80:
		// NOP
	case 0x81:
		// STA (d,x)
		p.Ram.Write(p.AddrIndirectX(&cycles), p.A)
	case 0x82:
		// NOP
	case 0x84:
		// STY d
		p.Ram.WriteZP(p.AddrZP(&cycles), p.Y)
	case 0x85:
		// STA d
		p.Ram.WriteZP(p.AddrZP(&cycles), p.A)
	case 0x86:
		// STX d
		p.Ram.WriteZP(p.AddrZP(&cycles), p.X)
	case 0x88:
		// DEY
		p.LoadRegister(&p.Y, p.Y-1)
	case 0x89:
		// NOP
	case 0x8A:
		// TXA
		p.LoadRegister(&p.A, p.X)
	case 0x8C:
		// STY a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.Y)
	case 0x8D:
		// STA a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.A)
	case 0x8E:
		// STX a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.X)
	case 0x90:
		// BCC *+d
		p.BCC(&cycles)
	case 0x91:
		// STA (d),y
		p.Ram.Write(p.AddrIndirectY(&cycles, false), p.A)
	case 0x92:
		p.halted = true
	case 0x94:
		// STY d,x
		p.Ram.WriteZP(p.AddrZPX(&cycles), p.Y)
	case 0x95:
		// STA d,x
		p.Ram.WriteZP(p.AddrZPX(&cycles), p.A)
	case 0x96:
		// STX d,y
		p.Ram.WriteZP(p.AddrZPY(&cycles), p.X)
	case 0x98:
		// TYA
		p.LoadRegister(&p.A, p.Y)
	case 0x99:
		// STA a,y
		p.Ram.Write(p.AddrAbsoluteY(&cycles, false), p.A)
	case 0x9D:
		// STA a,x
		p.Ram.Write(p.AddrAbsoluteX(&cycles, false), p.A)
	case 0xA0:
		// LDY #i
		p.LoadRegister(&p.Y, p.AddrImmediateVal(&cycles))
	case 0xA1:
		// LDA (d,x)
		p.LoadRegister(&p.A, p.AddrIndirectXVal(&cycles))
	case 0xA2:
		// LDX #i
		p.LoadRegister(&p.X, p.AddrImmediateVal(&cycles))
	case 0xA4:
		// LDY d
		p.LoadRegister(&p.Y, p.AddrZPVal(&cycles))
	case 0xA5:
		// LDA d
		p.LoadRegister(&p.A, p.AddrZPVal(&cycles))
	case 0xA6:
		// LDX d
		p.LoadRegister(&p.X, p.AddrZPVal(&cycles))
	case 0xA8:
		// TAY
		p.LoadRegister(&p.Y, p.A)
	case 0xA9:
		// LDA #i
		p.LoadRegister(&p.A, p.AddrImmediateVal(&cycles))
	case 0xAA:
		// TAX
		p.LoadRegister(&p.X, p.A)
	case 0xAC:
		// LDY a
		p.LoadRegister(&p.Y, p.AddrAbsoluteVal(&cycles))
	case 0xAD:
		// LDA a
		p.LoadRegister(&p.A, p.AddrAbsoluteVal(&cycles))
	case 0xAE:
		// LDX a
		p.LoadRegister(&p.X, p.AddrAbsoluteVal(&cycles))
	case 0xB0:
		// BCS *+d
		p.BCS(&cycles)
	case 0xB1:
		// LDA (d),y
		p.LoadRegister(&p.A, p.AddrIndirectYVal(&cycles, true))
	case 0xB2:
		p.halted = true
	case 0xB4:
		// LDY d,x
		p.LoadRegister(&p.Y, p.AddrZPX(&cycles))
	case 0xB5:
		// LDA d,x
		p.LoadRegister(&p.A, p.AddrZPX(&cycles))
	case 0xB6:
		// LDX d,y
		p.LoadRegister(&p.X, p.AddrZPY(&cycles))
	case 0xB9:
		// LDA a,y
		p.LoadRegister(&p.A, p.AddrAbsoluteYVal(&cycles, true))
	case 0xBC:
		// LDY a,x
		p.LoadRegister(&p.Y, p.AddrAbsoluteXVal(&cycles, true))
	case 0xBD:
		// LDA a,x
		p.LoadRegister(&p.A, p.AddrAbsoluteXVal(&cycles, true))
	case 0xBE:
		// LDX a,y
		p.LoadRegister(&p.X, p.AddrAbsoluteYVal(&cycles, true))
	case 0xC2:
		// NOP
	case 0xC8:
		// INY
		p.LoadRegister(&p.Y, p.Y+1)
	case 0xCA:
		// DEX
		p.LoadRegister(&p.X, p.X-1)
	case 0xD0:
		// BNE *+r
		p.BNE(&cycles)
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
		p.LoadRegister(&p.X, p.X+1)
	case 0xEA:
		// NOP
	case 0xF0:
		// BEQ *+d
		p.BEQ(&cycles)
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

// ZeroCheck sets the Z flag based on the register contents.
func (p *Processor) ZeroCheck(reg uint8) {
	if reg == 0 {
		p.P |= P_ZERO
	} else {
		p.P &^= P_ZERO
	}
}

// NegativeCheck sets the N flag based on the register contents.
func (p *Processor) NegativeCheck(reg uint8) {
	if (reg & P_NEGATIVE) == 0x80 {
		p.P |= P_NEGATIVE
	} else {
		p.P &^= P_NEGATIVE
	}
}

// CarryCheck sets the C flag if the result of an 8 bit ALU operation
// (passed as a 16 bit result) caused a carry out
func (p *Processor) CarryCheck(res uint16) {
	if res > 0xFF {
		p.P |= P_CARRY
	} else {
		p.P &^= P_CARRY
	}
}

// OverflowCheck sets the V flag if the result of the ALU operation
// caused a two's complement sign change.
func (p *Processor) OverflowCheck(reg uint8, arg uint8, res uint8) {
	// If the originals signs differ from the end sign bit
	if (reg^res)&(arg^res)&0x80 != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
}

// AdjustCycles determines (on NMOS 6502's) if a load cycle had to pay
// an extra cycle for page boundary "oops".
func (p *Processor) AdjustCycles(addr uint16, reg uint8) int {
	// If we cross a page boundary on an NMOS we have to adjust cycles by one
	if (addr&0xFF + uint16(reg)) > 0x00FF {
		return 1
	}
	return 0
}

// AddrImmediateVal implements immediate mode - #i
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrImmediateVal(cycles *int) uint8 {
	res := p.Ram.Read(p.PC)
	p.PC++
	return res
}

// AddrZP implements Zero page mode - d
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZP(cycles *int) uint8 {
	addr := p.Ram.Read(p.PC)
	p.PC++
	*cycles++
	return addr
}

// AddrZPVal implements Zero page mode - d
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPVal(cycles *int) uint8 {
	return p.Ram.ReadZP(p.AddrZP(cycles))
}

// AddrZPX implements Zero page plus X mode - d,x
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPX(cycles *int) uint8 {
	addr := p.Ram.Read(p.PC) + p.X
	p.PC++
	*cycles += 2
	return addr
}

// AddrZPXVal implements Zero page plus X mode - d,x
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPXVal(cycles *int) uint8 {
	return p.Ram.ReadZP(p.AddrZPX(cycles))
}

// AddrZPY implements Zero page plus Y mode - d,y
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPY(cycles *int) uint8 {
	addr := p.Ram.Read(p.PC) + p.Y
	p.PC++
	*cycles += 2
	return addr
}

// AddrZPYVal implements Zero page plus Y mode - d,y
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPYVal(cycles *int) uint8 {
	return p.Ram.ReadZP(p.AddrZPY(cycles))
}

// AddrIndirectX implements Zero page indirect plus X mode - (d,x)
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirectX(cycles *int) uint16 {
	addr := p.Ram.ReadZPAddr(p.Ram.Read(p.PC) + p.X)
	p.PC++
	*cycles += 4
	return addr
}

// AddrIndirectXVal implements Zero page indirect plus X mode - (d,x)
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirectXVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrIndirectX(cycles))
}

// AddrIndirectY implements Zero page indirect plus Y mode - (d),y
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirectY(cycles *int, load bool) uint16 {
	base := p.Ram.ReadZPAddr(p.Ram.Read(p.PC))
	addr := base + uint16(p.Y)
	p.PC++
	*cycles += 3
	if !load {
		// Stores are always +4
		*cycles++
	} else {
		// loads can vary on NMOS 6502
		*cycles += p.AdjustCycles(base, p.Y)
	}
	return addr
}

// AddrIndirectY implements Zero page indirect plus Y mode - (d),y
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirectYVal(cycles *int, load bool) uint8 {
	return p.Ram.Read(p.AddrIndirectY(cycles, load))
}

// AddrAbsolute implements absolute mode - a
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsolute(cycles *int) uint16 {
	addr := p.Ram.ReadAddr(p.PC)
	p.PC += 2
	*cycles += 2
	return addr
}

// AddrAbsoluteVal implements absolute mode - a
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrAbsolute(cycles))
}

// AddrAbsoluteX implements absolute plus X mode - a,x
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteX(cycles *int, load bool) uint16 {
	base := p.Ram.ReadAddr(p.PC)
	addr := base + uint16(p.X)
	p.PC += 2
	*cycles += 2
	if !load {
		// Stores are always +3
		*cycles++
	} else {
		// loads can vary on NMOS 6502
		*cycles += p.AdjustCycles(base, p.X)
	}
	return addr
}

// AddrAbsoluteXVal implements absolute plus X mode - a,x
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteXVal(cycles *int, load bool) uint8 {
	return p.Ram.Read(p.AddrAbsoluteX(cycles, load))
}

// AddrAbsoluteY implements absolute plus Y mode - a,y
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteY(cycles *int, load bool) uint16 {
	base := p.Ram.ReadAddr(p.PC)
	addr := base + uint16(p.Y)
	p.PC += 2
	*cycles += 2
	if !load {
		// Stores are always +3
		*cycles++
	} else {
		// loads can vary on NMOS 6502
		*cycles += p.AdjustCycles(base, p.Y)
	}
	return addr
}

// AddrAbsoluteYVal implements absolute plus Y mode - a,x
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteYVal(cycles *int, load bool) uint8 {
	return p.Ram.Read(p.AddrAbsoluteY(cycles, load))
}

// LoadRegister takes the val and inserts it into the register passed in. It then does
// Z and N checks against the new value.
func (p *Processor) LoadRegister(reg *uint8, val uint8) {
	*reg = val
	p.ZeroCheck(*reg)
	p.NegativeCheck(*reg)
}

func (p *Processor) PushStack(val uint8) {
	p.Ram.Write(0x0100+uint16(p.S), val)
	p.S--
}

// ADC implements the ADC/SBC instructions and sets all associated flags.
// For SBC simply ones-complement the arg before calling.
func (p *Processor) ADC(arg uint8) {
	// TODO(jchacon): Implement BCD mode

	// Pull the carry bit out which thankfully is the low bit so can be
	// used directly.
	carry := p.P & P_CARRY
	sum := p.A + arg + carry
	p.OverflowCheck(p.A, arg, sum)
	// Yes, could do bit checks here like the hardware but
	// just treating as uint16 math is simpler to code.
	p.CarryCheck(uint16(p.A) + uint16(arg) + uint16(carry))

	// Now set the accumulator so the other flag checks are against the result.
	p.LoadRegister(&p.A, sum)
}

// ASLAcc implements the ASL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) ASLAcc(cycles *int) {
	p.CarryCheck(uint16(p.A) << 1)
	p.LoadRegister(&p.A, p.A<<1)
}

// ASL implements the ASL instruction on either the accumulator or memory location.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) ASL(cycles *int, addr uint16) {
	var orig, new uint8
	orig = p.Ram.Read(addr)
	new = orig << 1
	p.Ram.Write(addr, new)
	// Costs the same as a store operation plus 2 more cycles
	*cycles += 2
	p.CarryCheck(uint16(orig) << 1)
	p.ZeroCheck(new)
	p.NegativeCheck(new)
}

func (p *Processor) BranchOffset() uint8 {
	offset := p.Ram.Read(p.PC)
	p.PC++
	return offset
}

// PerformBranch does the heavy lifting for branching by
// computing the new PC and computing appropriate cycle costs.
func (p *Processor) PerformBranch(cycles *int, offset uint8) {
	// Any branch taken uses a cycle for ALU on PC
	*cycles++
	page := p.PC & 0xFF00
	// Possibly sign extend the offset for 16 bits so we can
	// just add it and get signed effects.
	var new uint16
	if offset >= 0x80 {
		new = 0xFF00
	}
	new += uint16(offset)
	p.PC += new
	// Page boundary crossing equals one more cycle
	if p.PC&0xFF00 != page {
		*cycles++
	}
}

// BCC implements the BCC instruction and branches if C is clear.
func (p *Processor) BCC(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_CARRY == 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BCS implements the BCS instruction and branches if C is set.
func (p *Processor) BCS(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_CARRY != 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BEQ implements the BEQ instruction and branches if Z is set.
func (p *Processor) BEQ(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_ZERO != 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BIT implements the BIT instruction for AND'ing against A
// and setting N/V based on the value.
func (p *Processor) BIT(val uint8) {
	p.ZeroCheck(p.A & val)
	p.NegativeCheck(val)
	// Copy V from bit 6
	if val&P_OVERFLOW != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
}

// BMI implements the BMI instructions and branches if N is set.
func (p *Processor) BMI(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_NEGATIVE != 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BNE implements the BNE instructions and branches if Z is clear.
func (p *Processor) BNE(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_ZERO == 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BPL implements the BPL instructions and branches if N is clear.
func (p *Processor) BPL(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_NEGATIVE == 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BRK implements the BRK instruction and sets up and then calls the interrupt
// handler referenced at IRQ_VECTOR.
func (p *Processor) BRK(cycles *int) {
	*cycles += 5
	p.PushStack(uint8((p.PC & 0xFF00) >> 8))
	p.PushStack(uint8(p.PC & 0x00FF))
	p.PushStack(p.P)
	p.P |= P_B
	// PC is comes from IRQ_VECTOR
	p.PC = p.Ram.ReadAddr(IRQ_VECTOR)
}
