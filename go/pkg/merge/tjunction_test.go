package merge

// T-junction split tests. Geometry uses the follow_test.go metre frame (mPt),
// so fixture distances are metres.
//
// The T-junction split is the first merge step that MUTATES COMMITTED GEOMETRY, so every
// decline test here asserts canon is BYTE-UNCHANGED rather than merely that the
// terminal did not move. A split performed before its guard would leave the
// terminal alone and still have deleted a committed row; only the byte check
// catches that.

import (
	"testing"
)

// DELETED 2026-08-14: this decline case ("the endpoint warp's own taper zone
// cannot absorb the displacement, so the terminal would not move even if
// canon were split") no longer exists — the endpoint warp can no longer decline a terminal
// it has resolved a target for (warp_core.go, "there is one method for moving a
// node"), so TjunctionDeclineFor's own step 5 that predicted it is deleted
// too (see its doc). The same fixture, tjLegNS(505, -8, 11), now SPLITS —
// pinned as "a short leg still splits" in TestTJunctionDeclineFor and
// TestTJunctionReport_RecordsEachTerminalsOutcome.

// The decline rules, pinned directly. Every case is a candidate the search has
// ALREADY admitted (inside the radius), so this table is only about what the
// split does with one.
func TestTJunctionDeclineFor(t *testing.T) {
	// The stub brackets below were derived from a MinRunM of 25. The rule that
	// read it is lifted, so today nothing in this table turns on the number —
	// but it is the value a revert of the lift would restore the rule to, and
	// the brackets would be wrong against any other. Kept for that reason,
	// deliberately, not left behind.
	if DefaultFollowBands().MinRunM != 25 {
		t.Fatalf("MinRun is %v, not the ratified 25 m — this table's stub brackets were derived from 25",
			DefaultFollowBands().MinRunM)
	}
	// A candidate whose three length fields agree, as merge.NearestArc
	// guarantees: Fraction is exactly PriorLenM/(PriorLenM+NextLenM).
	cand := func(prior, next, dist float64) NearbyCanonArc {
		return NearbyCanonArc{
			ID: 1, DistM: dist,
			PriorLenM: prior, NextLenM: next,
			Fraction: prior / (prior + next),
		}
	}
	for _, tc := range []struct {
		name string
		cand NearbyCanonArc
		want TjunctionDecline
	}{
		{"an interior projection on a long leg splits", cand(500, 500, 8), TjNoDecline},

		// Fraction 0 / 1: the closest approach is a walk terminal vertex.
		{"fraction exactly 0", cand(0, 1000, 8), TjDeclineTerminalVertex},
		{"fraction exactly 1", cand(1000, 0, 8), TjDeclineTerminalVertex},

		// MinRun, from either side. The stub decline is LIFTED (2026-08-30)
		// so every one of these now splits. The rows are kept
		// rather than deleted so that reverting the lift makes this test red at
		// the exact rule that came back — a deleted row lets a revert land in
		// silence, and the lift is explicitly meant to be revertible in one
		// move if play-testing says the rule was right after all.
		{"the prior half is under MinRun — rule lifted, splits", cand(24.9, 975, 8), TjNoDecline},
		{"the next half is under MinRun — rule lifted, splits", cand(975, 24.9, 8), TjNoDecline},
		{"a half of exactly MinRun splits", cand(25, 975, 8), TjNoDecline},
		{"a half far under MinRun — rule lifted, splits", cand(0.5, 999.5, 8), TjNoDecline},

		// The former "warp zone test" case (step 5, whether the endpoint warp's
		// taper zone could absorb the displacement) is DELETED — the warp can no
		// longer decline a terminal it has resolved a target for (2026-08-14), so
		// TjunctionDeclineFor no longer asks the question and doesn't take a
		// legLen any more. See TjunctionDeclineFor's own doc.
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TjunctionDeclineFor(tc.cand); got != tc.want {
				t.Fatalf("tjunctionDeclineFor = %v, want %v", got, tc.want)
			}
		})
	}
}
