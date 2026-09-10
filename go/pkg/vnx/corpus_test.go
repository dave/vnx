package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The conformance corpus (spec §8).
//
// Its whole purpose is stopping independent implementations from quietly
// disagreeing, and it cannot do that retroactively — so it exists before the
// second library does, not after. Every implementation runs these same files
// and the same expectations; a construct no implementation can round-trip does
// not ship.
//
// The expectations live in a `.expect.json` sidecar next to each file rather
// than in this Go test, for the same reason: a Swift or Python implementation
// must be able to run the corpus without reading Go. The sidecar's `note` is
// part of the corpus too — it says why the case exists, which is the thing that
// rots first and is worth the most to whoever reads the file in a year.
const corpusDir = "../../../corpus"

// expectedDiagnostic is one entry of a sidecar's `diagnostics` list. Path is
// compared exactly and Message as a substring, because the exact wording of a
// diagnostic is a reader's business and naming its cause is not.
type expectedDiagnostic struct {
	Path string `json:"path"`
	// Code is what is COMPARED. Spec §6.3 requires a code on every diagnostic
	// a corpus case pins, for the reason the refusal wordings already showed:
	// five implementations matching substrings of free English is a corpus
	// comparing prose, and prose is a reader's own business.
	Code string `json:"code"`
	// Message is advisory and is NOT compared. It is here so a person reading
	// the sidecar can see what the case is about without running anything.
	//
	// ⚠ IT IS NAMED FOR WHAT IT IS. A field called `message` that nothing
	// compares reads as a claim and is not one — half-coupling is worse than
	// none, because it looks like it is there.
	MessageAdvisory string `json:"message_advisory"`
}

// expectedPosition pins one position in the document, addressed by a JSON
// Pointer in the wire's own key names — "/waypoints/0/at",
// "/networks/0/arcs/arc-1/pips/3", "/tracks/0/segments/1/pips/0". The pointer
// is resolved against the DECODED model, never against the raw JSON: resolving
// it against the file would compare the file to itself and could not see a
// decoder that swapped two channels.
type expectedPosition struct {
	At  string  `json:"at"`
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

type expectation struct {
	Kind string `json:"kind"`
	Note string `json:"note"`

	// Counts is exact, and it is what stops an accept file passing vacuously:
	// a decoder that returned an empty document for everything would otherwise
	// satisfy "no error, no diagnostics" on every file here.
	Counts map[string]int `json:"counts"`

	// Positions pins which number went into which channel. Nothing structural
	// can catch a lon/lat transposition — both orderings parse — so this is the
	// only defence, and the values are JSON numbers in the sidecar so the
	// comparison is between doubles rather than between formatted text.
	//
	// ⚠ IT ADDRESSES EVERY POSITION, NOT JUST WAYPOINTS. It named waypoints
	// only until 2026-09-09, which meant a reader that transposed lon/lat on
	// every arc pip, node, area vertex and track pip passed the entire corpus —
	// including accept/lonlat-transposition.vnx, the file that exists for this
	// bug. A fixture containing a construct is not a test of it; only a named
	// expectation is.
	Positions []expectedPosition `json:"positions"`

	// Tracks carries derived moving time. A null means UNKNOWN, which is not
	// zero: a reader that answers 0 for a track with no pip times has answered
	// a question it cannot answer.
	Tracks []struct {
		Name         string   `json:"name"`
		MovingTimeMS *float64 `json:"moving_time_ms"`
	} `json:"tracks"`

	Attachments *[]string `json:"attachments"`

	// Root names the archive's root document. It appears on a write/ container
	// case, whose output is an archive rather than a document.
	Root string `json:"root"`

	// Refusal is a substring of the expected error. Substring rather than
	// equality because the exact wording is a reader's business; what the
	// corpus pins is that the refusal names its cause.
	Refusal string `json:"refusal"`

	Diagnostics []expectedDiagnostic `json:"diagnostics"`

	CannotDetect string `json:"cannot_detect"`
}

func TestConformanceCorpus(t *testing.T) {
	cases := loadCorpus(t)
	// A corpus that shrank because a file was renamed and its sidecar was not
	// would still pass every case it found. Pin the shape.
	byKind := map[string]int{}
	for _, c := range cases {
		byKind[c.expect.Kind]++
	}
	for _, kind := range []string{"accept", "reject", "diagnose"} {
		if byKind[kind] == 0 {
			t.Fatalf("the corpus has no %s cases at all — it is not being loaded", kind)
		}
	}
	t.Logf("corpus: %d accept, %d reject, %d diagnose", byKind["accept"], byKind["reject"], byKind["diagnose"])

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			switch c.expect.Kind {
			case "accept":
				runAccept(t, c)
			case "reject":
				runReject(t, c)
			case "diagnose":
				runDiagnose(t, c)
			default:
				t.Fatalf("sidecar declares kind %q, which is not accept, reject or diagnose", c.expect.Kind)
			}
		})
	}
}

