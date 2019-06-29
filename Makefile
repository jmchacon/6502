all: bench binaries cov

bench: coverage coverage/cpu_bench coverage/tia_bench

binaries: bin convertprg_bin disassembler_bin hand_asm_bin vcs_bin

cov: coverage coverage/cpu.html coverage/c64basic.html coverage/pia6532.html coverage/tia.html coverage/atari2600.html

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
tia/tia_test.go: ../../../github.com/davecgh/go-spew/spew/spew.go ../../../github.com/go-test/deep/deep.go ../../../golang.org/x/image/draw/draw.go tia/tia.go
atari2600/atari2600_test.go: atari2600/atari2600.go
atari2600/atari2600.go: cpu/cpu.go io/io.go pia6532/pia6532.go tia/tia.go io/io.go
vcs/vcs_main.go: atari2600/atari2600.go tia/tia.go ../../../github.com/veandco/go-sdl2/sdl/sdl.go ../../../github.com/veandco/go-sdl2/img/sdl_image.go ../../../github.com/veandco/go-sdl2/mix/sdl_mixer.go ../../../github.com/veandco/go-sdl2/ttf/sdl_ttf.go ../../../github.com/veandco/go-sdl2/gfx/sdl_gfx.go

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

coverage/cpu_bench: cpu/cpu.go cpu/cpu_test.go
	(cd cpu && CGO_ENABLED=1 CC=gcc go test -v -run='^$$' -bench=.) && touch coverage/cpu_bench

coverage/tia_bench: tia/tia.go tia/tia_test.go
	(cd tia && CGO_ENABLED=1 CC=gcc go test -v -run='^$$' -bench=.) && touch coverage/tia_bench

../../../golang.org/x/image/draw/draw.go:
	go get golang.org/x/image/draw

../../../github.com/davecgh/go-spew/spew/spew.go:
	go get github.com/davecgh/go-spew/spew

../../../github.com/go-test/deep/deep.go:
	go get github.com/go-test/deep

../../../github.com/veandco/go-sdl2/sdl/sdl.go: 
	go get -v github.com/veandco/go-sdl2/sdl

../../../github.com/veandco/go-sdl2/img/sdl_image.go:
	go get -v github.com/veandco/go-sdl2/img

../../../github.com/veandco/go-sdl2/mix/sdl_mixer.go:
	go get -v github.com/veandco/go-sdl2/mix

../../../github.com/veandco/go-sdl2/ttf/sdl_ttf.go:
	go get -v github.com/veandco/go-sdl2/ttf

../../../github.com/veandco/go-sdl2/gfx/sdl_gfx.go:
	go get -v github.com/veandco/go-sdl2/gfx

coverage/cpu.html: cpu/cpu.go cpu/cpu_test.go
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/cpu.out -timeout=30m ./cpu/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: c64basic/c64basic_test.go
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

coverage/pia6532.html: pia6532/pia6532.go pia6532/pia6532_test.go
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/pia6532.out ./pia6532/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/pia6532.out -o coverage/pia6532.html

coverage/tia.html: tia/tia.go tia/tia_test.go
	rm -rf /tmp/tia_tests
	mkdir -p /tmp/tia_tests
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/tia.out ./tia/... -v -test_image_dir=/tmp/tia_tests
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/tia.out -o coverage/tia.html

coverage/atari2600.html: atari2600/atari2600.go atari2600/atari2600_test.go
	rm -rf /tmp/atari2600_tests
	mkdir -p /tmp/atari2600_tests
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/atari2600.out ./atari2600/... -v -test_image_dir=/tmp/atari2600_tests
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/atari2600.out -o coverage/atari2600.html

mpeg: coverage coverage/atari2600.html
	rm -rf /tmp/tia_tests_mp4 /tmp/tia_tests_mp4_gen
	mkdir -p /tmp/tia_tests_mp4 /tmp/tia_tests_mp4_gen
	CGO_ENABLED=1 CC=gcc go test -timeout=20m ./tia/... -v -test_image_dir=/tmp/tia_tests_mp4_gen -test_frame_multiplier=15 -test_image_scaler=5.0
	ffmpeg -r 60 -i /tmp/tia_tests_mp4_gen/TestBackgroundNTSC%06d.png -c:v libx264 -r 60 -pix_fmt yuv420p /tmp/tia_tests_mp4/ntsc.mp4
	ffmpeg -r 60 -i /tmp/tia_tests_mp4_gen/TestBackgroundPAL%06d.png -c:v libx264 -r 60 -pix_fmt yuv420p /tmp/tia_tests_mp4/pal.mp4
	ffmpeg -r 60 -i /tmp/tia_tests_mp4_gen/TestBackgroundSECAM%06d.png -c:v libx264 -r 60 -pix_fmt yuv420p /tmp/tia_tests_mp4/secam.mp4
	ffmpeg -r 60 -i /tmp/atari2600_tests/Combat%06d.png -c:v libx264 -r 60 -pix_fmt yuv420p /tmp/tia_tests_mp4/combat.mp4
	ffmpeg -r 60 -i /tmp/atari2600_tests/SpaceInvaders%06d.png -c:v libx264 -r 60 -pix_fmt yuv420p /tmp/tia_tests_mp4/spcinvad.mp4

convertprg_bin: convertprg/convertprg.go
	CGO_ENABLED=1 CC=gcc go build -o bin/convertprg ./convertprg/...

disassembler_bin: disassembler/disassembler.go
	CGO_ENABLED=1 CC=gcc go build -o bin/disassembler ./disassembler/...

hand_asm_bin: hand_asm/hand_asm.go
	CGO_ENABLED=1 CC=gcc go build -o bin/hand_asm ./hand_asm/...

vcs_bin: vcs/vcs_main.go
	CGO_ENABLED=1 CC=gcc go build -o bin/vcs ./vcs/...

clean:
	rm -rf coverage bin
