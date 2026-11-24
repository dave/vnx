package merge

// A GPS track that stands still repeats its coordinate exactly, and a repeat
// is a source edge of zero length. Such an edge carries no import measure, so
// every CorridorPiece minted on it has ImportMeasureAtLo == ImportMeasureAtHi: state
// the solver can switch through but can never travel along.
//
// Found on a real recording with 1,248 consecutive
// duplicate points, 460 of them on ONE coordinate. One leg minted 58,853
// pieces, 42,851 of them zero-width, piling 32,473 states onto a single
// layer — 546 M of that import's 610 M Dijkstra relaxations, on one leg,
// entirely on state that cannot carry the line.

import (
	"testing"
)

// TestCorridorIntervals_AStationarySourceMintsNoPieces is the red-first
// reproduction: the SAME leg, run twice, once with a 200-sample stall in the
// middle and once without. The stall adds no geometry, so it must add no
// pieces.
//
// It is a DIFFERENCE and not an absolute count on purpose. Zero-width pieces
// have a second, unrelated and entirely legitimate source — a source edge
// that grazes the d-band at exactly one t gives a regime whose interval is a
// point — and this fixture, being perfectly straight and parallel, produces
// over a hundred of them either way. An absolute "no zero-width pieces"
// assertion therefore fails on correct code, which is how it was caught:
// written that way first, it came back red with the fix in place.
//
// It asserts on the PIECES, not on the import time, deliberately — a timing
// assertion would pass or fail on how busy the machine is, whereas "how much
// state cannot carry the line" is a property of the geometry and answers the
// same question exactly.
func TestCorridorIntervals_AStationarySourceMintsNoPieces(t *testing.T) {
	const R = CorridorRadiusM

	// Candidate: a straight 200 m trail, split into many short edges — the
	// real-data shape, and what makes each duplicate expensive, since a
	// zero-length source edge mints one piece per canon edge within R.
	pc := prepOne(t, straightLegX(41, 200, 0)...)

	// Source: the same straight line, running 2 m off the candidate. `stalls`
	// samples of standing still are inserted at the halfway point.
	src := func(stalls int) []Point {
		var out []Point
		for i := 0; i <= 20; i++ {
			out = append(out, mPt(float64(i)*5, 2))
		}
		for i := 0; i < stalls; i++ {
			out = append(out, mPt(100, 2))
		}
		for i := 21; i <= 40; i++ {
			out = append(out, mPt(float64(i)*5, 2))
		}
		return out
	}

	count := func(pts []Point) (total, zero int) {
		t.Helper()
		ivs, err := corridorIntervalsForLeg(t.Context(), pts, []*PreppedCandidate{pc}, R)
		if err != nil {
			t.Fatalf("corridorIntervalsForLeg: %v", err)
		}
		for _, iv := range ivs {
			for _, p := range iv.Pieces {
				total++
				if p.ImportMeasureAtHi <= p.ImportMeasureAtLo {
					zero++
				}
			}
		}
		return total, zero
	}

	baseTotal, baseZero := count(src(0))
	stallTotal, stallZero := count(src(200))

	// Liveness: a fixture whose candidate never came within R would produce
	// no pieces at all and satisfy every comparison below vacuously.
	if baseTotal == 0 {
		t.Fatalf("no pieces at all — the fixture is not exercising the corridor")
	}
	if stallTotal != baseTotal {
		t.Errorf("200 stalled samples added %d pieces (%d -> %d); a zero-length source "+
			"edge carries no import measure and must mint none",
			stallTotal-baseTotal, baseTotal, stallTotal)
	}
	if stallZero != baseZero {
		t.Errorf("200 stalled samples added %d zero-width pieces (%d -> %d)",
			stallZero-baseZero, baseZero, stallZero)
	}
	t.Logf("no stall: %d pieces (%d zero-width); 200-sample stall: %d pieces (%d zero-width)",
		baseTotal, baseZero, stallTotal, stallZero)
}
