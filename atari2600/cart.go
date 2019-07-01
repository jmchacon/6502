package atari2600

import (
	"bytes"
	"fmt"
	"math"

	"github.com/jmchacon/6502/memory"
)

// basicCart implements support for a 2k or 4k ROM. For 2k the upper half is simply
// a mirror of the lower half. The simplest implementation of carts.
type basicCart struct {
	rom        []uint8
	mask       uint16
	parent     memory.Bank
	databusVal uint8
}

func NewStandardCart(rom []uint8, parent memory.Bank) (memory.Bank, error) {
	// Technically any cart size that is divisible by 2 and up to 4k we can handle and alias.
	got := len(rom)
	if got%2 != 0 || got > 4096 {
		return nil, fmt.Errorf("invalid StandardCart. Must be divisible by 2 and <= 4k in length. Got %d bytes", got)
	}
	mask := k4K_MASK >> uint(math.Log2(float64(4096/got)))
	b := &basicCart{
		rom:    rom,
		mask:   mask,
		parent: parent,
	}
	return b, nil
}

const (
	k2K_MASK = uint16(0x07FF)
	k4K_MASK = uint16(0x0FFF)
)

// Read implements the memory.Bank interface for Read.
// For a 2k ROM cart this means mirroring the lower 2k to the upper 2k
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space.
func (b *basicCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		// Move it into a range for indexing into our byte array and
		// normalized for 2k.
		val := b.rom[addr&b.mask]
		b.databusVal = val
		return val
	}
	// Outside our range so just return 0.
	b.databusVal = 0
	return 0
}

// Write implements the memory.Bank interface for Write.
// For a 2k or 4k ROM cart with no onboard RAM this does nothing
func (b *basicCart) Write(addr uint16, val uint8) {
	b.databusVal = val
}

// PowerOn implements the memory.Bank interface for PowerOn.
func (b *basicCart) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank.
func (b *basicCart) Parent() memory.Bank {
	return b.parent
}

// DatabusVal returns the most recent seen databus item.
func (b *basicCart) DatabusVal() uint8 {
	return b.databusVal
}

func scanSequence(rom []uint8, match []byte, nextByte byte) (bool, int) {
	cnt := 0
	idxs := bytes.SplitAfter(rom, match)
	for i := range idxs {
		cnt += len(idxs[i])
		if i != len(idxs)-1 {
			if idxs[i+1][0]&nextByte == nextByte {
				return true, cnt + 1
			}
		}
	}
	return false, -1
}

type matcher struct {
	match    []byte
	nextByte byte
	banks    []int
	desc     string
}

func runMatcher(rom []uint8, matchers [][]matcher) bool {
	// Run through both sets of tests but only advance to the 2nd if the first finds something.
	cnt := 0
	for _, tests := range matchers {
		cnt = 0
		for _, test := range tests {
			// Work through each match in sequence until we find one in the right bank or we run out of rom.
			for i := 0; i < len(rom); {
				if found, offset := scanSequence(rom[i:], test.match, test.nextByte); found {
					i += offset
					for _, bank := range test.banks {
						if i >= 4096*bank && i < 4096*(bank+1) {
							fmt.Printf("Found match on %s at 0x%.4X\n", test.desc, i)
							cnt++
							break
						}
					}
					// Found in one of the banks so we can quit this test.
					if cnt > 0 {
						break
					}
				} else {
					i = len(rom)
				}
			}
		}
		// No match in this block so just stop.
		if cnt == 0 {
			break
		}
	}
	if cnt > 0 {
		return true
	}
	return false
}

func IsBasicCart(rom []uint8) bool {
	if len(rom) <= 4096 {
		return true
	}
	return false
}

