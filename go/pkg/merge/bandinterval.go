package merge

// bandinterval.go — the exact band solve.
//
// For a source edge P→Q and a canon edge A→B, [BandInterval] returns the set
// of positions t ∈ [0,1] along P→Q whose distance to A→B is ≤ d metres. That
// set is a single CLOSED interval: distance to a convex set is convex, a
// segment is convex, and t ↦ P + t(Q−P) is affine, so f(t) = dist(X(t), AB) is
// convex on [0,1] and {t : f(t) ≤ d} is convex.
//
// This replaces sampling the source at densifyStep and classifying every
// resulting point.
//
// THE PROJECTION CONTRACT (mandatory, and the part most likely to go wrong).
// The convexity result is Euclidean, so the metric matters. The solve happens
// in EXACTLY the plane [ClosestPointOnSeg] projects into:
//
//   - equirectangular, with cosLat FIXED at the mid-latitude of the segment
//     being projected ONTO (here the canon edge A→B) — not the probe latitude,
//     not a per-leg projection;
//   - MetresPerDeg for latitude, MetresPerDeg·cosLat for longitude;
//   - never haversine for the point-to-segment distance.
//
// For a fixed segment that projection is a fixed affine map of (lat, lon), and
// the lat/lon lerp X(t) is therefore affine in t here too, so the convexity is
// EXACT rather than approximate. Reaching for haversine, or for cosLat at the
// probe latitude, drifts by millimetres over 15 m — far below any sampled
// reference's resolution, so no fuzz can catch it. [segFrame.project] mirrors
// ClosestPointOnSeg's arithmetic operation-for-operation and is pinned
// bit-exact against it by TestBandIntervalProjectionContract.
//
// NO TOLERANCES. Nothing in this file compares against an epsilon. Where the
// three regime contributions must join, they are required to meet EXACTLY.
// Away from a regime boundary that is free: a piece that runs to a regime
// boundary is clipped by the very float that bounds the next regime, so the
// two meet bitwise. AT a regime boundary the two adjacent regimes measure the
// same distance by different arithmetic and can disagree in the last bit about
// whether the boundary point is in the band, so the boundary's membership is
// decided ONCE, from the solves themselves: it is in the band when either
// adjacent regime's own solve reaches it, and both pieces are made to agree
// with that verdict (see [BandPieces], "the regime joins"). That is an exact
// float equality and an assignment, not a comparison against a small number. A
// gap that survives it means BOTH solves put the boundary outside a band they
// each found on their own side — an arithmetic self-contradiction, reported as
// an error carrying its magnitude, never absorbed. (MeasureEps is an MEASURE guard and is
// deliberately not reused here as a distance tolerance.)

import (
	"fmt"
	"math"

	"github.com/dave/vnx/go/pkg/detmath"
)

// segFrame is the planar frame [ClosestPointOnSeg] projects into for one
// segment a→b: equirectangular about a, with cosLat fixed at the segment's
// MID-LATITUDE. Holding it as a value lets the band solve work in the same
// coordinates as production's point-to-segment distance instead of a
// lookalike. Every coefficient below is the same expression ClosestPointOnSeg
// evaluates, and TestBandIntervalProjectionContract pins each one.
type segFrame struct {
	aLat, aLon float64
	bLat, bLon float64
	CosLat     float64
	Bx, By     float64 // b − a, in metres, in this frame
	LenSq      float64 // |b − a|², metres²
}

func NewSegFrame(aLat, aLon, bLat, bLon float64) segFrame {
	midLat := (aLat + bLat) * 0.5
	cosLat := detmath.Cos(midLat * math.Pi / 180.0)
	bx := float64((bLon - aLon) * MetresPerDeg * cosLat)
	by := float64((bLat - aLat) * MetresPerDeg)
	return segFrame{
		aLat:   aLat,
		aLon:   aLon,
		bLat:   bLat,
		bLon:   bLon,
		CosLat: cosLat,
		Bx:     bx,
		By:     by,
		LenSq:  float64(bx*bx) + float64(by*by),
	}
}

