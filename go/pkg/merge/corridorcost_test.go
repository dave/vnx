package merge

// Tests for the four corridor-matcher cost
// terms. See corridorcost.go's own header for the closed-form argument this
// file exists to hold up.

import (
	"context"
	"math"
	"testing"
)

// ---- k validation ---------------------------------------------------------

func TestValidateEvenK_RejectsOddZeroNegative(t *testing.T) {
	for _, k := range []int{-2, -1, 0, 1, 3, 5} {
		if err := validateEvenK(k); err == nil {
			t.Errorf("validateEvenK(%d): want error, got nil", k)
		}
	}
	for _, k := range []int{2, 4, 6, 8} {
		if err := validateEvenK(k); err != nil {
			t.Errorf("validateEvenK(%d): want nil, got %v", k, err)
		}
	}
}

// ---- integrateDistSqPow: closed form vs dense numeric reference ----------

// simpsonIntegrate is a dense reference integrator, independent of
// [integrateDistSqPow]'s closed-form path, used only to cross-check it —
// never a production code path.
func simpsonIntegrate(f func(float64) float64, lo, hi float64, n int) float64 {
	if n%2 != 0 {
		n++
	}
	h := (hi - lo) / float64(n)
	sum := f(lo) + f(hi)
	for i := 1; i < n; i++ {
		x := lo + float64(i)*h
		if i%2 == 0 {
			sum += 2 * f(x)
		} else {
			sum += 4 * f(x)
		}
	}
	return sum * h / 3
}

// nearPieceFromOffset builds a near-regime piece the way bandPieces does: from
// the linear offset X(t) − V = (x0 + x1·t, y0 + y1·t), with the trinomial
// expanded from it. A near piece carries both forms (see [BandPiece]).
func nearPieceFromOffset(kind BandRegime, lo, hi, x0, y0, x1, y1 float64) BandPiece {
	return BandPiece{Lo: lo, Hi: hi, Kind: kind,
		QuadA: x1*x1 + y1*y1, QuadB: 2 * (x0*x1 + y0*y1), QuadC: x0*x0 + y0*y0,
		NearX0: x0, NearY0: y0, NearX1: x1, NearY1: y1, HasNear: true}
}

func TestIntegrateDistSqPow_NearARegime_MatchesDenseReference(t *testing.T) {
	bp := nearPieceFromOffset(RegimeNearA, 0.1, 0.9, 2, -1.5, -1, 2)
	for _, m := range []int{1, 2, 3} {
		got, err := integrateDistSqPow(bp, m)
		if err != nil {
			t.Fatalf("m=%d: %v", m, err)
		}
		want := simpsonIntegrate(func(x float64) float64 { return math.Pow(bp.DistSq(x), float64(m)) }, bp.Lo, bp.Hi, 2000)
		if math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
			t.Errorf("m=%d: closed form %.12g, dense reference %.12g", m, got, want)
		}
	}
}

func TestIntegrateDistSqPow_SpanRegime_MatchesDenseReference(t *testing.T) {
	bp := BandPiece{Lo: -2, Hi: 3, Kind: RegimeSpan, SpanC0: 4, SpanC1: -1.5, SpanLenSq: 2.3}
	for _, m := range []int{1, 2, 3} {
		got, err := integrateDistSqPow(bp, m)
		if err != nil {
			t.Fatalf("m=%d: %v", m, err)
		}
		want := simpsonIntegrate(func(x float64) float64 { return math.Pow(bp.DistSq(x), float64(m)) }, bp.Lo, bp.Hi, 2000)
		if math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
			t.Errorf("m=%d: closed form %.12g, dense reference %.12g", m, got, want)
		}
	}
}

func TestIntegrateDistSqPow_ZeroMeasurePieceIsZero(t *testing.T) {
	bp := BandPiece{Lo: 0.5, Hi: 0.5, Kind: RegimeSpan, SpanC0: 1, SpanC1: 1, SpanLenSq: 1}
	got, err := integrateDistSqPow(bp, 2)
	if err != nil || got != 0 {
		t.Fatalf("got %v, %v; want 0, nil", got, err)
	}
}

// ---- emission --------------------------------------------------------

