package merge

import (
	"math"
	"testing"
)

// TestCorridorFinalPick_CheaperWinsWhateverTheGap is the final-layer pick's
// order: cost first, exactly, and stateLess only on an exact tie. Two states
// 1e-12 apart are two different costs, and the cheaper one must win even when
// it is the one stateLess puts second — a tolerance on the comparison would
// hand the leg to the dearer state and the tiebreak would decide a question
// the costs had already answered.
func TestCorridorFinalPick_CheaperWinsWhateverTheGap(t *testing.T) {
	first := CorridorStateID{SpIdx: 0, Dir: CorridorFWD}  // stateLess puts this first
	second := CorridorStateID{SpIdx: 1, Dir: CorridorFWD} // and this second
	if !stateLess(first, second) {
		t.Fatal("fixture: stateLess must order sp0 before sp1, or this test cannot separate cost from tiebreak")
	}
	states := []CorridorPackedState{CorridorPackedOFF, packCorridorState(first), packCorridorState(second)}

	const cheap = 10.0
	dear := cheap + 1e-12
	if !(dear > cheap) {
		t.Fatal("fixture: the two costs are equal in float64, so the test measures nothing")
	}
	got, cost, found := corridorFinalPick(states, []float64{math.Inf(1), dear, cheap})
	if !found {
		t.Fatal("no state picked")
	}
	if got != second || cost != cheap {
		t.Errorf("picked %v at %v, want %v at %v — the cheaper state lost on the tiebreak", got, cost, second, cheap)
	}

	// An EXACT tie is the only thing stateLess decides.
	got, _, _ = corridorFinalPick(states, []float64{math.Inf(1), cheap, cheap})
	if got != first {
		t.Errorf("exact tie picked %v, want %v (stateLess order)", got, first)
	}
}
