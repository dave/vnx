package merge

// Tests for the joinCorridorPieces defect found by re-importing real
// geometry: a leg
// re-following ONE candidate that sits bitwise identical to it, at distance
// 0 throughout, produced 53 corridor intervals instead of 1 — and 29 of the
// 53 were exactly 15.000m wide (== R to three decimals), each a single
// canon edge's redundant/clamped competitor that failed to fold into the
// ongoing chain.
//
// The mechanism: joinCorridorPieces decides whether a new source edge's own
// group of pieces continues an active chain by comparing only the group's
// FIRST piece (canon-edge-ascending list order) against the chain's LAST
// piece. When several canon edges compete for the same source position
// (ordinary once canon has been split into many short consecutive edges by
// earlier splitting — every edge within R of a straight stretch
// counts), the group's first piece is often a redundant, farther, lower-
// indexed neighbour rather than the true edge the highlight is actually
// tracking, so the comparison targets the wrong pair and the highlight breaks
// even though a genuinely continuous, non-adjacent-canon-edge piece IS
// present in both the old highlight and the new group.

import (
	"testing"
)

// TestCorridorIntervals_ManyShortConsecutiveEdgesStayOneInterval is the
// red-first minimal reproduction: a candidate and an identical source, both
// split into 20 short (5m) consecutive edges — the real-data shape (many short
// canon edges from repeated splitting, source running exactly on top of
// them). Distance is 0 everywhere; this should be ONE interval. Before the
// fix this produced 20 (one per source edge, matching the real leg's own
// pass-2 fragmentation pattern one-for-one).
func TestCorridorIntervals_ManyShortConsecutiveEdgesStayOneInterval(t *testing.T) {
	const R = CorridorRadiusM
	pc := prepOne(t, straightLegX(21, 100, 0)...) // 20 edges of 5m
	sa, err := NewSourceMeasure(straightLegX(21, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs, err := corridorIntervalsForLeg(t.Context(), sa.Pts, []*PreppedCandidate{pc}, R)
	if err != nil {
		t.Fatalf("corridorIntervalsForLeg: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("want 1 interval for a source running exactly on top of one straight, "+
			"many-short-edges candidate, got %d: %+v", len(ivs), ivs)
	}
	iv := ivs[0]
	const tol = 1e-6
	if iv.ImportMeasureLo > tol || iv.ImportMeasureHi < sa.Total()-tol {
		t.Errorf("interval should span the whole leg [0,%.6g], got [%.6g,%.6g]", sa.Total(), iv.ImportMeasureLo, iv.ImportMeasureHi)
	}

	// iv.Pieces legitimately CONTAINS redundant, off-canon competitor pieces
	// alongside the true on-canon ones — CorridorPiece's own doc comment
	// ("ascending import measure, contiguous OR SELF-APPROACH-OVERLAPPING") and
	// exactly what ArcCost/canonMeasureAtImportMeasure/coalescePieces' shared
	// first-piece-wins convention exists to resolve, not a property THIS
	// join fix is responsible for. What the join fix owns is fragmentation —
	// interval COUNT and EXTENT, checked above — not which piece the
	// first-in-list-order convention resolves to at a given position.
	//
	// NOT asserted here, flagged instead: the true (RegimeSpan, dist=0)
	// piece is not always first in iv.Pieces' own canon-edge-ascending
	// order — a redundant, lower-indexed competitor can still sort ahead of
	// it within one source edge's own group, so
	// canonMeasureAtImportMeasure/ArcCost/coalescePieces can resolve to a
	// non-minimal (but still within-R) piece at some positions even inside
	// one now-correctly-unfragmented interval. Measured on this exact
	// fixture at x=5.25: first-clipping-piece resolves to [srcEdge=1
	// canonEdge=0] (dist²=0.065), not canonEdge=1 (dist²≈0) — a real,
	// separate defect in the shared first-piece-wins convention itself, not
	// introduced by this fix and not fixed by it either. Reported
	// rather than silently absorbed into this test's bar.
}

// TestCorridorIntervals_HairpinApexOneInterval and
// TestCorridorIntervals_SelfApproachTwoIntervals (corridorinterval_test.go)
// are the boundary condition this fix must not cross: they are unaffected
// by this file, run unchanged, and their own package suite run is the
// verification that fixing the join here did not fold genuine self-approach
// into one interval.
