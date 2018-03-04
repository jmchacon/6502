all: bench binaries cov

bench: coverage/cpu_bench

binaries: bin bin/convertprg bin/disassembler bin/hand_asm

cov: coverage coverage/cpu.html coverage/c64basic.html

coverage:
	mkdir -p coverage

bin:
	mkdir -p bin

cpu/cpu.go: memory/memory.go
cpu/cpu_test.go: cpu/cpu.go disassemble/disassemble.go 
disassemble/disassemble.go: memory/memory.go
disassembler/disassembler.go: disassemble/disassemble.go c64basic/c64basic.go
c64basic/c64basic_test.go: memory/memory.go
c64basic/c64basic.go: cpu/cpu.go

testdata/dadc.bin: bin/convertprg testdata/dadc.prg
	./bin/convertprg --start_pc=2075 testdata/dadc.prg

testdata/dincsbc.bin: bin/convertprg testdata/dincsbc.prg
	./bin/convertprg --start_pc=2075 testdata/dincsbc.prg

testdata/dincsbc-deccmp.bin: bin/convertprg testdata/dincsbc-deccmp.prg
	./bin/convertprg --start_pc=2075 testdata/dincsbc-deccmp.prg

testdata/droradc.bin: bin/convertprg testdata/droradc.prg
	./bin/convertprg --start_pc=2075 testdata/droradc.prg

testdata/dsbc.bin: bin/convertprg testdata/dsbc.prg
	./bin/convertprg --start_pc=2075 testdata/dsbc.prg

testdata/dsbc-cmp-flags.bin: bin/convertprg testdata/dsbc-cmp-flags.prg
	./bin/convertprg --start_pc=2075 testdata/dsbc-cmp-flags.prg

testdata/sbx.bin: bin/convertprg testdata/sbx.prg
	./bin/convertprg --start_pc=2075 testdata/sbx.prg

testdata/vsbx.bin: bin/convertprg testdata/vsbx.prg
	./bin/convertprg --start_pc=2075 testdata/vsbx.prg

testdata/bcd_test.bin: bin/hand_asm testdata/bcd_test.asm
	./bin/hand_asm --offset=49152 testdata/bcd_test.asm testdata/bcd_test.bin

coverage/cpu_bench: coverage cpu/cpu.go cpu/cpu_test.go
	(cd cpu && go test -v -run='^$$' -bench=.) && touch coverage/cpu_bench

coverage/cpu.html: cpu/cpu.go cpu/cpu_test.go testdata/6502_functional_test.bin testdata/bcd_test.bin testdata/nestest.nes testdata/nestest.log testdata/dadc.bin testdata/dincsbc.bin testdata/dincsbc-deccmp.bin testdata/droradc.bin testdata/dsbc.bin testdata/dsbc-cmp-flags.bin testdata/sbx.bin testdata/vsbx.bin
	go test -coverprofile=coverage/cpu.out -timeout=20m ./cpu/... -v
	go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: c64basic/c64basic.go c64basic/c64basic_test.go testdata/dadc.prg testdata/dincsbc.prg testdata/dincsbc-deccmp.prg testdata/droradc.prg testdata/dsbc.prg testdata/dsbc-cmp-flags.prg testdata/sbx.prg testdata/vsbx.prg
	go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

bin/convertprg: convertprg/convertprg.go
	go build -o bin/convertprg ./convertprg/...

bin/disassembler: disassembler/disassembler.go
	go build -o bin/disassembler ./disassembler/...

bin/hand_asm: hand_asm/hand_asm.go
	go build -o bin/hand_asm ./hand_asm/...

clean:
	rm -rf coverage bin
