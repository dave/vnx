package vnx

import (
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// §6.3: a caller must be able to tell "read cleanly" from "read with N things
// dropped". A reader that returns a document and nothing else has discarded the
// only evidence anything was wrong.
func TestCleanDocumentHasNoDiagnostics(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0","created":"2026-08-31T06:45:12Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean document produced %v", diags)
	}
}

// §6.2, and the precedent is importer/gpx.go:210-214 — a coordinate that will
// not parse increments a count and continues. The bad thing is dropped, the
// count is surfaced, one bad character does not kill the file. Applied at KEY
// granularity here: an unknown key is not an unusable value, so the item lives.
func TestUnknownKeyIsDiagnosedAndSkipped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.10.0","nmae":"typo"}`))
	if err != nil {
		t.Fatalf("an unknown key must not refuse the document: %v", err)
	}
	if doc == nil || doc.Version != "0.10.0" {
		t.Fatalf("the rest of the document must survive, got %+v", doc)
	}
	if len(diags) != 1 {
		t.Fatalf("want exactly one diagnostic, got %v", diags)
	}
	if diags[0].Path != "/nmae" {
		t.Fatalf("path = %q, want /nmae — a path is what makes it actionable", diags[0].Path)
	}
	if diags[0].Severity != Warning {
		t.Fatalf("severity = %v, want Warning", diags[0].Severity)
	}
}

func TestUnknownKeysEachGetTheirOwnDiagnostic(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0","one":1,"two":2,"three":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 3 {
		t.Fatalf("want three diagnostics, got %d: %v", len(diags), diags)
	}
	seen := map[string]bool{}
	for _, d := range diags {
		seen[d.Path] = true
	}
	for _, want := range []string{"/one", "/two", "/three"} {
		if !seen[want] {
			t.Errorf("no diagnostic for %s", want)
		}
	}
}