func runAccept(t *testing.T, c corpusCase) {
	t.Helper()
	doc, diags, attachments, err := c.decode()
	if err != nil {
		t.Fatalf("must accept, but was refused: %v", err)
	}
	for _, d := range diags {
		t.Errorf("must accept cleanly, but reported: %s", d)
	}
	checkCounts(t, doc, c.expect.Counts)
	checkPositions(t, doc, c.expect)
	checkTracks(t, doc, c.expect)
	if want := c.expect.Attachments; want != nil {
		compareAttachments(t, attachments, *want, "the attachments the reader listed")
	}
	// The accept tier's check (spec §8.1): DECODE the file, ENCODE the model,
	// and require the two JSON TREES equal.
	//
	// ⚠ IT IS NOT A COMPARISON OF ONE MODEL AGAINST ANOTHER. A reader that
	// discarded every data record, every ruleset, every style and every `notes`
	// produces a model that compares equal to itself and passes such a tier
	// green — that is the whole extensibility mechanism, all styling and all
	// user prose. Comparing against the tree parsed from the ORIGINAL FILE is
	// what proves a reader kept what it read, and accept_tier_test.go freezes
	// the proof.
	//
	// ⚠ AND IT IS NOT A COMPARISON OF BYTES. Key order, number spelling and
	// whitespace are the writer's own business, so the two sides are parsed and
	// compared as trees, with the normalisations §8.1 permits.
	if c.isContainer() {
		return
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	diff, err := compareDocuments(got, c.data, treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("encoding the decoded document did not reproduce the file: %s\n"+
			"Either the reader dropped something it read, or the writer invented something", diff)
	}
}

// compareAttachments compares two attachment lists as a COLLECTION and not as a
// sequence.
//
// ⚠ ARCHIVE ORDER IS NOT A RULE, AND AN ORDERED COMPARISON HERE MAKES SIX
// LANGUAGES AGREE ON ONE ANYWAY. §7.1 constrains five things about an archive —
// the root document named `doc.vnx` and FIRST, the UTF-8 filename flag, unique
// names, safe paths, stored or deflated — and then says every other field is the
// writer's own business. Where the attachments fall among themselves is one of
// those other fields, and the sidecar's `attachments` list is read by every
// implementation, so a runner that compared it in order would export its own
// reader's iteration order as a conformance requirement.
//
// ⚠ ORDER IS A RULE IN EXACTLY ONE PLACE, AND IT IS NOT THIS ONE. §7 selects the
// root document as "the first entry with a `.vnx` extension in archive order",
// so a case pinning that SELECTION is asserting something real —
// TestContainerFallsBackToFirstVnxInArchiveOrder and
// TestSubdirectoryDocVnxDoesNotBeatARootEntryThatComesFirst. Neither compares an
// attachment list, and neither may be flattened into this helper.
//
// It counts rather than comparing sets, so a duplicated name cannot fill the
// hole left by a missing one.
func compareAttachments(t *testing.T, got, want []string, what string) {
	t.Helper()
	tally := map[string]int{}
	for _, n := range got {
		tally[n]++
	}
	for _, n := range want {
		tally[n]--
	}
	var missing, extra []string
	for n, count := range tally {
		for ; count < 0; count++ {
			missing = append(missing, n)
		}
		for ; count > 0; count-- {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	t.Errorf("%s do not match the sidecar (compared as a collection; archive order is not a rule)\n"+
		"  got:     %v\n  want:    %v\n  missing: %v\n  unexpected: %v", what, got, want, missing, extra)
}

func runReject(t *testing.T, c corpusCase) {
	t.Helper()
	_, _, _, err := c.decode()
	if err == nil {
		t.Fatal("must be refused, but was accepted")
	}
	if c.expect.Refusal == "" {
		t.Fatal("sidecar declares kind reject but names no expected refusal")
	}
	if !strings.Contains(err.Error(), c.expect.Refusal) {
		t.Errorf("refusal does not name its cause:\n got: %v\nwant substring: %q", err, c.expect.Refusal)
	}
}

func runDiagnose(t *testing.T, c corpusCase) {
	t.Helper()
	doc, diags, _, err := c.decode()
	if err != nil {
		t.Fatalf("must be READ with diagnostics, not refused: %v", err)
	}
	checkCounts(t, doc, c.expect.Counts)

	// Diagnostics are compared as a SET, not by index — spec §6.3 says emission
	// order is not significant. An index comparison would make order a
	// conformance requirement that no section of the spec states, and a reader
	// that resolves cross-references in a second pass emits its link
	// diagnostics after its key diagnostics while being entirely correct. This
	// is not laxness: it is the rule. Do not "fix" it back to diags[i].
	//
	// The limitation is accepted and recorded: a set comparison cannot catch a
	// reader that emits in an absurd order. There is no rule to catch it
	// against, which is the point.
	// ⚠ SPEC §6.3's INVARIANT, checked over the reader's OWN output rather than
	// against the sidecar. A path and a code are the only two fields a runner
	// may compare, so two reports sharing both are two a runner cannot tell
	// apart — and the set matcher below would then pass a reader that found one
	// of the two faults twice.
	//
	// Go emitted exactly that: with both terminals of one arc drifted, §3.3.2's
	// two diagnostics landed on one path and were separable only by their
	// English, which §6.3 says MUST NOT be compared.
	seen := map[string]bool{}
	for _, d := range diags {
		key := d.Path + "\x00" + d.Code
		if seen[key] {
			t.Errorf("two diagnostics share path %q and code %q; §6.3 requires the pair to "+
				"identify a report, and these two are indistinguishable to any runner", d.Path, d.Code)
		}
		seen[key] = true
	}

	want := c.expect.Diagnostics
	unmatched, extra := matchDiagnostics(diags, want)
	if len(unmatched) > 0 || len(extra) > 0 {
		t.Errorf("diagnostics do not match the sidecar (compared as a set, %d expected, %d reported):", len(want), len(diags))
		for _, u := range unmatched {
			t.Errorf("  expected but not reported: %s", u)
		}
		for _, e := range extra {
			t.Errorf("  reported but not expected: %s", e)
		}
	}
	// A documented cannot-detect expects NO diagnostic. If a reader ever grows
	// the ability to spot it, this goes red — which is the point: the
	// limitation gets revisited rather than quietly outliving its cause.
	if c.expect.CannotDetect != "" && len(want) != 0 {
		t.Errorf("a cannot-detect case cannot also expect a diagnostic")
	}
}

func checkCounts(t *testing.T, doc *Document, want map[string]int) {
	t.Helper()
	got := map[string]int{
		"folders":   len(doc.Folders),
		"networks":  len(doc.Networks),
		"routes":    len(doc.Routes),
		"tracks":    len(doc.Tracks),
		"waypoints": len(doc.Waypoints),
		"areas":     len(doc.Areas),
		"rulesets":  len(doc.Rulesets),
	}
	for k, w := range want {
		g, known := got[k]
		if !known {
			t.Fatalf("sidecar counts %q, which is not a collection", k)
		}
		if g != w {
			t.Errorf("%s: got %d, want %d", k, g, w)
		}
	}
	// A sidecar MUST name every collection its document carries. An omitted
	// collection used to mean "unchecked", which let a decoder invent or drop
	// items in it with nothing to say so — and the one file that had a gap was
	// the worked example, which is the file every implementer reads first.
	for k, g := range got {
		if g == 0 {
			continue
		}
		if _, named := want[k]; !named {
			t.Errorf("the document carries %d %s and the sidecar does not name that collection; "+
				"an unnamed collection is unchecked, not empty", g, k)
		}
	}
}

func checkPositions(t *testing.T, doc *Document, e expectation) {
	t.Helper()
	if len(e.Positions) == 0 {
		return
	}
	have := positionsByPointer(doc)
	for _, want := range e.Positions {
		got, ok := have[want.At]
		if !ok {
			t.Errorf("no position at %q; the document holds %d addressable positions", want.At, len(have))
			continue
		}
		// ⚠ BIT PATTERNS, NOT VALUES, AND FOR TWO REASONS.
		//
		// A value comparison cannot see a one-ulp move between the parse and
		// the model, and when it does fail it cannot SHOW the two numbers:
		// they print identically at default precision, so the failure reads as
		// a broken test rather than a broken decoder. Both values and both
		// patterns are printed below for the same reason.
		//
		// ⚠ TWO THINGS IT CANNOT SEE, BOTH MEASURED RATHER THAN REASONED,
		// because each is easy to assume it covers.
		//
		// A SIGN-OF-ZERO FLIP. `-0.0 == 0.0`, so a value comparison cannot tell
		// the two apart and a bit comparison can — but of the 19 positions
		// pinned across accept/ and diagnose/, not one carries a zero longitude
		// or latitude, so there is nothing here to bite on. A reader that
		// flipped every decoded 0.0 to -0.0 passes this GREEN. Negative zero has
		// exactly one instrument in the corpus, corpus/numbers/'s bit-pattern
		// comparison, and this is not a second one. Do not add a fixture to give
		// it a reach: a fixture placed to make a test reach is the wrong shape.
		//
		// A PARSER ONE ULP OUT. The sidecar's expected value is read by the
		// SAME parser, so such a parser is wrong on both sides and the two
		// agree. TestFullPrecisionSurvivesRoundTrip is the check for that.
		//
		// What it does see is anything that moves a value BETWEEN the parse and
		// the model: a reader shifted one ulp reddens 8 of these cases.
		if math.Float64bits(got.Lon) != math.Float64bits(want.Lon) ||
			math.Float64bits(got.Lat) != math.Float64bits(want.Lat) {
			t.Errorf("%s:\n  got  lon=%v lat=%v  (%016x %016x)\n  want lon=%v lat=%v  (%016x %016x)",
				want.At,
				got.Lon, got.Lat, math.Float64bits(got.Lon), math.Float64bits(got.Lat),
				want.Lon, want.Lat, math.Float64bits(want.Lon), math.Float64bits(want.Lat))
		}
	}
}

// positionsByPointer walks the decoded document and returns every position in
// it, keyed by a JSON Pointer in the wire's own key names.
//
// It is a producer rather than a resolver on purpose: one traversal answers
// every pointer, so a case can pin a pip deep inside a network without the
// harness growing a pointer parser. An implementation in another language
// writes the same walk over its own model.
func positionsByPointer(doc *Document) map[string]Position {
	out := map[string]Position{}
	pips := func(prefix string, ps []Position) {
		for i, p := range ps {
			out[fmt.Sprintf("%s/%d", prefix, i)] = p
		}
	}
	for i, w := range doc.Waypoints {
		out[fmt.Sprintf("/waypoints/%d/at", i)] = w.At
	}
	for i, a := range doc.Areas {
		pips(fmt.Sprintf("/areas/%d/polygon", i), a.Polygon)
	}
	for i, n := range doc.Networks {
		for key, node := range n.Nodes {
			out[fmt.Sprintf("/networks/%d/nodes/%s", i, key)] = node
		}
		for key, arc := range n.Arcs {
			pips(fmt.Sprintf("/networks/%d/arcs/%s/pips", i, key), arc.Pips)
		}
		for j, c := range n.Chains {
			for k, l := range c.Links {
				// Only an INLINE endpoint is a position. A node key is a
				// reference and is addressed at the node.
				if l.From.At != nil {
					out[fmt.Sprintf("/networks/%d/chains/%d/links/%d/from", i, j, k)] = *l.From.At
				}
				if l.To.At != nil {
					out[fmt.Sprintf("/networks/%d/chains/%d/links/%d/to", i, j, k)] = *l.To.At
				}
			}
		}
		for j, w := range n.Points {
			out[fmt.Sprintf("/networks/%d/points/%d/at", i, j)] = w.At
		}
		for j, a := range n.Areas {
			pips(fmt.Sprintf("/networks/%d/areas/%d/polygon", i, j), a.Polygon)
		}
	}
	for i, r := range doc.Routes {
		for j, a := range r.Arcs {
			pips(fmt.Sprintf("/routes/%d/arcs/%d/pips", i, j), a.Pips)
		}
	}
	for i, tr := range doc.Tracks {
		for j, s := range tr.Segments {
			pips(fmt.Sprintf("/tracks/%d/segments/%d/pips", i, j), s.Pips)
		}
	}
	return out
}

func checkTracks(t *testing.T, doc *Document, e expectation) {
	t.Helper()
	for _, want := range e.Tracks {
		var found *Track
		for i := range doc.Tracks {
			if doc.Tracks[i].Name == want.Name {
				found = &doc.Tracks[i]
				break
			}
		}
		if found == nil {
			t.Errorf("no track named %q", want.Name)
			continue
		}
		got, known := found.MovingTime()
		if want.MovingTimeMS == nil {
			if known {
				t.Errorf("%s: moving time reported as %v, but it is UNKNOWN — and unknown is not a number", want.Name, got)
			}
			continue
		}
		if !known {
			t.Errorf("%s: moving time reported as unknown, want %v ms", want.Name, *want.MovingTimeMS)
			continue
		}
		if float64(got.Milliseconds()) != *want.MovingTimeMS {
			t.Errorf("%s: moving time = %d ms, want %v ms", want.Name, got.Milliseconds(), *want.MovingTimeMS)
		}
	}
}

type corpusCase struct {
	name   string
	path   string
	data   []byte
	expect expectation
}

func (c corpusCase) isContainer() bool { return strings.HasSuffix(c.path, ".vnz") }

func (c corpusCase) decode() (*Document, []Diagnostic, []string, error) {
	if c.isContainer() {
		ct, diags, err := DecodeContainer(bytes.NewReader(c.data), int64(len(c.data)))
		if err != nil {
			return nil, nil, nil, err
		}
		return ct.Document, diags, ct.Attachments, nil
	}
	doc, diags, err := Decode(c.data)
	return doc, diags, nil, err
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	var cases []corpusCase
	for _, kind := range []string{"accept", "reject", "diagnose"} {
		dir := filepath.Join(corpusDir, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".vnx") && !strings.HasSuffix(name, ".vnz") {
				continue
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// A missing sidecar fails rather than skipping. A corpus that
			// silently ignores files it cannot explain is worth nothing: the
			// file everyone assumes is covered is exactly the one that is not.
			sidecar := strings.TrimSuffix(strings.TrimSuffix(path, ".vnx"), ".vnz") + ".expect.json"
			raw, err := os.ReadFile(sidecar)
			if err != nil {
				t.Fatalf("%s has no expectation beside it: %v", path, err)
			}
			var exp expectation
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&exp); err != nil {
				t.Fatalf("%s: %v", sidecar, err)
			}
			if exp.Kind != kind {
				t.Fatalf("%s declares kind %q but sits in %s/", sidecar, exp.Kind, kind)
			}
			if strings.TrimSpace(exp.Note) == "" {
				t.Fatalf("%s has no note; a corpus case that does not say why it exists cannot be maintained", sidecar)
			}
			cases = append(cases, corpusCase{
				name:   fmt.Sprintf("%s/%s", kind, name),
				path:   path,
				data:   data,
				expect: exp,
			})
		}
	}
	return cases
}

// The documented cannot-detect (spec §6.4), asserted directly rather than only
// by the absence of a diagnostic — absence of evidence is a weak test, and this
// one would keep passing if the file stopped holding a duplicate at all.
//
// What actually happens is last-wins in the JSON parser, before any VNX code
// runs: the first node-1 is gone, so the map holds 2 nodes where the file names
// 3. If a reader ever gains the ability to see this, both this test and the
// corpus file go red together and the limitation gets revisited.
func TestDuplicateObjectKeyIsSilentlyLastWins(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "diagnose", "duplicate-object-key.vnx"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte(`"n1"`)) < 3 {
		t.Fatal("the fixture no longer holds a duplicate key; this test would pass vacuously")
	}
	doc, diags, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("a duplicate key is a documented cannot-detect, yet something reported it: %v", diags)
	}
	nodes := doc.Networks[0].Nodes
	if len(nodes) != 2 {
		t.Fatalf("expected the duplicate to have collapsed to 2 nodes, got %d", len(nodes))
	}
	// And the survivor is the LAST one, which is the half of the behaviour that
	// makes the loss invisible: the document stays internally consistent.
	if got := nodes["n1"].Lon; got != -3.60 {
		t.Fatalf("n1 = %v, want the last spelling (-3.6); duplicate resolution is not last-wins here", got)
	}
}

