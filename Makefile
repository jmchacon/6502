all: bench binaries coverage

bench: coverage/cpu_bench coverage/tia_bench

binaries: bin convertprg_bin disassembler_bin hand_asm_bin vcs_bin

cov: coverage coverage/cpu.html coverage/c64basic.html coverage/pia6532.html coverage/tia.html coverage/atari2600.html

coverage:
	mkdir -p coverage

bin:
	mkdir -p bin

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

.PHONY: coverage/cpu_bench coverage/tia_bench coverage/cpu.html coverage/c64basic.html coverage/pia6532.html coverage/tia.html coverage/atari2600.html
coverage/cpu_bench: coverage
	(cd cpu && CGO_ENABLED=1 CC=gcc go test -v -run='^$$' -bench=.) && touch coverage/cpu_bench

coverage/tia_bench: coverage
	(cd tia && CGO_ENABLED=1 CC=gcc go test -v -run='^$$' -bench=.) && touch coverage/tia_bench

coverage/cpu.html: coverage testdata/dadc.bin testdata/dincsbc.bin testdata/dincsbc-deccmp.bin testdata/droradc.bin testdata/dsbc.bin testdata/dsbc-cmp-flags.bin testdata/sbx.bin testdata/vsbx.bin testdata/bcd_test.bin testdata/undocumented.bin
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/cpu.out -timeout=30m ./cpu/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: coverage
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

coverage/pia6532.html: coverage
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/pia6532.out ./pia6532/... -v
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/pia6532.out -o coverage/pia6532.html

coverage/tia.html: coverage
	rm -rf /tmp/tia_tests
	mkdir -p /tmp/tia_tests
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/tia.out ./tia/... -v -test_image_dir=/tmp/tia_tests
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/tia.out -o coverage/tia.html

coverage/atari2600.html: coverage
	rm -rf /tmp/atari2600_tests
	mkdir -p /tmp/atari2600_tests
	CGO_ENABLED=1 CC=gcc go test -coverprofile=coverage/atari2600.out ./atari2600/... -v -test_image_dir=/tmp/atari2600_tests
	CGO_ENABLED=1 CC=gcc go tool cover -html=coverage/atari2600.out -o coverage/atari2600.html

.PHONY: deps
deps:
	go get golang.org/x/image/draw
	go get github.com/davecgh/go-spew/spew
	go get github.com/go-test/deep
	go get -v github.com/veandco/go-sdl2/sdl
	go get -v github.com/veandco/go-sdl2/img
	go get -v github.com/veandco/go-sdl2/mix
	go get -v github.com/veandco/go-sdl2/ttf
	go get -v github.com/veandco/go-sdl2/gfx

mpeg: coverage/atari2600.html
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
