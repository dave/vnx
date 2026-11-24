package merge

// The paid teleport must be DRAWN.
//
// [crossCandidateSwitchCost] charges a cross-candidate switch the canon-graph
// distance foot_A -> V -> foot_B through a shared node V. That is a purchase,
// and the runs either side must contain what it bought: the drawn stretch on A
// runs up to V, and the stretch on B resumes from V. Without it the cost
// function prices geometry [assembleFollowedLegRunsCorridor] never commits,
// and the boundary lands interior to an arc where the align step has to CUT rather than
// take DECLINE 1 at the node.
//
// The carry-through is the rule case 2 already states for a same-candidate
// switch — "a continuation carrying the skipped canon, not a seam" — applied
// to the cross-candidate edge. It is NOT a snap: a run that was moving AWAY
// from V is at a genuine turnaround and is left alone.

import (
	"math"
	"testing"
)

// TestCarryThrough_PaidTeleportLandsOnTheArcEnd is the corner-cut fixture: the
// source runs straight past a junction the two canon arcs meet 3 m north of,
// so the walker's feet are a fraction of a metre short of V on each side and
// the switch is priced at that, not at P. The committed followRuns must end
// and begin exactly ON the arc ends.
func TestCarryThrough_PaidTeleportLandsOnTheArcEnd(t *testing.T) {
	// ⚠ THE ARCS MUST MEET AT AN ANGLE, AND THE SOURCE MUST CUT THE CORNER.
	// Two COLLINEAR arcs make this fixture vacuous: past V the near-B clamp
	// regime already pins A's foot to V exactly, so the boundary lands on the
	// arc end with or without the carry-through and the test cannot go red.
	// Here V sits 6 m north of a source that runs straight, so at the cheapest
	// switch layer one foot is at V and the other is about 0.7 m from it, and
	// only the carry-through closes that.
	v := mPt(100, 6)
	canon := []CanonArc{
		mCanon(1, mPt(0, 0), v),
		mCanon(2, v, mPt(200, 0)),
	}
	pts := straightLegX(41, 200, 0)
	runs, err := corridorFollowRuns(t.Context(), pts, canon, defaultFollowBands())
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}

	prepped := prepCandidates(canon)
	var onA, onB *followRun
	for i := range runs {
		if runs[i].Kind != runOn {
			continue
		}
		switch runs[i].CanonIdx {
		case 0:
			onA = &runs[i]
		case 1:
			onB = &runs[i]
		}
	}
	if onA == nil || onB == nil {
		t.Fatalf("the leg did not ride both arcs (A=%v B=%v), so the hand-off this test is about "+
			"never happened: %+v", onA != nil, onB != nil, runs)
	}
	// ⚠ EXACT, NOT WITHIN A TOLERANCE. The whole point is that the boundary is
	// the arc's own end measure, written by [crossCandidateSharedPair] as the
	// same float the candidate's measure table holds — so the align step reads it as
	// DECLINE 1 (at node) rather than cutting a few centimetres inside.
	if onA.MeasureEnd != prepped[0].Total {
		t.Errorf("arc A's run ends at canon measure %.17g, want exactly %.17g (its own end). "+
			"The switch PAID to teleport through the shared node, so the run must be drawn to it — "+
			"a boundary %.6g m short is one the align step has to cut an arc for",
			onA.MeasureEnd, prepped[0].Total, prepped[0].Total-onA.MeasureEnd)
	}
	if onB.MeasureStart != 0 {
		t.Errorf("arc B's run starts at canon measure %.17g, want exactly 0 (its own start) — "+
			"same purchase, other side of the node", onB.MeasureStart)
	}
}

// TestCarryThrough_EndIsExtremeIsStructural pins [carryEndIsExtreme], which is
// the whole of the direction rule now that the PAIRING is direction-consistent.
// The declared Dir is the authority on both sides: the run being extended must
// be paired with the end its own direction walks toward, and a run being
// started must be paired with the end it walks away from — the same predicate
// with the Dir inverted.
//
// ⚠ IT PINS AN INDEX, NOT A MEASURE, AND THAT IS THE POINT. The float form
// (carryReaches, deleted 2026-09-21) compared a run's canon end against the
// arc's own extreme end measure, which our code wrote; the only answers it
// could give that were not "yes" came from a measure interpolation had pushed
// nanometres outside the arc, and both carry sites abort the whole import
// transaction on a "no". The index the pairing chose answers the same question
// exactly.
//
// ⚠ IT REPLACED A DERIVED-DIRECTION TEST BEFORE THAT. carryContinues read the
// run's own measure pair to guess a direction and had to invent an answer for a
// zero-width run that has none. Once the pairing names the ends by Dir, the
// declared Dir is all either site needs and there is no degenerate case.
func TestCarryThrough_EndIsExtremeIsStructural(t *testing.T) {
	const n = 7 // vertices; the ends are index 0 and index 6
	for _, tc := range []struct {
		name string
		dir  CorridorDir
		vi   int
		want bool
	}{
		{"forward walks toward the last vertex", CorridorFWD, n - 1, true},
		{"forward does not walk toward the first", CorridorFWD, 0, false},
		{"reverse walks toward the first vertex", CorridorREV, 0, true},
		{"reverse does not walk toward the last", CorridorREV, n - 1, false},
		{"an interior vertex is neither end", CorridorFWD, 3, false},
		{"an interior vertex is neither end, reversed", CorridorREV, 3, false},
	} {
		if got := carryEndIsExtreme(tc.dir, tc.vi, n); got != tc.want {
			t.Errorf("%s: carryEndIsExtreme(%v, %v, %v) = %v, want %v", tc.name, tc.dir, tc.vi, n, got, tc.want)
		}
	}
}

