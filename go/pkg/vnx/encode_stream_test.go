package vnx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// ⚠ THE DIFFERENTIAL ORACLE THAT LIVED HERE IS GONE, AND DELIBERATELY.
// Until T8 this file carried TestEncodeToMatchesEncodeOverTheWholeCorpus,
// comparing EncodeTo against the pre-4b encodeMaterialised over all 37 oracle
// documents. T8 deleted encodeMaterialised — it was the whole point of the
// spec — so there is no second implementation left to diff against, and a test
// comparing EncodeTo with Encode would be comparing a function with a wrapper
// around itself: green, and incapable of failing.
//
// What replaces it is TestEncodeMatchesItsFrozenOutput in encode_oracle_test.go,
// which pins the same 37 documents to committed expectation bytes that predate
// every line of phase 2. That is the stronger of the two, and it was written
// first for exactly this moment.

// TestEncodeToCoversEveryCollectionTheDocumentDeclares is the structural guard
// on documentCollections, and it answers spec §8's open question 2.
//
// documentCollections names its collections one by one. A tenth added to
// vnx.Document and forgotten there would be SILENTLY DROPPED from every
// exported file — still valid VNX, still a green pair of goldens, and the data
// simply gone. Reflection finds the omission instead of trusting the next
// author to remember.
//
// It is deliberately a THIRD guard in this shape rather than a re-pointing of
// vnxbridge's TestDocumentIsEmptyCountsEveryCollectionTheDocumentDeclares and
// TestCountsCoverEveryCollectionTheDocumentDeclares. Those two guard
// documentIsEmpty and countDocument, which are different hand-written lists
// over the same struct; re-pointing either at this table would delete its
// coverage of its own subject.
func TestEncodeToCoversEveryCollectionTheDocumentDeclares(t *testing.T) {
	byField := map[string]documentCollection{}
	for _, c := range documentCollections {
		if _, dup := byField[c.field]; dup {
			t.Fatalf("two entries claim Document.%s", c.field)
		}
		byField[c.field] = c
	}

	typ := reflect.TypeOf(Document{})
	slices := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		slices++
		c, ok := byField[f.Name]
		if !ok {
			t.Errorf("Document.%s has no entry in documentCollections — every document carrying "+
				"one would be exported with that collection silently missing", f.Name)
			continue
		}
		delete(byField, f.Name)

		// One zero-valued element, set reflectively so the test cannot be made
		// green by hand-writing a fixture for the new field.
		doc := &Document{}
		reflect.ValueOf(doc).Elem().Field(i).Set(reflect.MakeSlice(f.Type, 1, 1))
		if !c.present(doc) {
			t.Errorf("documentCollections[%s].present says a document holding one %s does not carry it",
				c.key, f.Name)
		}
		// And an EMPTY-but-present collection is still present. Spec §3.1 makes
		// that a different document from one where the key is absent, so a
		// `present` written as len(...) > 0 would drop it — which is what this
		// arm exists to catch, reflectively, for every collection at once.
		empty := &Document{}
		reflect.ValueOf(empty).Elem().Field(i).Set(reflect.MakeSlice(f.Type, 0, 0))
		if !c.present(empty) {
			t.Errorf("documentCollections[%s].present says an EMPTY %s is absent; an empty "+
				"collection and an absent one are different documents", c.key, f.Name)
		}
		if absent := (&Document{}); c.present(absent) {
			t.Errorf("documentCollections[%s].present says an ABSENT %s is present", c.key, f.Name)
		}
		var b bytes.Buffer
		if err := EncodeTo(&b, doc); err != nil {
			t.Errorf("encoding a document holding one %s: %v", f.Name, err)
			continue
		}
		if !strings.Contains(b.String(), fmt.Sprintf("%q:", c.key)) {
			t.Errorf("a document holding one %s emitted no %q key:\n%s", f.Name, c.key, b.String())
		}
	}
	for field := range byField {
		t.Errorf("documentCollections has an entry for Document.%s, which is not a slice field "+
			"on the struct — the table has outlived its subject", field)
	}
	// LIVENESS. Zero slice fields and every slice field covered are the same
	// green, and the reflection is the half that can break silently.
	if slices < 7 {
		t.Fatalf("found only %d slice fields on vnx.Document, expected at least the 7 the format "+
			"defines — the reflection is broken, not the table", slices)
	}
}