// TestEmissionCost_MatchesDenseNumericIntegration builds a real
// CorridorPiece from the fixtures corridorinterval_test.go already trusts
// (straight overlap) and checks emissionCost against a dense midpoint sum
// of (d/R)^k per metre of import measure, independent of the closed-form path.
func TestEmissionCost_MatchesDenseNumericIntegration(t *testing.T) {
	const R = 15.0
	pc := prepOne(t, mPt(30, 5), mPt(70, 5))
	sa, err := NewSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil {
		t.Fatalf("candidateCorridorIntervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("got %d intervals, want 1", len(ivs))
	}

	for _, k := range []int{2, 4} {
		// The emission cost of adopting the WHOLE interval: emissionCost summed
		// over its pieces.
		total := 0.0
		for _, p := range ivs[0].Pieces {
			c, err := emissionCost(p, R, k)
			if err != nil {
				t.Fatalf("k=%d: emissionCost (srcEdge=%d canonEdge=%d): %v", k, p.SrcEdge, p.CanonEdge, err)
			}
			total += c
		}
		// Dense reference: sum (d/R)^k * dMeasure over many samples across every
		// piece, using bp.distSq directly (the same primitive the closed form
		// integrates) but evaluated numerically rather than symbolically.
		want := 0.0
		const n = 4000
		for _, p := range ivs[0].Pieces {
			bp := p.Regime
			if bp.Hi == bp.Lo {
				continue
			}
			rate := (p.ImportMeasureAtHi - p.ImportMeasureAtLo) / (bp.Hi - bp.Lo)
			want += simpsonIntegrate(func(x float64) float64 {
				return math.Pow(bp.DistSq(x)/(R*R), float64(k)/2)
			}, bp.Lo, bp.Hi, n) * rate
		}
		if math.Abs(total-want) > 1e-4*math.Max(1, math.Abs(want)) {
			t.Errorf("k=%d: closed form %.9g, dense reference %.9g", k, total, want)
		}
	}
}

func TestEmissionCost_RejectsOddK(t *testing.T) {
	pc := prepOne(t, mPt(30, 5), mPt(70, 5))
	sa, err := NewSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, CorridorRadiusM)
	if err != nil || len(ivs) != 1 {
		t.Fatalf("fixture setup: ivs=%v err=%v", ivs, err)
	}
	if _, err := emissionCost(ivs[0].Pieces[0], CorridorRadiusM, 3); err == nil {
		t.Fatalf("k=3: want error, got nil")
	}
}

// ---- backtrack --------------------------------------------------------

func TestBacktrackMeasureM_ZeroWhenAligned_PositiveWhenAgainst(t *testing.T) {
	fwd := CorridorPiece{CanonMeasureAtLo: 10, CanonMeasureAtHi: 25} // canon measure INCREASES
	if got := backtrackMeasureM(fwd, true); got != 0 {
		t.Errorf("aligned with dirIncreasing=true: got %v, want 0", got)
	}
	if got := backtrackMeasureM(fwd, false); got != 15 {
		t.Errorf("against dirIncreasing=false: got %v, want 15", got)
	}

	rev := CorridorPiece{CanonMeasureAtLo: 30, CanonMeasureAtHi: 12} // canon measure DECREASES
	if got := backtrackMeasureM(rev, false); got != 0 {
		t.Errorf("aligned with dirIncreasing=false: got %v, want 0", got)
	}
	if got := backtrackMeasureM(rev, true); got != 18 {
		t.Errorf("against dirIncreasing=true: got %v, want 18", got)
	}

	flat := CorridorPiece{CanonMeasureAtLo: 5, CanonMeasureAtHi: 5} // clamped to one vertex
	if backtrackMeasureM(flat, true) != 0 || backtrackMeasureM(flat, false) != 0 {
		t.Errorf("flat piece must cost 0 either direction")
	}
}

