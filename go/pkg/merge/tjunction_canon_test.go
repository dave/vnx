package merge

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// inCanonCopy runs fn on a copy of d, so whatever fn writes leaves d as it
// was.
func inCanonCopy(t *testing.T, d *MemCanon, fn func(c *MemCanon) error) error {
	t.Helper()
	snap, err := d.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	c, err := newMemCanon(snap)
	if err != nil {
		t.Fatalf("NewMemCanon: %v", err)
	}
	return fn(c)
}

// ---- fixtures ------------------------------------------------------------

// tjLegNS builds an n-point leg running due south from (xM, yM) in the metre
// frame, spaced 10 m apart. Used for legs that T onto an east-west canon
// arc: the leg's FIRST point is the terminal that does the T-ing.
func tjLegNS(xM, yM float64, n int) []Point {
	pts := make([]Point, n)
	for i := range pts {
		pts[i] = mPt(xM, yM-float64(i)*10)
	}
	return pts
}

// warpFixtureA is a 1000 m leg running due east along y=0.
func warpFixtureA() []Point {
	pts := make([]Point, 0, 101)
	for k := 0; k <= 100; k++ {
		pts = append(pts, mPt(float64(k)*10, 0))
	}
	return pts
}

// THE FRACTION-0/1 DECLINE CANNOT BE REACHED THROUGH THE IMPORT PATH, and it
// stays anyway. This test pins both halves of that claim.
//
// The reasoning: a fraction of exactly 0 or 1 means the closest point on the
// walk is a TERMINAL vertex, and a walk is [start node, pips…, end node] — so
// that vertex IS a node. If it is within the radius of the terminal, step 1
// found a node within the radius and declined before the search ran. The two
// checks use the same radius, so the shielding is total.
//
// Below: a terminal placed 8 m beyond A's end node. nearestArc
// really does answer Fraction == 1 for it — so the guard is not guarding
// against nothing — and the T-junction split really does decline at step 1, so
// the fraction check
// can never see it and abort a user's import.
func TestTJunction_TerminalVertexFractionIsShieldedByTheNodeProbe(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	addTestLeg(t, ctx, d, "A", warpFixtureA())

	// 8 m past A's end node at (1000, 0), on the same line.
	beyond := mPt(1008, 0)

	var (
		frac     float64
		found    bool
		declined TjunctionDecline
	)
	if err := inCanonCopy(t, d, func(c *MemCanon) error {
		cand, ok, err := nearestArc(
			ctx, c, beyond.Lat, beyond.Lon, EndpointWarpRadiusM)
		if err != nil {
			return err
		}
		frac, found = cand.Fraction, ok

		rep, err := splitCanonAtLegTerminals(ctx, c,
			append([]Point{beyond}, tjLegNS(1008, -10, 100)...))
		if err != nil {
			return err
		}
		declined = rep.Start.Decline
		return nil
	}); err != nil {
		t.Fatalf("probe transaction failed: %v", err)
	}

	if !found || frac != 1 {
		t.Fatalf("the search answered found=%v fraction=%v for a probe beyond the end node — this fixture is "+
			"supposed to produce the degenerate fraction the guard exists for", found, frac)
	}
	if declined != TjDeclineNodeInRadius {
		t.Fatalf("the T-junction split declined with %q; the node probe is supposed to shield the terminal-vertex case entirely, "+
			"so reaching %q would mean ValidateSnapFraction can abort a user's import", declined, TjDeclineTerminalVertex)
	}
}

// tjRoundTripBudgetM is the headroom the split's verify gate has on REAL DATA,
// and it is the number this file exists to defend.
//
// Measured over every terminal-to-canon-interior projection of several real
// data sets, the worst sits at 14.816 m against a 15 m gate. So if the
// fraction round trip displaces the minted node more than 15 − 14.816 =
// 0.184 m outward, that split ABORTS A REAL USER IMPORT.
//
// The gate is load-bearing, the margin is 18 centimetres, and the round trip
// therefore needs a measured bound rather than an argument.
const tjRoundTripBudgetM = 0.184

// tjRoundTripBoundM is what the round trip is actually ASSERTED to stay under.
//
// Not a tolerance on geometry and not a band: nothing is accepted or rejected
// by it in production. It is an alarm threshold on a quantity whose true value
// is ~1e-11 m, set five orders of magnitude below the budget so a regression
// shows up as a red test long before it can abort an import. Measured worst
// case is printed by the test on every run; if that number ever approaches this
// bound, the bound is not what needs changing.
const tjRoundTripBoundM = 1e-6