func IsF8BankSwitch(rom []uint8) bool {
	if len(rom) == 8192 {
		// Need one from each type. There needs to be something poking 0x1FF8 and something else touching 0x1FF9.
		test1 := []matcher{
			{[]byte{0xAD, 0xF8}, 0x1F, []int{1}, "LDA 0x1FF8"},
			{[]byte{0x8D, 0xF8}, 0x1F, []int{1}, "STA 0x1FF8"},
			{[]byte{0x2C, 0xF8}, 0x1F, []int{1}, "BIT 0x1FF8"},
		}
		test2 := []matcher{
			{[]byte{0xAD, 0xF9}, 0x1F, []int{0}, "LDA 0x1FF9"},
			{[]byte{0x8D, 0xF9}, 0x1F, []int{0}, "STA 0x1FF9"},
			{[]byte{0x2C, 0xF9}, 0x1F, []int{0}, "BIT 0x1FF9"},
			// Some games don't make this simple as they build the bank switch into RAM
			// and then jump into it.
			//
			// Raiders of the Lost Ark
			{[]byte{0xA9, 0xAD, 0x85, 0x84, 0xA9, 0xF9, 0x85, 0x85, 0xA9}, 0x1F, []int{0}, "LDA #AD STA 84 LDA #F9 STA 85 LDA #1F"},
			// E.T.
			{[]byte{0xA9, 0xAD, 0x85, 0x83, 0xA9, 0xF9, 0x85, 0x84, 0xA9}, 0x1F, []int{0}, "LDA #AD STA 83 LDA #F9 STA 84 LDA #1F"},
		}
		return runMatcher(rom, [][]matcher{test1, test2})
	}
	return false
}

func IsF6BankSwitch(rom []uint8) bool {
	if len(rom) == 16384 {
		// There are 4 banks here so we want to see the other banks touched
		// from somewhere other than their own.
		test1 := []matcher{
			{[]byte{0xAD, 0xF6}, 0x1F, []int{1, 2, 3}, "LDA 0x1FF6"},
			{[]byte{0x8D, 0xF6}, 0x1F, []int{1, 2, 3}, "STA 0x1FF6"},
			{[]byte{0x2C, 0xF6}, 0x1F, []int{1, 2, 3}, "BIT 0x1FF6"},
		}
		test2 := []matcher{
			{[]byte{0xAD, 0xF7}, 0x1F, []int{0, 2, 3}, "LDA 0x1FF7"},
			{[]byte{0x8D, 0xF7}, 0x1F, []int{0, 2, 3}, "STA 0x1FF7"},
			{[]byte{0x2C, 0xF7}, 0x1F, []int{0, 2, 3}, "BIT 0x1FF7"},
		}
		test3 := []matcher{
			{[]byte{0xAD, 0xF8}, 0x1F, []int{0, 1, 3}, "LDA 0x1FF8"},
			{[]byte{0x8D, 0xF8}, 0x1F, []int{0, 1, 3}, "STA 0x1FF8"},
			{[]byte{0x2C, 0xF8}, 0x1F, []int{0, 1, 3}, "BIT 0x1FF8"},
		}
		test4 := []matcher{
			{[]byte{0xAD, 0xF9}, 0x1F, []int{0, 1, 2}, "LDA 0x1FF9"},
			{[]byte{0x8D, 0xF9}, 0x1F, []int{0, 1, 2}, "STA 0x1FF9"},
			{[]byte{0x2C, 0xF9}, 0x1F, []int{0, 1, 2}, "BIT 0x1FF9"},
		}
		return runMatcher(rom, [][]matcher{test1, test2, test3, test4})
	}
	return false
}

func IsF6SCBankSwitch(rom []uint8) bool {
	if len(rom) == 16384 {
		// This should show 0x00->0x7f == 0x80-0xff if it's using a SC.
		if bytes.Compare(rom[0x00:0x80], rom[0x80:0x100]) == 0 {
			// Now that that passed detect if it bank switches correctly too.
			return IsF6BankSwitch(rom)
		}
	}
	return false
}

