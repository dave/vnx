package merge

// Tests for the ArcCost overlapping-pieces defect, found verifying the solve
// on a real re-import.
//
// CorridorInterval.Pieces can legitimately overlap in import measure on a
// self-approach (CorridorPiece's own doc comment: "ascending import measure,
// contiguous OR SELF-APPROACH-OVERLAPPING") — one common case being a
// straight stretch of canon split into several short consecutive edges
// (ordinary after repeated splitting), where a single source position
// sits within R of its own true edge AND of one or more neighbouring edges'
// clamped endpoints at the same time. [ArcCost] summed EmissionCost over
// every piece that clipped the queried range, so a position sitting EXACTLY
// on canon (true-piece emission 0) was charged for every OTHER, farther-away
// edge that also happened to be within R — measured on a straight re-import
// leg: ~0.5/m instead of 0, enough to make SolveCorridorPath choose all-OFF
// over adopting canon it sits exactly on top of.
//
// [canonMeasureAtImportMeasure] — the solver's own point lookup for transitions and
// bridges — already resolves the same overlap by taking the FIRST piece in
// iv.Pieces' own (source-edge-ascending, then canon-edge-ascending) list
// order whose range contains the query. [AssembleFollowedLegRunsCorridor]'s
// greedy left-to-right piece consumption (corridorwire.go) reproduces that
// same rule for a range. ArcCost must price whichever piece those two
// would draw — summing prices geometry nothing commits.

import (
	"sort"
	"testing"
)

// firstClippingPiece is this file's own, INDEPENDENT restatement of
// [canonMeasureAtImportMeasure]'s tie-break — first piece in iv.Pieces' own order
// whose range clips [x0,x1] — used as the oracle both tests below check
// [ArcCost] and [canonMeasureAtImportMeasure] against. It does not call either
// function under test.
func firstClippingPiece(iv CorridorInterval, x0, x1 float64) (CorridorPiece, bool) {
	for _, p := range iv.Pieces {
		if cp, ok := ClipCorridorPiece(p, x0, x1); ok {
			return cp, true
		}
	}
	return CorridorPiece{}, false
}

