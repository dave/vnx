package merge

// corridorwire_bridge_merge_test.go — the second half of the fragmentation
// fix, found investigating why a real file still made 41
// align cuts after coalescePieces
// (corridorwire.go) and the join-by-correspondence fix
// (corridorinterval.go) both landed.
//
// Measured directly against real data (probe, not this fixture): of 48
// solver runs, 18 were Bridged, and ALL 18 were immediately followed by a
// non-bridged run of the SAME (interval, direction) state — a bridge run
// and its own continuation, never merged into one FollowRun because
// CorridorFollowRuns builds a bridged run as its own standalone FollowRun
// (the `if run.Bridged` branch) with no look-ahead at what follows it. Each
// such pair therefore commits as TWO followRuns and so two seams, even
// though nothing downstream needs a boundary there: FollowRun itself has no
// Bridged field, and AssembleFollowedLegRunsCorridor draws MeasureStart->MeasureEnd
// on the candidate identically either way — a bridge's own canon measure and its
// following run's canon measure are exactly as continuous as two adjacent pieces
// of one non-bridged run, which coalescePieces already merges.

import (
	"math"
	"testing"
)

// bridgeThenContinueFixture builds a candidate running straight along y=0
// from x=0 to x=1000, and a source that hugs it (y=5) except for one short,
// deliberate excursion between approachX and returnX — the same shape
// corridorsolve_bridge_test.go's bridgeLoopFixture uses, reproduced here
// against raw points/canon (not PreppedCandidate/SourceMeasure) because this
// test drives CorridorFollowRuns, not SolveCorridorPath directly.
func bridgeThenContinueFixture(approachX, returnX, loopY float64) ([]Point, []CanonArc) {
	canon := []CanonArc{mCanon(1, mPt(0, 0), mPt(1000, 0))}
	var pts []Point
	pts = append(pts, straightLegX(9, approachX, 5)...)
	pts = append(pts, mPt(approachX, loopY))
	pts = append(pts, mPt(returnX, loopY))
	pts = append(pts, mPt(returnX, 5))
	for x := returnX + 40; x < 1000; x += 50 {
		pts = append(pts, mPt(x, 5))
	}
	pts = append(pts, mPt(1000, 5))
	return pts, canon
}

