package cpu

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmchacon/6502/disassemble"
)

var (
	instructionBuffer = flag.Int("instruction_buffer", 40, "Number of instructions to keep in circular buffer for debugging")
	verbose           = flag.Bool("verbose", false, "If set, some tests will print dots indicating their progress since they take a long time to run.")
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
	r.addr[NMI_VECTOR] = uint8(r.haltVector & 0xFF)
	r.addr[NMI_VECTOR+1] = uint8((r.haltVector & 0xFF00) >> 8)
	// Setup vectors so we have differing bit patterns
	r.addr[RESET_VECTOR] = uint8(RESET & 0xFF)
	r.addr[RESET_VECTOR+1] = uint8((RESET & 0xFF00) >> 8)
	r.addr[IRQ_VECTOR] = uint8(IRQ & 0xFF)
	r.addr[IRQ_VECTOR+1] = uint8((IRQ & 0xFF00) >> 8)
}

func Step(c *Processor) (cycles int, err error) {
	var done bool
	for {
		done, err = c.Tick(false, false)
		cycles++
		if done {
			break
		}
	}
	return
}

func TestNOP(t *testing.T) {
	tests := []struct {
		name       string
		fill       uint8
		haltVector uint16
		cycles     int
		pcBump     uint16
	}{
		{
			name:       "Classic NOP - 0x02 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x0202, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x12 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x22 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x2222, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x32 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x3232, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x42 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x4242, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x52 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x5252, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x62 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x6262, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x72 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x7272, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0x92 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0x9292, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0xB2 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0xB2B2, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0xD2 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0xD2D2, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "Classic NOP - 0xF2 halt",
			fill:       0xEA,   // classic NOP
			haltVector: 0xF2F2, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0x04 NOP - 0x12 halt",
			fill:       0x04,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     3,
			pcBump:     2,
		},
		{
			name:       "0x0C NOP - 0x12 halt",
			fill:       0x0C,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0x14 NOP - 0x12 halt",
			fill:       0x14,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     2,
		},
		{
			name:       "0x1C NOP - 0x12 halt",
			fill:       0x1C,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0x1A NOP - 0x12 halt",
			fill:       0x1A,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0x34 NOP - 0x12 halt",
			fill:       0x34,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     3,
			pcBump:     2,
		},
		{
			name:       "0x3C NOP - 0x12 halt",
			fill:       0x3C,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0x3A NOP - 0x12 halt",
			fill:       0x3A,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0x44 NOP - 0x12 halt",
			fill:       0x44,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     3,
			pcBump:     2,
		},
		{
			name:       "0x54 NOP - 0x12 halt",
			fill:       0x54,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     2,
		},
		{
			name:       "0x5C NOP - 0x12 halt",
			fill:       0x5C,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0x5A NOP - 0x12 halt",
			fill:       0x5A,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0x64 NOP - 0x12 halt",
			fill:       0x64,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     3,
			pcBump:     2,
		},
		{
			name:       "0x74 NOP - 0x12 halt",
			fill:       0x74,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     2,
		},
		{
			name:       "0x7C NOP - 0x12 halt",
			fill:       0x7C,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0x7A NOP - 0x12 halt",
			fill:       0x7A,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0x80 NOP - 0x12 halt",
			fill:       0x80,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     2,
		},
		{
			name:       "0x82 NOP - 0x12 halt",
			fill:       0x82,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     2,
		},
		{
			name:       "0x89 NOP - 0x12 halt",
			fill:       0x89,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     2,
		},
		{
			name:       "0xD4 NOP - 0x12 halt",
			fill:       0xD4,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     2,
		},
		{
			name:       "0xDC NOP - 0x12 halt",
			fill:       0xDC,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0xC2 NOP - 0x12 halt",
			fill:       0xC2,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     2,
		},
		{
			name:       "0xDA NOP - 0x12 halt",
			fill:       0xDA,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
		{
			name:       "0xF4 NOP - 0x12 halt",
			fill:       0xF4,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     2,
		},
		{
			name:       "0xFC NOP - 0x12 halt",
			fill:       0xFC,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     4,
			pcBump:     3,
		},
		{
			name:       "0xE2 NOP - 0x12 halt",
			fill:       0xE2,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     2,
		},
		{
			name:       "0xFA NOP - 0x12 halt",
			fill:       0xFA,
			haltVector: 0x1212, // If executed should halt the processor
			cycles:     2,
			pcBump:     1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := &flatMemory{
				fillValue:  test.fill,
				haltVector: test.haltVector,
			}
			canonical := r
			canonical.PowerOn()
			c, err := Init(CPU_NMOS, r, 0)
			if err != nil {
				t.Errorf("Can't initialize CPU_NMOS: %v", err)
				return
			}
			// Set things up so we execute 1000 NOP's before halting
			end := RESET + uint16(test.pcBump)*1000
			r.addr[end] = uint8(test.haltVector & 0x00FF)
			r.addr[end+1] = uint8(test.haltVector & 0x00FF)
			canonical.addr[end] = uint8(test.haltVector & 0x00FF)
			canonical.addr[end+1] = uint8(test.haltVector & 0x00FF)

			saved := c
			if c.PC != RESET {
				t.Errorf("Reset vector isn't correct. Got 0x%.4X, want 0x%.4X", c.PC, RESET)
				return
			}
			got := 0
			pageCross := 0
			var pc uint16
			for {
				pc = c.PC
				cycles := 0
				cycles, err = Step(c)
				got += cycles
				if err != nil {
					break
				}
				if got, want := cycles, test.cycles; got != want {
					// Don't bother computing these ahead. We'll just track how many happen.
					if got == want+1 {
						pageCross++
					} else {
						t.Errorf("Didn't cycle as expected. Got %d want %d on PC: 0x%.4X", got, want, pc)
						break
					}
				}
				// NOPs should be single PC increments only normally but some are multi-step
				if got, want := c.PC, pc+test.pcBump; got != want {
					t.Errorf("PC didn't increment by %d. Got 0x%.4X and started with 0x%.4X", test.pcBump, c.PC, pc)
					break
				}
				// Registers shouldn't be changing
				if saved.A != c.A || saved.X != c.X || saved.Y != c.Y || saved.S != c.S || saved.P != c.P {
					t.Errorf("Registers changed at PC: 0x%.4X\nGot  %v\nWwant %v", pc, c, saved)
					break
				}
				// Memory shouldn't be changing from initial setup
				if r.addr != canonical.addr {
					t.Errorf("Memory changed unexpectedly at PC: 0x%.4X", pc)
					break
				}
				// We've wrapped around so abort
				if got > (0xFFFF * 2) {
					break
				}
			}
			if err == nil {
				t.Errorf("Didn't get error as expected for invalid opcode. PC: 0x%.4X", pc)
			}

			// We should end up executing X cyckes 1000 times plus 2 for the halt plus any page crossings.
			if want := 2 + pageCross + (1000 * test.cycles); got != want {
				t.Errorf("Invalid cycle count. Stopped PC: 0x%.4X\nGot  %d\nwant %d (%d cycles)\n", pc, got, want, test.cycles)
			}

			e, ok := err.(HaltOpcode)
			if !ok {
				t.Errorf("Didn't stop due to halt: %T - %v", err, err)
			}
			if ok {
				if got, want := e.Opcode, uint8(test.haltVector&0xFF); got != want {
					t.Errorf("Halted on unexpected opcode. Got 0x%.2X\nWant 0x%.2X", got, want)
				}
			}
			pc = c.PC
			// Advance the PC forward to wrap around
			for i := 0; i < 8; i++ {
				_, err = Step(c)
			}
			if err == nil {
				t.Error("Didn't get an error after halting CPU")
			}
			e, ok = err.(HaltOpcode)
			if !ok {
				t.Errorf("After halting didn't stop due to halt: %T - %v", err, err)
			}
			if ok {
				if got, want := e.Opcode, uint8(test.haltVector&0xFF); got != want {
					t.Errorf("After halting, halted on unexpected opcode. Got 0x%.2X\nWant 0x%.2X", got, want)
				}
			}
			if pc != c.PC {
				t.Errorf("PC advanced after halting CPU - old 0x%.4X new 0x%.4X", pc, c.PC)
			}
			for {
				done, err := c.Reset()
				if err != nil {
					t.Errorf("Reset returned error: %v", err)
					break
				}
				if done {
					break
				}
			}
			pc = c.PC
			_, err = Step(c)
			if err != nil {
				t.Errorf("Still getting error after resetting on PC: 0x%.4X - %v", pc, err)
			}
		})
	}
}

