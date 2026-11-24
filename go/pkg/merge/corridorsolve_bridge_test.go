package merge

// corridorsolve_bridge_test.go — wiring CanonGapCost/EnvelopeMinCost
// (corridorcost.go) into the solve as a fourth edge kind, bridge edges
// (corridorsolve.go's own header, "bridge edges, the fourth cost term"). Two red-proofs, both against REAL solver output, not a mocked
// cost function:
//
//  1. a SHORT, shallow canon gap must be bridged when it is cheaper than
//     going OFF — and the bridged stretch must read non-zero on the
//     fabrication instrument ([bridgedMetresForRun]) when checked against
//     the FULL source, not the restricted candidate set the solve itself
//     used. Before this task, that instrument read EXACTLY 0.00 m on every
//     area, by construction — this is the case that makes it discriminating.
//  2. a WIDE, deep gap must NOT be bridged — the same mechanism, refusing,
//     proving bridging is genuinely cost-sensitive rather than a free pass
//     the moment two intervals of one candidate exist.

import (
	"testing"
)

// bridgeLoopFixture builds a candidate running straight along y=0 from x=0
// to x=1000, and a source that hugs it (y=5, well within R) except for one
// rectangular loop between approachX and returnX: up to loopY, across, and
// back down — a single, deliberate excursion, not scattered noise, so the
// resulting corridor gap is attributable to exactly one cause.
func bridgeLoopFixture(t *testing.T, approachX, returnX, loopY float64) (*SourceMeasure, []*PreppedCandidate, []CorridorInterval) {
	t.Helper()
	const R = CorridorRadiusM
	pc := prepOne(t, mPt(0, 0), mPt(1000, 0))

	var pts []Point
	pts = append(pts, straightLegX(9, approachX, 5)...) // approach, x in [0,approachX] at y=5
	pts = append(pts, mPt(approachX, loopY))
	pts = append(pts, mPt(returnX, loopY))
	pts = append(pts, mPt(returnX, 5))
	for x := returnX + 40; x < 1000; x += 50 {
		pts = append(pts, mPt(x, 5))
	}
	pts = append(pts, mPt(1000, 5)) // always end exactly at the leg's own declared end

	sa, err := NewSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	return sa, cands, ivs
}

// TestSolveCorridorPath_BridgesShortShallowGapAndFabricationReadsNonZero is
// red-proof 1: a 20 m-wide, 12 m-deep excursion (beyond R=10 at its peak, but
// short and close) is cheaper to bridge than to leave OFF. The solver must
// choose the bridge, and the bridged run's canon measure, checked against the
// FULL source polyline (not the restricted set the solve itself used — see
// corridorsolve.go's soundness note on why that restriction is safe for the
// SOLVE decision but is not the true-fabrication yardstick), must read
// non-zero on [bridgedMetresForRun] — the fabrication instrument, previously
// 0.00 m on every real area by construction.
//
// ⚠ RE-BASELINED 2026-09-03, FROM bridgeLoopFixture(400, 430, 40). The old
// fixture is a 30 m-wide, 40 m-DEEP excursion, and the comment here used to
// justify it as "well beyond R=15 at its peak" — written in the 15 m era and
// never revisited when R became 10, so the shape was four radii deep rather
// than under three. Under the import-side gap integral
// ([ImportGapCost], added the same day) that fixture's ~100 m of recorded
// source at up to 4R costs ~900 against a 190 no-bridge price, so it is now
// KEPT rather than bridged — which is correct, and is asserted in its own
// right by [TestSolveCorridorPath_RefusesToBridgeA40mExcursion] below.
//
// The rule, 2026-09-03: every source edge is legitimate geometry —
// "It's completely legitimate geometry. It doesn't matter whether you think
// someone could have hiked it or not." A 40 m-deep excursion is recorded
// geometry and must be committed, so the old bridged outcome was NOT
// preserved.
//
// ⚠ THE WIDTH IS LOAD-BEARING, AND 5 m WAS NOT ENOUGH — this test caught the
// first attempt at this re-baseline. A bridge fired at (400, 405, 12), but
// [bridgedMetresForRun] read 0.000 m and the test failed on its own
// fabrication clause: the bridged canon span was 4.99 m long and the source's
// approach and return legs pass within ~5 m of BOTH its ends, so no point of
// it is beyond R = 10 from the source and there is no fabrication to measure.
//
// The two requirements pull against each other, which is exactly what the
// import-side term does and is worth understanding before touching these
// numbers again:
//
//   - for the bridge to FIRE, the skipped source must stay close to the canon
//     it claims (that is what the import-side integral prices);
//   - for [bridgedMetresForRun] to read NON-ZERO, the middle of the canon span
//     must be further than R from any source.
//
// Both hold only when the span is wide enough that its middle escapes the
// approach and return legs: at width W the midpoint sits sqrt((W/2)^2 + 5^2)
// from the nearest one, so W must exceed ~17 m. At (400, 420, 12): the bridge
// fires at cost 55-ish against a no-bridge price of ~134, and bridgedM reads
// 2.997 m over a canon span of [399.551, 419.528].
func TestSolveCorridorPath_BridgesShortShallowGapAndFabricationReadsNonZero(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := bridgeLoopFixture(t, 400, 420, 12)
	if len(ivs) < 2 {
		t.Fatalf("fixture sanity: want at least 2 corridor intervals on the one candidate, got %d", len(ivs))
	}

	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}

	var bridgedRuns []CorridorRun
	for _, r := range res.Runs {
		if r.Bridged {
			bridgedRuns = append(bridgedRuns, r)
		}
	}
	if len(bridgedRuns) == 0 {
		t.Fatalf("want at least one bridged run over the short/shallow gap, got none: %+v", res.Runs)
	}

	srcPoly := NewHintPoly(sa.Pts)
	sawFabrication := false
	for _, r := range bridgedRuns {
		if r.CanonMeasureHi <= r.CanonMeasureLo {
			t.Errorf("bridged run has non-positive canon measure span [%.6g,%.6g]", r.CanonMeasureLo, r.CanonMeasureHi)
			continue
		}
		fr := FollowRun{MeasureStart: r.CanonMeasureLo, MeasureEnd: r.CanonMeasureHi, CanonIdx: r.CandIdx}
		pc := cands[r.CandIdx]
		onM, bridgedM := bridgedMetresForRun(pc, fr, srcPoly, R, fabricationSampleStepM)
		t.Logf("bridged run canon=[%.3f,%.3f]: onM=%.3f bridgedM=%.3f", r.CanonMeasureLo, r.CanonMeasureHi, onM, bridgedM)
		if bridgedM > 0 {
			sawFabrication = true
		}
	}
	if !sawFabrication {
		t.Fatalf("bridgedMetresForRun read 0.00 m on every bridged run against the FULL source — the fabrication instrument must be able to read non-zero once bridging is wired, and this fixture was built to have a genuine >R excursion")
	}
}

