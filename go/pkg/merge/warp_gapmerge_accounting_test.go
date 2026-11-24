package merge

// The gap-merge branch of [warpUnresolvedLegTerminals] MINTS a node and
// points both of a leg's terminals at it. Every other way both terminals come
// back resolved means they JOINED nodes that already existed. The two are
// bit-identical in the WarpReport — same two non-nil ids — and they need
// opposite accounting, which is why the flag is a required parameter rather
// than something [adoptedTerminalCount] could work out for itself.
//
// Without the flag, a leg set with one gap merge is corrected by −3 where the
// truth is −1. This test pins the contract, because a reader looking at
// `if ringMergedByGap { return 0 }` in isolation has no way to see why.

import (
	"testing"
)

func TestAdoptedTerminalCount_AGapMergeAdoptsNothing(t *testing.T) {
	id := NodeID(7)
	other := NodeID(9)

	cases := []struct {
		name            string
		rep             WarpReport
		ringMergedByGap bool
		want            int
	}{
		{"neither end resolved", WarpReport{}, false, 0},
		{"start joined an existing node", WarpReport{StartNode: &id}, false, 1},
		{"end joined an existing node", WarpReport{EndNode: &id}, false, 1},
		{"both joined existing nodes", WarpReport{StartNode: &id, EndNode: &other}, false, 2},
		{"both joined the SAME existing node", WarpReport{StartNode: &id, EndNode: &id}, false, 2},
		// The one that was wrong. Identical to the row above it — the flag is
		// the only thing that distinguishes a node this leg just minted from
		// one it found.
		{"gap merge onto a node this leg minted", WarpReport{StartNode: &id, EndNode: &id}, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := adoptedTerminalCount(c.rep, c.ringMergedByGap); got != c.want {
				t.Errorf("adoptedTerminalCount(%+v, ringMergedByGap=%v) = %d, want %d",
					c.rep, c.ringMergedByGap, got, c.want)
			}
		})
	}
}
