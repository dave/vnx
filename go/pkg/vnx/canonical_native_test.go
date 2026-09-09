package vnx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// The T4 guards: native numbers in the writers, and error propagation.
//
// Until spec 4b every value that reached writeCanonical had been through
// json.Marshal and back, so every number was a json.Number. The streaming
// encoder hands the writers the per-item trees DIRECTLY, and those hold native
// float64 / int64 / int. These tests are what stands between that change and a
// silently different file.

// renderNative renders a tree the way the streaming encoder will: straight
// through the writers, with no marshal-and-reparse in the middle.
func renderNative(t *testing.T, tree any) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := writeCanonical(&b, tree, 0); err != nil {
		t.Fatalf("native render: %v", err)
	}
	b.WriteByte('\n')
	return b.Bytes()
}

// TestNativeAndDecodedTreesRenderIdentically is T4's byte-identity argument,
// made a measurement instead of a paragraph.
//
// The SAME document is rendered twice: once from the NATIVE per-item trees the
// streaming encoder builds, and once from a json.Number tree decoded back out
// of that output — which is the path Canonical takes over foreign bytes. If the
// native number cases render anything differently from the decoded ones — a
// rounding, a spelling, an array that loses its one-line form — the two
// disagree here, over the whole oracle corpus.
//
// ⚠ IT IS ALSO THE PROOF THAT THE READ PATH IS UNAFFECTED. Widening
// allScalarNumbers changed code that `vnx fmt` runs on files this package did
// not write, where it should be a no-op because a decoded tree holds only
// json.Number. The decoded arm IS that path; if the widening moved it, this is
// where it shows.
//
// ⚠ Before T8 the native arm called documentTree. That function is gone — the
// materialising path was the thing spec 4b existed to delete — so the native
// arm is now EncodeTo itself, which is the tree-free rendering it was standing
// in for.
func TestNativeAndDecodedTreesRenderIdentically(t *testing.T) {
	cases := oracleCases(t)
	if len(cases) < 30 {
		t.Fatalf("only %d oracle cases — the corpus is not being loaded", len(cases))
	}
	native, decoded := 0, 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := EncodeTo(&b, c.doc); err != nil {
				t.Fatalf("EncodeTo: %v", err)
			}
			gotNative := b.Bytes()

			gotDecoded, err := Canonical(gotNative)
			if err != nil {
				t.Fatalf("Canonical: %v", err)
			}
			if !bytes.Equal(gotNative, gotDecoded) {
				i := firstDiff(gotNative, gotDecoded)
				t.Fatalf("the native tree and the decoded tree render differently at byte %d\n"+
					" native: %s\ndecoded: %s", i, excerpt(gotNative, i), excerpt(gotDecoded, i))
			}
			native, decoded = native+len(gotNative), decoded+len(gotDecoded)
		})
	}
	// Liveness: two empty renders are also equal.
	if native == 0 || decoded == 0 {
		t.Fatal("nothing was rendered; this comparison passed vacuously")
	}
}

// TestNativeNumberTypesReachTheWritersFromTheTrees names the three types the
// audit found missing, and fails with the exact message the streaming encoder
// would have produced without them.
func TestNativeNumberTypesReachTheWritersFromTheTrees(t *testing.T) {
	for _, c := range []struct {
		what string
		v    any
		want string
	}{
		{"int64, as Position.Time is (position.go:29)", int64(1778569391000), "1778569391000"},
		{"int, as PointStyle.IconSize is (item.go:17)", 35, "35"},
		{"int, as PointStyle.Importance is (item.go:21)", 5, "5"},
		{"float64, as every coordinate is", -3.6712, "-3.6712"},
		{"a negative-zero float64, which must render unsigned", negZero(), "0"},
		{"an int64 beyond 2^53, rounded exactly as the old path rounded it", int64(9007199254740993), "9007199254740992"},
	} {
		var b bytes.Buffer
		if err := writeCanonical(&b, c.v, 0); err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if b.String() != c.want {
			t.Errorf("%s: rendered %s, want %s", c.what, b.String(), c.want)
		}
	}
}

