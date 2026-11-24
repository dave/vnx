package merge

// The four corridor-matcher cost terms: emission, backtrack, state change,
// and the canon-side gap integral.
//
// Pure functions over CorridorPiece/corridorInterval (corridorinterval.go)
// and BandPiece (bandinterval.go). The solver (corridorsolve.go) combines
// these into a shortest path over (interval, direction) states, layered on
// import measure.
//
// Everything is metres, and every integral here is CLOSED FORM: the design's
// whole point is "no resolution constant", and a fixed-step numerical
// integration would reintroduce exactly that. The canon-side integral's
// closed form was verified to ~1e-9 relative error against brute force.

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// ---- constants -------------------------------------------------------

// CorridorRadiusM is R: emission and backtrack apply within it; the
// canon-side gap integral prices departure BEYOND it. ONE definition site.
// Deliberately NOT aliased to followBands.StayM, an older matcher's tunable.
//
// It is 10 m while the node snap radius is 15 m, so the two are numerically
// distinct: when they were equal, reading one in the other's place went
// unnoticed, and now that class of substitution fails loud.
//
// ⚠ CANDIDATE DISCOVERY MUST TEST EDGES, NOT POINTS. A tighten that keeps an
// arc only if a source POINT is within R of its bbox disagrees with the
// solver, which prices distance as an INTEGRAL along the source EDGE — so an
// arc could be beyond R of every point yet within R of a long edge's interior.
// On hand-drawn plans 92% of source edges are long enough to hide an arc this
// way at R=15, 98% at R=10 (recorded tracks 1.5% → 3.4%, GPS points being
// dense).
//
// ⚠ THAT 98% MEASURED GEOMETRIC POSSIBILITY, NOT ARCS ACTUALLY HIDDEN, and
// the two differ by two orders of magnitude. The tighten is
// [arcBBoxNearAnyEdge], an edge-vs-box test built on [rectsBeyond]; measured
// across six test corpora, the per-point test was dropping 171 arcs the edge
// test keeps, of which the solver binds 10 — 229 m of canon across 3 legs of
// 181. The exposure was real and it was small. "How many edges COULD hide an
// arc" is not "how many arcs were hidden", and only the second one is the
// defect.
//
// ⚠ THE EDGE TEST IS NOT A STRICT SUPERSET OF THE POINT TEST, AND THAT IS
// DELIBERATE. The per-point test padded the arc's bbox PER AXIS and asked
// whether a point fell in the padded rectangle, so it accepted a point off a
// corner at up to R*sqrt(2) — 9 m of latitude plus 9 m of longitude is 12.7 m,
// kept at R=10. [rectsBeyond] measures the true Euclidean box separation and
// drops that corner. Dropping it is CORRECT, not a second exposure: the
// solver's own pruning is Euclidean too ([rectsBeyond] is what
// sourceEdgeCorridorPieces already uses), so an arc 12.7 m from the nearest
// source geometry cannot bind at R=10 whatever the tighten does.
//
// So arc counts moved in BOTH directions between the two tests — some arcs
// newly kept because an edge reaches them, some no longer kept because a
// corner never really did — and a single corpus's delta is the net of two
// populations.
//
// Exported for the reference application's tests; not part of the stable API.
const CorridorRadiusM = 10.0

// emissionK is k, the exponent in the emission and canon-side gap terms:
// (d/R)^k. MUST be a positive even integer — [validateEvenK] explains why.
//
// 2 is a deliberate starting choice, to be raised if needed — not an
// unmeasured placeholder.
//
// What k buys, so the choice can be revisited on evidence rather than taste:
// it sets how fast cost-per-metre grows with departure depth, which is the
// whole of "not linearly". At R = 15 m, per-metre cost relative to sitting
// exactly on canon at R:
//
//	k     16 m      20 m      30 m (2R)
//	2     1.14x     1.78x       4x   <- current
//	4     1.29x     3.16x      16x
//	6     1.47x     5.62x      64x
//
// All three depths are buildable (a d-deep gap anchored at 0 needs length
// >= 2d, so 30 m needs a 60 m gap — routine on a real detour). Raise k if
// deep excursions turn out to be adopted that should have been rejected.
//
// ⚠ Do NOT try to settle k with a "40 m at 60 m departure vs 400 m at 20 m"
// ordering test. That fixture is geometrically impossible and the ordering it
// asks for does not flip at ANY k — see
// TestCostOrdering_RealizableGeometryDoesNotFlipWithK.
const emissionK = 2