// THE FRACTION ROUND TRIP, MEASURED — the evidence the verify gate rests on.
//
// The gate compares the terminal against the node splitPolylineAtFraction
// actually minted, not against the projection, because the two are reached by
// different code:
//
//	ProjectProbeOntoWalk:      Fraction = PriorLenM / (PriorLenM + NextLenM)
//	splitPolylineAtFraction:   target   = Fraction × total,  then interpolate
//
// Both accumulate the SAME vertices with the SAME haversine primitive in the
// SAME order — verified, not assumed: a canon arc's vertices are [start node,
// pips, end node] and splitPolylineAtFraction walks [start, polyline…, end]
// over the same pips. Neither filters or dedupes a vertex, so the only error
// is the round trip through Fraction.
//
// Theory says that error is ~eps × total — the cancellation in
// (target − cum[i-1]) is divided by segLen and then multiplied back by it, so
// it does not amplify on a short leg inside a long walk. Sub-nanometre. But
// EXPECTED IS NOT MEASURED, and the budget is 0.184 m, so this runs walk
// shapes chosen to break that reasoning if it is wrong: dense and sparse
// sampling, a 100 km walk with a 10 cm leg in the middle (the cancellation
// case), stationary duplicate vertices, doubling back, the ±180 seam, the
// equator and 71° north. The walks are the cases: each names the vertices
// beside its special feature (its focus), and only those are probed — a
// stride along the walk could step over a 10 cm leg, and once did. Most
// focus vertices round-trip exactly (0 m). The far-half sites on the equator
// (96), 100 km (96) and winding (369) walks are where the old stride sweep
// measured the non-zero error (1e-13 to 2.9e-11 m), so the worst
// case it printed is still sampled.
func TestTJunction_FractionRoundTripStaysWithinTheGatesBudget(t *testing.T) {
	ctx := context.Background()

	var (
		worst      float64
		worstWalk  string
		samples    int
		terminalHi int // projections landing on a walk terminal — step 3's case
	)

	for _, w := range tjRoundTripWalks() {
		d := emptyCanon(t)
		addTestLeg(t, ctx, d, w.name, w.pts)

		committed := committedArcs(t, ctx, d)
		if len(committed) != 1 {
			t.Fatalf("%s: expected 1 committed arc, got %d", w.name, len(committed))
		}
		verts := committed[0].Vertices
		// The focus indices are committed-vertex indices, derived from the
		// input on the premise that the import keeps every vertex (or, for a
		// walk with wantVerts set, a known count). A change here means the
		// focus no longer names the feature.
		wantVerts := len(w.pts)
		if w.wantVerts != 0 {
			wantVerts = w.wantVerts
		}
		if len(verts) != wantVerts {
			t.Fatalf("%s: committed %d vertices, want %d — the focus indices no longer name the feature",
				w.name, len(verts), wantVerts)
		}
		onNamedLeg := 0

		for _, probe := range tjRoundTripProbes(verts, w.focus) {
			// Each probe runs on its own copy of the canon, so every sample
			// is measured against the pristine walk rather than against
			// whatever the previous split left behind.
			var (
				projLat, projLon float64
				nodeLat, nodeLon float64
				got              bool
				onTerminal       bool
			)
			errRollback := fmt.Errorf("rollback")
			err := inCanonCopy(t, d, func(c *MemCanon) error {
				cand, ok, err := nearestArc(
					ctx, c, probe.Lat, probe.Lon, EndpointWarpRadiusM)
				if err != nil {
					return err
				}
				if !ok {
					return errRollback
				}
				if cand.Fraction == 0 || cand.Fraction == 1 {
					// Step 3's case: no interior to split, and
					// ValidateSnapFraction would reject it. Not a round-trip
					// sample.
					onTerminal = true
					return errRollback
				}
				res, err := splitArcAt(ctx, c, cand.ID, cand.Fraction)
				if err != nil {
					return err
				}
				projLat, projLon = cand.Lat, cand.Lon
				nodeLat, nodeLon = res.Node.Lat, res.Node.Lon
				got = true
				return errRollback
			})
			if err != nil && err != errRollback {
				t.Fatalf("%s: probe (%v, %v): %v", w.name, probe.Lat, probe.Lon, err)
			}
			if onTerminal {
				terminalHi++
				continue
			}
			if !got {
				continue
			}
			samples++
			if w.namedLeg != nil {
				a, b := verts[*w.namedLeg], verts[*w.namedLeg+1]
				legM := HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
				// On the leg: within its own length of both of its ends.
				if HaversineMetres(a.Lat, a.Lon, projLat, projLon) < legM &&
					HaversineMetres(b.Lat, b.Lon, projLat, projLon) < legM {
					onNamedLeg++
				}
			}
			// The conservative quantity: the DISTANCE BETWEEN the projection
			// and the minted node bounds the change in either one's distance
			// to the terminal, by the triangle inequality. So a bound here is
			// a bound on how much the round trip can push the gate's own
			// measurement.
			if d := HaversineMetres(projLat, projLon, nodeLat, nodeLon); d > worst {
				worst, worstWalk = d, w.name
			}
		}
		if w.namedLeg != nil && onNamedLeg == 0 {
			t.Fatalf("%s: no probe projected onto the leg starting at vertex %d — the walk's named feature is not measured",
				w.name, *w.namedLeg)
		}
	}

	// 22 focus vertices x 8 probes = 176; one projects onto a terminal. The
	// count is deterministic, so it is asserted exactly: a floor would let a
	// focus vertex (8 probes) drop out without a red.
	const wantSamples, wantTerminal = 175, 1
	if samples != wantSamples || terminalHi != wantTerminal {
		t.Fatalf("%d round-trip samples and %d terminal skips, want %d and %d — a focus vertex or probe "+
			"no longer reaches the split, so the sweep is not exercising what it claims",
			samples, terminalHi, wantSamples, wantTerminal)
	}
	t.Logf("fraction round trip: worst %.3e m over %d samples (worst walk: %s); "+
		"%d probes projected onto a walk terminal and were skipped. Gate budget %.3f m, asserted bound %.0e m",
		worst, samples, worstWalk, terminalHi, tjRoundTripBudgetM, tjRoundTripBoundM)

	if worst > tjRoundTripBoundM {
		t.Fatalf("the fraction round trip displaces the minted node by up to %.6e m (walk %q), over the %.0e m "+
			"this is asserted to stay under. The verify gate's real headroom is %.3f m (a real leg's "+
			"last terminal projects at 14.816 m against a 15 m gate), so a round trip approaching that aborts a "+
			"valid user import. Do NOT raise this bound to go green",
			worst, worstWalk, tjRoundTripBoundM, tjRoundTripBudgetM)
	}
}

