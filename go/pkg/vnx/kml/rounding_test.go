package kml

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Rounding at the write (vnx/round.go), from this format's side.
//
// ⚠ THE GOLDEN CANNOT WITNESS THIS. testdata/library.golden.kml is written
// from a fixture whose coordinates are already exact six-place values, so
// rounding changed it by zero bytes.

func kmlEle(v float64) *float64 { return &v }

func TestCoordinatesAreRoundedAtTheWrite(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "noisy",
		At: vnx.Position{
			Lon: -3.6712345678901235,
			Lat: 57.11812345678901,
			Ele: kmlEle(328.06075289469285),
		},
	}}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	const want = "<coordinates>-3.671235,57.118123,328.1</coordinates>"
	if !strings.Contains(out, want) {
		t.Fatalf("output does not contain %s:\n%s", want, out)
	}
	// RED PROOF: this is what the writer emits with the rounding removed.
	if strings.Contains(out, "-3.6712345678901235,57.11812345678901,328.06075289469285") {
		t.Fatalf("full precision reached the file:\n%s", out)
	}
}

// ⚠ HAZARD 5. KML has no way to say "unknown elevation", so the third ordinate
// is omitted. Rounding must not fill it in — 0 is sea level, not absence.
func TestRoundingNeverInventsAThirdOrdinate(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "no elevation",
		At:   vnx.Position{Lon: -3.6712345678901235, Lat: 57.11812345678901},
	}}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	const want = "<coordinates>-3.671235,57.118123</coordinates>"
	if !strings.Contains(b.String(), want) {
		t.Fatalf("output does not contain %s:\n%s", want, b.String())
	}
}

// ⚠ A STYLE WIDTH IS NOT A COORDINATE. `num` spells both, so a rounding rule
// pushed down into it would quietly round line widths to six places as well —
// harmless today and wrong in principle, and the kind of thing that is
// discovered when someone asks for sub-pixel widths.
func TestAStyleWidthIsNotRounded(t *testing.T) {
	w := 2.3456789012
	d := &vnx.Document{Routes: []vnx.Route{route(vnx.Route{
		Name:  "r",
		Style: vnx.Style{"width": w}},
		lineArc{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}},
	)}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "<width>2.3456789012</width>") {
		t.Fatalf("the style width was rounded:\n%s", b.String())
	}
}