// matchDiagnostics pairs each expected diagnostic with one distinct reported
// diagnostic, and returns what could not be paired from each side. Matching is
// on an exact Path and an exact Code; the English is not compared at all.
//
// It is a SET comparison because spec §6.3 says emission order is not
// significant. TestMatchDiagnostics is what keeps it from degenerating into
// "anything passes" — order-independent is not the same as undemanding, and a
// set matcher that quietly stopped discriminating would take the whole
// diagnose tier of the corpus with it.
func matchDiagnostics(got []Diagnostic, want []expectedDiagnostic) (unmatched, extra []string) {
	paired := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, d := range got {
			if !paired[i] && d.Path == w.Path && d.Code == w.Code {
				paired[i] = true
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, fmt.Sprintf("%s [%s]", w.Path, w.Code))
		}
	}
	for i, d := range got {
		if !paired[i] {
			extra = append(extra, d.String())
		}
	}
	return unmatched, extra
}

// TestMatchDiagnostics freezes the proof that the corpus's set comparison still
// discriminates. Spec §6.3 makes emission order insignificant, so runDiagnose
// compares diagnostics as a set — and a set matcher is exactly the shape that
// can rot into "anything passes" without any corpus file going red, because
// every diagnose case today expects at most one diagnostic and would keep
// passing against a matcher that checked nothing.
//
// Each case below is one way a reader can be wrong. If any of them stops being
// reported, the diagnose tier of the corpus has stopped measuring.
func TestMatchDiagnostics(t *testing.T) {
	d := func(path, code string) Diagnostic {
		return Diagnostic{Severity: Warning, Path: path, Code: code, Message: "some English nobody compares"}
	}
	w := func(path, code string) expectedDiagnostic {
		return expectedDiagnostic{Path: path, Code: code}
	}

	for _, tc := range []struct {
		name             string
		got              []Diagnostic
		want             []expectedDiagnostic
		unmatched, extra int
	}{
		{
			name: "order does not matter",
			got:  []Diagnostic{d("/areas/0/polygon", "polygon-nested-rings"), d("/waypoints/0/nmae", "unknown-key")},
			want: []expectedDiagnostic{w("/waypoints/0/nmae", "unknown-key"), w("/areas/0/polygon", "polygon-nested-rings")},
		},
		{
			// Two diagnostics with the same code and wildly different English
			// still pair: the English is advisory.
			name: "the English is not compared",
			got:  []Diagnostic{{Severity: Warning, Path: "/waypoints/0/nmae", Code: "unknown-key", Message: "totally different prose"}},
			want: []expectedDiagnostic{{Path: "/waypoints/0/nmae", Code: "unknown-key", MessageAdvisory: "unknown-key"}},
		},
		{
			name:      "a diagnostic the reader never reported",
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown-key")},
			unmatched: 1,
		},
		{
			name:  "a diagnostic the reader invented",
			got:   []Diagnostic{d("/waypoints/0/nmae", "unknown-key")},
			extra: 1,
		},
		{
			name:      "the right message at the wrong path",
			got:       []Diagnostic{d("/waypoints/1/nmae", "unknown-key")},
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown-key")},
			unmatched: 1,
			extra:     1,
		},
		{
			name:      "the right path with the wrong message",
			got:       []Diagnostic{d("/waypoints/0/nmae", "timestamp-not-utc")},
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown-key")},
			unmatched: 1,
			extra:     1,
		},
		{
			// Two expectations must consume two distinct diagnostics. A matcher
			// that let one reported diagnostic satisfy both would report a
			// reader that found half the defects as fully correct.
			name:      "duplicates are not double-counted",
			got:       []Diagnostic{d("/networks/0/chains/0/links/1", "link-arc-absent")},
			want:      []expectedDiagnostic{w("/networks/0/chains/0/links/1", "link-arc-absent"), w("/networks/0/chains/0/links/1", "link-arc-absent")},
			unmatched: 1,
		},
		{
			// A cannot-detect case: no diagnostics either side.
			name: "nothing expected and nothing reported",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unmatched, extra := matchDiagnostics(tc.got, tc.want)
			if len(unmatched) != tc.unmatched {
				t.Errorf("unmatched = %d %v, want %d", len(unmatched), unmatched, tc.unmatched)
			}
			if len(extra) != tc.extra {
				t.Errorf("extra = %d %v, want %d", len(extra), extra, tc.extra)
			}
		})
	}
}
