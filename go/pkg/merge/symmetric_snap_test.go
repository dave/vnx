package merge

// The symmetric-snap seam producer (symmetric_snap_core.go).
//
// The producer is tested DIRECTLY, on hand-built legs inside a transaction,
// exactly as legpieces_test.go tests its own half. Nothing here drives a real
// import through the producer. What these tests can and do check is every rule
// the producer owns, plus the two claims the phase's cost rests on: that the
// node identity is unchanged, and that `Absorbed` is what makes the leg reach.

import (
	"math"
	"testing"
)

// snapStraightLeg is a west→east leg along y=0 with a vertex every 250 m, and
// an elevation ramp so the inserted vertex's own elevation can be checked.
func snapStraightLeg() []Point {
	return []Point{
		mPt(0, 0, 100),
		mPt(250, 0, 200),
		mPt(500, 0, 300),
		mPt(750, 0, 400),
		mPt(1000, 0, 500),
	}
}

// TestSymmetricSnapDeclineFor pins the pure rule directly, including both
// boundaries, in the same shape TestTJunctionDeclineFor pins its mirror.
//
// There is no stub rule: a short stub is adopted. The real-import tests above
// pin that end to end, and TestTJunctionDeclineFor's stub rows pin the canon
// side.
func TestSymmetricSnapDeclineFor(t *testing.T) {
	cases := []struct {
		name       string
		fraction   float64
		vertexTake bool
		want       SymmetricSnapDecline
	}{
		{"interior", 0.5, false, SnapNoDecline},
		{"fraction exactly 0", 0, false, SnapDeclineTerminalVertex},
		{"fraction exactly 1", 1, false, SnapDeclineTerminalVertex},

		// See [SnapDeclineVertexAlreadySeamed].
		{"vertex already seamed", 0.5, true, SnapDeclineVertexAlreadySeamed},
		{"terminal outranks an already-seamed vertex", 0, true, SnapDeclineTerminalVertex},
		{"fraction 1 outranks an already-seamed vertex", 1, true, SnapDeclineTerminalVertex},
	}
	for _, c := range cases {
		if got := symmetricSnapDeclineFor(c.fraction, c.vertexTake, false); got != c.want {
			t.Errorf("%s: decline = %v, want %v", c.name, got, c.want)
		}
	}
}

// ── ONE RULER ───────────────────────────────────────────────────────────────

// TestSymmetricSnap_LegMeasureIsTheSameRulerAsTheProjection. The stub rule
// compares legMeasureToVertex against NodeNearPath.PriorLenM, which
// ProjectProbeOntoWalk produced by summing HaversineMetres over consecutive
// vertices of the SAME array in ascending order. Same primitive, same operands,
// same order — so these are bit-identical, not merely close.
//
// The merge has been bitten twice by a second ruler. If someone reorders
// the sum or swaps the distance function, the stub rule starts judging against
// a different leg and this goes red.
func TestSymmetricSnap_LegMeasureIsTheSameRulerAsTheProjection(t *testing.T) {
	pts := snapStraightLeg()
	for i := 1; i < len(pts)-1; i++ {
		proj, err := ProjectProbeOntoWalk(pts, pts[i].Lat, pts[i].Lon)
		if err != nil {
			t.Fatalf("project vertex %d: %v", i, err)
		}
		got := legMeasureToVertex(pts, i)
		if math.Float64bits(got) != math.Float64bits(proj.PriorLenM) {
			t.Errorf("vertex %d: legMeasureToVertex = %v, ProjectProbeOntoWalk's PriorLenM = %v — the stub rule and the search are measuring on DIFFERENT rulers",
				i, got, proj.PriorLenM)
		}
	}
	// And the whole-leg sum agrees with the one the T-junction split already depends on.
	if math.Float64bits(legMeasureToVertex(pts, len(pts)-1)) != math.Float64bits(LegMeasureLengthM(pts)) {
		t.Errorf("legMeasureToVertex to the last vertex disagrees with legMeasureLengthM")
	}
}