// stateChangeCostP is P: the flat penalty the solver charges at one
// state transition that is not measure-adjacent across a shared canon node
// (P is not charged along one trail — deciding WHEN a transition qualifies is
// the solver's job, not this file's). It replaces a minimum run length, a
// bridge-gap threshold and an entry gate.
//
// Units are metres, like every cost here — P = 50 means one change of what
// the line follows costs as much as 50 m of running slightly off-trail.
//
// 50 is a WORKING value, not a measured optimum, and the distinction matters.
// A sweep of P over two corpora cannot choose it, for a structural reason:
// coverage rises as P falls, so the sweep's argmax is always P -> 0, which is
// exactly the chattering the penalty exists to stop
// (TestSolveCorridorPath_CrossingTrailsNoChatterWithP: P = 0 splits one trail
// into 6 runs). The instrument that should push the other way — fabrication —
// read 0.00 m at EVERY sweep point, because the corridor pieces satisfy
// distance <= R by construction. One-sided instrument, so no interior optimum
// exists to find.
//
// Measured shape, for whoever revisits this:
//
//	area                 P=1     P=50    P=200   P=1000
//	plans corpus       85.02%  82.74%   76.30%   56.18%
//	tracks corpus       4.59%   3.96%    3.96%    0.00%
//
// Plans degrade gently; tracks fall off a cliff and adopt NOTHING by P = 1000.
// The tracks curve is also non-monotonic (dips at P = 5, recovers by P = 20),
// so no single-corpus reading generalises.
//
// ⚠ Synthetic tests cannot show what is acceptable: coverage cannot tell a
// correctly-refused spur from a wrongly-abandoned trail; only looking at the
// geometry can. Use an offline sweep to catch BREAKAGE ("adopts nothing above
// P = 1000"), never to pick between 50 and 75.
const stateChangeCostP = 50.0

// backtrackWeightPerM is the per-metre weight on canon measure travelled against
// a state's declared direction.
//
// 1.0 is a working value alongside P = 50. It was swept and is NOT a weak
// lever: on plans it moves coverage by ~4 points across a 320x range, but on
// tracks it is savage — one test track at P = 50 goes 4.28% at weight 0.25,
// 3.96% at 1, and 0.19% at 8, and at P = 200 weight 8 adopts NOTHING. It also
// interacts with P rather than being separable: its effect shrinks as P rises
// (1.14 points of spread at P = 10, 0.12 at P = 200 on one plan corpus).
//
// So 1 is measured and deliberately held rather than an untested default.
// Tune it on real data with P, not offline and not independently.
const backtrackWeightPerM = 1.0

// validateEvenK reports an error unless k is a positive even integer.
//
// WHY EVEN ONLY, verified against [BandPiece.distSq]'s own two forms: within
// one regime, dist²(t) is EXACTLY quadratic in t — either
// quadA·t²+quadB·t+quadC (RegimeNearA/RegimeNearB, solveQuadLE's own
// coefficients) or (spanC0+spanC1·t)²/spanLenSq (RegimeSpan) — both are a
// squared polynomial. For EVEN k, (dist²(t))^(k/2) is therefore a plain
// polynomial in t (a whole power of a polynomial), integrable term-by-term
// by the power rule — no numerical quadrature. For ODD k, (dist²(t))^(k/2)
// is a half-integer power of a quadratic that is not, in general, a perfect
// square — e.g. plain dist(t) itself at k=1 — and has no polynomial
// antiderivative; a closed form exists in general (arcsinh/log terms for the
// linear-squared case) but is a different, heavier code path than the power
// rule this file relies on throughout, and this package does not implement
// it.
func validateEvenK(k int) error {
	if k <= 0 || k%2 != 0 {
		return fmt.Errorf("corridor cost: k must be a positive even integer, got %d", k)
	}
	return nil
}

// ---- shared exact integration --------------------------------------------

// gaussLegendreMaxPoints bounds the rule table below. An n-point rule is exact
// for polynomials of degree ≤ 2n−1, and ∫ dist²(t)^m needs degree 2m, so n = m+1
// points; 16 covers every even k up to 30. A larger k is an error, not a
// silently inexact rule.
const gaussLegendreMaxPoints = 16

// gaussLegendreRules[n] holds the n-point Gauss–Legendre nodes and weights on
// [−1, 1], computed once at package init by Newton's method on P_n — the
// textbook construction, deterministic on every platform. Every weight is
// strictly positive; TestGaussLegendreRules checks them, and checks each rule
// integrates the monomials up to degree 2n−1 exactly.
var gaussLegendreRules = func() [gaussLegendreMaxPoints + 1]struct{ X, W []float64 } {
	var rules [gaussLegendreMaxPoints + 1]struct{ X, W []float64 }
	for n := 1; n <= gaussLegendreMaxPoints; n++ {
		x := make([]float64, n)
		w := make([]float64, n)
		for i := 0; i < n; i++ {
			// Chebyshev-like first guess for the i-th root, then Newton.
			z := math.Cos(math.Pi * (float64(i) + 0.75) / (float64(n) + 0.5))
			var dp float64
			// Newton until the iterate stops moving (a fixed point in float64),
			// capped so a two-float oscillation cannot spin; the cap is far
			// above the handful of steps Newton needs from this guess, and
			// TestGaussLegendreRules checks the nodes it leaves.
			for iter := 0; iter < 100; iter++ {
				p0, p1 := 1.0, z
				for j := 2; j <= n; j++ {
					p0, p1 = p1, ((2*float64(j)-1)*z*p1-(float64(j)-1)*p0)/float64(j)
				}
				// p1 = P_n(z), p0 = P_{n−1}(z).
				dp = float64(n) * (z*p1 - p0) / (z*z - 1)
				next := z - p1/dp
				if next == z {
					break
				}
				z = next
			}
			x[i] = z
			w[i] = 2 / ((1 - z*z) * dp * dp)
		}
		rules[n].X, rules[n].W = x, w
	}
	return rules
}()

