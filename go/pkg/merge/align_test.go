package merge

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

// alignFixture is one committed canon arc plus its measured measure length.
type alignFixture struct {
	d     *MemCanon
	arcID ArcID
	total float64
}

// alignCanon commits one straight west→east canon arc per span, each its own
// leg, and returns the canon, the arcs in id order and their measure lengths.
//
// Each is a separate leg, so each is its own arc with its own nodes — which
// is what makes the three-arc crossing case a genuine chain rather than one
// long arc.
func alignCanon(t *testing.T, ctx context.Context, label string, spans ...[2]float64) (*MemCanon, []ArcID, []float64) {
	t.Helper()
	d := emptyCanon(t)
	for i, s := range spans {
		addTestLeg(t, ctx, d, fmt.Sprintf("canon-%d", i), []Point{mPt(s[0], 0), mPt(s[1], 0)})
	}
	committed := committedArcs(t, ctx, d)
	if len(committed) != len(spans) {
		t.Fatalf("%s: committed %d arcs, want %d", label, len(committed), len(spans))
	}
	ids := make([]ArcID, len(committed))
	totals := make([]float64, len(committed))
	for i, c := range committed {
		ids[i] = c.ID
		totals[i] = CanonMeasureLengthM(c.Vertices)
	}
	return d, ids, totals
}

// alignOneCanon is the single-arc case, which most of the table wants.
func alignOneCanon(t *testing.T, ctx context.Context, label string, lenM float64) alignFixture {
	t.Helper()
	d, ids, totals := alignCanon(t, ctx, label, [2]float64{0, lenM})
	return alignFixture{d: d, arcID: ids[0], total: totals[0]}
}

// runAlign applies the align step to runs and fails the test on an error.
func runAlign(t *testing.T, ctx context.Context, d *MemCanon, runs []FollowRun) AlignReport {
	t.Helper()
	rep, err := SplitCanonAtRunBoundaries(ctx, d, runs, nil, nil)
	if err != nil {
		t.Fatalf("SplitCanonAtRunBoundaries: %v", err)
	}
	return rep
}

func onRun(arcID ArcID, measureStart, measureEnd float64, reversed bool) FollowRun {
	return FollowRun{Kind: RunOn, CanonID: arcID, MeasureStart: measureStart, MeasureEnd: measureEnd, Reversed: reversed}
}

// alignCountArcs is how many arcs the canon holds — the number the align
// step moves by exactly its cut count.
func alignCountArcs(t *testing.T, ctx context.Context, d *MemCanon) int {
	t.Helper()
	return len(committedArcs(t, ctx, d))
}

// describeAlign renders a report for a failure message.
func describeAlign(rep AlignReport) string {
	out := fmt.Sprintf("%d cut(s) over %d boundaries:", rep.Cuts, len(rep.Boundaries))
	for _, b := range rep.Boundaries {
		out += fmt.Sprintf("\n  run arc %d, parent %d, measure %.6f of %.6f → %s", b.RunCanonID, b.Parent, b.MeasureM, b.TotalM, b.Decline)
	}
	return out
}

// alignCrossFixture is two independent canon arcs crossing at one interior
// point each: arcA east-west, arcB north-south, centred on (500, 0).
func alignCrossFixture(t *testing.T, ctx context.Context, label string) (d *MemCanon, arcA, arcB ArcID, totalA, totalB float64) {
	t.Helper()
	dd := emptyCanon(t)
	addTestLeg(t, ctx, dd, "arcA", []Point{mPt(0, 0), mPt(1000, 0)})
	addTestLeg(t, ctx, dd, "arcB", []Point{mPt(500, -100), mPt(500, 100)})
	committed := committedArcs(t, ctx, dd)
	if len(committed) != 2 {
		t.Fatalf("%s: committed %d arcs, want 2 — arcA and arcB must land as two independent, uncut arcs for this fixture to isolate the align step", label, len(committed))
	}
	// mPt's xM/yM are calibrated metres but haversine over the resulting lat/lon
	// is not bit-exact to them (measured: a "1000 m" span comes back ~998.9 m) —
	// so pick arcA/arcB by which is longer, not by an exact length match.
	c0, c1 := committed[0], committed[1]
	len0 := CanonMeasureLengthM(c0.Vertices)
	len1 := CanonMeasureLengthM(c1.Vertices)
	if len0 < len1 {
		c0, c1 = c1, c0
		len0, len1 = len1, len0
	}
	if len0 < 900 || len0 > 1100 || len1 < 150 || len1 > 250 {
		t.Fatalf("%s: expected one ~1000 m arc (arcA) and one ~200 m arc (arcB), got %v and %v", label, len0, len1)
	}
	return dd, c0.ID, c1.ID, len0, len1
}