// project is the frame's point-to-segment distance, and it DELEGATES to
// [ClosestPointOnSeg] rather than restating it.
//
// DO NOT "TIDY" THIS INTO A COPY, AND DO NOT EXTRACT THE ARITHMETIC OUT OF
// ClosestPointOnSeg INTO A SHARED HELPER. Both are the obvious refactors and
// both are wrong, for the same reason (decided after measuring it):
//
// A duplicate cannot be relied on to agree bit-for-bit even when it is
// operation-for-operation identical source text. On arm64 the compiler fuses
// `px*bx + py*by` into an FMA, and WHICH of the two products gets fused is its
// choice — so the same expression in two functions can round differently.
// Measured: ClosestPointOnSeg fuses px*bx (0.85138780467973763) where a
// struct-fed copy did not (0.85138780467973774). Assigning the products to
// intermediates does not prevent fusion; the Go spec permits it across
// statements.
//
// The same mechanism makes an EXTRACTION not behaviour-preserving a priori.
// Pulling this plane construction into a helper that ClosestPointOnSeg then
// calls changes the fusion choice INSIDE ClosestPointOnSeg: measured over
// 500,000 random edge/probe triples, the extracted form differs from today's
// on 2.80% of distances (worst 2.27e-13 m) and 11.08% of t values. That is the
// follower's hot path, so an ulp shift there moves band boundaries, run
// boundaries and every pinned test output, for no benefit. "It happens to be
// identical on this toolchain" is not a defence — the next Go release can flip
// it.
//
// Delegating makes the agreement structural rather than tested: it cannot
// drift, on any toolchain, ever, and there is one definition of the metric
// instead of two. The frame's COEFFICIENTS are necessarily recomputed here
// (ClosestPointOnSeg does not expose them) and are pinned against the
// contract's own formulas by TestBandIntervalProjectionContract.
func (f segFrame) Project(probeLat, probeLon float64) (distM, t float64) {
	return ClosestPointOnSeg(f.aLat, f.aLon, f.bLat, f.bLon, probeLat, probeLon)
}

// BandRegime names the three pieces of the piecewise-quadratic dist²(t): which
// part of the canon edge the moving point projects onto.
type BandRegime int

const (
	RegimeNearA BandRegime = iota // s ≤ 0 — the foot clamps to A
	RegimeSpan                    // 0 ≤ s ≤ 1 — the foot is interior
	RegimeNearB                   // s ≥ 1 — the foot clamps to B
)

// BandPiece is a closed sub-interval of [0,1] in source-edge parameter space.
//
// kind and the coefficients below are exposed for the corridor cost integral
// (its canon-side and import-side terms), which
// needs dist²(t) over this piece, not just its band membership. They are
// captured from the SAME local variables BandPieces already computes for the
// membership solve — nothing here is recomputed on a different call path, so
// there is no risk of the FMA-fusion drift [segFrame.project]'s header warns
// about.
//
//   - RegimeNearA, RegimeNearB: dist²(t) = quadA·t² + quadB·t + quadC exactly
//     (SolveQuadLE's own a,b,c, minus the −d² shift that turns it into a
//     membership test).
//   - RegimeSpan: dist²(t) = (spanC0 + spanC1·t)² / spanLenSq, kept in this
//     linear-then-square form rather than expanded — SolveAbsLE's own header
//     explains why: the expanded quadratic cancels catastrophically on a long
//     canon edge. [distSq] squares only at evaluation time; do not expand it
//     into (a,b,c) form elsewhere.
//
// The near regimes ALSO carry their offset in linear form — the vector from the
// clamped vertex to X(t), (nearX0 + nearX1·t, nearY0 + nearY1·t) — from which
// quadA/quadB/quadC were expanded (quadA = nearX1² + nearY1², quadB =
// 2(nearX0·nearX1 + nearY0·nearY1), quadC = nearX0² + nearY0²). The cost
// integral evaluates dist² from it as a SUM OF SQUARES
// ([BandPiece.distSqSquares]), which cannot be negative; the expanded trinomial
// can, by cancellation, where X(t) passes close to the vertex. hasNear says the
// linear form was set, so a piece built without it is an error in the integral
// rather than a silent zero.
type BandPiece struct {
	Lo, Hi                         float64
	Kind                           BandRegime
	QuadA, QuadB, QuadC            float64
	SpanC0, SpanC1, SpanLenSq      float64
	NearX0, NearY0, NearX1, NearY1 float64
	HasNear                        bool
}

