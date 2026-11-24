package merge

// The cross-candidate switch price, case 3 of [TransitionCostT].
//
// It used to be a BINARY verdict over a continuous quantity — flat P unless
// both pieces sat exactly at their own arc endpoint at the same layer, in
// which case 0 — and that cliff is the defect three separate structural
// repairs were built against and could not close.
//
// [CrossCandidateSwitchCost] prices it continuously instead: the canon-GRAPH
// distance from A's foot to B's foot through the shared node, charged at
// BacktrackWeight and capped at P. These tests pin the two properties the
// cliff could not have — the price is the graph distance, and it is a pure
// function of (piece, piece, x) so candidate order cannot move it.

import (
	"math"
	"testing"
)

// crossCandidatePriceFixture is a source running due east with two candidates
// that MEET at a shared node V part-way along: A arrives at V, B leaves it.
// yV is how far north of the source V sits — 0 puts the node ON the recorded
// line, positive puts the source yV metres inside the corner.
type crossCandidatePriceFixture struct {
	cands []*PreppedCandidate
	sa    *SourceMeasure
	ivs   []CorridorInterval
	sps   []SolvePiece
	idxA  int
	idxB  int
}

func newCrossCandidatePriceFixture(t *testing.T, yV float64) crossCandidatePriceFixture {
	t.Helper()
	v := mPt(100, yV)
	a := prepOne(t, mPt(0, 0), v)
	b := prepOne(t, v, mPt(200, 0))
	if a.Verts[len(a.Verts)-1].Lat != b.Verts[0].Lat || a.Verts[len(a.Verts)-1].Lon != b.Verts[0].Lon {
		t.Fatalf("fixture: the shared vertex is not bitwise equal, so this test measures nothing")
	}
	sa, err := NewSourceMeasure(straightLegX(41, 200, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{a, b}
	ivs := buildIvs(t, sa, cands, CorridorRadiusM)
	return crossCandidatePriceFixture{cands: cands, sa: sa, ivs: ivs, sps: BuildSolvePieces(ivs), idxA: 0, idxB: 1}
}

// graphDistance is the price's own quantity, recomputed here from the pieces'
// and candidates' public fields WITHOUT calling the function under test: how
// far A's foot at x still is from the shared node along A, plus how far B's
// foot at x already is from it along B.
func (f crossCandidatePriceFixture) graphDistance(spA, spB SolvePiece, x float64) float64 {
	pA, pB := f.cands[spA.CandIdx], f.cands[spB.CandIdx]
	// A's shared end is its LAST vertex (measure pA.total); B's is its FIRST
	// (measure 0). That is the fixture's own construction, asserted above.
	dA := math.Abs(CanonMeasureAtPiece(spA.P, x) - pA.Total)
	dB := math.Abs(CanonMeasureAtPiece(spB.P, x) - 0)
	_ = pB
	return dA + dB
}

// TestCrossCandidateSwitchCost_IsTheCanonGraphDistance is the price identity,
// asserted at every position where a piece of A and a piece of B overlap in
// import measure — not only at the cheapest one, so a formula that happened to
// agree at the minimum and diverge elsewhere would still fail.
func TestCrossCandidateSwitchCost_IsTheCanonGraphDistance(t *testing.T) {
	f := newCrossCandidatePriceFixture(t, 3)
	tun := CorridorTunables{P: StateChangeCostP, BacktrackWeight: BacktrackWeightPerM}

	best := math.Inf(1)
	samples := 0
	for i, spA := range f.sps {
		if spA.CandIdx != f.idxA {
			continue
		}
		for j, spB := range f.sps {
			if spB.CandIdx != f.idxB {
				continue
			}
			lo := math.Max(spA.P.ImportMeasureAtLo, spB.P.ImportMeasureAtLo)
			hi := math.Min(spA.P.ImportMeasureAtHi, spB.P.ImportMeasureAtHi)
			if hi <= lo {
				continue
			}
			for s := 0; s <= 20; s++ {
				x := lo + (hi-lo)*float64(s)/20
				got := TransitionCostT(
					CorridorStateID{SpIdx: i, Dir: CorridorFWD},
					CorridorStateID{SpIdx: j, Dir: CorridorFWD},
					x, f.sps, f.cands, tun)
				want := math.Min(StateChangeCostP, BacktrackWeightPerM*f.graphDistance(spA, spB, x))
				if got != want {
					t.Fatalf("price at x=%v between sp%d (cand A) and sp%d (cand B) = %v, want %v — "+
						"the cross-candidate price must be the canon-graph distance through the shared "+
						"node, charged at the backtrack weight and capped at P", x, i, j, got, want)
				}
				samples++
				if got < best {
					best = got
				}
			}
		}
	}
	// ⚠ LIVENESS. A fixture whose pieces never overlap would pass every
	// assertion above by making none.
	if samples == 0 {
		t.Fatalf("no piece of A ever overlaps a piece of B in import measure, so this test measured nothing")
	}
	if !(best < StateChangeCostP) {
		t.Fatalf("the cheapest cross-candidate switch on a fixture whose two candidates SHARE a node "+
			"costs %v, which is not below P=%v — the shared node bought nothing and the price is "+
			"still the flat cliff", best, StateChangeCostP)
	}
	t.Logf("cheapest cross-candidate switch = %.6g over %d sampled positions (P = %v)", best, samples, StateChangeCostP)
}

// TestCrossCandidateSwitchCost_ThroughTheNodeIsFree is the special case the
// deleted binary rule was built for, and the one it refused on 2.9e-7 m of
// re-derivation noise.
//
// ⚠ WHAT IT GUARDS, PRECISELY: that a hand-off at the node stays free. It is
// red against a FLAT-P cripple and it would have been GREEN under the deleted
// binary rule, which granted exactly this case — both feet sit at their own arc
// endpoints, so the clamp path fired. It does not discriminate the continuous
// price from the cliff; TestCrossCandidateSwitchCost_IsTheCanonGraphDistance
// and the candidate-order tests do that. With the node ON the recorded line the graph distance
// is zero, so the price must be zero to float noise rather than P.
func TestCrossCandidateSwitchCost_ThroughTheNodeIsFree(t *testing.T) {
	f := newCrossCandidatePriceFixture(t, 0)
	tun := CorridorTunables{P: StateChangeCostP, BacktrackWeight: BacktrackWeightPerM}

	best := math.Inf(1)
	samples := 0
	for i, spA := range f.sps {
		if spA.CandIdx != f.idxA {
			continue
		}
		for j, spB := range f.sps {
			if spB.CandIdx != f.idxB {
				continue
			}
			lo := math.Max(spA.P.ImportMeasureAtLo, spB.P.ImportMeasureAtLo)
			hi := math.Min(spA.P.ImportMeasureAtHi, spB.P.ImportMeasureAtHi)
			if hi <= lo {
				continue
			}
			for s := 0; s <= 20; s++ {
				x := lo + (hi-lo)*float64(s)/20
				c := TransitionCostT(
					CorridorStateID{SpIdx: i, Dir: CorridorFWD},
					CorridorStateID{SpIdx: j, Dir: CorridorFWD},
					x, f.sps, f.cands, tun)
				samples++
				if c < best {
					best = c
				}
			}
		}
	}
	if samples == 0 {
		t.Fatalf("no piece of A ever overlaps a piece of B, so this test measured nothing")
	}
	if !(best < 1e-6) {
		t.Fatalf("the cheapest switch where the shared node sits ON the source costs %v, want < 1e-6 — "+
			"this is the hand-off the old binary rule existed to grant, and it must now fall out of "+
			"the continuous price rather than a verdict", best)
	}
}

// TestCrossCandidateSwitchCost_SolveRidesBothWithNoOffBetween is the same
// fixture end to end. The arithmetic above is only worth anything if the
// solve actually takes the hand-off: the failure this whole change exists to
// fix was a solve that went OFF at the junction and let the leg mint its own
// parallel copy of a row it should have adopted.
//
// ⚠ IT IS A SHAPE CHECK, NOT A PRICE GUARD, AND THE RED-PROOF SAID SO. Under a
// flat-P cripple this test still PASSES: on a fixture this cheap one extra P at
// the junction is far less than the metres of going OFF, so the solve rides
// both candidates either way. What it pins is that the two runs are adjacent
// and in A-then-B order. The price guard is
// [TestCrossCandidateSwitchCost_MarginalLegRidesRatherThanGoingOff], whose
// fixture is built to make that P decisive.
func TestCrossCandidateSwitchCost_SolveRidesBothWithNoOffBetween(t *testing.T) {
	for _, yV := range []float64{0, 3} {
		f := newCrossCandidatePriceFixture(t, yV)
		res, err := SolveCorridorPath(t.Context(), f.sa.Total(), f.sa, f.ivs, f.cands, CorridorRadiusM, EmissionK)
		if err != nil {
			t.Fatalf("yV=%v: solveCorridorPath: %v", yV, err)
		}
		firstA, lastA, firstB := -1, -1, -1
		for i, r := range res.Runs {
			switch {
			case r.State.Off:
			case r.CandIdx == f.idxA:
				if firstA < 0 {
					firstA = i
				}
				lastA = i
			case r.CandIdx == f.idxB:
				if firstB < 0 {
					firstB = i
				}
			}
		}
		if firstA < 0 || firstB < 0 {
			t.Fatalf("yV=%v: the solve did not ride both candidates (A at %d, B at %d): %+v",
				yV, firstA, firstB, res.Runs)
		}
		if firstB < lastA {
			t.Fatalf("yV=%v: B is ridden before A's last run, so this is not the A-then-B hand-off "+
				"the fixture builds: %+v", yV, res.Runs)
		}
		for i := lastA + 1; i < firstB; i++ {
			if res.Runs[i].State.Off {
				t.Fatalf("yV=%v: the solve went OFF between riding A and riding B — the two candidates "+
					"share a node, so the hand-off is a %v teleport and must beat paying 2P to leave "+
					"and re-enter: %+v", yV, 0, res.Runs)
			}
		}
	}
}

// TestCrossCandidateSwitchCost_MarginalLegRidesRatherThanGoingOff is the
// real defect's own arithmetic, in miniature.
//
// At a real three-way junction the leg did not merely pay an extra P at the junction —
// it abandoned the trail entirely. Entering a candidate costs P and the
// junction cost another, and 2P was more than the whole leg was worth against
// OFF's raw metres, so all-OFF won by 20.6 and the leg minted its own
// parallel copy of a row it should have adopted.
//
// ⚠ THE OFFSET IS LOAD-BEARING AND IS NOT A TUNED CONSTANT — it is what makes
// this fixture MARGINAL, which the gentler fixtures above are not. Emission is
// (d/R)^2 per metre and OFF is charged 1 per metre, so an ON piece is never
// dearer per metre than OFF and the only thing that can lose the leg is the
// fixed P's. At 7.5 m off a 10 m corridor the two candidates cost
// 200*(7.5/10)^2 = 112.5 of emission, so:
//
//	all-OFF                      200.0
//	enter + ride both, free      162.5   <- wins
//	enter + ride both, junction P 212.5  <- loses to all-OFF
//
// A flat cross-candidate price therefore makes the whole leg go OFF here, and
// that is what this test is red-proofed against.
//
// ⚠ AGAINST A FLAT P, NOT AGAINST THE DELETED BINARY RULE. Both feet sit at
// their own arc endpoints at the hand-off layer, so the old clamp path would
// have granted the free switch here too and this fixture would have been green
// under it. What it guards is that the hand-off keeps beating all-OFF on a leg
// where the margin is 37.5 units, which is the shape of that junction's
// defect; it is not evidence that a continuous price beats a binary one.
func TestCrossCandidateSwitchCost_MarginalLegRidesRatherThanGoingOff(t *testing.T) {
	v := mPt(100, 7.5)
	a := prepOne(t, mPt(0, 7.5), v)
	b := prepOne(t, v, mPt(200, 7.5))
	sa, err := NewSourceMeasure(straightLegX(41, 200, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{a, b}
	ivs := buildIvs(t, sa, cands, CorridorRadiusM)
	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, CorridorRadiusM, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	seen := map[int]bool{}
	offM := 0.0
	for _, r := range res.Runs {
		if r.State.Off {
			offM += r.ImportMeasureHi - r.ImportMeasureLo
			continue
		}
		seen[r.CandIdx] = true
	}
	if offM > 0 {
		t.Fatalf("the leg spent %.3f m OFF: with a shared node between the two candidates the whole "+
			"leg is worth riding (162.5 against all-OFF 200), and only a flat cross-candidate price "+
			"makes it 212.5 and loses the leg. Runs: %+v (cost %v)", offM, res.Runs, res.Cost)
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("the leg rode candidates %v, want both 0 and 1 — it must hand off at the shared "+
			"node rather than staying on one arc: %+v", seen, res.Runs)
	}
}