func BenchmarkNOPandADC(b *testing.B) {
	var totElapsed int64
	totCycles := 0
	// NOP and ADC a
	for _, test := range []uint8{0xEA, 0x6D} {
		got := 0
		var elapsed int64
		r := &flatMemory{
			fillValue:  test,
			haltVector: (uint16(test) << 8) + uint16(test),
		}
		c, err := Init(CPU_NMOS, r, 0)
		if err != nil {
			b.Fatalf("Can't initialize CPU_NMOS: %v", err)
		}
		r.addr[NMI_VECTOR] = test
		r.addr[NMI_VECTOR+1] = test
		r.addr[RESET_VECTOR] = test
		r.addr[RESET_VECTOR+1] = test
		r.addr[IRQ_VECTOR] = test
		r.addr[IRQ_VECTOR+1] = test
		n := time.Now()
		// Execute 100 million instructions so we get a reasonable timediff.
		// Otherwise calling time.Now() too close to another call mostly shows
		// upwards of 100ns of overhead just for gathering time (depending on arch).
		// At this many instructions we're accurate to 5-6 decimal places so "good enough".
		for i := 0; i < 100000000; i++ {
			cycles, err := Step(c)
			got += cycles
			if err != nil {
				b.Fatalf("Got error: %v", err)
			}
		}
		elapsed += time.Now().Sub(n).Nanoseconds()
		totElapsed += elapsed
		totCycles += got
		per := float64(elapsed) / float64(got)
		speed := 1e3 * (1 / per)
		b.Logf("%d cycles in %dns %.2fns/cycle at %.2fMhz", got, elapsed, per, speed)
	}
	per := float64(totElapsed) / float64(totCycles)
	speed := 1e3 * (1 / per)
	b.Logf("Average %d cycles in %dns %.2fns/cycle at %.2fMhz", totCycles, totElapsed, per, speed)
}