// integrateDistSqPow returns ∫ distSq(t)^m dt over [bp.lo, bp.hi], exactly up
// to rounding, for m = k/2.
//
// ⚠ IT IS NON-NEGATIVE BY CONSTRUCTION, and that is why it is written this way:
// a bug in the sign is fixed at its cause, not pasted over. It is an
// (m+1)-point
// Gauss–Legendre rule over the piece:
//
//	(hi − lo)/2 · Σ_i w_i · distSqSquares(t_i)^m ,  t_i = lo + (1 + x_i)(hi − lo)/2
//
// Every factor is ≥ 0: hi ≥ lo is checked, every weight w_i is > 0, and
// [BandPiece.distSqSquares] is a sum of squares. A sum of non-negative terms
// cannot be negative, whatever the rounding. And the rule is EXACT, not an
// approximation: the integrand is a polynomial of degree 2m in t, and an
// (m+1)-point rule integrates every polynomial of degree ≤ 2m+1 exactly. So
// this is the same integral as before, with no resolution constant.
//
// WHAT IT REPLACED. The old form expanded the integrand into power-basis
// coefficients and applied the power rule, Σ c_j/(j+1)·(hi^(j+1) − lo^(j+1)).
// On a short piece hi and lo are nearly equal, so each difference of powers
// cancels, and with coefficients of both signs the sum came out NEGATIVE: down
// to −1.43e-8 on pieces 4.4e-11 m to 1.04e-3 m wide, 4,932 times on two
// test imports, which made forward-arc costs negative and let the solve
// improve states it had already popped.
// A clamp at 0 was considered and refused: it hides the cancellation instead of
// removing it, and the value it clamps was already wrong before it went below 0.
//
// It evaluates dist² by the sum-of-squares form, not the trinomial, because the
// trinomial itself can come out below 0 where X(t) passes through the vertex —
// the same cancellation one level down.
func integrateDistSqPow(bp BandPiece, m int) (float64, error) {
	if bp.Hi < bp.Lo {
		return 0, fmt.Errorf("corridor cost: piece has hi %.17g < lo %.17g", bp.Hi, bp.Lo)
	}
	if bp.Hi == bp.Lo {
		return 0, nil // zero-measure piece (a tangency point): no contribution
	}
	n := m + 1
	if m < 1 || n > gaussLegendreMaxPoints {
		return 0, fmt.Errorf("corridor cost: power m=%d needs a %d-point Gauss–Legendre rule; this file carries rules of 2..%d points", m, n, gaussLegendreMaxPoints)
	}
	rule := gaussLegendreRules[n]
	half := (bp.Hi - bp.Lo) / 2
	sum := 0.0
	for i, x := range rule.X {
		v, err := bp.distSqSquares(bp.Lo + (1+x)*half)
		if err != nil {
			return 0, fmt.Errorf("corridor cost: %w", err)
		}
		p := v
		for j := 1; j < m; j++ {
			p *= v
		}
		sum += rule.W[i] * p
	}
	out := half * sum
	if !(out >= 0) || math.IsInf(out, 0) {
		// Unreachable for finite input — every factor above is ≥ 0. A NaN or an
		// infinity here means the piece's coefficients were not finite.
		return 0, fmt.Errorf("corridor cost: integral over [%.17g,%.17g] is %v — the piece's coefficients are not finite", bp.Lo, bp.Hi, out)
	}
	return out, nil
}

// ---- emission -----------------------------------------------------------

// emissionCost is the emission term for one CorridorPiece: (d/R)^k
// integrated per metre of IMPORT measure across the piece — the soft
// replacement for a band membership test.
//
// distSq(t) is exact per [BandPiece]; import measure is affine in t within one
// source edge ([CorridorPiece]'s own doc comment), so the Jacobian ds/dt is
// the piece's own constant rate — only a scale factor on the t-integral, no
// separate measure-length integral needed.
func emissionCost(cp CorridorPiece, r float64, k int) (float64, error) {
	if err := validateEvenK(k); err != nil {
		return 0, err
	}
	if !(r > 0) || !allFinite(r) {
		return 0, fmt.Errorf("corridor cost: emission radius must be finite and positive, got %v", r)
	}
	bp := cp.Regime
	if bp.Hi == bp.Lo {
		return 0, nil
	}
	rate := (cp.ImportMeasureAtHi - cp.ImportMeasureAtLo) / (bp.Hi - bp.Lo)
	if !allFinite(rate) {
		return 0, fmt.Errorf("corridor cost: piece import-measure rate not finite (ImportMeasureAtLo=%v ImportMeasureAtHi=%v lo=%v hi=%v)",
			cp.ImportMeasureAtLo, cp.ImportMeasureAtHi, bp.Lo, bp.Hi)
	}
	integral, err := integrateDistSqPow(bp, k/2)
	if err != nil {
		return 0, err
	}
	e := rate * integral / math.Pow(r, float64(k))
	// ⚠ A NEGATIVE EMISSION IS AN ERROR, not a value to pass on. The integral
	// is ≥ 0 by construction and r^k > 0, so only a negative rate — a piece
	// whose import measure runs backwards against t — can make it negative, and
	// that is a broken piece. A negative edge cost breaks the solve's Dijkstra
	// (a popped state can be improved again), which is how the old integral's
	// cancellation was found.
	if !(e >= 0) {
		return 0, fmt.Errorf("corridor cost: emission cost %v is negative or NaN (rate %v, integral %v, import measure [%v,%v] over t [%v,%v])",
			e, rate, integral, cp.ImportMeasureAtLo, cp.ImportMeasureAtHi, bp.Lo, bp.Hi)
	}
	return e, nil
}

