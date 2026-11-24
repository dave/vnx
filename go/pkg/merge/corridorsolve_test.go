package merge

// Tests for the shortest path over
// (interval, direction) states, layered on import measure. See corridorsolve.go's
// own header for the switch-cost mechanism these tests hold up.
//
// A warning carried into every test here: k, P and the backtrack weight
// were UNMEASURED placeholders when these were written. No test
// below depends on their absolute values — only on ORDERING or RATIO, which
// hold for any positive P and any even k.

import (
	"math"
	"testing"
)

// ---- case 8: empty network and all-OFF leg ------------------------------

func TestSolveCorridorPath_EmptyCandidatesIsAllOff(t *testing.T) {
	res, err := solveCorridorPath(t.Context(), 100, nil, nil, nil, CorridorRadiusM, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	if res == nil {
		t.Fatalf("empty network: want a well-formed all-OFF result, got nil")
	}
	if len(res.Runs) != 1 || !res.Runs[0].State.Off {
		t.Fatalf("empty network: want one OFF run, got %+v", res.Runs)
	}
	if res.Runs[0].ImportMeasureLo != 0 || res.Runs[0].ImportMeasureHi != 100 {
		t.Fatalf("OFF run should span the whole leg: got [%v,%v]", res.Runs[0].ImportMeasureLo, res.Runs[0].ImportMeasureHi)
	}
	// OFF is not free per metre (corridorsolve.go's forward-edge comment):
	// its cost is the leg's own raw import metres, matching
	// noBridgeCost's "+offImportMetres" term. An all-OFF leg's total cost is
	// therefore its own length, not 0.
	if res.Cost != 100 {
		t.Fatalf("all-OFF cost should equal the leg length (100), got %v", res.Cost)
	}
}

func TestSolveCorridorPath_ZeroLengthLegIsNilNotError(t *testing.T) {
	res, err := solveCorridorPath(t.Context(), 0, nil, nil, nil, CorridorRadiusM, emissionK)
	if err != nil || res != nil {
		t.Fatalf("zero-length leg: want nil,nil, got %v,%v", res, err)
	}
}

// ---- baseline: one candidate directly under the whole leg ---------------

// buildIvs is a small helper: corridor intervals for a straight source
// against one or more candidates, at R.
func buildIvs(t *testing.T, sa *sourceMeasure, cands []*PreppedCandidate, r float64) []corridorInterval {
	t.Helper()
	ivs, err := corridorIntervals(t.Context(), sa, cands, r)
	if err != nil {
		t.Fatalf("corridorIntervals: %v", err)
	}
	return ivs
}

// cloneCorridorIntervals copies the interval list and each interval's Pieces
// slice. [solveCorridorPath] takes ownership of the intervals it is given and
// releases their Pieces, so a test that
// solves twice over one interval list, or reads the pieces after solving, must
// hand it a copy. The pieces themselves are values, so a shallow copy of each
// Pieces slice is a full copy of the alphabet.
func cloneCorridorIntervals(ivs []corridorInterval) []corridorInterval {
	out := make([]corridorInterval, len(ivs))
	for i, iv := range ivs {
		out[i] = iv
		out[i].Pieces = append([]CorridorPiece(nil), iv.Pieces...)
	}
	return out
}

func TestSolveCorridorPath_SingleCandidateAdoptedNotOff(t *testing.T) {
	const R = CorridorRadiusM
	pc := prepOne(t, mPt(-20, 0), mPt(120, 0)) // candidate runs well past the source at both ends
	sa, err := newSourceMeasure(straightLegX(3, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	ivs := buildIvs(t, sa, []*PreppedCandidate{pc}, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, []*PreppedCandidate{pc}, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	if len(res.Runs) != 1 || res.Runs[0].State.Off {
		t.Fatalf("a candidate lying right under the whole source should be adopted, not OFF: %+v", res.Runs)
	}
}

// ---- case 5: parallel trails, one chosen, no swaps -----------------------

// TestSolveCorridorPath_ParallelTrailsOneChosenNoSwaps builds two candidates
// running parallel to the source at 5m and 10m offset (both within R=15,
// both plausible), and checks the solver commits to ONE of them for the
// whole leg rather than chattering between them (which would cost 2×P more
// than committing, for zero emission benefit — the closer one strictly
// dominates the farther one everywhere, so there is never a reason to
// switch).
func TestSolveCorridorPath_ParallelTrailsOneChosenNoSwaps(t *testing.T) {
	const R = CorridorRadiusM
	near := prepOne(t, mPt(-20, 5), mPt(120, 5))
	far := prepOne(t, mPt(-20, 10), mPt(120, 10))
	sa, err := newSourceMeasure(straightLegX(5, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{near, far}
	ivs := buildIvs(t, sa, cands, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}

	onRuns := 0
	seenCand := map[int]bool{}
	for _, r := range res.Runs {
		if !r.State.Off {
			onRuns++
			seenCand[r.CandIdx] = true
		}
	}
	if onRuns != 1 {
		t.Fatalf("want exactly one ON run (no swapping between the two parallel trails), got %d: %+v", onRuns, res.Runs)
	}
	if len(seenCand) != 1 {
		t.Fatalf("want exactly one candidate adopted, got %d: %+v", len(seenCand), res.Runs)
	}
	// The nearer candidate has strictly lower emission cost everywhere and
	// costs the same P to enter, so it must be the one chosen.
	nearCandIdx := 0 // cands[0] == near
	for c := range seenCand {
		if c != nearCandIdx {
			t.Fatalf("want the nearer candidate (index %d) chosen, got candidate %d", nearCandIdx, c)
		}
	}
}

// TestSolveCorridorPath_CrossingTrailsNoChatterWithP is the case above's
// stronger sibling. Red-proofing this file's first test found that the parallel,
// never-crossing fixture above passes even with P forced to 0 — the near
// candidate is strictly cheaper everywhere, so nothing there actually
// exercises "switching costs money", only "the cheaper one is chosen". This
// fixture crosses: candidate A is cheaper for the first half, candidate B
// for the second, so a solver with NO switching cost has a real, local
// incentive to hop between them every time the locally-cheaper one changes.
// With the real P this is not selected; confirmed by breaking P to 0 in a
// detached worktree and watching this exact fixture chatter into 6 runs.
func TestSolveCorridorPath_CrossingTrailsNoChatterWithP(t *testing.T) {
	const R = CorridorRadiusM
	a := prepOne(t, mPt(-20, 5), mPt(50, 5), mPt(120, 10))
	b := prepOne(t, mPt(-20, 10), mPt(50, 10), mPt(120, 5))
	sa, err := newSourceMeasure(straightLegX(9, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{a, b}
	ivs := buildIvs(t, sa, cands, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	onRuns := 0
	for _, r := range res.Runs {
		if !r.State.Off {
			onRuns++
		}
	}
	if onRuns != 1 {
		t.Fatalf("crossing trails with a real switching cost must still settle on ONE candidate, not chatter at the crossing point: got %d ON runs: %+v", onRuns, res.Runs)
	}
}

// ---- short deviation absorbed; short spurious match never selected -------

// TestSolveCorridorPath_ShortDeviationAbsorbed builds a candidate that
// wobbles a little (well within R throughout) alongside the source: the
// corridor stays one continuous interval, so the solver must stay ON without
// ever paying a state change for the wobble.
func TestSolveCorridorPath_ShortDeviationAbsorbed(t *testing.T) {
	const R = CorridorRadiusM
	// A mild bump consistently on ONE side of the source (y from 5 to 7 back
	// to 5) — small enough that the interval builder reports ONE corridor interval (checked
	// below), not the self-approach shape a sharper peak produces (that is
	// TestSolveCorridorPath_ArmJumpCostsMeasureGap below): this fixture exists
	// to isolate "never leaves R" from "the candidate approaches itself".
	pc := prepOne(t, mPt(-20, 5), mPt(50, 7), mPt(120, 5))
	sa, err := newSourceMeasure(straightLegX(9, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	if len(ivs) != 1 {
		t.Fatalf("fixture sanity: want ONE corridor interval for a wobble that never crosses itself, got %d: %+v", len(ivs), ivs)
	}
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	onRuns := 0
	for _, r := range res.Runs {
		if !r.State.Off {
			onRuns++
		}
	}
	if onRuns != 1 {
		t.Fatalf("a wobble that never leaves R must be absorbed into ONE ON run, got %d: %+v", onRuns, res.Runs)
	}
	// Absorbed means no P was ever paid beyond the single entry fee: cost
	// must be strictly less than a second state change would add.
	if res.Cost >= 2*stateChangeCost() {
		t.Errorf("cost %.6g suggests more than the one entry P was paid — not absorbed", res.Cost)
	}
}

// TestSolveCorridorPath_ShortSpuriousMatchNeverSelected builds a candidate
// that only brushes the corridor for a handful of metres in the middle of an
// otherwise-unrelated leg. Adopting it costs 2P (enter, leave) plus its own
// emission; staying OFF costs 0. For ANY positive P, OFF must win.
func TestSolveCorridorPath_ShortSpuriousMatchNeverSelected(t *testing.T) {
	const R = CorridorRadiusM
	// A short candidate sitting just inside R of the source only around
	// x=50, nowhere else near it.
	pc := prepOne(t, mPt(48, R-1), mPt(52, R-1))
	sa, err := newSourceMeasure(straightLegX(11, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	if len(ivs) == 0 {
		t.Fatalf("fixture sanity: expected a short corridor interval to exist")
	}
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	for _, r := range res.Runs {
		if !r.State.Off {
			t.Fatalf("a short spurious match must never be selected over staying OFF: %+v", res.Runs)
		}
	}
	if res.Cost != sa.Total() {
		t.Fatalf("an all-OFF path should cost its own length (%v), got %v", sa.Total(), res.Cost)
	}
}

// ---- out-and-back / lollipop ---------------------------------------------

// TestSolveCorridorPath_OutAndBackTwoStrokes builds a source that walks out
// along a candidate and back over the SAME geometry (an out-and-back). The
// interval builder represents this as ONE interval whose canon measure rises then falls; the
// solver may either (a) stay in one direction the whole way, paying
// backtrack over the return leg, or (b) flip direction once at the
// turnaround, paying P. Whichever is cheaper, the winning path must touch
// the candidate exactly twice in effect: forward then backward — i.e. it
// must not fabricate extra state changes, and cost must scale with P not
// with candidate count.
func TestSolveCorridorPath_OutAndBackTwoStrokes(t *testing.T) {
	const R = CorridorRadiusM
	pc := prepOne(t, mPt(0, 0), mPt(100, 0))
	// Source walks 0->100 then straight back 100->0, directly on the
	// candidate (dist=0 throughout) — an out-and-back over one edge.
	xs := []float64{0, 25, 50, 75, 100, 75, 50, 25, 0}
	pts := make([]Point, len(xs))
	for i, x := range xs {
		pts[i] = mPt(x, 0)
	}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	if len(ivs) != 1 {
		t.Fatalf("fixture sanity: want ONE interval for the whole out-and-back, got %d: %+v", len(ivs), ivs)
	}

	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}

	// The two competing strategies, computed from the SAME cost functions
	// the solver uses — not a hardcoded number, so this is agnostic to
	// wherever calibration lands P and the backtrack weight (compare
	// mechanisms, not constants).
	// dist=0 throughout, so emission is 0 either way: BOTH strategies pay one
	// P to enter the candidate from OFF at x=0. From there, staying FWD the
	// whole way additionally accrues backtrack over the ~100m return leg;
	// flipping to REV at the turnaround additionally costs one more P.
	returnLegM := 99.88764520516769 // canon measure measured (haversine, not nominal)
	stayCost := stateChangeCost() + backtrackWeightPerM*returnLegM
	flipCost := 2 * stateChangeCost()
	want := math.Min(stayCost, flipCost)
	if math.Abs(res.Cost-want) > 1e-6*math.Max(1, want) {
		t.Errorf("cost = %.9g, want min(stay=%.9g, flip=%.9g) = %.9g", res.Cost, stayCost, flipCost, want)
	}

	// Structural check regardless of which strategy won: no OFF anywhere
	// (the candidate covers the whole leg at dist=0, so going OFF can never
	// be cheaper than adopting — corridorsolve.go's OFF-costs-its-own-metres
	// fix is exactly what makes this true), and at most ONE switch (either
	// 0, staying FWD, or 1, flipping to REV) — never more.
	onRuns, offRuns := 0, 0
	for _, r := range res.Runs {
		if r.State.Off {
			offRuns++
		} else {
			onRuns++
		}
	}
	if offRuns != 0 {
		t.Errorf("an out-and-back directly on top of the candidate must never go OFF: %+v", res.Runs)
	}
	if onRuns < 1 || onRuns > 2 {
		t.Errorf("want 1 run (stay FWD) or 2 runs (flip to REV) — the 'two strokes' the acceptance case names — got %d: %+v", onRuns, res.Runs)
	}
	if onRuns == 2 {
		if res.Runs[0].State.Dir != CorridorFWD || res.Runs[1].State.Dir != CorridorREV {
			t.Errorf("a two-run out-and-back must go FWD then REV, got %v then %v", res.Runs[0].State.Dir, res.Runs[1].State.Dir)
		}
		if res.Runs[0].ImportMeasureHi != res.Runs[1].ImportMeasureLo {
			t.Errorf("the two runs must meet exactly at the turnaround: %+v", res.Runs)
		}
	}
}

// lollipopLerp is a plain N-point subdivision of a-b, used to densify a
// lollipop fixture near its own loop junction — see the test's own comment
// for why that matters.
func lollipopLerp(a, b Point, n int) []Point {
	out := make([]Point, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		out[i] = Point{Lat: a.Lat + f*(b.Lat-a.Lat), Lon: a.Lon + f*(b.Lon-a.Lon)}
	}
	return out
}

// TestSolveCorridorPath_LollipopTwoVisits builds a stem the source walks out
// along, a loop it completes once, then the SAME stem back — the lollipop
// shape acceptance case 7 names. The loop is walked once, forward, with no
// direction ambiguity (it never revisits its own canon measure).
//
// ⚠ NOT the same mechanism as the out-and-back test above, despite the
// similar shape — found while red-proofing this file: breaking the
// piece-boundary-breakpoint fix (the exact defect OutAndBackTwoStrokes
// exists to catch) leaves THIS test green. The loop in between breaks the
// interval continuity, so the outbound stem and the return stem land in two
// DIFFERENT corridorIntervals (not one interval whose direction flips
// mid-way) — the two-visit property here rests on interval-level
// breakpoints, which predate this task's fix, not the piece-level ones this
// task added. Kept as its own case because "does the return leg get
// re-adopted at all, across a real interval gap" is still a real property worth
// guarding, just not the one its name might suggest.
//
// The stem is long relative to R and the loop junction: where the loop
// closes back onto the stem's own end vertex, the interval join produces a handful
// of metres of real corridor gap (multiple canon edges meeting at one vertex
// within R of each other, a genuine construction this fixture did not
// invent — confirmed by direct inspection, and worth a follow-up, but
// not this task's to fix). A short stem makes that gap look like it forces
// abandoning the return leg entirely; a long one (this fixture) proves it
// doesn't: the solver still finds its way back onto the stem for the return
// visit, paying only the small real gap, not the whole return leg.
func TestSolveCorridorPath_LollipopTwoVisits(t *testing.T) {
	const R = CorridorRadiusM
	pc := prepOne(t,
		mPt(0, 0), mPt(200, 0), // stem
		mPt(240, 20), mPt(240, -20), mPt(200, 0), // loop, closing back on the stem end
	)
	var pts []Point
	pts = append(pts, lollipopLerp(mPt(0, 0), mPt(200, 0), 9)...)
	pts = append(pts, lollipopLerp(mPt(200, 0), mPt(240, 20), 5)[1:]...)
	pts = append(pts, lollipopLerp(mPt(240, 20), mPt(240, -20), 5)[1:]...)
	pts = append(pts, lollipopLerp(mPt(240, -20), mPt(200, 0), 5)[1:]...)
	pts = append(pts, lollipopLerp(mPt(200, 0), mPt(0, 0), 9)[1:]...)

	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	if len(ivs) == 0 {
		t.Fatalf("fixture sanity: expected at least one corridor interval")
	}

	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}

	offM, onM := 0.0, 0.0
	sawFWD, sawREV := false, false
	for _, r := range res.Runs {
		if r.State.Off {
			offM += r.ImportMeasureHi - r.ImportMeasureLo
			continue
		}
		onM += r.ImportMeasureHi - r.ImportMeasureLo
		if r.CandIdx != 0 {
			t.Fatalf("only one candidate exists in this fixture, got CandIdx=%d", r.CandIdx)
		}
		if r.State.Dir == CorridorFWD {
			sawFWD = true
		} else {
			sawREV = true
		}
	}
	// The two visits: the outbound stem+loop run FWD, the return stem run
	// REV — the same mechanism as the out-and-back test, not a special case.
	if !sawFWD || !sawREV {
		t.Errorf("want both a FWD run (outbound) and a REV run (return) — the lollipop's two visits — got FWD=%v REV=%v: %+v", sawFWD, sawREV, res.Runs)
	}
	// Coverage is overwhelmingly adoption, not OFF: the only OFF is the
	// small real corridor gap at the loop junction (this test's own header),
	// which must not be allowed to snowball into abandoning the whole
	// return leg (the failure mode a short stem would hide).
	if offM > 0.25*sa.Total() {
		t.Errorf("OFF metres %.6g is too large a fraction of the %.6g total leg — the return leg looks abandoned, not bridged past a small junction gap", offM, sa.Total())
	}
	if onM < 0.7*sa.Total() {
		t.Errorf("ON metres %.6g should dominate the %.6g total leg for a lollipop traced directly on its own candidate", onM, sa.Total())
	}
}

// ---- case 6: self-approach, arm-jump costs its measure gap -------------------

// selfApproachFixture is TestCorridorIntervals_SelfApproachTwoIntervals'
// own shape (corridorinterval_test.go), scaled up so the leg is long
// relative to P — a short leg makes "never adopt anything" look like the
// arm-jump guard when it's really just the short-leg-vs-P economics every
// other test in this file also has to scale around.
func selfApproachFixture(t *testing.T) (*sourceMeasure, []*PreppedCandidate, []corridorInterval) {
	t.Helper()
	const R = CorridorRadiusM
	pc := prepOne(t,
		mPt(0, 5), mPt(500, 5), // edge 0: near arm, y=+5
		mPt(500, 1000), mPt(0, 1000), mPt(0, -5), // edges 1-3: a detour far away
		mPt(500, -5), // edge 4: far arm, y=-5
	)
	sa, err := newSourceMeasure(straightLegX(5, 500, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)
	if len(ivs) != 2 || ivs[0].CandIdx != 0 || ivs[1].CandIdx != 0 {
		t.Fatalf("fixture sanity: want 2 self-approaching intervals on ONE candidate, got %+v", ivs)
	}
	return sa, cands, ivs
}

// TestTransitionCost_ArmJumpCostsFullMeasureGapRegardlessOfDirection is the
// mechanism check ("by mechanism, not hope"): jumping
// between the near arm and far arm costs the RAW canon-measure gap between them,
// unconditionally — not [backtrackCost]'s direction-conditional 0-when-
// aligned logic, which corridorsolve.go's header explains is wrong for an
// instantaneous jump (found the hard way: it silently zeroed a 3km jump
// whenever the jump direction happened to match the declared FWD/REV).
func TestTransitionCost_ArmJumpCostsFullMeasureGapRegardlessOfDirection(t *testing.T) {
	_, cands, ivs := selfApproachFixture(t)
	x := (ivs[0].ImportMeasureLo + ivs[0].ImportMeasureHi) / 2

	// The state names a PIECE now, so the jump is expressed between one piece
	// of the near arm and one of the far arm rather than between two
	// intervals. The mechanism under test is unchanged: an instantaneous
	// change of correspondence costs the raw canon-measure gap it crosses.
	sps := buildSolvePieces(ivs)
	pick := func(ivIdx int) int {
		for i, sp := range sps {
			if sp.IvIdx == ivIdx && x >= sp.P.ImportMeasureAtLo && x <= sp.P.ImportMeasureAtHi {
				return i
			}
		}
		t.Fatalf("fixture sanity: no piece of iv%d is active at x=%v", ivIdx, x)
		return -1
	}
	spA, spB := pick(0), pick(1)
	measureA := canonMeasureAtPiece(sps[spA].P, x)
	measureB := canonMeasureAtPiece(sps[spB].P, x)
	gap := math.Abs(measureB - measureA)
	if gap < 500 {
		t.Fatalf("fixture sanity: want a large measure gap (hundreds of metres, the measured 781m worst case), got %.6g", gap)
	}
	want := backtrackWeightPerM * gap

	prod, err := resolveTunables(nil)
	if err != nil {
		t.Fatalf("resolveTunables: %v", err)
	}
	for _, dir := range []CorridorDir{CorridorFWD, CorridorREV} {
		from := CorridorStateID{SpIdx: spA, Dir: dir}
		to := CorridorStateID{SpIdx: spB, Dir: dir}
		got := TransitionCostT(from, to, x, sps, cands, prod)
		if math.Abs(got-want) > 1e-6*want {
			t.Errorf("dir=%v: arm-jump cost = %.9g, want the full measure gap %.9g (regardless of which way the jump happens to point relative to the declared direction)", dir, got, want)
		}
		// The reverse jump (far arm back to near arm) must cost the SAME
		// magnitude — a jump's cost is not directional in the way a
		// continuous piece's backtrack is.
		got2 := TransitionCostT(to, from, x, sps, cands, prod)
		if math.Abs(got2-want) > 1e-6*want {
			t.Errorf("dir=%v reverse jump: cost = %.9g, want %.9g", dir, got2, want)
		}
	}
}

// TestSolveCorridorPath_ArmJumpCostsMeasureGap is the end-to-end acceptance
// case: given a self-approaching candidate, the winning path commits to ONE
// arm for the whole leg and never jumps — because the mechanism above prices
// a jump at its full, large measure gap, which is never cheaper than staying put
// (both arms have the same emission cost, dist=5m throughout, so there is
// strictly nothing to gain and hundreds of metres to lose by jumping).
func TestSolveCorridorPath_ArmJumpCostsMeasureGap(t *testing.T) {
	sa, cands, ivs := selfApproachFixture(t)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, CorridorRadiusM, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	seenArm := map[int]bool{}
	for _, r := range res.Runs {
		if !r.State.Off {
			seenArm[r.State.SpIdx] = true
		}
	}
	if len(seenArm) > 1 {
		t.Fatalf("a self-approaching candidate must never jump arms mid-leg, got runs on %d different arms: %+v", len(seenArm), res.Runs)
	}
}

// ---- P not charged across a shared node -----------------------------------

// TestSolveCorridorPath_SharedNodeNotChargedP builds two DIFFERENT
// candidates meeting end-to-end at a shared vertex (a real network node,
// the way two adjacent canon arcs do), directly under a single straight
// source. Crossing from one to the other must cost nothing extra: total
// cost is exactly ONE entry fee (P), not two.
func TestSolveCorridorPath_SharedNodeNotChargedP(t *testing.T) {
	const R = CorridorRadiusM
	a := prepOne(t, mPt(0, 0), mPt(50, 0))
	b := prepOne(t, mPt(50, 0), mPt(300, 0)) // shares vertex (50,0) with a
	sa, err := newSourceMeasure(straightLegX(7, 300, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{a, b}
	ivs := buildIvs(t, sa, cands, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	if math.Abs(res.Cost-stateChangeCost()) > 1e-6*stateChangeCost() {
		t.Errorf("crossing a shared node cost extra: total cost = %.9g, want exactly one entry fee P = %.9g", res.Cost, stateChangeCost())
	}
	for _, r := range res.Runs {
		if r.State.Off {
			t.Errorf("a straight line directly on top of two node-adjacent candidates must never go OFF: %+v", res.Runs)
		}
	}
}

// TestSolveCorridorPath_UnconnectedCandidatesChargedTwiceP is the negative
// control: the SAME two-candidate shape, but with a real 5m gap between
// them (no shared node) — crossing must cost the full two entry fees. Both
// candidates are made long relative to the leg (140m/155m of a 300m leg) so
// that skipping either one (an alternative the solver is free to take, and
// correctly does when a candidate is short — see
// TestSolveCorridorPath_ShortSpuriousMatchNeverSelected) is clearly worse
// than adopting both; a short candidate here would make the solver's
// CORRECT choice to skip it look like a failure to charge P.
func TestSolveCorridorPath_UnconnectedCandidatesChargedTwiceP(t *testing.T) {
	const R = CorridorRadiusM
	a := prepOne(t, mPt(0, 0), mPt(140, 0))
	b := prepOne(t, mPt(145, 0), mPt(300, 0)) // 5m short of a — no shared node
	sa, err := newSourceMeasure(straightLegX(7, 300, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{a, b}
	ivs := buildIvs(t, sa, cands, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	want := 2 * stateChangeCost()
	// A generous tolerance: the crossing sits inside R of both candidates'
	// endpoints, so a sliver of emission is real cost, not slop — this
	// checks "two entry fees, not one and not three", not exact equality.
	if res.Cost < want || res.Cost > want+0.05*want {
		t.Errorf("unconnected candidates: total cost = %.9g, want approximately two entry fees 2P = %.9g (no adjacency exemption without a real shared vertex)", res.Cost, want)
	}
	onCands := map[int]bool{}
	for _, r := range res.Runs {
		if !r.State.Off {
			onCands[r.CandIdx] = true
		}
	}
	if len(onCands) != 2 {
		t.Fatalf("both candidates should be adopted here (both are long relative to the leg), got %v: %+v", onCands, res.Runs)
	}
}

// ---- byte-identical duplicate candidates broken by id ----------------------

// TestSolveCorridorPath_DuplicateCandidatesBrokenByID builds two
// byte-identical candidates (the same geometry twice, corresponding to
// canonCandidatesForLeg's own id-sorted duplicate rows — arcs 104/115's
// shape). They score identically in every state,
// so the winning path must deterministically settle on the LOWER CandIdx —
// ascending id, since ivs is built from an id-sorted candidate list.
func TestSolveCorridorPath_DuplicateCandidatesBrokenByID(t *testing.T) {
	const R = CorridorRadiusM
	dup1 := prepOne(t, mPt(-20, 0), mPt(120, 0))
	dup2 := prepOne(t, mPt(-20, 0), mPt(120, 0)) // byte-identical geometry
	sa, err := newSourceMeasure(straightLegX(5, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{dup1, dup2}
	ivs := buildIvs(t, sa, cands, R)
	res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	for _, r := range res.Runs {
		if !r.State.Off && r.CandIdx != 0 {
			t.Errorf("byte-identical duplicates must break to the lower CandIdx (0), got %d: %+v", r.CandIdx, res.Runs)
		}
	}
}

// ---- deterministic tiebreak at d = R ---------------------------------------

// TestSolveCorridorPath_DeterministicAtBandEdge runs the same fixture
// several times and requires byte-identical results. Every corridor
// interval's OUTER boundary sits exactly on d = R by construction
// (corridorinterval_test.go's own checkPieceEndpointsOnBand), so ordinary
// production use already exercises this boundary constantly; this test
// exists to catch map-iteration nondeterminism (Go randomises map iteration
// order, and this file uses maps for breakpoint dedup and Dijkstra's
// dist/prev tables) leaking into the result, not to re-derive R itself.
func TestSolveCorridorPath_DeterministicAtBandEdge(t *testing.T) {
	const R = CorridorRadiusM
	// A candidate offset exactly R from the source over its whole extent —
	// every point of it sits precisely on the band edge, the most sensitive
	// case for a boundary tiebreak.
	pc := prepOne(t, mPt(-20, R), mPt(120, R))
	sa, err := newSourceMeasure(straightLegX(9, 100, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	cands := []*PreppedCandidate{pc}
	ivs := buildIvs(t, sa, cands, R)

	var first *corridorSolveResult
	for i := 0; i < 20; i++ {
		// A clone per run: the solve releases the intervals' pieces
		// ([solveCorridorPath]'s ownership note), so every run must be given
		// the same INPUT, not the leftovers of the previous one. Cloning keeps
		// this a test of the solve's determinism over one fixed alphabet —
		// rebuilding the intervals instead would also re-measure the mint.
		res, err := solveCorridorPath(t.Context(), sa.Total(), sa, cloneCorridorIntervals(ivs), cands, R, emissionK)
		if err != nil {
			t.Fatalf("run %d: solveCorridorPath: %v", i, err)
		}
		if first == nil {
			first = res
			continue
		}
		if first.Cost != res.Cost {
			t.Fatalf("run %d: cost %.17g != run 0's cost %.17g — nondeterministic", i, res.Cost, first.Cost)
		}
		if len(first.Runs) != len(res.Runs) {
			t.Fatalf("run %d: %d runs != run 0's %d runs — nondeterministic", i, len(res.Runs), len(first.Runs))
		}
		for j := range first.Runs {
			if first.Runs[j] != res.Runs[j] {
				t.Fatalf("run %d: run[%d] = %+v != run 0's %+v — nondeterministic", i, j, res.Runs[j], first.Runs[j])
			}
		}
	}
}

// ---- sourceArcsBetween -----------------------------------------------

// TestSourceArcsBetween_TrimsExactlyToTheWindow is the soundness
// argument's other half (corridorsolve.go's buildCorridorBridges doc
// comment): padding the competitor set OUTWARD past the true import-measure
// window would be the UNSAFE direction (more competitors can only make a
// bridge look cheaper), so this pins that the first and last returned
// arc start/end EXACTLY at the interpolated measure position, not at the
// nearest original vertex either side of it.
func TestSourceArcsBetween_TrimsExactlyToTheWindow(t *testing.T) {
	pts := []Point{mPt(0, 0), mPt(10, 0), mPt(20, 0), mPt(30, 0)}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	arcs, err := sourceArcsBetween(sa, 3, 23) // strictly inside edges 0 and 2
	if err != nil {
		t.Fatalf("sourceArcsBetween: %v", err)
	}
	if len(arcs) != 3 {
		t.Fatalf("want 3 arcs (partial, full, partial), got %d: %+v", len(arcs), arcs)
	}
	// Compared in METRES, not raw Lat/Lon: posAtMeasure/latLonAt interpolate via a
	// division-then-multiplication that need not reproduce mPt's own
	// straight-line construction to the last ULP — the same equirectangular
	// noise floor (~0.026 m on an 880 m edge), not a sign that the trim is inexact.
	wantLo, wantHi := mPt(3, 0), mPt(23, 0)
	// mPt's x->Lon conversion is a flat equirectangular approximation;
	// newSourceMeasure's cumMeasure is built from HaversineMetres (a true
	// great-circle distance) — the two do not need to agree to sub-cm
	// precision even over a few tens of metres, so this tolerance checks
	// "trimmed to the right place", not "bit-identical to mPt's own
	// construction".
	const tolM = 0.5
	if got := arcs[0][0]; HaversineMetres(got.Lat, got.Lon, wantLo.Lat, wantLo.Lon) > tolM {
		t.Errorf("first arc's start should sit exactly at measure=3, got %+v want %+v", got, wantLo)
	}
	if got := arcs[len(arcs)-1][1]; HaversineMetres(got.Lat, got.Lon, wantHi.Lat, wantHi.Lon) > tolM {
		t.Errorf("last arc's end should sit exactly at measure=23, got %+v want %+v", got, wantHi)
	}
	// The middle arc is the untouched original edge 1 (vertex 10 -> 20):
	// interior vertices are never trimmed, only the two ends.
	wantMidLo, wantMidHi := mPt(10, 0), mPt(20, 0)
	if got := arcs[1][0]; got != wantMidLo {
		t.Errorf("middle arc should start at the original vertex 10, got %+v want %+v", got, wantMidLo)
	}
	if got := arcs[1][1]; got != wantMidHi {
		t.Errorf("middle arc should end at the original vertex 20, got %+v want %+v", got, wantMidHi)
	}
}

// TestSourceArcsBetween_SameEdgeIsOneTrimmedArc: both ends inside the
// same edge collapses to a single arc, neither end a full-edge vertex.
func TestSourceArcsBetween_SameEdgeIsOneTrimmedArc(t *testing.T) {
	pts := []Point{mPt(0, 0), mPt(100, 0)}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	arcs, err := sourceArcsBetween(sa, 40, 60)
	if err != nil {
		t.Fatalf("sourceArcsBetween: %v", err)
	}
	if len(arcs) != 1 {
		t.Fatalf("want exactly 1 arc, got %d: %+v", len(arcs), arcs)
	}
	wantLo, wantHi := mPt(40, 0), mPt(60, 0)
	// mPt's x->Lon conversion is a flat equirectangular approximation;
	// newSourceMeasure's cumMeasure is built from HaversineMetres (a true
	// great-circle distance) — the two do not need to agree to sub-cm
	// precision even over a few tens of metres, so this tolerance checks
	// "trimmed to the right place", not "bit-identical to mPt's own
	// construction".
	const tolM = 0.5
	if got := arcs[0][0]; HaversineMetres(got.Lat, got.Lon, wantLo.Lat, wantLo.Lon) > tolM {
		t.Errorf("arc start = %+v, want %+v", got, wantLo)
	}
	if got := arcs[0][1]; HaversineMetres(got.Lat, got.Lon, wantHi.Lat, wantHi.Lon) > tolM {
		t.Errorf("arc end = %+v, want %+v", got, wantHi)
	}
}

// TestSourceArcsBetween_InvertedWindowIsError: hiMeasure <= loMeasure is a caller
// bug (buildCorridorBridges only ever calls this with a strict import-measure
// gap already established) — not an expected absence.
func TestSourceArcsBetween_InvertedWindowIsError(t *testing.T) {
	pts := []Point{mPt(0, 0), mPt(100, 0)}
	sa, err := newSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	if _, err := sourceArcsBetween(sa, 60, 40); err == nil {
		t.Fatalf("inverted window [60,40]: want error, got nil")
	}
	if _, err := sourceArcsBetween(sa, 40, 40); err == nil {
		t.Fatalf("empty window [40,40]: want error, got nil")
	}
}
