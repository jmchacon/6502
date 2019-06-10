// Package atari2600 is the main logic for pulling together an atari 2600 emulator.
// The actual chips are implemented in other packages and most the logic here is
// simply to pull together the memory mappings for them.
package atari2600

import (
	"errors"
	"fmt"
	"image/draw"
	"log"

	"github.com/jmchacon/6502/cpu"
	"github.com/jmchacon/6502/io"
	"github.com/jmchacon/6502/memory"
	"github.com/jmchacon/6502/pia6532"
	"github.com/jmchacon/6502/tia"
)

// VCS defines all the part which bring a complete Atari 2600 together.
// 2 input ports and a cpu and memory controller.
type VCS struct {
	portA    *portA
	portB    *portB
	cpuClock int
	cpu      *cpu.Chip
	memory   *controller
	debug    bool
}

// controller defines the various memory mapped components of the
// system including the PIA, TIA chips and then a cart abstraction
// which properly handles a given cart (which may internally handle
// bank switching as needed)
type controller struct {
	pia  *pia6532.Chip
	tia  *tia.Chip
	cart memory.Ram
}

// VCSDef defines the pieces needed to setup a basic Atari 2600. Assuming up to 2 joysticks and 4 paddles.
// TODO(jchacon): Add other controller types (wheel, keyboard, etc).
type VCSDef struct {
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

	// Image used to render output during a frame render. It is the same one passed to FrameDone.
	Image draw.Image

	// ScaleFactor indicates a scaling factor to apply when rendering into the image. If not set (i.e. 0)
	// this will default to 1. The Image must be at least the ScaleFactor * Mode size.
	ScaleFactor int

	// FrameDone is called on every VSYNC transition cycle. See tia documentation for more details.
	FrameDone func(draw.Image)

	// Rom is the data to load for this instance into the ROM space. It must be 2k or 4k in length.
	// A 2k ROM will be properly mirrored.
	// TODO(jchacon): Support other carts.
	Rom []uint8

	// Debug if true wll emit output from Debug() calls to the PIA, TIA and CPU chips.
	Debug bool
}

// Init returns an initialized and powered on Atari 2600 emulator.
func Init(def *VCSDef) (*VCS, error) {
	// Up front validation.
	if len(def.Rom)%2048 != 0 {
		return nil, fmt.Errorf("rom must be a multiple of 2k in size. Got %d bytes", len(def.Rom))
	}
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
	if def.Image == nil {
		return nil, errors.New("Image must be non-nil in def")
	}

	var ch [4]io.PortIn1
	var paddles bool
	for i, p := range def.Paddles {
		if p != nil {
			if p.Charged == nil || p.Button == nil {
				return nil, fmt.Errorf("paddle %d cannot be defined with a nil Charged or Button: %#v", i, p)
			}
			ch[i] = p.Charged
			paddles = true
		}
	}

	var b [2]io.PortIn1
	for i, j := range def.Joysticks {
		if j != nil {
			if paddles {
				return nil, errors.New("cannot have paddles and joysticks defined at the same time")
			}
			if j.Up == nil || j.Down == nil || j.Left == nil || j.Right == nil {
				return nil, fmt.Errorf("cannot pass in a Joystick for Joystick[%d] with nil members: %#v", i, j)
			}
			b[i] = j.Button
		}
	}

	// Order is important since the chips depend on each other.
	tia, err := tia.Init(&tia.ChipDef{
		Mode:        def.Mode,
		Port0:       ch[0],
		Port1:       ch[1],
		Port2:       ch[2],
		Port3:       ch[3],
		Port4:       b[0],
		Port5:       b[1],
		IoPortGnd:   def.PaddleGround,
		Image:       def.Image,
		ScaleFactor: def.ScaleFactor,
		FrameDone:   def.FrameDone,
		Debug:       def.Debug,
	})
	if err != nil {
		return nil, fmt.Errorf("can't initialize TIA: %v", err)
	}
	a := &VCS{
		portA: &portA{
			joysticks: def.Joysticks,
			paddles:   def.Paddles,
		},
		portB: &portB{
			difficulty: def.Difficulty,
			colorBW:    def.ColorBW,
			gameSelect: def.GameSelect,
			reset:      def.Reset,
		},
		memory: &controller{
			tia: tia,
		},
		debug: def.Debug,
	}

	switch {
	case len(def.Rom) == 2048 || len(def.Rom) == 4096:
		b, err := NewStandardCart(def.Rom)
		if err != nil {
			return nil, fmt.Errorf("can't initialize cart: %v", err)
		}
		a.memory.cart = b
	default:
		// TODO(jchacon): Implement support for bank switching
		b, err := NewPlaceholder(def.Rom)
		if err != nil {
			return nil, fmt.Errorf("can't initialize cart: %v", err)
		}
		a.memory.cart = b
	}

	pia, err := pia6532.Init(&pia6532.ChipDef{
		PortA: a.portA,
		PortB: a.portB,
		Debug: def.Debug,
	})
	if err != nil {
		return nil, fmt.Errorf("can't initialize PIA: %v", err)
	}

	a.memory.pia = pia

	// No IRQ in the VCS so those aren't setup.
	// Note there is some circular dependencies here as the CPU depends
	// on VCS for it's memory and the VCS needs to know about the CPU for
	// executing Tick() against it.
	c, err := cpu.Init(&cpu.ChipDef{
		Cpu:   cpu.CPU_NMOS,
		Ram:   a.memory,
		Rdy:   tia,
		Debug: def.Debug,
	})
	if err != nil {
		return nil, fmt.Errorf("can't initialize cpu: %v", err)
	}

	a.cpu = c
	return a, nil
}

