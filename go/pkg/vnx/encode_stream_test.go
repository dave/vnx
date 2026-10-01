package vnx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestEncodeToCoversEveryCollectionTheDocumentDeclares is the structural guard
// on documentCollections.
//
// documentCollections names its collections one by one. A seventh added to
// vnx.Document and forgotten there would be SILENTLY DROPPED from every
// exported file — still valid VNX, and the data simply gone. Reflection finds
// the omission instead of trusting the next author to remember.
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
		if f.Name == "AppKeys" {
			appKeyArm(t, f)
			continue
		}
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
		// The one zero value the encoder refuses: an area with no ring, which
		// a reader would drop (§3.7). It is given the least ring there is.
		if f.Name == "Areas" {
			doc.Areas[0].Polygon = []Position{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}}
		}
		if c.count(doc) != 1 {
			t.Errorf("documentCollections[%s].count says a document holding one %s holds %d",
				c.key, f.Name, c.count(doc))
		}
		if n := c.count(&Document{}); n != 0 {
			t.Errorf("documentCollections[%s].count says an absent %s holds %d", c.key, f.Name, n)
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
		t.Fatalf("found only %d slice fields on vnx.Document, expected at least the 6 collections "+
			"and the application keys — the reflection is broken, not the table", slices)
	}
}

// appKeyArm is the application-key arm of the structural guard above.
//
// ⚠ AppKeys HAS NO ENTRY IN documentCollections, AND MUST NOT. A collection is
// one fixed key holding an array; application keys are an open set of keys,
// each holding any value, written by streamAppKeys. So the table cannot vouch
// for them, and this arm fills one dotted key through the reflected field and
// asserts that EncodeTo wrote it. Its value is an empty array because SPEC §3.9
// exempts an application key from absent-equals-empty: a writer that skipped
// an "empty" one would drop content.
func appKeyArm(t *testing.T, f reflect.StructField) {
	t.Helper()
	const key = "com.example.arm"
	doc := &Document{}
	reflect.ValueOf(doc).Elem().FieldByIndex(f.Index).Set(
		reflect.ValueOf([]AppKey{{Key: key, Value: []any{}}}))
	var b bytes.Buffer
	if err := EncodeTo(&b, doc); err != nil {
		t.Errorf("encoding a document holding one application key: %v", err)
		return
	}
	var tree map[string]any
	if err := json.Unmarshal(b.Bytes(), &tree); err != nil {
		t.Fatalf("EncodeTo wrote something that is not a JSON object: %v\n%s", err, b.String())
	}
	v, ok := tree[key]
	if !ok {
		t.Errorf("a document holding the application key %q emitted no such key — every document "+
			"carrying one would be exported with it silently missing:\n%s", key, b.String())
		return
	}
	if arr, isArr := v.([]any); !isArr || len(arr) != 0 {
		t.Errorf("application key %q was written as %#v, want the empty array it holds", key, v)
	}
}

// Map keys are COLLECTED AND SORTED, never iterated.
//
// ⚠ IT IS NOT A RULE OF THE FORMAT AND IS STILL A REQUIREMENT HERE. §4.4 says
// key order is not significant, so no reader depends on this; what depends on
// it is the writer being deterministic at all. Go map iteration is randomised
// per process and per range, so an encoder that iterated these two maps would
// emit a different file on every run, with no error and no panic.
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
		"node": sortedNames(n.Nodes),
		"arc":  sortedArcNames(n.Arcs),
	} {
		last := -1
		for _, k := range want {
			at := strings.Index(out, `"`+k+`":`)
			if at < 0 {
				t.Fatalf("%s key %q was not emitted at all", what, k)
			}
			if at < last {
				t.Errorf("%s key %q appears out of sorted order", what, k)
			}
			last = at
		}
		// ⚠ "node-10" sorts BEFORE "node-2" as a string, and that is the
		// ordering this writer uses. Numeric ordering would be a different rule.
		if len(want) < 8 {
			t.Fatalf("only %d %s keys; too few for order to be meaningful", len(want), what)
		}
	}
}