func BenchmarkTime(b *testing.B) {
	var tot int64
	const runs = 1000000
	for j := 0; j < b.N; j++ {
		for i := 0; i < runs; i++ {
			s := time.Now()
			diff := time.Now().Sub(s).Nanoseconds()
			tot += diff
		}
	}
	avg := tot / int64(runs*b.N)
	const goal = int64(588)
	s := time.Now()
	for i := int64(0); i < (goal/avg)-1; i++ {
		_ = time.Now()
	}
	d := time.Now().Sub(s).Nanoseconds()
	b.Logf("avg diff: %d and sleep time: %s", avg, time.Duration(d))
}

func TestLoad(t *testing.T) {
	r := &flatMemory{
		fillValue:  0xEA,   // classic NOP
		haltVector: 0x0202, // If executed should halt the processor
	}
	c, err := Init(CPU_NMOS, r, 0)
	if err != nil {
		t.Fatalf("Can't initialize cpu - %v", err)
	}

	r.addr[RESET+0] = 0xA1 // LDA ($EA,x)
	r.addr[RESET+1] = 0xEA
	r.addr[RESET+2] = 0xA1 // LDA ($FF,x)
	r.addr[RESET+3] = 0xFF
	r.addr[RESET+4] = 0x12 // Halt

	// (0x00EA) points to 0x650F
	r.addr[0x00EA] = 0x0F
	r.addr[0x00EB] = 0x65

	// (0x00FA) points to 0x551F
	r.addr[0x00FA] = 0x1F
	r.addr[0x00FB] = 0x55

	// (0x00FF) points to 0xA1FA (since 0x0000 is 0xA1)
	r.addr[0x00FF] = 0xFA
	r.addr[0x0000] = 0xA1

	// (0x001F) points to 0xA20A
	r.addr[0x000F] = 0x0A
	r.addr[0x0010] = 0xA2

	// For LDA ($FA,x) X = 0x00
	r.addr[0x650F] = 0xAB
	// For LDA ($FA,x) X = 0x10
	r.addr[0x551F] = 0xCD

	// For LDA ($FF,x) X = 0x00
	r.addr[0xA1FA] = 0xEF
	// For LDA ($FF,x) X = 0x10
	r.addr[0xA20A] = 0x00

	tests := []struct {
		name     string
		x        uint8
		expected []uint8
	}{
		{
			name:     "LDA ($EA,x), LDA ($FF,x) - X == 0x00",
			x:        0x00,
			expected: []uint8{0xAB, 0xEF},
		},
		{
			name:     "LDA ($EA,x), LDA ($FF,x) - X == 0x10",
			x:        0x10,
			expected: []uint8{0xCD, 0x00},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for {
				done, err := c.Reset()
				if err != nil {
					t.Fatalf("Reset returned error: %v", err)
					break
				}
				if done {
					break
				}
			}
			for i, v := range test.expected {
				pc := c.PC
				// These don't change status but the actual load should update Z
				c.A = v - 1
				c.X = test.x
				cycles, err := Step(c)
				if err != nil {
					t.Errorf("CPU halted unexpectedly: old PC: 0x%.4X - PC: 0x%.4X - %v", pc, c.PC, err)
					break
				}
				if got, want := cycles, 6; got != want {
					t.Errorf("Invalid cycle count - got %d want %d", got, want)
				}
				if got, want := c.A, v; got != want {
					t.Errorf("A register doesn't have correct value for iteration %d. Got 0x%.2X and want 0x%.2X", i, got, want)
				}
				if got, want := (c.P&P_ZERO) == 0, v != 0; got != want {
					t.Errorf("Z flag is incorrect. Status - 0x%.2X and A is 0x%.2X", c.P, c.A)
				}
				if got, want := (c.P&P_NEGATIVE) == 0, v < 0x80; got != want {
					t.Errorf("N flag is incorrect. Status - 0x%.2X and A is 0x%.2X", c.P, c.A)
				}
			}
		})
	}
}