// assertNoCoincidentNodes fails loud on any two nodes of d sharing an exact
// lat/lon.
func assertNoCoincidentNodes(t *testing.T, ctx context.Context, d *MemCanon) {
	t.Helper()
	nodes, err := d.NodesInBox(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("assertNoCoincidentNodes: NodesInBox: %v", err)
	}
	byCoord := map[[2]float64][]NodeID{}
	for _, n := range nodes {
		k := [2]float64{n.Lat, n.Lon}
		byCoord[k] = append(byCoord[k], n.ID)
	}
	for k, ids := range byCoord {
		if len(ids) > 1 {
			t.Errorf("coordinate (%v,%v) carries %d nodes %v, want 1", k[0], k[1], len(ids), ids)
		}
	}
}

// ---- the acceptance table -------------------------------------------------

// TestAlign_PartOfOneArcCutsTwice — a run riding the middle of one canon
// arc needs a node at each end of the stretch it rides, so one arc
// becomes three.
func TestAlign_PartOfOneArcCutsTwice(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-part-of-one", 600)

	rep := runAlign(t, ctx, f.d, []FollowRun{onRun(f.arcID, 200, 400, false)})

	if rep.Cuts != 2 {
		t.Fatalf("cuts = %d, want 2 (%s)", rep.Cuts, describeAlign(rep))
	}
	if got := alignCountArcs(t, ctx, f.d); got != 3 {
		t.Errorf("network holds %d arcs, want 3 — one canon arc cut twice is three", got)
	}
	// The SECOND cut was resolved against the half the FIRST minted, not against
	// the id the run named. That is the re-resolve, and it is asserted rather than
	// assumed because getting it wrong targets a deleted row and the error would
	// name the wrong thing.
	if rep.Boundaries[1].Parent == rep.Boundaries[0].Parent {
		t.Errorf("both cuts resolved against arc %d — the second must be resolved against the prior half the first minted",
			rep.Boundaries[0].Parent)
	}
	if rep.Boundaries[1].Parent != rep.Boundaries[0].Prior {
		t.Errorf("the second cut resolved against %d, want the first cut's prior half %d",
			rep.Boundaries[1].Parent, rep.Boundaries[0].Prior)
	}
}

// TestAlign_EndToEndCutsNothing — the common case, and the one that makes A1
// idempotent: a run that rides a whole canon arc already has its nodes.
func TestAlign_EndToEndCutsNothing(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-end-to-end", 600)
	before := alignCountArcs(t, ctx, f.d)

	rep := runAlign(t, ctx, f.d, []FollowRun{onRun(f.arcID, 0, f.total, false)})

	if rep.Cuts != 0 {
		t.Fatalf("cuts = %d, want 0 (%s)", rep.Cuts, describeAlign(rep))
	}
	for i, b := range rep.Boundaries {
		if b.Decline != AlignDeclineAtNode {
			t.Errorf("boundary %d declined with %q, want %q", i, b.Decline, AlignDeclineAtNode)
		}
	}
	if got := alignCountArcs(t, ctx, f.d); got != before {
		t.Errorf("network holds %d arcs, want %d unchanged", got, before)
	}
}

// TestAlign_MeasureEpsQualifiesTheEnds asserts the DECLINE REASON, deliberately, and
// that is the whole design of this test.
//
// `t == 1` arithmetic reaches total through a + t*(b-a), which is not bitwise b,
// so a boundary a few ulps short of the end passes ValidateSnapFraction's strict
// (0,1) and would write a half-arc of order 1e-14 m that migration 050's
// self-loop CHECK cannot see — the two node ids are distinct.
//
// ⚠ BUT NO CUT HAPPENS EITHER WAY, and asserting the count would therefore be
// decorative. MinRunM (25 m) already refuses anything within nine orders of
// magnitude of an end, so removing the measureEps branch changes nothing an arc
// count can see — MEASURED, by removing it: `cuts = 0` still, table still green.
//
// What moves is the REASON, and the reason is load-bearing: "already at a node"
// is the state A1 exists to reach and "stub too short" is a compromise it
// accepted. L2 reconciles against this report and A2 consumes it. So the bar is
// the decline value, at a boundary inside measureEps where the two rules disagree
// about the name and agree about the outcome.
func TestAlign_MeasureEpsQualifiesTheEnds(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-measureeps", 600)
	before := alignCountArcs(t, ctx, f.d)

	// Strictly inside both ends, and strictly inside measureEps of them: a positive
	// measure that no geometry means anything at, which is exactly what the fraction
	// round trip produces at a boundary that is really an endpoint.
	const insideMeasureEps = measureEps / 2
	rep := runAlign(t, ctx, f.d, []FollowRun{
		onRun(f.arcID, insideMeasureEps, f.total-insideMeasureEps, false),
	})

	if rep.Cuts != 0 {
		t.Fatalf("cuts = %d, want 0 — a boundary within measureEps of an end is already at a node (%s)", rep.Cuts, describeAlign(rep))
	}
	if got := alignCountArcs(t, ctx, f.d); got != before {
		t.Errorf("network holds %d arcs, want %d — a sub-ulp half was written", got, before)
	}
	if len(rep.Boundaries) != 2 {
		t.Fatalf("considered %d boundaries, want 2", len(rep.Boundaries))
	}
	for i, b := range rep.Boundaries {
		if b.Decline != AlignDeclineAtNode {
			t.Errorf("the boundary at measure %.12g of %.6f was declined as %q, want %q. "+
				"It is %.12g m from an end — inside measureEps, so it IS that node. Reporting it as a short stub "+
				"reads a satisfied boundary as a compromise, and L2 reconciles against this report.",
				b.MeasureM, b.TotalM, b.Decline, AlignDeclineAtNode, math.Min(b.MeasureM, b.TotalM-b.MeasureM))
			_ = i
		}
	}
}