func TestBacktrackCost_WobbleVsArmJump(t *testing.T) {
	// The design: "0.47 m of GPS wobble costs 0.47" — "an
	// 800 m arm-jump costs 800" (measured worst case 781 m).
	wobble := CorridorPiece{CanonMeasureAtLo: 100, CanonMeasureAtHi: 99.53} // 0.47 m backward
	if got := backtrackCost(wobble, true); math.Abs(got-0.47) > 1e-9 {
		t.Errorf("wobble backtrack cost = %.6g, want 0.47", got)
	}
	armJump := CorridorPiece{CanonMeasureAtLo: 900, CanonMeasureAtHi: 119} // 781 m backward
	if got := backtrackCost(armJump, true); math.Abs(got-781) > 1e-9 {
		t.Errorf("arm-jump backtrack cost = %.6g, want 781", got)
	}
}

// ---- state change / bridge inequality ----------------------------------

// TestBridgeInequality_Basic grades the bridge/no-bridge inequality: a bridge
// ([bridgeCost]) wins when it is cheaper than the OFF path it replaces
// ([noBridgeCost]).
func TestBridgeInequality_Basic(t *testing.T) {
	bridges := func(gap, off float64) bool { return bridgeCost(gap) < noBridgeCost(off) }
	// Cheap bridge: small gap integral, big OFF detour avoided.
	if !bridges(10, 500) {
		t.Errorf("small gap integral (10) vs big no-bridge cost (2P+500): want bridge")
	}
	// Expensive bridge: huge gap integral, trivial OFF detour.
	if bridges(1e6, 1) {
		t.Errorf("huge gap integral vs tiny no-bridge cost: want NOT bridge")
	}
	// The inequality's own arithmetic, written down and checked directly.
	gap, off := 40.0, 5.0
	want := gap < 2*stateChangeCost()+off
	if got := bridges(gap, off); got != want {
		t.Errorf("bridges(%v,%v) = %v, want %v (bridgeCost=%v, noBridgeCost=%v)",
			gap, off, got, want, bridgeCost(gap), noBridgeCost(off))
	}
}

// ---- canon-side gap: single-arc geometry -----------------------------

// TestCanonEdgeDistSqRegimes_MatchesClosestPointOnSeg builds a canon edge
// long enough to pass through all three regimes against one fixed source
// arc (nearA clamp, span, nearB clamp) and checks the exposed dist²(u)
// against an independent evaluation (ClosestPointOnSeg, squared) at several
// u within each regime — the same style of check
// TestCorridorPieceDistSq_MatchesClosestPointOnSeg (corridorinterval_test.go)
// uses for the source-side pieces.
func TestCanonEdgeDistSqRegimes_MatchesClosestPointOnSeg(t *testing.T) {
	// Canon: long horizontal edge at y=5, x from -100 to 100.
	a, b := mPt(-100, 5), mPt(100, 5)
	// Source: shorter horizontal arc at y=0, x from -20 to 20 — centred
	// under the canon edge, so u sweeps nearA (x<-20) -> span (-20<=x<=20)
	// -> nearB (x>20).
	p, q := mPt(-20, 0), mPt(20, 0)

	regimes, n, err := canonEdgeDistSqRegimes(a, b, p, q)
	if err != nil {
		t.Fatalf("canonEdgeDistSqRegimes: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d regimes, want 3 (nearA, span, nearB): %+v", n, regimes[:n])
	}

	seenKinds := map[BandRegime]bool{}
	checked := 0
	for _, bp := range regimes[:n] {
		seenKinds[bp.Kind] = true
		for step := 0; step <= 10; step++ {
			frac := float64(step) / 10
			u := bp.Lo + frac*(bp.Hi-bp.Lo)
			lat := a.Lat + u*(b.Lat-a.Lat)
			lon := a.Lon + u*(b.Lon-a.Lon)
			want, _ := ClosestPointOnSeg(p.Lat, p.Lon, q.Lat, q.Lon, lat, lon)
			got := math.Sqrt(bp.DistSq(u))
			if math.Abs(got-want) > 1e-6 {
				t.Fatalf("kind=%v u=%.4g: distSq gives %.9g, closestPointOnSeg gives %.9g", bp.Kind, u, got, want)
			}
			checked++
		}
	}
	if !seenKinds[RegimeNearA] || !seenKinds[RegimeSpan] || !seenKinds[RegimeNearB] {
		t.Fatalf("fixture did not exercise all three regimes: saw %v", seenKinds)
	}
	if checked == 0 {
		t.Fatalf("no points checked")
	}
}

