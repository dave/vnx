package merge

import (
	"context"
	"fmt"
	"testing"
)

// ---- the census ---------------------------------------------------------

// corridorLegCost is one leg's finished record.
//
// SumKeptSq is Σ_e P_e², the quantity the solve's memory grows as, with P_e the
// pieces KEPT at source edge e summed over ALL candidates — see
// [corridorCostObserver.SourceEdgeMinted]. SumMintedSq is the same sum over
// minted pieces, reported beside it so an earlier fit (r = +0.903, which said
// "minted" where it meant kept) can be checked against both.
type corridorLegCost struct {
	SourceEdges int
	Candidates  int
	Minted      int
	Kept        int
	SumKeptSq   int
	SumMintedSq int
	PhaseEvents int
	Solved      bool
	Layers      int
	States      int
	Relaxations int
	PQPushes    int
	PQPops      int
}

// corridorCostCensus accumulates [corridorCostObserver] calls into one record
// per leg.
//
// ⚠ IT ASSERTS ITS OWN CALL SEQUENCE. A census is an instrument that can go
// silently dead — a hook that stopped firing, or fired for half a leg, would
// leave every test green and report a memory saving nobody made. So every
// out-of-order or missing call is recorded as a violation and the test fails on
// it. There is no tolerant path and no zero-filling: a count that cannot be
// produced is an error, never a zero.
type corridorCostCensus struct {
	legs []corridorLegCost

	open bool
	cur  corridorLegCost
	// curInserts is the open leg's pqInserts, kept beside cur rather than in
	// it so the pinned per-leg records do not change shape. Read only by
	// LegFinished's pops == inserts + 1, and only after SolveFinished set it.
	curInserts int
	keptBy     []int
	mintedBy   []int
	mintCalls  int
	violations []string
}

func (c *corridorCostCensus) violate(format string, args ...any) {
	c.violations = append(c.violations, fmt.Sprintf(format, args...))
}

func (c *corridorCostCensus) LegStart(sourceEdges, candidates int) {
	if c.open {
		c.violate("LegStart while leg %d is still open", len(c.legs))
	}
	c.open = true
	c.cur = corridorLegCost{SourceEdges: sourceEdges, Candidates: candidates}
	c.keptBy = make([]int, sourceEdges)
	c.mintedBy = make([]int, sourceEdges)
	c.mintCalls = 0
}

func (c *corridorCostCensus) SourceEdgeMinted(srcEdge, minted, kept int) {
	if !c.open {
		c.violate("SourceEdgeMinted(%d) with no open leg", srcEdge)
		return
	}
	if c.cur.PhaseEvents > 0 {
		c.violate("SourceEdgeMinted(%d) after the mint-finished phase event", srcEdge)
	}
	if srcEdge < 0 || srcEdge >= len(c.keptBy) {
		c.violate("SourceEdgeMinted names source edge %d, outside the leg's %d", srcEdge, len(c.keptBy))
		return
	}
	if kept > minted {
		c.violate("source edge %d kept %d of %d minted pieces — the prune cannot add pieces", srcEdge, kept, minted)
	}
	c.mintCalls++
	c.mintedBy[srcEdge] += minted
	c.keptBy[srcEdge] += kept
	c.cur.Minted += minted
	c.cur.Kept += kept
}

func (c *corridorCostCensus) MintFinished() {
	if !c.open {
		c.violate("MintFinished with no open leg")
		return
	}
	c.cur.PhaseEvents++
}

func (c *corridorCostCensus) SolveFinished(layers, states, relaxations, pqPushes, pqInserts, pqPops int) {
	if !c.open {
		c.violate("SolveFinished with no open leg")
		return
	}
	if c.cur.PhaseEvents != 1 {
		c.violate("SolveFinished after %d phase event(s), want exactly 1 — the probe's mint/solve split reads that boundary", c.cur.PhaseEvents)
	}
	if c.cur.Solved {
		c.violate("SolveFinished twice on one leg")
	}
	c.cur.Solved = true
	c.cur.Layers, c.cur.States = layers, states
	c.cur.Relaxations, c.cur.PQPushes, c.cur.PQPops = relaxations, pqPushes, pqPops
	c.curInserts = pqInserts
	// pops == inserts + 1 is checked in LegFinished, not here: a STOPPED solve
	// (cancelled, or refused by its heap) reports here too and does not drain
	// its heap. Every insert is an improvement, stopped or not, so
	// pushes >= inserts holds on every report.
	if pqPushes < pqInserts {
		c.violate("solve improved %d state(s) but inserted %d — every insert is an improvement", pqPushes, pqInserts)
	}
}