func sortedNames(m map[string]Vertex) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
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

// TestAnAbsentCollectionAndAnEmptyOneAreWrittenAlike pins §3.1 at every
// collection.
//
// An absent collection and an empty one carry the same content and a reader
// must treat them alike, so a writer may spell either — and this one spells
// both as absence. The rule is asserted directly because both goldens carry all
// six collections populated and can catch neither half of it. Application keys
// are not in the table and not in this rule: SPEC §3.9 exempts them.
func TestAnAbsentCollectionAndAnEmptyOneAreWrittenAlike(t *testing.T) {
	const headerOnly = "{\"vnx\":\"0.8.0\"}\n"
	for _, c := range documentCollections {
		var absent bytes.Buffer
		if err := EncodeTo(&absent, &Document{}); err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if absent.String() != headerOnly {
			t.Errorf("a document with no %s produced %q, want the header-only document", c.field, absent.String())
		}

		doc := &Document{}
		v := reflect.ValueOf(doc).Elem().FieldByName(c.field)
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		var empty bytes.Buffer
		if err := EncodeTo(&empty, doc); err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if empty.String() != absent.String() {
			t.Errorf("an empty %s produced %q and an absent one produced %q; §3.1 makes them the same content",
				c.field, empty.String(), absent.String())
		}
	}
}

// The scalar half. An empty header field is absent, not "", and the four are
// written in the order [Document] declares them.
func TestEncodeToOmitsEmptyHeaderFields(t *testing.T) {
	var b bytes.Buffer
	if err := EncodeTo(&b, &Document{Version: "0.8.0", Generator: "g", Created: "2026-09-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	want := "{\"vnx\":\"0.8.0\",\"generator\":\"g\",\"created\":\"2026-09-02T00:00:00Z\"}\n"
	if b.String() != want {
		t.Errorf("got %q\nwant %q", b.String(), want)
	}
}

func TestEncodeToRefusesANilDocument(t *testing.T) {
	if err := EncodeTo(&bytes.Buffer{}, nil); err == nil {
		t.Error("EncodeTo(nil) returned no error")
	}
}

var errSinkFull = errors.New("sink full")

// failAfter fails once n bytes have been accepted. It models the case that
// matters: a file that stops accepting writes PART WAY through a document, so
// the bytes already written are a valid prefix of a corrupt file.
type failAfter struct {
	n       int
	written int
	calls   int
}

func (f *failAfter) Write(p []byte) (int, error) {
	f.calls++
	if f.written >= f.n {
		return 0, errSinkFull
	}
	room := f.n - f.written
	if len(p) <= room {
		f.written += len(p)
		return len(p), nil
	}
	f.written = f.n
	// A SHORT WRITE WITH NO ERROR. io.Writer permits it only alongside an
	// error, and a wrapper that trusted that promise would lose these bytes
	// silently — which is why StickyWriter checks n as well as err.
	return room, nil
}

// A streaming encoder can fail HALF WAY THROUGH A DOCUMENT. It must return an
// error rather than a truncated success — otherwise the sink Syncs, sets
// committed, and renames a corrupt file into place.
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
	if w.calls < 100 {
		t.Fatalf("only %d writes for a %d byte document — the probe is not seeing the writes", w.calls, n)
	}
}

// A latched StickyWriter must not keep calling the sink underneath it. A file
// handle that has failed once is not a thing to keep writing to, and thousands
// of doomed syscalls per document is the difference between an error and a
// hang.
func TestALatchedWriterStopsCallingTheSink(t *testing.T) {
	w := &failAfter{n: 8}
	if err := EncodeTo(w, oracleEverything()); err == nil {
		t.Fatal("expected an error")
	}
	if w.calls > 8 {
		t.Errorf("the sink was called %d times after failing; the latch is not stopping the writes", w.calls)
	}
}
