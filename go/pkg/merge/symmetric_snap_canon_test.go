package merge

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// snapMarker commits one 2-point spur running NORTH from (xM, yM), and returns
// the node id at its south end. The spur is 400 m long so its far terminal is
// nowhere near the legs under test, and it runs perpendicular to them so the
// follow has nothing to bind: the ONLY thing the leg can see is the node.
func snapMarker(t *testing.T, ctx context.Context, d *MemCanon, name string, xM, yM float64) NodeID {
	t.Helper()
	addTestLeg(t, ctx, d, name, []Point{mPt(xM, yM), mPt(xM, yM+400)})
	want := mPt(xM, yM)
	nodes, err := d.NodesInBox(ctx, Bounds{MinLat: want.Lat, MaxLat: want.Lat, MinLon: want.Lon, MaxLon: want.Lon})
	if err != nil {
		t.Fatalf("%s: NodesInBox: %v", name, err)
	}
	for _, n := range nodes {
		if n.Lat == want.Lat && n.Lon == want.Lon {
			return n.ID
		}
	}
	t.Fatalf("%s: no node at (%v,%v) — the marker did not commit its south terminal where the fixture expects it", name, want.Lat, want.Lon)
	return 0
}

// runSnapProducer calls the producer, which writes nothing, and fails the
// test on an error.
func runSnapProducer(
	t *testing.T, ctx context.Context, d *MemCanon,
	pts []Point, seams []legSeam, startT, endT *NodeID,
) ([]Point, []legSeam, SymmetricSnapReport) {
	t.Helper()
	outPts, outSeams, rep, err := legSeamsFromNearbyNodes(ctx, d, pts, seams, nil, startT, endT)
	if err != nil {
		t.Fatalf("LegSeamsFromNearbyNodes: %v", err)
	}
	return outPts, outSeams, rep
}

// runInteriorNodeProducer calls the producer, which writes nothing, and fails
// the test on an error.
func runInteriorNodeProducer(
	t *testing.T, ctx context.Context, d *MemCanon,
	pts []Point, seams []legSeam, startT, endT *NodeID,
) ([]legSeam, InteriorNodeSeamReport) {
	t.Helper()
	outSeams, rep, err := legSeamsAtInteriorNodes(ctx, d, pts, seams, startT, endT)
	if err != nil {
		t.Fatalf("LegSeamsAtInteriorNodes: %v", err)
	}
	return outSeams, rep
}

// nodeDegree counts the committed arc ends at one node.
func nodeDegree(t *testing.T, ctx context.Context, d *MemCanon, id NodeID) int {
	t.Helper()
	n := 0
	for _, a := range committedArcs(t, ctx, d) {
		start, end, err := d.ArcEnds(ctx, a.ID)
		if err != nil {
			t.Fatalf("nodeDegree: ArcEnds: %v", err)
		}
		if start == id {
			n++
		}
		if end == id {
			n++
		}
	}
	return n
}

// canonCensus is the part of a network census these tests read: the node
// and arc counts, and each arc's geometry as exact bits.
type canonCensus struct {
	Nodes, Arcs int
	ArcHash     map[ArcID]string
}

// censusNetwork counts the canon's nodes and arcs and records each arc's
// vertices bit for bit.
func censusNetwork(t *testing.T, ctx context.Context, d *MemCanon) canonCensus {
	t.Helper()
	nodes, err := d.NodesInBox(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("censusNetwork: NodesInBox: %v", err)
	}
	arcs := committedArcs(t, ctx, d)
	c := canonCensus{Nodes: len(nodes), Arcs: len(arcs), ArcHash: map[ArcID]string{}}
	for _, a := range arcs {
		var b strings.Builder
		for _, v := range a.Vertices {
			fmt.Fprintf(&b, "%x,%x;", math.Float64bits(v.Lat), math.Float64bits(v.Lon))
		}
		c.ArcHash[a.ID] = b.String()
	}
	return c
}

// ── THE HEADLINE: A NODE ON THE LEG'S PATH IS ADOPTED ───────────────────────

