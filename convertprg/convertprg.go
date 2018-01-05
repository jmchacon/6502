// convertprg takes a C64 style PRG file
// and converts it into a 64k bin image for
// running as a test cart.
// This assumes exection will start at 0xD000
// which will then JSR to the start PC given.
// BRK/IRQ/NMI vectors will all point at 0xC000
// which simply infinite loops.
//
// The output file is named after the input with .bin
// appended onto the end.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

var (
	startPC = flag.Int("start_pc", 0x0000, "PC value to start execution")
)

func main() {
	flag.Parse()
	if len(flag.Args()) != 1 {
		log.Fatalf("Invalid command: %s --start_pc=XXXX <filename>", os.Args[0])
	}
	if *startPC < 0 || *startPC > 65535 {
		log.Fatal("--start_pc out of range. Must be between 0-65535")
	}
	fn := flag.Args()[0]
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("Can't open %s - %v", fn, err)
	}

	// We know this is a 64k image so allocate and zero it.
	out := make([]byte, 65536)

	// First 2 bytes are the load address. Everything up to here should be 0's
	addr := (int(b[1]) << 8) + int(b[0])
	fmt.Printf("Addr is 0x%.4X\n", addr)
	for i := 2; i < len(b); i++ {
		out[addr+i-2] = b[i]
	}

	// Now setup a starting routine and reset vectors.
	out[0xC000] = 0x4C // JMP 0xC000
	out[0xC001] = 0x00
	out[0xC002] = 0xC0

	out[0xD000] = 0x20 // JSR <addr>
	out[0xD001] = byte(*startPC & 0xFF)
	out[0xD002] = byte((*startPC >> 8) & 0xFF)
	out[0xD003] = 0x4C // JMP 0xD003
	out[0xD004] = 0x03
	out[0xD005] = 0xD0

	out[0xFFD2] = 0x60 // RTS

	out[0xFFFA] = 0x00
	out[0xFFFB] = 0xC0
	out[0xFFFC] = 0x00
	out[0xFFFD] = 0xC0
	out[0xFFFE] = 0x00
	out[0xFFFF] = 0xC0

	outfn := fn + ".bin"
	if err := ioutil.WriteFile(outfn, out, 0777); err != nil {
		log.Fatalf("Can't write %q: %v", outfn, err)
	}
}