// TestSolveCorridorPath_RefusesToBridgeAWideDeepGap is red-proof 2: the same
// mechanism, but the excursion is wide (200 m) as well as deep (100 m), so
// even the closest point of the "between" source at the middle of the gap
// sits far beyond R for a long stretch of canon measure — the gap integral must
// come out far more expensive than 2P + the off-metres it would otherwise
// cost, and the solver must refuse to bridge, falling back to the existing
// OFF-mediated path exactly as it did before this task.
func TestSolveCorridorPath_RefusesToBridgeAWideDeepGap(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := bridgeLoopFixture(t, 400, 600, 100)
	if len(ivs) < 2 {
		t.Fatalf("fixture sanity: want at least 2 corridor intervals on the one candidate, got %d", len(ivs))
	}

	bridges, err := BuildCorridorBridges(t.Context(), sa, ivs, cands, R, EmissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}
	if len(bridges) == 0 {
		t.Fatalf("fixture sanity: want at least one priced (but expensive) bridge candidate, got none")
	}
	offMetres := ivs[bridges[0].ToIv].ImportMeasureLo - ivs[bridges[0].FromIv].ImportMeasureHi
	if want := NoBridgeCost(offMetres); bridges[0].Cost <= want {
		t.Fatalf("fixture sanity: bridge cost %.6g should be far ABOVE noBridgeCost %.6g for this wide/deep excursion, or the negative control proves nothing", bridges[0].Cost, want)
	}

	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	for _, r := range res.Runs {
		if r.Bridged {
			t.Fatalf("solver bridged a gap priced far above its own no-bridge alternative — bridging is not cost-sensitive: %+v", r)
		}
	}
}

// TestSolveCorridorPath_RefusesToBridgeA40mExcursion is the re-baselined half
// of the fixture above, kept as its own assertion rather than deleted: a 30 m
// wide, 40 m deep excursion — four corridor radii off the trail — is RECORDED
// GEOMETRY, and the solve must keep it OFF rather than teleport across it.
//
// The rule, 2026-09-03: every source edge is legitimate geometry and the
// import does not judge walkability. Before the import-side gap
// integral this shape bridged, because a bridge paid the canon-side integral
// alone and the ~100 m of source it skipped was free. Measured after: ~900
// against a 190 no-bridge price.
//
// This is deliberately NOT a duplicate of
// [TestSolveCorridorPath_RefusesToBridgeAWideDeepGap]. That one refuses on the
// CANON side — its excursion is wide enough that canon itself sits far from
// any source for a long stretch, so the old cost model already refused it.
// This one is refused by the import side and by nothing else: the canon gap
// under it is 30 m of trail the source's own approach and return legs pass
// within 5 m of, so the canon-side envelope integral is small.
func TestSolveCorridorPath_RefusesToBridgeA40mExcursion(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := bridgeLoopFixture(t, 400, 430, 40)
	if len(ivs) < 2 {
		t.Fatalf("fixture sanity: want at least 2 corridor intervals on the one candidate, got %d", len(ivs))
	}

	bridges, err := BuildCorridorBridges(t.Context(), sa, ivs, cands, R, EmissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}
	if len(bridges) == 0 {
		t.Fatalf("fixture sanity: want at least one priced (but expensive) bridge candidate, got none")
	}
	for _, br := range bridges {
		offMetres := ivs[br.ToIv].ImportMeasureLo - ivs[br.FromIv].ImportMeasureHi
		t.Logf("bridge iv%d->iv%d: offMetres=%.3f cost=%.4f noBridge=%.4f",
			br.FromIv, br.ToIv, offMetres, br.Cost, NoBridgeCost(offMetres))
		if br.Cost <= NoBridgeCost(offMetres) {
			t.Errorf("a 40 m-deep excursion priced at %.4f, at or under its no-bridge price %.4f — "+
				"recorded geometry is being crossed for less than it costs to keep", br.Cost, NoBridgeCost(offMetres))
		}
	}

	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	for _, r := range res.Runs {
		if r.Bridged {
			t.Fatalf("the solve bridged a 40 m-deep recorded excursion instead of committing it: %+v", r)
		}
	}
}