// TestAlign_NearBoundaryAbsorbsOntoTheAdjacentNode — a boundary inside
// EndpointWarpRadiusM of its parent's end does not cut and does not decline
// empty-handed: it NAMES that end's node, so the leg joins the junction that is
// already there instead of minting a private one (an earlier behaviour) or
// nothing at all (a later one, which left the ride resolution a branch it
// could not merge).
func TestAlign_NearBoundaryAbsorbsOntoTheAdjacentNode(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-absorb", 600)
	before := alignCountArcs(t, ctx, f.d)

	// The parent's start node, read BEFORE A1 runs: a cut DELETES the parent row
	// and mints two halves, so f.arcID is gone by the time the report is back —
	// but the prior half inherits this id, which is what the boundary absorbs to.
	startNode, _, err := f.d.ArcEnds(ctx, f.arcID)
	if err != nil {
		t.Fatalf("ArcEnds: %v", err)
	}

	// 5 m from the start node — well inside the 15 m displacement budget.
	const near = 5.0
	rep := runAlign(t, ctx, f.d, []FollowRun{onRun(f.arcID, near, 400, false)})

	if rep.Cuts != 1 {
		t.Fatalf("cuts = %d, want 1 — the far boundary qualifies, the near one absorbs (%s)", rep.Cuts, describeAlign(rep))
	}
	// Boundaries are cut high first, so index 0 is the far one.
	if rep.Boundaries[0].Decline != AlignCutMade {
		t.Errorf("the boundary at measure %.3f declined with %q, want a cut", rep.Boundaries[0].MeasureM, rep.Boundaries[0].Decline)
	}
	b := rep.Boundaries[1]
	if b.Decline != AlignDeclineAbsorbedToNode {
		t.Fatalf("the boundary at measure %.3f declined with %q, want %q (it is %.3f m from the start node, inside the %.0f m budget)",
			b.MeasureM, b.Decline, AlignDeclineAbsorbedToNode, near, EndpointWarpRadiusM)
	}
	// ⚠ NAMING THE NODE IS THE WHOLE FIX. A decline that resolves to node 0 is
	// what the leg once had to drop the seam over.
	if b.Node != startNode {
		t.Errorf("absorbed onto node %d, want the parent's start node %d", b.Node, startNode)
	}
	if node, ok := b.SeamNode(); !ok || node == 0 {
		t.Errorf("seamNode() = (%d,%v), want the absorbed node and true — A2 cannot join a seam with no node", node, ok)
	}
	if b.AbsorbedM <= 0 || b.AbsorbedM > EndpointWarpRadiusM {
		t.Errorf("AbsorbedM = %.3f, want the displacement in (0,%.0f]", b.AbsorbedM, EndpointWarpRadiusM)
	}
	if got := alignCountArcs(t, ctx, f.d); got != before+1 {
		t.Errorf("network holds %d arcs, want %d — absorbing must not cut", got, before+1)
	}
}