// A refusal returns an error AND no document. A partial document from a refused
// read is worse than none: a caller that checks the document before the error
// would act on it.
func TestRefusalReturnsNoPartialDocument(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"9.9"}`))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v", err)
	}
	if doc != nil {
		t.Fatalf("refused read returned a document: %+v", doc)
	}
	if diags != nil {
		t.Fatalf("refused read returned diagnostics: %v", diags)
	}
}

// The format's collections and an application key must NOT be reported as
// unknown — that would train a caller to ignore diagnostics, which is the one
// habit this channel cannot survive.
func TestKnownCollectionsAreNotReportedAsUnknown(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0","folders":[],"networks":[],"plans":[],
	  "tracks":[],"waypoints":[],"areas":[],"app.viatrail.rulesets":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("known collections must not be diagnosed as unknown, got %v", diags)
	}
}

func TestDiagnosticStringIsReadable(t *testing.T) {
	got := Diagnostic{Severity: Warning, Path: "/networks/0/arcs/arc-1", Message: "dropped"}.String()
	if got != "warning /networks/0/arcs/arc-1: dropped" {
		t.Fatalf("got %q", got)
	}
}

// TestDecodeDiagnosticsCarryNoCode pins the ruling in [Diagnostic]'s doc: Code
// is OPTIONAL and the decode path does not set it. Written as a test rather
// than left to the comment because "empty when no stable code is assigned" is
// the kind of statement that stays quotable while somebody half-fills the
// field — and a half-populated Code is worse than an empty one, since a
// consumer cannot tell "this diagnostic has no code" from "codes are not
// populated yet".
func TestDecodeDiagnosticsCarryACodeOnlyWhereTheCorpusPinsOne(t *testing.T) {
	// ⚠ THIS TEST WAS "decode diagnostics carry NO code", AND THE RULING
	// NARROWED RATHER THAN REVERSED. Spec 5b's reason still stands: the sixty
	// decode sites in this package report malformed input to `vnx validate`
	// and to logs, and minting sixty codes to fill a field would be sixty wire
	// commitments nobody asked for.
	//
	// What changed on 2026-09-09 is that a conformance corpus case cannot pin
	// a diagnostic by its ENGLISH — five implementations matching substrings
	// of free prose is the defect the refusal wordings already had. So spec
	// §6.3 requires a code for exactly the diagnostics a corpus case pins,
	// which is decodeCodes and nothing else.
	//
	// The guard therefore still guards the thing worth guarding: code CREEP.
	// A decode site that mints a code outside that list reddens here.
	known := map[string]bool{}
	for _, c := range decodeCodes {
		known[c] = true
	}

	const src = `{"vnx":"0.10.0",` +
		`"folders":[{"id":"f1","name":"F"},{"name":"no id"}],` +
		`"nonsense":[1,2,3]}`
	_, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(diags) < 2 {
		t.Fatalf("expected several decode diagnostics, got %d (%v) — this test cannot see "+
			"the property it guards if nothing was diagnosed", len(diags), diags)
	}
	for _, d := range diags {
		if d.Code != "" && !known[d.Code] {
			t.Errorf("decode diagnostic %v carries Code %q, which is not in decodeCodes; "+
				"a decode site gets a code only when a corpus case pins it", d, d.Code)
		}
	}
}

// TestEveryDecodeCodeIsPinnedByTheCorpus is the other direction, and it is the
// half that stops the list growing for its own sake: a code declared here that
// no corpus case names is a wire commitment with nothing asking for it.
func TestEveryDecodeCodeIsPinnedByTheCorpus(t *testing.T) {
	pinned := map[string]bool{}
	for _, c := range loadCorpus(t) {
		for _, d := range c.expect.Diagnostics {
			if d.Code != "" {
				pinned[d.Code] = true
			}
		}
	}
	for _, c := range decodeCodes {
		if !pinned[c] {
			t.Errorf("decode code %q is declared but no corpus case pins it; "+
				"spec §6.3 gives a code to the diagnostics the corpus pins, and only those", c)
		}
	}
}

// TestWriterCodeRegistryRegistersAndCopies exercises the registry mechanism
// while it still has no entries. Without this the registry is a function
// nothing calls and a slice nothing fills, which is indistinguishable from a
// registry that does not work — and the bridge's mapping guard, which reads
// WriterCodes, would then be green because the list is broken rather than
// because it is clean.
//
// ⚠ The registration here CANNOT reach the bridge's guard: that runs in a
// different test binary, so this package's test-only entry is invisible to it.
func TestWriterCodeRegistryRegistersAndCopies(t *testing.T) {
	before := len(writerCodes)
	defer func() { writerCodes = writerCodes[:before] }()

	const probe = "test.registry-liveness"
	if got := writerCode(probe); got != probe {
		t.Fatalf("writerCode returned %q, want %q — a Code must be usable as the value it declares", got, probe)
	}
	codes := WriterCodes()
	if len(codes) != before+1 || codes[before] != probe {
		t.Fatalf("WriterCodes() = %v, which does not end with the code just registered", codes)
	}
	// The copy: appending to the returned slice must not register anything.
	codes = append(codes[:0:0], codes...)
	codes[before] = "test.mutated"
	if writerCodes[before] != probe {
		t.Errorf("WriterCodes() handed out the live slice — a caller mutating it would be " +
			"editing the registry the mapping guard reads")
	}
}

// A segment and a plan's arc have fixed key sets too (SPEC §3.10), and an
// unknown key on either is reported rather than dropped in silence.
func TestUnknownKeysOnASegmentAndAPlanArcAreReported(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0",
	  "tracks":[{"segments":[{"pips":[[1,2],[1,3]],"zz":1}]}],
	  "plans":[{"nodes":{"n1":[1,2],"n2":[1,3]},"arcs":{"a":{"pips":[],"from":"n1","to":"n2","zz":1}},
	    "walk":[{"arc":"a","from":"n1","to":"n2"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range diags {
		got[d.Path] = d.Code
	}
	want := map[string]string{
		"/tracks/0/segments/0/zz": CodeUnknownKey,
		"/plans/0/arcs/a/zz":      CodeUnknownKey,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics %v, want %v", got, want)
	}
}

// TestDecodeCodesListsEveryDecodeCodeAndNothingElse reads the declarations
// themselves — every `= decodeCode("…")` in diagnostic.go — and compares them
// with DecodeCodes(), in order. Enumerating from the source text, not from the
// slice DecodeCodes copies, is what makes a registry that lost or gained a
// code go red.
func TestDecodeCodesListsEveryDecodeCodeAndNothingElse(t *testing.T) {
	src, err := os.ReadFile("diagnostic.go")
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, m := range regexp.MustCompile(`= decodeCode\("([^"]+)"\)`).FindAllStringSubmatch(string(src), -1) {
		declared = append(declared, m[1])
	}
	if len(declared) == 0 {
		t.Fatal("found no decodeCode declarations in diagnostic.go; the scan is not reading them")
	}
	got := DecodeCodes()
	if strings.Join(got, ",") != strings.Join(declared, ",") {
		t.Fatalf("DecodeCodes() = %v\nthe declarations are %v", got, declared)
	}
	// The copy: changing the returned slice must not change the registry.
	got[0] = "test.mutated"
	if DecodeCodes()[0] != declared[0] {
		t.Error("DecodeCodes() handed out the live slice — a caller mutating it would be " +
			"editing the registry the bridge's mapping guard reads")
	}
}
