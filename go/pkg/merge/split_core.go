package merge

import (
	"math"
)

// PolylineSplit is the result of SplitPolylineAtFraction: the lat/lon/
// ele to assign the new node, and where the two halves' interior pips sit in
// the parent's polyline. It holds no pips: the caller slices the halves from
// the parent it already holds, once, in its own pip type.
type PolylineSplit struct {
	Lat, Lon float64
	Ele      *float64 // nil iff both endpoints around the split have unknown elevation

	// ConsumedParentVertex says the split landed exactly ON an interior pip of
	// the parent, so that pip BECAME the node and is in neither half — as
	// against the ordinary case, where the node is a fresh point interpolated
	// between two pips that both survive.
	//
	// KNOWABLE ONLY HERE. Afterwards the two halves look the same either way:
	// one shared coordinate at the join, no row saying whether it used to be a
	// point of the parent. Anything reconstructing the parent needs the
	// difference, because undoing the split must KEEP a consumed pip and DROP a
	// minted one — and getting it wrong deletes or invents a real corner.
	//
	// It is recorded rather than re-derived because every way of deriving it
	// afterwards is wrong in a way that does not announce itself. Asking "does
	// the join lie on the chord between its neighbours" needs a threshold, and
	// the two populations are only eleven orders of magnitude apart. Asking "is
	// the join one of the vertices the import was given" fails whenever the
	// parent itself holds a vertex no source had — which is every parent that
	// followed earlier canon; a randomised test once lost a 3.02 m corner that
	// way and then reported a perfectly good committed point as off-canon.
	ConsumedParentVertex bool

	// PriorEnd and NextStart locate the halves in the parent's polyline: the
	// prior half's interior is polyline[:PriorEnd] and the next half's is
	// polyline[NextStart:]. Always 0 <= PriorEnd <= NextStart <= len(polyline).
	// A zero-length walk gives PriorEnd = 0 and NextStart = len(polyline):
	// both halves have empty interiors.
	PriorEnd, NextStart int
}