// TestSymmetricSnap_AdoptsANodeOnTheLegsPath reproduces the shape the symmetric
// snap exists to close: a node minted by an earlier
// leg, and a later leg drawing a line past it that no rule ever looked at.
//
// The offset is derived from EndpointWarpRadiusM rather than typed, so this
// fixture cannot drift away from the rule it exercises.
func TestSymmetricSnap_AdoptsANodeOnTheLegsPath(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	inside := EndpointWarpRadiusM * 0.7 // 10.5 m — comfortably inside the radius
	marker := snapMarker(t, ctx, d, "marker", 600, inside)

	pts := snapStraightLeg()
	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if rep.Adopted() != 1 {
		t.Fatalf("adopted %d node(s), want 1\n%s", rep.Adopted(), describeSymmetricSnap(rep))
	}
	if len(outSeams) != 1 {
		t.Fatalf("produced %d seam(s), want 1\n%s", len(outSeams), describeSymmetricSnap(rep))
	}
	s := outSeams[0]

	if s.Node != marker {
		t.Errorf("seam claims node %d, want the marker %d", s.Node, marker)
	}
	if !s.HasNode {
		t.Errorf("seam has HasNode=false — a seam with no node is a private mint, not an adoption")
	}
	if !s.Adopted {
		t.Errorf("seam has Adopted=false, but the node existed in canon before this leg — nothing here mints")
	}
	// ⚠ THE AUDIT FINDING. applyLegBoundaryWarp (warp_boundary.go) targets a
	// seam's NODE coordinates ONLY when Absorbed; a seam marked merely Adopted
	// gets a zero-displacement anchor at its own point and the leg never
	// reaches the junction it claims.
	if !s.Absorbed {
		t.Errorf("seam has Absorbed=false — applyLegBoundaryWarp moves the leg ONLY for Absorbed, so the committed line would never touch the node it claims")
	}
	if s.AbsorbedM <= 0 {
		t.Errorf("AbsorbedM = %v, want the node's own distance from the leg", s.AbsorbedM)
	}
	if math.Abs(s.AbsorbedM-inside) > 0.5 {
		t.Errorf("AbsorbedM = %.3f m, want ~%.3f m (the node's distance from the leg)", s.AbsorbedM, inside)
	}

	// The vertex was INSERTED at the projection — the marker sits at x=600,
	// which falls strictly inside the 500→750 segment.
	if len(outPts) != len(pts)+1 {
		t.Fatalf("leg has %d point(s), want %d — one vertex inserted at the projection", len(outPts), len(pts)+1)
	}
	if !rep.Candidates[0].Inserted {
		t.Errorf("the candidate reports Inserted=false, but the projection falls strictly inside a segment")
	}
	if s.Idx != 3 {
		t.Errorf("seam at index %d, want 3 — inserted after original vertex 2 (x=500)", s.Idx)
	}
	// NodeLat/NodeLon are the NODE's own recorded coordinates, and the warp
	// target is built from them. They must not be the projection.
	n, _, err := d.Node(ctx, marker)
	if err != nil {
		t.Fatalf("load marker node: %v", err)
	}
	if s.NodeLat != n.Lat || s.NodeLon != n.Lon {
		t.Errorf("seam NodeLat/NodeLon = (%v,%v), want the node's own row (%v,%v) — carried through, never recomputed",
			s.NodeLat, s.NodeLon, n.Lat, n.Lon)
	}
	if outPts[s.Idx].Lat == n.Lat && outPts[s.Idx].Lon == n.Lon {
		t.Errorf("the inserted vertex is already AT the node — it should sit at the projection, and the boundary warp is what moves it")
	}

	// Elevation: the ramp is 300 m at x=500 and 400 m at x=750, so a vertex at
	// x=600 interpolates to ~340 m. The rule is splitNodeCoord's, mirrored.
	if outPts[s.Idx].Ele == nil {
		t.Fatalf("the inserted vertex has no elevation, but both segment ends do")
	}
	if got := *outPts[s.Idx].Ele; math.Abs(got-340) > 1 {
		t.Errorf("inserted vertex elevation %.2f, want ~340 (linear between 300 at x=500 and 400 at x=750)", got)
	}

	// Every original vertex survives, in order and bit-exact. This producer
	// adds a point; it never moves or drops one.
	for i, p := range pts {
		j := i
		if i > 2 {
			j = i + 1
		}
		if outPts[j].Lat != p.Lat || outPts[j].Lon != p.Lon {
			t.Errorf("original vertex %d is not at output index %d bit-exact", i, j)
		}
	}
}

// TestSymmetricSnap_ProjectionOnAnExistingVertexInsertsNothing is the other
// half of the insertion decision. ProjectProbeOntoWalk returns a vertex's
// STORED coordinates bitwise when the closest approach is that vertex, so the
// seam claims the index the leg already had. Inserting a duplicate point would
// put a zero-length edge into the committed geometry.
func TestSymmetricSnap_ProjectionOnAnExistingVertexInsertsNothing(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Directly north of the leg's x=500 vertex, so the foot of the
	// perpendicular IS that vertex.
	marker := snapMarker(t, ctx, d, "marker", 500, EndpointWarpRadiusM*0.7)

	pts := snapStraightLeg()
	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if rep.Adopted() != 1 || len(outSeams) != 1 {
		t.Fatalf("adopted %d, %d seam(s), want 1 and 1\n%s", rep.Adopted(), len(outSeams), describeSymmetricSnap(rep))
	}
	if len(outPts) != len(pts) {
		t.Fatalf("leg has %d point(s), want %d unchanged — the projection lands on a vertex the leg already had", len(outPts), len(pts))
	}
	if rep.Candidates[0].Inserted {
		t.Errorf("candidate reports Inserted=true, but no vertex was added")
	}
	if outSeams[0].Idx != 2 {
		t.Errorf("seam at index %d, want 2 — the existing x=500 vertex", outSeams[0].Idx)
	}
	if outSeams[0].Node != marker {
		t.Errorf("seam claims node %d, want %d", outSeams[0].Node, marker)
	}
}

// ── THE INHERITED DECLINES ───────────────────────────────────────────────

// TestSymmetricSnap_AdoptsAShortStubAtTheLegsOwnEnd. Formerly
// TestSymmetricSnap_DeclinesAShortStubAtTheLegsOwnEnd, INVERTED when the
// leg-side stub decline was lifted, so a candidate 10 m from the leg's start
// is now adopted rather than left alone.
//
// StubM is still asserted to be under the lifted 25 m minimum. That is not decoration — it is
// what keeps this a test of the LIFT rather than of an arbitrary fixture. Both
// halves only hold in the lifted state: the piece is short AND it was taken
// anyway. Restore the rule and this goes red at the adoption.
func TestSymmetricSnap_AdoptsAShortStubAtTheLegsOwnEnd(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	minRun := liftedStubMinRunM
	// 10 m along the leg — well inside the lifted 25 m minimum of the leg's start, so
	// adopting would leave a 10 m piece that no import can ever follow.
	snapMarker(t, ctx, d, "marker", minRun*0.4, EndpointWarpRadiusM*0.5)

	pts := snapStraightLeg()
	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if len(rep.Candidates) != 1 {
		t.Fatalf("search found %d candidate(s), want 1 — the fixture must put the node IN range so the DECLINE is what is being tested, not the radius\n%s",
			len(rep.Candidates), describeSymmetricSnap(rep))
	}
	if got := rep.Candidates[0].Decline; got != SnapNoDecline {
		t.Fatalf("decline = %v, want snapNoDecline — the stub rule is lifted\n%s", got, describeSymmetricSnap(rep))
	}
	if len(outSeams) != 1 {
		t.Errorf("produced %d seam(s), want 1 — the candidate is adopted now", len(outSeams))
	}
	if len(outPts) != len(pts)+1 {
		t.Errorf("leg has %d point(s), want %d — an adoption inserts the projection as a vertex", len(outPts), len(pts)+1)
	}
	if rep.Candidates[0].StubM >= minRun {
		t.Errorf("StubM = %.3f m, want under the lifted minimum (%v) — the fixture is not exercising the rule it was built for", rep.Candidates[0].StubM, minRun)
	}
	t.Logf("adopted a candidate leaving a %.3f m piece against a MinRun of %v m", rep.Candidates[0].StubM, minRun)
}

