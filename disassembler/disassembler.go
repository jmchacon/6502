// disassenble takes a filename and load's it and then
// disassembles it to stdout starting at the first instruction.
// If the filename ends in .prg (case insensitive) it will assume
// this is a C64 program file and use the first 2 bytes as the load
// address. If the load address is 0x0801 it will then assume it's
// BASIC program and start listing it until it ends. At that point it'll
// disassemble until the end of the load address space.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/jmchacon/6502/c64basic"
	"github.com/jmchacon/6502/disassemble"
)

// flatMemory implements the RAM interface
type flatMemory struct {
	addr [65536]uint8
}

func (r *flatMemory) Read(addr uint16) uint8 {
	return r.addr[addr]
}

func (r *flatMemory) Write(addr uint16, val uint8) {}
func (r *flatMemory) PowerOn() {}

var (
	startPC = flag.Int("start_pc", 0x0000, "PC value to start disassembling")
	offset  = flag.Int("offset", 0x0000, "Offset into RAM to start loading data. All other RAM will be zero'd out. Ignored for PRG files.")
)

func main() {
	flag.Parse()
	if len(flag.Args()) != 1 {
		log.Fatalf("Invalid command: %s [-start_pc <PC> -offset <offset>] <filename>", os.Args[0])
	}
	fn := flag.Args()[0]

	// Check if this is a c64 binary.
	c64 := false
	parts := strings.Split(fn, ".")
	suffix := strings.ToLower(parts[len(parts)-1])
	if suffix == "prg" {
		c64 = true
		fmt.Println("C64 program file")
	}

	f := &flatMemory{}
	f.PowerOn()
	var err error
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("Can't open %s - %v", fn, err)
	}
	pc := uint16(*startPC)
	if c64 {
		// We're supplied with the load offset instead of using the flag (which we'll override).
		*offset = int((uint16(b[1]) << 8) + uint16(b[0]))
		pc = uint16(*offset)
		*startPC = int(pc)
		b = b[2:]
	}
	max := 65536 - *offset
	if l := len(b); l > max {
		log.Printf("Length %d at offset %d too long, truncating to 64k", l, *offset)
		b = b[:max]
	}
	fmt.Printf("0x%.2X bytes at pc: %.4X\n", len(b), pc)
	copy(f.addr[*offset:], b)
	if c64 && *offset == 0x0801 {
		// Start with basic first
		for {
			out, newPC, err := c64basic.List(pc, f)
			if newPC == 0x0000 {
				// Account for 3 NULs indicating end of program
				pc += 2
				fmt.Printf("PC: %.4X\n", pc)
				break
			}
			fmt.Printf("%.4X %s\n", pc, out)
			if err != nil {
				fmt.Printf("%v", err)
				os.Exit(1)
			}
			pc = newPC
		}
	}
	cnt := 0
	// Can't base it on PC since it may rollover so just disassemble until we run out of buffer.
	for cnt < len(b) {
		dis, off := disassemble.Step(pc, f)
		pc += uint16(off)
		cnt += off
		fmt.Printf("%s\n", dis)
	}
}