func (c *corridorCostCensus) LegFinished() {
	if !c.open {
		c.violate("LegFinished with no open leg")
		return
	}
	// The search heap holds each state at most once and every pop is live, so
	// every pop but the seeded start item is the pop of one insert, and a
	// completed search runs until the heap is empty: pops == inserts + 1. It is
	// the census's own liveness check on the two counters: a dropped increment
	// on either side breaks it.
	//
	// ⚠ ONLY A COMPLETED SOLVE, which is why it is checked here and not in
	// SolveFinished. A stopped solve reports SolveFinished and then returns its
	// error, so its leg never reaches this call ([corridorCostObserver]'s
	// LegFinished: "a record left open is exactly how a census reader sees a
	// stop").
	//
	// ⚠ IT WAS pops == pushes + 1 UNTIL the indexed heap replaced the lazy
	// queue, when the lazy queue pushed a duplicate item on every improvement and popped
	// each one, stale or not.
	if c.cur.Solved && c.cur.PQPops != c.curInserts+1 {
		c.violate("solve popped %d and inserted %d — a completed solve drains its heap, so pops must be inserts + 1 (the seeded start item)", c.cur.PQPops, c.curInserts)
	}
	if want := c.cur.SourceEdges * c.cur.Candidates; c.mintCalls != want {
		c.violate("leg %d reported %d mint records, want %d (%d source edges × %d candidates) — a missing record is a dead hook, not a zero",
			len(c.legs), c.mintCalls, want, c.cur.SourceEdges, c.cur.Candidates)
	}
	for _, p := range c.keptBy {
		c.cur.SumKeptSq += p * p
	}
	for _, m := range c.mintedBy {
		c.cur.SumMintedSq += m * m
	}
	c.legs = append(c.legs, c.cur)
	c.open = false
}

// armCorridorCostCensus puts a new census into h. The census reaches a call
// only through a context made from h with WithTestHooks.
func armCorridorCostCensus(t *testing.T, h *TestHooks) *corridorCostCensus {
	t.Helper()
	if h.CorridorCost != nil {
		t.Fatal("a corridor cost census is already armed on these hooks")
	}
	c := &corridorCostCensus{}
	h.CorridorCost = c
	return c
}

// censusContext arms a new census on its own hooks and returns it with a
// context carrying them, for a test that needs no other hook.
func censusContext(t *testing.T, ctx context.Context) (*corridorCostCensus, context.Context) {
	t.Helper()
	var h TestHooks
	c := armCorridorCostCensus(t, &h)
	return c, WithTestHooks(ctx, h)
}

// check fails the test on any recorded call-sequence violation.
func (c *corridorCostCensus) check(t *testing.T) {
	t.Helper()
	if c.open {
		t.Error("census: a leg is still open — LegFinished never fired")
	}
	for _, v := range c.violations {
		t.Errorf("census: %s", v)
	}
}

// checkStopped is [corridorCostCensus.check] for a run whose last leg was
// STOPPED in its solve: that leg must be left open (a stop never reaches
// LegFinished) and must have reported SolveFinished, and nothing recorded may
// be a violation.
func (c *corridorCostCensus) checkStopped(t *testing.T) {
	t.Helper()
	if !c.open {
		t.Error("census: no leg is open — a stopped solve's leg must never reach LegFinished")
	}
	if !c.cur.Solved {
		t.Error("census: the stopped leg never reported SolveFinished")
	}
	for _, v := range c.violations {
		t.Errorf("census: %s", v)
	}
}

// ---- the hand-built leg -------------------------------------------------

// censusCanonNorth is a straight canon arc of 12 one-metre edges running north
// from the test frame's origin: vertex i sits at (0 east, i north).
func censusCanonNorth() CanonArc {
	verts := make([]Point, 13)
	for i := range verts {
		verts[i] = mPt(0, float64(i))
	}
	return mCanon(1, verts...)
}

