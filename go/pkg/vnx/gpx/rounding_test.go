package gpx

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Rounding at the write (vnx/round.go), from this format's side.
//
// ⚠ THE GOLDEN CANNOT WITNESS THIS. testdata/library.golden.gpx is built from
// `leg`, which makes its pips out of integer microdegrees on purpose
// (gpx_test.go) — six places exactly — and integer elevations. Rounding
// changed the golden by zero bytes, which says the fixture never asked the
// question, not that the answer is right.

func eleOf(v float64) *float64 { return &v }

func TestCoordinatesAndElevationAreRoundedAtTheWrite(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "noisy",
		At: vnx.Position{
			Lon: -3.6712345678901235,
			Lat: 57.11812345678901,
			Ele: eleOf(328.06075289469285),
		},
	}}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`lat="57.118123"`, `lon="-3.671235"`, `<ele>328.1</ele>`} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %s:\n%s", want, out)
		}
	}
	// RED PROOF: these are what the writer emits with the rounding removed.
	for _, never := range []string{"57.11812345678901", "-3.6712345678901235", "328.06075289469285"} {
		if strings.Contains(out, never) {
			t.Errorf("full precision reached the file (%s):\n%s", never, out)
		}
	}
}

// A track point takes the same path as a waypoint, and a route's points take a
// third. All three must round, or one format spells one library two ways.
func TestTrackAndRoutePointsRoundToo(t *testing.T) {
	p := vnx.Position{Lon: -3.6712345678901235, Lat: 57.11812345678901, Ele: eleOf(328.06075289469285)}
	q := vnx.Position{Lon: -3.6702345678901235, Lat: 57.11912345678901}
	d := &vnx.Document{
		Tracks: []vnx.Track{{Name: "t", Segments: []vnx.Segment{{Pips: []vnx.Position{p, q}}}}},
		Routes: []vnx.Route{{Name: "r", Arcs: []vnx.Arc{{Pips: []vnx.Position{p, q}}}}},
	}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if n := strings.Count(out, `lat="57.118123"`); n != 2 {
		t.Errorf("rounded latitude appears %d times, want 2 (one track point, one route point):\n%s", n, out)
	}
	if strings.Contains(out, "57.11812345678901") {
		t.Errorf("full precision reached the file:\n%s", out)
	}
}

// ⚠ HAZARD 5. An unknown elevation must not become <ele>0.0</ele>. The nil
// test comes before the rounding, so absence survives.
func TestRoundingNeverInventsAnElevation(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "no elevation",
		At:   vnx.Position{Lon: -3.6712345678901235, Lat: 57.11812345678901},
	}}}
	var b bytes.Buffer
	if _, err := Write(&b, d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<ele>") {
		t.Fatalf("an <ele> was invented for an unknown elevation:\n%s", b.String())
	}
}

// ⚠ HAZARD 1. A route's shared arc endpoint is deduped BY FULL VALUE
// (routePips), and that comparison must keep seeing full values: two readings
// 5 cm apart are two readings, and rounding them first would silently merge
// them into one. Here the two arcs meet at positions that are distinct at full
// precision and identical once rounded — the file therefore carries the
// repeated coordinate, which is what a zero-length step looks like and is
// correct: the document holds two points.
func TestTheArcBoundaryDedupStillComparesFullValues(t *testing.T) {
	// ⚠ THESE COORDINATES MUST STILL COLLIDE AT SIX PLACES, and checking that
	// is part of running this test. They were -3.6712345 and -3.67123452 until
	// 2026-09-09, when rounding moved to half-to-even (§4.1) and the first
	// began rounding DOWN to -3.671234 while the second stayed at -3.671235.
	// The pair stopped colliding, so the test stopped setting up its own
	// scenario and passed for the wrong reason — it was asserting that a dedup
	// had not merged two points it could no longer have merged.
	a := vnx.Position{Lon: -3.6712341, Lat: 57.1181234}
	b2 := vnx.Position{Lon: -3.67123412, Lat: 57.1181234} // same to six places, not equal
	d := &vnx.Document{Routes: []vnx.Route{{Name: "r", Arcs: []vnx.Arc{
		{Pips: []vnx.Position{{Lon: -3.67, Lat: 57.11}, a}},
		{Pips: []vnx.Position{b2, {Lon: -3.68, Lat: 57.12}}},
	}}}}
	var buf bytes.Buffer
	if _, err := Write(&buf, d); err != nil {
		t.Fatal(err)
	}
	// The premise first: if these two stop rounding to one value, the rest of
	// this test proves nothing.
	if vnx.RoundCoordinate(a.Lon) != vnx.RoundCoordinate(b2.Lon) {
		t.Fatalf("the two coordinates no longer collide at six places (%v and %v); "+
			"this test cannot see the property it guards until they do",
			vnx.RoundCoordinate(a.Lon), vnx.RoundCoordinate(b2.Lon))
	}
	if n := strings.Count(buf.String(), `lon="-3.671234"`); n != 2 {
		t.Fatalf("the shared coordinate appears %d times, want 2 — the dedup compared ROUNDED values "+
			"and merged two distinct readings:\n%s", n, buf.String())
	}
}
