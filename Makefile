all: bench binaries cov

bench: coverage/cpu_bench

binaries: bin bin/convertprg bin/disassembler bin/hand_asm

cov: coverage coverage/cpu.html coverage/c64basic.html coverage/pia6532.html coverage/tia.html

coverage:
	mkdir -p coverage

bin:
	mkdir -p bin

cpu/cpu.go: memory/memory.go irq/irq.go
cpu/cpu_test.go: cpu/cpu.go disassemble/disassemble.go ../../../github.com/davecgh/go-spew/spew/spew.go testdata/6502_functional_test.bin testdata/bcd_test.bin testdata/nestest.nes testdata/nestest.log testdata/dadc.bin testdata/dincsbc.bin testdata/dincsbc-deccmp.bin testdata/droradc.bin testdata/dsbc.bin testdata/dsbc-cmp-flags.bin testdata/sbx.bin testdata/vsbx.bin testdata/undocumented.bin
disassemble/disassemble.go: memory/memory.go
disassembler/disassembler.go: disassemble/disassemble.go c64basic/c64basic.go
c64basic/c64basic.go: cpu/cpu.go
c64basic/c64basic_test.go: c64basic/c64basic.go memory/memory.go cpu/cpu.go testdata/dadc.prg testdata/dincsbc.prg testdata/dincsbc-deccmp.prg testdata/droradc.prg testdata/dsbc.prg testdata/dsbc-cmp-flags.prg testdata/sbx.prg testdata/vsbx.prg
pia6532/pia6532.go: memory/memory.go irq/irq.go io/io.go
pia6532/pia6532_test.go: pia6532/pia6532.go
tia/tia.go: memory/memory.go
tia/tia_test.go: ../../../github.com/davecgh/go-spew/spew/spew.go ../../../github.com/go-test/deep/deep.go ../../../golang.org/x/image/draw/draw.go

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

testdata/undocumented.bin: bin/hand_asm testdata/undocumented.asm
	./bin/hand_asm --offset=49152 testdata/undocumented.asm testdata/undocumented.bin

coverage/cpu_bench: coverage cpu/cpu.go cpu/cpu_test.go
	(cd cpu && go test -v -run='^$$' -bench=.) && touch coverage/cpu_bench

../../../golang.org/x/image/draw/draw.go:
	go get golang.org/x/image/draw

../../../github.com/davecgh/go-spew/spew/spew.go:
	go get github.com/davecgh/go-spew/spew

../../../github.com/go-test/deep/deep.go:
	go get github.com/go-test/deep

coverage/cpu.html: cpu/cpu.go cpu/cpu_test.go
	go test -coverprofile=coverage/cpu.out -timeout=20m ./cpu/... -v
	go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: c64basic/c64basic_test.go
	go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

coverage/pia6532.html: pia6532/pia6532.go pia6532/pia6532_test.go
	go test -coverprofile=coverage/pia6532.out ./pia6532/... -v
	go tool cover -html=coverage/pia6532.out -o coverage/pia6532.html

coverage/tia.html: tia/tia.go tia/tia_test.go
	mkdir -p /tmp/tia_tests
	go test -coverprofile=coverage/tia.out ./tia/... -v -test_image_dir=/tmp/tia_tests
	go tool cover -html=coverage/tia.out -o coverage/tia.html

bin/convertprg: convertprg/convertprg.go
	go build -o bin/convertprg ./convertprg/...

bin/disassembler: disassembler/disassembler.go
	go build -o bin/disassembler ./disassembler/...

bin/hand_asm: hand_asm/hand_asm.go
	go build -o bin/hand_asm ./hand_asm/...

clean:
	rm -rf coverage bin