// SplitPolylineAtFraction walks [start, polyline..., end], measures
// cumulative haversine distance, finds the segment within the walk
// that contains `fraction * totalDistance`, and linearly interpolates
// the new node's coordinates within that arc.
//
// The walk has len(polyline)+1 small arcs. Pips before the split
// position become the prior half's interior; pips after become the
// next half's. If the split lands exactly on a pip (rare in practice
// but precise floats make it possible), that pip becomes the new node
// and is in neither half. The halves are returned as indices
// (PriorEnd, NextStart), not copies, and the polyline is read through
// [PipReader], so a caller passes its own pips without converting them.
//
// Elevation interpolation uses the same NaN-as-unknown convention as
// the stats helpers: if either side of the split has unknown
// elevation, the interpolated ele is also unknown (nil pointer).
func SplitPolylineAtFraction[P PipReader](start, end Node, polyline []P, fraction float64) PolylineSplit {
	// Build the full walk in order so the loop below indexes uniformly.
	walk := make([]WalkPoint, 0, len(polyline)+2)
	walk = append(walk, WalkPoint{Lat: start.Lat, Lon: start.Lon, Ele: NodeEle(start)})
	for _, p := range polyline {
		lat, lon, ele := p.LatLonEle()
		walk = append(walk, WalkPoint{Lat: lat, Lon: lon, Ele: ele})
	}
	walk = append(walk, WalkPoint{Lat: end.Lat, Lon: end.Lon, Ele: NodeEle(end)})

	// Cumulative distance up to walk[i].
	cum := make([]float64, len(walk))
	for i := 1; i < len(walk); i++ {
		cum[i] = cum[i-1] + HaversineMetres(
			walk[i-1].Lat, walk[i-1].Lon,
			walk[i].Lat, walk[i].Lon,
		)
	}
	total := cum[len(cum)-1]

	// Degenerate case: a zero-length walk (start == end, no interior).
	// Place the node exactly at start; both halves have empty interiors.
	if total == 0 {
		return PolylineSplit{
			Lat:       start.Lat,
			Lon:       start.Lon,
			Ele:       nodeEleAsPtr(start),
			PriorEnd:  0,
			NextStart: len(polyline),
		}
	}

	target := fraction * total

	// Find the first walk index i such that cum[i] >= target. The
	// split lies on the segment walk[i-1] -> walk[i].
	i := 1
	for i < len(walk) && cum[i] < target {
		i++
	}
	if i >= len(walk) {
		i = len(walk) - 1 // belt-and-braces; fraction in (0,1) exclusive
	}

	prevCum := cum[i-1]
	segLen := cum[i] - prevCum
	var t float64 // 0 = at walk[i-1], 1 = at walk[i]
	if segLen > 0 {
		t = (target - prevCum) / segLen
	}
	a, b := walk[i-1], walk[i]
	lat, lon, ele := SplitNodeCoord(a, b, t)
	// ⚠ A STRICT-INTERIOR t THAT ROUNDS ONTO A VERTEX IS AN ON-PIP SPLIT, AND
	// MUST BE RECORDED AS ONE. The interpolation can land bit-for-bit on
	// walk[i-1] or walk[i] with 0 < t < 1 (a target within an ulp of a vertex
	// measure). Treating that as strict-interior keeps the vertex in one half
	// as a pip sitting exactly on the new node — a zero-length step — while
	// ConsumedParentVertex says "minted, not consumed". Every reader of that
	// flag (the split provenance, the T-junction split, the align step) then
	// drops a vertex the store still holds, or keeps one it does not.
	//
	// Measured: dropping the coincident pip at the WRITER instead left
	// ConsumedParentVertex=false while the vertex was gone from both halves,
	// and a replay of the split lost a real parent vertex (65 -> 64).
	// The record and the bytes must be decided in ONE place, and this is it.
	// Float equality, no tolerance: the only way to be here is that the
	// arithmetic produced the vertex's own bits.
	switch {
	case lat == a.Lat && lon == a.Lon:
		t = 0
	case lat == b.Lat && lon == b.Lon:
		t = 1
	}
	// ⚠ RE-DERIVE AFTER THE CLAMP, OR THE ELEVATION IS STILL THE INTERPOLATED
	// ONE. The clamp above rewrites t, but `ele` was already computed from the
	// PRE-clamp t, so it came back from interpEle rather than from the vertex.
	// That breaks the rule SplitNodeCoord's doc-comment states outright — AT
	// THE ENDPOINTS THE COORDINATE IS COPIED, NEVER RECOMPUTED — and it is
	// visible, not cosmetic: interpEle returns nil when EITHER end is unknown,
	// so a node landing bit-exactly on a vertex whose elevation IS known came
	// back with no elevation whenever the other end of the segment had none.
	// Re-running SplitNodeCoord at the clamped t is idempotent when t did not
	// move, so it is unconditional.
	lat, lon, ele = SplitNodeCoord(a, b, t)

	// Build the two interior slices.
	//
	// Walk indices map to polyline indices via `pIdx = wIdx - 1`. The
	// interior pips are walk indices 1..len(walk)-2, i.e. polyline
	// indices 0..len(polyline)-1. The split lies on the walk arc
	// walk[i-1] → walk[i] at parameter t.
	//
	// In the strict-interior case (0 < t < 1), walk[i-1] is the last
	// point at-or-before the split and walk[i] is the first point
	// at-or-after, so:
	//
	//   prior interior = walk indices [1, i-1] inclusive
	//                  = polyline[0 : i-1]
	//   next  interior = walk indices [i, len(walk)-2] inclusive
	//                  = polyline[i-1 :]
	//
	// In the on-pip cases (t == 0 or t == 1), the matching walk point
	// IS the new node and is excluded from both halves. Specifically:
	//
	//   t == 0: split is at walk[i-1]; that becomes the new node.
	//           prior interior = polyline[0 : i-2]
	//           next  interior = polyline[i-1 :]
	//
	//   t == 1: split is at walk[i]; that becomes the new node.
	//           prior interior = polyline[0 : i-1]
	//           next  interior = polyline[i :]
	//
	// (When the matching walk point is start or end rather than an
	// interior pip, the slicing maths still works out: pPriorEnd
	// clamps to 0 or pNextStart clamps to len(polyline).)
	// consumedAt is the walk index the split landed exactly on, or -1. Interior
	// pips only: walk[0] is the parent's start node and walk[len-1] its end, and
	// a split that lands on either produces a zero-length half rather than
	// consuming anything (callers refuse the fractions that would).
	consumedAt := -1
	pPriorEnd, pNextStart := i-1, i-1
	switch t {
	case 0:
		pPriorEnd = i - 2
		consumedAt = i - 1
	case 1:
		pNextStart = i
		consumedAt = i
	}
	consumed := consumedAt >= 1 && consumedAt <= len(walk)-2
	if pPriorEnd < 0 {
		pPriorEnd = 0
	}
	if pPriorEnd > len(polyline) {
		pPriorEnd = len(polyline)
	}
	if pNextStart < 0 {
		pNextStart = 0
	}
	if pNextStart > len(polyline) {
		pNextStart = len(polyline)
	}

	return PolylineSplit{
		Lat:                  lat,
		Lon:                  lon,
		Ele:                  ele,
		ConsumedParentVertex: consumed,
		PriorEnd:             pPriorEnd,
		NextStart:            pNextStart,
	}
}