// The container half of the ordering rule, asserted rather than trusted to a
// hand-maintained slice literal. streamObject iterates each table in order and
// does no sorting of its own, so a table's order IS the output's.
//
// ⚠ THIS IS NOT PEDANTRY, IT CAUGHT A LIVE ERROR. Spec 4b's plan and its brief
// both state the network order as "areas, arcs, chains, data, nodes, points".
// It is not: "arcs" and "areas" agree on "ar" and then `c` < `e`, so `arcs`
// comes FIRST. Written the remembered way, the encoder produced a perfectly
// valid file that differed from the conformance corpus at byte 885. A guard
// that compared against a remembered list would have blessed it.
func TestStreamedContainerTablesAreInCanonicalOrder(t *testing.T) {
	docKeys := make([]string, 0, len(documentCollections))
	for _, c := range documentCollections {
		docKeys = append(docKeys, c.key)
	}
	netKeys := make([]string, 0)
	for _, c := range networkContainers(emitter{}, Network{}) {
		netKeys = append(netKeys, c.key)
	}
	for _, tbl := range []struct {
		what string
		keys []string
		min  int
	}{
		{"documentCollections", docKeys, 7},
		{"networkContainers", netKeys, 6},
	} {
		if len(tbl.keys) < tbl.min {
			t.Fatalf("%s has %d entries, expected at least %d — the guard is reading the wrong table",
				tbl.what, len(tbl.keys), tbl.min)
		}
		sorted := append([]string(nil), tbl.keys...)
		sort.Strings(sorted)
		if !reflect.DeepEqual(tbl.keys, sorted) {
			t.Errorf("%s is ordered %v; canonical form requires %v", tbl.what, tbl.keys, sorted)
		}
	}
	// The specific inversion the plan got wrong, named so a future reader who
	// "corrects" it back is told why.
	if sort.SearchStrings(netKeys, "arcs") > sort.SearchStrings(netKeys, "areas") {
		t.Error("`areas` is emitted before `arcs`; lexicographically `arcs` comes first (c < e)")
	}
}

// The nodes-after-arcs rule, asserted on real bytes rather than on the table,
// because it is the one a streaming encoder is actively tempted to break: it
// would be convenient to emit the nodes first so the arcs' from/to keys are
// established before they are referenced. Canonical form does not allow it.
func TestNodesAreEmittedAfterArcs(t *testing.T) {
	var b bytes.Buffer
	if err := EncodeTo(&b, &Document{Networks: []Network{oracleNetwork()}}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	arcs := strings.Index(out, `"arcs":`)
	nodes := strings.Index(out, `"nodes":`)
	if arcs < 0 || nodes < 0 {
		t.Fatalf("the subject emitted no arcs (%d) or no nodes (%d); this test would pass vacuously", arcs, nodes)
	}
	if nodes < arcs {
		t.Errorf("`nodes` is emitted at %d, before `arcs` at %d", nodes, arcs)
	}
}

// Map keys are COLLECTED AND SORTED, never iterated — asserted directly on the
// two maps, in addition to the determinism test's two arms.
func TestNetworkMapKeysAreEmittedSorted(t *testing.T) {
	n := oracleNetwork()
	if len(n.Nodes) < 8 || len(n.Arcs) < 8 {
		t.Fatalf("the subject has %d nodes and %d arcs; a small map is trivially ordered",
			len(n.Nodes), len(n.Arcs))
	}
	var b bytes.Buffer
	if err := EncodeTo(&b, &Document{Networks: []Network{n}}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for what, want := range map[string][]string{
		"node": sortedNames(n.Nodes, func(k string) string { return k }),
		"arc":  sortedArcNames(n.Arcs),
	} {
		last := -1
		for _, k := range want {
			at := strings.Index(out, `"`+k+`": `)
			if at < 0 {
				t.Fatalf("%s key %q was not emitted at all", what, k)
			}
			if at < last {
				t.Errorf("%s key %q appears out of sorted order", what, k)
			}
			last = at
		}
		// ⚠ "node-10" sorts BEFORE "node-2" as a string, and that is the
		// ordering the format uses. If the keys were numerically ordered this
		// assertion would be checking a different rule from writeObject's.
		if len(want) < 8 {
			t.Fatalf("only %d %s keys; too few for order to be meaningful", len(want), what)
		}
	}
}

func sortedNames(m map[string]Position, f func(string) string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, f(k))
	}
	sort.Strings(out)
	return out
}

