package atari2600

import (
	"fmt"

	"github.com/jmchacon/6502/memory"
)

func NewStandardCart(rom []uint8) (memory.Ram, error) {
	if got := len(rom); got != 2048 && got != 4096 {
		return nil, fmt.Errorf("invalid StandardCart. Must be 2k or 4k in length. Got %d bytes", got)
	}
	b := &basicCart{
		rom:  rom,
		mask: k4K_MASK,
	}
	if len(rom) == 2048 {
		b.mask = k2K_MASK
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
	// Move it into a range for indexing into our byte array and
	// normalized for 2k.
	return b.rom[addr&b.mask]
}

// Write implements the memory.Ram interface for Write.
// For a 2k or 4k ROM cart with no onboard RAM this does nothing
func (b *basicCart) Write(addr uint16, val uint8) {}

// PowerOn implements the memory.Ram interface for PowerOn.
func (b *basicCart) PowerOn() {}

func NewPlaceholder(rom []uint8) (memory.Ram, error) {
	p := &placeholder{
		rom: rom,
	}
	return p, nil
}

// placeholder implements support for all other carts by holding the entire
// ROM and just normalizing to a 4k address range.
type placeholder struct {
	rom []uint8
}

// Read implements the memory.Ram interface for Read.
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space.
func (b *placeholder) Read(addr uint16) uint8 {
	// Move it into a range for indexing into our byte array.
	return b.rom[addr&k4K_MASK]
}

// Write implements the memory.Ram interface for Write.
// For a 4k ROM cart with no onboard RAM this does nothing
func (b *placeholder) Write(addr uint16, val uint8) {}

// PowerOn implements the memory.Ram interface for PowerOn.
func (b *placeholder) PowerOn() {}