func TestCanonEdgeDistSqRegimes_DegenerateSourceArcIsPointTest(t *testing.T) {
	a, b := mPt(-50, 5), mPt(50, 5)
	p := mPt(0, 0)
	regimes, n, err := canonEdgeDistSqRegimes(a, b, p, p) // zero-length source
	if err != nil {
		t.Fatalf("canonEdgeDistSqRegimes: %v", err)
	}
	if n != 1 || regimes[0].Kind != RegimeNearA {
		t.Fatalf("degenerate source arc: got n=%d kind=%v, want 1 regimeNearA", n, regimes[0].Kind)
	}
	// At u=0.5 (canon point (0,5)), distance to the point (0,0) is 5 m.
	// Tolerance above 1e-6: mPt's fixed 111320 m/deg differs slightly from
	// MetresPerDeg's haversine-radius-derived constant this function
	// actually uses (corridorinterval_test.go's own header explains this).
	got := math.Sqrt(regimes[0].DistSq(0.5))
	if math.Abs(got-5) > corridorTol {
		t.Errorf("distance to degenerate source point = %.6g, want ~5", got)
	}
}

// ---- canon-side gap: envelope / triangular integral ---------------------

// tentPieces builds the min-envelope of two linear "distance to a vertex"
// ramps that cross at t=L/2 with value h — the closed-form realisation of
// the design's "honest triangular integral": departure is 0 at
// both ends of a length-L gap and peaks at h in the middle, exactly the
// bracketed hairpin/bridge shape. The derivation:
// ∫(min(a·t, a·L−a·t)/R)^k dt over [0,L], a=2h/L, works out to the closed
// form L/(k+1)·(h/R)^k for even k.
func tentPieces(L, h float64) []BandPiece {
	a := 2 * h / L
	return []BandPiece{
		{Lo: 0, Hi: L, Kind: RegimeSpan, SpanC0: 0, SpanC1: a, SpanLenSq: 1},
		{Lo: 0, Hi: L, Kind: RegimeSpan, SpanC0: 2 * h, SpanC1: -a, SpanLenSq: 1},
	}
}

// tentCost is the closed form of the tent's cost integral,
// ∫₀ᴸ (d(x)/R)^k dx = L·hᵏ / (Rᵏ·(k+1)) for the symmetric tent [tentPieces]
// builds. It is NOT an independent oracle on its own — it is pinned against a
// dense Simpson reference by
// [TestEnvelopeMinCost_TentMatchesTriangularFormulaAndDenseReference], which is
// what makes it usable as the expectation elsewhere.
//
// It exists because expectations written as typed-in decimals bake R into
// themselves silently: every one of them is proportional to R^-k, so moving
// [CorridorRadiusM] reddens them with no behaviour change at all. Retyping
// today's numbers would rebuild exactly that trap for the next person who
// moves R.
func tentCost(L, h, R float64, k int) float64 {
	return L / float64(k+1) * math.Pow(h/R, float64(k))
}

func TestEnvelopeMinCost_TentMatchesTriangularFormulaAndDenseReference(t *testing.T) {
	const R = CorridorRadiusM
	cases := []struct{ L, h float64 }{{40, 60}, {400, 20}, {17, 3}}
	for _, c := range cases {
		pieces := tentPieces(c.L, c.h)
		for _, k := range []int{2, 4} {
			got, err := envelopeMinCost(context.Background(), pieces, 0, c.L, R, k)
			if err != nil {
				t.Fatalf("L=%v h=%v k=%d: %v", c.L, c.h, k, err)
			}
			formula := tentCost(c.L, c.h, R, k)
			if math.Abs(got-formula) > 1e-6*math.Max(1, formula) {
				t.Errorf("L=%v h=%v k=%d: envelope %.9g, closed-form triangle %.9g", c.L, c.h, k, got, formula)
			}
			// Independent dense reference: min of the two RAW linear ramps
			// (not the envelope machinery), sampled and Simpson-integrated.
			a := 2 * c.h / c.L
			dense := simpsonIntegrate(func(x float64) float64 {
				d1 := math.Abs(a * x)
				d2 := math.Abs(2*c.h - a*x)
				d := math.Min(d1, d2)
				return math.Pow(d/R, float64(k))
			}, 0, c.L, 4000)
			if math.Abs(got-dense) > 1e-4*math.Max(1, dense) {
				t.Errorf("L=%v h=%v k=%d: envelope %.9g, dense reference %.9g", c.L, c.h, k, got, dense)
			}
		}
	}
}