// WalkPoint is a tiny ad-hoc struct used by SplitPolylineAtFraction so
// the loop indexes start, every interior pip, and end uniformly.
type WalkPoint struct {
	Lat, Lon, Ele float64
}

// SplitNodeCoord returns the coordinate of the node a split mints on the
// walk arc a → b at parameter t.
//
// AT THE ENDPOINTS THE COORDINATE IS COPIED, NEVER RECOMPUTED: a value that
// merely round-trips through a formula defeats an identity test silently.
// An earlier version broke this rule: it computed every case as
//
//	lat := a.lat + (b.lat-a.lat)*t
//
// which at t == 1 is `a + (b-a)`, and that is NOT `b` in IEEE754 whenever
// `b-a` is inexact. The node then landed NEAR the vertex it is supposed to
// land ON — in committed geometry.
//
// ── WHEN IT ACTUALLY DIFFERS, MEASURED ─────────────────────────────────────
// Rarely, which is why it has survived. Sterbenz's lemma makes `b-a` EXACT
// for same-sign operands within a factor of two of each other, and then
// `a + (b-a)` is exactly `b`. A typical trail arc is well inside that,
// so the old expression was already returning the right answer. Over 400k
// random same-sign latitude pairs 0.01° apart: zero differences.
//
// It breaks where Sterbenz does not apply:
//   - vertices STRADDLING ZERO — the equator, or the prime meridian
//     (Greenwich): ~49% of such pairs
//     differ, e.g. a=-0.0002605008472661031, b=0.003880056410940022 gives
//     0.0038800564109400214, off by 4.3e-19.
//   - same-sign operands more than 2× apart: ~0.6% of pairs, ~1.1e-16.
//
// So this was a LATENT defect, not one the test data was hitting. It is
// fixed because "no trail in the fixture set crosses zero" is a property of
// the fixtures, not a guarantee of the code, and the next import may not
// share it. t == 0 was already exact (`a + 0`); it is routed through the
// copy anyway so the invariant is structural rather than an accident of
// floating point.
//
// Elevation is copied at the endpoints for the same reason, and that is a
// deliberate behaviour change: interpEle returns nil when EITHER end is
// unknown, so a node sitting exactly on a vertex with a known elevation used
// to come back unknown if the OTHER end was unknown. It is the vertex's
// elevation; discarding it was wrong.
func SplitNodeCoord(a, b WalkPoint, t float64) (lat, lon float64, ele *float64) {
	switch t {
	case 0:
		return a.Lat, a.Lon, eleAsPtr(a.Ele)
	case 1:
		return b.Lat, b.Lon, eleAsPtr(b.Ele)
	default:
		return a.Lat + (b.Lat-a.Lat)*t,
			a.Lon + (b.Lon-a.Lon)*t,
			interpEle(a.Ele, b.Ele, t)
	}
}

// eleAsPtr returns v as a pointer, or nil for the NaN "unknown" sentinel.
func eleAsPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	e := v
	return &e
}

// interpEle linearly interpolates elevation between a and b at t ∈
// [0,1]. NaN sentinel handling mirrors ElevDelta: if either side is
// unknown the result is unknown (nil pointer).
func interpEle(a, b, t float64) *float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return nil
	}
	v := a + (b-a)*t
	return &v
}

// nodeEleAsPtr returns the node's elevation as a pointer or nil if the
// node has no elevation. Used for the degenerate-walk fallback.
func nodeEleAsPtr(n Node) *float64 {
	if n.Ele == nil {
		return nil
	}
	v := *n.Ele
	return &v
}