// TestAlign_StayBandDeclinesTheFarBoundaryCuts pins DECLINE 3's band, not the
// ruling it replaced.
//
// Both boundaries here — one 20 m from its end, one 200 m — cut. The StayM
// band (EndpointWarpRadiusM..MinRunM, 15-25 m) once had a decline of its own
// (DECLINE 3), because EndpointWarpRadiusM and the follow's StayM are the SAME
// 15 m: an entered run stays classified ON until it has drifted StayM past
// where it actually left its candidate, so a cut minted in that band nodes the
// point the matcher NOTICED the leg leave, not where it left. That decline is
// lifted, so both cut. The two are still asserted SEPARATELY rather than
// folded into a total, so a change reddens this on the boundary that actually
// moved.
//
// The lift is a trade, measured on real data: with the band decline restored,
// repeated imports of one batch made no align cuts and no cuts of earlier
// geometry; lifted, they made some of each, and the cuts decayed to zero
// within a few passes rather than re-rising. A green test here does not
// settle that trade.
func TestAlign_StayBandBothBoundariesCut(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-stayband", 600)
	before := alignCountArcs(t, ctx, f.d)

	const near = 20.0 // > EndpointWarpRadiusM (15), < MinRunM (25) — the StayM band: declines
	const far = 400.0 // 200 m from the nearer end — well past MinRunM: cuts
	rep := runAlign(t, ctx, f.d, []FollowRun{onRun(f.arcID, near, far, false)})

	if rep.Cuts != 2 {
		t.Fatalf("cuts = %d, want 2 — DECLINE 3 is lifted, so the near boundary cuts alongside the far one (%s)",
			rep.Cuts, describeAlign(rep))
	}
	if len(rep.Boundaries) != 2 {
		t.Fatalf("boundaries = %d, want 2 (%s)", len(rep.Boundaries), describeAlign(rep))
	}

	// DESCENDING cut order (align.go point 2, "Cutting the highest measure first"):
	// the far boundary (400) is resolved first, against the original 600 m
	// arc; the near boundary (20) is resolved second, against whichever
	// half now holds it.
	farB, nearB := rep.Boundaries[0], rep.Boundaries[1]
	if farB.Decline != AlignCutMade {
		t.Errorf("far boundary (measure %.3f of %.3f) resolved as %q, want a cut", farB.MeasureM, farB.TotalM, farB.Decline)
	}
	// The boundary that DECLINE 3 used to hold. Asserted as a cut, and its
	// measure asserted to still sit inside the band, so this test keeps naming
	// the case the rule was about: restore DECLINE 3 and it reddens here, on
	// the boundary that actually changed.
	if nearB.Decline != AlignCutMade {
		t.Errorf("near boundary (measure %.3f of %.3f) resolved as %q, want a cut — DECLINE 3 is lifted",
			nearB.MeasureM, nearB.TotalM, nearB.Decline)
	}
	if !(nearB.MeasureM > EndpointWarpRadiusM && nearB.MeasureM < liftedStubMinRunM) {
		t.Errorf("near boundary is at measure %.3f, outside the StayM band (%v..%v) — this fixture no longer poses DECLINE 3's question",
			nearB.MeasureM, EndpointWarpRadiusM, liftedStubMinRunM)
	}

	if got := alignCountArcs(t, ctx, f.d); got != before+2 {
		t.Errorf("network holds %d arcs, want %d — both boundaries should have cut", got, before+2)
	}
}

// TestAlign_ReversedRunCutsTheSamePlaces is AC #1's proof, and the single most
// likely way A1 silently does the wrong thing.
//
// FollowRun's measures are DIRECTIONAL: an anti-parallel run has MeasureStart > MeasureEnd.
// A predicate that does not order the pair first reads the high boundary against
// 0 and the low one against total. On real data over a third of ON runs are
// anti-parallel, so this is the common case, not the corner.
//
// The bar is EQUALITY WITH THE FORWARD TWIN, on two identical networks: same
// cuts, same measures, same resulting arc count.
//
// ⚠ HONEST ABOUT WHAT IT DOES NOT DO. It is a PROPERTY PIN, not a discriminator:
// crippling the measure ordering reddened nothing here, because A1 pools both measures
// per canon arc, sorts the pool, and qualifies each boundary symmetrically —
// so direction cannot reach the outcome. The helper that did the swapping was
// deleted on that measurement (see align.go's header, point 1). This stays
// because it is the thing that WOULD catch a rewrite back to a per-run predicate,
// which is the shape M1 uses and the shape the hazard is real in.
func TestAlign_ReversedRunCutsTheSamePlaces(t *testing.T) {
	ctx := context.Background()
	fwd := alignOneCanon(t, ctx, "align-forward", 600)
	rev := alignOneCanon(t, ctx, "align-reversed", 600)
	if fwd.total != rev.total {
		t.Fatalf("the two fixtures measure %v and %v — they must be identical for the comparison to mean anything", fwd.total, rev.total)
	}

	fwdRep := runAlign(t, ctx, fwd.d, []FollowRun{onRun(fwd.arcID, 200, 400, false)})
	revRep := runAlign(t, ctx, rev.d, []FollowRun{onRun(rev.arcID, 400, 200, true)})

	if fwdRep.Cuts != revRep.Cuts {
		t.Fatalf("forward cut %d time(s), reversed cut %d — an anti-parallel run rides the same stretch and needs the same nodes\n  forward:  %s\n  reversed: %s",
			fwdRep.Cuts, revRep.Cuts, describeAlign(fwdRep), describeAlign(revRep))
	}
	if len(fwdRep.Boundaries) != len(revRep.Boundaries) {
		t.Fatalf("forward considered %d boundaries, reversed %d", len(fwdRep.Boundaries), len(revRep.Boundaries))
	}
	for i := range fwdRep.Boundaries {
		f, r := fwdRep.Boundaries[i], revRep.Boundaries[i]
		if f.MeasureM != r.MeasureM {
			t.Errorf("boundary %d: forward at measure %.9f, reversed at %.9f — the measures were not ordered before use", i, f.MeasureM, r.MeasureM)
		}
		if f.Decline != r.Decline {
			t.Errorf("boundary %d: forward %q, reversed %q", i, f.Decline, r.Decline)
		}
	}
	if a, b := alignCountArcs(t, ctx, fwd.d), alignCountArcs(t, ctx, rev.d); a != b {
		t.Errorf("forward network holds %d arcs, reversed %d", a, b)
	}
}