// TestCostOrdering_HairpinVsSpur_RedAtK2GreenAtK4 reproduces
// the cost terms' stated acceptance criterion: a 40 m canon
// stretch departing up to 60 m (the hairpin/spike that must NOT be adopted)
// must score ABOVE a 400 m stretch departing up to 20 m (the gentle drift that
// SHOULD be adopted). At k=2 the ordering is inverted (RED, matching the
// design's own numbers: hairpin 213 < spur 237); at k=4 it flips (GREEN: 2048 >
// 253). Both numbers reproduce to <1%.
//
// ⚠ THIS FIXTURE IS GEOMETRICALLY UNREALIZABLE, SO IT GRADES THE INTEGRATOR,
// NOT THE DECISION RULE. It is kept because the design's cited numbers are its
// numbers and dropping it would lose that correspondence — but it must NOT be
// cited as evidence that k=4 orders real hairpins above real drifts. See
// TestCostOrdering_RealizableGeometryDoesNotFlipWithK below, which is the
// honest version of the same question.
//
// Reason: distance from a moving point to a FIXED SET is 1-Lipschitz, and a
// min-envelope of several such distances is still 1-Lipschitz (min of
// 1-Lipschitz functions is 1-Lipschitz). Canon is measure-length parameterised, so
// departure can rise at most 1 m per metre of canon measure. A tent of half-length
// 20 m reaching 60 m needs slope 3. No source geometry produces it, with any
// number of competing arcs. Measured 2026-08-19: 200 000 random
// configurations of 1–6 competing arcs, max |Δd/Δmeasure| = 1.000000000.
// That bound is a fact of plane geometry, not of this package, so no test
// re-measures it: the test that did called no production code.
func TestCostOrdering_HairpinVsSpur_RedAtK2GreenAtK4(t *testing.T) {
	const R = CorridorRadiusM
	hairpin := tentPieces(40, 60)
	spur := tentPieces(400, 20)

	hp2, err := envelopeMinCost(context.Background(), hairpin, 0, 40, R, 2)
	if err != nil {
		t.Fatalf("hairpin k=2: %v", err)
	}
	sp2, err := envelopeMinCost(context.Background(), spur, 0, 400, R, 2)
	if err != nil {
		t.Fatalf("spur k=2: %v", err)
	}
	t.Logf("k=2: hairpin=%.6g spur=%.6g", hp2, sp2)
	if want := tentCost(40, 60, R, 2); math.Abs(hp2-want) > 1e-6*want {
		t.Errorf("k=2 hairpin = %.6g, want %.6g", hp2, want)
	}
	if want := tentCost(400, 20, R, 2); math.Abs(sp2-want) > 1e-6*want {
		t.Errorf("k=2 spur = %.6g, want %.6g", sp2, want)
	}
	if !(hp2 < sp2) {
		t.Errorf("k=2 was expected to be RED (inverted): hairpin %.6g should score BELOW spur %.6g, but did not", hp2, sp2)
	}

	hp4, err := envelopeMinCost(context.Background(), hairpin, 0, 40, R, 4)
	if err != nil {
		t.Fatalf("hairpin k=4: %v", err)
	}
	sp4, err := envelopeMinCost(context.Background(), spur, 0, 400, R, 4)
	if err != nil {
		t.Fatalf("spur k=4: %v", err)
	}
	t.Logf("k=4: hairpin=%.6g spur=%.6g", hp4, sp4)
	if want := tentCost(40, 60, R, 4); math.Abs(hp4-want) > 1e-6*want {
		t.Errorf("k=4 hairpin = %.6g, want %.6g", hp4, want)
	}
	if want := tentCost(400, 20, R, 4); math.Abs(sp4-want) > 1e-6*want {
		t.Errorf("k=4 spur = %.6g, want %.6g", sp4, want)
	}

	// The correspondence to the design's OWN cited numbers, which
	// is the whole reason this unrealizable fixture is kept. Those numbers were
	// computed at R=15, so 15 here is a deliberate literal and must NOT be
	// changed to R when [CorridorRadiusM] moves — it is a historical citation,
	// not a live constant. If it ever stops matching, the integrator changed,
	// not the radius.
	for _, c := range []struct {
		name     string
		L, h     float64
		k        int
		planCite float64
	}{
		{"hairpin k=2", 40, 60, 2, 213.33},
		{"spur k=2", 400, 20, 2, 237.04},
		{"hairpin k=4", 40, 60, 4, 2048},
		{"spur k=4", 400, 20, 4, 252.84},
	} {
		if got := tentCost(c.L, c.h, 15, c.k); math.Abs(got-c.planCite) > 0.01*math.Max(1, c.planCite) {
			t.Errorf("%s at the design's R=15: %.6g, design cites %.6g", c.name, got, c.planCite)
		}
	}
	if !(hp4 > sp4) {
		t.Errorf("k=4 was expected to be GREEN: hairpin %.6g must score ABOVE spur %.6g", hp4, sp4)
	}
}

