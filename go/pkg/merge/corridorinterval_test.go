package merge

// Tests for import-measure corridor intervals with
// an exact, oriented canon-measure correspondence.
//
// Tolerances throughout are set ABOVE the equirectangular/haversine noise
// floor (a measured 0.026 m gap on an 880 m edge), never at
// 1e-6 — the intervals' own acceptance criterion. mPt places
// fixtures using a fixed linear lat/lon-to-metres approximation
// (follow_test.go), while newSourceMeasure/prepCandidates measure real haversine
// distance between them, so even an exactly-computed corridor boundary
// disagrees with a hand-computed planar expectation by a small, real amount —
// not a bug to chase to 1e-6.

import (
	"context"
	"math"
	"testing"
)

const corridorTol = 1e-2 // metres; see header

// ---- expected absences: fallbacks, not errors --------------------------

func TestCorridorIntervals_EmptyLegIsNilNotError(t *testing.T) {
	pc := prepOne(t, mPt(0, 0), mPt(100, 0))
	ivs, err := corridorIntervalsForLeg(t.Context(), nil, []*PreppedCandidate{pc}, 15)
	if err != nil || ivs != nil {
		t.Fatalf("empty leg: got ivs=%v err=%v, want nil,nil", ivs, err)
	}
}

func TestCorridorIntervals_SinglePointLegIsNilNotError(t *testing.T) {
	pc := prepOne(t, mPt(0, 0), mPt(100, 0))
	ivs, err := corridorIntervalsForLeg(t.Context(), []Point{mPt(5, 5)}, []*PreppedCandidate{pc}, 15)
	if err != nil || ivs != nil {
		t.Fatalf("single-point leg: got ivs=%v err=%v, want nil,nil", ivs, err)
	}
}

func TestCorridorIntervals_NoCandidatesIsNilNotError(t *testing.T) {
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs, err := corridorIntervals(t.Context(), sa, nil, 15)
	if err != nil || ivs != nil {
		t.Fatalf("no candidates: got ivs=%v err=%v, want nil,nil", ivs, err)
	}
}

func TestCorridorIntervals_DegenerateCandidateSkippedAsNil(t *testing.T) {
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	// prepCandidates itself would leave a <2-vertex or zero-length arc nil;
	// a caller handing this function a nil slot must see it skipped, not error.
	ivs, err := corridorIntervals(t.Context(), sa, []*PreppedCandidate{nil}, 15)
	if err != nil || ivs != nil {
		t.Fatalf("nil candidate slot: got ivs=%v err=%v, want nil,nil", ivs, err)
	}
}

// ---- straight overlap: one interval, exact crossings -------------------

