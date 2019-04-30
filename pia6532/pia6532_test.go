package pia6532

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func TestRam(t *testing.T) {
	p, err := Init(&ChipDef{})
	if err != nil {
		t.Fatalf("Can't init: %v", err)
	}

	// Put our own RAM in so we can manipulate directly below.
	r := &piaRam{}
	p.ram = r

	// Make sure RAM works for the basic 128 addresses including aliasing.
	for i := uint16(0x0000); i < 0xFFFF; i++ {
		// Force write a different value in.
		r.addr[i&kMASK_RAM] = uint8(^i)
		p.Write(i, uint8(i))
		if got, want := p.Read(i), uint8(i); got != want {
			t.Errorf("Bad Write/Read cycle for RAM: Wrote %.2X to %.4X but got %.2X on read", want, i, got)
		}
	}
}

func TestErrors(t *testing.T) {
	p, err := Init(&ChipDef{})
	if err != nil {
		t.Fatalf("Can't init: %v", err)
	}

	if err := p.Tick(); err != nil {
		t.Errorf("Unexpected error on first tick: %v", err)
	}
	if err := p.Tick(); err == nil {
		t.Error("Didn't get error on back-back Ticks?")
	}
}

func TestTimer(t *testing.T) {
	tests := []struct {
		name      string
		addr      uint16
		timerVal  uint8
		timerMult uint16
		interrupt bool
		overrun   uint8
	}{
		{
			name:      "1x with no interrupt",
			addr:      kWRITE_TIMER_1_NO_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT1,
			interrupt: false,
			overrun:   0x10,
		},
		{
			name:      "8x with no interrupt",
			addr:      kWRITE_TIMER_8_NO_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT8,
			interrupt: false,
			overrun:   0x10,
		},
		{
			name:      "64x with no interrupt",
			addr:      kWRITE_TIMER_64_NO_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT64,
			interrupt: false,
			overrun:   0x10,
		},
		{
			name:      "1024x with no interrupt",
			addr:      kWRITE_TIMER_1024_NO_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT1024,
			interrupt: false,
			overrun:   0x10,
		},
		{
			name:      "1x with interrupt",
			addr:      kWRITE_TIMER_1_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT1,
			interrupt: true,
			overrun:   0x10,
		},
		{
			name:      "8x with interrupt",
			addr:      kWRITE_TIMER_8_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT8,
			interrupt: true,
			overrun:   0x10,
		},
		{
			name:      "64x with interrupt",
			addr:      kWRITE_TIMER_64_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT64,
			interrupt: true,
			overrun:   0x10,
		},
		{
			name:      "1024x with interrupt",
			addr:      kWRITE_TIMER_1024_INT,
			timerVal:  0x76,
			timerMult: kTIMER_MULT1024,
			interrupt: true,
			overrun:   0x10,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, err := Init(&ChipDef{})
			if err != nil {
				t.Fatalf("Can't init: %v", err)
			}
			if p.Raised() {
				t.Errorf("%s: interrupt raised when not expected post init?", test.name)
			}
			p.IO().Write(test.addr, test.timerVal)
			// Have to run a tick off after a write in order for it to take effect. i.e. The p.Write happened
			// during a CPU tick that happens in HW at the same time as this tick.
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: Unexpected error: %v", test.name, err)
			}
			p.TickDone()
			for i := test.timerVal; i > 0x00; i-- {
				// These have to be a fatal since erroring on every iteration is too much.
				for j := uint16(0x0000); j < test.timerMult; j++ {
					if err := p.Tick(); err != nil {
						t.Fatalf("%s: Unexpected error: %v", test.name, err)
					}
					p.TickDone()
					if p.Raised() {
						t.Fatalf("%s: Interrupt raised on tick %.2X when not expected", test.name, i)
					}
				}
				// Subtract one because timer should have decremented by now.
				if got, want := p.timer, i-1; got != want {
					t.Fatalf("%s: Timer value not correct. Got %.2X and want %.2X\n%s", test.name, got, want, spew.Sdump(p))
				}
			}
			// Should be at timer 0 now
			if got, want := p.timer, uint8(0x00); got != want {
				t.Errorf("%s: Didn't get expected timer value at end. Got %.2X and want %.2X", test.name, got, want)
			}
			// Interrupts shouldn't be raised here (yet).
			if got, want := p.Raised(), false; got != want {
				t.Errorf("%s: Interrupt state (pre tick below 0) not as expected. Got %t and want %t", test.name, got, want)
			}
			// We always overrun one to test interrupts
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: Unexpected error ticking for overrun: %v", test.name, err)
			}
			p.TickDone()
			if got, want := p.Raised(), test.interrupt; got != want {
				t.Errorf("%s: Interrupt state not as expected. Got %t and want %t", test.name, got, want)
			}
			if got, want := p.timer, uint8(0xFF); got != want {
				t.Errorf("%s: Invalid timer count after expiration. Got %.2X and want %.2X", test.name, got, want)
			}
			// Read the timer in the same interrupt state we set.
			r := kREAD_TIMER_NO_INT
			if test.interrupt {
				r = kREAD_TIMER_INT
			}
			if got, want := p.IO().Read(r), uint8(0xFF); got != want {
				t.Errorf("%s: Invalid timer count after expiration using Read Got %.2X and want %.2X", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: Unexpected error ticking for overrun: %v", test.name, err)
			}
			p.TickDone()
			// Interrupt state shouldn't change on the 0xFF specific case (i.e. interrupt turning on during the same tick).
			if got, want := p.Raised(), test.interrupt; got != want {
				t.Errorf("%s: Interrupt state not as expected. Got %t and want %t", test.name, got, want)
			}

			for i := uint8(0); i < test.overrun; i++ {
				if err := p.Tick(); err != nil {
					t.Fatalf("%s: Unexpected error ticking for overrun: %v", test.name, err)
				}
				p.TickDone()
				if got, want := p.Raised(), test.interrupt; got != want {
					t.Errorf("%s: Interrupt state during overrun not as expected. Got %t and want %t", test.name, got, want)
				}
			}
			if got, want := p.timer, 0xFE-test.overrun; got != want {
				t.Errorf("%s: Invalid timer count after overrun. Got %.2X and want %.2X", test.name, got, want)
			}
			// Now read the timer through the actual Read interface and verify interrupts are always false now.
			if got, want := p.IO().Read(kREAD_TIMER_NO_INT), 0xFE-test.overrun; got != want {
				t.Errorf("%s: Invalid timer count (via Read) after overrun. Got %.2X and want %.2X", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: Unexpected error ticking for overrun: %v", test.name, err)
			}
			p.TickDone()
			if got, want := p.Raised(), false; got != want {
				t.Errorf("%s: After timer read %.4X interrupt should always be false", test.name, kREAD_TIMER_NO_INT)
			}
			// Now read it again and force interrupts to stay on (though docs say this isn't likely what you want).
			if got, want := p.IO().Read(kREAD_TIMER_INT), 0xFE-(test.overrun+1); got != want {
				t.Errorf("%s: Invalid timer count2 (via Read) after overrun. Got %.2X and want %.2X", test.name, got, want)
			}
			// Need to tick again for PIA to set states.
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: error ticking for interrupt check: %v", test.name, err)
			}
			p.TickDone()
			// Need to tick again for PIA to set states.
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: error ticking for interrupt check: %v", test.name, err)
			}
			p.TickDone()
			if got, want := p.Raised(), true; got != want {
				t.Errorf("%s: After timer read %.4X interrupt %t and should be %t\n%s", test.name, kREAD_TIMER_INT, got, want, spew.Sdump(p))
			}
		})
	}
}

