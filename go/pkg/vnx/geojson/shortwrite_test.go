package geojson

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// The short-write red-proof (spec §4.1b).
//
// ⚠ THE FAILURE THIS GUARDS IS NOT A CRASH. An ignored short write means Write
// returns nil, vnxbridge's sink Syncs, sets `committed`, and RENAMES A
// TRUNCATED FILE INTO PLACE AS A SUCCESSFUL EXPORT. The user sees a green tick
// and a corrupt .geojson. Spec 4b measured it; this is the same test shape.

// failingSink stops after n bytes. err controls WHICH of the two failures it
// is, and both must be caught: io.Writer permits a short write only alongside
// an error, but a writer that trusted that promise would hand a truncated
// document to the sink and call it a success.
type failingSink struct {
	limit   int
	written int
	silent  bool
	buf     bytes.Buffer
}

var errSink = errors.New("sink failed")

func (s *failingSink) Write(p []byte) (int, error) {
	room := s.limit - s.written
	if room <= 0 {
		if s.silent {
			return 0, nil // a short write with NO error
		}
		return 0, errSink
	}
	if len(p) > room {
		s.buf.Write(p[:room])
		s.written += room
		if s.silent {
			return room, nil
		}
		return room, errSink
	}
	s.buf.Write(p)
	s.written += len(p)
	return len(p), nil
}

// bigDocument is large enough that the 4096-byte cut is a real cut. The
// fixture is under 2 KB, so on the fixture alone that limit would silently be
// skipped and the brief's list would be a list of five.
func bigDocument() *vnx.Document {
	pips := make([]vnx.Position, 0, 400)
	for i := 0; i < 400; i++ {
		pips = append(pips, pos(-3+float64(i)/1000, 57+float64(i)/1000, ptr(float64(600+i))))
	}
	return &vnx.Document{Tracks: []vnx.Track{{Name: "long walk", Segments: []vnx.Segment{{Pips: pips}}}}}
}

func TestWriteFailsRatherThanTruncating(t *testing.T) {
	docs := map[string]func() *vnx.Document{"fixture": fixture, "big": bigDocument}
	for name, build := range docs {
		t.Run(name, func(t *testing.T) {
			var full bytes.Buffer
			if _, err := Write(&full, build()); err != nil {
				t.Fatalf("the working case must work, or this test passes vacuously: %v", err)
			}
			n := full.Len()

			cuts := 0
			for _, silent := range []bool{false, true} {
				for _, limit := range []int{0, 1, 17, 512, 4096, n - 1} {
					if limit >= n {
						continue
					}
					cuts++
					sink := &failingSink{limit: limit, silent: silent}
					report, err := Write(sink, build())
					if err == nil {
						t.Errorf("silent=%v limit=%d: Write returned nil after writing %d of %d bytes.\n"+
							"The sink would now Sync and rename a TRUNCATED file into place as a success.",
							silent, limit, sink.written, n)
					}
					if report.Emitted != 0 || report.Diagnostics != nil {
						t.Errorf("silent=%v limit=%d: a failed write returned a non-zero report %+v; a caller must not be able to read a count off a run that did not finish",
							silent, limit, report)
					}
					if silent && !errors.Is(err, io.ErrShortWrite) {
						t.Errorf("silent=%v limit=%d: wanted io.ErrShortWrite, got %v", silent, limit, err)
					}
					if !silent && !errors.Is(err, errSink) {
						t.Errorf("silent=%v limit=%d: wanted the sink's own error, got %v", silent, limit, err)
					}
				}
			}
			if name == "big" && cuts != 12 {
				t.Errorf("the big document exercised %d cuts, wanted all 12 (6 limits × 2 failure modes) — 4096 must not be skipped, and n=%d", cuts, n)
			}

			// The working case, so the loop above cannot pass because Write
			// always fails: a sink with exactly enough room must succeed.
			sink := &failingSink{limit: n}
			report, err := Write(sink, build())
			if err != nil {
				t.Fatalf("a sink with room for the whole document must succeed, got %v", err)
			}
			if report.Emitted == 0 {
				t.Fatal("the working case emitted nothing")
			}
			if sink.buf.String() != full.String() {
				t.Fatal("the working case produced different bytes from the buffer run")
			}
		})
	}
}

// A non-finite coordinate cannot reach Write from Decode, but a hand-built
// Document carries one, and json.Marshal fails on it. It must fail the WRITE:
// rendering null would put a null ordinate in a position array, which RFC 7946
// forbids, and 0 would put the point off Africa.
func TestNonFiniteCoordinateFailsTheWrite(t *testing.T) {
	var buf bytes.Buffer
	report, err := Write(&buf, docWithLon(math.Inf(1)))
	if err == nil {
		t.Fatalf("a non-finite coordinate was written as if it were fine: %s", buf.String())
	}
	if report.Emitted != 0 {
		t.Errorf("report was %+v on a failed write", report)
	}
}

func docWithLon(lon float64) *vnx.Document {
	return &vnx.Document{Points: []vnx.Point{{Name: "broken", At: vnx.Position{Lon: lon, Lat: 1}}}}
}
