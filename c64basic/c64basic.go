// c64basic dissassembles a commodore 64 basic
// program assuming that it's loaded at 0x0801
// in the memory area passed in.
package c64basic

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/jmchacon/6502/memory"
)

func readAddr(r memory.Bank, addr uint16) uint16 {
	return (uint16(r.Read(addr+1)) << 8) + uint16(r.Read(addr))
}

// List will take the given PC value and disassembles the Basic line at that location
// returning a string for the line and the PC of the next line. This does no sanity
// checking so a basic program which points to itself for listing will infinite loop
// if the PC values passed in aren't compared for loops.
// On a normal program end (next addr == 0x0000) it will return an empty string and PC of 0x0000.
// If there is a token parsing problem an error is returned instead with as much of the
// line as would tokenize. Normally a c64 won't continue so the newPC value here will be 0.
// NOTE: This returns the ASCII characters as parsed, displaying in PETSCII is up to the caller
//       to determine.
func List(pc uint16, r memory.Bank) (string, uint16, error) {
	// First entry is the linked list pointer to the next line
	newPC := readAddr(r, pc)
	pc += 2
	// Return an empty string and PC = 0x0000 for end of program.
	if newPC == 0x0000 {
		return "", 0x0000, nil
	}

	// Next 2 are line number also stored in little endian so we can just use readAddr again.
	lineNum := readAddr(r, pc)
	pc += 2

	// This is going to be built up as we read tokens so don't use strings directly.
	var b bytes.Buffer

	// Write the line number
	b.WriteString(fmt.Sprintf("%d ", lineNum))

	// Read until we reach a NUL indicating EOL.
	for {
		tok := r.Read(pc)
		pc++
		if tok == 0x00 {
			break
		}
		// Only defined for 0x00-0xCB (below 0x80 is just ascii chars)
		if tok > 0xCB {
			return b.String(), 0, errors.New("?SYNTAX  ERROR")
		}
		var t string
		switch tok {
		case 0x80:
			t = "END"
		case 0x81:
			t = "FOR"
		case 0x82:
			t = "NEXT"
		case 0x83:
			t = "DATA"
		case 0x84:
			t = "INPUT#"
		case 0x85:
			t = "INPUT"
		case 0x86:
			t = "DIM"
		case 0x87:
			t = "READ"
		case 0x88:
			t = "LET"
		case 0x89:
			t = "GOTO"
		case 0x8A:
			t = "RUN"
		case 0x8B:
			t = "IF"
		case 0x8C:
			t = "RESTORE"
		case 0x8D:
			t = "GOSUB"
		case 0x8E:
			t = "RETURN"
		case 0x8F:
			t = "REM"
		case 0x90:
			t = "STOP"
		case 0x91:
			t = "ON"
		case 0x92:
			t = "WAIT"
		case 0x93:
			t = "LOAD"
		case 0x94:
			t = "SAVE"
		case 0x95:
			t = "VERIFY"
		case 0x96:
			t = "DEF"
		case 0x97:
			t = "POKE"
		case 0x98:
			t = "PRINT#"
		case 0x99:
			t = "PRINT"
		case 0x9A:
			t = "CONT"
		case 0x9B:
			t = "LIST"
		case 0x9C:
			t = "CLR"
		case 0x9D:
			t = "CMD"
		case 0x9E:
			t = "SYS"
		case 0x9F:
			t = "OPEN"
		case 0xA0:
			t = "CLOSE"
		case 0xA1:
			t = "GET"
		case 0xA2:
			t = "NEW"
		case 0xA3:
			t = "TAB("
		case 0xA4:
			t = "TO"
		case 0xA5:
			t = "FN"
		case 0xA6:
			t = "SPC("
		case 0xA7:
			t = "THEN"
		case 0xA8:
			t = "NOT"
		case 0xA9:
			t = "STEP"
		case 0xAA:
			t = "+"
		case 0xAB:
			t = "−"
		case 0xAC:
			t = "*"
		case 0xAD:
			t = "/"
		case 0xAE:
			t = "^"
		case 0xAF:
			t = "AND"
		case 0xB0:
			t = "OR"
		case 0xB1:
			t = ">"
		case 0xB2:
			t = "="
		case 0xB3:
			t = "<"
		case 0xB4:
			t = "SGN"
		case 0xB5:
			t = "INT"
		case 0xB6:
			t = "ABS"
		case 0xB7:
			t = "USR"
		case 0xB8:
			t = "FRE"
		case 0xB9:
			t = "POS"
		case 0xBA:
			t = "SQR"
		case 0xBB:
			t = "RND"
		case 0xBC:
			t = "LOG"
		case 0xBD:
			t = "EXP"
		case 0xBE:
			t = "COS"
		case 0xBF:
			t = "SIN"
		case 0xC0:
			t = "TAN"
		case 0xC1:
			t = "ATN"
		case 0xC2:
			t = "PEEK"
		case 0xC3:
			t = "LEN"
		case 0xC4:
			t = "STR$"
		case 0xC5:
			t = "VAL"
		case 0xC6:
			t = "ASC"
		case 0xC7:
			t = "CHR$"
		case 0xC8:
			t = "LEFT$"
		case 0xC9:
			t = "RIGHT$"
		case 0xCA:
			t = "MID$"
		case 0xCB:
			t = "GO"
		default:
			t = fmt.Sprintf("%c", tok)
		}
		b.WriteString(t)
	}
	return b.String(), newPC, nil
}
