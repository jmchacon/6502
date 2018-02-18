all: cov

coverage:
	mkdir -p coverage

coverage/cpu.html: coverage cpu/cpu.go functionality_test.go
	go test -coverprofile=coverage/cpu.out -coverpkg github.com/jmchacon/6502/cpu . -v
	go tool cover -html=coverage/cpu.out -o coverage/cpu.html

cov: coverage/cpu.html

clean:
	rm -rf coverage