// TestCorridorCostCensus_HandBuiltLeg pins every census field against a leg
// whose numbers are derived from the geometry below, before the code was run.
//
// THE GEOMETRY. One canon arc of 12 edges, each 1 m, running north from the
// origin (vertex i at north = i). One source edge of 0.5 m, running north from
// north = 0.25 to north = 0.75, held 2 m to the EAST of the canon. So the whole
// leg lies alongside canon edge 0, offset by 2 m, and never reaches a canon
// vertex.
//
// WHY THE OFFSET. Laid exactly ON the canon, the source's own import measure and
// the canon measure its foot travels are the same 0.5 m, and two of the solve's
// prices then tie to the last bit (OFF's raw metres against a reversed run's
// backtrack charge). A tie is decided by float noise, so the counts below would
// not be derivable. Two metres east makes the reversed run cost 0.02 more than
// OFF, which is a margin, not a coin toss. The offset costs nothing else: the
// source's longitude is constant, so the projection parameter s along every
// canon edge is exactly what it would be at zero offset.
//
// ---- mint ----
//
// For canon edge j (from vertex j to vertex j+1) the source's projection
// parameter runs s(t) = (0.25 − j) + 0.5t, so the foot is interior to canon edge
// 0 for the whole source edge, and clamps to vertex j for every j ≥ 1. That is
// one BandPieces regime per canon edge, never two:
//
//	j = 0     RegimeSpan, the full source edge, distance 2 m throughout
//	j ≥ 1     RegimeNearA, the full source edge, distance √((j−pos)² + 2²)
//
// The corridor radius is 10 m ([CorridorRadiusM]). The farthest point of the
// source from vertex j is at north = 0.25, so canon edge j qualifies while
// (j − 0.25)² + 4 ≤ 100, i.e. j ≤ 10. Edge 10 is in by 0.06 m and edge 11 is out
// by 0.43 m, so neither verdict is a rounding question.
//
//	MINTED = 1 (edge 0) + 10 (edges 1..10) = 11 pieces, on ONE source edge.
//
// ---- the prune ----
//
// The pieces form one contiguous canon-edge run, 0..10. Canon edge 0 is 2 m away
// everywhere; the nearest any clamped foot gets is vertex 1 at
// √(0.25² + 2²) = 2.0156 m. So canon edge 0 wins at every sample and every
// midpoint, and [PruneToNearestCanonEdges] keeps its halo of
// [NearestRunHaloEdges] = 8 either side, clipped to the run:
//
//	KEPT = canon edges 0..8 = 9 pieces.
//
// Kept < minted, which is the point of measuring both. One source edge and one
// candidate, so Σ_e P_e² = 9² = 81 and Σ_e minted_e² = 11² = 121.
//
// ---- the solve ----
//
// Every kept piece spans the whole source edge, so every piece boundary is
// either 0 or the leg's total measure, and those are the only breakpoints:
//
//	LAYERS = 2.
//
// Each layer holds OFF plus a FWD and a REV state for each of the 9 pieces:
//
//	STATES = 2 × (1 + 18) = 38.
//
// Write P = 50 ([stateChangeCostP]), L ≈ 0.4994 m for the leg's measure length,
// and a_i for piece i's forward arc cost — the emission integral ∫(d/10)² over
// the arc, which is 0.0199 for the span piece and rises from 0.021 to 0.301 for
// the clamped ones. Piece 0 REVERSED also pays its backtrack, 1.0 × the 0.4994 m
// of canon measure its foot travels, so its arc costs ≈ 0.5194 — the one arc
// cost above L. The search then runs:
//
// The search queue is an indexed heap: each
// state is in it at most once, a first improvement INSERTS it and a later one
// DECREASES it in place. PUSHES counts both, as it always has.
//
//  1. Pop the seed (layer 0, OFF) at 0. The generic fan-out prices all 18 ON
//     states at layer 0 at P; the forward edge prices (layer 1, OFF) at L.
//     19 relaxations, 19 pushes, all 19 of them inserts.
//
//  2. Pop (layer 1, OFF) at L — cheaper than P. Its fan-out prices all 18 ON
//     states at layer 1 at L + P. 18 relaxations, 18 pushes, 18 inserts.
//
//  3. Pop the 18 layer-0 ON states, all at P. Each relaxes 17 same-candidate
//     switch edges (every kept piece is on the one candidate) and one forward
//     edge; the first of them also fires the runner-up fan-out back at OFF.
//     18 × 18 + 1 = 325 relaxations. No switch can improve anything — the
//     cheapest is the 0.75 m measure gap between the span piece and vertex 1,
//     and P + 0.75 > P. Each forward edge improves its own layer-1 slot from
//     L + P to P + a_i, except piece 0 REVERSED, whose 0.5194 exceeds L.
//     17 pushes, all decreases: each of those slots was inserted in step 2.
//
//  4. Pop the 18 layer-1 ON states, each once, cheapest first. Each relaxes its
//     17 switch edges; the first also fires the runner-up fan-out.
//     17 × 18 + 1 = 307 relaxations. Exactly one of these improves anything:
//     at layer 1 the span piece's foot has advanced to within 0.25 m of vertex
//     1, so piece 1 REVERSED reaches piece 0 REVERSED for 0.25 rather than the
//     L + P the fan-out had left there. 1 push, a decrease: piece 0 REVERSED is
//     the dearest layer-1 ON state (L + P, untouched in step 3), so it is still
//     in the heap when the cheaper piece 1 REVERSED is expanded.
//
//     The heap is now empty. There is no step 5: the lazy queue it replaced held
//     18 superseded layer-1 items at this point — the 17 decreases of step 3 and
//     the 1 of step 4 had each pushed a duplicate — and popped them to no effect.
//     Their stale dists (L + P against P + a_i) were far outside the 1e-9 window,
//     so none of them was ever re-expanded, and removing them removes no
//     relaxation.
//
//     RELAXATIONS = 19 + 18 + 325 + 307 = 669
//     PUSHES      = 19 + 18 +  17 +   1 =  55  (37 inserts + 18 decreases)
//     POPS        = 1 + 1 + 18 + 18     =  38  (= STATES = inserts + 1)
//
// Before T3 POPS was 56 (= pushes + 1), with step 5's 18 stale pops.
func TestCorridorCostCensus_HandBuiltLeg(t *testing.T) {
	c, ctx := censusContext(t, t.Context())

	leg := []Point{mPt(2, 0.25), mPt(2, 0.75)}
	if _, err := corridorFollowRuns(ctx, leg, []CanonArc{censusCanonNorth()}, defaultFollowBands()); err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	c.check(t)

	want := corridorLegCost{
		SourceEdges: 1,
		Candidates:  1,
		Minted:      11,
		Kept:        9,
		SumKeptSq:   81,
		SumMintedSq: 121,
		PhaseEvents: 1,
		Solved:      true,
		Layers:      2,
		States:      38,
		Relaxations: 669,
		PQPushes:    55,
		PQPops:      38,
	}
	if len(c.legs) != 1 {
		t.Fatalf("census recorded %d legs, want 1", len(c.legs))
	}
	if got := c.legs[0]; got != want {
		t.Errorf("leg census mismatch\n got %+v\nwant %+v", got, want)
	}
}

