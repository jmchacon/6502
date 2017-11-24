// disassenble takes a filename and load's it and then
// disassembles it to stdout starting at the first instruction.
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/jmchacon/6502/cpu"
)

// flatMemory implements the RAM interface
type flatMemory struct {
	addr []uint8
}

func (r *flatMemory) Read(addr uint16) uint8 {
	if addr >= uint16(len(r.addr)) {
		return 0x00
	}
	return r.addr[addr]
}

func (r *flatMemory) ReadAddr(addr uint16) uint16 {
	if addr >= uint16(len(r.addr)) || addr+1 >= uint16(len(r.addr)) {
		return 0x0000
	}
	return (uint16(r.addr[addr+1]) << 8) + uint16(r.addr[addr])
}

func (r *flatMemory) ReadZPAddr(addr uint8) uint16 {
	if addr >= uint8(len(r.addr)) || addr+1 >= uint8(len(r.addr)) {
		return 0x0000
	}
	return (uint16(r.addr[addr+1]) << 8) + uint16(r.addr[addr])
}

func (r *flatMemory) Write(addr uint16, val uint8) {}

func (r *flatMemory) Reset()   {}
func (r *flatMemory) PowerOn() {}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Invalid command: %s <filename>", os.Args[0])
	}
	fn := os.Args[1]
	f := &flatMemory{}
	var err error
	f.addr, err = ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("Can't open %s - %v", fn, err)
	}
	l := len(f.addr)
	if l > 65536 {
		log.Printf("Length %d too long, truncating to 64k", l)
		f.addr = f.addr[:65536]
	}
	fmt.Printf("%.2X bytes\n", l)
	for pc := 0; pc < (l - 1); {
		dis, off := cpu.Disassemble(uint16(pc), f)
		pc += off
		fmt.Printf("%s\n", dis)
	}
}