// tjRoundTripWalks are the canon shapes the round-trip sweep runs over. Each is
// chosen to stress a different part of the fraction round trip rather than to
// look like a nice trail.
//
// focus lists the vertex indices to probe beside (each vertex and the midpoint
// of the edge that follows it); namedLeg, when set, is an edge at least one
// probe must project onto.
func tjRoundTripWalks() []tjWalk {
	type walk = tjWalk
	// tjAt builds a point xM east / yM north of an arbitrary base, so walks can
	// be placed at any latitude and across the ±180 seam — mPt is pinned to one
	// fixed base.
	tjAt := func(baseLat, baseLon, xM, yM float64) Point {
		perDegLon := mPerDegLat * math.Cos(baseLat*math.Pi/180)
		lon := baseLon + xM/perDegLon
		for lon > 180 {
			lon -= 360
		}
		for lon < -180 {
			lon += 360
		}
		return Point{Lat: baseLat + yM/mPerDegLat, Lon: lon}
	}
	line := func(baseLat, baseLon, lenM, stepM float64) []Point {
		n := int(lenM/stepM) + 1
		pts := make([]Point, n)
		for i := range pts {
			pts[i] = tjAt(baseLat, baseLon, float64(i)*stepM, 0)
		}
		return pts
	}

	out := []walk{
		{name: "ordinary 1 km at 10 m spacing", pts: line(54, -3, 1000, 10), focus: []int{50}},
		{name: "dense 2 km at 1 m spacing", pts: line(54, -3, 2000, 1), focus: []int{1000}},
		{name: "sparse 50 km at 500 m spacing", pts: line(54, -3, 50000, 500), focus: []int{50}},
		{name: "short 40 m at 2 m spacing", pts: line(54, -3, 40, 2), focus: []int{10}},
		{name: "equator", pts: line(0.0001, 0.0001, 1000, 10), focus: []int{50, 96}},
		{name: "71 north", pts: line(71, 25, 1000, 10), focus: []int{50}},
		// 99 is the last edge, the one nearest the seam.
		{name: "near, but not across, the +/-180 seam", pts: line(54, 179.98, 1000, 10), focus: []int{50, 99}},
	}
	// ⚠ A walk that CROSSES the seam is deliberately NOT in this sweep, and it
	// is not an omission. It does not measure the fraction round trip — it
	// measures a different and much larger defect (splitPolylineAtFraction does
	// not wrap longitude, so a split inside the straddling leg mints a node
	// thousands of km away). Averaging that into a round-trip bound would hide
	// both: the round trip would look catastrophic and the real defect would
	// look like float noise. It has its own test, which asserts what production
	// actually does with it: the verify gate fires.

	// A 100 km walk with a 10 cm leg in the middle: the cancellation case, where
	// (target − cum[i-1]) subtracts two numbers agreeing to 6 significant
	// figures and the result is divided by a tiny segLen.
	{
		pts := line(54, -3, 100000, 1000)
		mid := len(pts) / 2
		tail := append([]Point{tjAt(54, -3, float64(mid)*1000+0.1, 0)}, pts[mid+1:]...)
		// The 10 cm leg is edge mid -> mid+1; mid+1 starts the 999.9 m leg after it.
		out = append(out, walk{name: "100 km with a 10 cm leg mid-walk", pts: append(pts[:mid+1:mid+1], tail...),
			focus: []int{25, mid, mid + 1, 96}, namedLeg: &mid})
	}

	// Stationary logging: eight identical fixes part way along, i.e. zero-length
	// legs INSIDE an otherwise ordinary walk.
	//
	// MEASURED: the import collapses the duplicates before commit
	// (109 input points, 101 committed vertices), so the canon this test
	// probes never holds a zero-length leg and the split's flat-cum[] case is
	// not reached from here. The walk is kept, with wantVerts pinning the
	// collapse: if the import ever stops collapsing, the count check fires
	// and the focus must be re-derived to probe the duplicates.
	{
		pts := line(54, -3, 1000, 10)
		dup := make([]Point, 0, len(pts)+8)
		dup = append(dup, pts[:40]...)
		for i := 0; i < 8; i++ {
			dup = append(dup, pts[40])
		}
		// After the collapse, 40 is the one surviving fix: 39 is the edge into
		// it and 40 the edge out of it.
		out = append(out, walk{name: "stationary duplicate vertices", pts: append(dup, pts[40:]...),
			wantVerts: len(pts), focus: []int{20, 39, 40}})
	}

	// Doubling back on itself: measure position stays monotone while lat/lon does
	// not, so a fraction and a position are no longer interchangeable.
	//
	// ⚠ THE RETURN TRACK'S OFFSET IS 20 m, NOT 4 m. At 4 m the walk's own start
	// (x=0,y=0) and end (x=0,y=4) sit within EndpointWarpRadiusM (15 m) of
	// each other, which the merge correctly joins into one ring node and
	// bisects — committing 2 arcs instead of 1, which breaks this test's OWN
	// precondition (`len(committed) != 1`, below). The property under test
	// (measure monotone, lat/lon not) is unaffected by how far apart the two
	// ends sit, so 20 m keeps the doubling-back shape while clearing the
	// radius with margin.
	//
	// ⚠ COVERAGE NOT HERE, STATED PLAINLY. A doubling-back walk whose two ends
	// sit INSIDE EndpointWarpRadiusM is not exercised by this test: that shape
	// is bisected into two arcs sharing one node, and this test's probe
	// machinery (tjRoundTripProbes) operates on a single committed polyline.
	// The round trip across a bisected doubling-back walk is untested here.
	{
		var pts []Point
		for x := 0.0; x <= 500; x += 10 {
			pts = append(pts, tjAt(54, -3, x, 0))
		}
		for x := 500.0; x >= 0; x -= 10 {
			pts = append(pts, tjAt(54, -3, x, 20))
		}
		// 50 is the far end of the outbound track; edge 50 -> 51 is the turn.
		out = append(out, walk{name: "doubles back on itself", pts: pts, focus: []int{25, 50, 51}})
	}

	// Winding: every leg changes bearing, so no two consecutive legs share an
	// interpolation direction.
	{
		var pts []Point
		for i := 0; i <= 500; i++ {
			x := float64(i) * 4
			pts = append(pts, tjAt(54, -3, x, 5*math.Sin(x/20)))
		}
		out = append(out, walk{name: "winding, 2 m amplitude", pts: pts, focus: []int{10, 250, 369}})
	}

	return out
}