// TestAlign_TwoRunsOnOneCanonArc — one leg, two disjoint ON runs bound to the
// same canon arc (follow.go's pass-5 re-bind produces exactly this).
//
// The hazard is that the second run names an id the first run's cut deleted.
// Boundaries are therefore collected per canon arc across the WHOLE leg
// before any cutting starts, and cut in descending measure order.
func TestAlign_TwoRunsOnOneCanonArc(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-two-runs", 1000)
	before := alignCountArcs(t, ctx, f.d)

	rep := runAlign(t, ctx, f.d, []FollowRun{
		onRun(f.arcID, 100, 300, false),
		onRun(f.arcID, 600, 800, false),
	})

	if rep.Cuts != 4 {
		t.Fatalf("cuts = %d, want 4 — two runs, two interior boundaries each (%s)", rep.Cuts, describeAlign(rep))
	}
	if got := alignCountArcs(t, ctx, f.d); got != before+4 {
		t.Errorf("network holds %d arcs, want %d", got, before+4)
	}
	// Descending measure order, and each cut resolved against the previous one's prior
	// half — the property that makes naming a deleted row impossible.
	for i := 1; i < len(rep.Boundaries); i++ {
		if !(rep.Boundaries[i].MeasureM < rep.Boundaries[i-1].MeasureM) {
			t.Errorf("boundary %d (measure %.3f) does not follow boundary %d (measure %.3f) in descending order",
				i, rep.Boundaries[i].MeasureM, i-1, rep.Boundaries[i-1].MeasureM)
		}
		if rep.Boundaries[i].Parent != rep.Boundaries[i-1].Prior {
			t.Errorf("boundary %d resolved against %d, want the previous cut's prior half %d",
				i, rep.Boundaries[i].Parent, rep.Boundaries[i-1].Prior)
		}
	}
}

// TestAlign_AcrossThreeCanonArcs — a leg crossing a chain of three cuts only
// at the boundaries that are not already nodes. The two interior joins already
// have nodes (they are where the three arcs meet), so only the two ends of
// the ridden stretch cut.
func TestAlign_AcrossThreeCanonArcs(t *testing.T) {
	ctx := context.Background()
	d, ids, totals := alignCanon(t, ctx, "align-three",
		[2]float64{0, 300}, [2]float64{300, 600}, [2]float64{600, 900})
	before := alignCountArcs(t, ctx, d)
	if before != 3 {
		t.Fatalf("fixture holds %d arcs, want 3", before)
	}

	// Rides the last 100 m of the first, all of the second, the first 100 m of
	// the third — the shape a leg crossing a chain produces.
	rep := runAlign(t, ctx, d, []FollowRun{
		onRun(ids[0], totals[0]-100, totals[0], false),
		onRun(ids[1], 0, totals[1], false),
		onRun(ids[2], 0, 100, false),
	})

	if rep.Cuts != 2 {
		t.Fatalf("cuts = %d, want 2 — the middle arc is ridden end to end and needs nothing (%s)", rep.Cuts, describeAlign(rep))
	}
	if got := alignCountArcs(t, ctx, d); got != before+2 {
		t.Errorf("network holds %d arcs, want %d", got, before+2)
	}
	// The middle arc must be untouched.
	for _, b := range rep.Boundaries {
		if b.RunCanonID == ids[1] && b.Decline != AlignDeclineAtNode {
			t.Errorf("the end-to-end middle arc %d was %q at measure %.3f — it already spans exactly what the run rides",
				ids[1], b.Decline, b.MeasureM)
		}
	}
}

// TestAlign_NoOnRunTouchesNothing — the all-OFF leg, which is most of them.
func TestAlign_NoOnRunTouchesNothing(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-all-off", 600)
	before := alignCountArcs(t, ctx, f.d)

	// Start/End omitted: the align step reads only
	// Kind/MeasureStart/MeasureEnd/CanonID.
	rep := runAlign(t, ctx, f.d, []FollowRun{
		{Kind: RunOff},
		{Kind: RunOff},
	})

	if rep.Cuts != 0 || len(rep.Boundaries) != 0 {
		t.Errorf("an all-OFF leg produced %d cut(s) over %d boundaries — there is no run riding canon", rep.Cuts, len(rep.Boundaries))
	}
	if got := alignCountArcs(t, ctx, f.d); got != before {
		t.Errorf("network holds %d arcs, want %d unchanged", got, before)
	}
}