// ---- backtrack ------------------------------------------------------

// backtrackMeasureM is the RAW (unweighted) canon measure travelled against a
// state's declared direction over one CorridorPiece — the term that
// replaces a turnaround rule and a wrong-arm measure-span guard.
// dirIncreasing is the state's own direction: true means the state expects
// canon measure to INCREASE as import measure increases.
//
// A single piece's canon measure is monotonic (or constant) in t: CanonMeasureAtLo
// and CanonMeasureAtHi are the whole story, no integral needed. RegimeNearA/
// RegimeNearB clamp the foot to a fixed vertex, so CanonMeasureAtLo==
// CanonMeasureAtHi (zero either way); RegimeSpan's foot parameter s(t) is
// affine in t (bandinterval.go's own s0+s1·t, "s(t) is ... affine in t"), so
// canon measure moves in exactly one direction across the whole piece and never
// reverses partway through it.
func backtrackMeasureM(cp CorridorPiece, dirIncreasing bool) float64 {
	delta := cp.CanonMeasureAtHi - cp.CanonMeasureAtLo
	if dirIncreasing {
		if delta < 0 {
			return -delta
		}
		return 0
	}
	if delta > 0 {
		return delta
	}
	return 0
}

// backtrackCost is [backtrackMeasureM] weighted by [backtrackWeightPerM] — the
// cost the solver actually charges.
//
// weightOverride is optional: omitted, this uses the production constant.
// [ArcCost] passes it from a [CorridorTunables] override, which only tests
// set, for sweeping the weight.
func backtrackCost(cp CorridorPiece, dirIncreasing bool, weightOverride ...float64) float64 {
	w := backtrackWeightPerM
	if len(weightOverride) > 0 {
		w = weightOverride[0]
	}
	return w * backtrackMeasureM(cp, dirIncreasing)
}

// ---- state change, and the bridge/no-bridge inequality --------------

// stateChangeCost is P: the flat penalty for one non-measure-adjacent state
// transition. Deciding WHICH transitions qualify — "P is not charged along
// one trail" — is the solver's job; this returns only the constant itself.
//
// pOverride is optional, same reason and same pattern as [backtrackCost]'s
// weightOverride — tests only, every production call site passes nothing.
func stateChangeCost(pOverride ...float64) float64 {
	if len(pOverride) > 0 {
		return pOverride[0]
	}
	return stateChangeCostP
}

// bridgeCost is the cost of BRIDGING a gap between two adopted stretches: the
// gap integral, and nothing else besides it. Bridging is an inequality with P
// on the other side ([noBridgeCost]).
//
// ⚠ "THE GAP INTEGRAL" IS TWO TERMS, NOT ONE. Taking it as the canon-side
// integral alone is the defect [importGapCost] fixes: a bridge consumes import
// measure too, and without that term it was the only edge in the solve that
// consumed it for free. Callers pass canonGapCostBetween +
// importGapCostBetween — see corridorsolve.go's [candidateBridgeCost], which
// is the only production caller and the place the two are summed.
func bridgeCost(gapIntegral float64) float64 {
	return gapIntegral
}

// noBridgeCost is the cost of NOT bridging: two state-change penalties (one
// to leave the interval before the gap, one to re-enter after it) plus the
// import's own OFF metres across the gap.
//
// pOverride, same test-only pattern, forwarded to [stateChangeCost].
func noBridgeCost(offImportMetres float64, pOverride ...float64) float64 {
	return 2*stateChangeCost(pOverride...) + offImportMetres
}

// ---- canon-side gap integral ------------------------------------------