func TestStore(t *testing.T) {
	r := &flatMemory{
		fillValue:  0xEA,   // classic NOP
		haltVector: 0x0202, // If executed should halt the processor
	}
	c, err := Init(CPU_NMOS, r, 0)
	if err != nil {
		t.Fatalf("Can't initialize cpu - %v", err)
	}

	r.addr[RESET+0] = 0x81 // STA ($EA,x)
	r.addr[RESET+1] = 0xEA
	r.addr[RESET+2] = 0x81 // STA ($FF,x)
	r.addr[RESET+3] = 0xFF
	r.addr[RESET+4] = 0x12 // Halt

	// (0x00EA) points to 0x650F
	r.addr[0x00EA] = 0x0F
	r.addr[0x00EB] = 0x65

	// (0x00FA) points to 0x551F
	r.addr[0x00FA] = 0x1F
	r.addr[0x00FB] = 0x55

	// (0x00FF) points to 0xA1FA (since 0x0000 is 0xA1)
	r.addr[0x00FF] = 0xFA
	r.addr[0x0000] = 0xA1

	// (0x001F) points to 0xA20A
	r.addr[0x000F] = 0x0A
	r.addr[0x0010] = 0xA2

	// For STA ($EA,x) X = 0x00
	r.addr[0x650F] = 0x00
	// For STA ($EA,x) X = 0x10
	r.addr[0x551F] = 0x00

	// For STA ($FF,x) X = 0x00
	r.addr[0xA1FA] = 0x00
	// For STA ($FF,x) X = 0x10
	r.addr[0xA20A] = 0x00

	tests := []struct {
		name     string
		a        uint8
		x        uint8
		expected []uint16
	}{
		{
			name:     "STA ($EA,x), LDA ($FF,x) - A = 0xAA X == 0x00",
			a:        0xAA,
			x:        0x00,
			expected: []uint16{0x650F, 0xA1FA},
		},
		{
			name:     "LDA ($EA,x), LDA ($FF,x) - A = 0x55 X == 0x10",
			a:        0x55,
			x:        0x10,
			expected: []uint16{0x551F, 0xA20A},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for {
				done, err := c.Reset()
				if err != nil {
					t.Fatalf("Reset returned error: %v", err)
					break
				}
				if done {
					break
				}
			}
			for i, v := range test.expected {
				pc := c.PC
				p := c.P
				// These don't change status flags in our testbed but we do verify the actual store doesn't.
				c.A = test.a
				c.X = test.x
				r.addr[v] = test.a - 1
				cycles, err := Step(c)
				if err != nil {
					t.Errorf("CPU halted unexpectedly: old PC: 0x%.4X - PC: 0x%.4X - %v", pc, c.PC, err)
					break
				}
				if got, want := cycles, 6; got != want {
					t.Errorf("Invalid cycle count - got %d want %d", got, want)
				}
				if got, want := r.addr[v], c.A; got != want {
					t.Errorf("Memory location 0x%.4X doesn't have correct value for iteration %d. Got 0x%.2X and want 0x%.2X", v, i, got, want)
				}
				if got, want := c.P, p; got != want {
					t.Errorf("Status register changed. Got 0x%.2X and want 0x%.2X", got, want)
				}
			}
		})
	}
}