// TestInlineArrayRuleCoversEveryNumberTypeTheWriterAccepts keeps
// allScalarNumbers and writeCanonical's number cases in step, in BOTH
// directions.
//
// They are two lists of the same set, and the failure when they drift is
// asymmetric and silent: a type the writer renders but the inline rule rejects
// produces a position per six lines instead of one — valid JSON, and a diff the
// size of the file.
func TestInlineArrayRuleCoversEveryNumberTypeTheWriterAccepts(t *testing.T) {
	numbers := []any{json.Number("1"), 1.5, int64(2), int(3), nil}
	for _, v := range numbers {
		if !allScalarNumbers([]any{v}) {
			t.Errorf("allScalarNumbers rejects %T, which writeCanonical renders as a number — "+
				"every array holding one would lose its one-line form", v)
		}
		var b bytes.Buffer
		if err := writeCanonical(&b, v, 0); err != nil {
			t.Errorf("writeCanonical rejects %T, which allScalarNumbers counts as a number: %v", v, err)
		}
	}
	var b bytes.Buffer
	if err := writeArray(&b, numbers, 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "\n") {
		t.Errorf("an array of native numbers rendered multi-line:\n%s", b.String())
	}

	// The other direction, and the liveness half. A type NEITHER accepts keeps
	// the two lists honest: if allScalarNumbers were widened to "anything not a
	// container" this would go green while writeCanonical still refused.
	if allScalarNumbers([]any{int32(1)}) {
		t.Error("allScalarNumbers accepts int32, which writeCanonical refuses — an array of them " +
			"would take the inline branch and then fail mid-line")
	}
	if err := writeCanonical(&bytes.Buffer{}, int32(1), 0); err == nil {
		t.Error("writeCanonical accepted an int32 — the default case must stay a loud error, " +
			"because any other type in a tree is a bug in the builder above it")
	}
}

// ---------------------------------------------------------------------------
// Error propagation.
// ---------------------------------------------------------------------------

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

func TestAFailingWriterMakesTheRenderFailRatherThanTruncate(t *testing.T) {
	tree := bigItemTree(t)
	full := renderNative(t, tree)
	if len(full) < 4096 {
		t.Fatalf("the subject renders %d bytes; too small for a mid-document failure to be interesting", len(full))
	}

	for _, limit := range []int{0, 1, 17, 512, 4096, len(full) - 2} {
		w := &failAfter{n: limit}
		err := writeCanonical(w, tree, 0)
		if err == nil {
			t.Errorf("a writer that stopped accepting bytes after %d of %d returned NO error — "+
				"the sink would Sync, set committed, and rename a truncated file into place as a success",
				limit, len(full))
			continue
		}
		if !errors.Is(err, errSinkFull) && !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("limit %d: failed with %v, want the sink's error or io.ErrShortWrite", limit, err)
		}
		if w.written > limit {
			t.Errorf("limit %d: %d bytes reached the writer", limit, w.written)
		}
	}
}

// The other half, and it is the one that catches a latch that never lets go: a
// writer that accepts everything must still render the whole document.
func TestAWorkingWriterIsNotLatchedOff(t *testing.T) {
	tree := bigItemTree(t)
	want := renderNative(t, tree)
	w := &failAfter{n: len(want)}
	if err := writeCanonical(w, tree, 0); err != nil {
		t.Fatalf("a writer with room for the whole document failed: %v", err)
	}
	if w.written != len(want)-1 {
		// renderNative appends the trailing newline itself, so the writer sees
		// one byte fewer than the file.
		t.Fatalf("wrote %d bytes, the document is %d", w.written, len(want)-1)
	}
	if w.calls < 100 {
		t.Fatalf("only %d writes for a %d byte document — the probe is not seeing the writes", w.calls, len(want))
	}
}

// A latched StickyWriter must not keep calling the sink underneath it. A file
// handle that has failed once is not a thing to keep writing to, and thousands
// of doomed syscalls per document is the difference between an error and a
// hang.
func TestALatchedWriterStopsCallingTheSink(t *testing.T) {
	tree := bigItemTree(t)
	w := &failAfter{n: 8}
	if err := writeCanonical(w, tree, 0); err == nil {
		t.Fatal("expected an error")
	}
	if w.calls > 4 {
		t.Errorf("the sink was called %d times after failing; the latch is not stopping the writes", w.calls)
	}
}

// bigItemTree is one item's tree, large enough that a failure part way through
// it is a real mid-render failure rather than a failure on the first fragment.
//
// It is a per-item tree deliberately: these tests guard writeCanonical itself,
// which Canonical still runs on foreign bytes for `vnx fmt`, and not only the
// streaming encoder above it. TestEncodeToFailsRatherThanTruncating covers the
// document level.
func bigItemTree(t *testing.T) map[string]any {
	t.Helper()
	ele := 340.5
	pips := make([]Position, 0, 400)
	for i := 0; i < 400; i++ {
		ms := int64(1778569391000 + i*1000)
		pips = append(pips, Position{Lon: -3.0 - float64(i)/1000, Lat: 57.0 + float64(i)/1000, Ele: &ele, Time: &ms})
	}
	tree, err := emitter{}.trackTree(Track{Name: "a long track", Segments: []Segment{{Pips: pips}}})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
