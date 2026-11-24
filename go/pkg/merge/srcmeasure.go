package merge

// srcmeasure.go — positions along a source polyline.
//
// The analytic follower stops classifying densified sample POINTS and starts
// solving for intervals along the source LINE, so it needs a position type with exact
// arithmetic: (edge, frac) — the index of a source edge plus the fraction
// along it. This file is that primitive and nothing more: no bands, no
// candidates, no classification.
//
// The arithmetic deliberately mirrors PrepCandidates/projectRangeCeil on the
// canon side, because the two sides meet in the band solve and a mismatched
// convention would surface as drift no fuzz could attribute:
//
//   - measure length per edge is haversine (HaversineMetres), accumulated
//     into a cumulative array, exactly PrepCandidates;
//   - measure WITHIN an edge is linear in the planar parameter —
//     cum[i] + f*(cum[i+1]-cum[i]), exactly projectRangeCeil's `a`;
//   - lat/lon within an edge is the linear interpolation of its endpoints,
//     exactly projectRangeCeil's (plat, plon). So `frac` is simultaneously the
//     measure fraction and the planar parameter the band solve works in — the
//     equirectangular projection is affine in (lat, lon), so the two coincide
//     with no conversion.
//
// NO FALLBACKS. An out-of-range edge or frac is a caller bug and every entry
// point returns an error saying so; nothing is clamped. Zero-length edges are
// NOT degenerate input: GPS resting jitter produces duplicate consecutive
// vertices, the follow path preserves them verbatim
// (TestFollowCorridorAssemble_OffVerbatimPreservesSourceDuplicates) and every
// operation here is defined on them.

import (
	"fmt"
	"math"
	"sort"
)

// SourcePos is a point on a source polyline: edge is an index into the
// polyline's edges (vertex i → vertex i+1) and frac ∈ [0,1] is the fraction
// along that edge.
//
// CANONICAL FORM — the contract every consumer inherits, settled here so no
// pass has to decide it again. The representation is deliberately NOT unique:
// one point can be written several ways. Two rewrites are point-preserving,
//
//   - frac 1 on edge i is frac 0 on edge i+1, and
//   - a zero-length edge (a duplicated source vertex) holds exactly one point,
//     so ANY frac on it is frac 0 on edge i+1 as well,
//
// and the canonical form is the FORWARD-MOST one: both applied to exhaustion.
// So (i, 1.0) canonicalises to (i+1, 0.0) and never the other way, and a whole
// run of duplicated vertices canonicalises to frac 0 on the first edge past
// the run. The polyline's own end is the one place the pushing stops:
// (edges-1, 1.0) has nowhere forward to go and is canonical, and a zero-length
// FINAL edge canonicalises to (edges-1, 0.0).
//
// SourceMeasure.normalise computes it, and SourceMeasure.compare applies it to both
// sides before comparing, which is what gives the passes these properties:
//
//   - the same point written two ways compares EQUAL, exactly, with no
//     epsilon anywhere;
//   - positions on a duplicate-vertex cluster are equal to each other AND to
//     the boundary either side of it. They are one point, so there is no order
//     among them to define. Ordering them by edge index instead — the obvious
//     alternative — would put compare at odds with measure order, and measure order is
//     the property the sweep's event merge relies on;
//   - compare therefore agrees exactly with measure order (measureAt) on every
//     polyline, including all-duplicate ones;
//   - two CANONICAL positions may be compared with == and used as map keys.
//     Raw ones may NOT — normalise first, or use compare.
type SourcePos struct {
	Edge int
	Frac float64
}

// SourceMeasure is a source polyline with its cumulative measure lengths precomputed,
// and is the only thing that can interpret a SourcePos. Positions are indices
// into ITS points; mixing positions between two sourceMeasures is a caller bug the
// type cannot detect, so don't.
type SourceMeasure struct {
	Pts        []Point
	CumMeasure []float64
}

// NewSourceMeasure precomputes the cumulative measure of pts. Fewer than two points is
// an error: a single vertex has no edges, so no SourcePos can exist on it, and
// a caller that reaches here with one point has skipped the matcher's own
// len(pts) == 1 case ([CorridorFollowRuns]) rather than found a degenerate leg.
//
// A polyline whose total length is zero (every vertex duplicated) is NOT an
// error — it is representable, every operation below is well defined on it,
// and rejecting it would reject pinned duplicate-preservation behaviour.
func NewSourceMeasure(pts []Point) (*SourceMeasure, error) {
	if len(pts) < 2 {
		return nil, fmt.Errorf("NewSourceMeasure: need at least 2 points, got %d", len(pts))
	}
	cum := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		cum[i] = cum[i-1] + HaversineMetres(
			pts[i-1].Lat, pts[i-1].Lon,
			pts[i].Lat, pts[i].Lon)
	}
	return &SourceMeasure{Pts: pts, CumMeasure: cum}, nil
}