// tjWalk is one walk shape for the round-trip test, with the vertices beside
// its special feature.
type tjWalk struct {
	name     string
	pts      []Point
	focus    []int
	namedLeg *int
	// wantVerts, when non-zero, is the committed vertex count where the
	// import does not keep every input point.
	wantVerts int
}

// tjRoundTripProbes returns probe points around a committed walk: beside each
// focus vertex and beside the midpoint of the edge that follows it, at a
// spread of offsets. Offsets stay inside EndpointWarpRadiusM so every sample
// is taken in the regime the gate actually governs.
func tjRoundTripProbes(verts []Point, focus []int) []Point {
	var out []Point
	// A probe beside a walk that already sits on the seam can be pushed past
	// ±180 by the offset. Wrap it rather than clamping: nearestArc
	// rejects an out-of-range probe outright (correctly — R4 guarantees no
	// parsed coordinate reaches it out of range), so an unwrapped fixture would
	// test the input guard instead of the round trip.
	wrap := func(lon float64) float64 {
		for lon > 180 {
			lon -= 360
		}
		for lon < -180 {
			lon += 360
		}
		return lon
	}
	for _, i := range focus {
		for _, off := range []struct{ dLat, dLon float64 }{
			{8 / mPerDegLat, 0},
			{-3 / mPerDegLat, 0},
			{0.5 / mPerDegLat, 0},
			{0, 8 / (mPerDegLat * math.Cos(verts[i].Lat*math.Pi/180))},
		} {
			out = append(out, Point{Lat: verts[i].Lat + off.dLat, Lon: wrap(verts[i].Lon + off.dLon)})
			if i+1 < len(verts) {
				// Beside the midpoint of the following edge, so the projection
				// lands strictly inside a leg rather than on a vertex. Skip the
				// pair that straddles the seam — their midpoint is on the far
				// side of the planet, which is a fixture artefact and not a
				// walk position.
				if math.Abs(verts[i].Lon-verts[i+1].Lon) < 180 {
					out = append(out, Point{
						Lat: (verts[i].Lat+verts[i+1].Lat)/2 + off.dLat,
						Lon: wrap((verts[i].Lon+verts[i+1].Lon)/2 + off.dLon),
					})
				}
			}
		}
	}
	return out
}

