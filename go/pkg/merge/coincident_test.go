package merge

// Prove-It for [DropCoincidentPoints] — the rule (2026-08-24): network
// geometry carries no time, so a point on top of its predecessor is
// meaningless and is thrown away.

import (
	"testing"
)

func TestDropCoincidentPoints(t *testing.T) {
	p := func(lat, lon float64) Point {
		return Point{Lat: lat, Lon: lon}
	}
	ele := func(v float64) *float64 { return &v }

	t.Run("a run collapses to its first point", func(t *testing.T) {
		in := []Point{p(1, 1), p(2, 2), p(2, 2), p(2, 2), p(3, 3)}
		got := DropCoincidentPoints(in)
		want := []Point{p(1, 1), p(2, 2), p(3, 3)}
		if len(got) != len(want) {
			t.Fatalf("got %d point(s), want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("point %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("the FIRST elevation of a run survives", func(t *testing.T) {
		in := []Point{
			{Lat: 2, Lon: 2, Ele: ele(100)},
			{Lat: 2, Lon: 2, Ele: ele(999)},
			{Lat: 3, Lon: 3},
		}
		got := DropCoincidentPoints(in)
		if len(got) != 2 {
			t.Fatalf("got %d point(s), want 2: %v", len(got), got)
		}
		if got[0].Ele == nil || *got[0].Ele != 100 {
			t.Errorf("kept elevation %v, want the run's first (100)", got[0].Ele)
		}
	})

	t.Run("a wholly stationary leg is left with no geometry", func(t *testing.T) {
		in := []Point{p(2, 2), p(2, 2), p(2, 2), p(2, 2)}
		if got := DropCoincidentPoints(in); len(got) != 1 {
			t.Errorf("got %d point(s), want 1 — the callers' len<2 skip then commits nothing: %v", len(got), got)
		}
	})

	t.Run("NON-consecutive repeats survive — a leg may legitimately revisit", func(t *testing.T) {
		in := []Point{p(1, 1), p(2, 2), p(1, 1)}
		if got := DropCoincidentPoints(in); len(got) != 3 {
			t.Errorf("got %d point(s), want 3 — only a repeat of the IMMEDIATE predecessor is meaningless: %v", len(got), got)
		}
	})

	t.Run("a short-but-real step is none of this function's business", func(t *testing.T) {
		// One ulp apart in longitude: not the same coordinate, so it stays.
		// The rule is exact equality, not a tolerance.
		in := []Point{p(2, 2), p(2, 2.0000000000000004)}
		if got := DropCoincidentPoints(in); len(got) != 2 {
			t.Errorf("got %d point(s), want 2 — the test is exact equality, not proximity: %v", len(got), got)
		}
	})

	t.Run("the input is not mutated", func(t *testing.T) {
		in := []Point{p(2, 2), p(2, 2), p(3, 3)}
		_ = DropCoincidentPoints(in)
		if len(in) != 3 || in[1] != p(2, 2) || in[2] != p(3, 3) {
			t.Errorf("input was mutated: %v — callers share parsed data with the folder path", in)
		}
	})
}
