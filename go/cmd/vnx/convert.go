package main

// `vnx convert` — a VNX file into GPX, KML or GeoJSON, with no database
// anywhere near it (spec 5b §2.4, §4.5, T7).
//
// ⚠ THIS COMMAND IS THE REASON THE WRITERS TAKE A *vnx.Document. A writer that
// walked a database could not live here: cmd/vnx imports pkg/vnx and the three
// writer packages and NOTHING else in this module, which is what lets the
// reference CLI ship in a public repo, cross-compile, and run on a machine with
// no SQLite. purity_test.go is the guard on that, and it names the four
// packages one at a time on purpose.
//
// ⚠ THE DOCUMENT GOES TO STDOUT AND EVERY DIAGNOSTIC GOES TO STDERR. That
// split is what makes `vnx convert --to gpx trip.vnx > trip.gpx` produce a
// usable file. A single diagnostic printed to stdout would land in the middle
// of the user's GPX and break it, silently, in a way only a map application
// would report — which is why convert_test.go never uses CombinedOutput.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dave/vnx/go/pkg/vnx"
	"github.com/dave/vnx/go/pkg/vnx/geojson"
	"github.com/dave/vnx/go/pkg/vnx/gpx"
	"github.com/dave/vnx/go/pkg/vnx/kml"
)

// writeFunc is what each of the three writer packages exports (spec 5b §4.1b).
type writeFunc func(io.Writer, *vnx.Document) (vnx.WriteReport, error)

// convertFormats is what --to accepts, and it is a closed set: an unrecognised
// token is a refusal, never a default (§4.5). A user who asked for a shapefile
// and was handed GPX has been told nothing.
//
// ⚠ "vnx" IS NOT IN HERE, and its absence is a decision. `convert` turns a VNX
// document into something else. Reading a VNX file and writing it back as VNX
// is not a conversion, and this CLI does not offer it.
//
// ⚠ THE TOKENS ARE THE SAME SPELLINGS vnxbridge.Format USES, AND NOTHING
// ENFORCES THAT. This command cannot import vnxbridge — that is the whole
// point of purity_test.go — so the two lists are separate by construction. They
// are three lower-case words with no punctuation and both sides pin them as
// literals in their own tests (pkg/core/vnxbridge/format_test.go's exact-
// spelling loop, and this file's convert_test.go), so a rename cannot happen
// on one side quietly.
var convertFormats = map[string]writeFunc{
	"gpx":     gpx.Write,
	"kml":     kml.Write,
	"geojson": geojson.Write,
}

func convertFormatNames() []string {
	names := make([]string, 0, len(convertFormats))
	for name := range convertFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writerFor resolves --to, or refuses.
//
// The empty string is refused with the same message as a wrong one: a caller
// that never chose a format has not chosen VNX, and picking for them is the
// defaulting this whole seam exists to prevent.
func writerFor(to string) (writeFunc, error) {
	write, ok := convertFormats[to]
	if !ok {
		return nil, fmt.Errorf("--to %q is not a format this tool writes (one of: %s)",
			to, strings.Join(convertFormatNames(), ", "))
	}
	return write, nil
}

func cmdConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	to := fs.String("to", "", "output format: "+strings.Join(convertFormatNames(), ", "))
	out := fs.String("out", "", "file to write; omitted means stdout")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("convert needs exactly one input file")
	}
	// ⚠ THE FORMAT IS RESOLVED BEFORE THE FILE IS READ. A mistyped --to costs
	// a message rather than a parse of a large document, and — the part that
	// matters — it cannot fail after a destination file has been created.
	write, err := writerFor(*to)
	if err != nil {
		return err
	}

	// read() is main.go's, and reusing it is what gives `convert` .vnz
	// container input for free: it branches on the extension and calls
	// vnx.DecodeContainer. A second decode path here would be a second answer
	// to "what can this tool open".
	doc, diags, err := read(fs.Arg(0))
	if err != nil {
		// A file that does not decode is the ONLY refusal on the input side. A
		// file that decodes WITH diagnostics converts, below: a decode
		// diagnostic says something in the input was dropped, not that the
		// input is unreadable, and refusing on one would make this command
		// useless on exactly the third-party files it exists to read.
		return err
	}
	printDiagnostics(os.Stderr, diags)

	report, err := writeConverted(*out, write, doc, *to)
	// ⚠ THE WRITER'S CASUALTIES ARE PRINTED AFTER THE FILE IS IN PLACE OR THE
	// REFUSAL IS DECIDED, never before. A run that failed at the rename and had
	// already listed what it dropped reads as a partial success that never
	// happened. On the empty-export refusal the casualties are not decoration
	// on a success any more — they are the reason — so they are printed there
	// too; on any other failure the report is zero and nothing prints.
	printDiagnostics(os.Stderr, report.Diagnostics)
	if err != nil {
		return err
	}
	return nil
}

