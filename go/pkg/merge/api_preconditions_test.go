package merge

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

// TestAddLeg_PreconditionsAreClearErrors checks each stated precondition of
// AddLeg: the call fails before any step runs, with a "merge: add leg:"
// error that names what is wrong, and the canon is left untouched.
func TestAddLeg_PreconditionsAreClearErrors(t *testing.T) {
	ctx := context.Background()
	good := []Point{{Lat: 54.5, Lon: -3.0}, {Lat: 54.5, Lon: -2.999}}
	opts := LegOptions{FillElevation: NoFill}
	cases := []struct {
		name  string
		nilC  bool
		pts   []Point
		opts  LegOptions
		wants []string
	}{
		{"nil canon", true, good, opts, []string{"canon is nil"}},
		{"no points", false, nil, opts, []string{"0 point(s)", "at least 2"}},
		{"one point", false, good[:1], opts, []string{"1 point(s)", "at least 2"}},
		{"NaN latitude", false, []Point{good[0], {Lat: math.NaN(), Lon: -3}}, opts, []string{"point 1", "not a coordinate"}},
		{"infinite longitude", false, []Point{{Lat: 54, Lon: math.Inf(1)}, good[1]}, opts, []string{"point 0", "not a coordinate"}},
		{"latitude out of range", false, []Point{good[0], {Lat: 91, Lon: -3}}, opts, []string{"point 1", "not a coordinate"}},
		{"longitude out of range", false, []Point{good[0], {Lat: 54, Lon: -181}}, opts, []string{"point 1", "not a coordinate"}},
		{"nil FillElevation", false, good, LegOptions{}, []string{"FillElevation is nil", "NoFill"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc, err := NewMemCanon(MemCanonSnapshot{})
			if err != nil {
				t.Fatal(err)
			}
			var canon Canon = mc
			if c.nilC {
				canon = nil
			}
			_, err = AddLeg(ctx, canon, c.pts, c.opts)
			if err == nil {
				t.Fatal("AddLeg returned no error")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "merge: add leg: ") {
				t.Errorf("error %q does not start with %q", msg, "merge: add leg: ")
			}
			for _, w := range c.wants {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q does not contain %q", msg, w)
				}
			}
			snap, err := mc.Snapshot()
			if err != nil {
				t.Fatalf("a precondition error poisoned the canon: %v", err)
			}
			if len(snap.Nodes) != 0 || len(snap.Arcs) != 0 || snap.NextNodeID != 1 || snap.NextArcID != 1 {
				t.Errorf("a precondition error wrote to the canon: %+v", snap)
			}
		})
	}
}

// TestNewMemCanon_EmptySnapshotIsAnEmptyCanon: the zero snapshot is an empty
// canon whose first ids are 1, and a negative counter is still an error.
func TestNewMemCanon_EmptySnapshotIsAnEmptyCanon(t *testing.T) {
	ctx := context.Background()
	c, err := NewMemCanon(MemCanonSnapshot{})
	if err != nil {
		t.Fatalf("NewMemCanon(MemCanonSnapshot{}): %v", err)
	}
	id, err := c.AddNode(ctx, 54.5, -3, nil)
	if err != nil || id != 1 {
		t.Fatalf("first node id = %d, %v; want 1", id, err)
	}
	if _, err := NewMemCanon(MemCanonSnapshot{NextNodeID: -1}); err == nil ||
		!strings.HasPrefix(err.Error(), "merge: new memory canon: ") {
		t.Errorf("a negative node counter: got %v, want a merge: new memory canon: error", err)
	}
}

// zeroNodeCanon is a Canon that breaks the contract: it reports node id 0
// as the start of every arc.
type zeroNodeCanon struct{ *MemCanon }

func (z zeroNodeCanon) ArcEnds(ctx context.Context, id ArcID) (NodeID, NodeID, error) {
	_, end, err := z.MemCanon.ArcEnds(ctx, id)
	return 0, end, err
}

// TestAlign_ZeroNodeIDFromTheCanonIsAnError: a boundary at an arc's start
// takes the arc's start node from ArcEnds. A canon that answers 0 there makes
// the align step fail with an error that says so, not a boundary with no node.
func TestAlign_ZeroNodeIDFromTheCanonIsAnError(t *testing.T) {
	ctx := context.Background()
	d, _, arcB, _, _ := alignCrossFixture(t, ctx, "zero-node-id")
	_, err := splitCanonAtRunBoundaries(ctx, zeroNodeCanon{d}, []followRun{onRun(arcB, 0, 0, false)}, nil, nil)
	if err == nil {
		t.Fatal("a canon that returned node id 0 gave no error")
	}
	if want := "node id below 1 for an end of canon arc"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q is not the arc-ends check (want %q)", err, want)
	}
}

// zeroSplitCanon answers node id id (0 or below) for the node a split mints.
type zeroSplitCanon struct {
	*MemCanon
	id NodeID
}

func (z zeroSplitCanon) SplitArc(ctx context.Context, id ArcID, s PolylineSplit) (SplitIDs, error) {
	ids, err := z.MemCanon.SplitArc(ctx, id, s)
	ids.Node = z.id
	return ids, err
}

