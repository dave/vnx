package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Path    string `json:"path"`
	Message string `json:"message"`
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

	// Schema is what the JSON Schema (corpus/schema) makes of this file:
	// "valid", "invalid", or "not-json" when it is not JSON at all and no
	// validator can reach it. It is recorded per case rather than derived,
	// because the interesting entries are the ones where the schema and the
	// reader DISAGREE — reject/version-too-new.vnx is schema-valid and refused,
	// and every must-diagnose file bar two is schema-valid and diagnosed. A
	// green schema is not a valid document, and this column is where that stops
	// being a claim in prose. Required for .vnx, meaningless for .vnz.
	Schema string `json:"schema"`

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
		if strings.Join(attachments, ",") != strings.Join(*want, ",") {
			t.Errorf("attachments = %v, want %v", attachments, *want)
		}
	}
	// Accept files are stored in canonical form (§4.5), so canonicalising one
	// is a no-op. This is not decoration: it is what proves the whole file —
	// unknown data records included, whose bodies nothing else in this package
	// inspects — survives parse and re-render at value level.
	if c.isContainer() {
		return
	}
	got, err := Canonical(c.data)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if !bytes.Equal(got, c.data) {
		t.Errorf("corpus file is not in canonical form; first difference at byte %d", firstDiff(got, c.data))
	}
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
		// Exact float comparison, deliberately. These are the same doubles the
		// file holds, parsed by the same parser, so anything but equality means
		// a channel was swapped or precision was lost.
		if got.Lon != want.Lon || got.Lat != want.Lat {
			t.Errorf("%s: got lon=%v lat=%v, want lon=%v lat=%v",
				want.At, got.Lon, got.Lat, want.Lon, want.Lat)
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
			switch {
			case strings.HasSuffix(path, ".vnz"):
				if exp.Schema != "" {
					t.Fatalf("%s declares a schema verdict, but the schema applies to a DOCUMENT, not an archive", sidecar)
				}
			case exp.Schema != "valid" && exp.Schema != "invalid" && exp.Schema != "not-json":
				t.Fatalf("%s: schema verdict is %q, want valid, invalid or not-json", sidecar, exp.Schema)
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

// The formatter cases (corpus/format).
//
// The accept files cannot catch a formatter bug, and finding that out is why
// this exists. They are stored CANONICAL, so canonicalising one only ever asks
// the renderer about values already spelled correctly — a writer that renders
// negative zero as `-0`, or `0.000001` as `1e-06`, is never asked the question
// at all. That is not hypothetical: the reference implementation had exactly the
// negative-zero bug, its own corpus was blind to it, and only writing an input
// file in the WRONG spelling surfaced it.
//
// So each case here is a deliberately messy document paired with exactly what a
// conforming formatter must emit for it. This is the test another language's
// implementation runs to prove its layout agrees byte-for-byte with everyone
// else's.
func TestFormatterCases(t *testing.T) {
	dir := filepath.Join(corpusDir, "format")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".in.vnx") {
			continue
		}
		cases++
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			wantPath := filepath.Join(dir, strings.TrimSuffix(name, ".in.vnx")+".canonical.vnx")
			want, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("%s has no expected output beside it: %v", name, err)
			}
			// The input must NOT already be canonical, or the case proves
			// nothing — it would pass for any formatter that echoed its input.
			if bytes.Equal(in, want) {
				t.Fatal("the input is already canonical; this case would pass vacuously")
			}
			got, err := Canonical(in)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("formatter output differs at byte %d\n got: %s\nwant: %s",
					firstDiff(got, want), got, want)
			}
			// And the expected output must be a fixed point, or it is not
			// canonical form.
			again, err := Canonical(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, want) {
				t.Fatalf("%s is not itself canonical", wantPath)
			}
		})
	}
	if cases == 0 {
		t.Fatal("no formatter cases found — dead probe, not a clean result")
	}
}

// matchDiagnostics pairs each expected diagnostic with one distinct reported
// diagnostic, and returns what could not be paired from each side. Matching is
// on an exact Path and a substring Message.
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
			if !paired[i] && d.Path == w.Path && strings.Contains(d.Message, w.Message) {
				paired[i] = true
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, fmt.Sprintf("%s ~ %q", w.Path, w.Message))
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
	d := func(path, msg string) Diagnostic {
		return Diagnostic{Severity: Warning, Path: path, Message: msg}
	}
	w := func(path, msg string) expectedDiagnostic {
		return expectedDiagnostic{Path: path, Message: msg}
	}

	for _, tc := range []struct {
		name             string
		got              []Diagnostic
		want             []expectedDiagnostic
		unmatched, extra int
	}{
		{
			name: "order does not matter",
			got:  []Diagnostic{d("/areas/0/polygon", "nested rings"), d("/waypoints/0/nmae", "unknown key")},
			want: []expectedDiagnostic{w("/waypoints/0/nmae", "unknown key"), w("/areas/0/polygon", "nested rings")},
		},
		{
			name: "message is matched as a substring",
			got:  []Diagnostic{d("/waypoints/0/nmae", "unknown key \"nmae\" — skipped")},
			want: []expectedDiagnostic{w("/waypoints/0/nmae", "unknown key")},
		},
		{
			name:      "a diagnostic the reader never reported",
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown key")},
			unmatched: 1,
		},
		{
			name:  "a diagnostic the reader invented",
			got:   []Diagnostic{d("/waypoints/0/nmae", "unknown key")},
			extra: 1,
		},
		{
			name:      "the right message at the wrong path",
			got:       []Diagnostic{d("/waypoints/1/nmae", "unknown key")},
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown key")},
			unmatched: 1,
			extra:     1,
		},
		{
			name:      "the right path with the wrong message",
			got:       []Diagnostic{d("/waypoints/0/nmae", "could not parse")},
			want:      []expectedDiagnostic{w("/waypoints/0/nmae", "unknown key")},
			unmatched: 1,
			extra:     1,
		},
		{
			// Two expectations must consume two distinct diagnostics. A matcher
			// that let one reported diagnostic satisfy both would report a
			// reader that found half the defects as fully correct.
			name:      "duplicates are not double-counted",
			got:       []Diagnostic{d("/networks/0/chains/0/links/1", "not in this network")},
			want:      []expectedDiagnostic{w("/networks/0/chains/0/links/1", "not in this network"), w("/networks/0/chains/0/links/1", "not in this network")},
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
