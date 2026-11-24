package merge

// Prove-It for [PruneToNearestCanonEdges]. The prune shipped with NO direct
// unit test — it was certified only by whole-import locks — and that is how the
// whole-run deletion below reached a real import without anything going red.
//
// The halo width (vnx merge's unexported nearestRunHaloEdges, 8) was fitted,
// not derived. These tests pin the prune's shape, not the width's adequacy:
// only a byte comparison of real-import output sees a halo regression.

import (
	"math"
	"testing"
)

// piece builds a CorridorPiece on canon edge e, active over t in [lo,hi], at
// a constant distance d. RegimeNearA's quadratic form with quadA/quadB zero
// is exactly a constant dist², which is all these cases need — the question
// under test is WHICH pieces survive, not how distance varies within one.
func nearestTestPiece(e int, lo, hi, d float64) CorridorPiece {
	return CorridorPiece{
		CanonEdge: e,
		Regime:    BandPiece{Lo: lo, Hi: hi, Kind: RegimeNearA, QuadC: d * d},
	}
}

func nearestKeptEdges(t *testing.T, pieces []CorridorPiece) []int {
	t.Helper()
	out, err := PruneToNearestCanonEdges(pieces, nil)
	if err != nil {
		t.Fatalf("pruneToNearestCanonEdges: %v", err)
	}
	edges := make([]int, 0, len(out))
	for _, p := range out {
		edges = append(edges, p.CanonEdge)
	}
	return edges
}

// TestPruneToNearestCanonEdges_ANarrowWindowIsNotDeleted is the red-first
// reproduction of the auditor's finding (2026-08-24).
//
// CRIPPLE: delete the per-piece-midpoint loop in PruneToNearestCanonEdges
// and this goes red — the piece is dropped outright, because its whole
// regime window falls between two points of the uniform sample grid.
func TestPruneToNearestCanonEdges_ANarrowWindowIsNotDeleted(t *testing.T) {
	// [0.001, 0.005] contains no k/64 for integer k, so the uniform grid
	// never lands inside it.
	const lo, hi = 0.001, 0.005
	for s := 0; s <= NearestRunSamples; s++ {
		if v := float64(s) / float64(NearestRunSamples); v >= lo && v <= hi {
			t.Fatalf("fixture is stale: sample %d (t=%v) lands inside [%v,%v], so this no longer "+
				"exercises a window the grid misses", s, v, lo, hi)
		}
	}
	got := nearestKeptEdges(t, []CorridorPiece{nearestTestPiece(3, lo, hi, 2)})
	if len(got) != 1 {
		t.Errorf("a run of one piece over t=[%v,%v] was pruned to %d piece(s) — the prune chooses "+
			"among competitors, it must never delete the only correspondence a run has", lo, hi, len(got))
	}
}

// TestPruneToNearestCanonEdges_KeepsTheNearestAndItsHalo pins the ordinary
// case: of a dense run of competitors, the nearest survives with its halo
// and the far ones do not.
func TestPruneToNearestCanonEdges_KeepsTheNearestAndItsHalo(t *testing.T) {
	var pieces []CorridorPiece
	const n = 40
	const nearest = 20
	for e := 0; e < n; e++ {
		// Distance rises with |e - nearest|, so edge 20 wins everywhere.
		pieces = append(pieces, nearestTestPiece(e, 0, 1, 1+math.Abs(float64(e-nearest))))
	}
	got := nearestKeptEdges(t, pieces)
	if len(got) == 0 || len(got) == n {
		t.Fatalf("kept %d of %d edges — the prune did nothing, or dropped everything", len(got), n)
	}
	// vnx merge's nearestRunHaloEdges. Restated rather than read, because it is
	// unexported; a change there turns this red, which is wanted: a halo change
	// needs the byte comparison above, not just this test.
	const haloEdges = 8
	lo, hi := nearest-haloEdges, nearest+haloEdges
	for _, e := range got {
		if e < lo || e > hi {
			t.Errorf("kept canon edge %d, outside the nearest edge's halo [%d,%d]", e, lo, hi)
		}
	}
	if len(got) != hi-lo+1 {
		t.Errorf("kept %d edge(s), want the whole halo %d..%d (%d)", len(got), lo, hi, hi-lo+1)
	}
}