// The report, which is the only observability the T-junction split has. One
// case per outcome, through the real split.
func TestTJunctionReport_RecordsEachTerminalsOutcome(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		leg       []Point
		wantSplit bool
		want      TjunctionDecline
	}{
		{"splits a canon interior", tjLegNS(505, -8, 101), true, TjNoDecline},
		{"a node is already in range", append([]Point{mPt(1004, 3)}, tjLegNS(1004, -7, 100)...), false, TjDeclineNodeInRadius},
		{"nothing within the radius", tjLegNS(505, -16, 101), false, TjDeclineNoCanonInRadius},
		// INVERTED when the short-stub decline was lifted, so this
		// splits. The row is kept rather than dropped so a revert of the lift
		// reddens the report path too, not only the pure rule.
		{"the stub would be under MinRun — rule lifted, splits", tjLegNS(20, -8, 101), true, TjNoDecline},
		// The endpoint warp can no longer decline, so a short leg splits too.
		{"a short leg still splits — the endpoint warp can no longer decline it", tjLegNS(505, -8, 11), true, TjNoDecline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := emptyCanon(t)
			addTestLeg(t, ctx, d, "A", warpFixtureA())

			var rep TjunctionReport
			if err := inCanonCopy(t, d, func(c *MemCanon) error {
				var err error
				rep, err = splitCanonAtLegTerminals(ctx, c, tc.leg)
				return err
			}); err != nil {
				t.Fatalf("transaction failed: %v", err)
			}

			if rep.Start.Split != tc.wantSplit || rep.Start.Decline != tc.want {
				t.Fatalf("first terminal: split=%v decline=%q, want split=%v decline=%q",
					rep.Start.Split, rep.Start.Decline, tc.wantSplit, tc.want)
			}
			// Every one of these legs runs away from canon, so the last
			// terminal never finds anything.
			if rep.End.Split || rep.End.Decline != TjDeclineNoCanonInRadius {
				t.Fatalf("last terminal: split=%v decline=%q, want no candidate", rep.End.Split, rep.End.Decline)
			}
			// The measurements the report exists to carry are only populated
			// when a candidate was actually found.
			switch tc.want {
			case TjDeclineNodeInRadius, TjDeclineNoCanonInRadius:
				if rep.Start.ArcID != 0 {
					t.Fatalf("no candidate was searched for, but the report names arc %d", rep.Start.ArcID)
				}
			default:
				if rep.Start.ArcID == 0 || rep.Start.ProjDistM <= 0 || rep.Start.StubLenM <= 0 {
					t.Fatalf("a candidate was found but the report is empty: %+v", rep.Start)
				}
			}
		})
	}
}

func TestTJunction_DeclinesWhenANodeSitsBesideTheProjection(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Canon A: 1000 m due east along y=0, nodes at its two ends only.
	addTestLeg(t, ctx, d, "A", warpFixtureA())

	// N sits 10 m SOUTH of A at x=500 — off canon, so it is nobody's endpoint.
	// Placed directly rather than grown from a leg: every leg that could mint a
	// node this close to A's interior gets its terminal warped ONTO A first, so
	// there is no import that produces this arrangement on this fixture. The
	// arrangement itself is real — on real data it arrives as an earlier leg's
	// terminal and as an interior cut.
	const nOffsetM = -10
	nPt := mPt(500, nOffsetM)
	nID, err := d.AddNode(ctx, nPt.Lat, nPt.Lon, nil)
	if err != nil {
		t.Fatalf("place N: %v", err)
	}
	n := Node{ID: nID, Lat: nPt.Lat, Lon: nPt.Lon}

	// The terminal sits 14 m NORTH of A at the same x. Its projection is (500,0),
	// 10 m from N — inside the radius. The terminal itself is 24 m from N —
	// outside it.
	term := mPt(500, 14)

	// LIVENESS. Without both of these the test would pass for the wrong reason:
	// if the terminal were inside the radius of N, step 1 would decline and step
	// 5 would never be reached; if the projection were outside it, there would be
	// nothing to decline.
	if dTerm := HaversineMetres(term.Lat, term.Lon, n.Lat, n.Lon); dTerm <= EndpointWarpRadiusM {
		t.Fatalf("fixture is stale: the terminal is %.3f m from N, inside the %v m radius — "+
			"step 1 would swallow this case and step 5 would never run", dTerm, EndpointWarpRadiusM)
	}
	proj := mPt(500, 0)
	if dProj := HaversineMetres(proj.Lat, proj.Lon, n.Lat, n.Lon); dProj > EndpointWarpRadiusM {
		t.Fatalf("fixture is stale: the projection is %.3f m from N, outside the %v m radius — "+
			"there is nothing for step 5 to decline", dProj, EndpointWarpRadiusM)
	}

	before := censusNetwork(t, ctx, d)

	got, err := splitCanonAtTerminal(ctx, d, term)
	if err != nil {
		t.Fatalf("splitCanonAtTerminalTx: %v", err)
	}
	if got.Split {
		t.Fatalf("the T-junction split cut canon and minted node %d beside N (%d), %.3f m away — "+
			"that is the one-node violation this rule exists to stop",
			got.NodeID, n.ID, HaversineMetres(proj.Lat, proj.Lon, n.Lat, n.Lon))
	}
	if got.Decline != TjDeclineNodeNearProjection {
		t.Fatalf("declined for %q, want %q", got.Decline, TjDeclineNodeNearProjection)
	}
	// It must have got as far as choosing a candidate — a decline for "no canon
	// in radius" would also leave canon alone and would mean nothing.
	if got.ArcID == 0 {
		t.Fatalf("no candidate arc was recorded — the decline did not come from step 5")
	}

	after := censusNetwork(t, ctx, d)
	if after.Arcs != before.Arcs || after.Nodes != before.Nodes {
		t.Fatalf("canon moved: %d arcs / %d nodes -> %d / %d",
			before.Arcs, before.Nodes, after.Arcs, after.Nodes)
	}
	for id, h := range before.ArcHash {
		if got, ok := after.ArcHash[id]; !ok || got != h {
			t.Fatalf("committed arc %d changed: %s -> %s (present=%v)", id, h, got, ok)
		}
	}
}

