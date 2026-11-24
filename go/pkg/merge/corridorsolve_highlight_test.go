package merge

// Same-layer switch chains are DRAWN (decided 2026-09-25).
//
// Several cross-candidate switches at ONE layer, each priced below P through a
// shared node, buy a walk across whole arcs at no import progress. The run
// reconstruction used to draw only the first and last candidate of such a
// chain, which had two effects:
//
//   - a forward chain X -> Y -> Z committed Y again as a straight duplicate
//     row, instead of referencing it (132 chains on one real-data test set);
//   - a chain that came back to its own candidate the other way round — a
//     loop at the end of a stub used as a U-turn — reached the carry-through
//     Dir check with the wrong run and aborted the whole import.
//
// Each intermediate candidate is now drawn as a zero-width run
// ([chainHopRun], [corridorRun.ChainHop]). These tests pin both shapes end to
// end, and the carry-through checks that construction now keeps unreachable.

import (
	"strings"
	"testing"
)

// The carry-through's two checks are unreachable by construction now that a
// highlight's intermediate states are drawn ([checkCarrySend],
// [checkCarryReceive]). They stay errors; these tests keep both branches of
// each from rotting into something that no longer fires.
func TestCheckCarrySend_RefusesTheWrongRun(t *testing.T) {
	pc := prepOne(t, mPt(0, 0), mPt(10, 0), mPt(20, 0)) // 3 vertices
	cands := []*PreppedCandidate{pc}
	run := func(dir CorridorDir) *corridorRun {
		return &corridorRun{State: CorridorStateID{SpIdx: 0, Dir: dir}, CandIdx: 0, CanonMeasureHi: 7}
	}
	if err := checkCarrySend(run(CorridorFWD), 0, CorridorFWD, 2, 5, cands); err != nil {
		t.Errorf("a FWD run leaving by its last vertex was refused: %v", err)
	}
	if err := checkCarrySend(run(CorridorREV), 0, CorridorREV, 0, 5, cands); err != nil {
		t.Errorf("a REV run leaving by its first vertex was refused: %v", err)
	}
	if err := checkCarrySend(run(CorridorFWD), 0, CorridorREV, 0, 5, cands); err == nil || !strings.Contains(err.Error(), "internal inconsistency") {
		t.Errorf("a FWD run extended for a switch priced REV was accepted: %v", err)
	}
	if err := checkCarrySend(run(CorridorFWD), 0, CorridorFWD, 0, 5, cands); err == nil || !strings.Contains(err.Error(), "internal inconsistency") {
		t.Errorf("a FWD run extended to its FIRST vertex was accepted: %v", err)
	}
}

func TestCheckCarryReceive_RefusesTheWrongRun(t *testing.T) {
	pc := prepOne(t, mPt(0, 0), mPt(10, 0), mPt(20, 0))
	cands := []*PreppedCandidate{pc}
	if err := checkCarryReceive(CorridorFWD, 3, 0, cands, CorridorFWD, 0); err != nil {
		t.Errorf("a FWD run starting at its first vertex was refused: %v", err)
	}
	if err := checkCarryReceive(CorridorREV, 3, 0, cands, CorridorREV, 2); err != nil {
		t.Errorf("a REV run starting at its last vertex was refused: %v", err)
	}
	if err := checkCarryReceive(CorridorREV, 3, 0, cands, CorridorFWD, 0); err == nil || !strings.Contains(err.Error(), "internal inconsistency") {
		t.Errorf("a REV run receiving a carry priced FWD was accepted: %v", err)
	}
	if err := checkCarryReceive(CorridorFWD, 3, 0, cands, CorridorFWD, 2); err == nil || !strings.Contains(err.Error(), "internal inconsistency") {
		t.Errorf("a FWD run starting at its LAST vertex was accepted: %v", err)
	}
}