// TestSymmetricSnap_StubIsMeasuredAgainstAnAlignSeamNotJustTheEnds. Formerly
// TestSymmetricSnap_DeclinesAShortStubBesideAnAlignSeam, RE-POINTED
// rather than inverted, because the bug it catches is untouched by
// the lift.
//
// What it catches, then and now: `boundaries` in [legSeamsFromNearbyNodes]
// must include every align seam the leg already carries, not just its two ends.
// The T-junction split cuts an arc that is whole, so its two halves are bounded by the arc's
// own ends; a leg arriving here may already carry align seams, and a piece 10 m
// from one of those is 10 m long exactly as surely as one 10 m from the end.
// The node sits 500 m from either terminal — nowhere near one — and 10 m from
// an existing align seam.
//
// What CHANGED is only the detector. The test used to read the generalisation
// off the DECLINE, which was a side effect: an ends-only boundary set would
// have made StubM ~500 m, no decline would fire, and the assertion would go
// red. The decline is lifted, so that detector is gone — but StubM itself is
// still computed against the same boundary set and still REPORTED, so the
// generalisation is read directly instead. That is strictly more specific than
// the decline ever was: it pins the measured value, not a verdict derived from
// it, and it cannot be satisfied by a rule that declines for another reason.
//
// ⚠ THIS TEST GOES RED IF THE STUB IS MEASURED AGAINST THE LEG'S TWO ENDS
// ONLY — which is what it has always been for.
func TestSymmetricSnap_StubIsMeasuredAgainstAnAlignSeamNotJustTheEnds(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	minRun := liftedStubMinRunM
	// The leg's x=500 vertex is an existing align seam. The marker projects
	// 10 m further along, inside the lifted minimum of it.
	other := snapMarker(t, ctx, d, "seamnode", 500, 300)
	snapMarker(t, ctx, d, "marker", 500+minRun*0.4, EndpointWarpRadiusM*0.5)

	pts := snapStraightLeg()
	align := []legSeam{{Idx: 2, Node: other, HasNode: true, Adopted: true,
		NodeLat: pts[2].Lat, NodeLon: pts[2].Lon}}

	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, align, nil, nil)

	if len(rep.Candidates) != 1 {
		t.Fatalf("search found %d candidate(s), want 1\n%s", len(rep.Candidates), describeSymmetricSnap(rep))
	}
	c := rep.Candidates[0]
	if c.PriorLenM < minRun || c.PriorLenM > legMeasureLengthM(pts)-minRun {
		t.Fatalf("candidate sits at measure %.1f m on a %.1f m leg — the fixture must put it far from BOTH ends, or an ends-only stub rule would decline it for the wrong reason",
			c.PriorLenM, legMeasureLengthM(pts))
	}
	// THE ASSERTION THIS TEST EXISTS FOR. The align seam sits at the leg's
	// x=500 vertex and the candidate projects minRun*0.4 further along, so a
	// boundary set that knows about the seam measures ~10 m and one that knows
	// only the leg's two ends measures ~500 m. The two answers are two orders
	// of magnitude apart; nothing rounds one into the other.
	wantStub := minRun * 0.4
	if math.Abs(c.StubM-wantStub) > 1 {
		t.Fatalf("StubM = %.3f m, want ~%.1f m — the stub is being measured against the leg's two ends (~%.0f m away) "+
			"instead of against every boundary the leg already carries\n%s",
			c.StubM, wantStub, c.PriorLenM, describeSymmetricSnap(rep))
	}
	// The stub rule is lifted, so a short piece no longer declines: the leg
	// keeps its align seam AND takes the new one. Asserted so this test also
	// fails if the lift is reverted without revisiting it.
	if c.Decline != SnapNoDecline {
		t.Fatalf("decline = %v, want snapNoDecline — the stub rule is lifted\n%s", c.Decline, describeSymmetricSnap(rep))
	}
	if len(outSeams) != 2 {
		t.Errorf("produced %d seam(s), want 2 — the pre-existing align seam plus the newly adopted one", len(outSeams))
	}
	if len(outPts) != len(pts)+1 {
		t.Errorf("leg has %d point(s), want %d — the adopted projection is inserted as a vertex", len(outPts), len(pts)+1)
	}
}

// TestSymmetricSnap_DeclinesAtALegTerminalVertex covers the terminal rule by way of
// TjunctionDeclineFor's rule 3: a closest approach at Fraction exactly 0 or 1
// is the leg's own endpoint ANCHOR, and noding it is the permanently rejected
// annotated-endpoint-noding.
func TestSymmetricSnap_DeclinesAtALegTerminalVertex(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Due WEST of the leg's start, so the perpendicular clamps to vertex 0 and
	// Fraction is exactly 0.
	snapMarker(t, ctx, d, "marker", -EndpointWarpRadiusM*0.6, 0)

	pts := snapStraightLeg()
	_, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if len(rep.Candidates) != 1 {
		t.Fatalf("search found %d candidate(s), want 1\n%s", len(rep.Candidates), describeSymmetricSnap(rep))
	}
	if rep.Candidates[0].Fraction != 0 {
		t.Fatalf("candidate Fraction = %v, want exactly 0 — the fixture is not exercising the terminal-vertex rule\n%s",
			rep.Candidates[0].Fraction, describeSymmetricSnap(rep))
	}
	if got := rep.Candidates[0].Decline; got != SnapDeclineTerminalVertex {
		t.Errorf("decline = %v, want snapDeclineTerminalVertex", got)
	}
	if len(outSeams) != 0 {
		t.Errorf("produced %d seam(s), want 0 — a leg terminal is a lat-lon anchor, never a shared node", len(outSeams))
	}
}