// canonEdgeDistSqRegimes returns dist²(u) for u ∈ [0,1] along canon edge
// A→B, against ONE fixed source arc P→Q, as up to three [BandPiece]
// regime pieces covering the edge's WHOLE extent — the transposed solve,
// verified to ~1e-9 against brute force: the gap term needs distance BEYOND
// R, where [bandPieces]' own band-clipped pieces give nothing.
//
// This mirrors bandPieces' own regime construction with A,B in the
// "moving" (p,q) role and P,Q in the "frame" (a,b) role — i.e. it computes
// exactly what bandPieces(A, B, P, Q, d) would, for the part that does not
// depend on d at all (the regime ranges and their dist² coefficients are
// computed BEFORE bandPieces ever looks at d — see its source). It is not
// extracted out of bandPieces into a shared helper: that function's every
// line carries FMA-fusion and catastrophic-cancellation warnings earned the
// hard way (bandinterval.go's header), and a refactor risks disturbing
// behaviour pinned by dozens of existing tests for a benefit — avoiding
// ~30 duplicated lines of index/coefficient bookkeeping — that does not
// justify the risk. What IS shared is [newSegFrame] and the coefficient
// FORMULAS themselves, copied verbatim from bandPieces so a diff against it
// stays legible; nothing here is independently re-derived.
//
// No band condition is applied: every regime's whole [0,1]-clipped domain is
// kept, because the gap term integrates distance BEYOND R, not membership
// within it. bandPieces' interior REGIME JOIN logic (the verdict that a
// boundary is in band when either adjacent solve reaches it,
// bandinterval.go's "THE REGIME JOINS") exists only
// to resolve band MEMBERSHIP disagreements at a boundary; it is not needed
// here because a single boundary point has zero measure and cannot change a
// definite integral regardless of which regime "claims" it.
func canonEdgeDistSqRegimes(a, b, p, q Point) (pieces [3]BandPiece, n int, err error) {
	if !allFinite(a.Lat, a.Lon, b.Lat, b.Lon, p.Lat, p.Lon, q.Lat, q.Lon) {
		return pieces, 0, fmt.Errorf("corridor cost: non-finite canon/source edge [A=(%v,%v) B=(%v,%v) P=(%v,%v) Q=(%v,%v)]",
			a.Lat, a.Lon, b.Lat, b.Lon, p.Lat, p.Lon, q.Lat, q.Lon)
	}

	f := newSegFrame(p.Lat, p.Lon, q.Lat, q.Lon)

	ax := (a.Lon - f.aLon) * MetresPerDeg * f.CosLat
	ay := (a.Lat - f.aLat) * MetresPerDeg
	bx := (b.Lon - f.aLon) * MetresPerDeg * f.CosLat
	by := (b.Lat - f.aLat) * MetresPerDeg
	dx, dy := bx-ax, by-ay
	dsq := dx*dx + dy*dy

	if f.LenSq == 0 {
		// Degenerate source arc: distance is to the point P throughout —
		// mirrors bandPieces' own degenerate-canon-edge branch, roles swapped.
		quadB := 2 * (ax*dx + ay*dy)
		quadC := ax*ax + ay*ay
		pieces[0] = BandPiece{Lo: 0, Hi: 1, Kind: RegimeNearA, QuadA: dsq, QuadB: quadB, QuadC: quadC,
			NearX0: ax, NearY0: ay, NearX1: dx, NearY1: dy, HasNear: true}
		return pieces, 1, nil
	}

	s0 := (ax*f.Bx + ay*f.By) / f.LenSq
	s1 := (dx*f.Bx + dy*f.By) / f.LenSq
	if !allFinite(s0, s1) {
		return pieces, 0, fmt.Errorf("corridor cost: projection parameter not finite (s0=%v s1=%v)", s0, s1)
	}

	// ⚠ THIS SOLVE IS TRANSPOSED — the canon edge moves and the SOURCE arc is
	// the frame — so its regime boundaries are where the foot hits the SOURCE
	// arc's ends, not the canon edge's vertices. Nothing may read these
	// pieces' ends as a statement about canon geometry: they feed the gap
	// integral only.
	var ranges [3]bandRegimeRange
	nr := 0
	if s1 == 0 {
		switch {
		case s0 < 0:
			ranges[0] = bandRegimeRange{Kind: RegimeNearA, Lo: 0, Hi: 1}
		case s0 > 1:
			ranges[0] = bandRegimeRange{Kind: RegimeNearB, Lo: 0, Hi: 1}
		default:
			ranges[0] = bandRegimeRange{Kind: RegimeSpan, Lo: 0, Hi: 1}
		}
		nr = 1
	} else {
		tZero := -s0 / s1
		tOne := (1 - s0) / s1
		if s1 > 0 {
			ranges[0] = bandRegimeRange{Kind: RegimeNearA, Lo: math.Inf(-1), Hi: tZero}
			ranges[1] = bandRegimeRange{Kind: RegimeSpan, Lo: tZero, Hi: tOne}
			ranges[2] = bandRegimeRange{Kind: RegimeNearB, Lo: tOne, Hi: math.Inf(1)}
		} else {
			ranges[0] = bandRegimeRange{Kind: RegimeNearB, Lo: math.Inf(-1), Hi: tOne}
			ranges[1] = bandRegimeRange{Kind: RegimeSpan, Lo: tOne, Hi: tZero}
			ranges[2] = bandRegimeRange{Kind: RegimeNearA, Lo: tZero, Hi: math.Inf(1)}
		}
		nr = 3
	}

	for i := 0; i < nr; i++ {
		r := ranges[i]
		lo, hi := max(r.Lo, 0), min(r.Hi, 1)
		if lo > hi {
			continue
		}
		piece := BandPiece{Lo: lo, Hi: hi, Kind: r.Kind}
		switch r.Kind {
		case RegimeNearA:
			piece.QuadA, piece.QuadB, piece.QuadC = dsq, 2*(ax*dx+ay*dy), ax*ax+ay*ay
			piece.NearX0, piece.NearY0, piece.NearX1, piece.NearY1, piece.HasNear = ax, ay, dx, dy, true
		case RegimeNearB:
			ux, uy := ax-f.Bx, ay-f.By
			piece.QuadA, piece.QuadB, piece.QuadC = dsq, 2*(ux*dx+uy*dy), ux*ux+uy*uy
			piece.NearX0, piece.NearY0, piece.NearX1, piece.NearY1, piece.HasNear = ux, uy, dx, dy, true
		case RegimeSpan:
			c0 := ay*f.Bx - ax*f.By
			c1 := dy*f.Bx - dx*f.By
			piece.SpanC0, piece.SpanC1, piece.SpanLenSq = c0, c1, f.LenSq
		}
		pieces[n] = piece
		n++
	}
	return pieces, n, nil
}

