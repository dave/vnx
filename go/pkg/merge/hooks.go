package merge

import "context"

// TestHooks are observers and switches for tests. A test hands them to one
// call through its context, with [WithTestHooks]; nothing else reads them.
// Every field's zero value (nil, false or 0) is production behaviour, so a
// context without TestHooks merges exactly as a real import does.
//
// An observer that takes a leg string is given the [LegOptions].Label of the
// AddLeg call it fires in, or "" when the step was called directly, outside
// AddLeg.
//
// FOR TESTS ONLY. The fields are not part of the merge's API: they name
// internal decisions and change whenever the merge does.
type TestHooks struct {
	// CorridorSolveFootprint receives one [CorridorSolveFootprint] per solve.
	// Production leaves it nil, so a solve pays one nil check per leg.
	CorridorSolveFootprint func(CorridorSolveFootprint)

	// nearestPrune receives (before, after) piece counts for every source
	// edge the prune runs on. LIVENESS, and it is not optional: a prune that
	// silently became a no-op would leave every corpus lock green and every
	// test passing, and the only symptom would be an import that got slow
	// again.
	nearestPrune func(before, after int)

	// CorridorSolveTable receives the finished Dijkstra table so a test can
	// check it is actually a shortest-path table.
	// ivs is [CorridorIvBounds], not [corridorInterval]: by the time this
	// fires the pieces have been released (see the release comment in
	// [solveCorridorPath]), so the hook is given exactly what still exists
	// rather than a struct whose Pieces field would silently read nil.
	//
	// ⚠ statesBy is [][]CorridorPackedState — THE SOLVE'S OWN LISTS, not a
	// decoded copy. Handing over a decoded copy would keep this signature,
	// but a state-list test asserts cap == len on what it is
	// given, and a copy built at its own length is exact-fit whatever the
	// solve allocated: that guard would pass for the wrong reason and could
	// never fail again. (The capacities are also measured by
	// the footprint test, through its own observer, and bounded by
	// the budget refusal.) Observers decode per
	// element with [CorridorPackedState.unpack].
	CorridorSolveTable func(bps []float64, statesBy [][]CorridorPackedState, sps []SolvePiece, cands []*PreppedCandidate, dist [][]float64, t CorridorTunables, r float64, k int, ivs []CorridorIvBounds, layerOf map[float64]int, bridgesFrom map[int][]CorridorBridge)

	// corridorSolveHeap is handed the solve's search heap once, after the
	// start state is seeded and before the first pop. It exists so a test
	// can put the heap in a state that makes it refuse an operation — a full
	// capacity, or a state marked popped — and prove the refusal leaves the
	// solve as an error with no partial result
	// (TestCorridorSolve_HeapRefusalOnInsertIsAnError and
	// TestCorridorSolve_HeapRefusalOnDecreaseIsAnError). A real refusal needs a
	// capacity projection that under-counts or a negative edge cost, and
	// neither can be built from geometry.
	corridorSolveHeap func(q *corridorHeap)

	// CorridorCost is the cost census; see [CorridorCostObserver].
	CorridorCost CorridorCostObserver

	// SolveSlotTableBudgetBytes caps what one leg's six slot-sized tables —
	// the four state tables and the search heap — may allocate. The gate
	// that reads it is in [solveCorridorPath], between the count pass and the
	// allocation; corridorfootprint.go says why it is not yet a real option.
	// 0 or a negative value is no budget, which is what production runs
	// with.
	SolveSlotTableBudgetBytes int

	// A1AbsorbIgnoreMeasure, when non-nil and true, drops the along-canon
	// half of the absorb search's candidate rule (see [nearestLineageNode]
	// and nodeIsAtMeasure), so a test can prove the ride-resolution guard
	// fires.
	A1AbsorbIgnoreMeasure func() bool

	// suppressNearbyAdopt, when non-nil, is called by the align step's
	// canon cut right after the nearby-node check's own nearby-node search decides
	// whether to adopt — if it returns true, the adopt is forced to false
	// regardless of what the search found. FAULT INJECTION, not a diagnostic:
	// the post-mint one-node-rule invariant check is supposed to be
	// unreachable under correct code (the nearby-node check would already have adopted
	// anything it could catch), so testing its abort path means simulating
	// the nearby-node check failing. Called on EVERY nearby-node decision, not only when it
	// returns true, so a test can confirm the injection point was actually
	// reached.
	suppressNearbyAdopt func() bool

	// A1ForeignAbsorb is called for every boundary the nearby-node check's lineage
	// preference redirected from an absorb to a cut: the node it refused, how
	// far away that was, and the node the cut minted instead, reported AFTER
	// the mint so an observer can classify the new node by its degree rather
	// than just count it.
	A1ForeignAbsorb func(leg string, runCanonID ArcID, run int, atRunEnd bool, refused NodeID, distM float64, minted NodeID)

	// DuplicateArc is called for EVERY piece the commit path is about to
	// write, with the detector's full result — empty for the ordinary case.
	//
	// ⚠ EVERY PIECE, INCLUDING THE MISSES. An observer that only fired on a
	// hit could report a rate but never a denominator, and "the detector fired
	// 10 times" is not a measurement without "out of how many pieces".
	DuplicateArc func(matches []DuplicateArcMatch, startNode, endNode *NodeID)

	// InteriorNodeSeam reports every leg's whole candidate set, accepted and
	// declined alike.
	//
	// It exists because a DECLINE leaves no trace whatsoever — the leg commits
	// exactly as it would have — so "the rule looked here and refused" is
	// otherwise unfalsifiable at scale, and a committed arc still running
	// through a node is equally consistent with a correct decline and with the
	// rule never having fired at all.
	InteriorNodeSeam func(leg string, rep InteriorNodeSeamReport)

	// RingSplitExclusion is called with (leg, mintedNodeID, excludedNodeID,
	// distM) every time commitPiece mints a GENUINE tight-ring residual: a
	// ring split whose nearby-node lookup excluded the piece's own start/end,
	// found nothing else within EndpointWarpRadiusM, minted fresh — but the
	// excluded node itself WAS within radM of the mint. That is the shape a
	// check of the one-node rule needs to exempt. The computation itself (one
	// HaversineMetres call against coordinates already in hand) is cheap
	// enough to make unconditionally, but the RESULT only matters to a test,
	// so it is only reported when a test is listening.
	//
	// A test-only need does not extend a production result type; if a
	// production consumer genuinely needs these pairs later, promote this
	// then.
	RingSplitExclusion func(leg string, mintedNodeID, excludedNodeID NodeID, distM float64)

	// LegSeamConflict reports EVERY tie-break legSeamsFromAlign resolves
	// between two different candidate nodes at one leg point: leg name, point
	// index, the two candidate node IDs, and each one's TRUE chord distance
	// from the leg's own raw point. It lets tests check that the tie-break
	// fires only on genuine rare edge cases rather than masking something
	// systemic.
	LegSeamConflict func(leg string, idx int, prevNode, node NodeID, prevD, thisD float64)

	// LegRide receives every leg's resolved rides and the ON pieces that
	// resolved to none. Tests use it to measure the resolved fraction and
	// name the remainder.
	LegRide func(leg string, rides *LegRides)

	// LegRideSuppress, when non-nil and true, makes every piece a CREATE:
	// no piece is given its ride. The claim "referencing the ride is what
	// removes the parallel copies" is only shown by a run in which it does
	// not happen.
	LegRideSuppress func() bool

	// LegRideFault may corrupt a resolved ride before the writer sees it.
	// See resolveLegRides' call site.
	LegRideFault func(*LegRide)

	// SymmetricSnap reports EVERY leg's whole symmetric-snap candidate set,
	// adopted and declined alike.
	SymmetricSnap func(leg string, rep SymmetricSnapReport)

	// SnapReverseSearchOrder reverses the symmetric snap's SEARCH's raw
	// result order before its sort, for the duration of one measurement.
	//
	// ⚠ A FALSIFICATION SEAM, NOT A FEATURE FLAG: what separates an
	// instrument from a kill switch in production source is a stated
	// condition for removal.
	//
	// WHAT IT IS FOR. Coincident candidates are resolved by picking one
	// deterministically, and a determinism test that cannot FAIL proves
	// nothing. The hazard is a resolution that depends on the order results
	// happened to come back in, so the only honest test injects exactly that:
	// run the same import twice, once with the search's order reversed, and
	// require the committed network to be identical. It reverses BEFORE the
	// sort on purpose — reversing after would grade the sort against itself
	// and could not detect an incomplete sort key, which is the actual defect
	// it exists to catch.
	//
	// ⚠ REMOVE IT WHEN: the tie-break's deciding key stops being the
	// candidate sort — if a future change resolves coincident candidates by an
	// explicit comparison instead of by acceptance order, that comparison can
	// be pinned directly and this seam has nothing left to falsify. It is NOT
	// a switch for production, has no plumbing to reach one, and must never
	// acquire any. The determinism test is its only reader.
	SnapReverseSearchOrder bool

	// SymmetricSnapOnCanon is called once per leg with the symmetric snap's
	// own report and the ON ranges it was judged against. It lets a test
	// count adoptions inside an ON stretch from the producer's OWN record
	// rather than from the rule under test.
	SymmetricSnapOnCanon func(leg string, rep SymmetricSnapReport, onRanges []OnPieceRange)

	// LegTerminalGap is called with the RAW RESULT of every lookup
	// warpUnresolvedLegTerminals runs (one call per still-unresolved side,
	// whether or not it found anything) so a test can confirm the lookup ran
	// at all, not only infer it from an adoption.
	LegTerminalGap func(leg string, side string, found bool, nearbyID NodeID, nearbyDistM float64)
}

