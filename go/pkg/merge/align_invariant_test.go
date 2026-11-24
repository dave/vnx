package merge

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestAlign_PostMintInvariantFiresWhenAdoptFails proves that the check after
// a mint catches a new node inside EndpointWarpRadiusM of a node on the
// ridden lineage. That check cannot fire under correct code: the search
// before the mint adopts any such node. So the test forces that adopt to fail
// (TestHooks.suppressNearbyAdopt) instead of building geometry that no
// correct code can produce.
//
// Geometry: arcB is cut 20 m past its midpoint, which mints existingNode. A
// second pass then cuts the half that starts or ends at existingNode, 5 m
// from it. existingNode is an end of that half, so it is on the ridden
// lineage and the search would adopt it. With the adopt forced off, the
// boundary is cut 5 m from existingNode, and the check must abort and name
// both nodes and the distance.
//
// The node must be on the lineage. A node on another arc (for example a cut
// of arcA at the crossing) is a foreign node: a mint next to it is an
// intended outcome of the lineage preference, which the check exempts.
func TestAlign_PostMintInvariantFiresWhenAdoptFails(t *testing.T) {
	ctx := context.Background()
	d, _, arcB, _, totalB := alignCrossFixture(t, ctx, "align-post-mint-invariant")

	// An ordinary cut, with no node near: mints existingNode.
	rep := runAlign(t, ctx, d, []followRun{onRun(arcB, totalB/2+20, totalB/2+20, false)})
	if rep.Cuts != 1 {
		t.Fatalf("cutting arcB 20 m past its midpoint: cuts = %d, want 1 (%s)", rep.Cuts, describeAlign(rep))
	}
	existingNode := rep.Boundaries[0].Node
	if existingNode == 0 {
		t.Fatalf("the cut of arcB minted no node id")
	}

	// The half of arcB that ends past the cut, and the measure on it 5 m from
	// existingNode.
	half := rep.Boundaries[0].Next
	start, end, err := d.ArcEnds(ctx, half)
	if err != nil {
		t.Fatalf("read the ends of arc %d: %v", half, err)
	}
	arc, ok, err := d.CanonArc(ctx, half)
	if err != nil || !ok {
		t.Fatalf("read arc %d: ok=%v err=%v", half, ok, err)
	}
	halfLen := canonMeasureLengthM(arc.Vertices)
	var at float64
	switch existingNode {
	case start:
		at = 5
	case end:
		at = halfLen - 5
	default:
		t.Fatalf("arc %d (nodes %d..%d) does not end at existingNode %d", half, start, end, existingNode)
	}

	// The injection forces every adopt to fail and counts its calls, so the
	// test cannot pass because the boundary never reached the search.
	suppressCalls := 0
	var hooks TestHooks
	hooks.suppressNearbyAdopt = func() bool {
		suppressCalls++
		return true
	}
	ctx = WithTestHooks(ctx, hooks)

	_, err = splitCanonAtRunBoundaries(ctx, d, []followRun{onRun(half, at, at, false)}, nil, nil)

	if suppressCalls == 0 {
		t.Fatalf("the adopt hook was never called: the boundary did not reach the existing-node search")
	}
	if err == nil {
		t.Fatalf("cutting arc %d 5 m from existingNode, with the adopt suppressed, returned no error: the check after the mint should catch the new node", half)
	}
	msg := err.Error()
	if !strings.Contains(msg, "already sits") {
		t.Fatalf("got error %q, want the one-node check's violation", msg)
	}
	if want := fmt.Sprintf("node %d already sits", existingNode); !strings.Contains(msg, want) {
		t.Errorf("error %q does not name existing node %d", msg, existingNode)
	}
	// The distance is near 5 m. It is not exactly 5: mPt's metres are not
	// bit-exact under haversine.
	m := regexp.MustCompile(`already sits ([0-9.]+) m away`).FindStringSubmatch(msg)
	if m == nil {
		t.Fatalf("error %q does not state the distance", msg)
	}
	dist, err := strconv.ParseFloat(m[1], 64)
	if err != nil || dist < 4 || dist > 6 {
		t.Errorf("error states a distance of %q m, want about 5 m", m[1])
	}
}