// TestWarpUnresolvedLegTerminalsTx_AdoptsAndTapers is the direct unit
// Prove-It: given an existing node within radius of an unresolved leg
// terminal, the function adopts it (bit-exact join, same postcondition
// warpLegEndpoints itself enforces) and TAPERS the approach — not a jump.
// Asserts two things: pips behind the
// adopted terminal move PROGRESSIVELY LESS, and a pip far enough back does
// not move AT ALL (bit-identical to its source coordinate).
func TestWarpUnresolvedLegTerminalsTx_AdoptsAndTapers(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Mint a real, committed node to adopt onto — far from everything else
	// in this test, the same shape a node the align step just minted for this leg has.
	targetAt := mPt(1000, 1000)
	far := Point{Lat: targetAt.Lat + 1, Lon: targetAt.Lon} // ~111 km away
	anchorArc, err := addArc(ctx, d, []Point{targetAt, far})
	if err != nil {
		t.Fatalf("mint throwaway anchor node: %v", err)
	}
	targetNode := anchorArc.StartNodeID

	// The leg under test: pts[0] sits ~7.8 m from targetAt (unresolved on
	// entry — rep is empty), then runs on for well over 39 m of measure (the
	// zone this displacement produces: 5x * 7.8m ≈ 39m, widenedZoneFactor
	// from warp_core.go) so pts[2] sits OUTSIDE the taper zone and must be
	// untouched, while pts[1] sits inside it and must show SOME but LESS
	// displacement than pts[0]'s full jump-to-target. The far end (pts[5])
	// stays far from anything, so it must resolve to no adoption at all.
	pts := []Point{
		mPt(1005, 1006), // ~7.81 m from targetAt
		mPt(1020, 1006), // 15 m further along — inside the ~39 m zone
		mPt(1060, 1000), // ~55.4 m cumulative — outside the zone, untouched
		mPt(1100, 1000),
		mPt(1150, 1000),
		mPt(1200, 1000), // far end, nothing nearby
	}

	out, rep, _, err := warpUnresolvedLegTerminals(ctx, d, pts, WarpReport{}, false)
	if err != nil {
		t.Fatalf("warpUnresolvedLegTerminalsTx: %v", err)
	}

	// Adoption: the start terminal joined the existing node, bit-exact.
	if rep.StartNode == nil || *rep.StartNode != targetNode {
		t.Fatalf("rep.StartNode = %v, want %d — the nearby node must be adopted", rep.StartNode, targetNode)
	}
	if math.Float64bits(out[0].Lat) != math.Float64bits(targetAt.Lat) ||
		math.Float64bits(out[0].Lon) != math.Float64bits(targetAt.Lon) {
		t.Errorf("out[0] = (%v,%v), want bit-exact target (%v,%v)", out[0].Lat, out[0].Lon, targetAt.Lat, targetAt.Lon)
	}
	if rep.EndNode != nil {
		t.Errorf("rep.EndNode = %v, want nil — the far end has nothing within radius", rep.EndNode)
	}

	// THE TAPER, NOT A JUMP.
	disp := func(i int) float64 {
		return HaversineMetres(pts[i].Lat, pts[i].Lon, out[i].Lat, out[i].Lon)
	}
	d0, d1, d2 := disp(0), disp(1), disp(2)
	if d0 <= 0 {
		t.Fatalf("disp(0) = %v, want > 0 — the terminal itself must have moved to adopt the node", d0)
	}
	if !(d1 > 0 && d1 < d0) {
		t.Errorf("disp(1) = %v, want strictly between 0 and disp(0)=%v — an untapered jump would leave every"+
			" pip at the SAME (near-15m) displacement as the terminal, or zero; this must show partial movement", d1, d0)
	}
	if d2 != 0 {
		t.Errorf("disp(2) = %v, want exactly 0 — pts[2] sits outside the taper zone and must be bit-untouched", d2)
	}
	if math.Float64bits(out[2].Lat) != math.Float64bits(pts[2].Lat) ||
		math.Float64bits(out[2].Lon) != math.Float64bits(pts[2].Lon) {
		t.Errorf("out[2] is not bit-identical to pts[2] — a point outside the zone must be untouched, not merely close")
	}
	// Everything further back is also untouched.
	for i := 3; i < len(pts); i++ {
		if math.Float64bits(out[i].Lat) != math.Float64bits(pts[i].Lat) ||
			math.Float64bits(out[i].Lon) != math.Float64bits(pts[i].Lon) {
			t.Errorf("out[%d] moved but sits well outside the taper zone — want bit-identical to pts[%d]", i, i)
		}
	}
}