func TestROMs(t *testing.T) {
	type verify struct {
		PC  uint16
		A   uint8
		X   uint8
		Y   uint8
		P   uint8
		S   uint8
		CYC uint64
	}
	tests := []struct {
		name                 string
		filename             string
		cpu                  CPUType
		nes                  bool
		startPC              uint16
		traceLog             []verify
		loadTrace            func() ([]verify, error)
		endCheck             func(oldPC uint16, c *Processor) bool
		successCheck         func(oldPC uint16, c *Processor) error
		expectedCycles       uint64
		expectedInstructions uint64
	}{
		{
			name:     "Functional test",
			filename: "6502_functional_test.bin",
			cpu:      CPU_NMOS,
			startPC:  0x400,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0x3469 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       96241367,
			expectedInstructions: 30646177,
		},
		// The next tests (up to and including vsbx.bin) all come from http://nesdev.com/6502_cpu.txt
		// NOTE: They are hard to debug even with the ring buffer since we don't snapshot memory
		//       state and the test itself is self modifying code...So you'll have to use the register values
		//       to infer state along the way.
		{
			name:     "dadc test",
			filename: "dadc.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       21230739,
			expectedInstructions: 8109021,
		},
		{
			name:     "dincsbc test",
			filename: "dincsbc.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       18939479,
			expectedInstructions: 6781979,
		},
		{
			name:     "dincsbc-deccmp test",
			filename: "dincsbc-deccmp.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       18095478,
			expectedInstructions: 5507188,
		},
		{
			name:     "droradc test",
			filename: "droradc.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       22148243,
			expectedInstructions: 8240093,
		},
		{
			name:     "dsbc test",
			filename: "dsbc.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       18021975,
			expectedInstructions: 6650907,
		},
		{
			name:     "dsbc-cmp-flags test",
			filename: "dsbc-cmp-flags.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       14425354,
			expectedInstructions: 4982868,
		},
		{
			name:     "sbx test",
			filename: "sbx.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					if *verbose {
						fmt.Println()
					}
					return true
				}
				if *verbose {
					// On this test it JSR's to FFD2 which is the C64
					// ROM print routine. It prints a dot for each iteration.
					// Do the same for easier debugging if it fails.
					if c.PC == 0xFFD2 {
						fmt.Printf(".")
					}
				}

				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       6044288251,
			expectedInstructions: 2081694799,
		},
		{
			name:     "vsbx test",
			filename: "vsbx.bin",
			cpu:      CPU_NMOS,
			startPC:  0xD000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC {
					if *verbose {
						fmt.Println()
					}
					return true
				}
				if *verbose {
					// On this test it JSR's to FFD2 which is the C64
					// ROM print routine. It prints a dot for each iteration.
					// Do the same for easier debugging if it fails.
					if c.PC == 0xFFD2 {
						fmt.Printf(".")
					}
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if c.PC == 0xD003 {
					return nil
				}
				return fmt.Errorf("CPU looping at PC: 0x%.4X", oldPC)
			},
			expectedCycles:       7525173527,
			expectedInstructions: 2552776789,
		}, {
			name:     "BCD test",
			filename: "bcd_test.bin",
			cpu:      CPU_NMOS,
			startPC:  0xC000,
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == c.PC || oldPC == 0xC04B {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if got, want := c.Ram.Read(0x0000), uint8(0x00); got != want {
					return fmt.Errorf("Invalid value at 0x00: Got %.2X and want %.2X", got, want)
				}
				return nil
			},
			expectedCycles:       53953828,
			expectedInstructions: 17609916,
		},
		{
			name:     "NES functional test",
			filename: "nestest.nes",
			cpu:      CPU_NMOS_RICOH,
			nes:      true,
			startPC:  0xC000,
			loadTrace: func() ([]verify, error) {
				f, err := os.Open(filepath.Join(testDir, "nestest.log"))
				if err != nil {
					return nil, err
				}
				var out []verify
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					t := scanner.Text()
					// Each line is 81 characters and each field is a specific offset.
					pc, err := strconv.ParseUint(t[0:4], 16, 16)
					if err != nil {
						return nil, err
					}
					a, err := strconv.ParseUint(t[50:52], 16, 8)
					if err != nil {
						return nil, err
					}
					x, err := strconv.ParseUint(t[55:57], 16, 8)
					if err != nil {
						return nil, err
					}
					y, err := strconv.ParseUint(t[60:62], 16, 8)
					if err != nil {
						return nil, err
					}
					p, err := strconv.ParseUint(t[65:67], 16, 8)
					if err != nil {
						return nil, err
					}
					s, err := strconv.ParseUint(t[71:73], 16, 8)
					if err != nil {
						return nil, err
					}
					// This can have spaces which ParseUint barfs on.
					tmp := t[78:81]
					tmp = strings.TrimLeft(tmp, " ")
					c, err := strconv.Atoi(tmp)
					if err != nil {
						return nil, err
					}
					out = append(out, verify{
						PC:  uint16(pc),
						A:   uint8(a),
						X:   uint8(x),
						Y:   uint8(y),
						P:   uint8(p),
						S:   uint8(s),
						CYC: uint64(c),
					})
				}
				return out, nil
			},
			endCheck: func(oldPC uint16, c *Processor) bool {
				if oldPC == 0xC66E || c.Ram.Read(0x0002) != 0x00 || c.Ram.Read(0x0003) != 0x00 {
					return true
				}
				return false
			},
			successCheck: func(oldPC uint16, c *Processor) error {
				if oldPC == 0xC66E {
					return nil
				}
				return fmt.Errorf("Error codes - 0x02: %.2X 0x03: %.2X", c.Ram.Read(0x0002), c.Ram.Read(0x0003))
			},
			expectedCycles:       26553,
			expectedInstructions: 8991,
		},
	}

	var totalCycles, totalInstructions uint64
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// If we have a trace log initialize it.
			if test.loadTrace != nil {
				var err error
				test.traceLog, err = test.loadTrace()
				if err != nil {
					t.Errorf("Can't load traces - %v", err)
					return
				}
			}
			// Initialize as always but then we'll overwrite it with a ROM image.
			r := &flatMemory{
				fillValue:  0x00,   // BRK
				haltVector: 0x0202, // If executed should halt the processor
			}
			c, err := Init(test.cpu, r, 0)
			if err != nil {
				t.Errorf("Can't initialize cpu - %v", err)
				return
			}

			// We're just assuming these aren't that large so reading into RAM is fine.
			rom, err := ioutil.ReadFile(filepath.Join(testDir, test.filename))
			if err != nil {
				t.Errorf("Can't read ROM: %v", err)
				return
			}
			if !test.nes {
				for i, b := range rom {
					r.addr[i] = uint8(b)
				}
			} else {
				if rom[0] != 'N' && rom[1] != 'E' && rom[2] != 'S' && rom[3] != 0x1A {
					t.Errorf("Bad NES ROM format - header bytes:\n%s", hex.Dump(rom[0:15]))
				}
				prgCount := rom[4]
				chrCount := rom[5]
				t.Logf("PRG count: %d, CHR count: %d", prgCount, chrCount)
				// Map the first PRG ROM into place
				for i := 0; i < 16*1024; i++ {
					r.addr[0xc000+i] = rom[16+i]
				}
				// Nothing else needs to happen unless we get more extensive NES ROM's
			}
			type run struct {
				ram    [65536]uint8
				PC     uint16
				P      uint8
				A      uint8
				X      uint8
				Y      uint8
				S      uint8
				Cycles int
			}
			buffer := make([]run, *instructionBuffer, *instructionBuffer) // last N PC values
			bufferLoc := 0
			bufferWrap := false
			dumper := func() {
				end := *instructionBuffer
				if !bufferWrap {
					end = bufferLoc
					bufferLoc = 0
				}
				t.Logf("Last %d instructions: (bufferloc: %d)", end, bufferLoc)
				t.Logf("Zero+stack pages dump:\n%s", hex.Dump(r.addr[0:0x0200]))
				for i := 0; i < end; i++ {
					dis, _ := disassemble.Step(buffer[bufferLoc].PC, &flatMemory{buffer[bufferLoc].ram, 0, 0})
					t.Logf("%d - %s - PC: %.4X P: %.2X A: %.2X X: %.2X Y: %.2X SP: %.2X post - cycles: %d", bufferLoc, dis, buffer[bufferLoc].PC, buffer[bufferLoc].P, buffer[bufferLoc].A, buffer[bufferLoc].X, buffer[bufferLoc].Y, buffer[bufferLoc].S, buffer[bufferLoc].Cycles)
					bufferLoc++
					if bufferLoc >= *instructionBuffer {
						bufferLoc = 0
					}
				}
			}
			c.PC = test.startPC
			var totCycles, totInstructions uint64
			var pc uint16
			for {
				abort := false
				if len(test.traceLog) > 0 {
					if totInstructions >= uint64(len(test.traceLog)) {
						err = fmt.Errorf("Ran out of trace log at PC: 0x%.4X", pc)
						break
					}
					entry := test.traceLog[totInstructions]
					testCyc := ((totCycles * 3) % 341)
					if c.PC != entry.PC || c.P != entry.P || c.A != entry.A || c.X != entry.X || c.Y != entry.Y || c.S != entry.S || testCyc != entry.CYC {
						err = fmt.Errorf("Trace log violation.\nGot  PC: %.4X A: %.2X X: %.2X Y: %.2X P: %.2X SP: %.2X CYC: %d\nWant PC: %.4X A: %.2X X: %.2X Y: %.2X P: %.2X SP: %.2X CYC: %d", c.PC, c.A, c.X, c.Y, c.P, c.S, testCyc, entry.PC, entry.A, entry.X, entry.Y, entry.P, entry.S, entry.CYC)
						// We want this in the log since we have room
						abort = true
					}
				}
				pc = c.PC
				// Have to snapshot RAM before we run as some of the tests are self modifying code...
				buffer[bufferLoc].ram[c.PC] = r.addr[c.PC]
				buffer[bufferLoc].ram[c.PC+1] = r.addr[c.PC+1]
				buffer[bufferLoc].ram[c.PC+2] = r.addr[c.PC+2]
				buffer[bufferLoc].PC = c.PC
				buffer[bufferLoc].P = c.P
				buffer[bufferLoc].A = c.A
				buffer[bufferLoc].X = c.X
				buffer[bufferLoc].Y = c.Y
				buffer[bufferLoc].S = c.S

				var cycles int
				// The special case where we inserted one more into the buffer for debugging but don't actually execute it (so no cycles).
				if !abort {
					cycles, err = Step(c)
					totInstructions++
					buffer[bufferLoc].Cycles = cycles
				}
				bufferLoc++
				if bufferLoc >= *instructionBuffer {
					bufferLoc = 0
					bufferWrap = true
				}
				if abort {
					break
				}
				totCycles += uint64(cycles)
				if err != nil {
					break
				}
				if test.endCheck(pc, c) {
					err = test.successCheck(pc, c)
					break
				}
			}
			errored := false
			if err != nil {
				t.Errorf("%d cycles %d instructions - CPU error at PC: 0x%.4X - %v", totCycles, totInstructions, pc, err)
				errored = true
			}
			if got, want := totCycles, test.expectedCycles; got != want {
				t.Logf("Invalid cycle count. Got %d and want %d", got, want)
				errored = true
			}
			if got, want := totInstructions, test.expectedInstructions; got != want {
				t.Errorf("Invalid instruction count. Got %d and want %d", got, want)
				errored = true
			}
			if errored {
				dumper()
				return
			}
			atomic.AddUint64(&totalCycles, totCycles)
			atomic.AddUint64(&totalInstructions, totInstructions)
			t.Logf("Completed %d cycles and %d instructions", totCycles, totInstructions)
		})
	}
	t.Logf("TestROMs totals: Completed %d cycles and %d instructions", totalCycles, totalInstructions)
}