// distSq evaluates this piece's exact dist²(t) for t in [lo,hi], using
// whichever of the two forms above its regime was solved in.
func (bp BandPiece) DistSq(t float64) float64 {
	if bp.Kind == RegimeSpan {
		lin := bp.SpanC0 + float64(bp.SpanC1*t)
		return lin * lin / bp.SpanLenSq
	}
	return float64(bp.QuadA*t*t) + float64(bp.QuadB*t) + bp.QuadC
}

// distSqSquares evaluates the same dist²(t) as [distSq], but as a sum of
// squares in every regime, so for finite input it is ≥ 0 BY CONSTRUCTION:
//
//   - RegimeSpan: (spanC0 + spanC1·t)² / spanLenSq — a square over a squared
//     length, which BandPieces only builds when the canon edge has length.
//   - RegimeNearA/RegimeNearB: (nearX0 + nearX1·t)² + (nearY0 + nearY1·t)².
//
// It is what the cost integral evaluates ([IntegrateDistSqPow]). [distSq] stays
// the trinomial for the membership solve and the envelope's choice of piece,
// which compare and solve rather than accumulate; changing those is a
// different change.
func (bp BandPiece) distSqSquares(t float64) (float64, error) {
	switch bp.Kind {
	case RegimeSpan:
		if !(bp.SpanLenSq > 0) {
			return 0, fmt.Errorf("bandPiece: span regime with non-positive spanLenSq %v", bp.SpanLenSq)
		}
		lin := bp.SpanC0 + float64(bp.SpanC1*t)
		return lin * lin / bp.SpanLenSq, nil
	case RegimeNearA, RegimeNearB:
		if !bp.HasNear {
			return 0, fmt.Errorf("bandPiece: near regime built without its linear offset (hasNear false) — the sum-of-squares form cannot be evaluated")
		}
		x := bp.NearX0 + float64(bp.NearX1*t)
		y := bp.NearY0 + float64(bp.NearY1*t)
		return float64(x*x) + float64(y*y), nil
	default:
		return 0, fmt.Errorf("bandPiece: unknown regime kind %v", bp.Kind)
	}
}

// BandRegimeRange is one regime's extent in t, before the band condition is
// applied. Regimes are built in ascending t order.
type BandRegimeRange struct {
	Kind   BandRegime
	Lo, Hi float64
}

// BandJoin is one internal regime boundary and the single authoritative verdict
// taken there: is the boundary point in the band? It is recorded rather than
// merely acted on so tests can pin the construction (that the two adjacent
// pieces agree with this verdict) instead of only its symptom (that no gap is
// reported), and so a surviving gap can name which solve said what.
type BandJoin struct {
	T           float64 // the shared boundary, in source-edge parameter space
	LowReaches  bool    // the regime below t (in t) solved its piece up to t
	HighReaches bool    // the regime above t solved its piece down to t
	InBand      bool    // lowReaches || highReaches
}

// BandInterval returns the positions t ∈ [0,1] along the source edge p→q whose
// distance to the canon edge a→b is at most d metres, as a single closed
// interval. ok is false when no position qualifies.
//
// The interval is CLOSED on both sides: a position sitting at exactly d is
// INSIDE, matching the ≤ band comparisons pinned elsewhere in this package. A
// tangency at exactly d is a double root and yields a single-point interval
// (t0 == t1, ok true), not an empty one.
//
// Degenerate geometry is solved exactly, not refused:
//
//   - a zero-length CANON edge (a == b) gives the disc of radius d about a —
//     duplicate consecutive canon vertices are pinned, supported behaviour
//     (TestFollowCorridorAssemble_OnInteriorPreservesCanonDuplicates), and
//     ClosestPointOnSeg already treats them as a point;
//   - a zero-length SOURCE edge (p == q) is a point test, so the answer is the
//     whole of [0,1] or nothing;
//   - parallel edges collapse the span regime's inequality to a constant, which
//     is solved as a constant.
//
// An error means the arithmetic contradicted itself (the convexity assertion)
// or the input was not finite. There is no fallback path: a case that cannot
// be justified propagates.
func BandInterval(p, q, a, b Point, d float64) (t0, t1 float64, ok bool, err error) {
	pieces, n, joins, nj, err := BandPieces(p, q, a, b, d)
	if err != nil {
		return 0, 0, false, err
	}
	t0, t1, ok, err = UnionOneInterval(pieces[:n])
	if err != nil {
		return 0, 0, false, fmt.Errorf("bandInterval: %w %s%s",
			err, BandGeomStr(p, q, a, b, d), bandJoinsStr(joins[:nj], d))
	}
	return t0, t1, ok, nil
}

