package atari2600

import (
	"fmt"
	"math"

	"github.com/jmchacon/6502/memory"
)

func NewStandardCart(rom []uint8) (memory.Ram, error) {
	// Technically any cart size that is divisible by 2 and up to 4k we can handle and alias.
	got := len(rom)
	if got%2 != 0 || got > 4096 {
		return nil, fmt.Errorf("invalid StandardCart. Must be divisible by 2 and <= 4k in length. Got %d bytes", got)
	}
	mask := k4K_MASK >> uint(math.Log2(float64(4096/got)))
	b := &basicCart{
		rom:  rom,
		mask: mask,
	}
	return b, nil
}

const (
	k2K_MASK = uint16(0x07FF)
	k4K_MASK = uint16(0x0FFF)
)

// basicCart implements support for a 2k or 4k ROM. For 2k the upper half is simply
// a mirror of the lower half. The simplest implementation of carts.
type basicCart struct {
	rom  []uint8
	mask uint16
}

// Read implements the memory.Ram interface for Read.
// For a 2k ROM cart this means mirroring the lower 2k to the upper 2k
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space.
func (b *basicCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		// Move it into a range for indexing into our byte array and
		// normalized for 2k.
		return b.rom[addr&b.mask]
	}
	// Outside our range so just return 0.
	return 0
}

// Write implements the memory.Ram interface for Write.
// For a 2k or 4k ROM cart with no onboard RAM this does nothing
func (b *basicCart) Write(addr uint16, val uint8) {}

// PowerOn implements the memory.Ram interface for PowerOn.
func (b *basicCart) PowerOn() {}

func IsF8BankSwitch(rom []uint8) bool {
	if len(rom) == 8192 {
		return true
	}
	return false
}

func NewF8BankSwitchCart(rom []uint8) (memory.Ram, error) {
	if len(rom) != 8192 {
		return nil, fmt.Errorf("F8BankSwitchCart must be 8k in length. Got %d bytes", len(rom))
	}
	p := &f8BankSwitchCart{
		rom:     rom,
		lowBank: true,
	}
	return p, nil
}

// f8BankSwitchCart implements support for F8 style bank switching. 8k cart where access to 0x1FF8 accesses
// the first 4k and 0x1FF9 access the other 4k.
type f8BankSwitchCart struct {
	rom     []uint8
	lowBank bool
}

// Read implements the memory.Ram interface for Read.
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space. If the special addresses are triggered this
// immediately does the bank switch to the appropriate bank.
func (f *f8BankSwitchCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		if addr&0x1FF8 == 0x1FF8 {
			f.lowBank = true
		}
		if addr&0x1FF9 == 0x1FF9 {
			f.lowBank = false
		}
		off := uint16(0)
		if !f.lowBank {
			off = 4096
		}
		// Move it into a range for indexing into our byte array.
		return f.rom[(addr&k4K_MASK)+off]
	}
	// Outside our range so just return 0.
	return 0
}

// Write implements the memory.Ram interface for Write.
// For a 8k ROM cart with no onboard RAM this does nothing
func (b *f8BankSwitchCart) Write(addr uint16, val uint8) {}

// PowerOn implements the memory.Ram interface for PowerOn.
func (b *f8BankSwitchCart) PowerOn() {}
