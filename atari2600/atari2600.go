// Package atari2600 is the main logic for pulling together an atari 2600 emulator.
// The actual chips are implemented in other packages and most the logic here is
// simply to pull together the memory mappings for them.
package atari2600

import (
	"errors"
	"fmt"
	"image"

	"github.com/jmchacon/6502/cpu"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/pia6532"
	"github.com/jmchacon/6502/tia"
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
type Paddle struct {
	Charged io.PortIn1
	Button  io.PortIn1
}

type portA struct {
	joysticks [2]*Joystick
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
	// TODO(jchacon): Also handle paddle buttons.

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

type Atari struct {
	cpu   *cpu.Chip
	pia   *pia6532.Chip
	tia   *tia.Chip
	portA *portA
	portB *portB
}

// AtariDef defines the pieces needed to setup a basic Atari 2600. Assuming up to 2 joysticks and 4 paddles.
// TODO(jchacon): Add other controller types (wheel, keyboard, etc).
type AtariDef struct {
	Mode      tia.TIAMode
	Joysticks [2]*Joystick
	Paddles   [4]*Paddle
	// PaddleGround will be called whenever the paddle input ports (INPT0-3) get grounded.
	PaddleGround func()
	// The console switchs (except power).

	// Difficulty defines the 2 player difficulty switches.
	// False == Beginner, true == Advanced.
	Difficulty [2]io.PortIn1
	// ColorBW defines color or B/W mode.
	// True == color, false == B/W
	ColorBW io.PortIn1
	// GameSelect is used to progress through options.
	// True == pressed.
	GameSelect io.PortIn1
	// Reset is generally used to start a game.
	// True == pressed.
	Reset io.PortIn1
	// FrameDone is called on every VSYNC transition cycle. See tia documentation for more details.
	FrameDone func(*image.NRGBA)
}

// Init returns an initialized and powered on Atari 2600 emulator.
func Init(def *AtariDef) (*Atari, error) {
	if def.Difficulty[0] == nil || def.Difficulty[1] == nil {
		return nil, errors.New("both difficulty switches must be non-nil in def")
	}
	if def.ColorBW == nil {
		return nil, errors.New("ColorBW must be non-nil in def")
	}
	if def.GameSelect == nil {
		return nil, errors.New("GameSelect must be non-nil in def")
	}
	if def.Reset == nil {
		return nil, errors.New("Reset must be non-nil in def")
	}

	var ch [4]io.PortIn1
	for i, p := range def.Paddles {
		if p != nil {
			if p.Charged == nil || p.Button == nil {
				return nil, fmt.Errorf("paddle %d cannot be defined with a nil Charged or Button: %$v", i, p)
			}
			ch[i] = p.Button
		}
	}

	var b [2]io.PortIn1
	for i, j := range def.Joysticks {
		if j != nil {
			if j.Up == nil || j.Down == nil || j.Left == nil || j.Right == nil {
				return nil, fmt.Errorf("cannot pass in a Joystick for Joystick[%d] with nil members: %#v", i, j)
			}
			b[i] = j.Button
		}
	}

	tia, err := tia.Init(&tia.ChipDef{
		Mode:      def.Mode,
		Port0:     ch[0],
		Port1:     ch[1],
		Port2:     ch[2],
		Port3:     ch[3],
		Port4:     b[0],
		Port5:     b[1],
		IoPortGnd: def.PaddleGround,
		FrameDone: def.FrameDone,
	})
	if err != nil {
		return nil, fmt.Errorf("can't initialize TIA: %v", err)
	}
	a := &Atari{
		tia: tia,
		portA: &portA{
			joysticks: def.Joysticks,
		},
		portB: &portB{
			difficulty: def.Difficulty,
			colorBW:    def.ColorBW,
			gameSelect: def.GameSelect,
			reset:      def.Reset,
		},
	}
	pia, err := pia6532.Init(a.portA, a.portB)
	if err != nil {
		return nil, fmt.Errorf("can't initialize PIA: %v", err)
	}

	a.pia = pia

	// No IRQ in the Atari so those aren't setup.
	c, err := cpu.Init(&cpu.ChipDef{
		Cpu: cpu.CPU_NMOS,
		Ram: a,
		Rdy: tia,
	})
	if err != nil {
		return nil, fmt.Errorf("can't initialize cpu: %v", err)
	}

	a.cpu = c
	return a, nil
}

// Read implements the memory.Ram interface for Read.
// On the Atari this is the main logic for tying the various chips together.
func (a *Atari) Read(addr uint16) uint8 {
	return 0x00
}

// Write implements the memory.Ram interface for Write.
// On the Atari this is the main logic for tying the various chips together.
func (a *Atari) Write(addr uint16, val uint8) {
}

// Write implements the memory.Ram interface for Reset.
// On the Atari this is the main logic for tying the various chips together.
func (a *Atari) Reset() {}

// Write implements the memory.Ram interface for PowerOn.
// On the Atari this is the main logic for tying the various chips together.
func (a *Atari) PowerOn() {}
