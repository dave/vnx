package merge

import (
	"context"
	"testing"
)

// TestCanonCandidates_WindowStraddlingCandidateSurvives is the behavioural pin
// on the two-phase lookup, and it is the property everything downstream rests
// on: PHASE 1 scans the leg in windows of canonWindowPts (32) points and PHASE 2
// discards candidates whose bbox is beyond Stay of every leg point. Neither may
// drop a candidate the matcher could bind — if one does, the leg silently
// commits verbatim instead of following, with no error anywhere.
//
// The fixture makes both phases work for their answer. The leg is long enough to
// span several windows, and canon is a single arc running the whole length
// beneath it, so:
//
//   - it is returned by MANY windows (exercising the canonCache reuse path,
//     where anyMiss is false and the decode is skipped), and
//   - it must survive phase 2's bbox tighten, which sees one arc whose bbox
//     covers the entire leg.
//
// A far-away decoy arc in the same network pins the other direction: the
// lookup must not simply return everything the network owns.
func TestCanonCandidates_WindowStraddlingCandidateSurvives(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Canon: one long west→east arc. 400 m at 1 m densification spans well
	// beyond a single 32-point window.
	canonLeg := straightLegX(41, 400, 0)
	addTestLeg(t, ctx, d, "canon", canonLeg)

	// Decoy: same network, 5 km north — far beyond Stay of every leg point.
	decoy := straightLegX(11, 400, 5000)
	addTestLeg(t, ctx, d, "decoy", decoy)

	committed := committedArcs(t, ctx, d)
	if len(committed) != 2 {
		t.Fatalf("fixture: expected 2 committed arcs, got %d", len(committed))
	}
	canonArcID := committed[0].ID

	// The probe leg retraces canon 2 m to the north — inside Stay (15 m), so the
	// matcher could bind it, so the lookup MUST return it.
	probe := straightLegX(41, 400, 2)

	cands, err := canonCandidatesForLeg(ctx, d, probe, defaultFollowBands().StayM)
	if err != nil {
		t.Fatalf("canonCandidatesForLeg: %v", err)
	}

	var gotCanon, gotDecoy bool
	for _, c := range cands {
		switch c.ID {
		case canonArcID:
			gotCanon = true
		case committed[1].ID:
			gotDecoy = true
		}
	}
	if !gotCanon {
		t.Errorf("the coincident canon arc %d was NOT returned as a candidate "+
			"(got %d candidates). The leg spans multiple %d-point windows, so either "+
			"the windowed prefilter or the phase-2 bbox tighten dropped a candidate "+
			"the matcher could bind — that leg would silently commit verbatim.",
			canonArcID, len(cands), canonWindowPts)
	}
	if gotDecoy {
		t.Errorf("the 5 km-distant decoy arc %d was returned as a candidate; "+
			"phase 2's bbox tighten is not tightening, and the matcher pays "+
			"O(points x candidates x vertices) for it.", committed[1].ID)
	}

	// Ids must be ascending — every CanonIdx and id tie-break in the matcher is
	// deterministic only because of this.
	for i := 1; i < len(cands); i++ {
		if cands[i-1].ID >= cands[i].ID {
			t.Errorf("candidates are not sorted ascending by id: %d then %d at %d",
				cands[i-1].ID, cands[i].ID, i)
		}
	}
}

// TestCanonCandidates_EmptyNetworkReturnsNone pins the all-OFF entry condition:
// followLegAgainstCanon is called unconditionally, with no empty-canon special
// case, so an empty network must yield an empty candidate set rather than an
// error.
func TestCanonCandidates_EmptyNetworkReturnsNone(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	cands, err := canonCandidatesForLeg(ctx, d, straightLegX(41, 400, 0),
		defaultFollowBands().StayM)
	if err != nil {
		t.Fatalf("canonCandidatesForLeg on an empty network: %v", err)
	}
	if len(cands) != 0 {
		t.Errorf("empty network yielded %d candidates, want 0", len(cands))
	}
}

