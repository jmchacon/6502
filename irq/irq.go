// Package irq defines the basic interfaces for working
// with a 6502 family interrupt. A receiver of interrupts (IRQ/NMI)
// will implement this interface to allow other components which generate
// them to easily raise state without cross coupling component logic.
// NOTE: Even though chips make a distinction between level and edge type interrupts
//       the interfaces here don't matter and assume implementors simply account for
//       this in clock cycle management.
package irq

// Sender defines the interface for an IRQ source.
type Sender interface {
	// Raised indicates whether the interrupt is currently held high.
	Raised() bool
}