// TestDropEmptyChainHops: a chain-hop run with no canon extent is dropped,
// one with canon extent is kept, and an ordinary run is never touched here
// whatever its extent.
func TestDropEmptyChainHops(t *testing.T) {
	on := func(cand int, lo, hi float64, hop bool) corridorRun {
		return corridorRun{State: CorridorStateID{SpIdx: cand}, CandIdx: cand, ImportMeasureLo: 5, ImportMeasureHi: 5, CanonMeasureLo: lo, CanonMeasureHi: hi, ChainHop: hop}
	}
	got := dropEmptyChainHops([]corridorRun{
		on(0, 3, 3, true), // a hop the chain paid nothing across
		on(1, 0, 4, true), // a hop with canon extent
		on(2, 7, 7, false),
	})
	if len(got) != 2 || got[0].CandIdx != 1 || got[1].CandIdx != 2 {
		t.Fatalf("got %+v, want the extentful hop (candidate 1) and the ordinary run (candidate 2)", got)
	}
}

// TestMergeCorridorRuns_AHopKeepsItsFlagThroughAMerge: a highlight hop followed by
// a sub-eps ordinary run of the same candidate that continues it merges into
// one run, and that run must still be a chain hop. It is still sub-eps wide,
// and the wire layer drops a sub-eps run unless it is one
// ([corridorFollowRuns]) — so losing the flag would drop the canon the chain
// paid for, without a word.
//
// Reachable in a real solve when the chain ends on a second piece of the
// candidate its last hop drew (a same-candidate hand-off) and the next layer
// is a sub-eps sliver away.
func TestMergeCorridorRuns_AHopKeepsItsFlagThroughAMerge(t *testing.T) {
	const x = 50.0
	got := mergeCorridorRuns([]corridorRun{
		{State: CorridorStateID{SpIdx: 1}, CandIdx: 0, ImportMeasureLo: x, ImportMeasureHi: x, CanonMeasureLo: 0, CanonMeasureHi: 5, ChainHop: true},
		{State: CorridorStateID{SpIdx: 2}, CandIdx: 0, ImportMeasureLo: x, ImportMeasureHi: x + 1e-9, CanonMeasureLo: 5, CanonMeasureHi: 5.000001},
	})
	if len(got) != 1 {
		t.Fatalf("got %d runs, want the hop and its continuation merged into 1: %+v", len(got), got)
	}
	if !got[0].ChainHop {
		t.Errorf("the merged run lost ChainHop — the wire layer will drop it as a sub-eps sliver: %+v", got[0])
	}
	if got[0].CanonMeasureLo != 0 || got[0].CanonMeasureHi != 5.000001 {
		t.Errorf("merged canon [%v,%v], want [0,5.000001]", got[0].CanonMeasureLo, got[0].CanonMeasureHi)
	}
}

// TestMergeAdjacentSameCandidateRuns_AReversedHopIsNotAnArtefact: a REVERSED
// chain hop lies within one source edge (it has no source extent at all), which
// is the shape [mergeAdjacentSameCandidateRuns] reads as a projection artefact
// with no direction. After a same-candidate forward run it would then be
// merged and its canon clamped away. It must stay its own run with its canon
// intact.
//
// Not reached by a real solve that we know of: it needs a same-candidate
// turnaround switch INTO the hop, which costs P and so never beats the flat-P
// fan-out except on an exact tie. Built here from the merge's own input, so
// the rule holds whether or not a tie ever produces it.
func TestMergeAdjacentSameCandidateRuns_AReversedHopIsNotAnArtefact(t *testing.T) {
	fwd := followRun{Kind: runOn, CanonIdx: 0, Start: sourcePos{Edge: 0}, End: sourcePos{Edge: 3, Frac: 1}, MeasureStart: 0, MeasureEnd: 10}
	hop := followRun{Kind: runOn, CanonIdx: 0, Start: sourcePos{Edge: 3, Frac: 1}, End: sourcePos{Edge: 3, Frac: 1}, MeasureStart: 10, MeasureEnd: 2, Reversed: true, ChainHop: true}
	got := mergeAdjacentSameCandidateRuns([]followRun{fwd, hop})
	if len(got) != 2 {
		t.Fatalf("got %d runs, want the forward run and the reversed hop kept apart: %+v", len(got), got)
	}
	if got[1].MeasureStart != 10 || got[1].MeasureEnd != 2 {
		t.Errorf("the hop's canon is [%v,%v], want [10,2] — the paid walk back down the candidate was clamped away", got[1].MeasureStart, got[1].MeasureEnd)
	}
}
