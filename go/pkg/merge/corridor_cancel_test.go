package merge

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// stagedCancelCtx is a context that reports cancelled from its (cancelAfter+1)'th
// Err() call onwards, and counts every call.
//
// ⚠ WHY A REAL context.WithCancel WILL NOT DO for the solve. The census has no
// event between "bridges finished" and "the first relaxation", so a trigger
// hung off the census can only cancel BEFORE the bridges, where the bridge check
// — correctly — stops the leg first. Cancelling at a counted check is the only
// way to put the cancellation exactly at the solve's own check and then state
// what it cost. It is also what makes the measurement exact rather than
// approximate: the call count says how many checks passed between the
// cancellation and the return.
//
// Not safe for concurrent use, and it does not implement Done(): the three code
// paths under test are single-goroutine and poll Err(). Anything that selects on
// Done() (database/sql, for one) must not be given this context.
type stagedCancelCtx struct {
	context.Context
	calls       int
	cancelAfter int
}

func (c *stagedCancelCtx) Err() error {
	c.calls++
	if c.calls > c.cancelAfter {
		return context.Canceled
	}
	return nil
}

// assertCancelled is the contract every stop must satisfy at the commit caller:
// a wrapped [context.Canceled], never a partial result and never a bare
// "something went wrong".
func assertCancelled(t *testing.T, err error, wantSite string) {
	t.Helper()
	if err == nil {
		t.Fatal("the cancelled leg returned no error — a stop is an error, never a partial or empty result")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) is false at the caller, so nothing upstream can tell a cancellation from a failure: %v", err)
	}
	if !strings.Contains(err.Error(), wantSite) {
		t.Errorf("the leg stopped somewhere other than the check under test: want a %q error, got %v", wantSite, err)
	}
}

// TestCorridorCancel_BridgesStopWithinOneBridge is the per-bridge half of the
// same check, measured rather than argued: with the cancellation placed at the
// SECOND context check, the first (the per-candidate one) passes and the leg
// must stop at the next one — before [candidateBridgeCost] prices a single
// bridge.
//
// It uses a small fixture and a direct call because the quantity being bounded
// is bridges, and this fixture has exactly one: if the per-bridge check were
// removed, the call would return that bridge and no error, which is the
// red-proof.
func TestCorridorCancel_BridgesStopWithinOneBridge(t *testing.T) {
	const R = CorridorRadiusM
	sa, cands, ivs := bridgeLoopFixture(t, 400, 420, 12)

	// What an uncancelled run costs in checks: one per candidate, one per
	// priced bridge, and the checks INSIDE pricing that bridge (the
	// envelope's minimum-cost integral checks once per piece row and every
	// envelopeCancelStride sub-intervals, so a cancel stops within one gap
	// integral rather than after it).
	count := &stagedCancelCtx{Context: t.Context(), cancelAfter: math.MaxInt}
	bridges, err := buildCorridorBridges(count, sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("buildCorridorBridges: %v", err)
	}
	if len(bridges) != 1 {
		t.Fatalf("fixture sanity: want exactly 1 priced bridge, got %d — the bound being measured is per bridge", len(bridges))
	}
	// Errorf, not Fatalf: when a check has been removed this is the first thing
	// that notices, and the subtests below say WHICH one is gone — so they must
	// still run.
	if count.calls < 3 {
		t.Errorf("fixture sanity: want at least 3 context checks (1 candidate + 1 bridge + the pricing's own), got %d", count.calls)
	}

	t.Run("at the candidate check", func(t *testing.T) {
		sc := &stagedCancelCtx{Context: t.Context(), cancelAfter: 0}
		got, err := buildCorridorBridges(sc, sa, ivs, cands, R, emissionK)
		// "candidate 0:" and not "candidate 0 ivs" — the per-candidate message
		// ends at the candidate, the per-bridge one names the interval pair. The
		// two must be told apart, or removing the per-candidate check would leave
		// this subtest passing on the per-bridge check's error.
		assertCancelled(t, err, "bridges cancelled at candidate 0:")
		if got != nil {
			t.Errorf("a cancelled bridge build returned %d bridge(s) — a stop is an error, never a partial result", len(got))
		}
		if sc.calls != 1 {
			t.Errorf("the build made %d checks, want 1 — it did work after the cancellation", sc.calls)
		}
	})

	t.Run("at the first bridge", func(t *testing.T) {
		sc := &stagedCancelCtx{Context: t.Context(), cancelAfter: 1}
		got, err := buildCorridorBridges(sc, sa, ivs, cands, R, emissionK)
		// The per-bridge check's own message, in full. The pricing's checks
		// fail with "bridge candidate 0 ivs 0->1: … gap integral cancelled",
		// which also names the pair, so matching only "ivs 0->1" would pass
		// with the per-bridge check removed (the next check, the pricing's
		// first, would stop it at the same count).
		assertCancelled(t, err, "bridges cancelled at candidate 0 ivs 0->1")
		if got != nil {
			t.Errorf("a cancelled bridge build returned %d bridge(s) — a stop is an error, never a partial result", len(got))
		}
		if sc.calls != 2 {
			t.Errorf("the build made %d checks, want 2 — it priced a bridge after the cancellation", sc.calls)
		}
	})
}
