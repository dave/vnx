// Command interop reads a VNX document with the Go implementation and writes
// it back out. It is one of six such drivers, one per language, and
// scripts/interop.py runs each of them over each other's output.
//
// It is a driver rather than a subcommand of `vnx` because the reference CLI
// has no rewrite command and does not need one: nothing but this matrix wants
// decode-then-encode as a shell operation.
//
// No fallback anywhere. A refusal, a diagnostic on a document that is meant to
// be clean, or an unwritable path is an exit code and a message on stderr.
package main

import (
	"fmt"
	"os"

	"github.com/dave/vnx/go/pkg/vnx"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: interop <in.vnx> <out.vnx>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "interop: read:", err)
		os.Exit(1)
	}
	doc, diags, err := vnx.Decode(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "interop: refused:", err)
		os.Exit(1)
	}
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "interop: diagnostic %s at %s: %s\n", d.Code, d.Path, d.Message)
	}
	if len(diags) > 0 {
		os.Exit(1)
	}
	out, err := vnx.Encode(doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "interop: encode:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "interop: write:", err)
		os.Exit(1)
	}
}
