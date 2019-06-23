// Package memory defines the basic interfaces for working
// with a 6502 family memory map. Since each implementation
// that is emulated has specific mappings (including shadowed
// regions) this is defined as an interface.
package memory

type Bank interface {
	// Read returns the data byte stored at addr.
	Read(addr uint16) uint8
	// Write updates addr with the new value. For ROM addresses this is simply a no-op without
	// any error.
	Write(addr uint16, val uint8)
	// PowerOn performs power on reset of the memory. This is implementation specific as to
	// whether it's randomized or preset to all zeros.
	PowerOn()
}