// BandPieces is the regime-by-regime half of the solve: it returns each
// regime's contribution, in ascending t order, before they are asserted to be
// one interval, together with the verdict taken at each internal regime join.
// Split out from [BandInterval] so the join can be measured directly by tests —
// the contiguity margin is the evidence that decided how this join is built.
func BandPieces(p, q, a, b Point, d float64) (pieces [3]BandPiece, n int, joins [2]BandJoin, nj int, err error) {
	if !AllFinite(p.Lat, p.Lon, q.Lat, q.Lon, a.Lat, a.Lon, b.Lat, b.Lon, d) {
		return pieces, 0, joins, 0, fmt.Errorf("bandInterval: non-finite input %s", BandGeomStr(p, q, a, b, d))
	}
	if d < 0 {
		return pieces, 0, joins, 0, fmt.Errorf("bandInterval: band must be non-negative, got %g m", d)
	}

	f := NewSegFrame(a.Lat, a.Lon, b.Lat, b.Lon)

	// The source edge in the same frame. X(t) is the lat/lon lerp of p→q, and
	// the frame map is affine in (lat, lon), so X is affine in t here as well.
	//
	// Every product that meets an add or a subtract is converted to float64,
	// so arm64 (which fuses x*y+z into one FMA) gives amd64's bits
	// (pkg/detmath).
	px := float64((p.Lon - f.aLon) * MetresPerDeg * f.CosLat)
	py := float64((p.Lat - f.aLat) * MetresPerDeg)
	qx := float64((q.Lon - f.aLon) * MetresPerDeg * f.CosLat)
	qy := float64((q.Lat - f.aLat) * MetresPerDeg)
	dx, dy := qx-px, qy-py
	dsq := float64(dx*dx) + float64(dy*dy)

	if f.LenSq == 0 {
		// Degenerate canon edge: ClosestPointOnSeg returns the point distance at
		// t = 0, so the band is |X(t) − a| ≤ d — one quadratic, one regime, and
		// no join to assert.
		quadB := 2 * (float64(px*dx) + float64(py*dy))
		quadC := float64(px*px) + float64(py*py)
		lo, hi, has, qerr := SolveQuadLE(dsq, quadB, quadC-float64(d*d), 0, 1)
		if qerr != nil {
			return pieces, 0, joins, 0, fmt.Errorf("bandInterval: zero-length canon edge: %w %s", qerr, BandGeomStr(p, q, a, b, d))
		}
		if !has {
			return pieces, 0, joins, 0, nil
		}
		// Tagged RegimeNearA: ClosestPointOnSeg's foot is fixed at A throughout
		// (a degenerate canon edge has nowhere else to clamp to), which is exactly
		// what RegimeNearA's dist² formula already is.
		pieces[0] = BandPiece{Lo: lo, Hi: hi, Kind: RegimeNearA, QuadA: dsq, QuadB: quadB, QuadC: quadC,
			NearX0: px, NearY0: py, NearX1: dx, NearY1: dy, HasNear: true}
		return pieces, 1, joins, 0, nil
	}

	// s(t) is the (unclamped) projection parameter of X(t) onto a→b, affine in t.
	s0 := (float64(px*f.Bx) + float64(py*f.By)) / f.LenSq
	s1 := (float64(dx*f.Bx) + float64(dy*f.By)) / f.LenSq
	if !AllFinite(s0, s1) {
		return pieces, 0, joins, 0, fmt.Errorf(
			"bandInterval: projection parameter not finite (s0=%v s1=%v) on a canon edge of %g m %s",
			s0, s1, math.Sqrt(f.LenSq), BandGeomStr(p, q, a, b, d))
	}

	// Regime extents in t, ascending. Boundaries are shared floats, so a piece
	// clipped to one regime's end meets the next regime's start exactly.
	var ranges [3]BandRegimeRange
	nr := 0
	if s1 == 0 {
		// The source edge is perpendicular to the canon edge (or is a point): s is
		// constant, so one regime covers all of [0,1].
		switch {
		case s0 < 0:
			ranges[0] = BandRegimeRange{Kind: RegimeNearA, Lo: 0, Hi: 1}
		case s0 > 1:
			ranges[0] = BandRegimeRange{Kind: RegimeNearB, Lo: 0, Hi: 1}
		default:
			ranges[0] = BandRegimeRange{Kind: RegimeSpan, Lo: 0, Hi: 1}
		}
		nr = 1
	} else {
		// tZero and tOne are where s(t) crosses 0 and 1 — i.e. exactly where
		// the foot IS vertex A and vertex B.
		tZero := -s0 / s1     // s(t) = 0 — foot at A
		tOne := (1 - s0) / s1 // s(t) = 1 — foot at B
		if s1 > 0 {
			ranges[0] = BandRegimeRange{RegimeNearA, math.Inf(-1), tZero}
			ranges[1] = BandRegimeRange{RegimeSpan, tZero, tOne}
			ranges[2] = BandRegimeRange{RegimeNearB, tOne, math.Inf(1)}
		} else {
			ranges[0] = BandRegimeRange{RegimeNearB, math.Inf(-1), tOne}
			ranges[1] = BandRegimeRange{RegimeSpan, tOne, tZero}
			ranges[2] = BandRegimeRange{RegimeNearA, tZero, math.Inf(1)}
		}
		nr = 3
	}

	// Each regime's contribution is kept against the regime it came from, not
	// appended straight into pieces: the join construction below has to know
	// which piece sits on which side of a boundary.
	var got [3]BandPiece
	var has [3]bool
	for i := range nr {
		r := ranges[i]
		lo, hi := max(r.Lo, 0), min(r.Hi, 1)
		if lo > hi {
			continue // regime does not meet the source edge
		}
		var plo, phi float64
		var hasPiece bool
		piece := BandPiece{Kind: r.Kind}
		switch r.Kind {
		case RegimeNearA:
			// dist² = |X(t) − a|².
			piece.QuadA, piece.QuadB, piece.QuadC = dsq, 2*(float64(px*dx)+float64(py*dy)), float64(px*px)+float64(py*py)
			piece.NearX0, piece.NearY0, piece.NearX1, piece.NearY1, piece.HasNear = px, py, dx, dy, true
			plo, phi, hasPiece, err = SolveQuadLE(piece.QuadA, piece.QuadB, piece.QuadC-float64(d*d), lo, hi)
		case RegimeNearB:
			// dist² = |X(t) − b|².
			ux, uy := px-f.Bx, py-f.By
			piece.QuadA, piece.QuadB, piece.QuadC = dsq, 2*(float64(ux*dx)+float64(uy*dy)), float64(ux*ux)+float64(uy*uy)
			piece.NearX0, piece.NearY0, piece.NearX1, piece.NearY1, piece.HasNear = ux, uy, dx, dy, true
			plo, phi, hasPiece, err = SolveQuadLE(piece.QuadA, piece.QuadB, piece.QuadC-float64(d*d), lo, hi)
		case RegimeSpan:
			// dist is the perpendicular offset, which is LINEAR in t: with the
			// unnormalised normal n = (−by, bx), dist·|AB| = |(X(t) − a)·n|. Solving
			// |c0 + c1·t| ≤ d·|AB| is algebraically the same as the expanded
			// |X − a|² − s²|AB|², but avoids that form's catastrophic cancellation
			// (two large near-equal terms whenever the source runs far along a long
			// canon edge) and needs no discriminant at all.
			c0 := float64(py*f.Bx) - float64(px*f.By)
			c1 := float64(dy*f.Bx) - float64(dx*f.By)
			piece.SpanC0, piece.SpanC1, piece.SpanLenSq = c0, c1, f.LenSq
			plo, phi, hasPiece = SolveAbsLE(c0, c1, d*math.Sqrt(f.LenSq), lo, hi)
		}
		if err != nil {
			return pieces, 0, joins, 0, fmt.Errorf("bandInterval: %w %s", err, BandGeomStr(p, q, a, b, d))
		}
		if hasPiece {
			piece.Lo, piece.Hi = plo, phi
			got[i] = piece
			has[i] = true
		}
	}

	// THE REGIME JOINS (analytic-follow D2(i)).
	//
	// Two adjacent regimes that both meet the source edge share a boundary float
	// t_b, and both measure dist(X(t_b), AB) — the SAME geometric quantity by
	// DIFFERENT arithmetic, the circle quadratic on one side and the
	// perpendicular linear form on the other. They agree to about 1e-16
	// relative, which on a shallow band crossing works out at ~1e-15 in t, and
	// that was enough to make the two pieces disagree about whether t_b is in
	// the band and leave a gap where the band is plainly one interval. Measured
	// before this construction existed: 0 firings in 204,000 random cases, but 4
	// in 20,000 engineered tangential ones (worst gap 5.884e-15 in t = 2.16e-12
	// m), and an import makes millions of joins.
	//
	// So t_b's membership is decided ONCE, and from the solves' own output: t_b
	// is in the band when EITHER adjacent regime's solve reaches it — when
	// clipTo left that piece ending exactly on t_b because its own root lies at
	// or beyond it. Both pieces are then made to agree with that verdict. Note
	// what this is: an exact float equality and a boolean assignment, not a
	// comparison against a small number. The disagreement is removed by
	// construction rather than absorbed by a tolerance, which is the whole point
	// of D2(i).
	//
	// WHY NOT A THIRD MEASUREMENT. The verdict used to be ClosestPointOnSeg at
	// the lat/lon lerp X(t_b). That point is rounded to the lat/lon grid (~1e-9 m
	// at 53°N), so its distance disagreed with BOTH solves by more than they
	// disagree with each other: with d within ~1e-9 m of the boundary distance it
	// said OUT while one solve reached t_b and the other stopped 1e-15 past it,
	// and the surviving gap was a hard "convexity violated" error on a geometry
	// that is one interval. Measured over 4.84M near-boundary bands (two seeds
	// of a boundary generator, d stepped ±60 × 1e-12)
	// plus 200,000 random cases: 48 such errors with ClosestPointOnSeg, 80 with
	// the same distance re-measured in this frame's affine arithmetic, 0 with
	// this verdict — which took no OUT verdict at all. Any measurement other
	// than the solves can land on the other side of one of them.
	//
	// IN BAND: extending both pieces to t_b is exact, not merely convenient.
	// {t : dist(X(t), AB) ≤ d} is convex, and one solve already says t_b is in
	// it, so a piece holding band points together with t_b brackets an interval
	// in the band. clipTo has already put got[i-1].hi ≤ t_b ≤ got[i].lo, so
	// each assignment can only extend, and only across a disagreement.
	//
	// OUT OF BAND: neither solve reaches t_b, yet each found band points on its
	// own side. There is nothing to assign — a closed interval cannot be made
	// to exclude its own endpoint. Both pieces keep their own roots, and the gap
	// between them survives to [UnionOneInterval] as the hard error it is: both
	// arithmetics contradict convexity at the same point, and [bandJoinsStr]
	// says so in the message.
	//
	// DO NOT "FIX" THAT BY DROPPING A PIECE. It is the obvious simplification
	// and it will be proposed again, so here is why it is wrong. It is sound
	// only when the piece being dropped is a sliver, and telling a sliver from a
	// real interval requires comparing its width against something, which is
	// the tolerance this construction exists to avoid. So: no drop, and no
	// epsilon — the contradiction is reported.
	//
	// A boundary where only one side found band points takes no verdict: there
	// is no join to make consistent, and no gap can arise there.
	joins, nj, err = JoinRegimePieces(ranges, nr, &got, has)
	if err != nil {
		return pieces, 0, joins, 0, fmt.Errorf("%w %s", err, BandGeomStr(p, q, a, b, d))
	}

	for i := range nr {
		if has[i] {
			pieces[n] = got[i]
			n++
		}
	}
	return pieces, n, joins, nj, nil
}