// ── THE SENTINEL ────────────────────────────────────────────────────────────

// TestSymmetricSnap_ZeroLengthLegIsSkippedNotFatal. A GPX route of two or more
// identical points is legal input that imports TODAY. nodesNearPath cannot
// project onto such a walk and errors, wrapping ErrWalkHasNoLength — so if the
// producer propagates that, an import that works today starts aborting.
func TestSymmetricSnap_ZeroLengthLegIsSkippedNotFatal(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)
	snapMarker(t, ctx, d, "marker", 0, EndpointWarpRadiusM*0.5)

	same := mPt(0, 0, 100)
	pts := []Point{same, same, same}

	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if !rep.Skipped {
		t.Errorf("report does not say Skipped — a caller cannot tell 'asked and found nothing' from 'never asked'")
	}
	if len(outSeams) != 0 {
		t.Errorf("produced %d seam(s) on a leg with no path", len(outSeams))
	}
	if len(outPts) != len(pts) {
		t.Errorf("leg has %d point(s), want %d unchanged", len(outPts), len(pts))
	}

	// The sentinel really is what the primitive raises — if this stops being
	// true the skip above is passing for the wrong reason.
	if _, err := nodesNearPath(ctx, d, pts, EndpointWarpRadiusM, nil); err == nil {
		t.Fatalf("NodesNearPath accepted a zero-length walk — the skip in the producer is now dead code guarding nothing")
	}
}

// ── EXCLUSION AND REMAPPING ─────────────────────────────────────────────────

// TestSymmetricSnap_NodeAlreadyClaimedIsNotAddedTwice. The exclusion goes into
// the search, before the radius test — a post-hoc discard of a returned row is
// how an exclusion gets defeated.
func TestSymmetricSnap_NodeAlreadyClaimedIsNotAddedTwice(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 600, EndpointWarpRadiusM*0.7)
	pts := snapStraightLeg()

	// Without the claim it is adopted — establishing that the exclusion below
	// is what suppresses it, not the radius or a decline.
	_, bare, repBare := runSnapProducer(t, ctx, d, pts, nil, nil, nil)
	if len(bare) != 1 {
		t.Fatalf("control: produced %d seam(s), want 1\n%s", len(bare), describeSymmetricSnap(repBare))
	}

	align := []legSeam{{Idx: 2, Node: marker, HasNode: true, Adopted: true,
		NodeLat: pts[2].Lat, NodeLon: pts[2].Lon}}
	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, align, nil, nil)

	if len(rep.Candidates) != 0 {
		t.Errorf("the search returned %d candidate(s) for a node an align seam already claims\n%s",
			len(rep.Candidates), describeSymmetricSnap(rep))
	}
	if len(outSeams) != 1 {
		t.Fatalf("produced %d seam(s), want 1 — the align seam and nothing else", len(outSeams))
	}
	if len(outPts) != len(pts) {
		t.Errorf("leg has %d point(s), want %d — nothing to insert", len(outPts), len(pts))
	}
}

// TestSymmetricSnap_LegTerminalNodesAreExcluded. A terminal the endpoint warp already resolved
// is a lat-lon anchor; re-adopting it as an INTERIOR seam would node the
// highlight's own end.
func TestSymmetricSnap_LegTerminalNodesAreExcluded(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Placed where it would otherwise be adopted as an interior seam.
	marker := snapMarker(t, ctx, d, "marker", 600, EndpointWarpRadiusM*0.7)
	pts := snapStraightLeg()

	_, outSeams, rep := runSnapProducer(t, ctx, d, pts, nil, &marker, nil)
	if len(rep.Candidates) != 0 || len(outSeams) != 0 {
		t.Errorf("a node named as this leg's own start terminal was still offered as an interior seam\n%s", describeSymmetricSnap(rep))
	}

	_, outSeams, rep = runSnapProducer(t, ctx, d, pts, nil, nil, &marker)
	if len(rep.Candidates) != 0 || len(outSeams) != 0 {
		t.Errorf("a node named as this leg's own end terminal was still offered as an interior seam\n%s", describeSymmetricSnap(rep))
	}
}