// TestWarpUnresolvedLegTerminalsTx_LeavesResolvedSidesAlone proves the
// function never re-touches a side the endpoint warp already resolved — only
// the nil
// side(s) get a lookup at all.
func TestWarpUnresolvedLegTerminalsTx_LeavesResolvedSidesAlone(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	fixedID := NodeID(999999) // does not exist — proves it is never looked up or overwritten
	pts := []Point{
		mPt(2000, 2000), // far from anything real
		mPt(2050, 2000),
		mPt(2100, 2000),
	}
	rep := WarpReport{StartNode: &fixedID}

	var calls []string
	hctx := WithTestHooks(ctx, TestHooks{
		LegTerminalGap: func(leg, side string, found bool, nearbyID NodeID, nearbyDistM float64) {
			calls = append(calls, side)
		},
	})

	out, gotRep, _, err := warpUnresolvedLegTerminals(hctx, d, pts, rep, false)
	if err != nil {
		t.Fatalf("warpUnresolvedLegTerminalsTx: %v", err)
	}
	if gotRep.StartNode == nil || *gotRep.StartNode != fixedID {
		t.Errorf("rep.StartNode = %v, want the untouched fixed id %d", gotRep.StartNode, fixedID)
	}
	if gotRep.EndNode != nil {
		t.Errorf("rep.EndNode = %v, want nil — nothing is near the far end", gotRep.EndNode)
	}
	for _, side := range calls {
		if side == "start" {
			t.Errorf("a lookup ran for the START side, which was already resolved by the caller — it must never be re-touched")
		}
	}
	if math.Float64bits(out[0].Lat) != math.Float64bits(pts[0].Lat) || math.Float64bits(out[0].Lon) != math.Float64bits(pts[0].Lon) {
		t.Errorf("out[0] moved even though its side was already fixed and nothing new was found for the other side")
	}
}