// TestCrossCandidateSharedPair_IsDirectionConsistent is the rule that closed
// the parallel-arc arbitrage.
//
// Two arcs A and B both LEAVE node V — they share their measure-0 end and run
// alongside each other. A walker heading FWD along A is walking away from V,
// so there is no continuation through V onto B and the switch must pay the
// flat P. Minimising over all four pairings instead priced it as
// BW*(mA + mB) — "back along A to V, then out along B" — which is a turnaround
// sold at less than the P a turnaround costs inside one arc (case 1), and the
// solver took the arbitrage by hopping to the parallel copy.
func TestCrossCandidateSharedPair_IsDirectionConsistent(t *testing.T) {
	v := mPt(0, 0)
	pcA := prepOne(t, v, mPt(200, 0))
	pcB := prepOne(t, v, mPt(200, 6))
	cands := []*PreppedCandidate{pcA, pcB}
	// Both pieces map import [0,200] onto their whole measure, so at x the
	// walker is x metres along each.
	spA := SolvePiece{CandIdx: 0, P: CorridorPiece{
		ImportMeasureAtLo: 0, ImportMeasureAtHi: 200,
		CanonMeasureAtLo: 0, CanonMeasureAtHi: pcA.Total,
	}}
	spB := SolvePiece{CandIdx: 1, P: CorridorPiece{
		ImportMeasureAtLo: 0, ImportMeasureAtHi: 200,
		CanonMeasureAtLo: 0, CanonMeasureAtHi: pcB.Total,
	}}
	tun := CorridorTunables{P: stateChangeCostP, BacktrackWeight: backtrackWeightPerM}

	// FWD -> FWD, TEN metres along: A's forward end is its FAR end, B's
	// away-end is its NEAR end. Those are different coordinates, so no pairing
	// exists and the switch pays the flat P.
	//
	// ⚠ THE 10 m IS WHERE THE ARBITRAGE BITES, AND THAT IS WHY IT IS NOT 60.
	// The direction-blind pairing prices this as BW*(mA + mB), and the cap
	// hides that above P / BW = 50 m of graph distance — at 60 m along each arc
	// the old rule already returned P and the price assertion could not tell
	// the two rules apart. Near the node it can: at 10 m along each the old
	// rule sells the turnaround for about 20 against P = 50. Short stubs are
	// exactly the population that regressed (the 3.30 m zero-pip cluster).
	if sh := crossCandidateSharedPair(&spA, &spB, CorridorFWD, CorridorFWD, cands, 10); sh.OK {
		t.Errorf("two arcs LEAVING one node resolved a pairing (EA=%v EB=%v dist=%v) — a walker "+
			"heading away from V has no continuation through it, and pricing one sells a turnaround "+
			"for less than the P a turnaround costs inside one arc", sh.EA, sh.EB, sh.Dist)
	}
	if got := crossCandidateSwitchCost(&spA, &spB, CorridorFWD, CorridorFWD, cands, 10, tun); got != stateChangeCostP {
		t.Errorf("price %v, want exactly P=%v — turning round onto a parallel copy must cost what "+
			"turning round in place costs, or the solver takes the copy", got, stateChangeCostP)
	}

	// REV -> FWD is the continuation that DOES exist: the walker comes back
	// down A toward V and leaves along B. Both ends are V, and at 60 m along
	// each the graph distance is 60 + 60.
	sh := crossCandidateSharedPair(&spA, &spB, CorridorREV, CorridorFWD, cands, 60)
	if !sh.OK {
		t.Fatalf("REV->FWD must resolve: the walker reaches V along A and leaves along B")
	}
	if sh.EA != 0 || sh.EB != 0 {
		t.Errorf("pairing (EA=%v, EB=%v), want (0, 0) — both arcs meet at their measure-0 end", sh.EA, sh.EB)
	}
	// The INDICES are what the carry-through's direction test reads, so they are
	// pinned with the measures they explain: both ends are the first vertex.
	if sh.IA != 0 || sh.IB != 0 {
		t.Errorf("pairing indices (IA=%v, IB=%v), want (0, 0) — the measure-0 end is vertex 0 on each arc", sh.IA, sh.IB)
	}
	if !carryEndIsExtreme(CorridorREV, sh.IA, len(pcA.Verts)) {
		t.Errorf("IA=%v is not the end a REV walker on A heads toward, so the carry-through would "+
			"refuse the pairing this switch just paid for", sh.IA)
	}
	if !carryEndIsExtreme(!CorridorFWD, sh.IB, len(pcB.Verts)) {
		t.Errorf("IB=%v is not the end a FWD walker on B starts from, so the carry-through would "+
			"refuse the pairing this switch just paid for", sh.IB)
	}
	// Recomputed from the pieces, not written as 120: mPt places points by
	// degrees, so a 200 m fixture's arc measures a little under 200 and the two
	// arcs differ (B is offset 6 m north at its far end).
	want := math.Abs(canonMeasureAtPiece(spA.P, 60)-0) + math.Abs(canonMeasureAtPiece(spB.P, 60)-0)
	if math.Abs(sh.Dist-want) > 1e-9 {
		t.Errorf("graph distance %v, want %v", sh.Dist, want)
	}
}