// TestSymmetricSnap_AlignSeamIndicesAreRemapped. legSeam.Idx is a VERTEX index,
// so inserting a vertex renumbers everything after it. THIS producer does the
// remapping — applyLegBoundaryWarp no longer remaps anything at all
// (its gap-collapse is gone), so an unremapped align seam would silently
// point at the wrong vertex all the way to the writer.
func TestSymmetricSnap_AlignSeamIndicesAreRemapped(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// One insertion BEFORE the align seam and one AFTER it, so a remap that is
	// applied unconditionally is as wrong as one that is not applied at all.
	before := snapMarker(t, ctx, d, "before", 100, EndpointWarpRadiusM*0.7)
	after := snapMarker(t, ctx, d, "after", 900, EndpointWarpRadiusM*0.7)
	seamNode := snapMarker(t, ctx, d, "seamnode", 500, 300)

	pts := snapStraightLeg()
	align := []legSeam{{Idx: 2, Node: seamNode, HasNode: true, Adopted: true,
		NodeLat: pts[2].Lat, NodeLon: pts[2].Lon}}

	outPts, outSeams, rep := runSnapProducer(t, ctx, d, pts, align, nil, nil)

	if rep.Adopted() != 2 {
		t.Fatalf("adopted %d, want 2\n%s", rep.Adopted(), describeSymmetricSnap(rep))
	}
	if len(outPts) != len(pts)+2 {
		t.Fatalf("leg has %d point(s), want %d", len(outPts), len(pts)+2)
	}
	if len(outSeams) != 3 {
		t.Fatalf("produced %d seam(s), want 3\n%s", len(outSeams), describeSymmetricSnap(rep))
	}

	// Ascending, strictly.
	for i := 1; i < len(outSeams); i++ {
		if outSeams[i].Idx <= outSeams[i-1].Idx {
			t.Fatalf("seams out of order: %d then %d", outSeams[i-1].Idx, outSeams[i].Idx)
		}
	}

	var alignSeam *legSeam
	for i := range outSeams {
		if outSeams[i].Node == seamNode {
			alignSeam = &outSeams[i]
		}
	}
	if alignSeam == nil {
		t.Fatalf("the align seam is gone from the merged list")
	}
	// One vertex was inserted before it (x=100, inside segment 0), so index 2
	// became index 3.
	if alignSeam.Idx != 3 {
		t.Errorf("align seam at index %d, want 3 after one insertion ahead of it", alignSeam.Idx)
	}
	// AND it still points at its own coordinate. The index is only right if it
	// still names the same physical vertex.
	if outPts[alignSeam.Idx].Lat != pts[2].Lat || outPts[alignSeam.Idx].Lon != pts[2].Lon {
		t.Errorf("the remapped align seam names a DIFFERENT vertex — index %d is (%v,%v), the seam's own point was (%v,%v)",
			alignSeam.Idx, outPts[alignSeam.Idx].Lat, outPts[alignSeam.Idx].Lon, pts[2].Lat, pts[2].Lon)
	}
	// The align seam's other fields survive the merge untouched.
	if !alignSeam.HasNode || !alignSeam.Adopted || alignSeam.Node != seamNode {
		t.Errorf("the align seam's own fields changed in the merge: %+v", *alignSeam)
	}
	// And the two new seams sit either side of it.
	if outSeams[0].Node != before || outSeams[2].Node != after {
		t.Errorf("merged order is %d,%d,%d — want before(%d), align(%d), after(%d)",
			outSeams[0].Node, outSeams[1].Node, outSeams[2].Node, before, seamNode, after)
	}
}

// ── THE ACCOUNTING CLAIM ────────────────────────────────────────────────────

