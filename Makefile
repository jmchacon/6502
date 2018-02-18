all: cov

coverage:
	mkdir -p coverage

coverage/cpu.html: cpu/cpu.go functionality_test.go
	go test -coverprofile=coverage/cpu.out -coverpkg github.com/jmchacon/6502/cpu . -v
	go tool cover -html=coverage/cpu.out -o coverage/cpu.html

coverage/c64basic.html: c64basic/c64basic.go c64basic/c64basic_test.go
	go test -coverprofile=coverage/c64basic.out ./c64basic/... -v
	go tool cover -html=coverage/c64basic.out -o coverage/c64basic.html

cov: coverage coverage/cpu.html coverage/c64basic.html

clean:
	rm -rf coverage
