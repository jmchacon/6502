all: cov binaries

coverage:
	mkdir -p coverage

bin:
	mkdir -p bin

cpu/cpu.go: memory/memory.go
functionality_test.go: cpu/cpu.go disassemble/disassemble.go
disassemble/disassemble.go: memory/memory.go
disassembler/disassembler.go: disassemble/disassemble.go c64basic/c64basic.go
c64basic/c64basic_test.go: testdata/dadc.prg testdata/dincsbc.prg testdata/dincsbc-deccmp.prg testdata/droradc.prg testdata/dsbc.prg testdata/dsbc-cmp-flags.prg testdata/sbx.prg testdata/vsbx.prg

coverage/cpu.html: cpu/cpu.go functionality_test.go
	go test -coverprofile=coverage/cpu.out -coverpkg github.com/jmchacon/6502/cpu . -v
	go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: c64basic/c64basic.go c64basic/c64basic_test.go
	go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

bin/convertprg: convertprg/convertprg.go
	go build -o bin/convertprg ./convertprg/...

bin/disassembler: disassembler/disassembler.go
	go build -o bin/disassembler ./disassembler/...

bin/hand_asm: hand_asm/hand_asm.go
	go build -o bin/hand_asm ./hand_asm/...

cov: coverage coverage/cpu.html coverage/c64basic.html

binaries: bin bin/convertprg bin/disassembler bin/hand_asm

clean:
	rm -rf coverage bin