// refuseIfNothingEmitted is spec 5b §2.2's empty-export rule, drawn at exactly
// the line vnxbridge.refuseIfNothingEmitted draws it — the app, `vnxdb` and
// this command must not disagree about which files are worth writing, and
// cmd/vnx's own doc comment says why: a reference tool that contradicts the
// reference implementation is worse than no tool at all.
//
// ⚠ THE TWO HALVES ARE DIFFERENT AND MUST STAY APART. An HONESTLY empty
// document still converts: the file says the document holds nothing, which is
// true, in every format. A document that holds things and that this format
// reduced to nothing is refused: that file says the document holds nothing
// while it holds plenty, and it is the one §2.2 calls "worse than an error,
// because it looks like a success".
//
// The emptiness question is [vnx.DocumentIsEmpty]'s, not this file's. It lives
// in pkg/vnx precisely so this command and vnxbridge cannot answer it two ways
// — see its doc comment.
func refuseIfNothingEmitted(report vnx.WriteReport, doc *vnx.Document, to string) error {
	if report.Emitted > 0 || vnx.DocumentIsEmpty(doc) {
		return nil
	}
	return fmt.Errorf("nothing in this document can be written as %s, so no file was written "+
		"(%d item(s) dropped — see the diagnostics above)", to, len(report.Diagnostics))
}

// printDiagnostics writes one diagnostic per line, to stderr.
//
// The shape is `severity [code] path: message`.
//
// ⚠ THE CODE IS PRESENT ON WRITER DIAGNOSTICS AND ABSENT ON DECODE ONES, and
// that is a ruling rather than a gap (pkg/vnx/diagnostic.go): the 60 decode
// sites have never reached a translated surface and were deliberately not given
// codes. Printing a placeholder for them would put a token in the output that
// matches nothing an agent could look up; the column is simply not there.
func printDiagnostics(w io.Writer, diags []vnx.Diagnostic) {
	for _, d := range diags {
		if d.Code == "" {
			fmt.Fprintf(w, "%s %s: %s\n", d.Severity, d.Path, d.Message)
			continue
		}
		fmt.Fprintf(w, "%s %s %s: %s\n", d.Severity, d.Code, d.Path, d.Message)
	}
}

// writeConverted sends the writer's bytes to path, or to stdout when path is "".
//
// ⚠ THE --out PATH IS TEMP-AND-RENAME, and it is not gold-plating. A writer
// can fail half way through a document — a full disk, a closed pipe — and a
// plain os.Create would leave a truncated .gpx at the destination while the
// command exited 1. Exit 1 means NOTHING WAS WRITTEN, including "whatever was
// already at --out is still there"; this is what makes that sentence true.
// vnxbridge.WriteFileAtomically makes the same promise for the app, and cannot
// be called from here: it lives behind a database dependency.
//
// ⚠ STDOUT CANNOT MAKE THAT PROMISE and nothing here pretends it can. Bytes
// handed to a pipe are gone. That is the honest cost of the redirect form, and
// it is why --out exists.
//
// An existing file at --out is overwritten, with no flag and no prompt — the
// same ruling cmd/vnxdb's --out took, for the same reason: a command line is a
// place where the caller named the path deliberately.
func writeConverted(path string, write writeFunc, doc *vnx.Document, to string) (vnx.WriteReport, error) {
	if path == "" {
		// ⚠ STDOUT STILL REFUSES, AND STILL CANNOT UNSEND. The bytes are gone
		// down the pipe by the time the count is known, so this refusal cannot
		// leave the redirect target empty — but the EXIT CODE is what
		// `convert … > out.gpx && next` reads, and it must say the export
		// failed. Half a promise kept is the most this form allows; --out is
		// where the whole one lives.
		report, err := write(os.Stdout, doc)
		if err != nil {
			return report, err
		}
		return report, refuseIfNothingEmitted(report, doc, to)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".vnx-convert-*.tmp")
	if err != nil {
		return vnx.WriteReport{}, err
	}
	tmp := f.Name()
	committed := false
	defer func() {
		f.Close() // a no-op after the Close below; the error there is the one that counts
		if !committed {
			os.Remove(tmp)
		}
	}()

	report, err := write(f, doc)
	if err != nil {
		return vnx.WriteReport{}, err
	}
	// ⚠ BEFORE THE SYNC AND THE RENAME. Checked afterwards, the header-only
	// file this refuses would already be at the destination under its final
	// name, and the error beside it would be an explanation for a file that
	// exists — the accepted-request-is-not-a-delivered-effect shape. The report
	// travels back with the error so the casualties can be printed as the
	// REASON.
	if err := refuseIfNothingEmitted(report, doc, to); err != nil {
		return report, err
	}
	// Sync before the rename: a rename that beats the data to the disk leaves
	// a correctly-named empty file behind a power loss.
	if err := f.Sync(); err != nil {
		return vnx.WriteReport{}, err
	}
	if err := f.Close(); err != nil {
		return vnx.WriteReport{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return vnx.WriteReport{}, err
	}
	committed = true
	return report, nil
}
