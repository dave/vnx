package merge

// Test hooks. Each is nil (or zero) in production and costs one nil check;
// a test assigns one to observe a decision the merge makes inside a leg.

// CorridorSolveFootprintObserverForTests receives one [CorridorSolveFootprint]
// per solve. Production leaves it nil, so a solve pays one nil check per leg —
// the same arrangement as [CorridorSolveTableObserverForTests].
var CorridorSolveFootprintObserverForTests func(CorridorSolveFootprint)

// NearestPruneObserverForTests, when non-nil, receives (before, after) piece
// counts for every source edge the prune runs on. LIVENESS, and it is not
// optional: a prune that silently became a no-op would leave every corpus
// lock green and every test passing, and the only symptom would be an import
// that got slow again.
var NearestPruneObserverForTests func(before, after int)

// CorridorSolveTableObserverForTests receives the finished Dijkstra table so a
// test can check it is actually a shortest-path table. Production leaves it
// nil, so the solve pays one nil check per leg.
// ivs is [CorridorIvBounds], not [CorridorInterval]: by the time this fires the
// pieces have been released (see the release comment in [SolveCorridorPath]), so
// the hook is given exactly what still exists rather than a struct whose Pieces
// field would silently read nil.
//
// ⚠ statesBy is [][]CorridorPackedState — THE SOLVE'S OWN LISTS, not a decoded
// copy. Handing over a decoded copy would keep this signature, but
// TestCorridorSolve_StateListBytes asserts cap == len on what it is given, and
// a copy built at its own length is exact-fit whatever the solve allocated: that
// guard would pass for the wrong reason and could never fail again. (The
// capacities are also measured by TestCorridorSolve_Footprint, through its own
// observer, and bounded by TestCorridorSolve_RefusesAtTheProjectedBoundary.) Observers decode per element with [CorridorPackedState.unpack].
var CorridorSolveTableObserverForTests func(bps []float64, statesBy [][]CorridorPackedState, sps []SolvePiece, cands []*PreppedCandidate, dist [][]float64, t CorridorTunables, r float64, k int, ivs []CorridorIvBounds, layerOf map[float64]int, bridgesFrom map[int][]CorridorBridge)

// CorridorSolveHeapHookForTests is handed the solve's search heap once, after
// the start state is seeded and before the first pop. It exists so a test can
// put the heap in a state that makes it refuse an operation — a full capacity,
// or a state marked popped — and prove the refusal leaves the solve as an error
// with no partial result (TestCorridorSolve_HeapRefusalOnInsertIsAnError and
// TestCorridorSolve_HeapRefusalOnDecreaseIsAnError). A real refusal
// needs a capacity projection that under-counts or a negative edge cost, and
// neither can be built from geometry. nil in production: one nil check per
// solve, outside the search loop.
var CorridorSolveHeapHookForTests func(q *CorridorHeap)

// CorridorCostObserverForTests is the census hook. nil in production — see
// [corridorCostObserver]. Read sequentially: a census test must not run in
// parallel with another.
var CorridorCostObserverForTests corridorCostObserver

// warpBoundaryZoneObserverForTests reports, on every WarpLegAtBoundaries
// call, how much of the leg the taper actually touched — leg name, total
// point count, touched-index count, touched-and-NOT-a-seam-index count,
// boundary count, and the leg's own measure length. A temporary diagnostic,
// built to measure how much of a leg the taper touches; nil in real builds.
var warpBoundaryZoneObserverForTests func(leg string, numPts, touched, touchedNonSeam, numBoundaries int, legLenM float64)

// snapCensusRecorder is a MEASUREMENT-ONLY hook — nil in every real build.
// attempted reports whether the reconciled trial pair differed from the
// original measures; accepted reports whether CheckCorridor passed it.
// entryMech/exitMech are "own-end", "any-node", or "none" per
// [snapMeasureToNearbyNodeCensus].
var snapCensusRecorder func(attempted, accepted bool, entryMech, exitMech string)

// CurrentLegForTests names the leg currently being merged. The caller sets it
// before each leg, unconditionally (a string assignment, cheaper than checking
// whether any hook is installed). Only test observers read it, so it has no
// effect unless a test has installed one.
var CurrentLegForTests string
