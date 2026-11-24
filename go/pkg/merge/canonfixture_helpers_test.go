package merge

import (
	"context"
	"testing"
)

// emptyCanon is an empty canon.
func emptyCanon(t *testing.T) *MemCanon {
	t.Helper()
	c, err := newMemCanon(MemCanonSnapshot{NextNodeID: 1, NextArcID: 1})
	if err != nil {
		t.Fatalf("NewMemCanon: %v", err)
	}
	return c
}

// addTestLeg merges pts into c as an import does: it drops coincident
// points (DropCoincidentPoints, as the reference import does before every
// leg), then calls AddLeg with no elevation fill. It fails the test on an
// error.
func addTestLeg(t *testing.T, ctx context.Context, c *MemCanon, label string, pts []Point) LegResult {
	t.Helper()
	res, err := addLeg(ctx, c, DropCoincidentPoints(pts), LegOptions{FillElevation: NoFill, Label: label})
	if err != nil {
		t.Fatalf("%s: AddLeg: %v", label, err)
	}
	return res
}

// liftedStubMinRunM is the MinRun (25 m) the lifted short-stub rules declined
// under: the length a stub had to reach before the old matcher could follow
// it. No merge rule reads it; the tests that pin the lift measure their stubs
// against it.
const liftedStubMinRunM = 25.0

// committedArcs is every arc of c with its full vertex list, in id order.
func committedArcs(t *testing.T, ctx context.Context, c *MemCanon) []CanonArc {
	t.Helper()
	arcs, err := c.ArcsInRect(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("ArcsInRect: %v", err)
	}
	return arcs
}