// TestAlign_UnknownCanonArcIsAnError — no fallback. A run naming a row that
// is not there means the pass has lost track of what it cut, and there is no
// answer to decline to.
func TestAlign_UnknownCanonArcIsAnError(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-unknown", 600)

	_, err := SplitCanonAtRunBoundaries(ctx, f.d, []FollowRun{onRun(999999, 100, 200, false)}, nil, nil)
	if err == nil {
		t.Fatal("a run naming an arc that does not exist was accepted — the pass would then be cutting geometry it cannot see")
	}
}

// ---- A1b: the prior half is SHORTER than the measure it was cut at -------------

// TestAlign_BoundaryPastAPriorHalfIsAtItsNode is A1b's own instrument, so the fix
// does not rest on a randomised test noticing.
//
// ⚠ THE FIXTURE HAS TO BE CURVED. `splitPolylineAtFraction` places the node by
// LINEAR lat/lon interpolation at a parameter derived from a measure ratio, and
// lat/lon-linear is not measure-linear — so on a turning polyline the node lands
// SHORT of the measure it was asked for and the prior half measures less than that
// measure. On a straight line the round trip is exact and nothing reproduces. That is
// measured, not assumed: the straight-line version of this case declines cleanly
// and was the first thing that made the defect look like it did not exist.
//
// Two boundaries at the SAME measure on one arc is the shape that reaches it: the
// first cuts, and the second is then resolved against the half the first minted,
// at a measure that half no longer contains.
func TestAlign_BoundaryPastAPriorHalfIsAtItsNode(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// A zigzag: every other vertex 3 m north, so every edge turns.
	pts := make([]Point, 0, 61)
	for i := 0; i <= 60; i++ {
		y := 0.0
		if i%2 == 1 {
			y = 3
		}
		pts = append(pts, mPt(float64(i)*10, y))
	}
	addTestLeg(t, ctx, d, "canon", pts)
	c := committedArcs(t, ctx, d)
	if len(c) != 1 {
		t.Fatalf("fixture: %d arcs, want 1", len(c))
	}
	arcID := c[0].ID
	total := CanonMeasureLengthM(c[0].Vertices)

	// Well clear of both ends and of MinRun, so nothing else can decline it.
	measure := total / 2
	rep := runAlign(t, ctx, d, []FollowRun{
		onRun(arcID, measure, measure+80, false),
		onRun(arcID, measure, measure+80, false),
	})

	// The liveness assertion. If the fixture stops producing a boundary that
	// overshoots its prior half, this test proves nothing and must say so rather
	// than pass.
	overshot := false
	for _, b := range rep.Boundaries {
		if b.MeasureM > b.TotalM {
			overshot = true
			t.Logf("boundary at measure %.9f overshoots its parent %d (%.9f m) by %.3e m — the A1b case",
				b.MeasureM, b.Parent, b.TotalM, b.MeasureM-b.TotalM)
		}
	}
	if !overshot {
		t.Fatalf("fixture is DEAD: no boundary landed past the end of the half holding it, so this test cannot see A1b. %s",
			describeAlign(rep))
	}

	// The bar: it is classified as being AT THAT HALF'S NODE, and the import does
	// not abort. runAlign fatals on error, so reaching here is half the assertion.
	for _, b := range rep.Boundaries {
		if b.MeasureM > b.TotalM && b.Decline != AlignDeclineAtNode {
			t.Errorf("the overshooting boundary was %q, want %q — an measure at or past a row's end IS at that row's end node, "+
				"and treating it as out of range aborts the user's entire import (7 of 24 chaos seeds)",
				b.Decline, AlignDeclineAtNode)
		}
	}
}

// TestAlign_MeasureOutsideTheArcTheRunNamedStillFailsLoudly is the other half of
// the A1b fix, and the reason it is a RECLASSIFICATION rather than a tolerance.
//
// The range check did not disappear; it moved to where the question is
// well-posed — against the row the RUN NAMED, once, before any cutting has moved
// anything. A genuinely lost boundary must still abort rather than be quietly
// absorbed by "it must be at a node then".
func TestAlign_MeasureOutsideTheArcTheRunNamedStillFailsLoudly(t *testing.T) {
	ctx := context.Background()
	f := alignOneCanon(t, ctx, "align-a1b-loud", 600)

	// Twice the arc's length: not an overshoot of a half, a wrong answer.
	_, err := SplitCanonAtRunBoundaries(ctx, f.d, []FollowRun{
		onRun(f.arcID, 100, f.total*2, false),
	}, nil, nil)
	if err == nil {
		t.Fatal("an measure beyond the arc the run named was accepted — the A1b fix must reclassify an overshoot of a HALF, " +
			"not swallow a boundary that was never on the geometry it claims to have followed")
	}
	if !strings.Contains(err.Error(), "is outside canon arc") {
		t.Errorf("error %q does not name the out-of-range boundary", err)
	}
}