// TestCorridorCostCensus_DegenerateLegs pins what the census reports for the
// legs that never reach a solve. Both shapes are ordinary production input, so
// the answer has to be stated rather than left to whatever falls out.
//
//   - 0 or 1 points: [corridorFollowRuns] returns on its degenerate-leg
//     contract before it prepares a candidate, so there is NO record at all —
//     no LegStart, and no phase event.
//   - 2 or more points that all coincide: the leg is real, so it opens a record
//     and mints (nothing: every source edge has zero measure length). It
//     reaches the mint→solve boundary and the phase event fires there. Then
//     total == 0 and [solveCorridorPath] returns nil before doing any work, so
//     the record closes with Solved false — NOT with zeroed solve counts, which
//     would be indistinguishable from a solve that ran and found nothing.
//
// That is why the phase event's contract is "once per leg that reaches the
// boundary" and not "once per solve": a memory probe has to split its
// reading before solveCorridorPath is entered, which is upstream of the point
// where the solve decides it has nothing to do.
func TestCorridorCostCensus_DegenerateLegs(t *testing.T) {
	canon := []CanonArc{censusCanonNorth()}

	t.Run("no points", func(t *testing.T) {
		c, ctx := censusContext(t, t.Context())
		if _, err := corridorFollowRuns(ctx, nil, canon, defaultFollowBands()); err != nil {
			t.Fatalf("corridorFollowRuns: %v", err)
		}
		c.check(t)
		if len(c.legs) != 0 {
			t.Errorf("census recorded %d legs for a 0-point leg, want 0: %+v", len(c.legs), c.legs)
		}
	})

	t.Run("one point", func(t *testing.T) {
		c, ctx := censusContext(t, t.Context())
		if _, err := corridorFollowRuns(ctx, []Point{mPt(2, 0.25)}, canon, defaultFollowBands()); err != nil {
			t.Fatalf("corridorFollowRuns: %v", err)
		}
		c.check(t)
		if len(c.legs) != 0 {
			t.Errorf("census recorded %d legs for a 1-point leg, want 0: %+v", len(c.legs), c.legs)
		}
	})

	t.Run("all points coincide", func(t *testing.T) {
		c, ctx := censusContext(t, t.Context())
		p := mPt(2, 0.25)
		if _, err := corridorFollowRuns(ctx, []Point{p, p, p}, canon, defaultFollowBands()); err != nil {
			t.Fatalf("corridorFollowRuns: %v", err)
		}
		c.check(t)
		want := corridorLegCost{
			SourceEdges: 2,
			Candidates:  1,
			PhaseEvents: 1,
			// Minted, Kept and both sums are 0 because two zero-length source
			// edges mint nothing, and both were REPORTED as zero rather than
			// skipped — c.check would have caught a missing record.
		}
		if len(c.legs) != 1 {
			t.Fatalf("census recorded %d legs, want 1", len(c.legs))
		}
		if got := c.legs[0]; got != want {
			t.Errorf("degenerate leg census mismatch\n got %+v\nwant %+v", got, want)
		}
	})
}