// TestCostOrdering_RealizableGeometryDoesNotFlipWithK is the honest version of
// the ordering criterion, and it is a FINDING, not a passing gate: once the
// hairpin is held to a buildable shape, raising k does not reorder it above the
// spur — because with both tents at the same peak height the cost ratio is
// L_hairpin/L_spur = 0.1 for EVERY k, independent of k entirely.
//
// This does not invalidate the cost code — the integrator is correct and pinned
// against a dense reference. It invalidates that method for CHOOSING k, which
// is the weight calibration's job (raised 2026-08-19).
func TestCostOrdering_RealizableGeometryDoesNotFlipWithK(t *testing.T) {
	const R = CorridorRadiusM
	// Steepest hairpin a 40 m gap can actually have when its ends sit ON the
	// source (departure 0): half-length 20 m at slope 1 → peak 20 m.
	hairpin := tentPieces(40, 20)
	spur := tentPieces(400, 20)

	for _, k := range []int{2, 4, 6, 8, 10} {
		hp, err := envelopeMinCost(context.Background(), hairpin, 0, 40, R, k)
		if err != nil {
			t.Fatalf("hairpin k=%d: %v", k, err)
		}
		sp, err := envelopeMinCost(context.Background(), spur, 0, 400, R, k)
		if err != nil {
			t.Fatalf("spur k=%d: %v", k, err)
		}
		t.Logf("k=%2d: hairpin=%10.2f spur=%10.2f ratio=%.4f", k, hp, sp, hp/sp)
		if hp > sp {
			t.Fatalf("k=%d unexpectedly ordered a realizable hairpin above the spur "+
				"(%.4g > %.4g) — the finding this test records has changed and "+
				"the choice of k must be revisited", k, hp, sp)
		}
		// The ratio is k-independent at equal peak height. Pin it, so that if
		// someone changes the cost shape this test says so rather than passing.
		if math.Abs(hp/sp-0.1) > 1e-9 {
			t.Errorf("k=%d: ratio %.12g, want exactly 0.1 (= L_hairpin/L_spur)", k, hp/sp)
		}
	}
}

// ---- canonGapCost: full wrapper over real geometry -----------------------

func TestCanonGapCost_NoCandidatesIsError(t *testing.T) {
	a, b := mPt(0, 0), mPt(10, 0)
	if _, err := canonGapCost(context.Background(), a, b, nil, 10, 0, 1, CorridorRadiusM, 2); err == nil {
		t.Fatalf("empty candidate set: want error, got nil")
	}
}

func TestCanonGapCost_SingleArcMatchesDirectRegimeIntegral(t *testing.T) {
	a, b := mPt(-100, 5), mPt(100, 5)
	p, q := mPt(-20, 0), mPt(20, 0)
	edgeLenM := HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)

	got, err := canonGapCost(context.Background(), a, b, [][2]Point{{p, q}}, edgeLenM, 0, 1, CorridorRadiusM, 2)
	if err != nil {
		t.Fatalf("canonGapCost: %v", err)
	}

	regimes, n, err := canonEdgeDistSqRegimes(a, b, p, q)
	if err != nil {
		t.Fatalf("canonEdgeDistSqRegimes: %v", err)
	}
	want := 0.0
	for _, bp := range regimes[:n] {
		integral, err := integrateDistSqPow(bp, 1) // k=2 -> m=1
		if err != nil {
			t.Fatalf("integrateDistSqPow: %v", err)
		}
		want += integral
	}
	want = want / (CorridorRadiusM * CorridorRadiusM) * edgeLenM
	if math.Abs(got-want) > 1e-6*math.Max(1, want) {
		t.Errorf("canonGapCost = %.9g, direct sum = %.9g", got, want)
	}
}

