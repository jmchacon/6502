package c64basic

import (
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jmchacon/6502/cpu"
)

const testDir = "../testdata"

// flatMemory implements the RAM interface
type flatMemory struct {
	addr       [65536]uint8
	fillValue  uint8
	haltVector uint16
}

func (r *flatMemory) Read(addr uint16) uint8 {
	return r.addr[addr]
}

func (r *flatMemory) Write(addr uint16, val uint8) {
	r.addr[addr] = val
}

func (r *flatMemory) Reset() {
}

const (
	RESET = uint16(0x1FFE)
	IRQ   = uint16(0xD001)
)

func (r *flatMemory) PowerOn() {
	for i := range r.addr {
		// Fill with continual bytes (likely NOPs)
		r.addr[i] = r.fillValue
	}
	// Set NMI_VECTOR to hopefully opcodes that will halt the CPU
	// as expected.
	r.addr[cpu.NMI_VECTOR] = uint8(r.haltVector & 0xFF)
	r.addr[cpu.NMI_VECTOR+1] = uint8((r.haltVector & 0xFF00) >> 8)
	// Setup vectors so we have differing bit patterns
	r.addr[cpu.RESET_VECTOR] = uint8(RESET & 0xFF)
	r.addr[cpu.RESET_VECTOR+1] = uint8((RESET & 0xFF00) >> 8)
	r.addr[cpu.IRQ_VECTOR] = uint8(IRQ & 0xFF)
	r.addr[cpu.IRQ_VECTOR+1] = uint8((IRQ & 0xFF00) >> 8)
}

func TestList(t *testing.T) {
	// Initialize but then we'll overwrite it with a ROM image.
	r := &flatMemory{
		fillValue:  0x00,   // BRK
		haltVector: 0x0202, // If executed should halt the processor
	}
	// There's no CPU here so just power on the RAM directly.
	r.PowerOn()

	tests := []string{
		"dadc.prg",
		"dincsbc.prg",
		"dincsbc-deccmp.prg",
		"droradc.prg",
		"dsbc.prg",
		"dsbc-cmp-flags.prg",
		"sbx.prg",
		"vsbx.prg",
	}
	for _, test := range tests {
		// We're just assuming these aren't that large so reading into RAM is fine.
		rom, err := ioutil.ReadFile(filepath.Join(testDir, test))
		if err != nil {
			t.Errorf("Can't read PRG %s: %v", test, err)
			continue
		}

		if rom[0] != 0x01 || rom[1] != 0x08 {
			t.Errorf("%s doesn't appear to be a valid Basic PRG file. Start address not 0x0801 but: 0x%.2X%.2X", test, rom[1], rom[0])
			continue
		}
		for i := 2; i < len(rom); i++ {
			r.addr[0x0801+(i-2)] = rom[i]
		}

		var got []string
		pc := uint16(0x0801)
		fail := false
		for {
			l, newPC, err := List(pc, r)
			// Done
			if newPC == 0x0000 && l == "" && err == nil {
				break
			}
			// Print always
			t.Logf("%s", l)
			got = append(got, l)
			if err != nil {
				t.Errorf("%s: Error: %v", test, err)
				fail = true
				break
			}
			if pc == newPC {
				t.Error("Looping")
				fail = true
				break
			}
			pc = newPC
		}
		if fail {
			continue
		}
		want := []string{"1993 SYSPEEK(43)+256*PEEK(44)+26"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Different output\ngot:  %v\nwant: %v\n", test, got, want)
		}
	}
}
