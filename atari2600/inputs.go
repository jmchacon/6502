package atari2600

import (
	"github.com/jmchacon/6502/io"
)

// Joystick defines a classic 1970's/1980s era digital joystick with 4 directions and a single button.
// For each direction true == pressed.
type Joystick struct {
	Up     io.PortIn1
	Down   io.PortIn1
	Left   io.PortIn1
	Right  io.PortIn1
	Button io.PortIn1
}

// Paddle defines an atari2600 paddle controller where the internal RC circuit is either charged (or not).
// Corresponds to reads on INPT0-3.
// The buttons are routed through portA on the PIA and true == pressed.
type Paddle struct {
	Charged io.PortIn1
	Button  io.PortIn1
}

type portA struct {
	joysticks [2]*Joystick
	paddles   [4]*Paddle
}

type portB struct {
	difficulty [2]io.PortIn1
	colorBW    io.PortIn1
	gameSelect io.PortIn1
	reset      io.PortIn1
}

// Input is used to map portA on the PIA to the Joysticks.
func (p *portA) Input() uint8 {
	out := uint8(0x00)
	// Technically this can cause inputs a physical joystick can't normally
	// do such as Up+Down or Left+Right. We don't worry about that as technically
	// someone disassembling a joystick could do the same back in 1977.

	// NOTE: These are all active low in the real HW (so 0 means pressed).
	if p.joysticks[0] != nil {
		if !p.joysticks[0].Up.Input() {
			out |= 0x10
		}
		if !p.joysticks[0].Down.Input() {
			out |= 0x20
		}
		if !p.joysticks[0].Left.Input() {
			out |= 0x40
		}
		if !p.joysticks[0].Right.Input() {
			out |= 0x80
		}
	}
	if p.joysticks[1] != nil {
		if !p.joysticks[1].Up.Input() {
			out |= 0x01
		}
		if !p.joysticks[1].Down.Input() {
			out |= 0x02
		}
		if !p.joysticks[1].Left.Input() {
			out |= 0x04
		}
		if !p.joysticks[1].Right.Input() {
			out |= 0x08
		}
	}

	// We check in setup and don't allow both to be defined at once.
	// Same thing, buttons are active low.
	if p.paddles[0] != nil {
		if !p.paddles[0].Button.Input() {
			out |= 0x80
		}
	}
	if p.paddles[1] != nil {
		if !p.paddles[1].Button.Input() {
			out |= 0x40
		}
	}
	if p.paddles[2] != nil {
		if !p.paddles[2].Button.Input() {
			out |= 0x08
		}
	}
	if p.paddles[3] != nil {
		if !p.paddles[3].Button.Input() {
			out |= 0x04
		}
	}

	return out
}

// Input is used to map portB on the PIA to the console switches.
func (p *portB) Input() uint8 {
	out := uint8(0x00)

	// NOTE: These 2 are active low in the real HW (so 0 means pressed).
	if !p.reset.Input() {
		out |= 0x01
	}
	if !p.gameSelect.Input() {
		out |= 0x02
	}
	// false == BW, true == Color.
	if p.colorBW.Input() {
		out |= 0x08
	}
	// false == Beginner, true == Advanced.
	if p.difficulty[0].Input() {
		out |= 0x40
	}
	if p.difficulty[1].Input() {
		out |= 0x80
	}
	return out
}