// TestPruneToNearestCanonEdges_ASelfApproachKeepsBothRuns is the case the
// per-run grouping exists for: one candidate coming back near the same
// source edge at two DISTANT canon positions. The far approach must survive
// even though it is never the nearer of the two.
func TestPruneToNearestCanonEdges_ASelfApproachKeepsBothRuns(t *testing.T) {
	pieces := []CorridorPiece{
		nearestTestPiece(10, 0, 1, 2),  // near arm
		nearestTestPiece(11, 0, 1, 2),  //
		nearestTestPiece(90, 0, 1, 12), // far arm — a separate approach
		nearestTestPiece(91, 0, 1, 12),
	}
	got := nearestKeptEdges(t, pieces)
	sawNear, sawFar := false, false
	for _, e := range got {
		if e < 50 {
			sawNear = true
		} else {
			sawFar = true
		}
	}
	if !sawNear {
		t.Errorf("the near approach was dropped; kept %v", got)
	}
	if !sawFar {
		t.Errorf("the FAR approach was dropped; kept %v — a gap in canon-edge index is a genuine "+
			"self-approach, not a competitor to be pruned against", got)
	}
}

// TestPruneToNearestCanonEdges_ReturnsARightSizedSlice is the Prove-It for
// the retention defect. The prune used to end
// with `out := pieces[:0]`, so the returned slice carried the PRUNED LENGTH
// with the UN-PRUNED CAPACITY over the caller's own backing array; the caller
// stores it in bySrcEdge, so every rejected piece stayed reachable until
// joinCorridorPieces returned — 281.3 MB held for 24.4 MB wanted on one
// real leg.
//
// CRIPPLE: restore `out := pieces[:0]` and both checks below go red. Note that
// slices.Clip would fix only the first: it lowers capacity but keeps the same
// backing array, so the rejected pieces stay reachable and the alias check
// still fails. That is why this test asserts the array, not just the capacity.
func TestPruneToNearestCanonEdges_ReturnsARightSizedSlice(t *testing.T) {
	var pieces []CorridorPiece
	const n = 40
	for e := 0; e < n; e++ {
		pieces = append(pieces, nearestTestPiece(e, 0, 1, 1+math.Abs(float64(e-20))))
	}
	// Grow the capacity well past the length, the way the mint's own append
	// loop does (sourceEdgeCorridorPieces builds `out` by append, so the
	// capacity is up to ~2× the minted count).
	pieces = append(make([]CorridorPiece, 0, 4*n), pieces...)
	inCap := cap(pieces)

	out, err := PruneToNearestCanonEdges(pieces, nil)
	if err != nil {
		t.Fatalf("pruneToNearestCanonEdges: %v", err)
	}
	if len(out) == 0 || len(out) == len(pieces) {
		t.Fatalf("kept %d of %d piece(s) — the fixture no longer prunes, so it cannot "+
			"witness retention", len(out), len(pieces))
	}
	if cap(out) != len(out) {
		t.Errorf("returned slice has len %d cap %d (input cap %d) — the spare capacity is "+
			"rejected pieces kept reachable through the caller's bySrcEdge", len(out), cap(out), inCap)
	}
	// Alias check: out's backing array must not be inside pieces'. Comparing
	// the first elements' addresses is not enough (a clipped slice starts at
	// the same address), so this asks whether writing through one is visible
	// through the other — the property that actually decides reachability.
	pieces[0].CanonEdge = -12345
	if out[0].CanonEdge == -12345 {
		t.Errorf("out shares its backing array with pieces — a write through pieces[0] was " +
			"visible at out[0], so every rejected piece is still reachable from the result")
	}
}

// TestPruneToNearestCanonEdges_ObserverFires is the liveness the observer's
// own comment declares mandatory and which nothing exercised. A prune that
// silently became a no-op would leave every whole-import lock green, and the only
// symptom would be imports getting slow again.
func TestPruneToNearestCanonEdges_ObserverFires(t *testing.T) {
	var before, after int
	var fired int
	hooks := &TestHooks{NearestPrune: func(b, a int) { fired++; before, after = b, a }}

	var pieces []CorridorPiece
	for e := 0; e < 40; e++ {
		pieces = append(pieces, nearestTestPiece(e, 0, 1, 1+math.Abs(float64(e-20))))
	}
	if _, err := PruneToNearestCanonEdges(pieces, hooks); err != nil {
		t.Fatalf("pruneToNearestCanonEdges: %v", err)
	}
	if fired != 1 {
		t.Fatalf("observer fired %d time(s), want exactly 1", fired)
	}
	if before != 40 {
		t.Errorf("observer reported %d piece(s) before, want 40", before)
	}
	if after >= before {
		t.Errorf("observer reported %d after %d — the prune removed nothing on a dense run of "+
			"competitors, which is the no-op this hook exists to catch", after, before)
	}
}
