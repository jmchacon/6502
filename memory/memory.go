// Package memory defines the basic interfaces for working
// with a 6502 family memory map. Since each implementation
// that is emulated has specific mappings (including shadowed
// regions) this is defined as an interface.
package memory

import (
	"fmt"
	"math/rand"
	"time"
)

type Bank interface {
	// Read returns the data byte stored at addr.
	Read(addr uint16) uint8
	// Write updates addr with the new value. For ROM addresses this is simply a no-op without
	// any error.
	Write(addr uint16, val uint8)
	// PowerOn performs power on reset of the memory. This is implementation specific as to
	// whether it's randomized or preset to all zeros.
	PowerOn()
	// Parent holds a reference (if non-nil) to the next level memory controller. A chain
	// of these can be created in order to find the top one and be able to query items
	// such as the databus state (from the last value to go over it). Some implementations
	// depend on transient databus state due to side effects.
	Parent() Bank
	// DatabusVal returns the last value seen to go across on the data bus.
	DatabusVal() uint8
}

// LatestDatabusVal hunts up a chain of Banks until it finds the outermost one and
// return the DatabusVal from it.
func LatestDatabusVal(b Bank) uint8 {
	if b.Parent() != nil {
		return LatestDatabusVal(b.Parent())
	}
	return b.DatabusVal()
}

// ram implements a standard R/W interface to an address space for 8 bit systems.
// If this is mapped into a larger memory map it's up to a parent Bank to properly mask addr
// before calling Read/Write.
type ram struct {
	ram        []uint8
	parent     Bank
	databusVal uint8
}

// New8BitRAMBank creates a R/W RAM bank of the given size. Size must be a power of 2.
// If this is smaller than 64k (uint16 max) aliasing will occur on Read/Write.
func New8BitRAMBank(size int, parent Bank) (Bank, error) {
	if size%2 != 0 {
		return nil, fmt.Errorf("invalid size: %d must be a power of 2", size)
	}
	if size > 1<<16 {
		return nil, fmt.Errorf("invalid size: %d is bigger than 64k", size)
	}
	b := &ram{
		parent: parent,
	}
	// Go ahead and completely preallocate this now.
	b.ram = make([]uint8, size, size)
	return b, nil
}

// Read implements the interface for Bank. Address is clipped based on length of ram buffer.
func (r *ram) Read(addr uint16) uint8 {
	// Mask addr to fit
	addr &= uint16(len(r.ram) - 1)
	val := r.ram[addr]
	r.databusVal = val
	return val
}

// Write implements the interface for Bank. Address is clipped based on length of ram buffer.
func (r *ram) Write(addr uint16, val uint8) {
	// Mask addr to fit
	addr &= uint16(len(r.ram) - 1)
	r.databusVal = val
	r.ram[addr] = val
}

// PowerOn implements the interface for memory.Bank and randomizes the RAM.
func (r *ram) PowerOn() {
	rand.Seed(time.Now().UnixNano())
	for i := range r.ram {
		r.ram[i] = uint8(rand.Intn(256))
	}
}

// Parent implements the interface for returning a possible parent memory.Bank.
func (r *ram) Parent() Bank {
	return r.parent
}

// DatabusVal returns the most recent seen databus item.
func (r *ram) DatabusVal() uint8 {
	return r.databusVal
}
