// hand_asm takes a filename and produces a bin file
// from parsing the output as a hand assembled file
// of the form:
//
// XXXX OP A1 A2 A3 ....
//
// Where XXXX is the address field and OP is the opcode
// A1,A2,A3 are then optional params as needed.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	offset = flag.Int("offset", 0x0000, "Offset to start writing assembled data. Everything prior is zero filled.")
)

func main() {
	flag.Parse()
	if len(flag.Args()) != 2 {
		log.Fatalf("Invalid command: %s <input> <output>", os.Args[0])
	}
	fn := flag.Args()[0]
	out := flag.Args()[1]

	b, err := exec.Command("/bin/sh", "-c", fmt.Sprintf(`egrep ^[0-9A-F][0-9A-F][0-9A-F][0-9A-F] %s | sed -e 's:\t.*$::' -e 's:(\*).*$::'| cut -c6-`, fn)).Output()
	if err != nil {
		log.Fatalf("Can't open and process %q for input - %v", fn, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	var output []byte
	for i := 0; i < *offset; i++ {
		output = append(output, 0x00)
	}
	l := 0
	for scanner.Scan() {
		t := scanner.Text()
		l++
		// Should be 1-3 tokens
		toks := strings.Split(t, " ")
		if len(toks) > 3 {
			log.Fatalf("Invalid line %d - %q", l, t)
		}
		for _, v := range toks {
			b, err := strconv.ParseUint(v, 16, 8)
			if err != nil {
				log.Fatalf("Can't process input line %d %q - %v", l, t, err)
			}
			output = append(output, byte(b))
		}
	}
	of, err := os.Create(out)
	if err != nil {
		log.Fatalf("Can't open output %q - %v", out, err)
	}
	n, err := of.Write(output)
	if got, want := n, len(output); got != want {
		log.Fatalf("Short write to %q. Got %d and want %d", out, got, want)
	}
	if err != nil {
		log.Fatalf("Got error writing to %q - %v", out, err)
	}
	if err := of.Close(); err != nil {
		log.Fatalf("Error closing %q - %v", out, err)
	}

}