// TestCorridorIntervals_StraightOverlapOneIntervalExactCrossings covers the
// simplest acceptance case and, in the same fixture, the join ACROSS source
// edges (the source is two edges, the candidate one) and the ORIENTED
// correspondence (canon measure must increase with import measure here, since both
// polylines run the same west→east direction).
func TestCorridorIntervals_StraightOverlapOneIntervalExactCrossings(t *testing.T) {
	const R = 15.0
	// Candidate: a single edge from x=30 to x=70 at y=5.
	pc := prepOne(t, mPt(30, 5), mPt(70, 5))
	// Source: two edges, x=0→50→100 at y=0, so the true corridor
	// [30-√200, 70+√200] ≈ [15.86, 84.14] straddles the source's own midpoint
	// vertex at x=50 and must be found by JOINING the two source edges' pieces.
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}

	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil {
		t.Fatalf("candidateCorridorIntervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("got %d intervals, want 1: %+v", len(ivs), ivs)
	}
	iv := ivs[0]

	// The ground truth is a DENSE SAMPLED SCAN against the true polyline
	// distance, not a hand-derived circle equation: mPt's fixed lat/lon-to-
	// metres constant (111320 m/deg) differs from the haversine radius
	// ClosestPointOnSeg/HaversineMetres actually use (6371000 m via
	// MetresPerDeg), so a "nominal 100 m" fixture measures as 99.888 m —
	// confirmed independently before writing this expectation. Hand-deriving
	// [30-√200, 70+√200] against the NOMINAL metres would be comparing this
	// file's exact solve to the wrong reference.
	dense := make([]Point, 4001)
	for i := range dense {
		dense[i] = mPt(100*float64(i)/4000, 0)
	}
	loRef, hiRef, ok := denseBandBoundary(dense, pc.Verts, R)
	if !ok {
		t.Fatalf("fixture sanity: dense scan found nothing in band")
	}
	const step = 100.0 / 4000.0
	if math.Abs(iv.ImportMeasureLo-loRef) > step || math.Abs(iv.ImportMeasureHi-hiRef) > step {
		t.Fatalf("interval [%.6g,%.6g], dense reference ≈[%.6g,%.6g] (step %.6g)", iv.ImportMeasureLo, iv.ImportMeasureHi, loRef, hiRef, step)
	}

	// Oriented correspondence: both polylines run west→east, so canon measure
	// must INCREASE with import measure across the whole interval, and the
	// interval's own extreme canon measures must be the candidate's own extent
	// [0, pc.total] — clamped to the candidate's endpoints at both ends,
	// since the crossing falls in the near-A/near-B clamp regions, not the
	// span.
	first, last := iv.Pieces[0], iv.Pieces[len(iv.Pieces)-1]
	if first.CanonMeasureAtLo > first.CanonMeasureAtHi {
		t.Fatalf("first piece canon measure not increasing: %.6g → %.6g", first.CanonMeasureAtLo, first.CanonMeasureAtHi)
	}
	if last.CanonMeasureAtLo > last.CanonMeasureAtHi {
		t.Fatalf("last piece canon measure not increasing: %.6g → %.6g", last.CanonMeasureAtLo, last.CanonMeasureAtHi)
	}
	if math.Abs(first.CanonMeasureAtLo-0) > corridorTol {
		t.Fatalf("interval should start clamped to the candidate's own start (measure 0), got %.6g", first.CanonMeasureAtLo)
	}
	if math.Abs(last.CanonMeasureAtHi-pc.Total) > corridorTol {
		t.Fatalf("interval should end clamped to the candidate's own end (measure %.6g), got %.6g", pc.Total, last.CanonMeasureAtHi)
	}

	// Pieces must chain: each piece's import measure end matches the next one's
	// start, and every piece's dist² at its own boundary is ≈ R².
	for i, p := range iv.Pieces {
		if i > 0 && math.Abs(p.ImportMeasureAtLo-iv.Pieces[i-1].ImportMeasureAtHi) > 1e-6 {
			t.Fatalf("piece %d does not chain from piece %d: %.9g vs %.9g", i, i-1, p.ImportMeasureAtLo, iv.Pieces[i-1].ImportMeasureAtHi)
		}
	}
	checkPieceEndpointsOnBand(t, iv, R)
}

// checkPieceEndpointsOnBand asserts every piece's dist²(lo) and dist²(hi), as
// computed by the exposed regime coefficients, land on the band edge (≈R²) —
// the load-bearing check that the coefficients are the SAME solve the
// boundary itself came from, not a lookalike.
func checkPieceEndpointsOnBand(t *testing.T, iv corridorInterval, R float64) {
	t.Helper()
	n := len(iv.Pieces)
	for i, p := range iv.Pieces {
		dLo := math.Sqrt(p.Regime.distSq(p.Regime.Lo))
		dHi := math.Sqrt(p.Regime.distSq(p.Regime.Hi))
		// Only the OUTER boundaries of the whole interval are guaranteed to sit
		// exactly on R — internal joins between pieces can be interior (both
		// pieces well inside the band, e.g. two regimes of one canon edge
		// meeting for reasons unrelated to the band edge). So check only piece
		// 0's lo and the last piece's hi against R; log the rest.
		if i == 0 && math.Abs(dLo-R) > corridorTol {
			t.Fatalf("interval start not on the band edge: dist=%.6g, want ≈%.6g", dLo, R)
		}
		if i == n-1 && math.Abs(dHi-R) > corridorTol {
			t.Fatalf("interval end not on the band edge: dist=%.6g, want ≈%.6g", dHi, R)
		}
	}
}