// JoinRegimePieces takes the verdict at each internal regime boundary where
// both adjacent regimes produced a piece, and makes both pieces agree with it
// (see "THE REGIME JOINS" in [BandPieces]). got holds each regime's piece
// against its range, has says which regimes produced one; got is updated in
// place. It is split out of BandPieces so its OUT branch can be tested: no
// geometry found reaches it, so only hand-made pieces can.
func JoinRegimePieces(ranges [3]BandRegimeRange, nr int, got *[3]BandPiece, has [3]bool) (joins [2]BandJoin, nj int, err error) {
	for i := 1; i < nr; i++ {
		// has[i] is only set below the clipping test, so it also says the regime
		// met the source edge — which is what puts the shared boundary in [0,1].
		if !has[i-1] || !has[i] {
			continue
		}
		tb := ranges[i].Lo
		if tb != ranges[i-1].Hi || tb < 0 || tb > 1 || got[i-1].Hi > tb || got[i].Lo < tb {
			return joins, 0, fmt.Errorf(
				"bandInterval: regime boundary %.17g is not the shared, clipped endpoint of [%.17g,%.17g] and [%.17g,%.17g] (ranges end %.17g, start %.17g)",
				tb, got[i-1].Lo, got[i-1].Hi, got[i].Lo, got[i].Hi, ranges[i-1].Hi, ranges[i].Lo)
		}
		// One boolean, and the SAME boolean is both recorded and acted on —
		// "decided once" is structural here, not a comment.
		join := BandJoin{T: tb, LowReaches: got[i-1].Hi == tb, HighReaches: got[i].Lo == tb}
		join.InBand = join.LowReaches || join.HighReaches
		joins[nj] = join
		nj++
		if join.InBand {
			got[i-1].Hi = tb
			got[i].Lo = tb
		}
	}
	return joins, nj, nil
}

