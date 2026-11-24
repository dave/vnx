package merge

import (
	"context"
	"errors"
	"math"
	"testing"
)

func f64(v float64) *float64 { return &v }

// TestMemCanon_Poisoning: the first error poisons a MemCanon; every later
// call returns it. A cancelled ctx poisons it too. (A database-backed canon's
// equivalent is the caller rolling the transaction back.)
func TestMemCanon_Poisoning(t *testing.T) {
	ctx := context.Background()
	for name, poison := range map[string]func(c *MemCanon) error{
		"a refused write": func(c *MemCanon) error { _, err := c.AddNode(ctx, math.NaN(), 0, nil); return err },
		"a missing node":  func(c *MemCanon) error { _, _, err := c.Node(ctx, 42); return err },
		"a cancelled ctx": func(c *MemCanon) error {
			cctx, cancel := context.WithCancel(ctx)
			cancel()
			_, err := c.NodesInBox(cctx, Bounds{})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled ctx gave %v, want context.Canceled", err)
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := NewMemCanon(MemCanonSnapshot{NextNodeID: 1, NextArcID: 1})
			if err != nil {
				t.Fatal(err)
			}
			first := poison(c)
			if first == nil || c.Err() == nil {
				t.Fatalf("no poison: %v", first)
			}
			calls := map[string]error{}
			_, calls["AddNode"] = c.AddNode(ctx, 1, 1, nil)
			_, calls["NodesInBox"] = c.NodesInBox(ctx, Bounds{MaxLat: 90, MaxLon: 180})
			_, _, calls["CanonArc"] = c.CanonArc(ctx, 1)
			_, calls["ArcIDsInRect"] = c.ArcIDsInRect(ctx, Bounds{})
			_, calls["ArcsBetween"] = c.ArcsBetween(ctx, 1, 2)
			_, calls["AddArc"] = c.AddArc(ctx, NewArc{MintStart: true, MintEnd: true})
			_, calls["Snapshot"] = c.Snapshot()
			_, _, calls["ArcBox"] = c.ArcBox(1)
			for m, err := range calls {
				if err == nil || err.Error() != c.Err().Error() {
					t.Errorf("%s after poisoning: %v, want %v", m, err, c.Err())
				}
			}
		})
	}
}