// ---- hairpin apex: one interval -----------------------------------------

// TestCorridorIntervals_HairpinApexOneInterval builds a candidate that turns
// back on itself (a hairpin) with its apex close to the source, and a source
// that runs past the apex perpendicular to the candidate's two arms. The
// nearest canon edge switches from one arm to the other partway along the
// source, but the corridor never leaves the band around the switch — both
// arms clamp to the SAME apex vertex there — so it must be reported as ONE
// interval, not two.
//
// Ground truth for the boundary comes from a dense sampled scan against the
// true polyline distance ([ptToPolylineDistM], the brute-force reference
// TestFollowHintPolyMatchesBruteForce pins), independent of this file's own
// solve.
func TestCorridorIntervals_HairpinApexOneInterval(t *testing.T) {
	const R = 15.0
	// Apex at the origin; arm 0 recedes west at y=+10, arm 1 recedes west at
	// y=-10 — a hairpin opening westward, apex easternmost.
	pc := prepOne(t, mPt(-200, 10), mPt(0, 0), mPt(-200, -10))
	// Source: a straight vertical line at x=5, from y=-40 to y=40, passing the
	// apex region at (0,0). At y=0 the nearest canon point is the apex, 5.10 m
	// away (well inside R); the corridor must close somewhere on each side of
	// y=0 as the source draws away, and reopen nowhere else on this fixture.
	n := 4001
	pts := make([]Point, n)
	for i := range pts {
		y := -40 + 80*float64(i)/float64(n-1)
		pts[i] = mPt(5, y)
	}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}

	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil {
		t.Fatalf("candidateCorridorIntervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("got %d intervals at the hairpin apex, want 1: %+v", len(ivs), ivs)
	}
	iv := ivs[0]

	// Dense-sample reference for the boundary, bounded by the sample step
	// (80m / 4000 steps = 0.02 m) with no slack added, matching this
	// package's existing sampled-reference bars.
	const step = 80.0 / 4000.0
	loRef, hiRef, ok := denseBandBoundary(pts, pc.Verts, R)
	if !ok {
		t.Fatalf("fixture sanity: dense scan found nothing in band")
	}
	if math.Abs(iv.ImportMeasureLo-loRef) > step || math.Abs(iv.ImportMeasureHi-hiRef) > step {
		t.Fatalf("interval [%.6g,%.6g], dense reference ≈[%.6g,%.6g] (step %.6g)",
			iv.ImportMeasureLo, iv.ImportMeasureHi, loRef, hiRef, step)
	}

	// The apex hand-off: somewhere in the middle, canon edge must switch from
	// 1 (arm1, near end) to 0 (arm0, near start) or vice versa, and at the
	// switch both sides must agree on the apex's own measure (cumMeasure[1]) — the
	// vertex-clamp agreement this file's header explains.
	sawEdge0, sawEdge1 := false, false
	for i, p := range iv.Pieces {
		switch p.CanonEdge {
		case 0:
			sawEdge0 = true
		case 1:
			sawEdge1 = true
		}
		if i > 0 {
			prev := iv.Pieces[i-1]
			if prev.CanonEdge != p.CanonEdge {
				if math.Abs(prev.CanonMeasureAtHi-p.CanonMeasureAtLo) > corridorTol {
					t.Fatalf("canon-edge hand-off %d→%d disagrees on measure: %.6g vs %.6g",
						prev.CanonEdge, p.CanonEdge, prev.CanonMeasureAtHi, p.CanonMeasureAtLo)
				}
			}
		}
	}
	if !sawEdge0 || !sawEdge1 {
		t.Fatalf("expected the interval to touch both arms (edges 0 and 1), got edge0=%v edge1=%v", sawEdge0, sawEdge1)
	}
}