type in struct {
	data uint8
}

func (i *in) Input() uint8 {
	return i.data
}

func TestInterruptState(t *testing.T) {
	tests := []struct {
		name     string
		regNoInt uint16
		regInt   uint16
		style    edgeType
	}{
		{
			name:     "Negative edge",
			regNoInt: kWRITE_NEG_NO_INT,
			regInt:   kWRITE_NEG_INT,
			style:    kEDGE_NEGATIVE,
		},
		{
			name:     "Positive edge",
			regNoInt: kWRITE_POS_NO_INT,
			regInt:   kWRITE_POS_INT,
			style:    kEDGE_POSITIVE,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			portA := &in{}
			p, err := Init(&ChipDef{portA, nil, false})
			if err != nil {
				t.Fatalf("Can't init: %v", err)
			}
			p.IO().Write(test.regNoInt, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(kWRITE_TIMER_1_NO_INT, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			if got, want := p.IO().Read(kREAD_INT), uint8(0x00); got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			if got, want := p.edgeStyle, test.style; got != want {
				t.Errorf("%s: Invalid edge style. Got %d and want %d", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(test.regInt, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(kWRITE_TIMER_1_NO_INT, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			if got, want := p.IO().Read(kREAD_INT), kMASK_EDGE; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			if got, want := p.edgeStyle, test.style; got != want {
				t.Errorf("%s: Invalid edge style. Got %d and want %d", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			// Should be off on a 2nd read.
			if got, want := p.IO().Read(kREAD_INT), uint8(0x00); got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			p.IO().Write(test.regNoInt, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(kWRITE_TIMER_1_INT, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			if got, want := p.IO().Read(kREAD_INT), kMASK_INT; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			if got, want := p.edgeStyle, test.style; got != want {
				t.Errorf("%s: Invalid edge style. Got %d and want %d", test.name, got, want)
			}
			p.IO().Write(test.regInt, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(kWRITE_TIMER_1_INT, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			if got, want := p.IO().Read(kREAD_INT), kMASK_INT|kMASK_EDGE; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			if got, want := p.edgeStyle, test.style; got != want {
				t.Errorf("%s: Invalid edge style. Got %d and want %d", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			// Edge should be off on a 2nd read.
			if got, want := p.IO().Read(kREAD_INT), kMASK_INT; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}

			// Setup edge again (but disable timer) and then trigger it.
			p.IO().Write(test.regInt, 0xFF)
			p.IO().Write(kWRITE_TIMER_1_NO_INT, 0xFF)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			portA.data = 0x80
			p.holdPortA = 0x00
			if test.style == kEDGE_POSITIVE {
				portA.data = 0x00
				p.holdPortA = 0x80
			}
			// Verify not raising interrupt
			if got, want := p.Raised(), false; got != want {
				t.Errorf("%s: invalid interrupt state got %t and want %t", test.name, got, want)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: unexpected error during tick: %v", test.name, err)
			}
			p.TickDone()
			if got, want := p.Raised(), true; got != want {
				t.Errorf("%s: invalid edge interrupt - got %t and want %t", test.name, got, want)
			}
			// Verify only edge ones could be firing.
			if got, want := p.IO().Read(kREAD_INT), kMASK_EDGE; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}
			// Edge should be off on a 2nd read.
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			if got, want := p.IO().Read(kREAD_INT), kMASK_NONE; got != want {
				t.Errorf("%s: Expected interrupt state %.2X and got %.2X", test.name, want, got)
			}

			// Now do the same tests on the output side
			p.IO().Write(test.regInt, 0xFF)
			p.IO().Write(kWRITE_TIMER_1_NO_INT, 0xFF)
			p.IO().Write(kWRITE_PORT_A_DDR, 0x80)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			// Verify not raising interrupt
			if got, want := p.Raised(), false; got != want {
				t.Errorf("%s: invalid interrupt state got %t and want %t", test.name, got, want)
			}
			// Default negative
			first := uint8(0x80)
			second := uint8(0x00)
			if test.style == kEDGE_POSITIVE {
				first = 0x00
				second = 0x80
			}
			p.IO().Write(kWRITE_PORT_A, first)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			p.IO().Write(kWRITE_PORT_A, second)
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			// Should happen immediately (i.e. doesn't take another tick)
			if got, want := p.Raised(), true; got != want {
				t.Errorf("%s: invalid output edge interrupt first %.2X seond %.2X - got %t and want %t", test.name, first, second, got, want)
			}
			// Should also continue through a tick.
			if err := p.Tick(); err != nil {
				t.Fatalf("%s: unexpected error during tick: %v", test.name, err)
			}
			p.TickDone()
			if got, want := p.Raised(), true; got != want {
				t.Errorf("%s: invalid output edge interrupt - got %t and want %t", test.name, got, want)
			}
			// Verify only edge ones could be firing.
			if got, want := p.IO().Read(kREAD_INT), kMASK_EDGE; got != want {
				t.Errorf("%s: Expected output interrupt state %.2X and got %.2X", test.name, want, got)
			}
			if err := p.Tick(); err != nil {
				t.Fatalf("Unexpected tick error: %v", err)
			}
			p.TickDone()
			// Verify no interrupt flags are set now.
			if got, want := p.IO().Read(kREAD_INT), kMASK_NONE; got != want {
				t.Errorf("%s: Expected output interrupt state %.2X and got %.2X", test.name, want, got)
			}
			// Interrupt should have also been disabled since only one firing was edge and above read stops it.
			if got, want := p.Raised(), false; got != want {
				t.Errorf("%s: invalid output edge interrupt - got %t and want %t", test.name, got, want)
			}

			// Finally, set an impossible edge state to make sure errors happen
			p.edgeStyle = kEDGE_UNIMPLEMENTED
			if err := p.Tick(); err == nil {
				t.Fatalf("%s: Should have gotten an error for invalid edge style...", test.name)
			}
			p.TickDone()
		})
	}

}

func TestPorts(t *testing.T) {
	portA := &in{0xA5}
	portB := &in{0xAA}
	p, err := Init(&ChipDef{portA, portB, false})
	if err != nil {
		t.Fatalf("Can't init: %v", err)
	}

	// Set portA DDR to all output
	p.IO().Write(kWRITE_PORT_A_DDR, 0xFF)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Set portB DDR to all input
	p.IO().Write(kWRITE_PORT_B_DDR, 0x00)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Verify portA DDR
	if got, want := p.IO().Read(kREAD_PORT_A_DDR), uint8(0xFF); got != want {
		t.Errorf("Didn't get expected port A DDR. Got %.2X and want %.2X", got, want)
	}
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Verify portB DDR
	if got, want := p.IO().Read(kREAD_PORT_B_DDR), uint8(0x00); got != want {
		t.Errorf("Didn't get expected port B DDR. Got %.2X and want %.2X", got, want)
	}
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Write out to port A
	p.IO().Write(kWRITE_PORT_A, 0xAA)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Write out to port B
	p.IO().Write(kWRITE_PORT_B, 0x55)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Verify port A output.
	if got, want := p.PortA().Output(), uint8(0xAA); got != want {
		t.Errorf("Bad portA output data. Got %.2X and want %.2X", got, want)
	}
	// Verify port B (should be 0xFF since pullups).
	if got, want := p.PortB().Output(), uint8(0xFF); got != want {
		t.Errorf("Bad portB output data. Got %.2X and want %.2X", got, want)
	}
	// Read portA (should be 0xA0 since input and output both holding those bits high.
	if got, want := p.IO().Read(kREAD_PORT_A), uint8(0xA0); got != want {
		t.Errorf("Bad portA input data. Got %.2X and want %.2X", got, want)
	}
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Same with portB except input signals mask correctly (internal pullups).
	if got, want := p.IO().Read(kREAD_PORT_B), uint8(0xAA); got != want {
		t.Errorf("Bad portB input data. Got %.2X and want %.2X", got, want)
	}
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()

	// Simulate atari 2600 combat where Port B pins 2,4,5 are unused and can be set to output to store data.
	// So 00110100 == 0x34
	p.IO().Write(kWRITE_PORT_B_DDR, 0x34)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// Reset portB input to not overlap the bits set above.
	portB.data = 0xC0
	// Write out to port B the bits we can set but also another we shouldn't (set bit 0).
	p.IO().Write(kWRITE_PORT_B, 0x35)
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
	// So reading now should give back 0xF4 since we'll OR in the set output bits for 2,4,5.
	if got, want := p.IO().Read(kREAD_PORT_B), uint8(0xF4); got != want {
		t.Errorf("Bad portB input data with output set. Got %.2X and want %.2X", got, want)
	}
	if err := p.Tick(); err != nil {
		t.Fatalf("Unexpected tick error: %v", err)
	}
	p.TickDone()
}
