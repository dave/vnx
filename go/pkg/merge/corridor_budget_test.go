package merge

import (
	"strconv"
	"strings"
	"testing"
)

// corridorLegFootprint is one solve's footprint with the leg it belongs to.
// The leg index is read on the import goroutine from the census's own finished
// count, so it is the same index the census files its legs under.
type corridorLegFootprint struct {
	Leg int
	F   CorridorSolveFootprint
}

// armCorridorSolveFootprint puts the footprint observer into h. It reaches a
// call only through a context made from h with WithTestHooks.
//
// c may be nil; when it is not, each footprint is tagged with the leg index the
// census is currently accumulating.
func armCorridorSolveFootprint(t *testing.T, h *TestHooks, c *corridorCostCensus) *[]corridorLegFootprint {
	t.Helper()
	if h.CorridorSolveFootprint != nil {
		t.Fatal("a corridor solve footprint observer is already armed on these hooks")
	}
	out := &[]corridorLegFootprint{}
	h.CorridorSolveFootprint = func(f CorridorSolveFootprint) {
		leg := -1
		if c != nil {
			leg = len(c.legs) // the open leg; LegFinished appends it afterwards
		}
		*out = append(*out, corridorLegFootprint{Leg: leg, F: f})
	}
	return out
}

// solveOneStraightLeg is the smallest leg with real state tables: a single
// candidate running under the whole source. solve runs it under a slot-table
// budget (TestHooks.SolveSlotTableBudgetBytes; production sets none) and
// returns the solve's error; footprints collects what every solve observed.
func solveOneStraightLeg(t *testing.T) (footprints *[]corridorLegFootprint, solve func(budget int) error) {
	t.Helper()
	const R = CorridorRadiusM
	pc := prepOne(t, mPt(-20, 0), mPt(120, 0))
	sa, err := newSourceMeasure(straightLegX(9, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs := buildIvs(t, sa, []*PreppedCandidate{pc}, R)
	var h TestHooks
	fps := armCorridorSolveFootprint(t, &h, nil)
	return fps, func(budget int) error {
		h := h
		h.SolveSlotTableBudgetBytes = budget
		// A fresh clone per solve: solveCorridorPath takes ownership of the
		// intervals and releases their pieces.
		_, err := solveCorridorPath(WithTestHooks(t.Context(), h), sa.Total(), sa, cloneCorridorIntervals(ivs), []*PreppedCandidate{pc}, R, emissionK)
		return err
	}
}

// TestCorridorSolve_RefusesAtTheProjectedBoundary measures what one leg's six
// slot tables (the four state tables and the heap's two) really allocate, then
// asks for that many bytes minus one.
//
// ⚠ THE BOUNDARY IS THE MEASUREMENT, NOT A ROUND NUMBER. Budget == the measured
// size must PASS and budget == measured − 1 must FAIL, which is only true if
// [corridorProjectedSlotBytes] projects the allocation exactly. A gate tested
// with a budget of 1 against a table of megabytes would pass just as happily
// while projecting a different quantity entirely.
func TestCorridorSolve_RefusesAtTheProjectedBoundary(t *testing.T) {
	fps, solve := solveOneStraightLeg(t)

	// Control: the shipped budget refuses nothing, and the solve reports what
	// its tables cost.
	if err := solve(corridorSolveBudgetUnset); err != nil {
		t.Fatalf("the unbudgeted control leg failed: %v", err)
	}
	if len(*fps) != 1 {
		t.Fatalf("observed %d footprint(s), want 1 — the control solve is not the leg being measured", len(*fps))
	}
	f := (*fps)[0].F
	measured := f.SlotTableBytes()
	if measured <= 0 || f.SlotsLen <= 2 {
		t.Fatalf("the control leg has no state tables worth budgeting (%d slots, %d B) — this test would pass on a gate that does nothing", f.SlotsLen, measured)
	}

	// At the budget: allowed. The gate refuses what is OVER budget, not what
	// exactly fits.
	*fps = nil
	if err := solve(measured); err != nil {
		t.Fatalf("a leg whose tables measure exactly the budget (%d B) was refused: %v", measured, err)
	}
	if len(*fps) != 1 {
		t.Fatalf("observed %d footprint(s) at the exact budget, want 1", len(*fps))
	}

	// One byte under: refused.
	*fps = nil
	err := solve(measured - 1)
	if err == nil {
		t.Fatalf("a leg needing %d B was allowed under a %d B budget — the gate is not reading the projection", measured, measured-1)
	}
	msg := err.Error()
	for _, want := range []string{
		"refused before allocating",
		// the projected bytes, which must be the measured bytes
		strconv.Itoa(measured),
		// the budget, the slot count and the layer count
		strconv.Itoa(measured - 1),
		strconv.Itoa(f.SlotsLen),
		strconv.Itoa(f.Layers),
		strconv.Itoa(f.Pieces),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q, so the log cannot say what was too big or by how much: %v", want, err)
		}
	}

	// ⚠ AND IT REFUSED BEFORE THE TABLES EXISTED. The footprint observer fires
	// where the search has finished; a refusal that allocated first and failed
	// afterwards would still have emitted one.
	if len(*fps) != 0 {
		t.Errorf("the refused solve emitted %d footprint(s) — it ran instead of refusing before allocating", len(*fps))
	}
}
