// Package memory defines the basic interfaces for working
// with a 6502 family memory map. Since each implementation
// that is emulated has specific mappings (including shadowed
// regions) this is defined as an interface.
package memory

type Ram interface {
	// Read returns the data byte stored at addr.
	Read(addr uint16) uint8
	// ReadZP returns the data byte stored at the zero page addr.
	ReadZP(addr uint8) uint8
	// ReadAddr returns the 2 bytes stored at addr in little endian form as an address.
	// Useful for processing JMP/IRQ/etc lookups
	ReadAddr(addr uint16) uint16
	// ReadZPAddr returns the 2 bytes storaed at addr (accounting for zero page rollover) in
	// little endian form as an address.
	ReadZPAddr(addr uint8) uint16
	// Write updates addr with the new value. For ROM addresses this is simply a no-op without
	// any error.
	Write(addr uint16, val uint8)
	// WriteZP updates the zero page addr with the new value. For ROM addresses this is simply a no-op without
	// any error.
	WriteZP(addr uint8, val uint8)
	// Reset does a soft reset of the memory.
	Reset()
	// PowerOn performs power on reset of the memory. This is implementation specific as to
	// whether it's randomized or preset to all zeros.
	PowerOn()
}