// bandJoinsStr renders the verdicts taken at the regime joins, so a surviving
// convexity error says which solves put the boundary outside the band rather
// than only the gap it left.
func bandJoinsStr(joins []BandJoin, d float64) string {
	if len(joins) == 0 {
		return " [no regime join was reached]"
	}
	s := ""
	for _, j := range joins {
		verdict := "OUT of band: neither adjacent solve reaches it"
		if j.InBand {
			verdict = "IN band"
		}
		s += fmt.Sprintf(" [join at t=%.17g, d=%g m: lower solve reaches it %v, upper %v — %s]", j.T, d, j.LowReaches, j.HighReaches, verdict)
	}
	return s
}

// UnionOneInterval merges the per-regime pieces, which arrive in ascending t
// order, and asserts convexity along the way: dist(X(t), AB) is convex, so the
// regime contributions MUST union to exactly one interval.
//
// There is deliberately no join epsilon, and this function is where that shows.
// Contiguous pieces meet EXACTLY: a piece that spills past a regime is clipped
// by the same float that starts the next regime, and where the two regimes
// would otherwise disagree in the last bit about the boundary point, the single
// verdict taken in [BandPieces] has already put both pieces on the boundary.
// Regimes share only their endpoints, so pieces cannot legitimately overlap.
// Either failure is the arithmetic disagreeing with itself — a gap here now
// means BOTH adjacent solves put the boundary point outside the band while each
// found band on its own side of it — so it is reported with its magnitude
// rather than merged away.
func UnionOneInterval(pieces []BandPiece) (lo, hi float64, ok bool, err error) {
	if len(pieces) == 0 {
		return 0, 0, false, nil
	}
	lo, hi = pieces[0].Lo, pieces[0].Hi
	for _, p := range pieces[1:] {
		switch {
		case p.Lo > hi:
			return 0, 0, false, fmt.Errorf(
				"convexity violated: regime pieces [%.17g,%.17g] and [%.17g,%.17g] leave a gap of %.17g in t — the band is not one interval",
				lo, hi, p.Lo, p.Hi, p.Lo-hi)
		case p.Lo < hi:
			return 0, 0, false, fmt.Errorf(
				"regime pieces [%.17g,%.17g] and [%.17g,%.17g] overlap by %.17g in t — regimes share only their endpoints, so this is an arithmetic inconsistency",
				lo, hi, p.Lo, p.Hi, hi-p.Lo)
		}
		hi = p.Hi
	}
	return lo, hi, true, nil
}