const (
	kADDRESS_MASK = uint16(0x1FFF)

	kROM_MASK = uint16(0x1000)

	kPIA_MASK    = uint16(0x0080)
	kPIA_IO_MASK = uint16(0x0280)

	kCpuClockSlowdown = 3
)

// Read implements the memory.Ram interface for Read.
// On the VCS this is the main logic for tying the various chips together.
func (c *controller) Read(addr uint16) uint8 {
	// We only have 13 address pins so mask for that.
	addr &= kADDRESS_MASK

	switch {
	case (addr & kROM_MASK) == kROM_MASK:
		return c.cart.Read(addr)
	case (addr & kPIA_MASK) == kPIA_MASK:
		if (addr & kPIA_IO_MASK) == kPIA_IO_MASK {
			return c.pia.IO().Read(addr)
		}
		return c.pia.Read(addr)
	}
	// Anything else is the TIA
	return c.tia.Read(addr)
}

// Write implements the memory.Ram interface for Write.
// On the VCS this is the main logic for tying the various chips together.
func (c *controller) Write(addr uint16, val uint8) {
	// We only have 13 address pins so mask for that.
	addr &= kADDRESS_MASK

	switch {
	case (addr & kROM_MASK) == kROM_MASK:
		c.cart.Write(addr, val)
		return
	case (addr & kPIA_MASK) == kPIA_MASK:
		if (addr & kPIA_IO_MASK) == kPIA_IO_MASK {
			c.pia.IO().Write(addr, val)
			return
		}
		c.pia.Write(addr, val)
		return
	}
	// Anything else is the TIA
	c.tia.Write(addr, val)
}

// PowerOn implements the memory.Ram interface for PowerOn.
func (c *controller) PowerOn() {}

// Tick implements basic running of the Atari by ticking all the components
// as needed. CPU/PIA run at 1/3 the rate of the TIA. Best to use the TIA FrameDone callback
// for synchronizing output to somewhere (file/UI/etc).
func (a *VCS) Tick() error {
	if err := a.memory.tia.Tick(); err != nil {
		return fmt.Errorf("TIA tick error: %v", err)
	}
	a.cpuClock = (a.cpuClock + 1) % kCpuClockSlowdown

	if a.cpuClock == 0 {
		// The PIA runs on the same clock as the CPU (1/3'd the speed of the TIA).
		if a.debug {
			if d := a.memory.pia.Debug(); d != "" {
				log.Printf("PIA: %s", d)
			}
			if d := a.cpu.Debug(); d != "" {
				log.Printf("CPU: %s", d)
			}
		}
		if err := a.memory.pia.Tick(); err != nil {
			return fmt.Errorf("PIA tick error: %v", err)
		}
		if err := a.cpu.Tick(); err != nil {
			return fmt.Errorf("CPU tick error: %v", err)
		}
		a.memory.pia.TickDone()
		a.cpu.TickDone()
	}
	a.memory.tia.TickDone()
	return nil
}
