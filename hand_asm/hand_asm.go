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
	"io/ioutil"
	"log"
	"os"
	"regexp"
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

	b, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("Can't read input %q: %v", fn, err)
	}

	var output []byte
	for i := 0; i < *offset; i++ {
		output = append(output, 0x00)
	}
	l := 0
	scanner := bufio.NewScanner(bytes.NewReader(b))
	re := regexp.MustCompilePOSIX("^[0-9A-F][0-9A-F][0-9A-F][0-9A-F].*")
	for scanner.Scan() {
		t := scanner.Text()
		l++
		// Only process lines that start with a 4 digit hex number (sans whitespace)
		t = strings.TrimLeft(t, " \t")
		if !re.Match([]byte(t)) {
			continue
		}
		// Some lines don't contain a tab but do contain (*) so replace that with a tab for below.
		t = strings.Replace(t, "(*)", "\t", 1)
		// Trim everything after the first tab
		ri := strings.IndexRune(t, '\t')
		if ri == -1 {
			log.Fatalf("Can't find tab in line %d %q", l, t)
		}
		t = t[:ri]
		// Now trim the first 5 chars off
		t = t[5:]
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