// edges is the number of edges, one fewer than the number of vertices.
func (sa *SourceMeasure) Edges() int { return len(sa.Pts) - 1 }

// total is the polyline's whole measure length in metres.
func (sa *SourceMeasure) Total() float64 { return sa.CumMeasure[len(sa.CumMeasure)-1] }

// edgeLen is the measure length of edge e, taken as the DIFFERENCE of cumulative
// values rather than a fresh haversine — projectRangeCeil does the same, and
// taking it any other way would let an edge's own length disagree with the
// cumulative array by a rounding step.
func (sa *SourceMeasure) EdgeLen(e int) float64 { return sa.CumMeasure[e+1] - sa.CumMeasure[e] }

// start and end are the polyline's terminal positions. Callers wanting to walk
// to a terminal must use these rather than advancing by the remaining measure: see
// advance's note on the exact-overshoot contract.
func (sa *SourceMeasure) Start() SourcePos { return SourcePos{Edge: 0, Frac: 0} }
func (sa *SourceMeasure) End() SourcePos   { return SourcePos{Edge: sa.Edges() - 1, Frac: 1} }

// vertexPos is the position of original vertex i. Original vertices are the
// positions the whole design leans on (source vertices are always sampled at
// every grid), so naming them is worth a function rather than
// leaving each caller to remember that the last one is (edges-1, 1).
func (sa *SourceMeasure) VertexPos(i int) (SourcePos, error) {
	if i < 0 || i >= len(sa.Pts) {
		return SourcePos{}, fmt.Errorf("sourceMeasure.vertexPos: vertex %d out of range [0,%d]", i, len(sa.Pts)-1)
	}
	if i == sa.Edges() {
		return SourcePos{Edge: i - 1, Frac: 1}, nil
	}
	return SourcePos{Edge: i, Frac: 0}, nil
}

// validate reports whether p is a position on this polyline. The frac test is
// written as a negated range so a NaN frac fails it. Out of range is an error,
// never a clamp: a caller that computed frac = 1.0000001 has a bug in its
// solve, and hiding it here would be a silent fallback.
func (sa *SourceMeasure) Validate(p SourcePos) error {
	if p.Edge < 0 || p.Edge >= sa.Edges() {
		return fmt.Errorf("SourceMeasure.Validate: edge %d out of range [0,%d]", p.Edge, sa.Edges()-1)
	}
	if !(p.Frac >= 0 && p.Frac <= 1) {
		return fmt.Errorf("SourceMeasure.Validate: frac %v out of range [0,1] on edge %d", p.Frac, p.Edge)
	}
	return nil
}

// normalise returns p's canonical representative — the forward-most way of
// writing the same point, as defined on SourcePos.
//
// The invariant it establishes, and the one compare leans on: a normalised
// position that is not on the final edge sits strictly inside an edge of
// POSITIVE length, so its measure is strictly below that edge's end. That is what
// makes lexicographic (edge, frac) order and measure order the same order.
//
// It is idempotent, and its output is safe to compare with == or use as a map
// key.
func (sa *SourceMeasure) Normalise(p SourcePos) (SourcePos, error) {
	if err := sa.Validate(p); err != nil {
		return SourcePos{}, err
	}
	last := sa.Edges() - 1
	for p.Edge < last && (p.Frac == 1 || sa.EdgeLen(p.Edge) == 0) {
		p.Edge++
		p.Frac = 0
	}
	if sa.EdgeLen(p.Edge) == 0 {
		p.Frac = 0 // only reachable on the final edge
	}
	return p, nil
}