// TestCarryThrough_TheGateIsTheCapNotTheSharedNode is the limit on the other
// side. The carry-through fires only where the switch actually PAID for a
// teleport — strictly below P. Two candidates can share a node and still be
// priced at the flat P, when the walker's feet are so far from it that the
// canon-graph distance exceeds P / BacktrackWeight; that switch bought
// nothing and owes no geometry.
//
// Tested on [crossCandidateSharedPair] and [crossCandidateSwitchCost], which
// are the same two calls the run reconstruction makes — it reads the identical
// predicate rather than a second copy of it, which is why there is one
// definition of the shared pair at all.
func TestCarryThrough_TheGateIsTheCapNotTheSharedNode(t *testing.T) {
	// A ends at V, B starts at V: one continuous line through the node.
	v := mPt(100, 0)
	pcA := prepOne(t, mPt(0, 0), v)
	pcB := prepOne(t, v, mPt(200, 0))
	cands := []*PreppedCandidate{pcA, pcB}
	tun := CorridorTunables{P: stateChangeCostP, BacktrackWeight: backtrackWeightPerM}

	// The source runs [0,200] west to east under both arcs, so A's walk maps
	// import [0,100] onto A's whole measure and B's maps [100,200] onto B's.
	// A's shared end is therefore its LAST vertex (measure pcA.total) reached
	// at x=100, and B's is its FIRST (measure 0) reached at the same x — one
	// continuous line through V. [canonMeasureAtPiece] extrapolates outside a
	// piece's own range, which is what lets one x probe both.
	spA := SolvePiece{CandIdx: 0, P: CorridorPiece{
		ImportMeasureAtLo: 0, ImportMeasureAtHi: 100,
		CanonMeasureAtLo: 0, CanonMeasureAtHi: pcA.Total,
	}}
	spB := SolvePiece{CandIdx: 1, P: CorridorPiece{
		ImportMeasureAtLo: 100, ImportMeasureAtHi: 200,
		CanonMeasureAtLo: 0, CanonMeasureAtHi: pcB.Total,
	}}

	for _, tc := range []struct {
		name      string
		x         float64
		wantUnder bool
	}{
		// At x=100 both feet are AT V: distance 0, the free hand-off.
		{"feet on the node", 100, true},
		// At x=70 each foot is 30% of an arc short of V, so the canon-graph
		// distance is about 30 + 30 = 60 m, over P = 50.
		{"feet far from the node on both sides", 70, false},
	} {
		sh := crossCandidateSharedPair(&spA, &spB, CorridorFWD, CorridorFWD, cands, tc.x)
		if !sh.OK {
			t.Fatalf("%s: the two candidates share V, so the pair must resolve", tc.name)
		}
		gotUnder := backtrackWeightPerM*sh.Dist < stateChangeCostP
		if gotUnder != tc.wantUnder {
			t.Errorf("%s: canon-graph distance %.4f m gives priced-below-P = %v, want %v",
				tc.name, sh.Dist, gotUnder, tc.wantUnder)
		}
		price := crossCandidateSwitchCost(&spA, &spB, CorridorFWD, CorridorFWD, cands, tc.x, tun)
		if tc.wantUnder && !(price < stateChangeCostP) {
			t.Errorf("%s: price %v is not below P=%v", tc.name, price, stateChangeCostP)
		}
		if !tc.wantUnder && price != stateChangeCostP {
			t.Errorf("%s: price %v, want exactly P=%v — a shared node beyond the cap buys nothing, "+
				"so the run reconstruction must carry nothing through it either",
				tc.name, price, stateChangeCostP)
		}
	}
}