// f8BankSwitchCart implements support for F8 style bank switching. 8k cart where access to 0x1FF8 accesses
// the first 4k and 0x1FF9 access the other 4k.
type f8BankSwitchCart struct {
	rom        []uint8
	lowBank    bool
	parent     memory.Bank
	databusVal uint8
}

func NewF8BankSwitchCart(rom []uint8, parent memory.Bank) (memory.Bank, error) {
	if len(rom) != 8192 {
		return nil, fmt.Errorf("F8BankSwitchCart must be 8k in length. Got %d bytes", len(rom))
	}
	p := &f8BankSwitchCart{
		rom:     rom,
		lowBank: true,
		parent:  parent,
	}
	return p, nil
}

// Read implements the memory.Bank interface for Read.
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space. If the special addresses are triggered this
// immediately does the bank switch to the appropriate bank.
func (f *f8BankSwitchCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		if addr&0x1FFF == 0x1FF8 {
			f.lowBank = true
		}
		if addr&0x1FFF == 0x1FF9 {
			f.lowBank = false
		}
		off := uint16(0)
		if !f.lowBank {
			off = 4096
		}
		// Move it into a range for indexing into our byte array.
		val := f.rom[(addr&k4K_MASK)+off]
		f.databusVal = val
		return val
	}
	// Outside our range so just return 0.
	f.databusVal = 0
	return 0
}

// Write implements the memory.Bank interface for Write.
// For a 8k ROM cart with no onboard RAM this does nothing except trigger
// bank switching.
func (f *f8BankSwitchCart) Write(addr uint16, val uint8) {
	f.databusVal = val
	if (addr & kROM_MASK) == kROM_MASK {
		if addr&0x1FFF == 0x1FF8 {
			f.lowBank = true
		}
		if addr&0x1FFF == 0x1FF9 {
			f.lowBank = false
		}
	}
}

// PowerOn implements the memory.Bank interface for PowerOn.
func (b *f8BankSwitchCart) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank.
func (b *f8BankSwitchCart) Parent() memory.Bank {
	return b.parent
}

// DatabusVal returns the most recent seen databus item.
func (b *f8BankSwitchCart) DatabusVal() uint8 {
	return b.databusVal
}

// f6BankSwitchCart implements support for F6 style bank switching. 16k cart where access to 0x1FF6 accesses
// the first 4k and so on through 0x1FF9 accessing the 4th bank.
type f6BankSwitchCart struct {
	rom        []uint8
	bank       uint16
	parent     memory.Bank
	databusVal uint8
}

func NewF6BankSwitchCart(rom []uint8, parent memory.Bank) (memory.Bank, error) {
	if len(rom) != 16384 {
		return nil, fmt.Errorf("F6BankSwitchCart must be 16k in length. Got %d bytes", len(rom))
	}
	p := &f6BankSwitchCart{
		rom:    rom,
		parent: parent,
	}
	return p, nil
}

// Read implements the memory.Bank interface for Read.
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space. If the special addresses are triggered this
// immediately does the bank switch to the appropriate bank.
func (f *f6BankSwitchCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		f.switchBank(addr)
		off := f.bank * 4096
		// Move it into a range for indexing into our byte array.
		val := f.rom[(addr&k4K_MASK)+off]
		f.databusVal = val
		return val
	}
	// Outside our range so just return 0.
	f.databusVal = 0
	return 0
}

func (f *f6BankSwitchCart) switchBank(addr uint16) {
	switch addr & 0x1FFF {
	case 0x1FF6:
		f.bank = 0
	case 0x1FF7:
		f.bank = 1
	case 0x1FF8:
		f.bank = 2
	case 0x1FF9:
		f.bank = 3
	}
}

// Write implements the memory.Bank interface for Write.
// For a 8k ROM cart with no onboard RAM this does nothing except trigger
// bank switching.
func (f *f6BankSwitchCart) Write(addr uint16, val uint8) {
	f.databusVal = val
	if (addr & kROM_MASK) == kROM_MASK {
		f.switchBank(addr)
	}
}

