// Package disassemble implements a disassembler for 6502 opcodes
package disassemble

import (
	"fmt"

	"github.com/jmchacon/6502/memory"
)

const (
	kMODE_IMMEDIATE = iota
	kMODE_ZP
	kMODE_ZPX
	kMODE_ZPY
	kMODE_INDIRECTX
	kMODE_INDIRECTY
	kMODE_ABSOLUTE
	kMODE_ABSOLUTEX
	kMODE_ABSOLUTEY
	kMODE_INDIRECT
	kMODE_IMPLIED
	kMODE_RELATIVE
)

// Step will take the given PC value and disassemble the instruction at that location
// returning a string for the disassembly and the bytes forward the PC should move to get to
// the next instruction. This does not interpret the instructions so LDA, JMP, LDA in memory
// will disassemble as that sequence and not follow the JMP.
// This always reads at least one byte past the current PC so make sure that address is valid.
func Step(pc uint16, r memory.Ram) (string, int) {
	// All instructions read a 2nd byte generally so just do that now.
	pc1 := r.Read(pc + 1)
	// Setup a 16 bit value so it can be added the the PC for branch offsets.
	// Sign extend it as needed.
	pc116 := uint16(int16(int8(pc1)))
	// And preread the 2nd byte for 3 byte instructions.
	pc2 := r.Read(pc + 2)

	// When done this will have op and the mode determined and byte count
	var op string
	mode := kMODE_IMPLIED
	o := r.Read(pc)
	switch o {
	case 0x00:
		op = "BRK"
		mode = kMODE_IMMEDIATE // Ok, not really but the byte after BRK is read and skipped.
	case 0x01:
		op = "ORA"
		mode = kMODE_INDIRECTX
	case 0x02:
		op = "HLT"
	case 0x03:
		op = "SLO"
		mode = kMODE_INDIRECTX
	case 0x04:
		op = "NOP"
		mode = kMODE_ZP
	case 0x05:
		op = "ORA"
		mode = kMODE_ZP
	case 0x06:
		op = "ASL"
		mode = kMODE_ZP
	case 0x07:
		op = "SLO"
		mode = kMODE_ZP
	case 0x08:
		op = "PHP"
	case 0x09:
		op = "ORA"
		mode = kMODE_IMMEDIATE
	case 0x0A:
		op = "ASL"
	case 0x0B:
		op = "ANC"
		mode = kMODE_IMMEDIATE
	case 0x0C:
		op = "NOP"
		mode = kMODE_ABSOLUTE
	case 0x0D:
		op = "ORA"
		mode = kMODE_ABSOLUTE
	case 0x0E:
		op = "ASL"
		mode = kMODE_ABSOLUTE
	case 0x0F:
		op = "SLO"
		mode = kMODE_ABSOLUTE
	case 0x10:
		op = "BPL"
		mode = kMODE_RELATIVE
	case 0x11:
		op = "ORA"
		mode = kMODE_INDIRECTY
	case 0x12:
		op = "HLT"
	case 0x13:
		op = "SLO"
		mode = kMODE_INDIRECTY
	case 0x14:
		op = "NOP"
		mode = kMODE_ZPX
	case 0x15:
		op = "ORA"
		mode = kMODE_ZPX
	case 0x16:
		op = "ASL"
		mode = kMODE_ZPX
	case 0x17:
		op = "SLO"
		mode = kMODE_ZPX
	case 0x18:
		op = "CLC"
	case 0x19:
		op = "ORA"
		mode = kMODE_ABSOLUTEY
	case 0x1A:
		op = "NOP"
	case 0x1B:
		op = "SLO"
		mode = kMODE_ABSOLUTEY
	case 0x1C:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0x1D:
		op = "ORA"
		mode = kMODE_ABSOLUTEX
	case 0x1E:
		op = "ASL"
		mode = kMODE_ABSOLUTEX
	case 0x1F:
		op = "SLO"
		mode = kMODE_ABSOLUTEX
	case 0x20:
		op = "JSR"
		mode = kMODE_ABSOLUTE
	case 0x21:
		op = "AND"
		mode = kMODE_INDIRECTX
	case 0x22:
		op = "HLT"
	case 0x23:
		op = "RLA"
		mode = kMODE_INDIRECTX
	case 0x24:
		op = "BIT"
		mode = kMODE_ZP
	case 0x25:
		op = "AND"
		mode = kMODE_ZP
	case 0x26:
		op = "ROL"
		mode = kMODE_ZP
	case 0x27:
		op = "RLA"
		mode = kMODE_ZP
	case 0x28:
		op = "PLP"
	case 0x29:
		op = "AND"
		mode = kMODE_IMMEDIATE
	case 0x2A:
		op = "ROL"
	case 0x2B:
		op = "ANC"
		mode = kMODE_IMMEDIATE
	case 0x2C:
		op = "BIT"
		mode = kMODE_ABSOLUTE
	case 0x2D:
		op = "AND"
		mode = kMODE_ABSOLUTE
	case 0x2E:
		op = "ROL"
		mode = kMODE_ABSOLUTE
	case 0x2F:
		op = "RLA"
		mode = kMODE_ABSOLUTE
	case 0x30:
		op = "BMI"
		mode = kMODE_RELATIVE
	case 0x31:
		op = "AND"
		mode = kMODE_INDIRECTY
	case 0x32:
		op = "HLT"
	case 0x33:
		op = "RLA"
		mode = kMODE_INDIRECTY
	case 0x34:
		op = "NOP"
		mode = kMODE_ZPX
	case 0x35:
		op = "AND"
		mode = kMODE_ZPX
	case 0x36:
		op = "ROL"
		mode = kMODE_ZPX
	case 0x37:
		op = "RLA"
		mode = kMODE_ZPX
	case 0x38:
		op = "SEC"
	case 0x39:
		op = "AND"
		mode = kMODE_ABSOLUTEY
	case 0x3A:
		op = "NOP"
	case 0x3B:
		op = "RLA"
		mode = kMODE_ABSOLUTEY
	case 0x3C:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0x3D:
		op = "AND"
		mode = kMODE_ABSOLUTEX
	case 0x3E:
		op = "ROL"
		mode = kMODE_ABSOLUTEX
	case 0x3F:
		op = "RLA"
		mode = kMODE_ABSOLUTEX
	case 0x40:
		op = "RTI"
	case 0x41:
		op = "EOR"
		mode = kMODE_INDIRECTX
	case 0x42:
		op = "HLT"
	case 0x43:
		op = "SRE"
		mode = kMODE_INDIRECTX
	case 0x44:
		op = "NOP"
		mode = kMODE_ZP
	case 0x45:
		op = "EOR"
		mode = kMODE_ZP
	case 0x46:
		op = "LSR"
		mode = kMODE_ZP
	case 0x47:
		op = "SRE"
		mode = kMODE_ZP
	case 0x48:
		op = "PHA"
	case 0x49:
		op = "EOR"
		mode = kMODE_IMMEDIATE
	case 0x4A:
		op = "LSR"
	case 0x4B:
		op = "ALR"
		mode = kMODE_IMMEDIATE
	case 0x4C:
		op = "JMP"
		mode = kMODE_ABSOLUTE
	case 0x4D:
		op = "EOR"
		mode = kMODE_ABSOLUTE
	case 0x4E:
		op = "LSR"
		mode = kMODE_ABSOLUTE
	case 0x4F:
		op = "SRE"
		mode = kMODE_ABSOLUTE
	case 0x50:
		op = "BVC"
		mode = kMODE_RELATIVE
	case 0x51:
		op = "EOR"
		mode = kMODE_INDIRECTY
	case 0x52:
		op = "HLT"
	case 0x53:
		op = "SRE"
		mode = kMODE_INDIRECTY
	case 0x54:
		op = "NOP"
		mode = kMODE_ZPX
	case 0x55:
		op = "EOR"
		mode = kMODE_ZPX
	case 0x56:
		op = "LSR"
		mode = kMODE_ZPX
	case 0x57:
		op = "SRE"
		mode = kMODE_ZPX
	case 0x58:
		op = "CLI"
	case 0x59:
		op = "EOR"
		mode = kMODE_ABSOLUTEY
	case 0x5A:
		op = "NOP"
	case 0x5B:
		op = "SRE"
		mode = kMODE_ABSOLUTEY
	case 0x5C:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0x5D:
		op = "EOR"
		mode = kMODE_ABSOLUTEX
	case 0x5E:
		op = "LSR"
		mode = kMODE_ABSOLUTEX
	case 0x5F:
		op = "SRE"
		mode = kMODE_ABSOLUTEX
	case 0x60:
		op = "RTS"
	case 0x61:
		op = "ADC"
		mode = kMODE_INDIRECTX
	case 0x62:
		op = "HLT"
	case 0x63:
		op = "RRA"
		mode = kMODE_INDIRECTX
	case 0x64:
		op = "NOP"
		mode = kMODE_ZP
	case 0x65:
		op = "ADC"
		mode = kMODE_ZP
	case 0x66:
		op = "ROR"
		mode = kMODE_ZP
	case 0x67:
		op = "RRA"
		mode = kMODE_ZP
	case 0x68:
		op = "PLA"
	case 0x69:
		op = "ADC"
		mode = kMODE_IMMEDIATE
	case 0x6A:
		op = "ROR"
	case 0x6B:
		op = "ARR"
		mode = kMODE_IMMEDIATE
	case 0x6C:
		op = "JMP"
		mode = kMODE_INDIRECT
	case 0x6D:
		op = "ADC"
		mode = kMODE_ABSOLUTE
	case 0x6E:
		op = "ROR"
		mode = kMODE_ABSOLUTE
	case 0x6F:
		op = "RRA"
		mode = kMODE_ABSOLUTE
	case 0x70:
		op = "BVS"
		mode = kMODE_RELATIVE
	case 0x71:
		op = "ADC"
		mode = kMODE_INDIRECTY
	case 0x72:
		op = "HLT"
	case 0x73:
		op = "RRA"
		mode = kMODE_INDIRECTY
	case 0x74:
		op = "NOP"
		mode = kMODE_ZPX
	case 0x75:
		op = "ADC"
		mode = kMODE_ZPX
	case 0x76:
		op = "ROR"
		mode = kMODE_ZPX
	case 0x77:
		op = "RRA"
		mode = kMODE_ZPX
	case 0x78:
		op = "SEI"
	case 0x79:
		op = "ADC"
		mode = kMODE_ABSOLUTEY
	case 0x7A:
		op = "NOP"
	case 0x7B:
		op = "RRA"
		mode = kMODE_ABSOLUTEY
	case 0x7C:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0x7D:
		op = "ADC"
		mode = kMODE_ABSOLUTEX
	case 0x7E:
		op = "ROR"
		mode = kMODE_ABSOLUTEX
	case 0x7F:
		op = "RRA"
		mode = kMODE_ABSOLUTEX
	case 0x80:
		op = "NOP"
		mode = kMODE_IMMEDIATE
	case 0x81:
		op = "STA"
		mode = kMODE_INDIRECTX
	case 0x82:
		op = "NOP"
		mode = kMODE_IMMEDIATE
	case 0x83:
		op = "SAX"
		mode = kMODE_INDIRECTX
	case 0x84:
		op = "STY"
		mode = kMODE_ZP
	case 0x85:
		op = "STA"
		mode = kMODE_ZP
	case 0x86:
		op = "STX"
		mode = kMODE_ZP
	case 0x87:
		op = "SAX"
		mode = kMODE_ZP
	case 0x88:
		op = "DEY"
	case 0x89:
		op = "NOP"
		mode = kMODE_IMMEDIATE
	case 0x8A:
		op = "TXA"
	case 0x8B:
		op = "XAA"
		mode = kMODE_IMMEDIATE
	case 0x8C:
		op = "STY"
		mode = kMODE_ABSOLUTE
	case 0x8D:
		op = "STA"
		mode = kMODE_ABSOLUTE
	case 0x8E:
		op = "STX"
		mode = kMODE_ABSOLUTE
	case 0x8F:
		op = "SAX"
		mode = kMODE_ABSOLUTE
	case 0x90:
		op = "BCC"
		mode = kMODE_RELATIVE
	case 0x91:
		op = "STA"
		mode = kMODE_INDIRECTY
	case 0x92:
		op = "HLT"
	case 0x94:
		op = "STY"
		mode = kMODE_ZPX
	case 0x95:
		op = "STA"
		mode = kMODE_ZPX
	case 0x96:
		op = "STX"
		mode = kMODE_ZPY
	case 0x97:
		op = "SAX"
		mode = kMODE_ZPY
	case 0x98:
		op = "TYA"
	case 0x99:
		op = "STA"
		mode = kMODE_ABSOLUTEY
	case 0x9A:
		op = "TXS"
	case 0x9D:
		op = "STA"
		mode = kMODE_ABSOLUTEX
	case 0xA0:
		op = "LDY"
		mode = kMODE_IMMEDIATE
	case 0xA1:
		op = "LDA"
		mode = kMODE_INDIRECTX
	case 0xA2:
		op = "LDX"
		mode = kMODE_IMMEDIATE
	case 0xA3:
		op = "LAX"
		mode = kMODE_INDIRECTX
	case 0xA4:
		op = "LDY"
		mode = kMODE_ZP
	case 0xA5:
		op = "LDA"
		mode = kMODE_ZP
	case 0xA6:
		op = "LDX"
		mode = kMODE_ZP
	case 0xA7:
		op = "LAX"
		mode = kMODE_ZP
	case 0xA8:
		op = "TAY"
	case 0xA9:
		op = "LDA"
		mode = kMODE_IMMEDIATE
	case 0xAA:
		op = "TAX"
	case 0xAB:
		op = "OAL"
		mode = kMODE_IMMEDIATE
	case 0xAC:
		op = "LDY"
		mode = kMODE_ABSOLUTE
	case 0xAD:
		op = "LDA"
		mode = kMODE_ABSOLUTE
	case 0xAE:
		op = "LDX"
		mode = kMODE_ABSOLUTE
	case 0xAF:
		op = "LAX"
		mode = kMODE_ABSOLUTE
	case 0xB0:
		op = "BCS"
		mode = kMODE_RELATIVE
	case 0xB1:
		op = "LDA"
		mode = kMODE_INDIRECTY
	case 0xB2:
		op = "HLT"
	case 0xB3:
		op = "LAX"
		mode = kMODE_INDIRECTY
	case 0xB4:
		op = "LDY"
		mode = kMODE_ZPX
	case 0xB5:
		op = "LDA"
		mode = kMODE_ZPX
	case 0xB6:
		op = "LDX"
		mode = kMODE_ZPY
	case 0xB7:
		op = "LAX"
		mode = kMODE_ZPY
	case 0xB8:
		op = "CLV"
	case 0xB9:
		op = "LDA"
		mode = kMODE_ABSOLUTEY
	case 0xBA:
		op = "TSX"
	case 0xBC:
		op = "LDY"
		mode = kMODE_ABSOLUTEX
	case 0xBD:
		op = "LDA"
		mode = kMODE_ABSOLUTEX
	case 0xBE:
		op = "LDX"
		mode = kMODE_ABSOLUTEY
	case 0xBF:
		op = "LAX"
		mode = kMODE_ABSOLUTEY
	case 0xC0:
		op = "CPY"
		mode = kMODE_IMMEDIATE
	case 0xC1:
		op = "CMP"
		mode = kMODE_INDIRECTX
	case 0xC2:
		op = "NOP"
		mode = kMODE_IMMEDIATE
	case 0xC3:
		op = "DCP"
		mode = kMODE_INDIRECTX
	case 0xC4:
		op = "CPY"
		mode = kMODE_ZP
	case 0xC5:
		op = "CMP"
		mode = kMODE_ZP
	case 0xC6:
		op = "DEC"
		mode = kMODE_ZP
	case 0xC7:
		op = "DCP"
		mode = kMODE_ZP
	case 0xC8:
		op = "INY"
	case 0xC9:
		op = "CMP"
		mode = kMODE_IMMEDIATE
	case 0xCA:
		op = "DEX"
	case 0xCB:
		op = "AXS"
		mode = kMODE_IMMEDIATE
	case 0xCC:
		op = "CPY"
		mode = kMODE_ABSOLUTE
	case 0xCD:
		op = "CMP"
		mode = kMODE_ABSOLUTE
	case 0xCE:
		op = "DEC"
		mode = kMODE_ABSOLUTE
	case 0xCF:
		op = "DCP"
		mode = kMODE_ABSOLUTE
	case 0xD0:
		op = "BNE"
		mode = kMODE_RELATIVE
	case 0xD1:
		op = "CMP"
		mode = kMODE_INDIRECTY
	case 0xD2:
		op = "HLT"
	case 0xD3:
		op = "DCP"
		mode = kMODE_INDIRECTY
	case 0xD4:
		op = "NOP"
		mode = kMODE_ZPX
	case 0xD5:
		op = "CMP"
		mode = kMODE_ZPX
	case 0xD6:
		op = "DEC"
		mode = kMODE_ZPX
	case 0xD7:
		op = "DCP"
		mode = kMODE_ZPX
	case 0xD8:
		op = "CLD"
	case 0xD9:
		op = "CMP"
		mode = kMODE_ABSOLUTEY
	case 0xDA:
		op = "NOP"
	case 0xDB:
		op = "DCP"
		mode = kMODE_ABSOLUTEY
	case 0xDC:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0xDD:
		op = "CMP"
		mode = kMODE_ABSOLUTEX
	case 0xDE:
		op = "DEC"
		mode = kMODE_ABSOLUTEX
	case 0xDF:
		op = "DCP"
		mode = kMODE_ABSOLUTEX
	case 0xE0:
		op = "CPX"
		mode = kMODE_IMMEDIATE
	case 0xE1:
		op = "SBC"
		mode = kMODE_INDIRECTX
	case 0xE2:
		op = "NOP"
		mode = kMODE_IMMEDIATE
	case 0xE3:
		op = "ISC"
		mode = kMODE_INDIRECTX
	case 0xE4:
		op = "CPX"
		mode = kMODE_ZP
	case 0xE5:
		op = "SBC"
		mode = kMODE_ZP
	case 0xE6:
		op = "INC"
		mode = kMODE_ZP
	case 0xE7:
		op = "ISC"
		mode = kMODE_ZP
	case 0xE8:
		op = "INX"
	case 0xE9:
		op = "SBC"
		mode = kMODE_IMMEDIATE
	case 0xEA:
		op = "NOP"
	case 0xEB:
		op = "SBC"
		mode = kMODE_IMMEDIATE
	case 0xEC:
		op = "CPX"
		mode = kMODE_ABSOLUTE
	case 0xED:
		op = "SBC"
		mode = kMODE_ABSOLUTE
	case 0xEE:
		op = "INC"
		mode = kMODE_ABSOLUTE
	case 0xEF:
		op = "ISC"
		mode = kMODE_ABSOLUTE
	case 0xF0:
		op = "BEQ"
		mode = kMODE_RELATIVE
	case 0xF1:
		op = "SBC"
		mode = kMODE_INDIRECTY
	case 0xF2:
		op = "HLT"
	case 0xF3:
		op = "ISC"
		mode = kMODE_INDIRECTY
	case 0xF4:
		op = "NOP"
		mode = kMODE_ZPX
	case 0xF5:
		op = "SBC"
		mode = kMODE_ZPX
	case 0xF6:
		op = "INC"
		mode = kMODE_ZPX
	case 0xF7:
		op = "ISC"
		mode = kMODE_ZPX
	case 0xF8:
		op = "SED"
	case 0xF9:
		op = "SBC"
		mode = kMODE_ABSOLUTEY
	case 0xFA:
		op = "NOP"
	case 0xFB:
		op = "ISC"
		mode = kMODE_ABSOLUTEY
	case 0xFC:
		op = "NOP"
		mode = kMODE_ABSOLUTEX
	case 0xFD:
		op = "SBC"
		mode = kMODE_ABSOLUTEX
	case 0xFE:
		op = "INC"
		mode = kMODE_ABSOLUTEX
	case 0xFF:
		op = "ISC"
		mode = kMODE_ABSOLUTEX
	default:
		op = "UNIMPLEMENTED"
	}

	count := 2 // Default byte count, adjusted below.
	out := fmt.Sprintf("%.4X %.2X ", pc, o)
	switch mode {
	case kMODE_IMMEDIATE:
		out += fmt.Sprintf("%.2X      %s #%.2X       ", pc1, op, pc1)
	case kMODE_ZP:
		out += fmt.Sprintf("%.2X      %s %.2X        ", pc1, op, pc1)
	case kMODE_ZPX:
		out += fmt.Sprintf("%.2X      %s %.2X,X      ", pc1, op, pc1)
	case kMODE_ZPY:
		out += fmt.Sprintf("%.2X      %s %.2X,Y      ", pc1, op, pc1)
	case kMODE_INDIRECTX:
		out += fmt.Sprintf("%.2X      %s (%.2X,X)    ", pc1, op, pc1)
	case kMODE_INDIRECTY:
		out += fmt.Sprintf("%.2X      %s (%.2X),Y    ", pc1, op, pc1)
	case kMODE_ABSOLUTE:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X      ", pc1, pc2, op, pc2, pc1)
		count++
	case kMODE_ABSOLUTEX:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X,X    ", pc1, pc2, op, pc2, pc1)
		count++
	case kMODE_ABSOLUTEY:
		out += fmt.Sprintf("%.2X %.2X   %s %.2X%.2X,Y    ", pc1, pc2, op, pc2, pc1)
		count++
	case kMODE_INDIRECT:
		out += fmt.Sprintf("%.2X %.2X   %s (%.2X%.2X)    ", pc1, pc2, op, pc2, pc1)
		count++
	case kMODE_IMPLIED:
		out += fmt.Sprintf("        %s           ", op)
		count--
	case kMODE_RELATIVE:
		out += fmt.Sprintf("%.2X      %s %.2X (%.4X) ", pc1, op, pc1, pc+pc116+2)
	default:
		panic(fmt.Sprintf("Invalid mode: %d", mode))
	}
	return out, count
}