// compare orders two positions along the polyline: -1 if a is before b, +1 if
// after, 0 if they are the SAME POINT.
//
// Comparison is EXACT and there is deliberately no epsilon. Positions that are
// merely near each other are different positions; the tolerance that would say
// otherwise (a "join epsilon") is an open design decision. Do not add one here.
//
// Both sides are normalised to the canonical form defined on SourcePos first,
// so the ordering agrees exactly with measure order: compare returns 0 precisely
// when the two positions have the same measure — which includes every pair of
// representations of a duplicated vertex — and the sign of the difference
// otherwise. Note the converse is only true in exact
// arithmetic — measureAt of two equal positions can differ in the last ulp (see
// measureAt), which is why compare works on the representation and not on measures.
//
// USAGE CONSEQUENCE, and it decides how the sweep must be written: positions
// are values to be CARRIED, not re-derived. A round trip through measure space
// (measureAt → advance, or measureBetween → advance) lands within half a nanometre but
// reproduces the position bit-for-bit only about 12% of the time (measured
// over 15,000 random polylines). Exact comparison then calls the
// two different, which is correct and is the point: derive an interval
// endpoint once and pass the SourcePos along, rather than recomputing it and
// expecting it to match.
func (sa *SourceMeasure) Compare(a, b SourcePos) (int, error) {
	na, err := sa.Normalise(a)
	if err != nil {
		return 0, err
	}
	nb, err := sa.Normalise(b)
	if err != nil {
		return 0, err
	}
	switch {
	case na.Edge < nb.Edge:
		return -1, nil
	case na.Edge > nb.Edge:
		return 1, nil
	case na.Frac < nb.Frac:
		return -1, nil
	case na.Frac > nb.Frac:
		return 1, nil
	}
	return 0, nil
}

// equal reports whether a and b denote the same point, on compare's exact
// terms.
func (sa *SourceMeasure) Equal(a, b SourcePos) (bool, error) {
	c, err := sa.Compare(a, b)
	return c == 0, err
}

// measureAt is p's cumulative measure from the start of the polyline, in metres.
//
// The formula is projectRangeCeil's verbatim, so a source measure and a canon measure
// are computed the same way. One consequence worth knowing: at an edge
// boundary the two representations of the same point can differ by a rounding
// step — measureAt({k,1}) is cum[k] + (cum[k+1]-cum[k]), which need not be bit-
// identical to cum[k+1] = measureAt({k+1,0}). In practice they always did agree
// (88,315 of 88,315 random edge boundaries), but the guarantee is
// compare's, not this one's: ask compare whether two positions are one point.
func (sa *SourceMeasure) MeasureAt(p SourcePos) (float64, error) {
	if err := sa.Validate(p); err != nil {
		return 0, err
	}
	return sa.CumMeasure[p.Edge] + p.Frac*sa.EdgeLen(p.Edge), nil
}

// measureBetween is the SIGNED measure length from a to b in metres: positive when b
// is after a, negative when before. Callers wanting a gap magnitude take
// math.Abs, as the canon side does.
func (sa *SourceMeasure) MeasureBetween(a, b SourcePos) (float64, error) {
	aa, err := sa.MeasureAt(a)
	if err != nil {
		return 0, err
	}
	ab, err := sa.MeasureAt(b)
	if err != nil {
		return 0, err
	}
	return ab - aa, nil
}

// posAtMeasure is the position at cumulative measure `at` metres, the inverse of
// measureAt. It mirrors interpOnCanon's binary search — except that where
// interpOnCanon clamps out-of-range measures to the terminals, this errors: a measure
// outside [0, total] is a caller bug and clamping it would silently shorten
// whatever the caller was measuring.
//
// Where several positions share the measure (a run of zero-length edges), the
// EARLIEST representation is returned; normalise maps it forward if a
// canonical form is wanted, and compare treats them all as equal either way.
func (sa *SourceMeasure) PosAtMeasure(at float64) (SourcePos, error) {
	if math.IsNaN(at) {
		return SourcePos{}, fmt.Errorf("sourceMeasure.posAtMeasure: measure is NaN")
	}
	if at < 0 || at > sa.Total() {
		return SourcePos{}, fmt.Errorf("sourceMeasure.posAtMeasure: measure %.6g outside [0,%.6g]", at, sa.Total())
	}
	// The last edge whose cumulative start is at or below `at`. sort.Search
	// finds the first cumulative value ≥ `at`; stepping back one lands on the
	// edge containing it, and on a tie (zero-length edges, or `at` exactly on a
	// vertex) lands on the edge that ENDS at `at`, so frac comes out as 1.
	e := sort.Search(len(sa.CumMeasure), func(k int) bool { return sa.CumMeasure[k] >= at }) - 1
	e = max(e, 0)
	e = min(e, sa.Edges()-1)
	l := sa.EdgeLen(e)
	if l == 0 {
		return SourcePos{Edge: e, Frac: 0}, nil
	}
	// Both numerator and denominator are differences of the same monotone
	// array and the numerator is the smaller, so the quotient is in [0,1]
	// without any clamping: float division rounds a value ≤ 1 to a value ≤ 1.
	return SourcePos{Edge: e, Frac: (at - sa.CumMeasure[e]) / l}, nil
}

