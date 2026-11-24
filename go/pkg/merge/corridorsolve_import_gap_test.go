package merge

// corridorsolve_import_gap_test.go — the IMPORT-SIDE gap integral.
//
// A bridge edge used to be the only edge in the corridor solve that
// consumed import measure and paid nothing per metre for it: it relaxed with
// the canon-side gap integral alone ([bridgeCost]), while ON pays emission
// (d/R)^k per metre of import and OFF pays its raw metres. A bridge was
// therefore priced exactly as PERFECT ADOPTION AT d=0 over source that is by
// construction OUTSIDE the corridor.
//
// Measured on a real route imported with seven other files
// (2026-09-03): one bridge crossed 5,043.11 m of
// recorded track for a cost of 4.3277, against 5,143.11 for the honest path —
// an undercut of 1,188x. The leg committed NO geometry of its own; the
// recording was silently discarded.
//
// The rule, and it introduces no constant: every metre of import measure a
// path consumes is paid for exactly once, by the edge that consumes it, so a
// bridge pays the IMPORT-side gap integral — emission (d/R)^k over the source
// it skips, against the canon it claims — as well as the canon-side one.

import (
	"testing"
)

// importGapFixture is the SHAPE OF THE DEFECT, in synthetic form: a source
// that leaves the canon corridor, travels a long way, and comes back to
// nearly the same canon position.
//
// That shape is what makes the two gaps disagree. The CANON gap between the
// two landing points is only (returnX-approachX) metres wide, and the
// envelope minimum over it is taken against the excursion's own approach and
// return legs, which pass within 5 m of the canon — so the canon-side
// integral is near zero. The IMPORT gap is the whole excursion, hundreds of
// metres of recorded geometry sitting `depthY` from the canon it is about to
// be priced against.
//
// This is deliberately NOT bridgeLoopFixture's shape: that one is short and
// shallow, where the two gaps roughly agree.
func importGapFixture(t *testing.T, approachX, returnX, depthY float64) (*sourceMeasure, []*PreppedCandidate, []corridorInterval) {
	t.Helper()
	const R = CorridorRadiusM
	pc := prepOne(t, mPt(0, 0), mPt(1000, 0))

	var pts []Point
	pts = append(pts, straightLegX(9, approachX, 5)...)
	pts = append(pts, mPt(approachX, depthY))
	pts = append(pts, mPt(returnX, depthY))
	pts = append(pts, mPt(returnX, 5))
	for x := returnX + 40; x < 1000; x += 50 {
		pts = append(pts, mPt(x, 5))
	}
	pts = append(pts, mPt(1000, 5))

	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	return sa, cands, buildIvs(t, sa, cands, R)
}

// TestCandidateBridgeCost_ChargesForTheSourceItSkips is the unit red-proof: a
// bridge whose canon span is 10 m but whose import gap is ~600 m of recorded
// geometry 300 m off the trail must cost MORE than the honest alternative
// (2P + the OFF metres), not less.
//
// Before the import-side term this priced at the canon-side integral alone —
// a few units against a no-bridge price of ~700 — because the canon-gap
// envelope is minimised against the excursion's own near-canon approach and
// return legs. The 600 m in between was free.
func TestCandidateBridgeCost_ChargesForTheSourceItSkips(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := importGapFixture(t, 400, 410, 300)
	if len(ivs) < 2 {
		t.Fatalf("fixture sanity: want at least 2 corridor intervals on the one candidate, got %d", len(ivs))
	}

	bridges, err := buildCorridorBridges(t.Context(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}
	if len(bridges) == 0 {
		t.Fatalf("fixture sanity: want at least one priced bridge candidate, got none")
	}

	sawSkipper := false
	for _, br := range bridges {
		importGap := ivs[br.ToIv].ImportMeasureLo - ivs[br.FromIv].ImportMeasureHi
		canonSpan := br.CanonHi - br.CanonLo
		if canonSpan < 0 {
			canonSpan = -canonSpan
		}
		if importGap < 100 {
			continue // not the long excursion; a short adjacent gap proves nothing here
		}
		sawSkipper = true
		want := noBridgeCost(importGap)
		t.Logf("bridge iv%d->iv%d: importGap=%.3fm canonSpan=%.3fm cost=%.4f noBridge=%.4f",
			br.FromIv, br.ToIv, importGap, canonSpan, br.Cost, want)
		if br.Cost <= want {
			t.Errorf("a bridge crossing %.1f m of recorded source for a %.1f m canon span cost %.4f, "+
				"at or under the honest no-bridge price %.4f — the import measure it consumes is not being paid for",
				importGap, canonSpan, br.Cost, want)
		}
	}
	if !sawSkipper {
		t.Fatalf("fixture sanity: no bridge candidate spanned the long excursion, so nothing was measured")
	}

	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	for _, r := range res.Runs {
		if r.Bridged && r.ImportMeasureHi-r.ImportMeasureLo > 100 {
			t.Fatalf("the solve still teleported across %.1f m of recorded source: %+v",
				r.ImportMeasureHi-r.ImportMeasureLo, r)
		}
	}
}

// TestImportGapCost_ShortNearGapStaysCheap is the positive control for the
// same term: a bridge over a SHORT gap whose source stays close to the canon
// it claims must still be cheap enough to fire. Without this the test above
// is satisfied by any change that simply makes bridging impossible.
func TestImportGapCost_ShortNearGapStaysCheap(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := importGapFixture(t, 400, 405, 12)
	if len(ivs) < 2 {
		t.Fatalf("fixture sanity: want at least 2 corridor intervals, got %d", len(ivs))
	}
	bridges, err := buildCorridorBridges(t.Context(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}
	if len(bridges) == 0 {
		t.Fatalf("fixture sanity: want at least one priced bridge candidate, got none")
	}
	cheap := false
	for _, br := range bridges {
		importGap := ivs[br.ToIv].ImportMeasureLo - ivs[br.FromIv].ImportMeasureHi
		t.Logf("bridge iv%d->iv%d: importGap=%.3fm cost=%.4f noBridge=%.4f",
			br.FromIv, br.ToIv, importGap, br.Cost, noBridgeCost(importGap))
		if br.Cost < noBridgeCost(importGap) {
			cheap = true
		}
	}
	if !cheap {
		t.Fatalf("no bridge over a short, near-canon gap came out cheaper than 2P + off-metres — " +
			"the import-side term has priced legitimate bridging out of existence")
	}
}
