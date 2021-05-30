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
	pia        *pia6532.Chip
	tia        *tia.Chip
	cart       memory.Bank
	databusVal uint8
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
			if j.Up == nil || j.Down == nil || j.Left == nil || j.Right == nil || j.Button == nil {
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

	type detector struct {
		detect func([]uint8) bool
		create func([]uint8, memory.Bank) (memory.Bank, error)
	}
	carts := map[int][]detector{
		2048: []detector{
			{IsBasicCart, NewStandardCart},
		},
		4096: []detector{
			{IsBasicCart, NewStandardCart},
		},
		8192: []detector{
			{IsF8BankSwitch, NewF8BankSwitchCart},
		},
		16384: []detector{
			{IsF6SCBankSwitch, NewF6SCBankSwitchCart},
			{IsF6BankSwitch, NewF6BankSwitchCart},
		},
	}
	for _, d := range carts[len(def.Rom)] {
		if d.detect(def.Rom) {
			b, err := d.create(def.Rom, a.memory)
			if err != nil {
				return nil, fmt.Errorf("can't initialize cart: %v", err)
			}
			a.memory.cart = b
			break
		}
	}

	if a.memory.cart == nil {
		return nil, fmt.Errorf("can't determine cart type (%d bytes)", len(def.Rom))
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

	kTIA_MASK         = uint16(0x003F)
	kCpuClockSlowdown = 3
)

// Read implements the memory.Bank interface for Read.
// On the VCS this is the main logic for tying the various chips together.
func (c *controller) Read(addr uint16) uint8 {
	// We only have 13 address pins so mask for that.
	addr &= kADDRESS_MASK

	// The board implements CS for the address/data bus for the TIA
	// and PIA. But..the cart sees all address/data lines always
	// and is supposed to chip select itself. This means a Read()
	// from a TIA address needs to go to the cart and the TIA but
	// only the TIA value returned. This allows some carts to implement
	// bank switching by trapping TIA/PIA address mappings.
	// This also means cart implementations need to validate addresses
	// as necessary.
	read := false
	var ret uint8
	if (addr&kPIA_MASK) == kPIA_MASK && (addr&kROM_MASK) != kROM_MASK {
		if (addr & kPIA_IO_MASK) == kPIA_IO_MASK {
			ret = c.pia.IO().Read(addr)
			//			fmt.Printf("PIA IO: 0x%.4X\n", addr)
		} else {
			ret = c.pia.Read(addr)
			//			fmt.Printf("PIA: 0x%.4X\n", addr)
		}
		read = true
	}
	if !read && (addr&kROM_MASK) != kROM_MASK {
		// TIA is from 0x00-0x3F and mirrors except for the ROM bank (A12) being set.
		ret = c.tia.Read(addr)
		//		fmt.Printf("TIA: 0x%.4X\n", addr)
		read = true
	}
	// Cart see all
	cart := c.cart.Read(addr)

	if read {
		c.databusVal = ret
		return ret
	}
	c.databusVal = cart
	return cart
}

// Write implements the memory.Bank interface for Write.
// On the VCS this is the main logic for tying the various chips together.
func (c *controller) Write(addr uint16, val uint8) {
	// We only have 13 address pins so mask for that.
	addr &= kADDRESS_MASK

	c.databusVal = val

	// See notes in Read() above. Same logic here except
	// there's no return value.
	write := false
	if (addr&kPIA_MASK) == kPIA_MASK && (addr&kROM_MASK) != kROM_MASK {
		if (addr & kPIA_IO_MASK) == kPIA_IO_MASK {
			c.pia.IO().Write(addr, val)
			//			fmt.Printf("PIA IO write: 0x%.4X\n", addr)
		} else {
			c.pia.Write(addr, val)
			//			fmt.Printf("PIA write: 0x%.4X\n", addr)
		}
		write = true
	}
	if !write && (addr&kROM_MASK) != kROM_MASK {
		// TIA is from 0x00-0x3F and mirrors except for the ROM bank (A12) being set.
		c.tia.Write(addr, val)
		//		fmt.Printf("TIA write: 0x%.4X\n", addr)
	}
	// Cart gets a copy always
	c.cart.Write(addr, val)
	return
}

// PowerOn implements the memory.Bank interface for PowerOn.
func (c *controller) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank
// which for a controller is nil since it's the top of the pile always.
func (c *controller) Parent() memory.Bank {
	return nil
}

// DatabusVal returns the most recent seen databus item.
func (c *controller) DatabusVal() uint8 {
	return c.databusVal
}

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
