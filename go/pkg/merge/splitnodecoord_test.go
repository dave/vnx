package merge

import (
	"math"
	"testing"
)

// Coordinates are copied, never recomputed: a value that merely round-trips
// through a formula defeats that silently.
//
// When a split lands exactly on an existing walk vertex the node must BE that
// vertex, bit for bit. The old code computed every case as
// `a + (b-a)*t`, so at t == 1 it produced `a + (b-a)`, which is not `b`
// whenever `b-a` is inexact.
//
// MUTATION: replace splitNodeCoord's body with the old one-liner —
//
//	return a.lat + (b.lat-a.lat)*t, a.lon + (b.lon-a.lon)*t, interpEle(a.ele, b.ele, t)
//
// and the t == 1 straddle-zero subtest goes red on lon.
//
// The pairs below are not arbitrary. Sterbenz's lemma makes `b-a` exact for
// same-sign operands within a factor of two, which is every ordinary trail
// arc — so the defect is invisible there and stayed latent. It bites where
// the operands straddle zero (the equator, or the prime meridian) or differ by
// more than 2x. Both cases are pinned.
func TestSplitNodeCoord_EndpointsAreCopiedNotRecomputed(t *testing.T) {
	bits := math.Float64bits

	// Straddles the prime meridian. Verified: a+(b-a) != b for this lon pair.
	straddleA := walkPoint{Lat: 51.4779, Lon: -0.0002605008472661031, Ele: 12}
	straddleB := walkPoint{Lat: 51.4782, Lon: 0.003880056410940022, Ele: 47}

	t.Run("t==1 copies b even when the recompute would not", func(t *testing.T) {
		// Guard: if this ever stops holding, the fixture has stopped
		// exercising the defect and the test below proves nothing.
		if straddleA.Lon+(straddleB.Lon-straddleA.Lon)*1.0 == straddleB.Lon {
			t.Fatalf("fixture no longer exercises the defect: a+(b-a) == b for lon %v/%v — "+
				"pick operands where the subtraction is inexact, or this test is vacuous",
				straddleA.Lon, straddleB.Lon)
		}
		lat, lon, ele := splitNodeCoord(straddleA, straddleB, 1)
		if bits(lon) != bits(straddleB.Lon) {
			t.Errorf("lon = %v (bits %#x), want the vertex's own %v (bits %#x) — the node must BE the vertex, not a value near it",
				lon, bits(lon), straddleB.Lon, bits(straddleB.Lon))
		}
		if bits(lat) != bits(straddleB.Lat) {
			t.Errorf("lat = %v, want %v", lat, straddleB.Lat)
		}
		if ele == nil || *ele != straddleB.Ele {
			t.Errorf("ele = %v, want %v", ele, straddleB.Ele)
		}
	})

	t.Run("t==0 copies a", func(t *testing.T) {
		lat, lon, ele := splitNodeCoord(straddleA, straddleB, 0)
		if bits(lat) != bits(straddleA.Lat) || bits(lon) != bits(straddleA.Lon) {
			t.Errorf("got (%v, %v), want (%v, %v)", lat, lon, straddleA.Lat, straddleA.Lon)
		}
		if ele == nil || *ele != straddleA.Ele {
			t.Errorf("ele = %v, want %v", ele, straddleA.Ele)
		}
	})

	t.Run("an endpoint keeps its OWN elevation when the far end is unknown", func(t *testing.T) {
		// interpEle returns nil if EITHER end is unknown. A node sitting
		// exactly on a vertex with a known elevation must not inherit the
		// other end's ignorance.
		known := walkPoint{Lat: 51.4779, Lon: -0.0002605008472661031, Ele: 12}
		unknown := walkPoint{Lat: 51.4782, Lon: 0.003880056410940022, Ele: math.NaN()}
		if _, _, ele := splitNodeCoord(known, unknown, 0); ele == nil || *ele != 12 {
			t.Errorf("t==0 ele = %v, want 12 — the node is AT the known vertex", ele)
		}
		if _, _, ele := splitNodeCoord(unknown, known, 1); ele == nil || *ele != 12 {
			t.Errorf("t==1 ele = %v, want 12 — the node is AT the known vertex", ele)
		}
		// Unknown at the node's own end stays unknown.
		if _, _, ele := splitNodeCoord(unknown, known, 0); ele != nil {
			t.Errorf("t==0 ele = %v, want nil — the vertex's own elevation is unknown", *ele)
		}
	})

	t.Run("the strict interior still interpolates", func(t *testing.T) {
		lat, lon, ele := splitNodeCoord(straddleA, straddleB, 0.25)
		wantLat := straddleA.Lat + (straddleB.Lat-straddleA.Lat)*0.25
		wantLon := straddleA.Lon + (straddleB.Lon-straddleA.Lon)*0.25
		if bits(lat) != bits(wantLat) || bits(lon) != bits(wantLon) {
			t.Errorf("interior split moved: got (%v, %v), want (%v, %v)", lat, lon, wantLat, wantLon)
		}
		if ele == nil || *ele != interpEleValue(straddleA.Ele, straddleB.Ele, 0.25) {
			t.Errorf("interior ele = %v, want the interpolated value", ele)
		}
	})
}

// interpEleValue is the scalar form of interpEle, for the assertion above.
func interpEleValue(a, b, t float64) float64 { return a + (b-a)*t }