// quadFormCoeffs returns bp's dist²(t) as plain (a,b,c) trinomial
// coefficients, valid over bp's own domain.
//
// For RegimeNearA/RegimeNearB this is quadA/quadB/quadC directly — no
// re-derivation. For RegimeSpan it squares the LINEAR form
// (spanC0+spanC1·t) into (a,b,c) — SAFE, because squaring a linear form is
// not the risky operation: the catastrophic cancellation [BandPiece]'s
// header warns against is specific to a DIFFERENT derivation
// (|X−a|² − s²|AB|², a subtraction of two large near-equal terms when a
// source runs far along a long canon edge), not to evaluating (c0+c1·t)²
// directly, which has no subtraction in it at all. This form is used ONLY
// to locate envelope CROSSINGS between competing candidates
// ([pieceCrossings]) — a new purpose, never a membership test, and never
// over the long-edge scenario the original warning concerns.
func quadFormCoeffs(bp BandPiece) (a, b, c float64) {
	if bp.Kind == RegimeSpan {
		return bp.SpanC1 * bp.SpanC1 / bp.SpanLenSq,
			2 * bp.SpanC0 * bp.SpanC1 / bp.SpanLenSq,
			bp.SpanC0 * bp.SpanC0 / bp.SpanLenSq
	}
	return bp.QuadA, bp.QuadB, bp.QuadC
}

// pieceCrossings returns the t-values strictly inside (lo,hi) where x and
// y's dist²(t) are equal, via the closed-form quadratic-formula solve of
// their difference: a crossing between any two pieces solves a quadratic,
// cheap and exact.
func pieceCrossings(x, y BandPiece, lo, hi float64) []float64 {
	ax, bx, cx := quadFormCoeffs(x)
	ay, by, cy := quadFormCoeffs(y)
	a, b, c := ax-ay, bx-by, cx-cy
	var roots []float64
	switch {
	case a == 0 && b == 0:
		// Equal everywhere in this pair, or never equal: no isolated root.
	case a == 0:
		roots = []float64{-c / b}
	default:
		disc := b*b - 4*a*c
		if disc >= 0 {
			sq := math.Sqrt(disc)
			roots = []float64{(-b - sq) / (2 * a), (-b + sq) / (2 * a)}
		}
	}
	var out []float64
	for _, root := range roots {
		if root > lo && root < hi {
			out = append(out, root)
		}
	}
	return out
}

