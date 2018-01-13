// convertprg takes a C64 style PRG file
// and converts it into a 64k bin image for
// running as a test cart.
// This assumes exection will start at 0xD000
// which will then JSR to the start PC given.
// BRK/IRQ/NMI vectors will all point at 0xC000
// which simply infinite loops.
//
// Certain parts of RAM in zero page will be initialized
// with c64 values (such as the vectors used for finding
// start of basic, etc)
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
	b = b[2:]

	max := 65536 - addr
	if l := addr + len(b); l >= max {
		log.Printf("Length %d at offset %d too long, truncating to 64k", l, addr)
		b = b[:max]
	}

	fmt.Printf("Addr is 0x%.4X\n", addr)
	copy(out[addr:], b)

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

	// Based from data in http://sta.c64.org/cbm64mem.html.

	// Setup zero page.
	out[0x0000] = 0x2F
	out[0x0000] = 0x37
	out[0x0003] = 0xAA
	out[0x0004] = 0xB1
	out[0x0005] = 0x91
	out[0x0006] = 0xB3
	out[0x0016] = 0x19
	out[0x002B] = 0x01 // Pointer to start of BASIC area
	out[0x002C] = 0x08
	out[0x0038] = 0xA0 // Pointer to end of BASIC area
	out[0x0053] = 0x03
	out[0x0054] = 0x4C
	out[0x0091] = 0xFF
	out[0x009A] = 0x03
	out[0x00B2] = 0x3C
	out[0x00B3] = 0x03
	out[0x00C8] = 0x27
	out[0x00D5] = 0x27

	// Some other random locations in RAM that have presets
	// which may be used in test programs assuming c64.
	out[0x0282] = 0x08
	out[0x0284] = 0xA0
	out[0x0288] = 0x04
	out[0x0300] = 0x8B
	out[0x0301] = 0xE3
	out[0x0302] = 0x83
	out[0x0303] = 0xA4
	out[0x0304] = 0x7C
	out[0x0305] = 0xA5
	out[0x0306] = 0x1A
	out[0x0307] = 0xA7
	out[0x0308] = 0xE4
	out[0x0309] = 0xA7
	out[0x030A] = 0x86
	out[0x030B] = 0xAE
	out[0x0310] = 0x4C
	out[0x0314] = 0x31
	out[0x0315] = 0xEA
	out[0x0316] = 0x66
	out[0x0317] = 0xFE
	out[0x0318] = 0x47
	out[0x0319] = 0xFE
	out[0x031A] = 0x4A
	out[0x031B] = 0xF3
	out[0x031C] = 0x91
	out[0x031D] = 0xF2
	out[0x031E] = 0x0E
	out[0x031F] = 0xF2
	out[0x0320] = 0x50
	out[0x0321] = 0xF2
	out[0x0322] = 0x33
	out[0x0323] = 0xF3
	out[0x0324] = 0x57
	out[0x0325] = 0xF1
	out[0x0326] = 0xCA
	out[0x0327] = 0xF1
	out[0x0328] = 0xED
	out[0x0329] = 0xF6
	out[0x032A] = 0x3E
	out[0x032B] = 0xF1
	out[0x032C] = 0x2F
	out[0x032D] = 0xF3
	out[0x032E] = 0x66
	out[0x032F] = 0xFE
	out[0x0330] = 0xA5
	out[0x0331] = 0xF4
	out[0x0332] = 0xED
	out[0x0333] = 0xF5

	outfn := fn + ".bin"
	if err := ioutil.WriteFile(outfn, out, 0777); err != nil {
		log.Fatalf("Can't write %q: %v", outfn, err)
	}
}
