package vnx

import "math"

// Cutting an arc at a chain's endpoints. ONE implementation, two callers —
// kml.chainLine and geojson.traverse.
//
// ⚠ A CHAIN IS NOT A LIST OF WHOLE ARCS. Each of its links traverses one arc
// BETWEEN TWO ENDPOINTS, and an endpoint is either a node — the arc's own end —
// or an inline position part-way along it ([Endpoint]). Emitting the whole arc
// for a link that uses part of it would draw the chain running past where the
// user stopped.
//
// ⚠ IT LIVES HERE BECAUSE TWO WRITERS MUST DRAW THE SAME LINE. It used to live
// in kml only, with geojson carrying a second answer that SNAPPED an inline
// endpoint to the nearest recorded pip instead of cutting the segment. One
// library then exported one chain as two different polylines, differing by up
// to half a segment at each end — the four-writers drift round.go's header
// warns about, made real. Following DocumentIsEmpty's discipline (a rule two
// callers must obey belongs in pkg/vnx), the answer is here and there is only
// one of it.

// cut is a point ON a polyline: the index of the segment it lies on, and how
// far along that segment it sits (0..1).
type cut struct {
	seg int
	t   float64
}

// before reports whether c comes earlier along the polyline than o.
func (c cut) before(o cut) bool {
	if c.seg != o.seg {
		return c.seg < o.seg
	}
	return c.t < o.t
}

// project finds the point on pips closest to p.
//
// ⚠ IT IS USED FOR NODE ENDPOINTS TOO, not only inline ones, and that is what
// keeps this to one code path. An arc's pips are its COMPLETE point list and
// must meet its nodes (checkArcMeetsNodes), so a node endpoint projects to t=0
// of the first segment or t=1 of the last — exactly the whole-arc answer,
// arrived at without a second branch that could disagree with this one.
func project(pips []Position, p Position) cut {
	best := cut{}
	bestDist := math.Inf(1)
	for i := 0; i+1 < len(pips); i++ {
		t, d := nearestOnSegment(p, pips[i], pips[i+1])
		if d < bestDist {
			bestDist, best = d, cut{seg: i, t: t}
		}
	}
	return best
}

// nearestOnSegment projects p onto the segment ab in a local planar frame and
// returns how far along it lands (0..1) and how far away it is, in metres.
// Over the length of one arc segment the flat-earth error is far below
// anything this decides, so a geodesic projection would buy nothing.
func nearestOnSegment(p, a, b Position) (t, metres float64) {
	const metresPerDegLat = 111132.0
	cosLat := math.Cos(a.Lat * math.Pi / 180)
	px := (p.Lon - a.Lon) * metresPerDegLat * cosLat
	py := (p.Lat - a.Lat) * metresPerDegLat
	bx := (b.Lon - a.Lon) * metresPerDegLat * cosLat
	by := (b.Lat - a.Lat) * metresPerDegLat
	lenSq := bx*bx + by*by
	if lenSq == 0 {
		return 0, math.Hypot(px, py)
	}
	t = (px*bx + py*by) / lenSq
	t = math.Max(0, math.Min(1, t))
	return t, math.Hypot(px-t*bx, py-t*by)
}

// at is the position a cut denotes.
//
// ⚠ AN INTERPOLATED POINT CARRIES NO ELEVATION AND NO TIME, and that is the
// same ruling as everywhere else in this format: absent is absent. We know
// where the walker turned round; we do not know how high it was there, and
// writing a number we derived would be indistinguishable, downstream, from one
// that was measured. A cut landing exactly ON a pip copies that pip whole, so
// the ordinary case — every node endpoint, and every inline endpoint that sits
// on a recorded point — keeps its elevation.
func at(pips []Position, c cut) Position {
	a, b := pips[c.seg], pips[c.seg+1]
	switch c.t {
	case 0:
		return a
	case 1:
		return b
	}
	return Position{
		Lon: a.Lon + (b.Lon-a.Lon)*c.t,
		Lat: a.Lat + (b.Lat-a.Lat)*c.t,
	}
}

// between is the piece of pips running from a to b, in that order. When b comes
// first along the polyline the piece is reversed, which is how a chain walking
// an arc backwards gets drawn the way it is walked.
func between(pips []Position, a, b cut) []Position {
	if b.before(a) {
		out := between(pips, b, a)
		for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
			out[l], out[r] = out[r], out[l]
		}
		return out
	}
	out := []Position{at(pips, a)}
	for i := a.seg + 1; i <= b.seg; i++ {
		out = append(out, pips[i])
	}
	out = append(out, at(pips, b))
	return out
}

// SubPolyline is the piece of pips a link walks: the run from the point on the
// polyline nearest from to the point nearest to, in that order, reversed when
// the walk runs backwards along pips.
//
// ⚠ AN ENDPOINT LANDING MID-SEGMENT IS INTERPOLATED, NOT SNAPPED to the nearest
// recorded pip. The exact cut point is what the traversal actually covers;
// snapping moves the end by up to half a segment and reports a length the
// traversal does not have. Nor does interpolating cost the format's identity
// rule anything: the projection is a pure function of the endpoint and the
// pips, so two traversals sharing one turnaround endpoint over one run of pips
// both emit the SAME interpolated coordinate.
//
// The doc deliberately talks about pips and traversals rather than the model's
// own nouns: this is a geometry helper over a list of positions, and the two
// writers that call it are the ones that know what the run of pips belongs to.
//
// ⚠ IT MUST RUN BEFORE ROUNDING, like every other geometry step (round.go).
// The cut is taken at full precision and the writer rounds what comes out.
//
// A polyline with no segment to cut has no piece to return: a single pip is
// returned as itself — that pip is the only answer to "the point nearest
// anything" — and an empty pip list yields nil.
func SubPolyline(pips []Position, from, to Position) []Position {
	switch len(pips) {
	case 0:
		return nil
	case 1:
		return []Position{pips[0]}
	}
	return between(pips, project(pips, from), project(pips, to))
}
