package merge

import (
	"testing"
)

// TestCorridorPackedState_RoundTrips pins [CorridorPackedState] as an exact
// bijection with the states the solve actually builds: OFF, and (SpIdx, Dir)
// over the whole SpIdx range the field claims to hold.
//
// Both directions are asserted, because only one of them is enough to hide a
// defect: pack→unpack→pack catches a lossy decode, and unpack→pack→unpack
// catches two states packing to one value (which would alias two slots and
// silently corrupt the winning path, the failure the SpIdx guard in
// [solveCorridorPath] exists for).
func TestCorridorPackedState_RoundTrips(t *testing.T) {
	// OFF first: it is the value 0 and it must not decode as SpIdx 0.
	off := packCorridorState(CorridorStateID{Off: true})
	if off != CorridorPackedOFF {
		t.Fatalf("OFF packed to %#x, want %#x", off, CorridorPackedOFF)
	}
	if got := off.Unpack(); got != (CorridorStateID{Off: true}) {
		t.Fatalf("OFF round-tripped to %v, want OFF", got)
	}
	if !off.off() {
		t.Fatal("off() is false for OFF")
	}
	if on := packCorridorOn(0, CorridorFWD); on == CorridorPackedOFF || on.off() {
		t.Fatalf("SpIdx 0 FWD packed to %#x, which reads as OFF — OFF and SpIdx 0 must differ", on)
	}

	// SpIdx values that touch every field edge: the low bit next to the
	// direction bit (1), a bit above it (2), the top bit next to the ON flag
	// (1<<29), the all-ones field (max), and two alternating patterns that
	// catch a shift or mask off by one in either direction.
	// corridorMaxSolvePieces is exclusive.
	idxs := []int{0, 1, 2, 1 << 29, corridorMaxSolvePieces - 1, 0x15555555, 0x2AAAAAAA}

	seen := map[CorridorPackedState]CorridorStateID{CorridorPackedOFF: {Off: true}}
	for _, i := range idxs {
		for _, dir := range []CorridorDir{CorridorFWD, CorridorREV} {
			want := CorridorStateID{SpIdx: i, Dir: dir}
			ps := packCorridorState(want)
			if got := ps.Unpack(); got != want {
				t.Fatalf("pack→unpack: %v became %v (packed %#x)", want, got, ps)
			}
			if ps.off() {
				t.Fatalf("%v reads as OFF (packed %#x)", want, ps)
			}
			if ps.spIdx() != i {
				t.Fatalf("spIdx() of %v is %d, want %d", want, ps.spIdx(), i)
			}
			if again := packCorridorState(ps.Unpack()); again != ps {
				t.Fatalf("unpack→pack: %#x became %#x", ps, again)
			}
			if prev, dup := seen[ps]; dup {
				t.Fatalf("%v and %v both pack to %#x", prev, want, ps)
			}
			seen[ps] = want
		}
	}
}

// TestCorridorPackedState_OrderMatchesStateLess pins the ordering property the
// packing was chosen for: ascending packed value is exactly [stateLess]'s
// order (OFF first, then ascending SpIdx, then FWD before REV). The solve's
// final-layer tiebreak reads stateLess on the decoded state; this test is what
// lets a later reader compare packed values instead.
func TestCorridorPackedState_OrderMatchesStateLess(t *testing.T) {
	var states []CorridorStateID
	states = append(states, CorridorStateID{Off: true})
	for _, i := range []int{0, 1, 2, 7, 8, 1023, 1024, 2242965, corridorMaxSolvePieces - 1} {
		states = append(states,
			CorridorStateID{SpIdx: i, Dir: CorridorFWD},
			CorridorStateID{SpIdx: i, Dir: CorridorREV})
	}

	pairs := 0
	for _, a := range states {
		for _, b := range states {
			pa, pb := packCorridorState(a), packCorridorState(b)
			if got, want := pa < pb, stateLess(a, b); got != want {
				t.Fatalf("order disagrees for (%v, %v): packed %#x < %#x is %v, stateLess is %v", a, b, pa, pb, got, want)
			}
			if (pa == pb) != (a == b) {
				t.Fatalf("equality disagrees for (%v, %v): packed %#x vs %#x", a, b, pa, pb)
			}
			pairs++
		}
	}
	if pairs != len(states)*len(states) {
		t.Fatalf("compared %d pairs, want %d", pairs, len(states)*len(states))
	}
}

// packCorridorState packs any state, OFF included.
//
// ⚠ TEST-ONLY (moved from corridorsolve.go on 2026-09-26: no production code that
// only tests call). Production does not call it.
func packCorridorState(s CorridorStateID) CorridorPackedState {
	if s.Off {
		return CorridorPackedOFF
	}
	return packCorridorOn(s.SpIdx, s.Dir)
}
