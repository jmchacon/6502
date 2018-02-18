// Package cpu defines the 6502 architecture and provides
// the methods needed to run the CPU and interface with it
// for emulation.
package cpu

import (
	"fmt"
	"math/rand"

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

	NEGATIVE_ONE = uint8(0xFF)
)

type Processor struct {
	A                 uint8  // Accumulator register
	X                 uint8  // X register
	Y                 uint8  // Y register
	S                 uint8  // Stack pointer
	P                 uint8  // Processor status register
	PC                uint16 // Program counter
	CpuType           int    // Must be between UNIMPLEMENTED and MAX from above.
	Ram               memory.Ram
	skipInterrupt     bool  // If set then one more instruction is executed before checking interrupts
	prevSkipInterrupt bool  // If set then the previous instruction set skipInterrupt and we don't want to do it again.
	holdNMI           bool  // Hold NMI state over by an instruction
	holdIRQ           bool  // Hold IRQ state over by an instruction
	halted            bool  // If stopped due to a halt instruction
	haltOpcode        uint8 // Opcode that caused the halt
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
	// This bit is always set.
	p.P = P_S1
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

const (
	MODE_IMMEDIATE = iota
	MODE_ZP
	MODE_ZPX
	MODE_ZPY
	MODE_INDIRECTX
	MODE_INDIRECTY
	MODE_ABSOLUTE
	MODE_ABSOLUTEX
	MODE_ABSOLUTEY
	MODE_INDIRECT
	MODE_IMPLIED
	MODE_RELATIVE
)

// Disassemble will take the given PC value and disassemble the instruction at that location
// returning a string for the disassembly and the bytes forward the PC should move to get to
// the next instruction. This does not interpret the instructions so LDA, JMP, LDA in memory
// will disassemble as that sequence and not follow the JMP.
// This always reads at least one byte past the current PC so make sure that address is valid.
func Disassemble(pc uint16, r memory.Ram) (string, int) {
	// All instructions read a 2nd byte generally so just do that now.
	pc1 := r.Read(pc + 1)
	// Setup a 16 bit value so it can be added the the PC for branch offset.s
	pc116 := uint16(pc1)
	// Sign extend it so can be added to PC and wrap/negate as needed.
	if pc1 >= 0x80 {
		pc116 |= 0xFF00
	}
	// And preread the 2nd byte for 3 byte instructions.
	pc2 := r.Read(pc + 2)

	// When done this will have op and the mode determined and byte count
	var op string
	mode := MODE_IMPLIED
	o := r.Read(pc)
	switch o {
	case 0x00:
		op = "BRK"
		mode = MODE_IMMEDIATE // Ok, not really but the byte after BRK is read and skipped.
	case 0x01:
		op = "ORA"
		mode = MODE_INDIRECTX
	case 0x02:
		op = "HLT"
	case 0x03:
		op = "SLO"
		mode = MODE_INDIRECTX
	case 0x04:
		op = "NOP"
		mode = MODE_ZP
	case 0x05:
		op = "ORA"
		mode = MODE_ZP
	case 0x06:
		op = "ASL"
		mode = MODE_ZP
	case 0x07:
		op = "SLO"
		mode = MODE_ZP
	case 0x08:
		op = "PHP"
	case 0x09:
		op = "ORA"
		mode = MODE_IMMEDIATE
	case 0x0A:
		op = "ASL"
	case 0x0B:
		op = "ANC"
		mode = MODE_IMMEDIATE
	case 0x0C:
		op = "NOP"
		mode = MODE_ABSOLUTE
	case 0x0D:
		op = "ORA"
		mode = MODE_ABSOLUTE
	case 0x0E:
		op = "ASL"
		mode = MODE_ABSOLUTE
	case 0x0F:
		op = "SLO"
		mode = MODE_ABSOLUTE
	case 0x10:
		op = "BPL"
		mode = MODE_RELATIVE
	case 0x11:
		op = "ORA"
		mode = MODE_INDIRECTY
	case 0x12:
		op = "HLT"
	case 0x13:
		op = "SLO"
		mode = MODE_INDIRECTY
	case 0x14:
		op = "NOP"
		mode = MODE_ZPX
	case 0x15:
		op = "ORA"
		mode = MODE_ZPX
	case 0x16:
		op = "ASL"
		mode = MODE_ZPX
	case 0x17:
		op = "SLO"
		mode = MODE_ZPX
	case 0x18:
		op = "CLC"
	case 0x19:
		op = "ORA"
		mode = MODE_ABSOLUTEY
	case 0x1A:
		op = "NOP"
	case 0x1B:
		op = "SLO"
		mode = MODE_ABSOLUTEY
	case 0x1C:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0x1D:
		op = "ORA"
		mode = MODE_ABSOLUTEX
	case 0x1E:
		op = "ASL"
		mode = MODE_ABSOLUTEX
	case 0x1F:
		op = "SLO"
		mode = MODE_ABSOLUTEX
	case 0x20:
		op = "JSR"
		mode = MODE_ABSOLUTE
	case 0x21:
		op = "AND"
		mode = MODE_INDIRECTX
	case 0x22:
		op = "HLT"
	case 0x23:
		op = "RLA"
		mode = MODE_INDIRECTX
	case 0x24:
		op = "BIT"
		mode = MODE_ZP
	case 0x25:
		op = "AND"
		mode = MODE_ZP
	case 0x26:
		op = "ROL"
		mode = MODE_ZP
	case 0x27:
		op = "RLA"
		mode = MODE_ZP
	case 0x28:
		op = "PLP"
	case 0x29:
		op = "AND"
		mode = MODE_IMMEDIATE
	case 0x2A:
		op = "ROL"
	case 0x2B:
		op = "ANC"
		mode = MODE_IMMEDIATE
	case 0x2C:
		op = "BIT"
		mode = MODE_ABSOLUTE
	case 0x2D:
		op = "AND"
		mode = MODE_ABSOLUTE
	case 0x2E:
		op = "ROL"
		mode = MODE_ABSOLUTE
	case 0x2F:
		op = "RLA"
		mode = MODE_ABSOLUTE
	case 0x30:
		op = "BMI"
		mode = MODE_RELATIVE
	case 0x31:
		op = "AND"
		mode = MODE_INDIRECTY
	case 0x32:
		op = "HLT"
	case 0x33:
		op = "RLA"
		mode = MODE_INDIRECTY
	case 0x34:
		op = "NOP"
		mode = MODE_ZPX
	case 0x35:
		op = "AND"
		mode = MODE_ZPX
	case 0x36:
		op = "ROL"
		mode = MODE_ZPX
	case 0x37:
		op = "RLA"
		mode = MODE_ZPX
	case 0x38:
		op = "SEC"
	case 0x39:
		op = "AND"
		mode = MODE_ABSOLUTEY
	case 0x3A:
		op = "NOP"
	case 0x3B:
		op = "RLA"
		mode = MODE_ABSOLUTEY
	case 0x3C:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0x3D:
		op = "AND"
		mode = MODE_ABSOLUTEX
	case 0x3E:
		op = "ROL"
		mode = MODE_ABSOLUTEX
	case 0x3F:
		op = "RLA"
		mode = MODE_ABSOLUTEX
	case 0x40:
		op = "RTI"
	case 0x41:
		op = "EOR"
		mode = MODE_INDIRECTX
	case 0x42:
		op = "HLT"
	case 0x43:
		op = "SRE"
		mode = MODE_INDIRECTX
	case 0x44:
		op = "NOP"
		mode = MODE_ZP
	case 0x45:
		op = "EOR"
		mode = MODE_ZP
	case 0x46:
		op = "LSR"
		mode = MODE_ZP
	case 0x47:
		op = "SRE"
		mode = MODE_ZP
	case 0x48:
		op = "PHA"
	case 0x49:
		op = "EOR"
		mode = MODE_IMMEDIATE
	case 0x4A:
		op = "LSR"
	case 0x4B:
		op = "ALR"
		mode = MODE_IMMEDIATE
	case 0x4C:
		op = "JMP"
		mode = MODE_ABSOLUTE
	case 0x4D:
		op = "EOR"
		mode = MODE_ABSOLUTE
	case 0x4E:
		op = "LSR"
		mode = MODE_ABSOLUTE
	case 0x4F:
		op = "SRE"
		mode = MODE_ABSOLUTE
	case 0x50:
		op = "BVC"
		mode = MODE_RELATIVE
	case 0x51:
		op = "EOR"
		mode = MODE_INDIRECTY
	case 0x52:
		op = "HLT"
	case 0x53:
		op = "SRE"
		mode = MODE_INDIRECTY
	case 0x54:
		op = "NOP"
		mode = MODE_ZPX
	case 0x55:
		op = "EOR"
		mode = MODE_ZPX
	case 0x56:
		op = "LSR"
		mode = MODE_ZPX
	case 0x57:
		op = "SRE"
		mode = MODE_ZPX
	case 0x58:
		op = "CLI"
	case 0x59:
		op = "EOR"
		mode = MODE_ABSOLUTEY
	case 0x5A:
		op = "NOP"
	case 0x5B:
		op = "SRE"
		mode = MODE_ABSOLUTEY
	case 0x5C:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0x5D:
		op = "EOR"
		mode = MODE_ABSOLUTEX
	case 0x5E:
		op = "LSR"
		mode = MODE_ABSOLUTEX
	case 0x5F:
		op = "SRE"
		mode = MODE_ABSOLUTEX
	case 0x60:
		op = "RTS"
	case 0x61:
		op = "ADC"
		mode = MODE_INDIRECTX
	case 0x62:
		op = "HLT"
	case 0x63:
		op = "RRA"
		mode = MODE_INDIRECTX
	case 0x64:
		op = "NOP"
		mode = MODE_ZP
	case 0x65:
		op = "ADC"
		mode = MODE_ZP
	case 0x66:
		op = "ROR"
		mode = MODE_ZP
	case 0x67:
		op = "RRA"
		mode = MODE_ZP
	case 0x68:
		op = "PLA"
	case 0x69:
		op = "ADC"
		mode = MODE_IMMEDIATE
	case 0x6A:
		op = "ROR"
	case 0x6B:
		op = "ARR"
		mode = MODE_IMMEDIATE
	case 0x6C:
		op = "JMP"
		mode = MODE_INDIRECT
	case 0x6D:
		op = "ADC"
		mode = MODE_ABSOLUTE
	case 0x6E:
		op = "ROR"
		mode = MODE_ABSOLUTE
	case 0x6F:
		op = "RRA"
		mode = MODE_ABSOLUTE
	case 0x70:
		op = "BVS"
		mode = MODE_RELATIVE
	case 0x71:
		op = "ADC"
		mode = MODE_INDIRECTY
	case 0x72:
		op = "HLT"
	case 0x73:
		op = "RRA"
		mode = MODE_INDIRECTY
	case 0x74:
		op = "NOP"
		mode = MODE_ZPX
	case 0x75:
		op = "ADC"
		mode = MODE_ZPX
	case 0x76:
		op = "ROR"
		mode = MODE_ZPX
	case 0x77:
		op = "RRA"
		mode = MODE_ZPX
	case 0x78:
		op = "SEI"
	case 0x79:
		op = "ADC"
		mode = MODE_ABSOLUTEY
	case 0x7A:
		op = "NOP"
	case 0x7B:
		op = "RRA"
		mode = MODE_ABSOLUTEY
	case 0x7C:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0x7D:
		op = "ADC"
		mode = MODE_ABSOLUTEX
	case 0x7E:
		op = "ROR"
		mode = MODE_ABSOLUTEX
	case 0x7F:
		op = "RRA"
		mode = MODE_ABSOLUTEX
	case 0x80:
		op = "NOP"
		mode = MODE_IMMEDIATE
	case 0x81:
		op = "STA"
		mode = MODE_INDIRECTX
	case 0x82:
		op = "NOP"
		mode = MODE_IMMEDIATE
	case 0x83:
		op = "SAX"
		mode = MODE_INDIRECTX
	case 0x84:
		op = "STY"
		mode = MODE_ZP
	case 0x85:
		op = "STA"
		mode = MODE_ZP
	case 0x86:
		op = "STX"
		mode = MODE_ZP
	case 0x87:
		op = "SAX"
		mode = MODE_ZP
	case 0x88:
		op = "DEY"
	case 0x89:
		op = "NOP"
		mode = MODE_IMMEDIATE
	case 0x8A:
		op = "TXA"
	case 0x8B:
		op = "XAA"
		mode = MODE_IMMEDIATE
	case 0x8C:
		op = "STY"
		mode = MODE_ABSOLUTE
	case 0x8D:
		op = "STA"
		mode = MODE_ABSOLUTE
	case 0x8E:
		op = "STX"
		mode = MODE_ABSOLUTE
	case 0x8F:
		op = "SAX"
		mode = MODE_ABSOLUTE
	case 0x90:
		op = "BCC"
		mode = MODE_RELATIVE
	case 0x91:
		op = "STA"
		mode = MODE_INDIRECTY
	case 0x92:
		op = "HLT"
	case 0x94:
		op = "STY"
		mode = MODE_ZPX
	case 0x95:
		op = "STA"
		mode = MODE_ZPX
	case 0x96:
		op = "STX"
		mode = MODE_ZPY
	case 0x97:
		op = "SAX"
		mode = MODE_ZPY
	case 0x98:
		op = "TYA"
	case 0x99:
		op = "STA"
		mode = MODE_ABSOLUTEY
	case 0x9A:
		op = "TXS"
	case 0x9D:
		op = "STA"
		mode = MODE_ABSOLUTEX
	case 0xA0:
		op = "LDY"
		mode = MODE_IMMEDIATE
	case 0xA1:
		op = "LDA"
		mode = MODE_INDIRECTX
	case 0xA2:
		op = "LDX"
		mode = MODE_IMMEDIATE
	case 0xA3:
		op = "LAX"
		mode = MODE_INDIRECTX
	case 0xA4:
		op = "LDY"
		mode = MODE_ZP
	case 0xA5:
		op = "LDA"
		mode = MODE_ZP
	case 0xA6:
		op = "LDX"
		mode = MODE_ZP
	case 0xA7:
		op = "LAX"
		mode = MODE_ZP
	case 0xA8:
		op = "TAY"
	case 0xA9:
		op = "LDA"
		mode = MODE_IMMEDIATE
	case 0xAA:
		op = "TAX"
	case 0xAB:
		op = "OAL"
		mode = MODE_IMMEDIATE
	case 0xAC:
		op = "LDY"
		mode = MODE_ABSOLUTE
	case 0xAD:
		op = "LDA"
		mode = MODE_ABSOLUTE
	case 0xAE:
		op = "LDX"
		mode = MODE_ABSOLUTE
	case 0xAF:
		op = "LAX"
		mode = MODE_ABSOLUTE
	case 0xB0:
		op = "BCS"
		mode = MODE_RELATIVE
	case 0xB1:
		op = "LDA"
		mode = MODE_INDIRECTY
	case 0xB2:
		op = "HLT"
	case 0xB3:
		op = "LAX"
		mode = MODE_INDIRECTY
	case 0xB4:
		op = "LDY"
		mode = MODE_ZPX
	case 0xB5:
		op = "LDA"
		mode = MODE_ZPX
	case 0xB6:
		op = "LDX"
		mode = MODE_ZPY
	case 0xB7:
		op = "LAX"
		mode = MODE_ZPY
	case 0xB8:
		op = "CLV"
	case 0xB9:
		op = "LDA"
		mode = MODE_ABSOLUTEY
	case 0xBA:
		op = "TSX"
	case 0xBC:
		op = "LDY"
		mode = MODE_ABSOLUTEX
	case 0xBD:
		op = "LDA"
		mode = MODE_ABSOLUTEX
	case 0xBE:
		op = "LDX"
		mode = MODE_ABSOLUTEY
	case 0xBF:
		op = "LAX"
		mode = MODE_ABSOLUTEY
	case 0xC0:
		op = "CPY"
		mode = MODE_IMMEDIATE
	case 0xC1:
		op = "CMP"
		mode = MODE_INDIRECTX
	case 0xC2:
		op = "NOP"
		mode = MODE_IMMEDIATE
	case 0xC3:
		op = "DCP"
		mode = MODE_INDIRECTX
	case 0xC4:
		op = "CPY"
		mode = MODE_ZP
	case 0xC5:
		op = "CMP"
		mode = MODE_ZP
	case 0xC6:
		op = "DEC"
		mode = MODE_ZP
	case 0xC7:
		op = "DCP"
		mode = MODE_ZP
	case 0xC8:
		op = "INY"
	case 0xC9:
		op = "CMP"
		mode = MODE_IMMEDIATE
	case 0xCA:
		op = "DEX"
	case 0xCB:
		op = "AXS"
		mode = MODE_IMMEDIATE
	case 0xCC:
		op = "CPY"
		mode = MODE_ABSOLUTE
	case 0xCD:
		op = "CMP"
		mode = MODE_ABSOLUTE
	case 0xCE:
		op = "DEC"
		mode = MODE_ABSOLUTE
	case 0xCF:
		op = "DCP"
		mode = MODE_ABSOLUTE
	case 0xD0:
		op = "BNE"
		mode = MODE_RELATIVE
	case 0xD1:
		op = "CMP"
		mode = MODE_INDIRECTY
	case 0xD2:
		op = "HLT"
	case 0xD3:
		op = "DCP"
		mode = MODE_INDIRECTY
	case 0xD4:
		op = "NOP"
		mode = MODE_ZPX
	case 0xD5:
		op = "CMP"
		mode = MODE_ZPX
	case 0xD6:
		op = "DEC"
		mode = MODE_ZPX
	case 0xD7:
		op = "DCP"
		mode = MODE_ZPX
	case 0xD8:
		op = "CLD"
	case 0xD9:
		op = "CMP"
		mode = MODE_ABSOLUTEY
	case 0xDA:
		op = "NOP"
	case 0xDB:
		op = "DCP"
		mode = MODE_ABSOLUTEY
	case 0xDC:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0xDD:
		op = "CMP"
		mode = MODE_ABSOLUTEX
	case 0xDE:
		op = "DEC"
		mode = MODE_ABSOLUTEX
	case 0xDF:
		op = "DCP"
		mode = MODE_ABSOLUTEX
	case 0xE0:
		op = "CPX"
		mode = MODE_IMMEDIATE
	case 0xE1:
		op = "SBC"
		mode = MODE_INDIRECTX
	case 0xE2:
		op = "NOP"
		mode = MODE_IMMEDIATE
	case 0xE3:
		op = "ISC"
		mode = MODE_INDIRECTX
	case 0xE4:
		op = "CPX"
		mode = MODE_ZP
	case 0xE5:
		op = "SBC"
		mode = MODE_ZP
	case 0xE6:
		op = "INC"
		mode = MODE_ZP
	case 0xE7:
		op = "ISC"
		mode = MODE_ZP
	case 0xE8:
		op = "INX"
	case 0xE9:
		op = "SBC"
		mode = MODE_IMMEDIATE
	case 0xEA:
		op = "NOP"
	case 0xEB:
		op = "SBC"
		mode = MODE_IMMEDIATE
	case 0xEC:
		op = "CPX"
		mode = MODE_ABSOLUTE
	case 0xED:
		op = "SBC"
		mode = MODE_ABSOLUTE
	case 0xEE:
		op = "INC"
		mode = MODE_ABSOLUTE
	case 0xEF:
		op = "ISC"
		mode = MODE_ABSOLUTE
	case 0xF0:
		op = "BEQ"
		mode = MODE_RELATIVE
	case 0xF1:
		op = "SBC"
		mode = MODE_INDIRECTY
	case 0xF2:
		op = "HLT"
	case 0xF3:
		op = "ISC"
		mode = MODE_INDIRECTY
	case 0xF4:
		op = "NOP"
		mode = MODE_ZPX
	case 0xF5:
		op = "SBC"
		mode = MODE_ZPX
	case 0xF6:
		op = "INC"
		mode = MODE_ZPX
	case 0xF7:
		op = "ISC"
		mode = MODE_ZPX
	case 0xF8:
		op = "SED"
	case 0xF9:
		op = "SBC"
		mode = MODE_ABSOLUTEY
	case 0xFA:
		op = "NOP"
	case 0xFB:
		op = "ISC"
		mode = MODE_ABSOLUTEY
	case 0xFC:
		op = "NOP"
		mode = MODE_ABSOLUTEX
	case 0xFD:
		op = "SBC"
		mode = MODE_ABSOLUTEX
	case 0xFE:
		op = "INC"
		mode = MODE_ABSOLUTEX
	case 0xFF:
		op = "ISC"
		mode = MODE_ABSOLUTEX
	default:
		op = "UNIMPLEMENTED"
	}

	count := 2 // Default byte count, adjusted below.
	out := fmt.Sprintf("%.4X %.2X ", pc, o)
	switch mode {
	case MODE_IMMEDIATE:
		out += fmt.Sprintf("%.2X      %s #%.2X       ", pc1, op, pc1)
	case MODE_ZP:
		out += fmt.Sprintf("%.2X      %s %.2X        ", pc1, op, pc1)
	case MODE_ZPX:
		out += fmt.Sprintf("%.2X      %s %.2X,X      ", pc1, op, pc1)
	case MODE_ZPY:
		out += fmt.Sprintf("%.2X      %s %.2X,Y      ", pc1, op, pc1)
	case MODE_INDIRECTX:
		out += fmt.Sprintf("%.2X      %s (%.2X,X)    ", pc1, op, pc1)
	case MODE_INDIRECTY:
		out += fmt.Sprintf("%.2X      %s (%.2X),Y    ", pc1, op, pc1)
	case MODE_ABSOLUTE:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X      ", pc1, pc2, op, pc2, pc1)
		count++
	case MODE_ABSOLUTEX:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X,X    ", pc1, pc2, op, pc2, pc1)
		count++
	case MODE_ABSOLUTEY:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X,Y    ", pc1, pc2, op, pc2, pc1)
		count++
	case MODE_INDIRECT:
		out += fmt.Sprintf("%.2X %.2X   %s (%.2X%.2X)    ", pc1, pc2, op, pc2, pc1)
		count++
	case MODE_IMPLIED:
		out += fmt.Sprintf("        %s           ", op)
		count--
	case MODE_RELATIVE:
		out += fmt.Sprintf("%.2X      %s %.2X (%.4X) ", pc1, op, pc1, pc+pc116+2)
	default:
		panic(fmt.Sprintf("Invalid mode: %d", mode))
	}
	return out, count
}

// Step executes the next instruction and returns the cycles it took to run. An error is returned
// if the instruction isn't implemented or otherwise halts the CPU.
// On an interrupt the cycle count for setup is accounted before executing the first instruction.
// For an NMOS cpu on a taken branch and an interrupt coming in immediately after will cause one
// more instruction to be executed before the first interrupt instruction. This is also accounted
// for in the cycle count.
func (p *Processor) Step(irq bool, nmi bool) (int, error) {
	// Fast path if halted. The PC won't advance. i.e. we just keep returning the same error.
	if p.halted {
		return 0, HaltOpcode{p.haltOpcode}
	}

	// Everything takes at least 2 cycles
	cycles := 2

	// On NMOS cpus a previous taken branch will set this to delay interrupt processing by
	// one instruction.
	switch {
	case nmi, p.holdNMI:
		if p.skipInterrupt && p.CpuType != CPU_CMOS {
			p.holdNMI = true
		} else {
			p.holdNMI = false
			p.prevSkipInterrupt = false
			p.SetupInterrupt(&cycles, NMI_VECTOR, true)
		}
	case irq, p.holdIRQ:
		if p.skipInterrupt && p.CpuType != CPU_CMOS {
			p.holdIRQ = true
		} else {
			p.holdIRQ = false
			p.prevSkipInterrupt = false
			if p.P&P_INTERRUPT == 0x00 {
				p.SetupInterrupt(&cycles, IRQ_VECTOR, true)
			}
		}

	}
	p.prevSkipInterrupt = p.skipInterrupt
	p.skipInterrupt = false
	op := p.Ram.Read(p.PC)
	p.PC++
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
	switch op {
	case 0x00:
		// BRK
		p.BRK(&cycles)
	case 0x01:
		// ORA (d,x)
		p.LoadRegister(&p.A, p.A|p.AddrIndirectXVal(&cycles))
	case 0x02:
		// HLT
		p.halted = true
	case 0x03:
		// SLO (d,x)
		p.SLO(&cycles, p.AddrIndirectX(&cycles))
	case 0x04:
		// NOP
		_ = p.AddrZPVal(&cycles)
	case 0x05:
		// ORA d
		p.LoadRegister(&p.A, p.A|p.AddrZPVal(&cycles))
	case 0x06:
		// ASL d
		p.ASL(&cycles, p.AddrZP(&cycles))
	case 0x07:
		// SLO d
		p.SLO(&cycles, p.AddrZP(&cycles))
	case 0x08:
		// PHP
		p.PHP(&cycles)
	case 0x09:
		// ORA #i
		p.LoadRegister(&p.A, p.A|p.AddrImmediateVal(&cycles))
	case 0x0A:
		// ASL
		p.ASLAcc()
	case 0x0B:
		// ANC #i
		p.ANC(p.AddrImmediateVal(&cycles))
	case 0x0C:
		// NOP a
		_ = p.AddrAbsoluteVal(&cycles)
	case 0x0D:
		// ORA a
		p.LoadRegister(&p.A, p.A|p.AddrAbsoluteVal(&cycles))
	case 0x0E:
		// ASL a
		p.ASL(&cycles, p.AddrAbsolute(&cycles))
	case 0x0F:
		// SLO a
		p.SLO(&cycles, p.AddrAbsolute(&cycles))
	case 0x10:
		// BPL *+r
		p.BPL(&cycles)
	case 0x11:
		// ORA (d),y
		p.LoadRegister(&p.A, p.A|p.AddrIndirectYVal(&cycles, true))
	case 0x12:
		// HLT
		p.halted = true
	case 0x13:
		// SLO (d),y
		p.SLO(&cycles, p.AddrIndirectY(&cycles, false))
	case 0x14:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0x15:
		// ORA d,x
		p.LoadRegister(&p.A, p.A|p.AddrZPXVal(&cycles))
	case 0x16:
		// ASL d,x
		p.ASL(&cycles, p.AddrZPX(&cycles))
	case 0x17:
		// SLO d,x
		p.SLO(&cycles, p.AddrZPX(&cycles))
	case 0x18:
		// CLC
		p.P &^= P_CARRY
	case 0x19:
		// ORA a,y
		p.LoadRegister(&p.A, p.A|p.AddrAbsoluteYVal(&cycles, true))
	case 0x1A:
		// NOP
	case 0x1B:
		// SLO a,y
		p.SLO(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0x1C:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0x1D:
		// ORA a,x
		p.LoadRegister(&p.A, p.A|p.AddrAbsoluteXVal(&cycles, true))
	case 0x1E:
		// ASL a,x
		p.ASL(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x1F:
		// SLO a,x
		p.SLO(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x20:
		// JSR a
		p.JSR(&cycles, p.AddrAbsolute(&cycles))
	case 0x21:
		// AND (d,x)
		p.LoadRegister(&p.A, p.A&p.AddrIndirectXVal(&cycles))
	case 0x22:
		// HLT
		p.halted = true
	case 0x23:
		// RLA (d,x)
		p.RLA(&cycles, p.AddrIndirectX(&cycles))
	case 0x24:
		// BIT d
		p.BIT(p.AddrZPVal(&cycles))
	case 0x25:
		// AND d
		p.LoadRegister(&p.A, p.A&p.AddrZPVal(&cycles))
	case 0x26:
		// ROL d
		p.ROL(&cycles, p.AddrZP(&cycles))
	case 0x27:
		// RLA d
		p.RLA(&cycles, p.AddrZP(&cycles))
	case 0x28:
		// PLP
		p.PLP(&cycles)
	case 0x29:
		// AND #i
		p.LoadRegister(&p.A, p.A&p.AddrImmediateVal(&cycles))
	case 0x2A:
		// ROL
		p.ROLAcc()
	case 0x2B:
		// ANC #i
		p.ANC(p.AddrImmediateVal(&cycles))
	case 0x2C:
		// BIT a
		p.BIT(p.AddrAbsoluteVal(&cycles))
	case 0x2D:
		// AND a
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteVal(&cycles))
	case 0x2E:
		// ROL a
		p.ROL(&cycles, p.AddrAbsolute(&cycles))
	case 0x2F:
		// RLA a
		p.RLA(&cycles, p.AddrAbsolute(&cycles))
	case 0x30:
		// BMI *+r
		p.BMI(&cycles)
	case 0x31:
		// AND (d),y
		p.LoadRegister(&p.A, p.A&p.AddrIndirectYVal(&cycles, true))
	case 0x32:
		// HLT
		p.halted = true
	case 0x33:
		// RLA (d),y
		p.RLA(&cycles, p.AddrIndirectY(&cycles, false))
	case 0x34:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0x35:
		// AND d,x
		p.LoadRegister(&p.A, p.A&p.AddrZPXVal(&cycles))
	case 0x36:
		// ROL d,x
		p.ROL(&cycles, p.AddrZPX(&cycles))
	case 0x37:
		// RLA d,x
		p.RLA(&cycles, p.AddrZPX(&cycles))
	case 0x38:
		// SEC
		p.P |= P_CARRY
	case 0x39:
		// AND a,y
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteYVal(&cycles, true))
	case 0x3A:
		// NOP
	case 0x3B:
		// RLA a,y
		p.RLA(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0x3C:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0x3D:
		// AND a,x
		p.LoadRegister(&p.A, p.A&p.AddrAbsoluteXVal(&cycles, true))
	case 0x3E:
		// ROL a,x
		p.ROL(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x3F:
		// RLA a,x
		p.RLA(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x40:
		// RTI
		p.RTI(&cycles)
	case 0x41:
		// EOR (d,x)
		p.LoadRegister(&p.A, p.A^p.AddrIndirectXVal(&cycles))
	case 0x42:
		// HLT
		p.halted = true
	case 0x43:
		// SRE (d,x)
		p.SRE(&cycles, p.AddrIndirectX(&cycles))
	case 0x44:
		// NOP d
		_ = p.AddrZPVal(&cycles)
	case 0x45:
		// EOR d
		p.LoadRegister(&p.A, p.A^p.AddrZPVal(&cycles))
	case 0x46:
		// LSR d
		p.LSR(&cycles, p.AddrZP(&cycles))
	case 0x47:
		// SRE d
		p.SRE(&cycles, p.AddrZP(&cycles))
	case 0x48:
		// PHA
		p.PushStack(&cycles, p.A)
	case 0x49:
		// EOR #i
		p.LoadRegister(&p.A, p.A^p.AddrImmediateVal(&cycles))
	case 0x4A:
		// LSR
		p.LSRAcc()
	case 0x4B:
		// ALR #i
		p.ALR(p.AddrImmediateVal(&cycles))
	case 0x4C:
		// JMP a
		p.JMP(&cycles)
	case 0x4D:
		// EOR a
		p.LoadRegister(&p.A, p.A^p.AddrAbsoluteVal(&cycles))
	case 0x4E:
		// LSR a
		p.LSR(&cycles, p.AddrAbsolute(&cycles))
	case 0x4F:
		// SRE a
		p.SRE(&cycles, p.AddrAbsolute(&cycles))
	case 0x50:
		// BVC *+r
		p.BVC(&cycles)
	case 0x51:
		// EOR (d),y
		p.LoadRegister(&p.A, p.A^p.AddrIndirectYVal(&cycles, true))
	case 0x52:
		// HLT
		p.halted = true
	case 0x53:
		// SRE (d),y
		p.SRE(&cycles, p.AddrIndirectY(&cycles, false))
	case 0x54:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0x55:
		// EOR d,x
		p.LoadRegister(&p.A, p.A^p.AddrZPXVal(&cycles))
	case 0x56:
		// LSR d,x
		p.LSR(&cycles, p.AddrZPX(&cycles))
	case 0x57:
		// SRE d,x
		p.SRE(&cycles, p.AddrZPX(&cycles))
	case 0x58:
		// CLI
		p.P &^= P_INTERRUPT
	case 0x59:
		// EOR a,y
		p.LoadRegister(&p.A, p.A^p.AddrAbsoluteYVal(&cycles, true))
	case 0x5A:
		// NOP
	case 0x5B:
		// SRE a,y
		p.SRE(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0x5C:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0x5D:
		// EOR a,x
		p.LoadRegister(&p.A, p.A^p.AddrAbsoluteXVal(&cycles, true))
	case 0x5E:
		// LSR a,x
		p.LSR(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x5F:
		// SRE a,x
		p.SRE(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x60:
		// RTS
		p.RTS(&cycles)
	case 0x61:
		// ADC (d,x)
		p.ADC(p.AddrIndirectXVal(&cycles))
	case 0x62:
		// HLT
		p.halted = true
	case 0x63:
		// RRA (d,x)
		p.RRA(&cycles, p.AddrIndirectX(&cycles))
	case 0x64:
		// NOP d
		_ = p.AddrZPVal(&cycles)
	case 0x65:
		// ADC d
		p.ADC(p.AddrZPVal(&cycles))
	case 0x66:
		// ROR d
		p.ROR(&cycles, p.AddrZP(&cycles))
	case 0x67:
		// RRA d
		p.RRA(&cycles, p.AddrZP(&cycles))
	case 0x68:
		// PLA
		p.PLA(&cycles)
	case 0x69:
		// ADC #i
		p.ADC(p.AddrImmediateVal(&cycles))
	case 0x6A:
		// ROR
		p.RORAcc()
	case 0x6B:
		// ARR #i
		p.ARR(p.AddrImmediateVal(&cycles))
	case 0x6C:
		// JMP (a)
		p.PC = p.AddrIndirect(&cycles)
	case 0x6D:
		// ADC a
		p.ADC(p.AddrAbsoluteVal(&cycles))
	case 0x6E:
		// ROR a
		p.ROR(&cycles, p.AddrAbsolute(&cycles))
	case 0x6F:
		// RRA a
		p.RRA(&cycles, p.AddrAbsolute(&cycles))
	case 0x70:
		// BVS *+r
		p.BVS(&cycles)
	case 0x71:
		// ADC (d),y
		p.ADC(p.AddrIndirectYVal(&cycles, true))
	case 0x72:
		// HLT
		p.halted = true
	case 0x73:
		// RRA (d),y
		p.RRA(&cycles, p.AddrIndirectY(&cycles, false))
	case 0x74:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0x75:
		// ADC d,x
		p.ADC(p.AddrZPXVal(&cycles))
	case 0x76:
		// ROR d,x
		p.ROR(&cycles, p.AddrZPX(&cycles))
	case 0x77:
		// RRA d,x
		p.RRA(&cycles, p.AddrZPX(&cycles))
	case 0x78:
		// SEI
		p.P |= P_INTERRUPT
	case 0x79:
		// ADC a,y
		p.ADC(p.AddrAbsoluteYVal(&cycles, true))
	case 0x7A:
		// NOP
	case 0x7B:
		// RRA a,y
		p.RRA(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0x7C:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0x7D:
		// ADC a,x
		p.ADC(p.AddrAbsoluteXVal(&cycles, true))
	case 0x7E:
		// ROR a,x
		p.ROR(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x7F:
		// RRA a,x
		p.RRA(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0x80:
		// NOP #i
		_ = p.AddrImmediateVal(&cycles)
	case 0x81:
		// STA (d,x)
		p.Ram.Write(p.AddrIndirectX(&cycles), p.A)
	case 0x82:
		// NOP #i
		_ = p.AddrImmediateVal(&cycles)
	case 0x83:
		// SAX (d,x)
		p.SAX(p.AddrIndirectX(&cycles))
	case 0x84:
		// STY d
		p.Ram.Write(p.AddrZP(&cycles), p.Y)
	case 0x85:
		// STA d
		p.Ram.Write(p.AddrZP(&cycles), p.A)
	case 0x86:
		// STX d
		p.Ram.Write(p.AddrZP(&cycles), p.X)
	case 0x87:
		// SAX d
		p.SAX(p.AddrZP(&cycles))
	case 0x88:
		// DEY
		p.LoadRegister(&p.Y, p.Y-1)
	case 0x89:
		// NOP #i
		_ = p.AddrImmediateVal(&cycles)
	case 0x8A:
		// TXA
		p.LoadRegister(&p.A, p.X)
	case 0x8B:
		// XAA #i
		p.XAA(p.AddrImmediateVal(&cycles))
	case 0x8C:
		// STY a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.Y)
	case 0x8D:
		// STA a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.A)
	case 0x8E:
		// STX a
		p.Ram.Write(p.AddrAbsolute(&cycles), p.X)
	case 0x8F:
		// SAX a
		p.SAX(p.AddrAbsolute(&cycles))
	case 0x90:
		// BCC *+d
		p.BCC(&cycles)
	case 0x91:
		// STA (d),y
		p.Ram.Write(p.AddrIndirectY(&cycles, false), p.A)
	case 0x92:
		// HLT
		p.halted = true
	case 0x94:
		// STY d,x
		p.Ram.Write(p.AddrZPX(&cycles), p.Y)
	case 0x95:
		// STA d,x
		p.Ram.Write(p.AddrZPX(&cycles), p.A)
	case 0x96:
		// STX d,y
		p.Ram.Write(p.AddrZPY(&cycles), p.X)
	case 0x97:
		// SAX d,y
		p.SAX(p.AddrZPY(&cycles))
	case 0x98:
		// TYA
		p.LoadRegister(&p.A, p.Y)
	case 0x99:
		// STA a,y
		p.Ram.Write(p.AddrAbsoluteY(&cycles, false), p.A)
	case 0x9A:
		// TXS
		p.S = p.X
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
	case 0xA3:
		// LAX (d,x)
		p.LAX(p.AddrIndirectXVal(&cycles))
	case 0xA4:
		// LDY d
		p.LoadRegister(&p.Y, p.AddrZPVal(&cycles))
	case 0xA5:
		// LDA d
		p.LoadRegister(&p.A, p.AddrZPVal(&cycles))
	case 0xA6:
		// LDX d
		p.LoadRegister(&p.X, p.AddrZPVal(&cycles))
	case 0xA7:
		// LAX d
		p.LAX(p.AddrZPVal(&cycles))
	case 0xA8:
		// TAY
		p.LoadRegister(&p.Y, p.A)
	case 0xA9:
		// LDA #i
		p.LoadRegister(&p.A, p.AddrImmediateVal(&cycles))
	case 0xAA:
		// TAX
		p.LoadRegister(&p.X, p.A)
	case 0xAB:
		// OAL #i
		p.OAL(p.AddrImmediateVal(&cycles))
	case 0xAC:
		// LDY a
		p.LoadRegister(&p.Y, p.AddrAbsoluteVal(&cycles))
	case 0xAD:
		// LDA a
		p.LoadRegister(&p.A, p.AddrAbsoluteVal(&cycles))
	case 0xAE:
		// LDX a
		p.LoadRegister(&p.X, p.AddrAbsoluteVal(&cycles))
	case 0xAF:
		// LAX a
		p.LAX(p.AddrAbsoluteVal(&cycles))
	case 0xB0:
		// BCS *+d
		p.BCS(&cycles)
	case 0xB1:
		// LDA (d),y
		p.LoadRegister(&p.A, p.AddrIndirectYVal(&cycles, true))
	case 0xB2:
		// HLT
		p.halted = true
	case 0xB3:
		// LAX (d),y
		p.LAX(p.AddrIndirectYVal(&cycles, true))
	case 0xB4:
		// LDY d,x
		p.LoadRegister(&p.Y, p.AddrZPXVal(&cycles))
	case 0xB5:
		// LDA d,x
		p.LoadRegister(&p.A, p.AddrZPXVal(&cycles))
	case 0xB6:
		// LDX d,y
		p.LoadRegister(&p.X, p.AddrZPYVal(&cycles))
	case 0xB7:
		// LAX d,y
		p.LAX(p.AddrZPYVal(&cycles))
	case 0xB8:
		// CLV
		p.P &^= P_OVERFLOW
	case 0xB9:
		// LDA a,y
		p.LoadRegister(&p.A, p.AddrAbsoluteYVal(&cycles, true))
	case 0xBA:
		// TSX
		p.LoadRegister(&p.X, p.S)
	case 0xBC:
		// LDY a,x
		p.LoadRegister(&p.Y, p.AddrAbsoluteXVal(&cycles, true))
	case 0xBD:
		// LDA a,x
		p.LoadRegister(&p.A, p.AddrAbsoluteXVal(&cycles, true))
	case 0xBE:
		// LDX a,y
		p.LoadRegister(&p.X, p.AddrAbsoluteYVal(&cycles, true))
	case 0xBF:
		// LAX a,y
		p.LAX(p.AddrAbsoluteYVal(&cycles, true))
	case 0xC0:
		// CPY #i
		p.Compare(p.Y, p.AddrImmediateVal(&cycles))
	case 0xC1:
		// CMP (d,x)
		p.Compare(p.A, p.AddrIndirectXVal(&cycles))
	case 0xC2:
		// NOP #i
		_ = p.AddrImmediateVal(&cycles)
	case 0xC3:
		// DCP (d,X)
		p.DCP(&cycles, p.AddrIndirectX(&cycles))
	case 0xC4:
		// CPY d
		p.Compare(p.Y, p.AddrZPVal(&cycles))
	case 0xC5:
		// CMP d
		p.Compare(p.A, p.AddrZPVal(&cycles))
	case 0xC6:
		// DEC d
		p.AdjustMemory(&cycles, NEGATIVE_ONE, p.AddrZP(&cycles))
	case 0xC7:
		// DCP d
		p.DCP(&cycles, p.AddrZP(&cycles))
	case 0xC8:
		// INY
		p.LoadRegister(&p.Y, p.Y+1)
	case 0xC9:
		// CMP #i
		p.Compare(p.A, p.AddrImmediateVal(&cycles))
	case 0xCA:
		// DEX
		p.LoadRegister(&p.X, p.X-1)
	case 0xCB:
		// AXS #i
		p.AXS(p.AddrImmediateVal(&cycles))
	case 0xCC:
		// CPY a
		p.Compare(p.Y, p.AddrAbsoluteVal(&cycles))
	case 0xCD:
		// CMP a
		p.Compare(p.A, p.AddrAbsoluteVal(&cycles))
	case 0xCE:
		// DEC a
		p.AdjustMemory(&cycles, NEGATIVE_ONE, p.AddrAbsolute(&cycles))
	case 0xCF:
		// DCP a
		p.DCP(&cycles, p.AddrAbsolute(&cycles))
	case 0xD0:
		// BNE *+r
		p.BNE(&cycles)
	case 0xD1:
		// CMP (d),y
		p.Compare(p.A, p.AddrIndirectYVal(&cycles, true))
	case 0xD2:
		// HLT
		p.halted = true
	case 0xD3:
		// DCP (d),y
		p.DCP(&cycles, p.AddrIndirectY(&cycles, false))
	case 0xD4:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0xD5:
		// CMP d,x
		p.Compare(p.A, p.AddrZPXVal(&cycles))
	case 0xD6:
		// DEC d,x
		p.AdjustMemory(&cycles, NEGATIVE_ONE, p.AddrZPX(&cycles))
	case 0xD7:
		// DCP d,x
		p.DCP(&cycles, p.AddrZPX(&cycles))
	case 0xD8:
		// CLD
		p.P &^= P_DECIMAL
	case 0xD9:
		// CMP a,y
		p.Compare(p.A, p.AddrAbsoluteYVal(&cycles, true))
	case 0xDA:
		// NOP
	case 0xDB:
		// DCP a,y
		p.DCP(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0xDC:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0xDD:
		// CMP a,x
		p.Compare(p.A, p.AddrAbsoluteXVal(&cycles, true))
	case 0xDE:
		// DEC a,x
		p.AdjustMemory(&cycles, NEGATIVE_ONE, p.AddrAbsoluteX(&cycles, false))
	case 0xDF:
		// DCP a,x
		p.DCP(&cycles, p.AddrAbsoluteX(&cycles, false))
	case 0xE0:
		// CPX #i
		p.Compare(p.X, p.AddrImmediateVal(&cycles))
	case 0xE1:
		// SBC (d,x)
		p.SBC(p.AddrIndirectXVal(&cycles))
	case 0xE2:
		// NOP #i
		_ = p.AddrImmediateVal(&cycles)
	case 0xE3:
		// ISC (d,x)
		p.ISC(&cycles, p.AddrIndirectX(&cycles))
	case 0xE4:
		// CPX d
		p.Compare(p.X, p.AddrZPVal(&cycles))
	case 0xE5:
		// SBC d
		p.SBC(p.AddrZPVal(&cycles))
	case 0xE6:
		// INC d
		p.AdjustMemory(&cycles, 1, p.AddrZP(&cycles))
	case 0xE7:
		// ISC d
		p.ISC(&cycles, p.AddrZP(&cycles))
	case 0xE8:
		// INX
		p.LoadRegister(&p.X, p.X+1)
	case 0xE9:
		// SBC #i
		p.SBC(p.AddrImmediateVal(&cycles))
	case 0xEA:
		// NOP
	case 0xEB:
		// SBC #i
		p.SBC(p.AddrImmediateVal(&cycles))
	case 0xEC:
		// CPX a
		p.Compare(p.X, p.AddrAbsoluteVal(&cycles))
	case 0xED:
		// SBC a
		p.SBC(p.AddrAbsoluteVal(&cycles))
	case 0xEE:
		// INC a
		p.AdjustMemory(&cycles, 1, p.AddrAbsolute(&cycles))
	case 0xEF:
		// ISC a
		p.ISC(&cycles, p.AddrAbsolute(&cycles))
	case 0xF0:
		// BEQ *+d
		p.BEQ(&cycles)
	case 0xF1:
		// SBC (d),y
		p.SBC(p.AddrIndirectYVal(&cycles, true))
	case 0xF2:
		// HLT
		p.halted = true
	case 0xF3:
		// ISC (d),y
		p.ISC(&cycles, p.AddrIndirectY(&cycles, false))
	case 0xF4:
		// NOP d,x
		_ = p.AddrZPXVal(&cycles)
	case 0xF5:
		// SBC d,x
		p.SBC(p.AddrZPXVal(&cycles))
	case 0xF6:
		// INC d,x
		p.AdjustMemory(&cycles, 1, p.AddrZPX(&cycles))
	case 0xF7:
		// ISC d,x
		p.ISC(&cycles, p.AddrZPX(&cycles))
	case 0xF8:
		// SED
		p.P |= P_DECIMAL
	case 0xF9:
		// SBC a,y
		p.SBC(p.AddrAbsoluteYVal(&cycles, true))
	case 0xFA:
		// NOP
	case 0xFB:
		// ISC a,y
		p.ISC(&cycles, p.AddrAbsoluteY(&cycles, false))
	case 0xFC:
		// NOP a,x
		_ = p.AddrAbsoluteXVal(&cycles, true)
	case 0xFD:
		// SBC a,x
		p.SBC(p.AddrAbsoluteXVal(&cycles, true))
	case 0xFE:
		// INC a,x
		p.AdjustMemory(&cycles, 1, p.AddrAbsoluteX(&cycles, false))
	case 0xFF:
		// ISC a,x
		p.ISC(&cycles, p.AddrAbsoluteX(&cycles, false))
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
// (passed as a 16 bit result) caused a carry out by generating a value >= 0x100.
// NOTE: normally this just means masking 0x100 but in some overflow cases for BCD
//       math the value can be 0x200 here so it's still a carry.g
func (p *Processor) CarryCheck(res uint16) {
	if res >= 0x100 {
		p.P |= P_CARRY
	} else {
		p.P &^= P_CARRY
	}
}

// OverflowCheck sets the V flag if the result of the ALU operation
// caused a two's complement sign change.
// Taken from http://www.righto.com/2012/12/the-6502-overflow-flag-explained.html
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
	if p.CpuType == CPU_CMOS || (addr&0xFF+uint16(reg)) > 0x00FF {
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
func (p *Processor) AddrZP(cycles *int) uint16 {
	addr := uint16(0x00FF & p.Ram.Read(p.PC))
	p.PC++
	*cycles++
	return addr
}

// AddrZPVal implements Zero page mode - d
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrZP(cycles))
}

// AddrZPX implements Zero page plus X mode - d,x
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPX(cycles *int) uint16 {
	off := p.Ram.Read(p.PC) + p.X
	addr := uint16(0x00FF & off)
	p.PC++
	*cycles += 2
	return addr
}

// AddrZPXVal implements Zero page plus X mode - d,x
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPXVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrZPX(cycles))
}

// AddrZPY implements Zero page plus Y mode - d,y
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPY(cycles *int) uint16 {
	off := p.Ram.Read(p.PC) + p.Y
	addr := uint16(0x00FF & off)
	p.PC++
	*cycles += 2
	return addr
}

// AddrZPYVal implements Zero page plus Y mode - d,y
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrZPYVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrZPY(cycles))
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
		// loads can vary on NMOS 6502 possibly.
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
		// loads can vary on NMOS 6502 possibly.
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
		// loads can vary on NMOS 6502 possibly.
		*cycles += p.AdjustCycles(base, p.Y)
	}
	return addr
}

// AddrAbsoluteYVal implements absolute plus Y mode - a,x
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrAbsoluteYVal(cycles *int, load bool) uint8 {
	return p.Ram.Read(p.AddrAbsoluteY(cycles, load))
}

// AddrIndirect implements indirect mode - (a)
// and returns the address to be read. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirect(cycles *int) uint16 {
	*cycles += 3
	addr := p.Ram.ReadAddr(p.PC)
	// For CMOS it just takes the next 2 bytes only wrapping on end of RAM
	if addr&0x00FF != 0xFF || p.CpuType == CPU_CMOS {
		return p.Ram.ReadAddr(addr)
	}
	// Otherwise NMOS ones have to page wrap.
	lo := p.Ram.Read(addr)
	hi := p.Ram.Read(addr & 0xFF00)
	return (uint16(hi) << 8) + uint16(lo)
}

// AddrIndirectVal implements indirect mode - (a)
// returning the value at this address. It adjusts the PC and cycle count as needed.
func (p *Processor) AddrIndirectVal(cycles *int) uint8 {
	return p.Ram.Read(p.AddrIndirect(cycles))
}

// LoadRegister takes the val and inserts it into the register passed in. It then does
// Z and N checks against the new value.
func (p *Processor) LoadRegister(reg *uint8, val uint8) {
	*reg = val
	p.ZeroCheck(*reg)
	p.NegativeCheck(*reg)
}

// PushStack pushes the given byte onto the stack and adjusts the stack pointer accordingly.
func (p *Processor) PushStack(cycles *int, val uint8) {
	*cycles++
	p.Ram.Write(0x0100+uint16(p.S), val)
	p.S--
}

// PushPC takes the current PC value and pushes it onto the stack adjusting the stack
// pointer accordingly. It doesn't modify the PC.
func (p *Processor) PushPC(cycles *int) {
	p.PushStack(cycles, uint8((p.PC&0xFF00)>>8))
	p.PushStack(cycles, uint8(p.PC&0x00FF))
}

// PopStack pops the top byte off the stack and adjusts the stack pointer accordingly.
func (p *Processor) PopStack(cycles *int) uint8 {
	*cycles++
	p.S++
	return p.Ram.Read(0x0100 + uint16(p.S))
}

// PopPC pulls a PC value off of the stack and then assigns it to the PC.
func (p *Processor) PopPC(cycles *int) {
	low := p.PopStack(cycles)
	high := p.PopStack(cycles)
	p.PC = (uint16(high) << 8) + uint16(low)
}

// BranchOffset reads the next byte as the branch offset and adjusts PC.
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
	// We only skip if the last instruction didn't. This way a branch always doesn't prevent interrupt processing
	// since real silicon this is what happens (just a delay in the pipelining).
	if *cycles == 3 && !p.prevSkipInterrupt {
		p.skipInterrupt = true
	}
}

// AdjustMemory adds the given adjustment to the memory location given.
// Generally used to implmenet INC/DEC.
func (p *Processor) AdjustMemory(cycles *int, adj uint8, addr uint16) {
	*cycles += 2
	new := p.Ram.Read(addr) + adj
	p.Ram.Write(addr, new)
	p.ZeroCheck(new)
	p.NegativeCheck(new)
}

const BRK = 0x000

func (p *Processor) SetupInterrupt(cycles *int, addr uint16, irq bool) {
	p.PushPC(cycles)
	push := p.P
	// S1 is always set
	push |= P_S1
	// B always set unless this triggered due to IRQ
	push |= P_B
	// http://nesdev.com/6502_cpu.txt claims that if an NMI/IRQ happens
	// on a BRK then B is still set in the pushed flags.
	if irq && p.Ram.Read(p.PC) != BRK {
		push &^= P_B
	}
	p.PushStack(cycles, push)
	*cycles += 2
	if p.CpuType == CPU_CMOS {
		p.P &^= P_DECIMAL
	}
	p.P |= P_INTERRUPT
	p.PC = p.Ram.ReadAddr(addr)
}

// ADC implements the ADC/SBC instructions and sets all associated flags.
// For SBC simply ones-complement the arg before calling.
// NOTE: This doesn't take cycles as ALO operations are done combinatorially on
//       all clocks so don't cost extra cycles.
func (p *Processor) ADC(arg uint8) {
	// Pull the carry bit out which thankfully is the low bit so can be
	// used directly.
	carry := p.P & P_CARRY

	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.CpuType != CPU_NMOS_RICOH {
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
		p.OverflowCheck(p.A, arg, seq)
		p.CarryCheck(sum)
		// TODO(jchacon): CMOS gets N/Z set correctly and needs implementing.
		p.NegativeCheck(seq)
		p.ZeroCheck(bin)
		p.A = res
		return
	}

	// Otherwise do normal binary math.
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
func (p *Processor) ASLAcc() {
	p.CarryCheck(uint16(p.A) << 1)
	p.LoadRegister(&p.A, p.A<<1)
}

// ASL implements the ASL instruction on the given memory location.
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
	// BRK always adds one more to the PC before pushing
	p.PC++
	// PC comes from IRQ_VECTOR
	p.SetupInterrupt(cycles, IRQ_VECTOR, false)
}

// BVC implements the BVC instructions and branches if V is clear.
func (p *Processor) BVC(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_OVERFLOW == 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// BVS implements the BVS instructions and branches if V is set.
func (p *Processor) BVS(cycles *int) {
	offset := p.BranchOffset()
	if p.P&P_OVERFLOW != 0x00 {
		p.PerformBranch(cycles, offset)
	}
}

// Compare implements the logic for all CMP/CPX/CPY instructions and
// sets flags accordingly from the results.
func (p *Processor) Compare(reg uint8, val uint8) {
	p.ZeroCheck(reg - val)
	p.NegativeCheck(reg - val)
	// A-M done as 2's complement addition by ones complement and add 1
	// This way we get valid sign extension and a carry bit test.
	p.CarryCheck(uint16(reg) + uint16(^val) + uint16(1))
}

// JSR implments the JSR instruction for jumping to a new address.
func (p *Processor) JMP(cycles *int) {
	p.PC = p.AddrAbsolute(cycles)
	// JMP doesn't take 2 extra to load, only 1.
	*cycles--
}

// JSR implments the JSR instruction for jumping to a subroutine.
func (p *Processor) JSR(cycles *int, addr uint16) {
	// Adjust PC back so it's correct for pushing as RTS expects.
	p.PC--
	p.PushPC(cycles)
	p.PC = addr
}

// LSRAcc implements the LSR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) LSRAcc() {
	// Get bit0 from A but in a 16 bit value and then shift it up into
	// the carry position
	p.CarryCheck(uint16(p.A&0x01) << 8)
	p.LoadRegister(&p.A, p.A>>1)
}

// LSR implements the LSR instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) LSR(cycles *int, addr uint16) {
	var orig, new uint8
	orig = p.Ram.Read(addr)
	new = orig >> 1
	p.Ram.Write(addr, new)
	// Costs the same as a store operation plus 2 more cycles
	*cycles += 2

	// Get bit0 from orig but in a 16 bit value and then shift it up into
	// the carry position
	p.CarryCheck(uint16(orig&0x01) << 8)
	p.ZeroCheck(new)
	p.NegativeCheck(new)
}

// PLA implements the PLA instruction and pops the stock into the accumulator.
func (p *Processor) PLA(cycles *int) {
	p.LoadRegister(&p.A, p.PopStack(cycles))
	*cycles++
}

// PHP implements the PHP instructions for pushing P onto the stacks.
func (p *Processor) PHP(cycles *int) {
	push := p.P
	// This bit is always set no matter what.
	push |= P_S1

	// TODO(jchacon): Seems NMOS varieties always push B on with PHP but
	//                unsure on CMOS. Verify
	push |= P_B
	p.PushStack(cycles, push)
}

// PLP implements the PLP instruction and pops the stack into the flags.
func (p *Processor) PLP(cycles *int) {
	p.P = p.PopStack(cycles)
	// The actual flags register always has S1 set to one
	p.P |= P_S1
	// And the B bit is never set in the register
	p.P &^= P_B
	*cycles++
}

// ROLAcc implements the ROL instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) ROLAcc() {
	carry := p.P & P_CARRY
	p.CarryCheck(uint16(p.A) << 1)
	p.LoadRegister(&p.A, (p.A<<1)|carry)
}

// ROL implements the ROL instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) ROL(cycles *int, addr uint16) {
	var orig, new uint8
	orig = p.Ram.Read(addr)
	carry := p.P & P_CARRY
	new = (orig << 1) | carry
	p.Ram.Write(addr, new)
	// Costs the same as a store operation plus 2 more cycles
	*cycles += 2

	p.CarryCheck(uint16(orig) << 1)
	p.ZeroCheck(new)
	p.NegativeCheck(new)
}

// RORAcc implements the ROR instruction directly on the accumulator.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) RORAcc() {
	carry := (p.P & P_CARRY) << 7
	// Just see if carry is set or not.
	p.CarryCheck((uint16(p.A) << 8) & 0x0100)
	p.LoadRegister(&p.A, (p.A>>1)|carry)
}

// ROR implements the ROR instruction on the given memory location.
// It then sets all associated flags and adjust cycles as needed.
func (p *Processor) ROR(cycles *int, addr uint16) {
	var orig, new uint8
	orig = p.Ram.Read(addr)
	carry := (p.P & P_CARRY) << 7
	new = (orig >> 1) | carry
	p.Ram.Write(addr, new)
	// Costs the same as a store operation plus 2 more cycles
	*cycles += 2

	// Just see if carry is set or not.
	p.CarryCheck((uint16(orig) << 8) & 0x0100)
	p.ZeroCheck(new)
	p.NegativeCheck(new)
}

// RTI implements the RTI instruction and pops the flags and PC off the stack for returning from an interrupt.
func (p *Processor) RTI(cycles *int) {
	p.PLP(cycles)
	p.PopPC(cycles)
}

// RTS implements the RTS instruction and pops the PC off the stack.
func (p *Processor) RTS(cycles *int) {
	p.PopPC(cycles)
	p.PC++
	*cycles += 2
}

// SBC implements the SBC instruction for both binary and BCD modes (if implemented) and sets all associated flags.
func (p *Processor) SBC(arg uint8) {
	// The Ricoh version didn't implement BCD (used in NES)
	if (p.P&P_DECIMAL) != 0x00 && p.CpuType != CPU_NMOS_RICOH {
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
		p.OverflowCheck(p.A, ^arg, b)
		p.NegativeCheck(b)
		// Yes, could do bit checks here like the hardware but
		// just treating as uint16 math is simpler to code.
		p.CarryCheck(uint16(p.A) + uint16(^arg) + uint16(carry))
		p.ZeroCheck(b)
		p.A = res
		return
	}

	// Otherwise binary mode is just ones complement the arg and ADC.
	p.ADC(^arg)
}

// ALR implements the undocumented opcode for ALR. This does AND #i and then LSR setting all associated flags.
func (p *Processor) ALR(arg uint8) {
	p.LoadRegister(&p.A, p.A&arg)
	p.LSRAcc()
}

// ANC implements the undocumented opcode for ANC. This does AND #i and then sets carry based on bit 7 (sign extend).
func (p *Processor) ANC(arg uint8) {
	p.LoadRegister(&p.A, p.A&arg)
	p.CarryCheck(uint16(p.A) << 1)
}

// ARR implements the undocumented opcode for ARR. This does And #i and then ROR except some flags are set differently.
func (p *Processor) ARR(arg uint8) {
	p.LoadRegister(&p.A, p.A&arg)
	p.RORAcc()
	// C is bit 6
	p.CarryCheck(uint16(p.A) << 2)
	// V is bit 5 ^ bit 6
	if (p.A&0x40)^(p.A^0x20) != 0x00 {
		p.P |= P_OVERFLOW
	} else {
		p.P &^= P_OVERFLOW
	}
}

// AXS implements the undocumented opcode for AXS. (A AND X) - arg (no borrow) setting all associated flags post SBC.
func (p *Processor) AXS(arg uint8) {
	// Save A off to restore later
	a := p.A
	p.LoadRegister(&p.A, p.A&p.X)
	// Carry is always set
	p.P |= P_CARRY
	// Save D & V state since it's always ignored for this but needs to keep values.
	d := p.P & P_DECIMAL
	v := p.P & P_OVERFLOW
	// Clear D so SBC never uses BCD mode (we'll reset it later from saved state).
	p.P &^= P_DECIMAL
	p.SBC(arg)
	// Clear V now in case SBC set it so we can properly restore it below.
	p.P &^= P_OVERFLOW
	// Save A in a temp so we can load registers in the right order to set flags (based on X, not old A)
	x := p.A
	p.LoadRegister(&p.A, a)
	p.LoadRegister(&p.X, x)
	// Restore D & V from our initial state.
	p.P |= d | v
}

// LAX implements the undocumented opcode for LAX. This loads A and X with the same value and sets all associated flags.
func (p *Processor) LAX(arg uint8) {
	p.LoadRegister(&p.A, arg)
	p.LoadRegister(&p.X, arg)
}

// SAX implements the undocumented opcode for SAX. This stores A AND X into the given address. It doesn't change any flags.
func (p *Processor) SAX(addr uint16) {
	t := p.A & p.X
	p.Ram.Write(addr, t)
}

// DCP implements the undocumented opcode for DCP. This decrements the given address and then does a CMP with A setting associated flags.
func (p *Processor) DCP(cycles *int, addr uint16) {
	p.AdjustMemory(cycles, NEGATIVE_ONE, addr)
	p.Compare(p.A, p.Ram.Read(addr))
}

// ISC implements the undocumented opcode for ISC. This increments the given address and then does an SBC with setting associated flags.
func (p *Processor) ISC(cycles *int, addr uint16) {
	p.AdjustMemory(cycles, 1, addr)
	p.SBC(p.Ram.Read(addr))
}

// SLO implements the undocumented opcode for SLO. This does an ASL on the given address and then OR's it against A. Sets flags and carry.
func (p *Processor) SLO(cycles *int, addr uint16) {
	t := p.Ram.Read(addr)
	p.Ram.Write(addr, t<<1)
	// Account for the additional read cycle like AdjustMemory would.
	*cycles += 2
	p.CarryCheck(uint16(t) << 1)
	p.LoadRegister(&p.A, (t<<1)|p.A)
}

// RLA implements the undocumented opcode for RLA. This does a ROL on the given address and then AND's it against A. Sets flags and carry.
func (p *Processor) RLA(cycles *int, addr uint16) {
	t := p.Ram.Read(addr)
	n := t<<1 | (p.P & P_CARRY)
	p.Ram.Write(addr, n)
	// Account for the additional read cycle like AdjustMemory would.
	*cycles += 2
	p.CarryCheck(uint16(t) << 1)
	p.LoadRegister(&p.A, n&p.A)
}

// SRE implements the undocumented opcode for SRE. This does a LSR on the given address and then EOR's it against A. Sets flags and carry.
func (p *Processor) SRE(cycles *int, addr uint16) {
	t := p.Ram.Read(addr)
	p.Ram.Write(addr, t>>1)
	// Account for the additional read cycle like AdjustMemory would.
	*cycles += 2
	// Old bit 0 becomes carry
	p.CarryCheck(uint16(t) << 8)
	p.LoadRegister(&p.A, (t>>1)^p.A)
}

// RRA implements the undocumented opcode for RRA. This does a ROR on the given address and then ADC's it against A. Sets flags and carry.
func (p *Processor) RRA(cycles *int, addr uint16) {
	t := p.Ram.Read(addr)
	n := ((p.P & P_CARRY) << 7) | t>>1
	p.Ram.Write(addr, n)
	// Account for the additional read cycle like AdjustMemory would.
	*cycles += 2
	// Old bit 0 becomes carry
	p.CarryCheck((uint16(t) << 8) & 0x0100)
	p.ADC(p.Ram.Read(addr))
}

// XAA implements the undocumented opcode for XAA. We'll go with http://visual6502.org/wiki/index.php?title=6502_Opcode_8B_(XAA,_ANE)
// for implementation and pick 0xEE as the constant.
func (p *Processor) XAA(arg uint8) {
	p.LoadRegister(&p.A, (p.A|0xEE)&p.X&arg)
}

// OAL implements the undocumented opcode for OAL. This one acts a bit randomly. It somtimes does XAA and sometimes
// does A=X=A&val.
func (p *Processor) OAL(arg uint8) {
	if rand.Float32() >= 0.5 {
		p.XAA(arg)
		return
	}
	v := p.A & arg
	p.LoadRegister(&p.A, v)
	p.LoadRegister(&p.X, v)
}