// reimportOverlapFixture builds a candidate and a source that follow the
// SAME straight 100m line, both split into short (~5m) edges — the shape
// that produces overlapping pieces within one CorridorInterval: any source
// position sitting on one edge is also within R=15 of several neighbouring
// edges' clamped endpoints. Since corridorinterval_join_test.go's join fix,
// this whole leg is correctly ONE interval (it used to fragment into 20 —
// that was the interval-join defect, not this file's), so this now searches WITHIN
// that one interval for a sub-range genuinely covered by more than one
// non-zero-emission piece — the overlap CorridorPiece's own doc comment
// documents as legitimate ("ascending import measure, contiguous OR
// SELF-APPROACH-OVERLAPPING") and which ArcCost must resolve to one
// piece, not sum.
func reimportOverlapFixture(t *testing.T) (iv CorridorInterval, x0, x1 float64) {
	t.Helper()
	const R = CorridorRadiusM
	pc := prepOne(t, straightLegX(21, 100, 0)...) // 20 edges of 5m
	sa, err := NewSourceMeasure(straightLegX(21, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs := buildIvs(t, sa, []*PreppedCandidate{pc}, R)
	if len(ivs) != 1 {
		t.Fatalf("fixture sanity: want 1 interval for this leg (see corridorinterval_join_test.go), got %d: %+v", len(ivs), ivs)
	}
	iv = ivs[0]

	// Every distinct piece boundary, sorted — clipping pieces are constant
	// between consecutive boundaries (the same guarantee SolveCorridorPath's
	// own breakpoint construction relies on), so testing each such
	// elementary sub-range finds a genuine multi-piece overlap without
	// guessing coordinates by hand.
	bset := map[float64]bool{}
	for _, p := range iv.Pieces {
		bset[p.ImportMeasureAtLo], bset[p.ImportMeasureAtHi] = true, true
	}
	var bps []float64
	for b := range bset {
		bps = append(bps, b)
	}
	sort.Float64s(bps)

	for i := 0; i+1 < len(bps); i++ {
		lo, hi := bps[i], bps[i+1]
		if hi-lo < 1e-9 {
			continue
		}
		nonZero := 0
		for _, p := range iv.Pieces {
			cp, ok := ClipCorridorPiece(p, lo, hi)
			if !ok {
				continue
			}
			if e, err := EmissionCost(cp, R, EmissionK); err == nil && e > 1e-9 {
				nonZero++
			}
		}
		if nonZero >= 2 {
			return iv, lo, hi
		}
	}
	t.Fatalf("fixture sanity: found no sub-range with >=2 non-zero-emission overlapping pieces in %+v", iv.Pieces)
	return CorridorInterval{}, 0, 0
}

// TestArcCost_OverlappingPiecesPricesOneNotSum is the red-first case:
// ArcCost must price ONE piece, never the sum of every piece that clips
// the range. Before the original fix it summed EmissionCost over every
// clipping piece, over-charging by exactly the extra pieces' own emission —
// which is what [reimportOverlapFixture] is built to guarantee is possible.
//
// The property is now STRUCTURAL rather than defended: the solver's state
// names the piece ([CorridorStateID]), so ArcCost is handed one and has
// nothing to search. The test is kept anyway, and kept red-provable, because
// "prices one piece" is the invariant — a future change that reintroduces a
// loop over an interval's pieces would silently restore the over-charge, and
// this is what would catch it.
func TestArcCost_OverlappingPiecesPricesOneNotSum(t *testing.T) {
	const R = CorridorRadiusM
	iv, x0, x1 := reimportOverlapFixture(t)

	want, ok := firstClippingPiece(iv, x0, x1)
	if !ok {
		t.Fatalf("fixture sanity: no piece clips [%.6g,%.6g]", x0, x1)
	}
	wantE, err := EmissionCost(want, R, EmissionK)
	if err != nil {
		t.Fatalf("emissionCost: %v", err)
	}
	wantCost := wantE + BacktrackCost(want, bool(CorridorFWD))

	// The piece the fixture picked, presented as the state that names it.
	var sp SolvePiece
	found := false
	for _, pc := range iv.Pieces {
		cp, ok := ClipCorridorPiece(pc, x0, x1)
		if !ok {
			continue
		}
		if cp == want {
			sp = SolvePiece{IvIdx: 0, CandIdx: iv.CandIdx, P: pc}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture sanity: firstClippingPiece returned a clip no piece of the interval produces")
	}

	got, err := ArcCost(&sp, CorridorFWD, x0, x1, R, EmissionK)
	if err != nil {
		t.Fatalf("arcCost: %v", err)
	}

	const tol = 1e-9
	if diff := got - wantCost; diff > tol || diff < -tol {
		t.Fatalf("arcCost priced the OVERLAP, not the single covering piece: got %.12g, want %.12g", got, wantCost)
	}
}

// TestArcCost_PricesTheStatesOwnPieceNotAnotherOne replaces the old
// agreement bar between ArcCost and canonMeasureAtImportMeasure.
//
// That bar existed because there were TWO resolvers of the same question
// ("which piece covers this position?") and they could disagree — one used by
// the cost model, one by the assembler. There is now exactly one answer,
// because the piece IS the state, so there is nothing left to agree. What
// takes its place is the property that makes that true: for EVERY piece of an
// overlapping interval, ArcCost prices THAT piece and is not contaminated
// by any of its competitors. If it ever searched again, the pieces would all
// return the same number and this would fail.
func TestArcCost_PricesTheStatesOwnPieceNotAnotherOne(t *testing.T) {
	const R = CorridorRadiusM
	iv, x0, x1 := reimportOverlapFixture(t)

	seen := map[float64]int{}
	n := 0
	for _, pc := range iv.Pieces {
		cp, ok := ClipCorridorPiece(pc, x0, x1)
		if !ok {
			continue
		}
		e, err := EmissionCost(cp, R, EmissionK)
		if err != nil {
			t.Fatalf("emissionCost: %v", err)
		}
		want := e + BacktrackCost(cp, bool(CorridorFWD))
		got, err := ArcCost(&SolvePiece{IvIdx: 0, CandIdx: iv.CandIdx, P: pc}, CorridorFWD, x0, x1, R, EmissionK)
		if err != nil {
			t.Fatalf("arcCost: %v", err)
		}
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("arcCost priced %.12g for a state naming a piece whose own cost is %.12g — it is not pricing the state's own piece", got, want)
		}
		seen[got]++
		n++
	}
	if n < 2 {
		t.Fatalf("fixture sanity: expected >=2 clipping pieces, got %d", n)
	}
	if len(seen) < 2 {
		t.Fatalf("every clipping piece priced the SAME cost (%v) — arcCost is resolving the piece itself instead of pricing the one the state names", seen)
	}
}