// TestAlign_DeclinesAtExistingNode is the headline Prove-It. arcA is cut at
// its own midpoint (measure 500, coordinate (500,0)) first, exactly as any
// ordinary align cut would be. arcB's OWN midpoint (measure 100 of its 200 m
// length) is the SAME coordinate (500,0) by construction. Without the
// existing-node decline, cutting arcB there mints a SECOND node 0.000 m from
// the first; with it, the boundary declines and names the node arcA's cut
// already minted.
func TestAlign_DeclinesAtExistingNode(t *testing.T) {
	ctx := context.Background()
	d, arcA, arcB, totalA, totalB := alignCrossFixture(t, ctx, "align-existing-node")

	// Cut arcA at its midpoint — an ordinary cut, no coincidence involved yet.
	repA := runAlign(t, ctx, d, []FollowRun{onRun(arcA, totalA/2, totalA/2, false)})
	if repA.Cuts != 1 {
		t.Fatalf("cutting arcA's midpoint: cuts = %d, want 1 (%s)", repA.Cuts, describeAlign(repA))
	}
	existingNode := repA.Boundaries[0].Node
	if existingNode == 0 {
		t.Fatalf("arcA's cut minted no node id")
	}

	// A run from arcB's own start (measure 0) to its midpoint (measure totalB/2, the
	// coincident coordinate) — two boundaries, deliberately DIFFERENT reasons:
	// the run's start end is at arcB's OWN end (ordinary DECLINE 1), and its
	// other end is where this test's claim lives.
	repB := runAlign(t, ctx, d, []FollowRun{onRun(arcB, 0, totalB/2, false)})
	if repB.Cuts != 0 {
		t.Fatalf("cutting arcB at the coincident coordinate: cuts = %d, want 0 — this should DECLINE onto the existing node, not mint a second one (%s)",
			repB.Cuts, describeAlign(repB))
	}
	if len(repB.Boundaries) != 2 {
		t.Fatalf("arcB boundary count = %d, want 2 (its own start end, and the coincident end)", len(repB.Boundaries))
	}
	// DESCENDING measure order (SplitCanonAtRunBoundaries, point 2 of align.go's
	// header): the higher measure (the coincident midpoint) resolves first.
	coincident, own := repB.Boundaries[0], repB.Boundaries[1]
	if own.Decline != AlignDeclineAtNode {
		t.Errorf("arcB's own-start boundary declined with %q, want %q", own.Decline, AlignDeclineAtNode)
	}
	if coincident.Decline != AlignDeclineAtExistingNode {
		t.Errorf("arcB's coincident boundary declined with %q, want %q", coincident.Decline, AlignDeclineAtExistingNode)
	}
	if coincident.Node != existingNode {
		t.Errorf("arcB's coincident boundary named node %d, want %d (the one arcA's cut minted at the same coordinate)", coincident.Node, existingNode)
	}

	// THE HEADLINE, asserted directly on committed rows rather than trusted
	// from the report: no two node rows anywhere in this network share a
	// coordinate.
	assertNoCoincidentNodes(t, ctx, d)
	// arcA's two halves + arcB UNCUT (both its boundaries declined) = 3.
	if got := alignCountArcs(t, ctx, d); got != 3 {
		t.Errorf("network holds %d arcs, want 3 (arcA's two halves + arcB left whole)", got)
	}
}