// TestCandidateDiscovery_LongSourceEdgeFindsAnArcNoVertexIsNear is the
// red-proof.
//
// The source leg is ONE 300 m edge: two points, at x = 0 and x = 300, both on
// y = 0. Canon is a 200 m arc running parallel 5 m to the north, from x = 50 to
// x = 250 — so it is:
//
//   - within R = 10 m of the source EDGE along its whole length, and
//   - 50 m in x from the nearer source VERTEX, five times R.
//
// The per-point tighten dropped it. The per-edge tighten keeps it, and the
// solver then binds it: 200 m at d = 5 clears the break-even length
// 2P / (1 - (d/R)^2) = 133 m, so adopting it is genuinely cheaper than
// staying OFF and this is not a candidate the solve would ignore anyway.
func TestCandidateDiscovery_LongSourceEdgeFindsAnArcNoVertexIsNear(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Canon: 200 m of arc, 5 m north of the source line, starting 50 m in.
	canonPts := make([]Point, 0, 21)
	for i := 0; i <= 20; i++ {
		canonPts = append(canonPts, mPt(50+float64(i)*10, 5))
	}
	addTestLeg(t, ctx, d, "canon", canonPts)

	committed := committedArcs(t, ctx, d)
	if len(committed) != 1 {
		t.Fatalf("fixture: expected 1 committed arc, got %d", len(committed))
	}
	wantArc := committed[0].ID

	// The source leg: one long edge, no vertex anywhere near the arc.
	leg := []Point{mPt(0, 0), mPt(300, 0)}

	cands, err := canonCandidatesForLeg(ctx, d, leg, CorridorRadiusM)
	if err != nil {
		t.Fatalf("canonCandidatesForLeg: %v", err)
	}
	var found bool
	for _, c := range cands {
		if c.ID == wantArc {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidate lookup returned %d arc(s), none of them arc %d — the 200 m arc running 5 m from this leg's only edge was dropped because neither of the leg's two vertices comes within %v m of it", len(cands), wantArc, CorridorRadiusM)
	}

	// Discovery is only half the claim: an arc kept but never bound would be
	// cost without benefit. The solver must actually ride it.
	runs, err := corridorFollowRuns(t.Context(), leg, cands, defaultFollowBands())
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	var onM float64
	for _, r := range runs {
		if r.Kind == RunOn && r.CanonID == wantArc {
			m := r.MeasureEnd - r.MeasureStart
			if m < 0 {
				m = -m
			}
			onM += m
		}
	}
	if onM <= 0 {
		t.Errorf("the arc is now discovered but the solve binds none of it — %d run(s), no ON run on arc %d", len(runs), wantArc)
	}
}

// TestCandidateDiscovery_ArcBesideTheWindowSeamIsFound is the second red-proof,
// for the PHASE-1 half of the same hole.
//
// Candidate lookup has two stages, and closing the per-edge tighten (stage 2)
// left stage 1 open. Phase 1 scans the leg in windows of canonWindowPts and
// asks the rtree for arcs near each window's rect, and that rect is the bbox of
// the window's POINTS padded by R. With half-open, non-overlapping windows the
// EDGE that straddles a seam — pts[31]-pts[32] — is in no window at all, so an
// arc beside its midpoint is outside every rect the rtree is ever asked about.
// The per-edge tighten cannot save it: the tighten filters what phase 1
// returned, and phase 1 never returned it.
//
// ⚠ THE TWO-POINT FIXTURE ABOVE CANNOT SEE THIS. It has one edge and one
// window, so its edge can never straddle a seam. A fix verified only against
// that fixture reports the exposure closed while one hole in 32 points remains
// open — which is exactly what happened, and what a review caught.
//
// The leg here is canonWindowPts+2 points, deliberately shaped so the ONLY long
// edge is the one across the seam at index 31-32.
func TestCandidateDiscovery_ArcBesideTheWindowSeamIsFound(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Points 0..31 packed into the first 31 m, then a 400 m jump to point 32,
	// then one more. So edge 31->32 spans x = 31..431 and straddles the window
	// boundary; every other edge is 1 m long.
	leg := make([]Point, 0, canonWindowPts+2)
	for i := 0; i < canonWindowPts; i++ {
		leg = append(leg, mPt(float64(i), 0))
	}
	leg = append(leg, mPt(431, 0))
	leg = append(leg, mPt(432, 0))
	if len(leg) != canonWindowPts+2 {
		t.Fatalf("fixture: leg has %d points, want %d", len(leg), canonWindowPts+2)
	}

	// Canon: 200 m of arc, 5 m north, sitting around the seam edge's middle.
	// Its bbox spans x = 150..350, so the nearest leg VERTEX is 119 m away
	// (x=31) — far outside any per-point rect — while the seam edge passes 5 m
	// from it along its whole length.
	//
	// ⚠ 200 m, NOT the 50 m this fixture was first drafted with. At d = 5 and
	// R = 10 the break-even length is 2P / (1 - (d/R)^2) = 133 m, so a 50 m arc
	// is one the solve would decline on price even after finding it, and the
	// BOUND half of the assertion below would have been asserting the solver's
	// entry fee rather than this commit's discovery fix.
	canonPts := make([]Point, 0, 21)
	for i := 0; i <= 20; i++ {
		canonPts = append(canonPts, mPt(150+float64(i)*10, 5))
	}
	addTestLeg(t, ctx, d, "canon", canonPts)

	committed := committedArcs(t, ctx, d)
	if len(committed) != 1 {
		t.Fatalf("fixture: expected 1 committed arc, got %d", len(committed))
	}
	wantArc := committed[0].ID

	cands, err := canonCandidatesForLeg(ctx, d, leg, CorridorRadiusM)
	if err != nil {
		t.Fatalf("canonCandidatesForLeg: %v", err)
	}
	var found bool
	for _, c := range cands {
		if c.ID == wantArc {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidate lookup returned %d arc(s), none of them arc %d — the arc beside the edge straddling the window seam at index %d was never asked for, because consecutive windows share no point and that edge is in neither one's rect",
			len(cands), wantArc, canonWindowPts-1)
	}

	// Same second half as the first red-proof: discovered is not enough, the
	// solve has to ride it, or the arc is cost without benefit.
	runs, err := corridorFollowRuns(t.Context(), leg, cands, defaultFollowBands())
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	var onM float64
	for _, r := range runs {
		if r.Kind == RunOn && r.CanonID == wantArc {
			m := r.MeasureEnd - r.MeasureStart
			if m < 0 {
				m = -m
			}
			onM += m
		}
	}
	if onM <= 0 {
		t.Errorf("the seam arc is now discovered but the solve binds none of it — %d run(s), no ON run on arc %d", len(runs), wantArc)
	}
}