// TestCorridorFollowRuns_BridgeMergesIntoFollowingSameStateRun is the
// red-first case: a bridge run and the run immediately continuing the same
// candidate in the same direction, with canon measure touching across the
// bridge boundary, must commit as ONE FollowRun.
func TestCorridorFollowRuns_BridgeMergesIntoFollowingSameStateRun(t *testing.T) {
	const R = CorridorRadiusM
	// ⚠ RE-BASELINED 2026-09-03, FROM (400, 430, 40), and for the SAME reason
	// corridorsolve_bridge_test.go's own fixture moved: under the import-side
	// gap integral ([importGapCost]) a 40 m-deep excursion is recorded
	// geometry the solve now KEEPS rather than bridges (every source edge
	// is legitimate geometry), so that shape produces no
	// bridge and this test's own fixture-sanity check would fire. 12 m deep
	// over 20 m wide still clears R = 10 and still bridges, which is what this
	// test needs — its subject is the MERGE of a bridge run into its
	// continuation, not the depth of the excursion that produced it.
	//
	// Kept at the SAME (400, 420, 12) as corridorsolve_bridge_test.go's own
	// fixture on purpose: the two shapes were identical before and a silent
	// divergence between them would make one test's fixture sanity depend on
	// the other's unrelated constraint. See that file for why the WIDTH in
	// particular is load-bearing there (5 m bridged, but read 0.000 m on the
	// fabrication instrument).
	pts, canon := bridgeThenContinueFixture(400, 420, 12)

	// Fixture sanity: this shape must actually produce a bridge in the raw
	// solve, or this test proves nothing about merging — cross-check
	// against SolveCorridorPath directly, the same way
	// corridorsolve_bridge_test.go's own red-proofs do.
	sa, err := NewSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	prepped := PrepCandidates(canon)
	ivs, err := corridorIntervalsForLeg(t.Context(), pts, prepped, R)
	if err != nil {
		t.Fatalf("corridorIntervalsForLeg: %v", err)
	}
	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, ivs, prepped, R, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	sawBridge := false
	for _, r := range res.Runs {
		if r.Bridged {
			sawBridge = true
		}
	}
	if !sawBridge {
		t.Fatalf("fixture sanity: the raw solve produced no bridged run — this fixture no longer exercises bridging")
	}

	b := DefaultFollowBands()
	runs, err := CorridorFollowRuns(t.Context(), pts, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}

	// Two adjacent ON followRuns that share a candidate and direction, and
	// whose canon measure touches across the boundary (MeasureEnd of the first ==
	// MeasureStart of the second, within MeasureEps), represent geometry that is
	// exactly as continuous as two pieces coalescePieces would already
	// merge within one non-bridged run — one of them being a former bridge
	// changes nothing about whether a seam is needed there.
	for i := 0; i+1 < len(runs); i++ {
		a, c := runs[i], runs[i+1]
		if a.Kind != RunOn || c.Kind != RunOn {
			continue
		}
		if a.CanonIdx != c.CanonIdx || a.Reversed != c.Reversed {
			continue
		}
		if math.Abs(a.MeasureEnd-c.MeasureStart) <= MeasureEps {
			t.Fatalf("adjacent ON followRuns %d and %d share candidate %d, direction (reversed=%v), and touch at measure %.6g — "+
				"should have been merged into one followRun, not left as two: %+v then %+v",
				i, i+1, a.CanonIdx, a.Reversed, a.MeasureEnd, a, c)
		}
	}
}