// TestSymmetricSnap_NodeIdentityHoldsAcrossAnAdoptedSeam is the claim the whole
// cost of the symmetric snap rests on, red-proofed rather than assumed.
//
//	Nodes == 2×SourceLegs − AdoptedTerminals + AlignCuts + TJunctionCuts
//	         + legMintedSeamNodes + RingSplits − RingMergedTerminals
//
// Every term counts a node CREATED or a creation SUPPRESSED. An adopted seam
// creates nothing, so the identity must hold with every term unchanged. If it
// does not, the snap costs a node per adoption.
//
// The leg is driven through the REST of the real commit path — the producer,
// then applyLegBoundaryWarp, then commitLegPieces — because the claim is
// about what reaches the canon, not about what the producer returns.
func TestSymmetricSnap_NodeIdentityHoldsAcrossAnAdoptedSeam(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 600, EndpointWarpRadiusM*0.7)

	before := censusNetwork(t, ctx, d)
	degBefore := nodeDegree(t, ctx, d, marker)

	pts := snapStraightLeg()

	outPts, seams, rep, err := legSeamsFromNearbyNodes(ctx, d, pts, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("legSeamsFromNearbyNodesTx: %v", err)
	}
	if rep.Adopted() != 1 {
		t.Fatalf("adopted %d, want 1 — nothing is being proved otherwise\n%s", rep.Adopted(), describeSymmetricSnap(rep))
	}
	// THE TERM THAT COULD HAVE MOVED. legMintedSeamNodes counts only seams
	// with !HasNode; every seam this producer makes has HasNode true, so it
	// contributes zero by construction rather than by arithmetic luck.
	if got := legMintedSeamNodes(outPts, seams); got != 0 {
		t.Fatalf("legMintedSeamNodes = %d, want 0 — an adopted seam must mint nothing, and the identity's own term says so", got)
	}

	warped, seams, err := applyLegBoundaryWarp(outPts, seams)
	if err != nil {
		t.Fatalf("applyLegBoundaryWarp: %v", err)
	}
	pieces, _, ringSplits, ringMergedTerminals, _, _, _, _, _, err := commitLegPieces(ctx, d, warped, seams, nil, nil, nil)
	if err != nil {
		t.Fatalf("commitLegPiecesTx: %v", err)
	}

	if len(pieces) != 2 {
		t.Fatalf("committed %d piece(s), want 2 — the leg is cut at the adopted seam", len(pieces))
	}
	if ringSplits != 0 || ringMergedTerminals != 0 {
		t.Fatalf("ringSplits=%d ringMergedTerminals=%d, want 0 and 0", ringSplits, ringMergedTerminals)
	}

	after := censusNetwork(t, ctx, d)

	// ONE source leg, both terminals minted fresh (nothing was near them),
	// no align cut, no T-junction cut, no leg-minted seam node, no ring.
	const wantNew = 2*1 - 0 + 0 + 0 + 0 + 0 - 0
	gained := after.Nodes - before.Nodes
	if got := gained; got != wantNew {
		t.Errorf("the network gained %d node(s), want %d\n"+
			"  Nodes == 2×SourceLegs − AdoptedTerminals + AlignCuts + TJunctionCuts + LegMintedSeamNodes + RingSplits − RingMergedTerminals\n"+
			"  == 2×1 − 0 + 0 + 0 + 0 + 0 − 0 == %d\n"+
			"A THIRD node means the adopted seam MINTED instead of adopting, and phase 1 needs the eighth accounting term the plan says it does not.",
			got, wantNew, wantNew)
	}
	// Positively: the seam is SHARED. The adopted node now carries two more
	// arc ends than it did, which is what "the leg joins the network here"
	// means — a node count that happens to match while the leg terminated on
	// a fresh node beside it would pass the subtraction above.
	if got := nodeDegree(t, ctx, d, marker) - degBefore; got != 2 {
		t.Errorf("the adopted node's degree rose by %d, want 2 — the leg passes THROUGH it, so both of its pieces must terminate on it", got)
	}

	// ── THE CONTROL ARM: A FROZEN WRONG IMPLEMENTATION ──────────────────────
	//
	// The subtraction above is only evidence if it can come out DIFFERENTLY.
	// Crippling the producer to prove that does not reach it: legMintedSeamNodes
	// is checked first and fires before the commit ever runs. So the wrong
	// implementation is frozen here instead and driven through the SAME commit
	// path — a seam at the same point that MINTS its own node rather than
	// adopting canon's, which is exactly what a private seam is and exactly
	// what the snap would be if the adoption did not take.
	//
	// It must gain THREE nodes: two terminals plus the seam's own mint. If it
	// gains two, the node count cannot tell adoption from minting and the
	// assertion above is measuring nothing.
	t.Run("control: a seam that mints instead of adopting gains one more node", func(t *testing.T) {
		cd := emptyCanon(t)
		snapMarker(t, ctx, cd, "marker", 600, EndpointWarpRadiusM*0.7)
		cBefore := censusNetwork(t, ctx, cd)

		cPts, cSeams, cRep, err := legSeamsFromNearbyNodes(ctx, cd, snapStraightLeg(), nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("legSeamsFromNearbyNodesTx: %v", err)
		}
		if cRep.Adopted() != 1 {
			t.Fatalf("control fixture drifted: adopted %d, want 1", cRep.Adopted())
		}
		// THE FREEZE: same index, same geometry, but the seam claims no canon
		// node — so commitLegPieces mints one of the leg's own there.
		wrong := []legSeam{{Idx: cSeams[0].Idx, HasNode: false}}
		if got := legMintedSeamNodes(cPts, wrong); got != 1 {
			t.Fatalf("the frozen wrong seam does not read as a mint (legMintedSeamNodes = %d) — it is not the implementation this arm exists to contrast with", got)
		}
		cWarped, wrong, err := applyLegBoundaryWarp(cPts, wrong)
		if err != nil {
			t.Fatalf("applyLegBoundaryWarp: %v", err)
		}
		if _, _, _, _, _, _, _, _, _, err := commitLegPieces(ctx, cd, cWarped, wrong, nil, nil, nil); err != nil {
			t.Fatalf("commitLegPiecesTx: %v", err)
		}

		cAfter := censusNetwork(t, ctx, cd)
		if got := cAfter.Nodes - cBefore.Nodes; got != wantNew+1 {
			t.Fatalf("the minting arm gained %d node(s), want %d — if it gains the same %d the adopting arm did, the node count cannot tell adoption from minting and this test's headline assertion is measuring nothing",
				got, wantNew+1, gained)
		}
	})
}

// ── THE AUDIT FINDING, RED-PROOFED ──────────────────────────────────────────

// TestSymmetricSnap_AbsorbedIsWhatMakesTheLegReach is the cripple for the audit
// finding. applyLegBoundaryWarp targets a seam's NODE coordinates only when
// Absorbed; every other seam gets a zero-displacement anchor at its own current
// point. So a seam marked merely Adopted commits a line that never touches the
// junction it claims.
//
// Run twice on the same seam — as produced, and with Absorbed forced false.
func TestSymmetricSnap_AbsorbedIsWhatMakesTheLegReach(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 600, EndpointWarpRadiusM*0.7)
	n, _, err := d.Node(ctx, marker)
	if err != nil {
		t.Fatalf("load marker: %v", err)
	}

	pts := snapStraightLeg()
	outPts, seams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)
	if len(seams) != 1 {
		t.Fatalf("produced %d seam(s), want 1\n%s", len(seams), describeSymmetricSnap(rep))
	}
	idx := seams[0].Idx

	// AS PRODUCED: the leg reaches the node, bit-exact.
	warped, _, err := applyLegBoundaryWarp(outPts, seams)
	if err != nil {
		t.Fatalf("applyLegBoundaryWarp: %v", err)
	}
	if warped[idx].Lat != n.Lat || warped[idx].Lon != n.Lon {
		t.Errorf("the warped leg does not reach its node: point %d is (%v,%v), the node is (%v,%v)",
			idx, warped[idx].Lat, warped[idx].Lon, n.Lat, n.Lon)
	}

	// CRIPPLED: Absorbed off, everything else identical.
	crippled := make([]legSeam, len(seams))
	copy(crippled, seams)
	crippled[0].Absorbed = false
	crippledPts, _, err := applyLegBoundaryWarp(outPts, crippled)
	if err != nil {
		t.Fatalf("applyLegBoundaryWarp (crippled): %v", err)
	}
	if crippledPts[idx].Lat == n.Lat && crippledPts[idx].Lon == n.Lon {
		t.Fatalf("with Absorbed=false the leg STILL reaches the node — applyLegBoundaryWarp no longer gates on Absorbed, so setting it is no longer what makes this work and this test is proving nothing")
	}
	gap := HaversineMetres(crippledPts[idx].Lat, crippledPts[idx].Lon, n.Lat, n.Lon)
	if gap < EndpointWarpRadiusM*0.5 {
		t.Errorf("crippled gap is only %.3f m — expected the leg to stay roughly its full distance (%.3f m) from the node", gap, EndpointWarpRadiusM*0.7)
	}
}

