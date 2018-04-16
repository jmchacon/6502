// Package io defines the basic interfaces for working
// with a 6502 family based I/O port (generally bi-directional).
// It's intended that implementors of I/O (such as a 6532) call
// the input callback (if provided) on every clock tick and properly
// account for the fact that output won't mirror input for a clock
// cycle (to account for latches being loaded)
package io

// PortIn8 defines an 8 bit I/O port for input
type PortIn8 interface {
	// Input will return the current value being set on the given input port.
	Input() uint8
}

// PortOut8 defines an 8 bit I/O port for output
type PortOut8 interface {
	// Input will return the current value being set on the given output port.
	Output() uint8
}

// PortIn1 defines a 1 bit I/O port for input
type PortIn1 interface {
	// Input will return the current value being set on the given input port.
	Input() bool
}

// PortOut8 defines a 1 bit I/O port for output
type PortOut1 interface {
	// Input will return the current value being set on the given output port.
	Output() bool
}