// denseBandBoundary scans pts at their own resolution and returns the
// cumulative-measure bounds of the (assumed single, contiguous) in-band run
// against poly, using the brute-force distance primitive
// TestFollowHintPolyMatchesBruteForce pins.
func denseBandBoundary(pts, poly []Point, R float64) (lo, hi float64, ok bool) {
	measure := 0.0
	for i, p := range pts {
		if i > 0 {
			measure += HaversineMetres(pts[i-1].Lat, pts[i-1].Lon, p.Lat, p.Lon)
		}
		d := ptToPolylineDistM(p.Lat, p.Lon, poly)
		if d <= R {
			if !ok {
				lo = measure
				ok = true
			}
			hi = measure
		}
	}
	return lo, hi, ok
}

// ---- self-approach: two intervals ---------------------------------------

// TestCorridorIntervals_SelfApproachTwoIntervals builds one candidate with
// two stretches — canon-edge-adjacent to neither of the OTHER's own arm — both
// close to the SAME source stretch, joined by a long detour far away so their
// canon-edge indices are nowhere near adjacent. The corridor must NOT merge
// them into one interval: the two stretches are unrelated parts of the
// candidate, which is exactly what self-approach is.
func TestCorridorIntervals_SelfApproachTwoIntervals(t *testing.T) {
	const R = 15.0
	pc := prepOne(t,
		mPt(0, 5), mPt(50, 5), // edge 0: near arm, y=+5
		mPt(50, 500), mPt(0, 500), mPt(0, -5), // edges 1-3: a detour far away
		mPt(50, -5), // edge 4: far arm, y=-5
	)
	sa, err := newSourceMeasure(straightLegX(3, 50, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}

	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil {
		t.Fatalf("candidateCorridorIntervals: %v", err)
	}
	if len(ivs) != 2 {
		t.Fatalf("got %d intervals, want 2 (self-approach): %+v", len(ivs), ivs)
	}

	// Both intervals should span (approximately) the whole source, since the
	// source's whole extent [0,50] sits directly under both arms.
	for i, iv := range ivs {
		if iv.ImportMeasureLo > 1 || iv.ImportMeasureHi < 49 {
			t.Fatalf("interval %d = [%.6g,%.6g], want ≈[0,50]", i, iv.ImportMeasureLo, iv.ImportMeasureHi)
		}
	}
	// The two intervals must come from DIFFERENT, non-adjacent canon edges —
	// the whole reason they did not join.
	e0 := ivs[0].Pieces[0].CanonEdge
	e1 := ivs[1].Pieces[0].CanonEdge
	if canonEdgeRangesMeet(e0, e0, e1, e1) {
		t.Fatalf("the two intervals' canon edges (%d, %d) are adjacent — this is not testing self-approach", e0, e1)
	}
	// And they must actually overlap in import measure, which is the defining
	// property of self-approach (not just "two intervals somewhere").
	if ivs[0].ImportMeasureLo > ivs[1].ImportMeasureHi || ivs[1].ImportMeasureLo > ivs[0].ImportMeasureHi {
		t.Fatalf("expected the two intervals to overlap in import measure: [%.6g,%.6g] and [%.6g,%.6g]",
			ivs[0].ImportMeasureLo, ivs[0].ImportMeasureHi, ivs[1].ImportMeasureLo, ivs[1].ImportMeasureHi)
	}
}

// ---- dist² regime coefficients: exposed and correct ---------------------

// TestCorridorPieceDistSq_MatchesClosestPointOnSeg checks the exposed dist²
// coefficients against an independent evaluation (ClosestPointOnSeg,
// squared), at several t within each piece — the check that the coefficients
// are the SAME solve the boundary came from, not a separately-derived
// lookalike that happens to agree at the endpoints.
func TestCorridorPieceDistSq_MatchesClosestPointOnSeg(t *testing.T) {
	const R = 15.0
	pc := prepOne(t, mPt(30, 5), mPt(70, 5))
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
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

	checked := 0
	for _, p := range ivs[0].Pieces {
		srcP, srcQ := sa.Pts[p.SrcEdge], sa.Pts[p.SrcEdge+1]
		canonA, canonB := pc.Verts[p.CanonEdge], pc.Verts[p.CanonEdge+1]
		for k := 0; k <= 10; k++ {
			frac := float64(k) / 10
			tt := p.Regime.Lo + frac*(p.Regime.Hi-p.Regime.Lo)
			lat := srcP.Lat + tt*(srcQ.Lat-srcP.Lat)
			lon := srcP.Lon + tt*(srcQ.Lon-srcP.Lon)
			want, _ := ClosestPointOnSeg(canonA.Lat, canonA.Lon, canonB.Lat, canonB.Lon, lat, lon)
			got := math.Sqrt(p.Regime.distSq(tt))
			if math.Abs(got-want) > 1e-6 {
				t.Fatalf("piece [srcEdge=%d canonEdge=%d kind=%v] t=%.4g: distSq gives %.9g, closestPointOnSeg gives %.9g",
					p.SrcEdge, p.CanonEdge, p.Regime.Kind, tt, got, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatalf("fixture produced no pieces to check")
	}
	t.Logf("checked distSq against closestPointOnSeg at %d points", checked)
}

// TestCorridorPieceDistSq_SpanRegimeAgreesUnexpanded exercises the SPAN
// regime specifically (a source running parallel and well inside a long
// canon edge, so the interior/span regime dominates the whole piece), to
// pin that the (spanC0, spanC1, spanLenSq) form is being squared correctly
// rather than silently mishandled.
func TestCorridorPieceDistSq_SpanRegimeAgreesUnexpanded(t *testing.T) {
	const R = 15.0
	pc := prepOne(t, mPt(-500, 5), mPt(500, 5)) // long canon edge, y=5
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
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
	iv := ivs[0]
	// The whole source [0,100] is far inside the canon edge's [-500,500]
	// extent, so every piece must be the span regime.
	sawSpan := false
	for _, p := range iv.Pieces {
		if p.Regime.Kind != RegimeSpan {
			t.Fatalf("expected only regimeSpan pieces for a source well inside a long canon edge, got kind=%v", p.Regime.Kind)
		}
		sawSpan = true
		mid := (p.Regime.Lo + p.Regime.Hi) / 2
		got := math.Sqrt(p.Regime.distSq(mid))
		if math.Abs(got-5) > corridorTol {
			t.Fatalf("span regime at midpoint: dist=%.6g, want ≈5 (the perpendicular offset)", got)
		}
	}
	if !sawSpan {
		t.Fatalf("fixture produced no span-regime pieces")
	}
	// The whole source is within R of the canon edge (dist≈5 < 15 everywhere,
	// far from either endpoint), so the interval must cover the WHOLE source
	// — sa.total(), its own haversine-measured length, not the nominal 100 m
	// the fixture was built from (mPt's fixed lat/lon-to-metres constant
	// differs slightly from the haversine radius newSourceMeasure measures with).
	if iv.ImportMeasureLo > corridorTol || iv.ImportMeasureHi < sa.Total()-corridorTol {
		t.Fatalf("interval [%.6g,%.6g], want ≈[0,%.6g]", iv.ImportMeasureLo, iv.ImportMeasureHi, sa.Total())
	}
}

// corridorIntervalsForLeg is [corridorIntervals] taking raw source points
// instead of a pre-built [sourceMeasure].
//
// A leg of fewer than two points has no source EDGES, so it has no corridor
// to find — this is an EXPECTED absence (a single-point or empty leg is
// ordinary production input),
// not the caller bug [newSourceMeasure] itself refuses with an error, so it is
// intercepted here as a fallback rather than allowed to become one.
//
// ⚠ TEST-ONLY (moved from corridorinterval.go on 2026-09-26: no production code that
// only tests call). Production does not call it.
// Production takes the same absence in corridorFollowRuns, before it builds
// a sourceMeasure.
func corridorIntervalsForLeg(ctx context.Context, pts []Point, cands []*PreppedCandidate, d float64) ([]corridorInterval, error) {
	if len(pts) < 2 {
		return nil, nil
	}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		return nil, err
	}
	return corridorIntervals(ctx, sa, cands, d)
}

func prepOne(t *testing.T, verts ...Point) *PreppedCandidate {
	t.Helper()
	pcs := prepCandidates([]CanonArc{mCanon(1, verts...)})
	if len(pcs) != 1 || pcs[0] == nil {
		t.Fatalf("prepCandidates rejected the fixture candidate")
	}
	return pcs[0]
}