// ── ORDER AND DETERMINISM ───────────────────────────────────────────────────

// TestSymmetricSnap_TwoNodesAdoptedInAlongLegOrder. The search's own order is
// ascending PointIdx — the nearest VERTEX — which is not the same question as
// where the foot of the perpendicular fell. Seams must come out in the order
// the commit will walk them.
func TestSymmetricSnap_TwoNodesAdoptedInAlongLegOrder(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	east := snapMarker(t, ctx, d, "east", 800, EndpointWarpRadiusM*0.7)
	west := snapMarker(t, ctx, d, "west", 200, EndpointWarpRadiusM*0.7)

	pts := snapStraightLeg()
	outPts, seams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if len(seams) != 2 {
		t.Fatalf("produced %d seam(s), want 2\n%s", len(seams), describeSymmetricSnap(rep))
	}
	if seams[0].Node != west || seams[1].Node != east {
		t.Errorf("seams are %d then %d, want west(%d) then east(%d) — leg order, not id or discovery order",
			seams[0].Node, seams[1].Node, west, east)
	}
	if seams[0].Idx >= seams[1].Idx {
		t.Errorf("seam indices %d, %d are not ascending", seams[0].Idx, seams[1].Idx)
	}
	if len(outPts) != len(pts)+2 {
		t.Errorf("leg has %d point(s), want %d", len(outPts), len(pts)+2)
	}
}

// TestSymmetricSnap_NoNodesInRangeChangesNothing. An empty answer is an answer.
func TestSymmetricSnap_NoNodesInRangeChangesNothing(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Well outside the radius.
	snapMarker(t, ctx, d, "marker", 600, EndpointWarpRadiusM*3)

	pts := snapStraightLeg()
	outPts, seams, rep := runSnapProducer(t, ctx, d, pts, nil, nil, nil)

	if len(rep.Candidates) != 0 || len(seams) != 0 {
		t.Errorf("a node %v m away was offered\n%s", EndpointWarpRadiusM*3, describeSymmetricSnap(rep))
	}
	if len(outPts) != len(pts) {
		t.Errorf("leg has %d point(s), want %d unchanged", len(outPts), len(pts))
	}
	if rep.Skipped {
		t.Errorf("report says Skipped — the leg WAS asked, and found nothing")
	}
}

// TestSymmetricSnap_IsWiredBetweenAlignAndTheWarp. AddLeg calls the producer, and
// WHERE it sits is the claim worth holding: after LegSeamsFromAlign, because
// the align seams are its exclusion set and its stub-rule boundaries; before
// applyLegBoundaryWarp, because the seams it makes are Absorbed and it is the
// warp that moves the leg onto their nodes. Either swap is silent — the import
// still succeeds, it just adopts nothing useful or never reaches what it
// adopted.
//
// A source scan, with the limits source scans have: it proves the ORDER of three call sites in one file,
// which is exactly the claim, and nothing about what they do. It self-tests by
// finding all three first, so a rename cannot turn it into a check of nothing.
// addLegSourcePath is AddLeg's source.
const addLegSourcePath = "addleg.go"

func TestSymmetricSnap_IsWiredBetweenAlignAndTheWarp(t *testing.T) {
	body := snapReadPackageFile(t, addLegSourcePath)
	align := strings.Index(body, "seams, err := legSeamsFromAlign(")
	snap := strings.Index(body, "err = legSeamsFromNearbyNodes(")
	warp := strings.Index(body, "seams, err = applyLegBoundaryWarp(")
	if align < 0 || snap < 0 || warp < 0 {
		t.Fatalf("self-test: one of the three call sites is not in %s (align=%d snap=%d warp=%d) — this scan would pass on a rename", addLegSourcePath, align, snap, warp)
	}
	if !(align < snap && snap < warp) {
		t.Errorf("the symmetric-snap producer is not between legSeamsFromAlign and applyLegBoundaryWarp (align=%d snap=%d warp=%d)", align, snap, warp)
	}
}

// snapReadPackageFile reads a file in this package's own directory. Fails loud
// on a read error: a scan that cannot read its subject must not report "not
// found".
func snapReadPackageFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// ── THE UNIT, ON A HAND-BUILT LEG ───────────────────────────────────────────

// TestInteriorNodeSeam_ABitwiseVertexIsCutAt is the rule at its simplest: a leg
// walking straight through an existing node's EXACT coordinate is cut there.
//
// The node is planted with [snapMarker], whose south terminal commits at
// mPt(x, y) verbatim, and the leg is given that same mPt(x, y) as an interior
// vertex — so the coincidence is bitwise by construction rather than by a
// coordinate typed twice and hoped to be equal.
func TestInteriorNodeSeam_ABitwiseVertexIsCutAt(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 500, 0)

	pts := []Point{mPt(0, 0), mPt(250, 0), mPt(500, 0), mPt(750, 0), mPt(1000, 0)}
	seams, rep := runInteriorNodeProducer(t, ctx, d, pts, nil, nil, nil)

	if rep.Accepted() != 1 {
		t.Fatalf("accepted %d candidate(s), want 1: %+v", rep.Accepted(), rep.Candidates)
	}
	if len(seams) != 1 {
		t.Fatalf("produced %d seam(s), want 1: %+v", len(seams), seams)
	}
	s := seams[0]
	if s.Idx != 2 {
		t.Errorf("seam at point %d, want 2 — the vertex that IS the node's coordinate", s.Idx)
	}
	if !s.HasNode || s.Node != marker {
		t.Errorf("seam claims node %d (HasNode=%v), want the marker %d", s.Node, s.HasNode, marker)
	}
	if !s.Adopted {
		t.Errorf("Adopted=false, but the node existed before this leg — this rule mints nothing")
	}
	// ⚠ ABSORBED MUST BE FALSE, and it is the opposite of the symmetric snap's
	// requirement for a reason. applyLegBoundaryWarp moves the leg onto a
	// seam's node only when Absorbed; here the leg is ALREADY on it, bitwise,
	// so asking for a taper would move geometry that is already correct.
	if s.Absorbed || s.AbsorbedM != 0 {
		t.Errorf("Absorbed=%v AbsorbedM=%v, want false/0 — the leg already stands on the node, so there is nothing to reach",
			s.Absorbed, s.AbsorbedM)
	}
	if s.NodeLat != pts[2].Lat || s.NodeLon != pts[2].Lon {
		t.Errorf("seam node coords (%v,%v) are not the vertex's own (%v,%v) — they are the same point bitwise or the rule did not fire on identity",
			s.NodeLat, s.NodeLon, pts[2].Lat, pts[2].Lon)
	}
}