// SolveQuadLE returns the sub-interval of [lo,hi] on which a·t² + b·t + c ≤ 0.
// a is a squared length here, so it is never negative; a == 0 is the
// zero-length source edge, which is solved as the linear equation it is rather
// than special-cased away.
func SolveQuadLE(a, b, c, lo, hi float64) (float64, float64, bool, error) {
	if a < 0 {
		return 0, 0, false, fmt.Errorf("solveQuadLE: leading coefficient %g is negative, but it is a squared length", a)
	}
	if a == 0 {
		return solveLinearLE(b, c, lo, hi)
	}
	disc := float64(b*b) - float64(4*a*c)
	if disc < 0 {
		return 0, 0, false, nil // the parabola never reaches the band
	}
	sq := math.Sqrt(disc)
	// Stable roots: the textbook formula loses the root nearer zero to
	// cancellation when |b| dominates. r0 ≤ r1 after the swap below.
	var r0, r1 float64
	if b >= 0 {
		w := -0.5 * (b + sq)
		r0 = w / a
		if w != 0 {
			r1 = c / w
		} else {
			r1 = r0 // b == 0 and disc == 0 ⇒ c == 0 ⇒ a double root at 0
		}
	} else {
		w := -0.5 * (b - sq)
		r1 = w / a
		if w != 0 {
			r0 = c / w
		} else {
			r0 = r1
		}
	}
	if r0 > r1 {
		r0, r1 = r1, r0
	}
	return clipTo(r0, r1, lo, hi)
}