// TestAlign_StillCutsWhenNotExactlyCoincident is the companion arm of the
// existing-node decline, re-aimed when the one-node rule unified the radii:
// before, there were two (5 mm exact-coordinate, 15 m parent-scoped), and this
// test proved a half-metre gap fell between them and still cut. The rule
// deletes that gap on purpose — DECLINE 1b and DECLINE 2 are now one network-wide
// search at EndpointWarpRadiusM (15 m), so a half-metre boundary now
// correctly ABSORBS onto the existing node (see
// TestAlign_NearBoundaryAbsorbsOntoTheAdjacentNode for that case). What this
// test now proves is the same shape one radius further out: a boundary
// genuinely beyond EndpointWarpRadiusM from the existing node — 20 m, clear
// of both the 15 m absorb radius and the 15–25 m StayM band this fixture's
// geometry keeps it well away from — must still cut and mint its own node. A
// fix that widened the search past 15 m would satisfy a version of this test
// with a bigger number and be exactly the mistake
// terminal_cluster_spread_test.go's finding warns against.
func TestAlign_StillCutsWhenNotExactlyCoincident(t *testing.T) {
	ctx := context.Background()
	d, arcA, arcB, totalA, totalB := alignCrossFixture(t, ctx, "align-near-miss")

	repA := runAlign(t, ctx, d, []FollowRun{onRun(arcA, totalA/2, totalA/2, false)})
	if repA.Cuts != 1 {
		t.Fatalf("cutting arcA's midpoint: cuts = %d, want 1 (%s)", repA.Cuts, describeAlign(repA))
	}

	// arcB's boundary 20 m off its own midpoint — clear of EndpointWarpRadiusM
	// (15 m) from arcA's node, and clear of the StayM band too (arcB is ~200 m
	// long, so measure 120 is 80 m from either end).
	measure := totalB/2 + 20
	repB := runAlign(t, ctx, d, []FollowRun{onRun(arcB, measure, measure, false)})
	if repB.Cuts != 1 {
		t.Fatalf("cutting arcB 20 m from the existing node: cuts = %d, want 1 — a genuine miss must still cut (%s)", repB.Cuts, describeAlign(repB))
	}
	if got := repB.Boundaries[0].Decline; got != AlignCutMade {
		t.Errorf("arcB's boundary declined with %q, want a cut — 20 m is well outside endpointWarpRadiusM", got)
	}
	if repB.Boundaries[0].Node == repA.Boundaries[0].Node {
		t.Errorf("arcB's cut minted the SAME node id as arcA's, want a genuinely new one 20 m away")
	}
}

func TestAlign_LineageCutRecordsTheForeignNodeItRefused(t *testing.T) {
	ctx := context.Background()
	d, arcA, arcB, totalA, totalB := alignCrossFixture(t, ctx, "align-t2b-record")

	// CONTROL FIRST. arcA's midpoint cut has nothing within EndpointWarpRadiusM
	// to refuse, so it must record NOTHING — a field that filled on every cut
	// would exempt every pair in the network and the invariant would be
	// decorative.
	repA := runAlign(t, ctx, d, []FollowRun{onRun(arcA, totalA/2, totalA/2, false)})
	if repA.Cuts != 1 {
		t.Fatalf("cutting arcA's midpoint: cuts = %d, want 1 (%s)", repA.Cuts, describeAlign(repA))
	}
	if len(repA.LineageCutConnectorPairs) != 0 {
		t.Errorf("an ordinary cut with nothing nearby recorded %d T2b pair(s) %v, want none — it refused nothing",
			len(repA.LineageCutConnectorPairs), repA.LineageCutConnectorPairs)
	}
	refused := repA.Boundaries[0].Node
	if refused == 0 {
		t.Fatalf("arcA's cut minted no node id")
	}

	// THE SUBJECT. arcB's boundary at (500,5) is 5 m from the node arcA's cut
	// just minted — inside EndpointWarpRadiusM (15 m), so the pre-T2b DECLINE 2
	// would have absorbed onto it. It is on arc A, so it is not on arc B's own
	// ridden lineage, and T2b cuts instead.
	repB := runAlign(t, ctx, d, []FollowRun{onRun(arcB, totalB/2+5, totalB/2+5, false)})
	if repB.Cuts != 1 {
		t.Fatalf("cutting arcB 5 m from arcA's node: cuts = %d, want 1 — T2b must CUT, not absorb onto a foreign node (%s)", repB.Cuts, describeAlign(repB))
	}
	if got := repB.Boundaries[0].Decline; got != AlignCutMade {
		t.Fatalf("arcB's boundary declined with %q, want a cut", got)
	}
	minted := repB.Boundaries[0].Node

	want := [][2]NodeID{{minted, refused}}
	got := repB.LineageCutConnectorPairs
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("T2b recorded %v, want %v — the pair (minted node, the foreign node it refused)", got, want)
	}

	// THE PAIR IS A REAL VIOLATION OF THE INVARIANT IT EXEMPTS, measured, not
	// assumed: if these two nodes were NOT within EndpointWarpRadiusM the
	// exemption would be exempting nothing and this test would pass on a
	// record that never mattered.
	nodes, err := d.NodesInBox(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("NodesInBox: %v", err)
	}
	coord := map[NodeID][2]float64{}
	for _, n := range nodes {
		coord[n.ID] = [2]float64{n.Lat, n.Lon}
	}
	a, ok := coord[minted]
	if !ok {
		t.Fatalf("minted node %d is not in the network", minted)
	}
	b, ok := coord[refused]
	if !ok {
		t.Fatalf("refused node %d is not in the network", refused)
	}
	distM := HaversineMetres(a[0], a[1], b[0], b[1])
	if distM > EndpointWarpRadiusM {
		t.Errorf("minted node %d and refused node %d are %.3f m apart, beyond endpointWarpRadiusM (%v m) — this fixture no longer reaches the T2b case it grades",
			minted, refused, distM, EndpointWarpRadiusM)
	}

}