// PowerOn implements the memory.Bank interface for PowerOn.
func (b *f6BankSwitchCart) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank.
func (b *f6BankSwitchCart) Parent() memory.Bank {
	return b.parent
}

// DatabusVal returns the most recent seen databus item.
func (b *f6BankSwitchCart) DatabusVal() uint8 {
	return b.databusVal
}

// f6SCBankSwitchCart implements support for F6 style bank switching with a SuperChip.
// 16k cart where access to 0x1FF6 accessesthe first 4k and so on through 0x1FF9 accessing the 4th bank.
// There's also 128 bytes of RAM from 0x00->0xFF (bottom half is write port, top half is read port).
type f6SCBankSwitchCart struct {
	rom        []uint8
	bank       uint16
	ram        memory.Bank
	parent     memory.Bank
	databusVal uint8
}

func NewF6SCBankSwitchCart(rom []uint8, parent memory.Bank) (memory.Bank, error) {
	if len(rom) != 16384 {
		return nil, fmt.Errorf("F6BankSwitchCart must be 16k in length. Got %d bytes", len(rom))
	}
	p := &f6SCBankSwitchCart{
		rom:    rom,
		parent: parent,
	}
	var err error
	if p.ram, err = memory.New8BitRAMBank(128, p); err != nil {
		return nil, fmt.Errorf("can't initialize RAM: %v", err)
	}
	return p, nil
}

// Read implements the memory.Bank interface for Read.
// The address passed in is only assumed to map into the 4k ROM somewhere
// in the address space. If the special addresses are triggered this
// immediately does the bank switch to the appropriate bank.
// If the range is for the RAM it will use that instead.
func (f *f6SCBankSwitchCart) Read(addr uint16) uint8 {
	if (addr & kROM_MASK) == kROM_MASK {
		f.switchBank(addr)
		// Hit the read address so return RAM value.
		if addr&0x1FFF >= 0x1080 && addr&0x1FFF <= 0x10FF {
			val := f.ram.Read(addr & k4K_MASK)
			f.databusVal = val
			return val
		}
		// If this ends up in the write window for the RAM side effects
		// happen based on the most recent value sitting on the databus.
		if addr&0x1FFF < 0x1080 {
			val := memory.LatestDatabusVal(f)
			f.ram.Write(addr&k4K_MASK, val)
			f.databusVal = val
			return val
		}
		// Otherwise it's a real ROM read.
		off := f.bank * 4096
		// Move it into a range for indexing into our byte array.
		val := f.rom[(addr&k4K_MASK)+off]
		f.databusVal = val
		return val
	}
	// Outside our range so just return 0.
	return 0
}

func (f *f6SCBankSwitchCart) switchBank(addr uint16) {
	switch addr & 0x1FFF {
	case 0x1FF6:
		f.bank = 0
	case 0x1FF7:
		f.bank = 1
	case 0x1FF8:
		f.bank = 2
	case 0x1FF9:
		f.bank = 3
	}
}

// Write implements the memory.Bank interface for Write.
// For a 8k ROM cart with no onboard RAM this does nothing except trigger
// bank switching. However this has RAM so writing to the right range will
// update that RAM.
func (f *f6SCBankSwitchCart) Write(addr uint16, val uint8) {
	f.databusVal = val
	if (addr & kROM_MASK) == kROM_MASK {
		f.switchBank(addr)
		if addr&0x1FFF < 0x1080 {
			f.ram.Write(addr&k4K_MASK, val)
		}
	}
}

// PowerOn implements the memory.Bank interface for PowerOn.
func (b *f6SCBankSwitchCart) PowerOn() {}

// Parent implements the interface for returning a possible parent memory.Bank.
func (b *f6SCBankSwitchCart) Parent() memory.Bank {
	return b.parent
}

// DatabusVal returns the most recent seen databus item.
func (b *f6SCBankSwitchCart) DatabusVal() uint8 {
	return b.databusVal
}
