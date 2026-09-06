// Command vnx reads, checks and formats Viatrail Network eXchange documents.
//
// It wraps pkg/vnx, which is the reference implementation extracted from the
// app's own code — so this tool cannot disagree with what Viatrail writes. A
// reference tool that contradicts the reference implementation is worse than no
// tool at all.
//
// Every command supports --json and exits non-zero on failure. That is not
// decoration: the CLI is the primary interface for AI agents (spec §9), and an
// agent handed "unexpected key" has to go hunting, while one handed a document
// path can fix the thing.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/vnx/go/pkg/vnx"
)

const usage = `vnx — Viatrail Network eXchange tools

  vnx validate [--json] <file>   read a document and report what is wrong with it
  vnx fmt [--write] [--json] <file>
                                 render in canonical form (spec §4.5)
  vnx outline [--json] <file>    the document's shape, without geometry
  vnx convert --to gpx|kml|geojson [--out <file>] <file>
                                 write the document as GPX, KML or GeoJSON

A .vnx file is a plain JSON document; a .vnz file is a zip container.

convert writes the document to stdout unless --out names a file, so
    vnx convert --to gpx trip.vnx > trip.gpx
works. Diagnostics — the items the
chosen format cannot carry — go to stderr, one per line, as
"severity [code] path: message". They are not a failure by themselves: a format
drops what it cannot hold and says so.

Two things DO exit non-zero: a file that does not read at all, and a document
none of which the chosen format can carry. The second writes no file — a .gpx
holding nothing is worse than an error, because it looks like a success. A
document that is genuinely empty is not that case, and converts.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "fmt":
		err = cmdFmt(os.Args[2:])
	case "outline":
		err = cmdOutline(os.Args[2:])
	case "convert":
		err = cmdConvert(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "vnx: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vnx:", err)
		os.Exit(1)
	}
}

type jsonReport struct {
	File        string           `json:"file"`
	OK          bool             `json:"ok"`
	Error       string           `json:"error,omitempty"`
	Diagnostics []jsonDiagnostic `json:"diagnostics"`
}

type jsonDiagnostic struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

func toJSONDiags(diags []vnx.Diagnostic) []jsonDiagnostic {
	out := make([]jsonDiagnostic, 0, len(diags))
	for _, d := range diags {
		out = append(out, jsonDiagnostic{d.Severity.String(), d.Path, d.Message})
	}
	return out
}

// read handles both a bare document and a container, chosen by extension.
func read(path string) (*vnx.Document, []vnx.Diagnostic, error) {
	if strings.EqualFold(filepath.Ext(path), ".vnz") {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, nil, err
		}
		c, diags, err := vnx.DecodeContainer(f, info.Size())
		if err != nil {
			return nil, nil, err
		}
		return c.Document, diags, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return vnx.Decode(data)
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("validate needs exactly one file")
	}
	path := fs.Arg(0)
	doc, diags, err := read(path)

	if *asJSON {
		rep := jsonReport{File: path, OK: err == nil, Diagnostics: toJSONDiags(diags)}
		if err != nil {
			rep.Error = err.Error()
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		if err != nil {
			os.Exit(1)
		}
		// Diagnostics alone are not a failure: the document read, with things
		// reported. Only a refusal is a non-zero exit.
		return nil
	}

	if err != nil {
		return err
	}
	for _, d := range diags {
		fmt.Println(d)
	}
	if len(diags) == 0 {
		fmt.Printf("%s: ok, vnx %s\n", path, doc.Version)
	} else {
		fmt.Printf("%s: read with %d diagnostic(s)\n", path, len(diags))
	}
	return nil
}

func cmdFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	write := fs.Bool("write", false, "rewrite the file in place")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("fmt needs exactly one file")
	}
	path := fs.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := vnx.Canonical(data)
	if err != nil {
		return err
	}
	if *write {
		return os.WriteFile(path, out, 0o644)
	}
	_, err = os.Stdout.Write(out)
	return err
}

// cmdOutline prints structure without geometry. A day's track is tens of
// thousands of pips; an agent that must load them all to rename a route has
// spent its context on coordinates.
func cmdOutline(args []string) error {
	fs := flag.NewFlagSet("outline", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("outline needs exactly one file")
	}
	doc, _, err := read(fs.Arg(0))
	if err != nil {
		return err
	}

	type netOutline struct {
		Name   string `json:"name"`
		Nodes  int    `json:"nodes"`
		Arcs   int    `json:"arcs"`
		Chains int    `json:"chains"`
		Points int    `json:"points"`
		Areas  int    `json:"areas"`
	}
	out := struct {
		Version   string       `json:"vnx"`
		Profile   string       `json:"profile,omitempty"`
		Folders   int          `json:"folders"`
		Networks  []netOutline `json:"networks"`
		Routes    int          `json:"routes"`
		Tracks    int          `json:"tracks"`
		Waypoints int          `json:"waypoints"`
		Areas     int          `json:"areas"`
	}{
		Version: doc.Version, Profile: doc.Profile, Folders: len(doc.Folders),
		Routes: len(doc.Routes), Tracks: len(doc.Tracks),
		Waypoints: len(doc.Waypoints), Areas: len(doc.Areas),
	}
	for _, n := range doc.Networks {
		out.Networks = append(out.Networks, netOutline{
			n.Name, len(n.Nodes), len(n.Arcs), len(n.Chains), len(n.Points), len(n.Areas)})
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	fmt.Printf("vnx %s", out.Version)
	if out.Profile != "" {
		fmt.Printf(" (%s)", out.Profile)
	}
	fmt.Printf("\n  folders   %d\n", out.Folders)
	for _, n := range out.Networks {
		fmt.Printf("  network   %q — %d nodes, %d arcs, %d chains, %d points, %d areas\n",
			n.Name, n.Nodes, n.Arcs, n.Chains, n.Points, n.Areas)
	}
	fmt.Printf("  routes    %d\n  tracks    %d\n  waypoints %d\n  areas     %d\n",
		out.Routes, out.Tracks, out.Waypoints, out.Areas)
	return nil
}
