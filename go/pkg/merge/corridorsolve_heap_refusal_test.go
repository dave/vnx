package merge

// A refusal inside the search must reach the caller of [SolveCorridorPath] as
// an error, with no partial result. corridorheap_test.go proves the heap
// refuses; these tests prove the solve does not swallow the refusal.
//
// Neither refusal can be built from geometry: the capacity comes from the
// count pass, which counts the states exactly, and every edge cost is
// non-negative by construction. So the heap is put into the refusing state
// through the [TestHooks] corridorSolveHeap hook before the first pop, and the solve
// is otherwise real.

import (
	"math"
	"strings"
	"testing"
)

// solveWithHeapHook runs one real solve (a single candidate under a straight
// source) with hook applied to its heap, and returns what the caller sees.
func solveWithHeapHook(t *testing.T, hook func(q *CorridorHeap)) (*CorridorSolveResult, error) {
	t.Helper()
	pc := prepOne(t, mPt(-20, 0), mPt(120, 0))
	sa, err := NewSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, CorridorRadiusM)
	ctx := WithTestHooks(t.Context(), TestHooks{corridorSolveHeap: hook})
	return SolveCorridorPath(ctx, sa.Total(), sa, ivs, cands, CorridorRadiusM, EmissionK)
}

// TestCorridorSolve_HeapRefusalOnDecreaseIsAnError marks layer 1's OFF state
// popped before the search starts. The first pop, (layer 0, OFF), relaxes its
// forward edge to that state, which is an improvement to a popped state: the
// heap refuses it in decrease (the popped-key rule), and relax must turn that into the solve's
// error.
func TestCorridorSolve_HeapRefusalOnDecreaseIsAnError(t *testing.T) {
	hooked := false
	res, err := solveWithHeapHook(t, func(q *CorridorHeap) {
		if len(q.Pos) < 2 || len(q.Pos[1]) == 0 {
			t.Fatalf("fixture has no layer 1 — the forward edge this test poisons does not exist")
		}
		q.Pos[1][0] = CorridorHeapPopped
		hooked = true
	})
	if !hooked {
		t.Fatal("the heap hook never ran — the solve did not reach its search, so this test measured nothing")
	}
	if err == nil {
		t.Fatalf("an improvement to a popped state returned no error — the heap's refusal was swallowed, and the solve answered %+v", res)
	}
	if res != nil {
		t.Errorf("the refused solve returned a result as well as its error: %+v", res)
	}
	if !strings.Contains(err.Error(), "after it was popped") {
		t.Errorf("the solve failed, but not with the heap's refusal: %v", err)
	}
}

// TestCorridorSolve_HeapRefusalOnInsertIsAnError caps the heap at the one
// entry it holds (the seeded start state), so the first insert is past its
// fixed capacity. That is the error a capacity projection that under-counts
// would raise, and it must reach the caller the same way.
func TestCorridorSolve_HeapRefusalOnInsertIsAnError(t *testing.T) {
	hooked := false
	res, err := solveWithHeapHook(t, func(q *CorridorHeap) {
		q.H = q.H[:len(q.H):len(q.H)]
		hooked = true
	})
	if !hooked {
		t.Fatal("the heap hook never ran — the solve did not reach its search, so this test measured nothing")
	}
	if err == nil {
		t.Fatalf("an insert past the heap's capacity returned no error — the heap's refusal was swallowed, and the solve answered %+v", res)
	}
	if res != nil {
		t.Errorf("the refused solve returned a result as well as its error: %+v", res)
	}
	if !strings.Contains(err.Error(), "fixed capacity") {
		t.Errorf("the solve failed, but not with the heap's refusal: %v", err)
	}
}

// TestArcCost_NegativeCostIsAnError is the guard in [ArcCost] that keeps a
// negative edge out of the Dijkstra. Emission is ≥ 0 by construction and
// refuses on its own, so the only input that reaches the guard is the backtrack
// term with a weight override that is negative or NaN — the parameter a
// weight sweep varies. The piece runs canon measure DOWN while the state is FWD, so the
// backtrack term is non-zero and the weight decides its sign.
func TestArcCost_NegativeCostIsAnError(t *testing.T) {
	// A real piece: the candidate runs WEST under a source that runs east, so
	// its canon measure falls as import measure rises.
	pc := prepOne(t, mPt(120, 0), mPt(-20, 0))
	sa, err := NewSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs := buildIvs(t, sa, []*PreppedCandidate{pc}, CorridorRadiusM)
	sps := BuildSolvePieces(ivs)
	if len(sps) == 0 {
		t.Fatal("fixture minted no pieces")
	}
	sp := sps[0]
	if !(sp.P.CanonMeasureAtHi < sp.P.CanonMeasureAtLo) {
		t.Fatalf("fixture piece runs canon measure %v -> %v, want falling — a FWD state would have no backtrack to price", sp.P.CanonMeasureAtLo, sp.P.CanonMeasureAtHi)
	}
	x0, x1 := sp.P.ImportMeasureAtLo, sp.P.ImportMeasureAtHi
	// Liveness: with the production weight the same piece prices cleanly and
	// the backtrack term is what makes it non-zero.
	c, err := ArcCost(&sp, CorridorFWD, x0, x1, CorridorRadiusM, EmissionK)
	if err != nil {
		t.Fatalf("arcCost with the production weight: %v", err)
	}
	if !(c > 0) {
		t.Fatalf("arcCost with the production weight = %v, want > 0 — the fixture has no backtrack, so the override below cannot make it negative", c)
	}
	for _, w := range []float64{-1, math.NaN()} {
		c, err := ArcCost(&sp, CorridorFWD, x0, x1, CorridorRadiusM, EmissionK, w)
		if err == nil {
			t.Errorf("backtrack weight %v: arcCost returned %v with no error — a negative or NaN edge cost reached the solve", w, c)
			continue
		}
		if !strings.Contains(err.Error(), "negative or NaN") {
			t.Errorf("backtrack weight %v: arcCost failed, but not with the negative-cost guard: %v", w, err)
		}
	}
}