func sortedArcNames(m map[string]Arc) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestAnAbsentCollectionOmitsItsKeyAndAnEmptyOneDoesNot pins both sides of
// spec §3.1's distinction, at every collection.
//
// ⚠ THE RULE REVERSED ON 2026-09-09, and the test it replaced knew exactly
// which case was at stake — its comment read "an EMPTY non-nil slice, which is
// the case an `if len(...) > 0` guard and an `if ... != nil` guard disagree
// about", and it asserted the len guard. §3.1 now makes an absent collection
// and an empty one the same CONTENT and different DOCUMENTS: a reader treats
// them alike, and a formatter reproduces which it was given, because §8.1's
// accept tier requires the bytes back.
//
// Both goldens carry all seven containers populated, so neither can catch
// either half of this. It is asserted directly.
func TestAnAbsentCollectionOmitsItsKeyAndAnEmptyOneDoesNot(t *testing.T) {
	const headerOnly = "{\n  \"vnx\": \"0.1.0\"\n}\n"
	for _, c := range documentCollections {
		// ABSENT: nil, so the key is omitted and nothing else appears.
		var absent bytes.Buffer
		if err := EncodeTo(&absent, &Document{}); err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if absent.String() != headerOnly {
			t.Errorf("a document with no %s produced %q, want the header-only document", c.field, absent.String())
		}

		// EMPTY BUT PRESENT: a non-nil zero-length slice, which must emit its
		// key with an empty array.
		doc := &Document{}
		v := reflect.ValueOf(doc).Elem().FieldByName(c.field)
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		var empty bytes.Buffer
		if err := EncodeTo(&empty, doc); err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		want := fmt.Sprintf("{\n  \"vnx\": \"0.1.0\",\n  %q: []\n}\n", c.key)
		if empty.String() != want {
			t.Errorf("an empty %s produced %q, want %q", c.field, empty.String(), want)
		}
	}
}

// The scalar half. An empty header field is absent, not "".
func TestEncodeToOmitsEmptyHeaderFieldsAndSortsTheRest(t *testing.T) {
	var b bytes.Buffer
	if err := EncodeTo(&b, &Document{Version: "0.1.0", Generator: "g", Created: "c"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"created\": \"c\",\n  \"generator\": \"g\",\n  \"vnx\": \"0.1.0\"\n}\n"
	if b.String() != want {
		t.Errorf("got %q\nwant %q", b.String(), want)
	}
}

func TestEncodeToRefusesANilDocument(t *testing.T) {
	if err := EncodeTo(&bytes.Buffer{}, nil); err == nil {
		t.Error("EncodeTo(nil) returned no error")
	}
}

// A streaming encoder can fail HALF WAY THROUGH A DOCUMENT, which Encode never
// could. It must return an error rather than a truncated success — otherwise
// the sink Syncs, sets committed, and renames a corrupt file into place.
func TestEncodeToFailsRatherThanTruncating(t *testing.T) {
	doc := oracleEverything()
	var full bytes.Buffer
	if err := EncodeTo(&full, doc); err != nil {
		t.Fatal(err)
	}
	n := full.Len()
	if n < 4096 {
		t.Fatalf("the subject is %d bytes; too small for a mid-document failure to be interesting", n)
	}
	for _, limit := range []int{0, 1, 17, 512, 4096, n - 1} {
		w := &failAfter{n: limit}
		err := EncodeTo(w, doc)
		if err == nil {
			t.Errorf("a writer that stopped accepting bytes after %d of %d returned NO error", limit, n)
			continue
		}
		if !errors.Is(err, errSinkFull) && !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("limit %d: %v", limit, err)
		}
	}
	// And the working case, so a latch that never lets go is not mistaken for
	// correct error handling.
	w := &failAfter{n: n}
	if err := EncodeTo(w, doc); err != nil {
		t.Fatalf("a writer with room for the whole document failed: %v", err)
	}
	if w.written != n {
		t.Fatalf("wrote %d bytes, the document is %d", w.written, n)
	}
}
