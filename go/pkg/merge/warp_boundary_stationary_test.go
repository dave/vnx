package merge

// Prove-It: warpLegAtBoundaries' RIGHT walk had no s==0 guard, unlike its
// LEFT walk, so a boundary with zero measure length after it divided by zero.
//
// CRIPPLE: delete the `if s == 0` branch in the RIGHT walk and this goes red
// with out[2] = (NaN,NaN).

import (
	"math"
	"testing"
)

// TestWarpLegAtBoundaries_AWhollyStationaryLegDoesNotWarpToNaN uses the exact
// shape found in a real recording: a four-point leg piece whose every point is one
// repeated GPS coordinate, warped toward a node 15 m away. legLen is then
// exactly 0, so BOTH taper zones are zero-width.
//
// It asserts finiteness rather than a particular output coordinate on
// purpose: what was wrong is that the arithmetic was undefined, and pinning
// the resulting coordinates would freeze a policy (where a stationary leg's
// points should land) that this fix does not claim to decide.
func TestWarpLegAtBoundaries_AWhollyStationaryLegDoesNotWarpToNaN(t *testing.T) {
	const lat, lon = 56.79803405782627, -5.019873120646128
	pts := make([]Point, 4)
	for i := range pts {
		pts[i] = Point{Lat: lat, Lon: lon}
	}

	// Liveness: the fixture must actually present a zero-width zone, or a
	// green result grades nothing.
	var legLen float64
	for k := 1; k < len(pts); k++ {
		legLen += HaversineMetres(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
	}
	if legLen != 0 {
		t.Fatalf("fixture measure length is %v m, want exactly 0 — this no longer exercises a zero-width taper zone", legLen)
	}

	out, err := warpLegAtBoundaries(pts, []warpBoundaryTarget{
		{Idx: 1, Target: warpTarget{Lat: 56.798015, Lon: -5.019949, NodeID: 1}},
	})
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	for i, q := range out {
		if math.IsNaN(q.Lat) || math.IsNaN(q.Lon) || math.IsInf(q.Lat, 0) || math.IsInf(q.Lon, 0) {
			t.Errorf("point %d warped to (%v,%v) — a zero-width taper zone divided by zero", i, q.Lat, q.Lon)
		}
	}
}
