package merge

import (
	"context"
	"math"
	"strings"
	"testing"
)

// loadArcs is every arc of d as stored, in id order.
func loadArcs(t *testing.T, ctx context.Context, d *MemCanon) []Arc {
	t.Helper()
	var out []Arc
	for _, c := range committedArcs(t, ctx, d) {
		a, err := d.Arc(ctx, c.ID)
		if err != nil {
			t.Fatalf("Arc(%d): %v", c.ID, err)
		}
		out = append(out, a)
	}
	return out
}

func indexArcs(arcs []Arc) map[ArcID]Arc {
	m := make(map[ArcID]Arc, len(arcs))
	for _, s := range arcs {
		m[s.ID] = s
	}
	return m
}

// indexNodes is every node of d, by id.
func indexNodes(t *testing.T, ctx context.Context, d *MemCanon) map[NodeID]Node {
	t.Helper()
	nodes, err := d.NodesInBox(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("NodesInBox: %v", err)
	}
	m := make(map[NodeID]Node, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	return m
}

// arcVertsEle is an arc's full vertex walk — start node, pips, end node —
// with elevations (nil where unknown); false when an end node is missing.
func arcVertsEle(a Arc, nodes map[NodeID]Node) ([]Point, bool) {
	start, ok1 := nodes[a.StartNodeID]
	end, ok2 := nodes[a.EndNodeID]
	if !ok1 || !ok2 {
		return nil, false
	}
	out := make([]Point, 0, len(a.Polyline)+2)
	out = append(out, Point{Lat: start.Lat, Lon: start.Lon, Ele: start.Ele})
	for _, p := range a.Polyline {
		pt := Point{Lat: p.Lat, Lon: p.Lon}
		if !math.IsNaN(p.Ele) {
			e := p.Ele
			pt.Ele = &e
		}
		out = append(out, pt)
	}
	return append(out, Point{Lat: end.Lat, Lon: end.Lon, Ele: end.Ele}), true
}

// TestA2_CommitPieceSplitsAPieceThatEndsWhereItBegan is the ring split stated on the
// writer itself, and it exists because the behavioural test above did NOT catch
// the rule being removed.
//
// WHY A SECOND TEST FOR ONE RULE. Crippling [commitPiece] to return a ring
// unsplit reddens 200 pieces on a large test corpus and NOTHING in the short suite:
// whether the bulge fixture's OFF piece resolves both ends to one node depends
// on the follow's decomposition, and it currently does not. A rule that only a
// two-minute corpus run can falsify is a rule that is easy to delete by
// accident. This one calls the writer directly, so the fixture cannot drift out
// from under it.
//
// The unsplit ARM IS THE CONTROL: the same call with two different end nodes
// must yield one arc, or "always returns two" would pass this test while
// splitting every piece in the network.
//
// CRIPPLE THAT MUST REDDEN IT: return the piece unsplit from [commitPiece]
// when its two ends resolve to one node.
func TestA2_CommitPieceSplitsAPieceThatEndsWhereItBegan(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// The ring leaves node j, goes round, and comes back to it — the shape a leg
	// that leaves canon at a junction and rejoins at the same junction produces.
	ring := []Point{mPt(0, 0), mPt(50, 0), mPt(50, 50), mPt(0, 50), mPt(0, 0)}

	anchor, j, _, _, _, _, _, _, _, _, err := commitPiece(ctx, d, []Point{mPt(-100, 0), mPt(-50, 0), mPt(0, 0)}, nil, nil, nil)
	if err != nil {
		t.Fatalf("anchor piece: %v", err)
	}
	if len(anchor) != 1 {
		t.Fatalf("a piece whose two ends are different nodes committed %d arcs, want 1 — the ring split splits a RING, not every piece", len(anchor))
	}

	// The ring is a 50x50 m square — its halfway vertex is tens of metres from
	// node j, nowhere near EndpointWarpRadiusM, so this must NOT fire as the
	// tight-ring shape, in either of the two places it's reported.
	tightRingFired := false
	var hooks TestHooks
	hooks.ringSplitExclusion = func(leg string, mintedNodeID, excludedNodeID NodeID, distM float64) {
		tightRingFired = true
	}
	ctx = WithTestHooks(ctx, hooks)

	arcs, end, ringSplit, ringSplitMinted, _, tightPair, _, _, _, _, err := commitPiece(ctx, d, ring, &j, &j, nil)
	if err != nil {
		t.Fatalf("ring piece: %v", err)
	}
	if !ringSplit {
		t.Errorf("a piece whose two ends are the same node did not report ringSplit — the ring split must have taken the split branch here")
	}
	if tightPair != nil {
		t.Errorf("ring piece reported a tight-ring pair %v, want none — the ring's own halfway point is nowhere near node %d", *tightPair, j)
	}
	if tightRingFired {
		t.Errorf("ring split fired the tight-ring observer, want no fire — the ring's own halfway point is nowhere near node %d", j)
	}
	// Nothing else in this fresh, single-ring network is within radius of the
	// ring's own halfway point, so the one-node lookup finds nothing and this must
	// be a genuine mint, not an adoption.
	if !ringSplitMinted {
		t.Errorf("ring split reported an adoption, want a genuine mint — nothing else in this network is within endpointWarpRadiusM of the ring's halfway point")
	}

	if len(arcs) != 2 {
		t.Fatalf("a piece beginning and ending on node %d committed %d arc(s), want 2 — migration 050 forbids a row whose two ends are one node", j, len(arcs))
	}
	if end != j {
		t.Errorf("the ring reported ending on node %d, want %d — the caller threads this into the next piece, so a wrong answer opens a hole at the NEXT seam", end, j)
	}
	if arcs[0].Arc.StartNodeID != j || arcs[1].Arc.EndNodeID != j {
		t.Errorf("the halves run %d..%d and %d..%d — the ring's two ends are still node %d, and the split may only add a node in the middle",
			arcs[0].Arc.StartNodeID, arcs[0].Arc.EndNodeID, arcs[1].Arc.StartNodeID, arcs[1].Arc.EndNodeID, j)
	}
	if arcs[0].Arc.EndNodeID != arcs[1].Arc.StartNodeID {
		t.Errorf("the first half ends on node %d and the second begins on node %d — the minted node is a seam like any other and both sides must name it",
			arcs[0].Arc.EndNodeID, arcs[1].Arc.StartNodeID)
	}
	if arcs[0].Arc.EndNodeID == j {
		t.Errorf("the split minted node %d, which is the ring's own end — that is the row the CHECK rejects, written twice", j)
	}

	// GEOMETRY IS UNTOUCHED. The split is there to satisfy storage, so the two
	// halves read back as exactly the points that went in — an interpolated
	// midpoint, or a half taken from pts[mid+1:], would show up here.
	nodes := indexNodes(t, ctx, d)
	byID := indexArcs(loadArcs(t, ctx, d))
	var got []Point
	for i, s := range arcs {
		pts, ok := arcVertsEle(byID[s.Arc.ID], nodes)
		if !ok {
			t.Fatalf("half %d references a missing endpoint node", i)
		}
		if i > 0 {
			pts = pts[1:] // the shared seam vertex, already carried by the first half
		}
		got = append(got, pts...)
	}
	if len(got) != len(ring) {
		t.Fatalf("the halves read back as %d point(s), want the ring's %d — a split changes where the rows END, never what geometry they hold", len(got), len(ring))
	}
	for i := range got {
		if got[i].Lat != ring[i].Lat || got[i].Lon != ring[i].Lon {
			t.Errorf("point %d reads (%v,%v), want (%v,%v)", i, got[i].Lat, got[i].Lon, ring[i].Lat, ring[i].Lon)
		}
	}

	// NO FALLBACK ON THE DEGENERATE. A two-point ring has no interior vertex to
	// cut at, and inventing a midpoint would write geometry the source never
	// had — so it is an error, and the error is what the import surfaces.
	if _, _, _, _, _, _, _, _, _, _, err := commitPiece(ctx, d, []Point{mPt(0, 0), mPt(0, 0)}, &j, &j, nil); err == nil {
		t.Errorf("a two-point ring committed without complaint — it has no interior vertex, and the only ways to write it are a self-loop row or an invented coordinate")
	} else if !strings.Contains(err.Error(), "no interior vertex") {
		t.Errorf("a two-point ring failed with %v — it should name the missing interior vertex, since that is what the caller has to fix", err)
	}
}

// TestA2_RingSplitLandsAtTheTurnNotMeasureHalfway is the Prove-It for
// FurthestVertex: a ring is split at the point along it that is furthest from
// its start and end, not at its measure's halfway point.
//
// An ASYMMETRIC out-and-back: OUT is a single 30 m hop (the turn); BACK
// wanders home over three shorter hops totalling 61.75 m, none of which
// strays as far from the anchor as the turn does. Measure-halfway (91.75 m
// total, half 45.87 m) lands on the SECOND back-leg vertex (cumulative run
// 41.18 m, the closest of the four interior candidates to 45.87) — nowhere
// near the turn. Furthest-from-anchor lands on the FIRST vertex (30.0 m
// from the anchor, versus 26.93 / 15.81 / 15.81 for the other three) — the
// turn itself.
//
// CRIPPLE: make the ring-split branch pick the interior vertex whose cumulative
// run is closest to half the ring's measure, instead of FurthestVertex, and
// this goes red — the split lands on the second back-leg vertex (25,10)
// instead of the turn (30,0).
func TestA2_RingSplitLandsAtTheTurnNotMeasureHalfway(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	anchor, j, _, _, _, _, _, _, _, _, err := commitPiece(ctx, d, []Point{mPt(-100, 0), mPt(-50, 0), mPt(0, 0)}, nil, nil, nil)
	if err != nil {
		t.Fatalf("anchor piece: %v", err)
	}
	if len(anchor) != 1 {
		t.Fatalf("anchor piece committed %d arc(s), want 1", len(anchor))
	}

	// The turn: pts[1], 30 m out from the anchor. The wandering way home:
	// pts[2..4], each closer to the anchor than the turn (26.93 / 15.81 /
	// 15.81 m) but summing to more total distance than the out leg alone —
	// exactly the asymmetric-out-and-back shape measure-halfway gets wrong.
	turn := mPt(30, 0)
	ring := []Point{mPt(0, 0), turn, mPt(25, 10), mPt(5, 15), mPt(15, 5), mPt(0, 0)}

	arcs, _, ringSplit, _, _, _, _, _, _, _, err := commitPiece(ctx, d, ring, &j, &j, nil)
	if err != nil {
		t.Fatalf("ring piece: %v", err)
	}
	if !ringSplit {
		t.Fatalf("a piece whose two ends are the same node did not report ringSplit — the ring split must have taken the split branch here")
	}
	if len(arcs) != 2 {
		t.Fatalf("committed %d arc(s), want 2", len(arcs))
	}

	nodes := indexNodes(t, ctx, d)
	splitNode := arcs[0].Arc.EndNodeID
	got, ok := nodes[splitNode]
	if !ok {
		t.Fatalf("split node %d has no committed row", splitNode)
	}
	if got.Lat != turn.Lat || got.Lon != turn.Lon {
		t.Errorf("the split landed at (%v,%v), want the turn (%v,%v) — furthestVertex should have picked the OUT leg's own far point, not an measure-halfway point on the way back",
			got.Lat, got.Lon, turn.Lat, turn.Lon)
	}
}

// TestA2_CloseUnresolvedEndsMergeIntoOneRingNode is the Prove-It for merging
// a leg's own close ends and bisecting the loop: a leg whose own start and end are BOTH unresolved
// (neither adopted by anything upstream — commitPiece's mergeOwnEnds) and
// sit within EndpointWarpRadiusM of each other commits as TWO arcs
// sharing ONE freshly-minted node, via the same ring-split halfway split a
// pre-resolved ring already uses — not two separate, too-close nodes.
//
// CRIPPLE: revert commitPiece's mergeOwnEnds branch to the ordinary
// single-arc write and this test goes red with "1 arc(s), want 2"
// (the pre-decision-2 behaviour: two nodes minted, no shared seam).
func TestA2_CloseUnresolvedEndsMergeIntoOneRingNode(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// A 50x50 m loop whose end (5 m north of the start) is well within
	// EndpointWarpRadiusM (15 m) of its start, but the loop itself departs
	// the radius (out to 50 m) — LegDepartsRadius must read this as a
	// genuine loop, not a short straight spur (the adversarial-review fix,
	// same commit).
	loop := []Point{mPt(0, 0), mPt(50, 0), mPt(50, 50), mPt(0, 50), mPt(0, 5)}

	arcs, end, ringSplit, ringSplitMinted, ringNodeMinted, tightPair, _, _, _, _, err := commitPiece(ctx, d, loop, nil, nil, nil)
	if err != nil {
		t.Fatalf("commitPieceTx: %v", err)
	}
	if !ringSplit {
		t.Fatalf("a leg with close unresolved ends did not report ringSplit — mergeOwnEnds must route through the ring split")
	}
	if !ringNodeMinted {
		t.Errorf("ringNodeMinted = false, want true — neither end was resolved upstream, so the shared node must be a genuine fresh mint")
	}
	if !ringSplitMinted {
		t.Errorf("ringSplitMinted = false, want true — nothing else in this fresh network is within radius of the loop's own halfway point")
	}
	if tightPair != nil {
		t.Errorf("reported a tight-ring pair %v, want none — the halfway point is nowhere near the loop's own start/end", *tightPair)
	}

	if len(arcs) != 2 {
		t.Fatalf("a leg with close unresolved ends committed %d arc(s), want 2 — its start and end must merge into ONE shared node and bisect via the ring split, not mint two separate nodes 5 m apart", len(arcs))
	}
	if arcs[0].Arc.StartNodeID != arcs[1].Arc.EndNodeID {
		t.Fatalf("the two halves run %d..%d and %d..%d — the merged terminal must be the SAME node on both ends", arcs[0].Arc.StartNodeID, arcs[0].Arc.EndNodeID, arcs[1].Arc.StartNodeID, arcs[1].Arc.EndNodeID)
	}
	if arcs[0].Arc.EndNodeID != arcs[1].Arc.StartNodeID {
		t.Errorf("the first half ends on node %d and the second begins on node %d — the halfway node is a seam like any other and both sides must name it", arcs[0].Arc.EndNodeID, arcs[1].Arc.StartNodeID)
	}
	if end != arcs[0].Arc.StartNodeID {
		t.Errorf("commitPieceTx reported ending on node %d, want %d (the merged terminal) — the caller threads this into the next piece", end, arcs[0].Arc.StartNodeID)
	}
}

// TestA2_TwoDeclinedSeamsAtOneCoordinateShareOneNode: two declined seams at
// one coordinate share one node.
//
// The align step's exact-node check checks the network for an existing node at
// a boundary's coordinate BEFORE cutting canon — but it runs before this leg has
// committed anything, so it cannot see a node THIS SAME LEG is about to mint at
// one of its OWN declined seams. If the leg's followed geometry touches one
// coordinate at two SEPARATE stub-decline seams (the align step declined to cut canon at
// both — too short a run to be worth it), nothing before this fix asked whether
// the first seam's private mint already sits where the second is about to mint
// another. Measured on real data: one leg left 9 nodes at one lat/lon, and
// other legs more such stacks.
//
// The fixture calls [commitLegPieces] directly with two declined seams
// (HasNode: false) at DIFFERENT indices but the IDENTICAL coordinate, with real
// interior geometry between them — the shape the corpus measured, not the
// adjacent-claim case [TestA2_TwoSeamsOnOneNodeAtAdjacentPointsAreOneSeam]
// already covers (that one merges ADJACENT same-CANON-node claims; this one is
// two of the LEG'S OWN private mints, non-adjacent, sharing a coordinate no
// canon node was ever at).
//
// Threading the first seam's minted node into the second makes the piece BETWEEN
// them start and end on the same node — a genuine ring, which the writer
// already knows how to split. So the expected shape is: ONE node at the shared
// coordinate (not two), and the piece between the seams split at its interior
// halfway vertex (not aborted, not left as a same-node row).
//
// CRIPPLE: revert [commitLegPieces]'s `mintedAt` reuse (always mint on a
// declined seam) — reddens node count (2 at the coordinate instead of 1) and
// arc count (3 instead of 4, no ring split).
func TestA2_TwoDeclinedSeamsAtOneCoordinateShareOneNode(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	dup := mPt(10, 0)
	points := []Point{
		mPt(0, 0),  // idx0: leg start terminal (anchor, mints its own node)
		dup,        // idx1: seam A — declined, no canon node
		mPt(20, 0), // idx2: interior, between the two seams
		mPt(30, 0), // idx3: interior, between the two seams
		dup,        // idx4: seam B — declined, no canon node, SAME coordinate as idx1
		mPt(40, 0), // idx5: leg end terminal (anchor, mints its own node)
	}
	seams := []legSeam{
		{Idx: 1, HasNode: false},
		{Idx: 4, HasNode: false},
	}

	pieces, _, _, _, _, _, _, _, _, err := commitLegPieces(ctx, d, points, seams, nil, nil, nil)
	if err != nil {
		t.Fatalf("commitLegPiecesTx: %v", err)
	}

	if len(pieces) != 4 {
		t.Fatalf("committed %d piece(s), want 4 — start-terminal..seamA (1), seamA..seamB split by the ring split into 2 since "+
			"both ends now share ONE node, seamB..end-terminal (1)", len(pieces))
	}

	nodes := indexNodes(t, ctx, d)
	atDup := 0
	for _, n := range nodes {
		if n.Lat == dup.Lat && n.Lon == dup.Lon {
			atDup++
		}
	}
	if atDup != 1 {
		t.Fatalf("%d node row(s) at the shared coordinate %v, want 1 — two declined seams of one leg at one lat/lon "+
			"must share a node, not mint one each", atDup, dup)
	}

	// pieces = [start-term..seamA, seamA..half (ring 1st half), half..seamB (ring
	// 2nd half), seamB..end-term]. The shared coordinate's ONE node is seamA's
	// end, seamB's start AND end (the ring closes on it), and seamB's own start —
	// named four different ways, and they must all agree.
	j := pieces[0].Arc.EndNodeID
	if pieces[1].Arc.StartNodeID != j {
		t.Errorf("piece 1 starts on node %d, want %d (seam A's node)", pieces[1].Arc.StartNodeID, j)
	}
	if pieces[1].Arc.StartNodeID == pieces[1].Arc.EndNodeID {
		t.Errorf("piece 1 (%d..%d) is a same-node row — the ring split exists to make that impossible", pieces[1].Arc.StartNodeID, pieces[1].Arc.EndNodeID)
	}
	if pieces[1].Arc.EndNodeID != pieces[2].Arc.StartNodeID {
		t.Errorf("the ring's two halves disagree on their shared halfway node: piece 1 ends on %d, piece 2 starts on %d",
			pieces[1].Arc.EndNodeID, pieces[2].Arc.StartNodeID)
	}
	if pieces[2].Arc.EndNodeID != j {
		t.Errorf("piece 2 (the ring's own end) is on node %d, want %d — the ring closes on the SAME node it opened on", pieces[2].Arc.EndNodeID, j)
	}
	if pieces[3].Arc.StartNodeID != j {
		t.Errorf("piece 3 starts on node %d, want %d (seam B's node, threaded from the ring's end)", pieces[3].Arc.StartNodeID, j)
	}
	// No assertNetworkSound: it checks network bounds, which the full commit
	// pipeline recomputes and this direct call to commitLegPieces does not —
	// same reason TestA2_CommitPieceSplitsAPieceThatEndsWhereItBegan skips it.
}
