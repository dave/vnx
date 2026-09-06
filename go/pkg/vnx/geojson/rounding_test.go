package geojson

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Rounding at the write (vnx/round.go), from this format's side.
//
// ⚠ THE GOLDEN CANNOT WITNESS THIS. testdata/library.golden.geojson is built
// from a fixture whose coordinates are already exact six-place values, so
// rounding changed it by zero bytes. That is not evidence that rounding
// happens — it is evidence that the fixture never asked. This file asks.

func roundEle(v float64) *float64 { return &v }

// noisy carries the digits the ruling is about: a coordinate at full binary64
// precision, and an elevation with a computed tail.
func noisyDoc() *vnx.Document {
	return &vnx.Document{
		Waypoints: []vnx.Waypoint{{
			Name: "noisy",
			At: vnx.Position{
				Lon: -3.6712345678901235,
				Lat: 57.11812345678901,
				Ele: roundEle(328.06075289469285),
			},
		}},
	}
}

func TestCoordinatesAreRoundedAtTheWrite(t *testing.T) {
	var b bytes.Buffer
	if _, err := Write(&b, noisyDoc()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	const want = `"coordinates":[-3.671235,57.118123,328.1]`
	if !strings.Contains(out, want) {
		t.Fatalf("output does not contain %s:\n%s", want, out)
	}
	// RED PROOF: with the rounding removed from emit.position, the string
	// below is what the writer emits instead. Its ABSENCE is what this test
	// is really asserting.
	if strings.Contains(out, "-3.6712345678901235") || strings.Contains(out, "328.06075289469285") {
		t.Fatalf("full precision reached the file:\n%s", out)
	}
}

// The Document is the source of truth and keeps full precision; only the
// encoder rounds. A writer that rounded the model would corrupt whatever the
// caller does with the document next.
func TestWritingDoesNotRoundTheDocument(t *testing.T) {
	d := noisyDoc()
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	if d.Waypoints[0].At.Lon != -3.6712345678901235 || *d.Waypoints[0].At.Ele != 328.06075289469285 {
		t.Fatalf("the document was rounded in place: %v", d.Waypoints[0].At)
	}
}

// ⚠ HAZARD 5. An unknown elevation stays a TWO-element position. Rounding must
// not invent a third ordinate, which would put the point at sea level.
func TestRoundingLeavesAnUnknownElevationAbsent(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "no elevation",
		At:   vnx.Position{Lon: -3.6712345678901235, Lat: 57.11812345678901},
	}}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	const want = `"coordinates":[-3.671235,57.118123]`
	if !strings.Contains(b.String(), want) {
		t.Fatalf("output does not contain %s:\n%s", want, b.String())
	}
}

// ⚠ HAZARD 3. A ring's first and last position must stay byte-identical after
// rounding, or the Polygon is not closed. Rounding is deterministic so this
// holds, but the RFC does not care why — assert it.
//
// ⚠ HAZARD 2. Rounding cannot drop a ring below RFC 7946's four positions:
// nothing here removes a position, only respells one. A ring small enough to
// collapse to a single repeated point is still four closed positions, which is
// what the RFC asks for; the validator agrees, and its degenerate-ring branch
// is what makes that true rather than luck.
func TestRoundingKeepsARingClosedAndValid(t *testing.T) {
	for _, c := range []struct {
		name string
		ring []vnx.Position
	}{
		{"an ordinary ring at full precision", []vnx.Position{
			{Lon: -3.6712345678901235, Lat: 57.11812345678901},
			{Lon: -3.6702345678901235, Lat: 57.11812345678901},
			{Lon: -3.6702345678901235, Lat: 57.11912345678901},
			{Lon: -3.6712345678901235, Lat: 57.11812345678901},
		}},
		// Every vertex inside one 11 cm cell: the whole ring collapses to one
		// repeated position.
		{"a ring smaller than the grid", []vnx.Position{
			{Lon: -3.6712341, Lat: 57.1181231},
			{Lon: -3.6712342, Lat: 57.1181231},
			{Lon: -3.6712342, Lat: 57.1181232},
			{Lon: -3.6712341, Lat: 57.1181231},
		}},
		// A sliver whose winding SIGN flips when its vertices snap to the
		// grid: counterclockwise at full precision, clockwise on the grid.
		// signedArea is computed on the rounded ring for exactly this case.
		{"a sliver whose winding flips on the grid", []vnx.Position{
			{Lon: 0, Lat: 0},
			{Lon: 0.000003, Lat: 0.000001},
			{Lon: 0.000001, Lat: 0.0000004},
			{Lon: 0, Lat: 0},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := &vnx.Document{Areas: []vnx.Area{{Name: "a", Polygon: c.ring}}}
			var b bytes.Buffer
			rep, err := Write(&b, d)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Emitted != 1 {
				t.Fatalf("emitted %d, want the polygon written; diagnostics %v", rep.Emitted, rep.Diagnostics)
			}
			if problems := validate(b.Bytes()); len(problems) > 0 {
				t.Fatalf("rounded output is not valid RFC 7946:\n%s\n%s",
					strings.Join(problems, "\n"), b.String())
			}
			// Closure, asserted on the BYTES rather than on the floats.
			coords := b.String()
			i := strings.Index(coords, `"coordinates":[[`)
			if i < 0 {
				t.Fatalf("no polygon in:\n%s", coords)
			}
			ring := coords[i+len(`"coordinates":[[`):]
			ring = ring[:strings.Index(ring, "]]")]
			parts := strings.Split(ring, "],[")
			for i := range parts {
				parts[i] = strings.Trim(parts[i], "[]")
			}
			if len(parts) < 4 {
				t.Fatalf("ring lost positions: %s", ring)
			}
			if parts[0] != parts[len(parts)-1] {
				t.Fatalf("ring is not closed after rounding: first %q, last %q", parts[0], parts[len(parts)-1])
			}
		})
	}
}