// advance moves p by distM metres along the polyline — forward for a positive
// distance, backward for a negative one — and returns the position it lands
// on.
//
// Running off either end is an ERROR, not a clamp: a sweep that asks for a
// window the source does not have has mis-derived its window, and silently
// returning the terminal would let it measure a shorter span than it thinks it
// measured. Landing exactly ON a terminal is legal.
//
// CONTRACT NOTE, and it is sharp — measured, not theoretical. The overshoot
// test is exact, so `advance(p, measureBetween(p, sa.end()))` is not guaranteed to
// reach the end: the two roundings can leave the target an ulp above total,
// and that errors. Over 15,000 random polylines it errored on 1.1% of
// attempts and landed an ulp short of the terminal on another 1.3%. Use
// start()/end() to name a terminal; never arrive at one by arithmetic. Making
// this case succeed needs a tolerance for "close enough to the end", which is
// the undecided join epsilon (see compare), so it is not decided here.
func (sa *SourceMeasure) Advance(p SourcePos, distM float64) (SourcePos, error) {
	if math.IsNaN(distM) || math.IsInf(distM, 0) {
		return SourcePos{}, fmt.Errorf("sourceMeasure.advance: distance %v is not finite", distM)
	}
	if distM == 0 {
		// Exact, not a shortcut for speed: moving nothing cannot change the
		// position, and going round through measure and back would re-derive frac
		// from a division that can land an ulp away. The identity is worth
		// having, since a sweep that advances by a computed zero must not see
		// the position twitch.
		if err := sa.Validate(p); err != nil {
			return SourcePos{}, err
		}
		return p, nil
	}
	at, err := sa.MeasureAt(p)
	if err != nil {
		return SourcePos{}, err
	}
	target := at + distM
	if target < 0 || target > sa.Total() {
		return SourcePos{}, fmt.Errorf(
			"sourceMeasure.advance: %.6g m from measure %.6g reaches %.6g, outside [0,%.6g]",
			distM, at, target, sa.Total())
	}
	return sa.PosAtMeasure(target)
}

// latLonAt is p's position on the ground, interpolated linearly along its edge
// exactly as projectRangeCeil interpolates a canon projection. Linear in the
// same parameter the measure is linear in, so a position half the measure along an
// edge is also half way along it geometrically.
func (sa *SourceMeasure) LatLonAt(p SourcePos) (lat, lon float64, err error) {
	if err := sa.Validate(p); err != nil {
		return 0, 0, err
	}
	a, b := sa.Pts[p.Edge], sa.Pts[p.Edge+1]
	return a.Lat + p.Frac*(b.Lat-a.Lat), a.Lon + p.Frac*(b.Lon-a.Lon), nil
}

// eleAt is p's elevation, linearly interpolated along its edge from the
// bracketing ORIGINAL source vertices' own recorded elevation, by [lerpEle],
// at the same parameter latLonAt interpolates lat/lon at. Reusing
// lerpEle rather than restating it means its callers cannot define
// "interpolated" differently.
//
// Nil propagates, never fabricates: an edge with either endpoint's elevation
// unknown carries an unknown elevation at every position on it, not a 0. See
// [SourceMeasure.pointAt].
func (sa *SourceMeasure) EleAt(p SourcePos) (*float64, error) {
	if err := sa.Validate(p); err != nil {
		return nil, err
	}
	a, b := sa.Pts[p.Edge], sa.Pts[p.Edge+1]
	return lerpEle(a.Ele, b.Ele, p.Frac), nil
}

// pointAt is p's full ground position — lat/lon plus elevation — by the two
// lerps a crossing vertex `cp` is built from. The position is already known
// exactly — a corridor interval's own boundary
// (`CorridorPiece.ImportMeasureAtLo/Hi`) — so there is no search.
func (sa *SourceMeasure) PointAt(p SourcePos) (Point, error) {
	lat, lon, err := sa.LatLonAt(p)
	if err != nil {
		return Point{}, err
	}
	ele, err := sa.EleAt(p)
	if err != nil {
		return Point{}, err
	}
	return Point{Lat: lat, Lon: lon, Ele: ele}, nil
}