// envelopeMinCost integrates (min_i distSq_i(t))^(k/2) / r^k over
// [globalLo, globalHi], given competing pieces that may each cover only
// part of that range (a piece not covering a given t is ignored there).
//
// Closed form throughout: breakpoints are the pieces' own domain edges plus
// every pairwise CROSSING ([pieceCrossings]), each an exact quadratic-
// formula root — not a sampled step. Between two consecutive breakpoints no
// piece's relative ordering can change (that is what a crossing IS), so
// evaluating once at the midpoint correctly identifies the minimum for the
// WHOLE sub-interval: exact interval bookkeeping, not sampling.
//
// ctx is checked once per row of the O(n²) crossing loop and every
// envelopeCancelStride sub-intervals of the evaluation: one bridge's gap can
// hold hundreds of thousands of pieces, and without these checks a cancel
// waited minutes for a single call. They read ctx and nothing else, so the
// result is unchanged.
func envelopeMinCost(ctx context.Context, pieces []BandPiece, globalLo, globalHi, r float64, k int) (float64, error) {
	if err := validateEvenK(k); err != nil {
		return 0, err
	}
	if !(r > 0) || !allFinite(r) {
		return 0, fmt.Errorf("corridor cost: gap radius must be finite and positive, got %v", r)
	}
	if len(pieces) == 0 || globalHi <= globalLo {
		return 0, nil
	}

	// ⚠ THE THREE THINGS BELOW ARE SPEED ONLY, AND ARE HELD TO THAT BY
	// TestEnvelopeMinCost_MatchesFrozenReferenceBitForBit, which grades this
	// function against a frozen copy of the pre-rewrite implementation bit for
	// bit. Not "within epsilon": the solver COMPARES these costs against each
	// other, so the last bit can flip a Dijkstra tie-break and move committed
	// geometry. If you change the maths here, that test is what must be
	// deliberately re-baselined.
	//
	// Profiled on one test import: this function was 29% of the import's
	// CPU, [pieceCrossings] a further 16%, and runtime.madvise 13% of all
	// samples — that last being the per-pair slice allocation, not the
	// arithmetic.
	//
	// 1. COEFFICIENTS ARE HOISTED. quadFormCoeffs was called twice per PAIR,
	//    so n² times for n pieces where n would do.
	// 2. CROSSINGS COME BACK IN A FIXED ARRAY. A quadratic has at most two
	//    roots, so the caller can own the storage and the pair loop allocates
	//    nothing at all.
	// 3. BREAKPOINTS ARE A SORTED SLICE, NOT A map[float64]bool. The map was
	//    hashing floats and allocating buckets to produce a set that is
	//    immediately sorted anyway; sort-then-dedup gives the identical
	//    sequence for any non-NaN input, and a NaN breakpoint would already be
	//    a fault upstream rather than something to preserve.
	coeffs := make([][3]float64, len(pieces))
	for i, p := range pieces {
		a, b, c := quadFormCoeffs(p)
		coeffs[i] = [3]float64{a, b, c}
	}

	breaks := make([]float64, 0, 2*len(pieces)+4)
	breaks = append(breaks, globalLo, globalHi)
	for i := range pieces {
		p := &pieces[i]
		if p.Lo > globalLo && p.Lo < globalHi {
			breaks = append(breaks, p.Lo)
		}
		if p.Hi > globalLo && p.Hi < globalHi {
			breaks = append(breaks, p.Hi)
		}
	}
	var roots [2]float64
	for i := 0; i < len(pieces); i++ {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("corridor cost: gap integral cancelled at piece %d of %d: %w", i, len(pieces), err)
		}
		for j := i + 1; j < len(pieces); j++ {
			lo := max(pieces[i].Lo, pieces[j].Lo, globalLo)
			hi := min(pieces[i].Hi, pieces[j].Hi, globalHi)
			if lo >= hi {
				continue
			}
			n := crossingsInto(coeffs[i], coeffs[j], lo, hi, &roots)
			breaks = append(breaks, roots[:n]...)
		}
	}
	sort.Float64s(breaks)
	sorted := breaks[:0]
	for i, b := range breaks {
		if i == 0 || b != breaks[i-1] {
			sorted = append(sorted, b)
		}
	}

	m := k / 2
	total := 0.0
	for i := 0; i+1 < len(sorted); i++ {
		if i%envelopeCancelStride == 0 {
			if err := ctx.Err(); err != nil {
				return 0, fmt.Errorf("corridor cost: gap integral cancelled at sub-interval %d of %d: %w", i, len(sorted)-1, err)
			}
		}
		lo, hi := sorted[i], sorted[i+1]
		if hi <= lo {
			continue
		}
		mid := (lo + hi) / 2
		best := -1
		bestVal := math.Inf(1)
		// By index, not by value: a BandPiece is 112 B since the near regimes
		// gained their linear offset, and copying one per piece per
		// sub-interval cost real CPU — this loop's flat time on one large
		// test import rose from 9 s to 20 s, the rest of the solve did not
		// move. The arithmetic is unchanged; only the copy is gone.
		for pi := range pieces {
			p := &pieces[pi]
			if mid < p.Lo || mid > p.Hi {
				continue
			}
			if v := p.distSq(mid); v < bestVal {
				bestVal, best = v, pi
			}
		}
		if best < 0 {
			return 0, fmt.Errorf("corridor cost: no candidate piece covers t=%.17g in [%.17g,%.17g] — the gap's candidate set is incomplete", mid, lo, hi)
		}
		win := pieces[best]
		win.Lo, win.Hi = lo, hi
		integral, err := integrateDistSqPow(win, m)
		if err != nil {
			return 0, err
		}
		total += integral
	}
	return total / math.Pow(r, float64(k)), nil
}

// envelopeCancelStride is how many sub-intervals envelopeMinCost evaluates
// between cancel checks; each sub-interval scans every piece, so a stride of
// 64 is a few milliseconds at most on the largest gaps.
const envelopeCancelStride = 64

// crossingsInto is [pieceCrossings] with the coefficients already solved and
// the storage owned by the caller: it writes the t-values strictly inside
// (lo,hi) where the two quadratics are equal into out, and returns how many
// there are (0, 1 or 2). Same roots, same order, no allocation — the pair loop
// in [envelopeMinCost] runs this O(n²) times and used to allocate two slices
// on every one of them.
func crossingsInto(x, y [3]float64, lo, hi float64, out *[2]float64) int {
	a, b, c := x[0]-y[0], x[1]-y[1], x[2]-y[2]
	n := 0
	keep := func(root float64) {
		if root > lo && root < hi {
			out[n] = root
			n++
		}
	}
	switch {
	case a == 0 && b == 0:
		// Equal everywhere in this pair, or never equal: no isolated root.
	case a == 0:
		keep(-c / b)
	default:
		disc := b*b - 4*a*c
		if disc >= 0 {
			sq := math.Sqrt(disc)
			keep((-b - sq) / (2 * a))
			keep((-b + sq) / (2 * a))
		}
	}
	return n
}