// ---- canonGapCost: uLo/uHi sub-range --------------------------------------

// TestCanonGapCost_SubRangeSumsToWholeEdge checks the property
// canonGapCostBetween (corridorsolve.go) leans on: integrating [0,mid] plus
// [mid,1] must equal integrating [0,1] exactly (additivity of a definite
// integral over adjacent sub-ranges), so trimming a bridge's first/last
// canon edge to its true overlap prices EXACTLY that overlap — not the
// whole edge, and not a resampled approximation of the overlap either.
func TestCanonGapCost_SubRangeSumsToWholeEdge(t *testing.T) {
	a, b := mPt(-100, 5), mPt(100, 5)
	p, q := mPt(-20, 0), mPt(20, 0)
	edgeLenM := HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
	srcArcs := [][2]Point{{p, q}}

	whole, err := canonGapCost(context.Background(), a, b, srcArcs, edgeLenM, 0, 1, CorridorRadiusM, 2)
	if err != nil {
		t.Fatalf("canonGapCost whole: %v", err)
	}
	for _, mid := range []float64{0.1, 0.37, 0.5, 0.9} {
		lo, err := canonGapCost(context.Background(), a, b, srcArcs, edgeLenM, 0, mid, CorridorRadiusM, 2)
		if err != nil {
			t.Fatalf("canonGapCost [0,%v]: %v", mid, err)
		}
		hi, err := canonGapCost(context.Background(), a, b, srcArcs, edgeLenM, mid, 1, CorridorRadiusM, 2)
		if err != nil {
			t.Fatalf("canonGapCost [%v,1]: %v", mid, err)
		}
		if got := lo + hi; math.Abs(got-whole) > 1e-6*math.Max(1, whole) {
			t.Errorf("mid=%v: [0,mid]+[mid,1] = %.9g, want whole-edge %.9g", mid, got, whole)
		}
	}
}

// TestCanonGapCost_ZeroWidthSubRangeIsZero: a sub-range with uLo==uHi is a
// zero-measure piece (mirrors integrateDistSqPow's own bp.hi==bp.lo
// convention) — needed so a bridge whose two ends coincide in canon measure
// (candidateBridgeCost's own early return) is consistent with what this
// function would say if asked directly, not merely bypassed by a caller-side
// special case.
func TestCanonGapCost_ZeroWidthSubRangeIsZero(t *testing.T) {
	a, b := mPt(-100, 5), mPt(100, 5)
	p, q := mPt(-20, 0), mPt(20, 0)
	got, err := canonGapCost(context.Background(), a, b, [][2]Point{{p, q}}, 200, 0.4, 0.4, CorridorRadiusM, 2)
	if err != nil {
		t.Fatalf("canonGapCost: %v", err)
	}
	if got != 0 {
		t.Errorf("zero-width sub-range: want exactly 0, got %v", got)
	}
}

// TestCanonGapCost_InvertedSubRangeIsError: uLo > uHi is a caller bug, not an
// expected absence — canonGapCostBetween never constructs one (its eLo<=eHi
// clamp), so a caller reaching this is a genuine inconsistency worth an
// error, not a silent fallback.
func TestCanonGapCost_InvertedSubRangeIsError(t *testing.T) {
	a, b := mPt(-100, 5), mPt(100, 5)
	p, q := mPt(-20, 0), mPt(20, 0)
	if _, err := canonGapCost(context.Background(), a, b, [][2]Point{{p, q}}, 200, 0.6, 0.4, CorridorRadiusM, 2); err == nil {
		t.Fatalf("inverted sub-range [0.6,0.4]: want error, got nil")
	}
}
