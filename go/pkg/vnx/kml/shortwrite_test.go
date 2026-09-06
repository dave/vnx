package kml

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// ⚠ THE FAILURE THIS GUARDS IS DATA LOSS PRESENTING AS A CLEAN EXPORT, measured
// in spec 4b and restated in 5b §4.1b: an ignored short write means Write
// returns nil, writeAtomically Syncs, sets `committed`, and renames a TRUNCATED
// file into place as a success. Nothing downstream can tell that apart from a
// real export.
//
// The pattern is 4b's TestEncodeToFailsRatherThanTruncating: fail the sink at 0,
// 1, 17, 512, 4096 and n−1 bytes, AND include the working case, so a latch that
// never lets go cannot be mistaken for correct error handling.

var errSinkFull = errors.New("sink full")

// failAfter accepts n bytes and then refuses. It also models the OTHER half of
// the hazard on the last accepted write: a SHORT write with no error at all,
// which io.Writer permits and which a wrapper that only checked `err != nil`
// would sail straight past.
type failAfter struct {
	n       int
	written int
}

func (f *failAfter) Write(p []byte) (int, error) {
	if f.written >= f.n {
		return 0, errSinkFull
	}
	room := f.n - f.written
	if len(p) <= room {
		f.written += len(p)
		return len(p), nil
	}
	f.written += room
	// A short count and a nil error. This is the one io.Writer allows and the
	// one that used to slip through.
	return room, nil
}

// bigDocument is large enough that a failure at 4096 bytes lands in the middle
// of the walk rather than after it.
func bigDocument(t *testing.T) *vnx.Document {
	t.Helper()
	d := fixture()
	var pips []vnx.Position
	for i := 0; i < 2000; i++ {
		pips = append(pips, posEle(-3.5+float64(i)/10000, 57.5+float64(i)/10000, float64(600+i%100)))
	}
	d.Tracks = append(d.Tracks, vnx.Track{Name: "Long Walk", Segments: []vnx.Segment{{Pips: pips}}})
	return d
}

func TestWriteFailsRatherThanTruncating(t *testing.T) {
	doc := bigDocument(t)
	var full bytes.Buffer
	if _, err := Write(&full, doc); err != nil {
		t.Fatal(err)
	}
	n := full.Len()
	if n < 4096 {
		t.Fatalf("the subject is %d bytes; too small for a mid-document failure to be interesting", n)
	}

	for _, limit := range []int{0, 1, 17, 512, 4096, n - 1} {
		w := &failAfter{n: limit}
		report, err := Write(w, doc)
		if err == nil {
			t.Errorf("a writer that stopped accepting bytes after %d of %d returned NO error — a truncated file would have been renamed into place as a success", limit, n)
			continue
		}
		if !errors.Is(err, errSinkFull) && !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("limit %d: unexpected error %v", limit, err)
		}
		// ⚠ AND THE REPORT IS ZERO. A caller reading Emitted from a walk that
		// did not finish would refuse or accept on a count that means nothing.
		if report.Emitted != 0 || report.Diagnostics != nil {
			t.Errorf("limit %d: report from a failed write = %+v, want the zero value", limit, report)
		}
	}

	// The working case, so this test cannot pass on a writer that always fails.
	w := &failAfter{n: n}
	report, err := Write(w, doc)
	if err != nil {
		t.Fatalf("a writer with room for the whole document failed: %v", err)
	}
	if w.written != n {
		t.Fatalf("wrote %d bytes, the document is %d", w.written, n)
	}
	if report.Emitted == 0 {
		t.Fatal("the working case emitted nothing, so the failing cases prove nothing")
	}
}

// The sticky writer must be the one in pkg/vnx and not a second one wrapped
// around it: a second wrapper latches into an object nobody reads, and the walk
// finishes "clean".
func TestWriteReusesTheSharedStickyWriter(t *testing.T) {
	sticky := vnx.Stick(&failAfter{n: 10})
	if _, err := Write(sticky, bigDocument(t)); err == nil {
		t.Fatal("writing through an already-sticky writer that fails returned no error")
	}
	if sticky.Err() == nil {
		t.Fatal("the caller's own StickyWriter did not latch the error — the writer wrapped it a second time, and the error is held by nobody")
	}
}