// badBoxCanon answers node id id (0 or below) for every node NodesInBox finds.
type badBoxCanon struct {
	*MemCanon
	id NodeID
}

func (z badBoxCanon) NodesInBox(ctx context.Context, b Bounds) ([]Node, error) {
	rows, err := z.MemCanon.NodesInBox(ctx, b)
	for i := range rows {
		rows[i].ID = z.id
	}
	return rows, err
}

// zeroHalfEndsCanon answers node id 0 for both ends of any arc above max,
// which here are the halves of a split.
type zeroHalfEndsCanon struct {
	*MemCanon
	max ArcID
}

func (z zeroHalfEndsCanon) ArcEnds(ctx context.Context, id ArcID) (NodeID, NodeID, error) {
	s, e, err := z.MemCanon.ArcEnds(ctx, id)
	if id > z.max {
		return 0, 0, err
	}
	return s, e, err
}

// TestAlign_ZeroNodeIDFromTheCanonIsAnError_AtEveryBoundarySource covers the
// two other places a boundary's node comes from the canon: the node a split
// mints, and the end node of an arc the boundary is already at.
func TestAlign_ZeroNodeIDFromTheCanonIsAnError_AtEveryBoundarySource(t *testing.T) {
	ctx := context.Background()
	for _, id := range []NodeID{0, -1} {
		t.Run(fmt.Sprintf("split/id=%d", id), func(t *testing.T) {
			d, _, arcB, _, totalB := alignCrossFixture(t, ctx, "zero-split")
			_, err := splitCanonAtRunBoundaries(ctx, zeroSplitCanon{d, id}, []followRun{onRun(arcB, totalB/2+20, totalB/2+20, false)}, nil, nil)
			want := fmt.Sprintf("node id %d for the node a split of arc", id)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want the split check (%q)", err, want)
			}
		})
	}
	t.Run("at an end", func(t *testing.T) {
		d, arcA, arcB, _, totalB := alignCrossFixture(t, ctx, "zero-half-end")
		_, err := splitCanonAtRunBoundaries(ctx, zeroHalfEndsCanon{d, max(arcA, arcB)}, []followRun{onRun(arcB, totalB/2+20, totalB/2+20, false)}, nil, nil)
		want := "node id 0 for an align boundary (at-row-end)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("got %v, want the at-an-end check (%q)", err, want)
		}
	})
}

// TestReresolve_NodeIDBelowOneFromTheCanonIsAnError: an absorbed boundary that
// finds a node at its own coordinate takes that node's id from NodesInBox. A
// canon that answers an id below 1 there fails with the at-existing-node check.
func TestReresolve_NodeIDBelowOneFromTheCanonIsAnError(t *testing.T) {
	ctx := context.Background()
	for _, id := range []NodeID{0, -1} {
		t.Run(fmt.Sprintf("id=%d", id), func(t *testing.T) {
			d := emptyCanon(t)
			p := mPt(0, 0)
			if _, err := d.AddNode(ctx, p.Lat, p.Lon, nil); err != nil {
				t.Fatal(err)
			}
			rep := AlignReport{Boundaries: []AlignBoundary{{
				Decline: AlignDeclineAbsorbedToNode, Node: 999, Lat: p.Lat, Lon: p.Lon,
			}}}
			err := reresolveAbsorbsAgainstThisPass(ctx, badBoxCanon{d, id}, &rep)
			want := fmt.Sprintf("node id %d for an align boundary (at-existing-node)", id)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want the at-existing-node check (%q)", err, want)
			}
		})
	}
}

// TestTJunction_NodeIDBelowOneFromTheSplitIsAnError: the T-junction split
// joins the leg terminal to the node the split mints, so an id below 1 there
// fails with the split check.
func TestTJunction_NodeIDBelowOneFromTheSplitIsAnError(t *testing.T) {
	ctx := context.Background()
	for _, id := range []NodeID{0, -1} {
		t.Run(fmt.Sprintf("id=%d", id), func(t *testing.T) {
			d := emptyCanon(t)
			addTestLeg(t, ctx, d, "A", warpFixtureA())
			_, err := splitCanonAtTerminal(ctx, zeroSplitCanon{d, id}, mPt(500, 14))
			want := fmt.Sprintf("node id %d for the node a split of arc", id)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want the split check (%q)", err, want)
			}
		})
	}
}

// TestWarpTarget_NodeIDBelowOneFromTheCanonIsAnError: the endpoint warp
// joins a terminal to a node NodesInBox found, so an id below 1 there fails
// with the warp-target check.
func TestWarpTarget_NodeIDBelowOneFromTheCanonIsAnError(t *testing.T) {
	ctx := context.Background()
	for _, id := range []NodeID{0, -1} {
		t.Run(fmt.Sprintf("id=%d", id), func(t *testing.T) {
			d := emptyCanon(t)
			n := mPt(0, 0)
			if _, err := d.AddNode(ctx, n.Lat, n.Lon, nil); err != nil {
				t.Fatal(err)
			}
			_, err := resolveWarpTarget(ctx, badBoxCanon{d, id}, mPt(3, 0))
			want := fmt.Sprintf("warp target: node search returned id %d", id)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want the warp-target check (%q)", err, want)
			}
		})
	}
}