// TestWarpUnresolvedLegTerminalsTx_MultiPieceGeneralisation is the Prove-It
// for the multi-piece case: a MULTI-piece leg's own two far ends, both still
// unresolved, sitting within radius of EACH OTHER (not of any existing
// third-party node).
//
// This is a real leg's measured shape reduced to a fixture: a single 28-arc
// leg whose own start and end sit 5.98 m apart. Confirmed RED against this
// exact shape before the multi-piece case was added: calling
// warpUnresolvedLegTerminals with multiPiece=true on this fixture left BOTH
// ends nil, and commitLegPieces then minted two SEPARATE nodes 7.07 m apart.
func TestWarpUnresolvedLegTerminalsTx_MultiPieceGeneralisation(t *testing.T) {
	ctx := context.Background()
	d := emptyCanon(t)

	// Mint the interior seam's own node first, 50m from the leg's own
	// start/end -- far outside EndpointWarpRadiusM, so it plays no part in
	// the close-ends question below (the same fixture shape as the
	// close-ends cycle test).
	anchor, seamNode, _, _, _, _, _, _, _, _, err := commitPiece(ctx, d, []Point{mPt(-50, 0), mPt(0, 50)}, nil, nil, nil)
	if err != nil {
		t.Fatalf("anchor piece (interior seam node): %v", err)
	}
	if len(anchor) != 1 {
		t.Fatalf("anchor piece committed %d arc(s), want 1", len(anchor))
	}

	// The leg's own points: idx0 is the TRUE start (unresolved), idx1 is
	// the interior seam (HasNode, at seamNode), idx2 is the TRUE end
	// (unresolved, 7.07m from idx0 -- well within EndpointWarpRadiusM, but
	// NOT from the seam node). Departs the radius via idx1 (50m out from
	// idx0), same shape mergeOwnEnds' own LegDepartsRadius gate requires.
	pts := []Point{
		mPt(0, 0),  // idx0: leg start terminal
		mPt(0, 50), // idx1: interior seam, HasNode, at seamNode
		mPt(5, 5),  // idx2: leg end terminal, 7.07m from idx0
	}
	seams := []legSeam{{Idx: 1, HasNode: true, Node: seamNode}}

	out, rep, ringMerged, err := warpUnresolvedLegTerminals(ctx, d, pts, WarpReport{}, true)
	if err != nil {
		t.Fatalf("warpUnresolvedLegTerminalsTx: %v", err)
	}
	if !ringMerged {
		t.Fatalf("ringMerged = false, want true -- the leg's own two far ends are 7.07m apart and the leg genuinely departs the radius via idx1")
	}
	if rep.StartNode == nil || rep.EndNode == nil || *rep.StartNode != *rep.EndNode {
		t.Fatalf("rep = {Start:%v End:%v}, want both non-nil and EQUAL -- one shared node", rep.StartNode, rep.EndNode)
	}

	// pts[0] itself must be UNTOUCHED -- the shared node was minted AT its
	// own coordinate, zero displacement, not a jump.
	if math.Float64bits(out[0].Lat) != math.Float64bits(pts[0].Lat) || math.Float64bits(out[0].Lon) != math.Float64bits(pts[0].Lon) {
		t.Errorf("out[0] moved -- the shared node must be minted at the start terminal's OWN coordinate so that side needs no taper at all")
	}
	// The minted node's row must actually carry pts[0]'s coordinate.
	n, _, err := d.Node(ctx, *rep.StartNode)
	if err != nil {
		t.Fatalf("load minted node: %v", err)
	}
	if math.Float64bits(n.Lat) != math.Float64bits(pts[0].Lat) || math.Float64bits(n.Lon) != math.Float64bits(pts[0].Lon) {
		t.Errorf("minted node (%v,%v) != pts[0] (%v,%v) -- must be bit-exact", n.Lat, n.Lon, pts[0].Lat, pts[0].Lon)
	}
	// The end terminal must land bit-exact on the shared node (the same
	// postcondition warpLegEndpoints enforces for every other adoption).
	if math.Float64bits(out[2].Lat) != math.Float64bits(n.Lat) || math.Float64bits(out[2].Lon) != math.Float64bits(n.Lon) {
		t.Errorf("out[2] = (%v,%v), want bit-exact on the shared node (%v,%v)", out[2].Lat, out[2].Lon, n.Lat, n.Lon)
	}

	// Feed this through commitLegPieces exactly as production would and
	// confirm the SAME outcome the reverted commit's own test proved at
	// the writer: a closed CYCLE across two arcs, no self-loop in
	// either one.
	pieces, _, ringSplits, ringMergedTerminals, _, _, _, _, _, err := commitLegPieces(ctx, d, out, seams, nil, rep.StartNode, rep.EndNode)
	if err != nil {
		t.Fatalf("commitLegPiecesTx: %v", err)
	}
	if ringSplits != 0 || ringMergedTerminals != 0 {
		t.Errorf("ringSplits=%d ringMergedTerminals=%d, want 0 and 0 -- the shared node was already resolved before commitLegPiecesTx ran, so DECISION-2's own single-piece ring-split logic must not fire again for either piece",
			ringSplits, ringMergedTerminals)
	}
	if len(pieces) != 2 {
		t.Fatalf("committed %d piece(s), want 2", len(pieces))
	}
	// ⚠ THE WALKED ENDS, NOT THE ROW'S OWN. Every claim below is
	// about the LEG'S PATH — where it enters and leaves each piece — and a
	// piece may now be a canon row this leg rides BACKWARDS, in which case the
	// row's stored start is the leg's exit. walkStart/walkEnd answer the
	// question actually being asked; Arc.StartNodeID answers a different one.
	first, second := pieces[0], pieces[1]
	if first.WalkStart() == first.WalkEnd() {
		t.Errorf("piece 1 (arc %d) begins and ends on node %d -- a self-loop in ONE row", first.Arc.ID, first.WalkStart())
	}
	if second.WalkStart() == second.WalkEnd() {
		t.Errorf("piece 2 (arc %d) begins and ends on node %d -- a self-loop in ONE row", second.Arc.ID, second.WalkStart())
	}
	if first.WalkStart() != second.WalkEnd() {
		t.Errorf("piece 1 starts on node %d, piece 2 ends on node %d -- want the SAME shared node", first.WalkStart(), second.WalkEnd())
	}
	if first.WalkEnd() != seamNode || second.WalkStart() != seamNode {
		t.Errorf("the seam node is %d, but piece 1 ends on %d and piece 2 starts on %d -- want both to name the seam", seamNode, first.WalkEnd(), second.WalkStart())
	}
}
