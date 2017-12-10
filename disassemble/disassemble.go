// disassenble takes a filename and load's it and then
// disassembles it to stdout starting at the first instruction.
package main

import (
	"flag"
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

var (
	startPC = flag.Int("start_pc", 0x0000, "PC value to start disassembling")
	offset  = flag.Int("offset", 0x0000, "Offset into RAM to start loading data. All other RAM will be zero'd out.")
)

func main() {
	flag.Parse()
	if len(flag.Args()) != 1 {
		log.Fatalf("Invalid command: %s [-start_pc <PC> -offset <offset>] <filename>", os.Args[0])
	}
	fn := flag.Args()[0]
	f := &flatMemory{}
	var err error
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("Can't open %s - %v", fn, err)
	}
	for i := 0; i < *offset; i++ {
		f.addr = append(f.addr, 0)
	}
	f.addr = append(f.addr, b...)
	l := len(b)
	if l > 65536-*offset {
		log.Printf("Length %d at offset %d too long, truncating to 64k", l, *offset)
		f.addr = f.addr[:65536-*offset]
	}
	fmt.Printf("0x%.2X bytes\n", l)
	for pc := *startPC; pc < (*startPC + l); {
		dis, off := cpu.Disassemble(uint16(pc), f)
		pc += off
		fmt.Printf("%s\n", dis)
	}
}