// solveLinearLE returns the sub-interval of [lo,hi] on which b·t + c ≤ 0.
func solveLinearLE(b, c, lo, hi float64) (float64, float64, bool, error) {
	if b == 0 {
		if c <= 0 {
			return lo, hi, true, nil
		}
		return 0, 0, false, nil
	}
	root := -c / b
	if b > 0 {
		return clipTo(math.Inf(-1), root, lo, hi)
	}
	return clipTo(root, math.Inf(1), lo, hi)
}

// SolveAbsLE returns the sub-interval of [lo,hi] on which |c0 + c1·t| ≤ m.
func SolveAbsLE(c0, c1, m, lo, hi float64) (float64, float64, bool) {
	if c1 == 0 {
		// Parallel edges (or a point source): the offset does not vary with t.
		if math.Abs(c0) <= m {
			return lo, hi, true
		}
		return 0, 0, false
	}
	r0 := (-m - c0) / c1
	r1 := (m - c0) / c1
	if r0 > r1 {
		r0, r1 = r1, r0
	}
	res0, res1, ok, _ := clipTo(r0, r1, lo, hi)
	return res0, res1, ok
}

// clipTo intersects the solution interval [r0,r1] with the regime's [lo,hi].
func clipTo(r0, r1, lo, hi float64) (float64, float64, bool, error) {
	r0, r1 = max(r0, lo), min(r1, hi)
	if r0 > r1 {
		return 0, 0, false, nil
	}
	return r0, r1, true, nil
}

func AllFinite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// BandGeomStr renders the offending geometry for an error message, so a
// failure names the exact edges rather than just the symptom.
func BandGeomStr(p, q, a, b Point, d float64) string {
	return fmt.Sprintf("[P=(%.9g,%.9g) Q=(%.9g,%.9g) A=(%.9g,%.9g) B=(%.9g,%.9g) d=%g]",
		p.Lat, p.Lon, q.Lat, q.Lon, a.Lat, a.Lon, b.Lat, b.Lon, d)
}