// WithTestHooks returns a copy of ctx that carries h: every merge step
// called with the returned context, directly or through [AddLeg], reports
// to h's observers and obeys its switches. It replaces any TestHooks ctx
// already carries.
//
// FOR TESTS ONLY. Production passes a context without TestHooks.
func WithTestHooks(ctx context.Context, h TestHooks) context.Context {
	st := *stateOf(ctx)
	st.hooks = h
	return context.WithValue(ctx, callStateKey{}, &st)
}

// callStateKey is the context key of a call's [callState].
type callStateKey struct{}

// callState is what one call carries on its context besides cancellation:
// the test hooks, and, inside [AddLeg], the leg's label and Logf. It is
// immutable once on a context, so concurrent calls share nothing.
type callState struct {
	hooks TestHooks
	leg   string
	logf  func(format string, args ...any)
}

// noCallState is the state of a context that carries none: no hooks, no
// label, silent.
var noCallState callState

// stateOf returns the call state ctx carries, or the empty one. Callers read
// it once per function, never inside a loop.
func stateOf(ctx context.Context) *callState {
	if st, ok := ctx.Value(callStateKey{}).(*callState); ok {
		return st
	}
	return &noCallState
}

// withLeg returns ctx carrying ctx's hooks plus the leg's label and Logf.
func withLeg(ctx context.Context, label string, logf func(format string, args ...any)) context.Context {
	st := *stateOf(ctx)
	st.leg, st.logf = label, logf
	return context.WithValue(ctx, callStateKey{}, &st)
}

// debugf writes one diagnostic line through the call's Logf, if it has one.
func (st *callState) debugf(format string, args ...any) {
	if st.logf != nil {
		st.logf(format, args...)
	}
}
