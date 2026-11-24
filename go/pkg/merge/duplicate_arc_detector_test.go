package merge

import (
	"context"
	"testing"
)

// TestDuplicateArcDetector_RedProof is the detector's own cripple test.
//
// ⚠ IT CALLS THE DETECTOR DIRECTLY AND DOES NOT GO THROUGH AN IMPORT, and the
// reason is a finding in its own right. Written first as two imports — canon,
// then a plan with one vertex nudged 1e-6 degrees — every "must not match"
// case MATCHED. That is not a tolerance in the detector: it is S1 doing its
// job. The matcher recognises the second plan as following canon and rewrites
// its geometry byte-exactly onto canon's before anything commits, which is
// precisely the mechanism the duplicate-strand census's own header describes.
// So a full import CANNOT present the detector with a near-miss — by write
// time there are no near-misses left — and a red-proof built that way is an
// instrument that can only return the answer we expect.
//
// The exactness of the predicate therefore has to be fixed here, one node pair
// and one hand-built vertex sequence at a time.
//
// ⚠ THE CORPUS MEASUREMENT ABOVE CANNOT DO THIS EITHER. Its bar is a
// relationship between two counts that move TOGETHER — widen the predicate and
// both the census band and the firing count grow, so the comparison stays
// satisfied while both are wrong.
func TestDuplicateArcDetector_RedProof(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	base := []Point{mPt(0, 0), mPt(50, 0), mPt(100, 0)}
	res := addTestLeg(t, ctx, d, "canon", base)
	canonArc := res.Pieces[0].Arc.ID
	if n := len(committedArcs(t, ctx, d)); n != 1 {
		t.Fatalf("fixture committed %d arc(s), want 1", n)
	}
	arc, err := d.Arc(ctx, canonArc)
	if err != nil {
		t.Fatalf("load arc: %v", err)
	}
	startID, endID := arc.StartNodeID, arc.EndNodeID
	startNode, _, err := d.Node(ctx, startID)
	if err != nil {
		t.Fatalf("load start node: %v", err)
	}
	endNode, _, err := d.Node(ctx, endID)
	if err != nil {
		t.Fatalf("load end node: %v", err)
	}
	// The detector replaces a piece's own two END vertices with its nodes'
	// coordinates, because that is what the writer does. So a fixture varying
	// an endpoint would be varying nothing; every case below varies the
	// INTERIOR, which is the part that actually reaches the comparison.
	interior := func(pts ...Point) []Point {
		out := []Point{{Lat: startNode.Lat, Lon: startNode.Lon}}
		out = append(out, pts...)
		return append(out, Point{Lat: endNode.Lat, Lon: endNode.Lon})
	}
	mid := Point{Lat: arc.Polyline[0].Lat, Lon: arc.Polyline[0].Lon}
	nudged := Point{Lat: mid.Lat + 1e-9, Lon: mid.Lon}

	// ⚠ DIRECTION IS CARRIED BY WHICH NODE THE PIECE STARTS AT, not by the
	// order of the points handed in. Written first as a reversed point list
	// with the SAME node ids, the reversed case reported forward — correctly,
	// because the detector replaces a piece's end vertices with its nodes'
	// coordinates, so reversing only the points reverses nothing. A piece that
	// genuinely runs the other way arrives with its node ids swapped.
	for _, tc := range []struct {
		name       string
		pts        []Point
		start, end NodeID
		want       bool
		reversed   bool
	}{
		{"the committed sequence", interior(mid), startID, endID, true, false},
		{
			"the committed sequence reversed",
			[]Point{
				{Lat: endNode.Lat, Lon: endNode.Lon}, mid, {Lat: startNode.Lat, Lon: startNode.Lon},
			},
			endID, startID, true, true,
		},
		{
			// ⚠ NO TOLERANCE. 1e-9 degrees is roughly a tenth of a
			// millimetre — far below anything anyone would propose as a
			// tolerance. If this ever matches, the predicate has stopped being
			// exact and the 2.46 m strands the census separates would be
			// swallowed next.
			"the interior vertex nudged 1e-9 degrees",
			interior(nudged), startID, endID, false, false,
		},
		{
			// The three real strands that sit 0.00 m from their
			// base measured BOTH ways and still differ: every vertex here
			// lands on a vertex of canon, and canon carries one this does not.
			"the interior vertex dropped — coincident, not identical",
			interior(), startID, endID, false, false,
		},
		{
			"an extra interior vertex",
			interior(Point{Lat: mid.Lat, Lon: mid.Lon}, mid), startID, endID, false, false,
		},
		{
			"the interior vertices in the wrong order",
			interior(mid, nudged), startID, endID, false, false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end := tc.start, tc.end
			got, err := findDuplicateCanonArcs(ctx, d, tc.pts, &start, &end)
			if err != nil {
				t.Fatalf("detector: %v", err)
			}
			if (len(got) > 0) != tc.want {
				t.Fatalf("detector returned %+v, want match = %v", got, tc.want)
			}
			if !tc.want {
				return
			}
			if len(got) != 1 || got[0].Arc.ID != canonArc {
				t.Fatalf("matched %+v, want exactly canon's arc %d", got, canonArc)
			}
			if got[0].Reversed != tc.reversed {
				t.Errorf("Reversed = %v, want %v", got[0].Reversed, tc.reversed)
			}
		})
	}

	// An unresolved end is an EXPECTED absence, answered without a query.
	if got, err := findDuplicateCanonArcs(ctx, d, interior(mid), nil, &endID); err != nil || len(got) != 0 {
		t.Errorf("a piece whose start end resolved to no node: got %+v, %v — want no match, because a node that does not exist cannot bound an arc that does", got, err)
	}
}
