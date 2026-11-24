package merge

// A bridge whose target has no import-measure extent has nowhere to land.
//
// [solveCorridorPath] creates a layer only for `lo < hi`, at both the interval
// and the piece level, so an interval made entirely of tangency pieces — a
// source edge grazing the d-band at exactly one t — contributes no layer at
// all. buildCorridorBridges used to build a bridge to such an interval anyway;
// the solve then looked its ImportMeasureLo up in layerOf, missed, and failed the
// WHOLE import with "not a breakpoint — internal inconsistency".
//
// Found by adversarial review during /prep, 2026-08-24. Loud rather than
// silent, but a hard failure on geometry that demonstrably occurs: the
// stationary-source test observes over a hundred zero-width graze pieces on
// its own straight fixture.

import (
	"testing"
)

func TestBuildCorridorBridges_SkipsIntervalsWithNoExtent(t *testing.T) {
	sa, err := newSourceMeasure(straightLegX(41, 400, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	pc := prepOne(t, straightLegX(41, 400, 0)...)

	mkPiece := func(lo, hi float64) CorridorPiece {
		return CorridorPiece{
			ImportMeasureAtLo: lo, ImportMeasureAtHi: hi,
			CanonMeasureAtLo: lo, CanonMeasureAtHi: hi,
		}
	}
	// Three intervals of ONE candidate, ascending, separated by real gaps.
	// The third has zero extent — a graze-only component.
	ivs := []corridorInterval{
		{CandIdx: 0, ImportMeasureLo: 0, ImportMeasureHi: 50, Pieces: []CorridorPiece{mkPiece(0, 50)}},
		{CandIdx: 0, ImportMeasureLo: 120, ImportMeasureHi: 170, Pieces: []CorridorPiece{mkPiece(120, 170)}},
		{CandIdx: 0, ImportMeasureLo: 300, ImportMeasureHi: 300, Pieces: []CorridorPiece{mkPiece(300, 300)}},
	}

	bridges, err := buildCorridorBridges(t.Context(), sa, ivs, []*PreppedCandidate{pc}, CorridorRadiusM, emissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}

	// Liveness: the 0->1 gap is real and must still produce a bridge, or this
	// test would pass by building no bridges at all.
	got := map[[2]int]bool{}
	for _, b := range bridges {
		got[[2]int{b.FromIv, b.ToIv}] = true
	}
	if !got[[2]int{0, 1}] {
		t.Fatalf("no bridge 0->1; the fixture's real gap is not being bridged, so this test proves nothing (bridges: %+v)", bridges)
	}

	for _, b := range bridges {
		for _, idx := range []int{b.FromIv, b.ToIv} {
			iv := ivs[idx]
			if iv.ImportMeasureHi <= iv.ImportMeasureLo {
				t.Errorf("bridge %d->%d names interval %d, which spans no import measure ([%v,%v]) — "+
					"such an interval contributes no layer, so the solve cannot resolve it and fails the whole import",
					b.FromIv, b.ToIv, idx, iv.ImportMeasureLo, iv.ImportMeasureHi)
			}
		}
	}
}
