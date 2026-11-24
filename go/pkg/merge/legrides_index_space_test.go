package merge

import (
	"strings"
	"testing"
)

// THE MISMATCH IS AN ERROR, NOT A SHRUG.
//
// LegRides keys are POINT INDICES. Until 2026-09-02 a leg whose point count
// had moved since the rides were resolved simply got no rides:
// [LegRides.forPiece] returned nil on `pointCount != r.PointCount`. That reads
// as a guard and behaves as a fallback — every ON piece of the leg becomes a
// CREATED copy of canon, which is the parallel-strand defect the duplicate-strand
// rule exists to remove, and the import reports success while doing it.
//
// The comparison now lives in checkRidesIndexSpace, raised once for the leg.
// These are its bars.
func TestCheckRidesIndexSpace(t *testing.T) {
	t.Run("a mismatched count is an error naming both counts", func(t *testing.T) {
		rides := &LegRides{PointCount: 41}
		err := checkRidesIndexSpace(rides, 40, `plan "Scafell Pike" (item 3)`)
		if err == nil {
			t.Fatal("checkRidesIndexSpace = nil for 40 points against rides resolved at 41 — a stale index space must fail the item, not silently disable the rides and let the leg copy canon")
		}
		// Both counts, or the error cannot be acted on: which stage moved the
		// space is the whole question, and the direction and size of the move
		// is the only evidence in hand.
		for _, want := range []string{"40", "41", "Scafell Pike", "plan", "item 3"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	t.Run("a matching count passes", func(t *testing.T) {
		if err := checkRidesIndexSpace(&LegRides{PointCount: 40}, 40, `track "n" (item 0)`); err != nil {
			t.Errorf("checkRidesIndexSpace = %v for a leg whose count never moved, want nil", err)
		}
	})

	t.Run("nil rides is not a mismatch", func(t *testing.T) {
		// A leg the matcher resolved nothing for offers no ranges, so there is
		// no index space to invalidate. Erroring here would fail every leg
		// that simply had no canon to ride.
		if err := checkRidesIndexSpace(nil, 40, `track "n" (item 0)`); err != nil {
			t.Errorf("checkRidesIndexSpace = %v for a leg with no rides at all, want nil", err)
		}
	})
}