// TestMergeAdjacentSameCandidateRuns_DegenerateChainStaysSeparate is the
// other boundary this file's merge must respect, found on
// TestA2_Seed6Order012ImportsCleanly (bridge-merge's own regression):
// walk-2's leg approaches ONE canon vertex repeatedly from several
// unrelated directions along its own length. Each approach is a genuinely
// separate solver run — five of them, EVERY one clamped to the exact same
// degenerate canon measure (MeasureStart==MeasureEnd==209.936562184, measured directly)
// because the source never actually rides a canon edge's interior, only
// ever the same nearest vertex. All five "touch" by the letter of the merge
// test (an equal constant trivially satisfies |MeasureEnd-MeasureStart|<=MeasureEps
// against itself, repeatedly), so mergeAdjacentSameCandidateRuns collapsed
// them into ONE FollowRun spanning the leg's own full 298-edge import-measure
// length at a ZERO-width canon span — committing a "2 point(s), begins and
// ends on the same node, no interior vertex to split at" piece,
// because assembleOnRun draws canon vertices strictly between MeasureStart and
// MeasureEnd and there are none between a position and itself.
//
// The bridge+continuation case this file exists for always has REAL canon
// movement on at least one side (the continuation walks an actual
// interval); two BOTH-degenerate runs merging is the failure mode, not the
// target one.
func TestMergeAdjacentSameCandidateRuns_DegenerateChainStaysSeparate(t *testing.T) {
	const constMeasure = 209.936562184
	runs := []FollowRun{
		{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: constMeasure, MeasureEnd: constMeasure,
			Start: SourcePos{Edge: 0, Frac: 0}, End: SourcePos{Edge: 0, Frac: 0.8371901071005368}},
		{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: constMeasure, MeasureEnd: constMeasure,
			Start: SourcePos{Edge: 0, Frac: 0.8371901071005368}, End: SourcePos{Edge: 292, Frac: 0.5645430622677673}},
		{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: constMeasure, MeasureEnd: constMeasure,
			Start: SourcePos{Edge: 292, Frac: 0.5645430622677673}, End: SourcePos{Edge: 293, Frac: 0.6250645772644844}},
		{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: constMeasure, MeasureEnd: constMeasure,
			Start: SourcePos{Edge: 293, Frac: 0.6250645772644844}, End: SourcePos{Edge: 295, Frac: 0.11446677235408986}},
		{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: constMeasure, MeasureEnd: constMeasure,
			Start: SourcePos{Edge: 295, Frac: 0.11446677235408986}, End: SourcePos{Edge: 298, Frac: 1}},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != len(runs) {
		t.Fatalf("want %d runs (a chain of mutually-degenerate re-approaches to the same vertex must not collapse into one), got %d: %+v",
			len(runs), len(got), got)
	}
}

// TestMergeAdjacentSameCandidateRuns_DegenerateRunDoesNotSplitAReverseFollow
// is the OTHER side of the degenerate question, and it is a mid-node defect
// seen in play-testing on 2026-08-21.
//
// A degenerate run — source consumed, ZERO canon measure — has no direction of its
// own, but Reversed is derived from canonHi<canonLo, so it always reads
// FORWARD. Drop one into the middle of a REVERSE follow and the direction test
// below refuses to merge on both sides: one continuous ride down a canon
// arc commits as several followRuns, and every seam between them cuts
// canon and mints a node.
//
// The runs here are the measured ones. Importing
// one real file against another
// alone (one candidate, no ambiguity) produced eight runs where one was
// right: three zero-measure stalls at canon vertices, each splitting the reverse
// ride and minting the nodes at 54.46174,-3.02663 and 54.46344,-3.03018.
//
// A stall is legitimate geometry — the leg rounds a corner canon takes as a
// sharp vertex, so the foot point stops while the source keeps moving. What is
// not legitimate is reading its absent direction as "forward".
//
// Network-wide on a real test set before the fix: 21 of 61 ON runs
// were degenerate, and 42 of 67 run boundaries were adjacent to one.
func TestMergeAdjacentSameCandidateRuns_DegenerateRunDoesNotSplitAReverseFollow(t *testing.T) {
	rev := func(a, b float64, s, e SourcePos) FollowRun {
		return FollowRun{Kind: RunOn, CanonIdx: 1, Reversed: true, MeasureStart: a, MeasureEnd: b, Start: s, End: e}
	}
	stall := func(a float64, s, e SourcePos) FollowRun {
		return FollowRun{Kind: RunOn, CanonIdx: 1, Reversed: false, MeasureStart: a, MeasureEnd: a, Start: s, End: e}
	}
	runs := []FollowRun{
		rev(12456.49, 12170.03, SourcePos{Edge: 0, Frac: 0.02516}, SourcePos{Edge: 2, Frac: 0.94372}),
		stall(12170.03, SourcePos{Edge: 2, Frac: 0.94372}, SourcePos{Edge: 2, Frac: 1}),
		stall(12170.03, SourcePos{Edge: 2, Frac: 1}, SourcePos{Edge: 3, Frac: 0.01119}),
		rev(12170.03, 11867.79, SourcePos{Edge: 3, Frac: 0.01119}, SourcePos{Edge: 6, Frac: 0.17859}),
		stall(11867.79, SourcePos{Edge: 6, Frac: 0.17859}, SourcePos{Edge: 6, Frac: 0.18106}),
		rev(11867.79, 11790.47, SourcePos{Edge: 6, Frac: 0.18106}, SourcePos{Edge: 7, Frac: 0.25469}),
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 1 {
		t.Fatalf("one continuous reverse ride down canon arc 1, stalled three times at canon vertices, "+
			"must commit as ONE followRun — got %d: %+v", len(got), got)
	}
	if got[0].MeasureStart != 12456.49 || got[0].MeasureEnd != 11790.47 {
		t.Fatalf("merged run must span the whole ride 12456.49 -> 11790.47, got %.2f -> %.2f", got[0].MeasureStart, got[0].MeasureEnd)
	}
	if !got[0].Reversed {
		t.Fatalf("merged run must stay REVERSED — the stalls have no direction of their own")
	}
	if got[0].Start != (SourcePos{Edge: 0, Frac: 0.02516}) || got[0].End != (SourcePos{Edge: 7, Frac: 0.25469}) {
		t.Fatalf("merged run must span the whole source stretch, got %+v -> %+v", got[0].Start, got[0].End)
	}
}

// TestMergeAdjacentSameCandidateRuns_ReversalInsideOneSourceEdgeIsNotATraversal
// was the last unjustified node in a real test network — and it is the same defect family as the zero-width run
// above: a run whose DIRECTION comes from the projection rather than from the
// walk.
//
// A real file rejoins canon after a 9.2 km
// excursion and commits, verbatim:
//
//	run 2  OFF                                       src edges 11..184  = 9191 m
//	run 3  ON  canon 40  measure 8629.46 -> 8609.91  REV  src edge 184 ONLY =   36 m
//	run 4  ON  canon 40  measure 8609.91 -> 14569.85 FWD  src edges 184..246= 5956 m
//
// Run 3 travels 19.55 m BACKWARDS along canon while consuming 36 m of ONE
// source edge. A source edge is a straight line between two recorded points,
// so the walker did not double back — the projected foot slid backwards as the
// source approached canon at an angle, then advanced once past it. The
// committed line still draws those 19.55 m twice, and the turnaround mints a
// node on a stretch where nothing divides.
//
// WHY THE COST MODEL DOES NOT CATCH IT: backtrackCost charges canon measure
// travelled AGAINST a run's declared direction. A run that declares itself
// REVERSED and then goes backwards is travelling WITH its direction and is
// charged nothing; it pays only the flat state-change penalty to turn round.
// That is correct for a genuine out-and-back — a spur walked up and back
// should be cheap — which is why the fix is not to make reversals expensive.
//
// THE DISTINGUISHING FACT IS IN THE SOURCE, NOT THE COST: a genuine reversal
// spans several source edges, because the walker has to travel out and then
// back. One contained in a single straight edge cannot be real. So a reversal
// has to be present in the SOURCE to count as a direction, exactly as a
// zero-width span has no direction at all.
//
// Whole population, measured on a real test set 2026-08-21: 3 runs,
// 21.48 m of canon drawn twice (19.55 m, 1.93 m, and one at 0.00 m which the
// zero-width rule already handles).
func TestMergeAdjacentSameCandidateRuns_ReversalInsideOneSourceEdgeIsNotATraversal(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 3, Reversed: true,
			MeasureStart: 8629.46, MeasureEnd: 8609.91,
			Start: SourcePos{Edge: 184, Frac: 0.10}, End: SourcePos{Edge: 184, Frac: 0.62},
		},
		{
			Kind: RunOn, CanonIdx: 3, Reversed: false,
			MeasureStart: 8609.91, MeasureEnd: 14569.85,
			Start: SourcePos{Edge: 184, Frac: 0.62}, End: SourcePos{Edge: 246, Frac: 1},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 1 {
		t.Fatalf("a reversal contained in ONE source edge is a projection artefact, not a traversal — "+
			"it must fold into the run that continues, leaving 1 followRun; got %d: %+v", len(got), got)
	}
	if got[0].Reversed {
		t.Fatalf("the merged run must take the direction of the side that actually travelled: got Reversed=true")
	}
	if got[0].MeasureStart != 8629.46 || got[0].MeasureEnd != 14569.85 {
		t.Fatalf("merged run must run forward from where the leg REJOINED canon (8629.46) to where it left "+
			"(14569.85), never dipping to 8609.91 — that dip is the 19.55 m drawn twice and the node it mints; "+
			"got %.2f -> %.2f", got[0].MeasureStart, got[0].MeasureEnd)
	}
}

// TestMergeAdjacentSameCandidateRuns_GenuineOutAndBackSurvives is the other
// side of the rule above, and it is the one that keeps it honest. A spur
// walked up and back down IS a real reversal: the walker travels out over
// several source edges and returns over several more. That must stay two runs
// with a real turnaround between them, or the committed line would lose the
// spur entirely.
func TestMergeAdjacentSameCandidateRuns_GenuineOutAndBackSurvives(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 3, Reversed: false,
			MeasureStart: 1000, MeasureEnd: 1400,
			Start: SourcePos{Edge: 10, Frac: 0}, End: SourcePos{Edge: 24, Frac: 0.5},
		},
		{
			Kind: RunOn, CanonIdx: 3, Reversed: true,
			MeasureStart: 1400, MeasureEnd: 1000,
			Start: SourcePos{Edge: 24, Frac: 0.5}, End: SourcePos{Edge: 39, Frac: 1},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 2 {
		t.Fatalf("a genuine out-and-back spans many source edges in BOTH directions and must stay two runs — "+
			"merging it would delete the return leg from the committed line; got %d: %+v", len(got), got)
	}
}

// TestMergeAdjacentSameCandidateRuns_ArtefactNotFoldedWhenTheStrideCannotReachIt
// is the boundary the single-source-edge rule above has to respect, and it was
// found by shipping the rule without it: synthetic-trail seeds 76, 114, 150
// and 109 all turned into HARD IMPORT FAILURES —
// "assembleOnRun: arc 1 forward measures not ascending: [1174.92 1174.84]".
//
// Folding a directionless run into its neighbour keeps the run's OWN MeasureStart,
// because that is where the leg rejoined canon. When the continuation then
// travels far enough to pass that point, the merged run is well-formed and the
// artefact disappears — that is the 9.2 km case above. When it does NOT, keeping
// MeasureStart produces a run labelled forward whose measures descend, and worse, the
// directionless run is covering canon the continuation never reaches, so
// folding it away would delete ground the leg actually rode.
//
// So the merge only fires when the result is monotone in its own direction.
// Here the continuation ends at 42.52, BELOW the rejoin at 52.13, so both runs
// stay — seam and node included. That is the honest answer, not a workaround.
func TestMergeAdjacentSameCandidateRuns_ArtefactNotFoldedWhenTheStrideCannotReachIt(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 2, Reversed: true,
			MeasureStart: 52.13, MeasureEnd: 40.00,
			Start: SourcePos{Edge: 71, Frac: 0.08}, End: SourcePos{Edge: 71, Frac: 0.61},
		},
		{
			Kind: RunOn, CanonIdx: 2, Reversed: false,
			MeasureStart: 40.00, MeasureEnd: 42.52,
			Start: SourcePos{Edge: 71, Frac: 0.61}, End: SourcePos{Edge: 72, Frac: 0.87},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 2 {
		t.Fatalf("the continuation ends BELOW the rejoin, so folding would both mislabel the direction and "+
			"delete canon the leg rode — both runs must stay; got %d: %+v", len(got), got)
	}
	for i, r := range got {
		if !r.Reversed && r.MeasureEnd < r.MeasureStart {
			t.Fatalf("run %d is labelled forward but its measures descend (%.2f -> %.2f) — assembleOnRun rejects this "+
				"and the user's whole import fails", i, r.MeasureStart, r.MeasureEnd)
		}
	}
}

// TestMergeAdjacentSameCandidateRuns_BridgedContinuationInsideOneSparseEdge is
// the case the OLD "refuse when both inputs are degenerate" spelling refused by
// accident, and it was the last unjustified node in a real test network
// (diagnosed 2026-09-03).
//
// A real file rejoins another's canon and commits, verbatim
// (measured on the two files alone, one candidate, no ambiguity):
//
//	run 0  ON canon 1 measure 12448.075933 -> 12312.448784 REV src e0 f0.000000 -> e0 f0.883785
//	run 1  ON canon 1 measure 12312.448784 -> 12298.453532 REV src e0 f0.883785 -> e0 f0.931003  (BRIDGE)
//	run 2  ON canon 1 measure 12298.453532 -> 11982.114701 REV src e0 f0.931003 -> e4 f0.568454
//
// Source edge 0 is 157.9 m long — one sparse GPX edge covering 135.6 m of canon
// — so runs 0 and 1 BOTH satisfy [noDirection]'s second clause (reversed,
// Start.edge == End.edge), even though between them they travel 149.6 m of
// canon. "Both degenerate" therefore refused the merge, the seam survived, and
// it minted a degree-2 node at 54.460789,-3.025749.
//
// The refusal the old rule was written for is about the MERGED span having no
// canon extent. This one has 149.6 m of it. All three runs must fold into one.
func TestMergeAdjacentSameCandidateRuns_BridgedContinuationInsideOneSparseEdge(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 0, Reversed: true,
			MeasureStart: 12448.075933, MeasureEnd: 12312.448784,
			Start: SourcePos{Edge: 0, Frac: 0}, End: SourcePos{Edge: 0, Frac: 0.883785},
		},
		{
			Kind: RunOn, CanonIdx: 0, Reversed: true,
			MeasureStart: 12312.448784, MeasureEnd: 12298.453532,
			Start: SourcePos{Edge: 0, Frac: 0.883785}, End: SourcePos{Edge: 0, Frac: 0.931003},
		},
		{
			Kind: RunOn, CanonIdx: 0, Reversed: true,
			MeasureStart: 12298.453532, MeasureEnd: 11982.114701,
			Start: SourcePos{Edge: 0, Frac: 0.931003}, End: SourcePos{Edge: 4, Frac: 0.568454},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 1 {
		t.Fatalf("one continuous reverse ride down canon arc 1, whose first 149.6 m happen to be covered by a "+
			"single sparse source edge, must commit as ONE followRun — got %d: %+v", len(got), got)
	}
	if got[0].MeasureStart != 12448.075933 || got[0].MeasureEnd != 11982.114701 {
		t.Fatalf("merged run must span the whole ride 12448.075933 -> 11982.114701, got %.6f -> %.6f",
			got[0].MeasureStart, got[0].MeasureEnd)
	}
	if !got[0].Reversed {
		t.Fatalf("merged run must stay REVERSED")
	}
}

// TestMergeAdjacentSameCandidateRuns_ZeroWidthRunsAtDifferentMeasuresMerge is
// the other half of the equivalence question the rule above raises, and the one
// place the merged-span test is deliberately LOOSER than "both inputs are
// zero-width".
//
// Two zero-width runs clamped to the SAME canon vertex merge to a zero-extent
// span and are refused (the test above this one). Two clamped to DIFFERENT
// vertices, IN ASCENDING ORDER, do not: the merged run runs from one to the
// other, so it has real canon extent and two DISTINCT terminal anchors, and
// assembleOnRun's same-node complaint — "begins and ends on the same node, no
// interior vertex to split at" — cannot arise. Refusing it would leave a seam,
// and a node, on a stretch where nothing divides.
//
// ASCENDING ONLY, and that is not a detail of this fixture. A zero-width run
// reads Reversed=false (canonHi<canonLo is false for a constant), so with both
// sides degenerate strideReversed is FORWARD and `continues` demands
// r.MeasureStart >= last.MeasureEnd. The pair here (100 then 140) merges; the
// same two stalls the other way round (140 then 100) would step backwards
// against a forward stride and stay two runs. The merged-span rule loosened the
// SAME-vertex case only; it did not make descending stalls mergeable.
//
// No such pair occurs in any real data measured (counted 2026-09-03: zero
// everywhere), so this test is the only place the
// difference is exercised. It is here so the difference is a decision on the
// record rather than an accident.
func TestMergeAdjacentSameCandidateRuns_ZeroWidthRunsAtDifferentMeasuresMerge(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 1, Reversed: false,
			MeasureStart: 100, MeasureEnd: 100,
			Start: SourcePos{Edge: 4, Frac: 0}, End: SourcePos{Edge: 4, Frac: 0.5},
		},
		{
			Kind: RunOn, CanonIdx: 1, Reversed: false,
			MeasureStart: 140, MeasureEnd: 140,
			Start: SourcePos{Edge: 4, Frac: 0.5}, End: SourcePos{Edge: 7, Frac: 0.25},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 1 {
		t.Fatalf("two stalls at DIFFERENT canon vertices merge to a 40 m span with two distinct anchors — "+
			"that is well-formed for assembleOnRun and must be ONE run; got %d: %+v", len(got), got)
	}
	if got[0].MeasureStart != 100 || got[0].MeasureEnd != 140 {
		t.Fatalf("merged run must span 100 -> 140, got %.2f -> %.2f", got[0].MeasureStart, got[0].MeasureEnd)
	}
}

// TestMergeAdjacentSameCandidateRuns_OneDegenerateSideLandingOnItsOwnStartStaysSeparate
// is the one behaviour the fix newly INTRODUCED, as opposed to the ones it
// stopped refusing: a pair with only ONE degenerate side, whose merge would
// land back on its own start.
//
// "Refuse when BOTH inputs are degenerate" let this through — only the first
// run is degenerate — and the merge then committed a run whose start and end
// are the SAME measure. That is precisely the piece assembleOnRun rejects:
// canon vertices are drawn strictly BETWEEN MeasureStart and MeasureEnd, and
// there are none between a position and itself — "2 point(s), begins and ends
// on the same node, no interior vertex to split at", a hard import
// failure. Measured on real data 2026-09-03: four such merges were being
// allowed through, across three areas.
//
// The shape: a reversed run contained in ONE source edge (so [noDirection]'s
// second clause flags it) walking canon 50 -> 40, followed by a forward run
// spanning several source edges that walks 40 -> 50 and stops exactly where the
// first one began. Folding the first away keeps its MeasureStart of 50, the
// stride is forward, the clamp leaves newEnd at 50 — start == end, zero extent.
//
// Before the merged-span rule this returned ONE run, Reversed=false,
// MeasureStart=50, MeasureEnd=50. Now the two stay separate: the seam survives,
// and with it its node, which is the honest outcome for geometry that came back
// to where it started.
func TestMergeAdjacentSameCandidateRuns_OneDegenerateSideLandingOnItsOwnStartStaysSeparate(t *testing.T) {
	runs := []FollowRun{
		{
			Kind: RunOn, CanonIdx: 2, Reversed: true,
			MeasureStart: 50, MeasureEnd: 40,
			Start: SourcePos{Edge: 71, Frac: 0.08}, End: SourcePos{Edge: 71, Frac: 0.61},
		},
		{
			Kind: RunOn, CanonIdx: 2, Reversed: false,
			MeasureStart: 40, MeasureEnd: 50,
			Start: SourcePos{Edge: 71, Frac: 0.61}, End: SourcePos{Edge: 78, Frac: 0.42},
		},
	}
	got := mergeAdjacentSameCandidateRuns(runs)
	if len(got) != 2 {
		t.Fatalf("the continuation ends exactly where the reversed single-edge run BEGAN, so merging them "+
			"commits a zero-extent run — both runs must stay; got %d: %+v", len(got), got)
	}
	for i, r := range got {
		if r.MeasureStart == r.MeasureEnd {
			t.Fatalf("run %d spans no canon at all (%.2f -> %.2f) — assembleOnRun refuses it as \"begins and ends "+
				"on the same node, no interior vertex to split at\" and the user's whole import fails",
				i, r.MeasureStart, r.MeasureEnd)
		}
	}
}