// importGapCost is the IMPORT-SIDE gap integral, and it is the exact mirror
// of [canonGapCost]: the emission term (d/R)^k integrated over ONE source arc
// P→Q of length arcLenM, against the canon arcs a bridge CLAIMS to have
// travelled.
//
// WHY IT EXISTS. Every other edge in the corridor solve that consumes import
// measure pays for it — ON pays emission per metre of import, OFF pays its
// raw metres — but a bridge used to relax with [bridgeCost], the canon-side
// integral and nothing else. A bridge was therefore priced exactly as PERFECT
// ADOPTION AT d = 0 over source that is by construction OUTSIDE the corridor.
// On one recorded test track, a bridge crossed 5,043 m of track for 4.33
// against 5,143 for the honest path, and the leg committed no geometry of its
// own. The rule the
// term restores introduces no constant: every metre of import measure a path
// consumes is paid for exactly once, by the edge that consumes it.
//
// The mirror is literal. [canonEdgeDistSqRegimes] is called with the roles
// SWAPPED — the source arc in the "moving" role and the canon arc in the
// "frame" role — and the resulting pieces go through the same
// [envelopeMinCost]. Same machinery, same R, same k, closed form throughout.
//
// No uLo/uHi sub-range here, unlike canonGapCost: canon edges are not
// densified and must be trimmed, but the source arcs a caller hands this
// function come from [sourceArcsBetween], which already trims EXACTLY to the
// gap's two import-measure positions. The whole [0,1] of each arc is in the
// gap by construction.
//
// An empty canon set is a caller error for the same reason canonGapCost says
// so about its source set: "no claimed canon" means the caller never
// established there was a bridge to price.
func importGapCost(ctx context.Context, p, q Point, canonArcs [][2]Point, arcLenM, r float64, k int) (float64, error) {
	if len(canonArcs) == 0 {
		return 0, fmt.Errorf("corridor cost: import gap arc has no claimed canon arcs")
	}
	if !allFinite(arcLenM) || arcLenM < 0 {
		return 0, fmt.Errorf("corridor cost: import gap arc length must be finite and non-negative, got %v", arcLenM)
	}
	if arcLenM == 0 {
		return 0, nil
	}
	var pieces []BandPiece
	for _, arc := range canonArcs {
		regimes, n, err := canonEdgeDistSqRegimes(p, q, arc[0], arc[1])
		if err != nil {
			return 0, err
		}
		pieces = append(pieces, regimes[:n]...)
	}
	integral, err := envelopeMinCost(ctx, pieces, 0, 1, r, k)
	if err != nil {
		return 0, err
	}
	return arcLenM * integral, nil
}

// canonGapCost is the canon-side gap integral over the [uLo,uHi] ⊆ [0,1]
// sub-range of ONE canon edge A→B of length edgeLenM, against a GIVEN set of
// competing source arcs. uLo=0, uHi=1 integrates the whole edge.
//
// The [uLo,uHi] sub-range matters in practice, not just in principle: canon
// edges are NOT densified (one measured real edge is 1,956.8 m long), so a
// caller pricing a short gap that only clips the START or END of a long
// edge — corridorsolve.go's [canonGapCostBetween] — MUST trim to the true
// overlap or a near-zero gap would cost nearly a whole edge. The scale factor
// stays edgeLenM (the WHOLE edge's length) regardless of the sub-range:
// ds/dt = edgeLenM is constant across the entire edge (measure is affine in
// t), so edgeLenM * ∫[uLo,uHi] is exact for any sub-range, not just [0,1].
//
// Candidate-set DISCOVERY — which arcs are ever competitive near a
// gap — is explicitly OUT OF SCOPE here: discovery is not itself closed-form
// (a probe-and-pad heuristic missed the true nearest arc by 138% on a real
// 1,956.8 m canon edge) and needs its own certified box-pruning. This is a
// pure function over a GIVEN candidate set. Callers hand this function the
// arcs already known to compete; an empty set is a caller error, not an
// expected absence, because "no known candidates" means the caller never
// established there was a gap to price in the first place.
func canonGapCost(ctx context.Context, a, b Point, srcArcs [][2]Point, edgeLenM, uLo, uHi, r float64, k int) (float64, error) {
	if len(srcArcs) == 0 {
		return 0, fmt.Errorf("corridor cost: canon gap edge has no competing source arcs")
	}
	if !allFinite(uLo, uHi) || uLo < 0 || uHi > 1 || uLo > uHi {
		return 0, fmt.Errorf("corridor cost: canon gap sub-range [%v,%v] must be within [0,1] and non-inverted", uLo, uHi)
	}
	var pieces []BandPiece
	for _, arc := range srcArcs {
		regimes, n, err := canonEdgeDistSqRegimes(a, b, arc[0], arc[1])
		if err != nil {
			return 0, err
		}
		pieces = append(pieces, regimes[:n]...)
	}
	integral, err := envelopeMinCost(ctx, pieces, uLo, uHi, r, k)
	if err != nil {
		return 0, err
	}
	return edgeLenM * integral, nil
}