// TestInteriorNodeSeam_AStationaryRunIsOneCrossing is guard (1) from the
// rule's commissioning: on real data, arcs stood on one node for 7 and 20
// consecutive samples; a seam per sample would shatter them into 8 and 21
// pieces, all but two of zero length, which storage rejects outright.
func TestInteriorNodeSeam_AStationaryRunIsOneCrossing(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 500, 0)

	stood := mPt(500, 0)
	pts := []Point{mPt(0, 0), mPt(250, 0)}
	for i := 0; i < 7; i++ {
		pts = append(pts, stood)
	}
	pts = append(pts, mPt(750, 0), mPt(1000, 0))

	seams, rep := runInteriorNodeProducer(t, ctx, d, pts, nil, nil, nil)
	if len(seams) != 1 {
		t.Fatalf("produced %d seam(s) for one stationary crossing, want 1: %+v", len(seams), seams)
	}
	if seams[0].Idx != 2 || seams[0].Node != marker {
		t.Errorf("seam at point %d on node %d, want point 2 (the FIRST vertex of the run) on node %d",
			seams[0].Idx, seams[0].Node, marker)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Visits != 7 {
		t.Errorf("report says %d candidate(s) with %v visit(s), want 1 candidate with 7 — the run must be recognised as one crossing, not counted away",
			len(rep.Candidates), rep.Candidates)
	}
}

// TestInteriorNodeSeam_ARevisitIsDeclined is the other half of guard (1): a leg
// that touches the node's coordinate, leaves, and comes back is NOT a
// stationary run. One seam would leave the second visit still running through
// the node; one seam per visit makes a piece that begins and ends on the same
// node. Neither is this rule's to decide, so it refuses and says so.
func TestInteriorNodeSeam_ARevisitIsDeclined(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	snapMarker(t, ctx, d, "marker", 500, 0)

	stood := mPt(500, 0)
	pts := []Point{mPt(0, 0), stood, mPt(500, 300), stood, mPt(1000, 0)}
	seams, rep := runInteriorNodeProducer(t, ctx, d, pts, nil, nil, nil)
	if len(seams) != 0 {
		t.Errorf("produced %d seam(s) for a leg that visits the node twice, want 0: %+v", len(seams), seams)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Decline != InteriorNodeDeclineRevisited {
		t.Errorf("report %+v, want one candidate declined %q", rep.Candidates, InteriorNodeDeclineRevisited)
	}
}

// TestInteriorNodeSeam_ATerminalVertexIsNotNoded pins the terminal rule here: a
// leg's own first and last vertices are lat-lon anchors and are never cut,
// whoever is standing there.
func TestInteriorNodeSeam_ATerminalVertexIsNotNoded(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	snapMarker(t, ctx, d, "marker", 0, 0)

	pts := []Point{mPt(0, 0), mPt(250, 0), mPt(500, 0)}
	seams, rep := runInteriorNodeProducer(t, ctx, d, pts, nil, nil, nil)
	if len(seams) != 0 {
		t.Errorf("produced %d seam(s) at the leg's own start anchor, want 0: %+v", len(seams), seams)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Decline != InteriorNodeDeclineTerminal {
		t.Errorf("report %+v, want one candidate declined %q", rep.Candidates, InteriorNodeDeclineTerminal)
	}
}

// TestInteriorNodeSeam_AnAlreadyClaimedNodeIsExcluded pins the exclusion. A
// node an align seam already names must not get a second seam — the same rule
// [legSeamsFromNearbyNodes] applies, and applied BEFORE the search rather
// than as a post-hoc discard.
func TestInteriorNodeSeam_AnAlreadyClaimedNodeIsExcluded(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	marker := snapMarker(t, ctx, d, "marker", 500, 0)

	pts := []Point{mPt(0, 0), mPt(250, 0), mPt(500, 0), mPt(750, 0), mPt(1000, 0)}
	existing := []legSeam{{Idx: 3, Node: marker, HasNode: true, Adopted: true}}
	seams, rep := runInteriorNodeProducer(t, ctx, d, pts, existing, nil, nil)
	if len(seams) != 1 || seams[0].Idx != 3 {
		t.Errorf("seams %+v, want only the align seam at point 3 — the node it names must not be claimed twice", seams)
	}
	// ⚠ DECLINED AND REPORTED, not filtered out of the search: a rule that
	// cannot say which nodes it refused is one nobody can measure. See
	// [InteriorNodeDeclineAlreadySeamed].
	if len(rep.Candidates) != 1 || rep.Candidates[0].Decline != InteriorNodeDeclineAlreadySeamed {
		t.Errorf("report %+v, want one candidate declined %q", rep.Candidates, InteriorNodeDeclineAlreadySeamed)
	}
}
