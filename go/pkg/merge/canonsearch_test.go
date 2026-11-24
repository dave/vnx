package merge

import (
	"context"
	"errors"
	"math"
	"testing"
)

// mustAddNode adds a node at (lat, lon) and returns its ID.
func mustAddNode(t *testing.T, ctx context.Context, c *MemCanon, lat, lon float64) NodeID {
	t.Helper()
	id, err := c.AddNode(ctx, lat, lon, nil)
	if err != nil {
		t.Fatalf("AddNode(%v, %v): %v", lat, lon, err)
	}
	return id
}

// mustAddNodeOffset adds a node `distM` metres from (lat, lon) along
// `bearingDeg`. Go cannot spread destPoint's two returns into mustAddNode's
// trailing pair, and the offset is what every case here actually wants to
// say.
func mustAddNodeOffset(t *testing.T, ctx context.Context, c *MemCanon, lat, lon, bearingDeg, distM float64) NodeID {
	t.Helper()
	nLat, nLon := destPoint(lat, lon, bearingDeg, distM)
	return mustAddNode(t, ctx, c, nLat, nLon)
}

// addArc stores one arc through PrepareArc and AddArc, minting both end
// nodes, as an import's new arc is stored.
func addArc(ctx context.Context, c *MemCanon, points []Point) (AddedArc, error) {
	a, err := PrepareArc(ctx, c, points, nil, nil)
	if err != nil {
		return AddedArc{}, err
	}
	return c.AddArc(ctx, a)
}

// addCanonArc is addArc for a fixture that cannot fail; it returns the arc
// ID.
func addCanonArc(t *testing.T, c *MemCanon, points []Point) ArcID {
	t.Helper()
	a, err := addArc(context.Background(), c, points)
	if err != nil {
		t.Fatalf("addArc: %v", err)
	}
	return a.ID
}

// canonPts builds a vertex walk from bare lat/lon pairs, with no elevation.
func canonPts(coords ...[2]float64) []Point {
	out := make([]Point, len(coords))
	for i, c := range coords {
		out[i] = Point{Lat: c[0], Lon: c[1]}
	}
	return out
}

// canonArcIDs is the IDs of arcs, in order, for a failure message.
func canonArcIDs(arcs []CanonArc) []ArcID {
	out := make([]ArcID, len(arcs))
	for i, a := range arcs {
		out[i] = a.ID
	}
	return out
}

// findNearby runs [NearestNode] and returns the node's ID.
func findNearby(t *testing.T, ctx context.Context, c *MemCanon, lat, lon, radiusM float64) (NodeID, bool, error) {
	t.Helper()
	n, ok, err := findNearbyNode(t, ctx, c, lat, lon, radiusM)
	return n.ID, ok, err
}

// findNearbyNode is the same, returning the whole [NearbyNode] for the
// tests that care about the coordinates and distance rather than just the ID.
func findNearbyNode(t *testing.T, ctx context.Context, c *MemCanon, lat, lon, radiusM float64) (NearbyNode, bool, error) {
	t.Helper()
	return NearestNode(ctx, c, lat, lon, radiusM, nil)
}

// findNearbyCanon runs [NearestArc].
func findNearbyCanon(t *testing.T, ctx context.Context, c *MemCanon, lat, lon, radiusM float64) (NearbyCanonArc, bool, error) {
	t.Helper()
	return NearestArc(ctx, c, lat, lon, radiusM)
}

// nodesNearPathIn runs [NodesNearPath].
func nodesNearPathIn(t *testing.T, ctx context.Context, c *MemCanon, verts []Point, radiusM float64, exclude []NodeID) ([]NodeNearPath, error) {
	t.Helper()
	return NodesNearPath(ctx, c, verts, radiusM, exclude)
}

// The coordinates come back EXACTLY as stored. The endpoint warp copies them
// onto an incoming endpoint to make the two bitwise coincident, so a value
// that had been round-tripped through any formula — or recomputed from the
// distance and bearing — would silently defeat the node identity test the
// merge is built on.
func TestCanonNearestNode_ReturnsStoredCoordinatesBitwise(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	// Deliberately awkward coordinates: full float64 precision, not the
	// round numbers the other tests use.
	const wantLat = 56.798041234567891
	const wantLon = -5.019987654321098
	id := mustAddNode(t, ctx, d, wantLat, wantLon)

	// Probe from a few metres away so the search has to do real work.
	pLat, pLon := destPoint(wantLat, wantLon, 37, 4)
	got, ok, err := findNearbyNode(t, ctx, d, pLat, pLon, 10)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if !ok {
		t.Fatal("expected to find the node 4 m away")
	}
	if got.ID != id {
		t.Fatalf("got node %d, want %d", got.ID, id)
	}
	if math.Float64bits(got.Lat) != math.Float64bits(wantLat) {
		t.Fatalf("lat is %v (%#x), want exactly %v (%#x)",
			got.Lat, math.Float64bits(got.Lat), wantLat, math.Float64bits(wantLat))
	}
	if math.Float64bits(got.Lon) != math.Float64bits(wantLon) {
		t.Fatalf("lon is %v (%#x), want exactly %v (%#x)",
			got.Lon, math.Float64bits(got.Lon), wantLon, math.Float64bits(wantLon))
	}
	if got.DistM <= 0 || got.DistM > 10 {
		t.Fatalf("DistM is %v, expected roughly 4 m", got.DistM)
	}
}

// TestCanonNearestNode_NearestWins pins the tie-break: with three
// nodes inside the radius, the closest one is returned — not the first
// row the query happens to yield (which, without an ORDER BY, is
// insertion order, so the fixture deliberately inserts the far one
// first).
func TestCanonNearestNode_NearestWins(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0

	farLat, farLon := destPoint(probeLat, probeLon, 10, 9.0)
	midLat, midLon := destPoint(probeLat, probeLon, 200, 5.0)
	nearLat, nearLon := destPoint(probeLat, probeLon, 300, 1.5)

	mustAddNode(t, ctx, d, farLat, farLon)
	mustAddNode(t, ctx, d, midLat, midLon)
	want := mustAddNode(t, ctx, d, nearLat, nearLon)

	got, ok, err := findNearby(t, ctx, d, probeLat, probeLon, 10)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if !ok {
		t.Fatalf("expected a hit, got none")
	}
	if got != want {
		t.Errorf("nearest node: got %d want %d", got, want)
	}
}

// TestCanonNearestNode_EmptyNetwork verifies the not-found shape on
// a network with no nodes at all: (0, false, nil) — not an error.
func TestCanonNearestNode_EmptyNetwork(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	got, ok, err := findNearby(t, ctx, d, 54.4, -3.0, 10)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if ok || got != 0 {
		t.Errorf("empty network: got (%d, %v) want (0, false)", got, ok)
	}
}

// TestCanonNearestNode_OutsideRadius verifies that a node which
// clears the bounding box but fails the haversine refine is rejected —
// the refine is the decision, not the box.
func TestCanonNearestNode_OutsideRadius(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0
	// 14 m away on a diagonal: inside the 10 m box's CORNER
	// (10 m north-east ≈ 14.1 m of diagonal reach) but outside the 10 m
	// disc, so only the refine can reject it.
	nLat, nLon := destPoint(probeLat, probeLon, 45, 14.0)
	mustAddNode(t, ctx, d, nLat, nLon)

	got, ok, err := findNearby(t, ctx, d, probeLat, probeLon, 10)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if ok || got != 0 {
		t.Errorf("node outside radius: got (%d, %v) want (0, false)", got, ok)
	}
}

// TestCanonNearestNode_RadiusBoundaryInclusive pins the boundary
// convention: a node at EXACTLY radiusM qualifies (`d <= radiusM`), and
// the same node one ULP of radius short does not. The fixture measures
// the node's true distance rather than assuming destPoint's 10 m came
// back as bit-exactly 10 m.
func TestCanonNearestNode_RadiusBoundaryInclusive(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0
	nLat, nLon := destPoint(probeLat, probeLon, 45, 10.0)
	want := mustAddNode(t, ctx, d, nLat, nLon)

	exact := HaversineMetres(probeLat, probeLon, nLat, nLon)

	got, ok, err := findNearby(t, ctx, d, probeLat, probeLon, exact)
	if err != nil {
		t.Fatalf("NearestNode(exact): %v", err)
	}
	if !ok || got != want {
		t.Errorf("node at exactly radiusM: got (%d, %v) want (%d, true)", got, ok, want)
	}

	justShort := math.Nextafter(exact, 0)
	got, ok, err = findNearby(t, ctx, d, probeLat, probeLon, justShort)
	if err != nil {
		t.Fatalf("NearestNode(justShort): %v", err)
	}
	if ok || got != 0 {
		t.Errorf("node one ULP outside radiusM: got (%d, %v) want (0, false)", got, ok)
	}
}

// TestCanonNearestNode_InvalidInputErrors covers the no-fallback
// rule: nonsensical input ERRORS rather than quietly reporting
// "nothing found", which would look identical to a clean miss at the
// call site.
func TestCanonNearestNode_InvalidInputErrors(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()
	mustAddNode(t, ctx, d, 54.4, -3.0)

	cases := []struct {
		name             string
		lat, lon, radius float64
	}{
		{"zero radius", 54.4, -3.0, 0},
		{"negative radius", 54.4, -3.0, -1},
		{"NaN radius", 54.4, -3.0, math.NaN()},
		{"Inf radius", 54.4, -3.0, math.Inf(1)},
		{"lat above pole", 90.5, -3.0, 10},
		{"lat below pole", -90.5, -3.0, 10},
		{"NaN lat", math.NaN(), -3.0, 10},
		{"lon past antimeridian", 54.4, 180.5, 10},
		{"NaN lon", 54.4, math.NaN(), 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := findNearby(t, ctx, d, tc.lat, tc.lon, tc.radius)
			if err == nil {
				t.Fatalf("expected an error, got (%d, %v, nil)", got, ok)
			}
			if got != 0 || ok {
				t.Errorf("error case must return the zero result: got (%d, %v)", got, ok)
			}
		})
	}
}

// TestCanonNearestNode_AcrossAntimeridian is the end-to-end
// counterpart of the box property test for the ±180 seam: two points
// ~15 m apart whose longitudes differ by ~360°. A plain
// `lon BETWEEN lon-d AND lon+d` predicate around the probe would exclude
// the node outright, and the haversine refine never gets to see it.
func TestCanonNearestNode_AcrossAntimeridian(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat = 12.0
	probeLon := 179.99995 // ~5.4 m west of the seam at this latitude
	nLat, nLon := destPoint(probeLat, probeLon, 90, 15.0)
	if nLon > 0 {
		t.Fatalf("fixture is wrong: node at lon %v did not cross the seam", nLon)
	}
	want := mustAddNode(t, ctx, d, nLat, nLon)

	got, ok, err := findNearby(t, ctx, d, probeLat, probeLon, 20)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if !ok || got != want {
		t.Errorf("node across the antimeridian: got (%d, %v) want (%d, true)", got, ok, want)
	}
}

// TestCanonNearestNode_AcrossPole is the same end-to-end check for
// the polar singularity: a probe a metre from the north pole and a node
// on the far side of it, 2 m away in metric terms but 180° away in
// longitude. Any finite longitude half-extent excludes it; only widening
// to the whole world lets the refine decide.
func TestCanonNearestNode_AcrossPole(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	probeLat, probeLon := destPoint(90, 0, 0, 1.0) // 1 m from the pole
	nLat, nLon := destPoint(probeLat, probeLon, 0, 2.0)
	if math.Abs(nLon-probeLon) < 90 {
		t.Fatalf("fixture is wrong: node at lon %v did not cross the pole", nLon)
	}
	want := mustAddNode(t, ctx, d, nLat, nLon)

	got, ok, err := findNearby(t, ctx, d, probeLat, probeLon, 10)
	if err != nil {
		t.Fatalf("NearestNode: %v", err)
	}
	if !ok || got != want {
		t.Errorf("node across the pole: got (%d, %v) want (%d, true)", got, ok, want)
	}
}

// TestCanonNearestArc_NearestWins: with three arcs inside the
// radius the closest one is returned, not the first row the rtree happens
// to yield — the fixture commits the far one first so id order and distance
// order disagree.
func TestCanonNearestArc_NearestWins(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0

	// Three parallel east-west arcs north of the probe, committed
	// furthest-first.
	eastWest := func(lat float64) []Point {
		return canonPts([2]float64{lat, probeLon - 0.001}, [2]float64{lat, probeLon + 0.001})
	}
	addCanonArc(t, d, eastWest(probeLat+0.00009)) // ~10 m
	addCanonArc(t, d, eastWest(probeLat+0.00005)) // ~5.6 m
	want := addCanonArc(t, d, eastWest(probeLat+0.00002))

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 15)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if !ok {
		t.Fatal("expected a hit")
	}
	if got.ID != want {
		t.Errorf("nearest arc: got %d want %d (DistM %v)", got.ID, want, got.DistM)
	}
	if got.DistM > 3 {
		t.Errorf("DistM = %v, want ~2.2 m", got.DistM)
	}
	// The projection lands mid-arc, so the caller gets a splittable
	// fraction and two non-zero halves.
	if got.Fraction <= 0 || got.Fraction >= 1 {
		t.Errorf("Fraction = %v, want strictly inside (0, 1)", got.Fraction)
	}
	if got.PriorLenM <= 0 || got.NextLenM <= 0 {
		t.Errorf("halves = (%v, %v), want both positive", got.PriorLenM, got.NextLenM)
	}
}

// TestCanonNearestArc_TieGoesToLowestArcID pins the tie-break.
// Two arcs with IDENTICAL geometry make the distances bit-identical by
// construction rather than by luck, so the only thing deciding the winner
// is the comparison operator: `<` keeps the first candidate and
// ArcsNearPoint orders by id ascending, so the lower id wins. Under
// `<=` the later, higher id would replace it and this test fails.
//
// Two coincident arcs are a real state, not a contrived one: the
// endpoint-uniqueness constraint was relaxed precisely so parallel routes
// could share endpoints.
func TestCanonNearestArc_TieGoesToLowestArcID(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0
	geom := canonPts(
		[2]float64{probeLat + 0.00005, probeLon - 0.001},
		[2]float64{probeLat + 0.00005, probeLon + 0.001},
	)
	lower := addCanonArc(t, d, geom)
	higher := addCanonArc(t, d, geom)
	if higher <= lower {
		t.Fatalf("fixture is wrong: second arc id %d is not above the first %d", higher, lower)
	}

	// Guard against a vacuous pass: both arcs must actually be in the
	// candidate set, or "lowest id wins" would hold trivially.
	cands, err := d.ArcsInRect(ctx, Bounds{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180})
	if err != nil {
		t.Fatalf("ArcsInRect: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidate set = %v, want both arcs", canonArcIDs(cands))
	}

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 15)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if !ok {
		t.Fatal("expected a hit")
	}
	if got.ID != lower {
		t.Errorf("tie went to arc %d, want the LOWEST id %d (strict `<`, not `<=`)", got.ID, lower)
	}
}

// TestCanonNearestArc_ZeroLengthCandidateIsSkippedNotRaised is a
// REGRESSION test for a live abort, not a hypothetical.
//
// A GPX route of two or more identical points commits today as an arc
// whose whole vertex walk is one coincident point. When the T-junction
// search went live, such an arc sitting in a later terminal's search BOX but
// outside the 15 m disc raised out of the projection and aborted an
// unrelated, entirely valid import. Inside the disc it was always harmless — the degenerate arc's
// own endpoints are nodes, so the node probe declines first — so the blast
// radius was exactly that box-minus-disc corner, which is why no corpus sweep
// found it and a constructed case did.
//
// The fixture puts the degenerate arc in the corner deliberately: near
// enough to be returned by the rtree, far enough that it is not within the
// radius. A good candidate sits within radius, so the assertion is not merely
// "no error" but "the real answer still comes back".
func TestCanonNearestArc_ZeroLengthCandidateIsSkippedNotRaised(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0

	// Degenerate: every vertex coincident, placed in the BOX CORNER — the only
	// region where this ever bit.
	//
	// NodeSearchBox is a lat/lon RECTANGLE circumscribing the 15 m disc, so at
	// this latitude it spans about ±0.000135 deg lat (15 m) and ±0.000231 deg
	// lon (15 m / (111320 * cos 54.4)). A point near the corner is inside the
	// rectangle — so the rtree returns it as a candidate — while its true
	// distance is ~sqrt(2) x 15 m and it is comfortably outside the disc.
	//
	// Getting this wrong is how the FIRST version of this test passed against
	// the unfixed code: placing the arc 22 m due east puts it outside the
	// BOX as well as the disc, so the rtree never returned it, nothing was ever
	// projected, and the test asserted nothing. Mutation caught that.
	degenerate := [2]float64{probeLat + 0.00012, probeLon + 0.00020}
	if _, err := addArc(ctx, d, canonPts(
		degenerate, degenerate, degenerate,
	)); err != nil {
		t.Fatalf("commit degenerate arc: %v", err)
	}

	// Ordinary canon ~5.5 m north of the probe: comfortably within radius.
	good, err := addArc(ctx, d, canonPts(
		[2]float64{probeLat + 0.00005, probeLon - 0.001},
		[2]float64{probeLat + 0.00005, probeLon + 0.001},
	))
	if err != nil {
		t.Fatalf("commit good arc: %v", err)
	}

	got, ok, err := NearestArc(ctx, d, probeLat, probeLon, 15)
	if err != nil {
		t.Fatalf("NearestArc raised on a zero-length candidate: %v\n"+
			"An arc that is geometrically a POINT has no interior to T onto, so it is "+
			"not a candidate — but it IS legal committed geometry, and raising here aborts "+
			"a valid import over an arc that was never going to be chosen.", err)
	}
	if !ok || got.ID != good.ID {
		t.Fatalf("got (%d, %v), want (%d, true) — the degenerate candidate must be skipped "+
			"WITHOUT costing the real answer", got.ID, ok, good.ID)
	}
}

// TestCanonNearestArc_RadiusBoundaryInclusive pins the ratified
// boundary: a projection at EXACTLY radiusM qualifies, one ULP short does
// not. The fixture measures the true distance first rather than assuming
// the geometry produced a round number.
func TestCanonNearestArc_RadiusBoundaryInclusive(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0
	want := addCanonArc(t, d, canonPts(
		[2]float64{probeLat + 0.0001, probeLon - 0.001},
		[2]float64{probeLat + 0.0001, probeLon + 0.001},
	))

	measured, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 100)
	if err != nil {
		t.Fatalf("NearestArc(measure): %v", err)
	}
	if !ok {
		t.Fatal("measuring pass found nothing")
	}
	exact := measured.DistM

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, exact)
	if err != nil {
		t.Fatalf("NearestArc(exact): %v", err)
	}
	if !ok || got.ID != want {
		t.Errorf("projection at exactly radiusM: got (%d, %v), want (%d, true)", got.ID, ok, want)
	}

	justShort := math.Nextafter(exact, 0)
	got, ok, err = findNearbyCanon(t, ctx, d, probeLat, probeLon, justShort)
	if err != nil {
		t.Fatalf("NearestArc(justShort): %v", err)
	}
	if ok || got.ID != 0 {
		t.Errorf("projection one ULP outside radiusM: got (%d, %v), want (0, false)", got.ID, ok)
	}
}

// TestCanonNearestArc_EmptyNetwork: no candidates is a clean miss,
// not an error.
func TestCanonNearestArc_EmptyNetwork(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	got, ok, err := findNearbyCanon(t, ctx, d, 54.4, -3.0, 15)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if ok || got.ID != 0 {
		t.Errorf("empty network: got (%d, %v), want (0, false)", got.ID, ok)
	}
}

// TestCanonNearestArc_InvalidInputErrors is the no-fallback rule at
// the entry point, matching NearestNode case for case: a
// nonsense query must not be reported as "nothing nearby", because the
// caller cannot tell the two apart.
func TestCanonNearestArc_InvalidInputErrors(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()
	addCanonArc(t, d, canonPts([2]float64{54.4001, -3.001}, [2]float64{54.4001, -2.999}))

	cases := []struct {
		name             string
		lat, lon, radius float64
	}{
		{"zero radius", 54.4, -3.0, 0},
		{"negative radius", 54.4, -3.0, -1},
		{"NaN radius", 54.4, -3.0, math.NaN()},
		{"Inf radius", 54.4, -3.0, math.Inf(1)},
		{"lat above pole", 90.5, -3.0, 15},
		{"lat below pole", -90.5, -3.0, 15},
		{"NaN lat", math.NaN(), -3.0, 15},
		{"lon past antimeridian", 54.4, 180.5, 15},
		{"NaN lon", 54.4, math.NaN(), 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := findNearbyCanon(t, ctx, d, tc.lat, tc.lon, tc.radius)
			if err == nil {
				t.Fatalf("expected an error, got (%d, %v, nil)", got.ID, ok)
			}
			if got.ID != 0 || ok {
				t.Errorf("error case must return the zero result: got (%d, %v)", got.ID, ok)
			}
		})
	}
}

// TestCanonNearestArc_AcrossAntimeridian is the seam hazard end to
// end: the query box must widen to the whole world in longitude (as
// NodeSearchBox does) or the rtree never offers the candidate, and the
// projection must wrap the delta or the candidate is dismissed as 40 000 km
// away. Either failure reports a clean miss.
func TestCanonNearestArc_AcrossAntimeridian(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat = 12.0
	const probeLon = 179.99995 // ~5.4 m west of the seam
	want := addCanonArc(t, d, canonPts(
		[2]float64{probeLat - 0.0001, -179.99995},
		[2]float64{probeLat + 0.0001, -179.99995},
	))

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 20)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if !ok || got.ID != want {
		t.Fatalf("arc across the antimeridian: got (%d, %v), want (%d, true)", got.ID, ok, want)
	}
	if got.DistM > 20 {
		t.Errorf("DistM = %v, want <= the 20 m radius", got.DistM)
	}
	if got.Lon < -180 || got.Lon > 180 {
		t.Errorf("Lon = %v is outside [-180, 180]", got.Lon)
	}
}

// TestCanonNearestArc_AcrossPole is the same end-to-end check for
// the polar singularity: metres apart, 180° apart in longitude. Only a box
// widened to the whole world lets the candidate through at all.
func TestCanonNearestArc_AcrossPole(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	probeLat, probeLon := destPoint(90, 0, 0, 1.0) // 1 m from the pole
	aLat, aLon := destPoint(probeLat, probeLon, 0, 2.0)
	bLat, bLon := destPoint(probeLat, probeLon, 0, 4.0)
	if math.Abs(aLon-probeLon) < 90 {
		t.Fatalf("fixture is wrong: vertex at lon %v did not cross the pole", aLon)
	}
	want := addCanonArc(t, d, canonPts([2]float64{aLat, aLon}, [2]float64{bLat, bLon}))

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 10)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if !ok || got.ID != want {
		t.Fatalf("arc across the pole: got (%d, %v), want (%d, true)", got.ID, ok, want)
	}
	if math.Abs(got.DistM-2.0) > 0.01 {
		t.Errorf("DistM = %v, want ~2 m", got.DistM)
	}
}

// TestCanonNearestArc_OutsideRadius: an arc that clears the
// bounding box but fails the haversine refine is rejected. The box is a
// prefilter; the refine is the decision.
func TestCanonNearestArc_OutsideRadius(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const probeLat, probeLon = 54.4, -3.0
	// A short arc 14 m away on the diagonal: inside the corner of the
	// 10 m box, outside the 10 m disc.
	cLat, cLon := destPoint(probeLat, probeLon, 45, 14.0)
	dLat, dLon := destPoint(cLat, cLon, 45, 20.0)
	addCanonArc(t, d, canonPts([2]float64{cLat, cLon}, [2]float64{dLat, dLon}))

	got, ok, err := findNearbyCanon(t, ctx, d, probeLat, probeLon, 10)
	if err != nil {
		t.Fatalf("NearestArc: %v", err)
	}
	if ok || got.ID != 0 {
		t.Errorf("arc outside the radius: got (%d, %v at %v m), want (0, false)", got.ID, ok, got.DistM)
	}
}

// A node 15 m off the DEEP INTERIOR of a long leg is the case the whole
// symmetric-snap question is about (a node 14 m from an arc's interior,
// ~6 km from either end node). It is invisible to every
// endpoint-probing search, and it is invisible to any box built as the union
// of per-vertex boxes — so this test is also the falsifier for that
// narrowing.
func TestCanonNodesNearPath_FindsNodeOffLegInterior(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	// One 2 km leg due east, and a node 10 m north of its midpoint.
	const aLat, aLon = 54.4, -3.0
	bLat, bLon := destPoint(aLat, aLon, 90, 2000)
	midLat, midLon := destPoint(aLat, aLon, 90, 1000)
	nLat, nLon := destPoint(midLat, midLon, 0, 10)
	want := mustAddNode(t, ctx, d, nLat, nLon)

	got, err := nodesNearPathIn(t, ctx, d,
		walkOf([2]float64{aLat, aLon}, [2]float64{bLat, bLon}), 15, nil)
	if err != nil {
		t.Fatalf("NodesNearPath: %v", err)
	}
	if len(got) != 1 || got[0].ID != want {
		t.Fatalf("got %+v, want the single node %d", got, want)
	}
	// ~10, not exactly 10: destPoint walks a great circle while the walk's
	// one leg is a straight chord, so the true perpendicular to the chord is
	// a little more than the 10 m offset. The bracket is deliberately loose
	// — TestCanonNodesNearPath_RadiusBoundaryInclusive is what pins the distance
	// exactly, against the refine's own number.
	if got[0].DistM < 9.5 || got[0].DistM > 10.5 {
		t.Errorf("DistM = %v, want ~10", got[0].DistM)
	}
	// The projection is mid-leg; the nearest VERTEX is a kilometre away in
	// either direction, and ties there go to the lower index.
	if got[0].PointIdx != 0 {
		t.Errorf("PointIdx = %d, want 0 (nearest vertex, ties low)", got[0].PointIdx)
	}
	if got[0].PriorLenM < 990 || got[0].PriorLenM > 1010 {
		t.Errorf("PriorLenM = %v, want ~1000", got[0].PriorLenM)
	}
	// The node's own coordinates come back EXACTLY as stored, never the
	// projection's.
	if got[0].Lat != nLat || got[0].Lon != nLon {
		t.Errorf("coordinates = (%v, %v), want the stored (%v, %v)", got[0].Lat, got[0].Lon, nLat, nLon)
	}
	if got[0].ProjLat == nLat && got[0].ProjLon == nLon {
		t.Errorf("ProjLat/ProjLon must be the point on the walk, not the node")
	}
}

// EVERY node in range, not the nearest one. A nearest-wins answer would
// hide every junction the leg passes but one.
func TestCanonNodesNearPath_ReturnsEveryNodeNotJustNearest(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const aLat, aLon = 54.4, -3.0
	bLat, bLon := destPoint(aLat, aLon, 90, 3000)

	var want []NodeID
	for _, m := range []float64{500, 1500, 2500} {
		pLat, pLon := destPoint(aLat, aLon, 90, m)
		// Different offsets, so "nearest wins" would return only the 2 m one.
		off := 2.0 + m/1000
		nLat, nLon := destPoint(pLat, pLon, 0, off)
		want = append(want, mustAddNode(t, ctx, d, nLat, nLon))
	}
	// And one well outside the radius, which must not appear.
	farLat, farLon := destPoint(aLat, aLon, 0, 200)
	mustAddNode(t, ctx, d, farLat, farLon)

	got, err := nodesNearPathIn(t, ctx, d,
		walkOf([2]float64{aLat, aLon}, [2]float64{bLat, bLon}), 15, nil)
	if err != nil {
		t.Fatalf("NodesNearPath: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(got), len(want), got)
	}
	seen := map[NodeID]bool{}
	for _, g := range got {
		seen[g.ID] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("node %d in range but missing from the answer: %+v", w, got)
		}
	}
}

// Bracketed BOTH sides. The convention is `d <= radiusM`, the same one
// TestCanonNearestNode_RadiusBoundaryInclusive pins for the point
// search; a node at exactly the radius qualifies and one ULP further does
// not.
func TestCanonNodesNearPath_RadiusBoundaryInclusive(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const aLat, aLon = 54.4, -3.0
	bLat, bLon := destPoint(aLat, aLon, 90, 1000)
	midLat, midLon := destPoint(aLat, aLon, 90, 500)
	nLat, nLon := destPoint(midLat, midLon, 0, 12)
	want := mustAddNode(t, ctx, d, nLat, nLon)

	walk := walkOf([2]float64{aLat, aLon}, [2]float64{bLat, bLon})

	// The exact distance the refine will measure — taken from the refine
	// itself, so the bracket is around the rule's own number and not around
	// a second computation of it.
	proj, err := ProjectProbeOntoWalk(walk, nLat, nLon)
	if err != nil {
		t.Fatalf("ProjectProbeOntoWalk: %v", err)
	}
	exact := proj.DistM

	got, err := nodesNearPathIn(t, ctx, d, walk, exact, nil)
	if err != nil {
		t.Fatalf("NodesNearPath(exact): %v", err)
	}
	if len(got) != 1 || got[0].ID != want {
		t.Errorf("node at exactly radiusM: got %+v, want the node %d", got, want)
	}

	justShort := math.Nextafter(exact, 0)
	got, err = nodesNearPathIn(t, ctx, d, walk, justShort, nil)
	if err != nil {
		t.Fatalf("NodesNearPath(justShort): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("node one ULP outside radiusM: got %+v, want none", got)
	}
}

// The order is stated in the doc comment and pinned here: ascending
// PointIdx, then ascending PriorLenM, then ascending node id. Two nodes at
// the SAME point on the walk still come back in one fixed order.
func TestCanonNodesNearPath_DeterministicOrder(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	// A three-vertex walk, 100 m per leg, so nodes fall nearest to
	// different vertices.
	const aLat, aLon = 54.4, -3.0
	bLat, bLon := destPoint(aLat, aLon, 90, 100)
	cLat, cLon := destPoint(bLat, bLon, 90, 100)
	walk := walkOf([2]float64{aLat, aLon}, [2]float64{bLat, bLon}, [2]float64{cLat, cLon})

	// Inserted in an order that is neither the answer's nor its reverse, so
	// "the canon's row order" cannot pass by accident.
	nearC := mustAddNodeOffset(t, ctx, d, cLat, cLon, 0, 3)
	nearA := mustAddNodeOffset(t, ctx, d, aLat, aLon, 0, 4)
	nearB := mustAddNodeOffset(t, ctx, d, bLat, bLon, 0, 5)
	// Two nodes at the SAME point on the walk (one either side of vertex A,
	// both projecting to A itself), which only the id tie-break separates.
	tieHi := mustAddNodeOffset(t, ctx, d, aLat, aLon, 180, 6)
	tieLo := mustAddNodeOffset(t, ctx, d, aLat, aLon, 180, 7)
	if tieLo < tieHi {
		tieLo, tieHi = tieHi, tieLo
	}

	got, err := nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if err != nil {
		t.Fatalf("NodesNearPath: %v", err)
	}
	var gotIDs []NodeID
	for _, g := range got {
		gotIDs = append(gotIDs, g.ID)
	}
	// nearA, then the two behind A in id order (all three project to
	// PriorLenM 0 at vertex 0 — the ids separate them), then nearB, nearC.
	want := []NodeID{nearA, tieHi, tieLo, nearB, nearC}
	if len(gotIDs) != len(want) {
		t.Fatalf("got %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("order: got %v, want %v", gotIDs, want)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].PointIdx < got[i-1].PointIdx {
			t.Errorf("PointIdx not ascending: %+v", got)
		}
	}
}

// `exclude` removes rows from the CANDIDATE SET, before anything is
// selected — the precedent and the reasoning are
// NearestNode's. An empty slice excludes nothing.
func TestCanonNodesNearPath_ExcludeRemovesBeforeSelection(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	const aLat, aLon = 54.4, -3.0
	bLat, bLon := destPoint(aLat, aLon, 90, 500)
	walk := walkOf([2]float64{aLat, aLon}, [2]float64{bLat, bLon})

	// The leg's own two ends are nodes as well — exactly what a caller
	// excludes — plus a third node mid-leg it must still find.
	start := mustAddNode(t, ctx, d, aLat, aLon)
	end := mustAddNode(t, ctx, d, bLat, bLon)
	midLat, midLon := destPoint(aLat, aLon, 90, 250)
	mid := mustAddNodeOffset(t, ctx, d, midLat, midLon, 0, 8)

	all, err := nodesNearPathIn(t, ctx, d, walk, 15, []NodeID{})
	if err != nil {
		t.Fatalf("NodesNearPath(empty exclude): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("empty exclude must exclude nothing: got %+v", all)
	}

	got, err := nodesNearPathIn(t, ctx, d, walk, 15, []NodeID{start, end})
	if err != nil {
		t.Fatalf("NodesNearPath(exclude ends): %v", err)
	}
	if len(got) != 1 || got[0].ID != mid {
		t.Fatalf("excluding the ends must still find the interior node %d: got %+v", mid, got)
	}
}

// An empty answer is an ANSWER. A walk that passes nothing, and an empty
// network, both return no rows and no error — a caller must not have to
// treat "nothing near this leg" as a failure.
func TestCanonNodesNearPath_EmptyIsNotAnError(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()
	walk := walkOf([2]float64{54.4, -3.0}, [2]float64{54.41, -3.0})

	got, err := nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if err != nil {
		t.Fatalf("empty network: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty network: got %+v, want none", got)
	}

	// A node in the network but 200 m away is the same clean miss. Due EAST
	// — the walk itself runs north, so a node offset along its own bearing
	// would sit ON it.
	farLat, farLon := destPoint(54.4, -3.0, 90, 200)
	mustAddNode(t, ctx, d, farLat, farLon)
	got, err = nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if err != nil {
		t.Fatalf("out-of-range node: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("out-of-range node: got %+v, want none", got)
	}
}

// Invalid input errors rather than reading as a clean miss, following
// NearestNode's own validation. The verdict must not depend on
// whether any candidate node happened to exist, so these run on an EMPTY
// network — where a validation done per-candidate would never fire.
func TestCanonNodesNearPath_InvalidInputErrors(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()
	ok := walkOf([2]float64{54.4, -3.0}, [2]float64{54.41, -3.0})

	cases := []struct {
		name    string
		verts   []Point
		radiusM float64
	}{
		{"zero radius", ok, 0},
		{"negative radius", ok, -1},
		{"NaN radius", ok, math.NaN()},
		{"infinite radius", ok, math.Inf(1)},
		{"nil walk", nil, 15},
		{"one-vertex walk", walkOf([2]float64{54.4, -3.0}), 15},
		{"latitude off the globe", walkOf([2]float64{91, -3.0}, [2]float64{54.4, -3.0}), 15},
		{"longitude off the globe", walkOf([2]float64{54.4, -3.0}, [2]float64{54.4, 181}), 15},
		{"NaN vertex", walkOf([2]float64{54.4, -3.0}, [2]float64{math.NaN(), -3.0}), 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := nodesNearPathIn(t, ctx, d, c.verts, c.radiusM, nil)
			if err == nil {
				t.Fatalf("want an error, got %+v", got)
			}
		})
	}
}

// A walk of zero total length is an error too, but a DISTINGUISHABLE one:
// it wraps ErrWalkHasNoLength, exactly as ProjectProbeOntoWalk does, so a
// caller that treats such a leg as ordinary geometry to skip can test for
// it with errors.Is rather than parsing a message. And it is decided UP
// FRONT: the same walk gives the same verdict whether a candidate node
// exists or not.
func TestCanonNodesNearPath_ZeroLengthWalkIsDistinguishable(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()
	walk := walkOf([2]float64{54.4, -3.0}, [2]float64{54.4, -3.0}, [2]float64{54.4, -3.0})

	// No candidate node at all.
	_, err := nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if !errors.Is(err, ErrWalkHasNoLength) {
		t.Fatalf("empty network: want ErrWalkHasNoLength, got %v", err)
	}

	// And with a node sitting right on it — the same verdict.
	mustAddNode(t, ctx, d, 54.4, -3.0)
	_, err = nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if !errors.Is(err, ErrWalkHasNoLength) {
		t.Fatalf("with a coincident node: want ErrWalkHasNoLength, got %v", err)
	}
}

// SegIdx and PointIdx answer different questions, and this is the case the
// SegIdx field exists for: a four-vertex walk whose THIRD leg is 4 km long,
// with a node 10 m off that leg's midpoint. The foot of the perpendicular is
// on leg 2 (SegIdx 2); the nearest VERTEX is 2 km away at either end of it,
// and the tie goes low, so PointIdx is 2 as well — which would let the two
// look interchangeable — so the node is pushed along the leg to break that
// tie deliberately.
//
// If someone later "simplifies" SegIdx into PointIdx, or derives one from
// the other, this goes red.
func TestCanonNodesNearPath_SegIdxAndPointIdxAnswerDifferentQuestions(t *testing.T) {
	d := emptyCanon(t)
	ctx := context.Background()

	// Vertices 0..3, all due east: two short legs then one 4 km leg.
	const v0Lat, v0Lon = 54.4, -3.0
	v1Lat, v1Lon := destPoint(v0Lat, v0Lon, 90, 50)
	v2Lat, v2Lon := destPoint(v1Lat, v1Lon, 90, 50)
	v3Lat, v3Lon := destPoint(v2Lat, v2Lon, 90, 4000)
	walk := walkOf(
		[2]float64{v0Lat, v0Lon},
		[2]float64{v1Lat, v1Lon},
		[2]float64{v2Lat, v2Lon},
		[2]float64{v3Lat, v3Lon},
	)

	// 3 km along the long leg — nearer to vertex 3 than to vertex 2 — and
	// 10 m off it.
	footLat, footLon := destPoint(v2Lat, v2Lon, 90, 3000)
	node := mustAddNodeOffset(t, ctx, d, footLat, footLon, 0, 10)

	got, err := nodesNearPathIn(t, ctx, d, walk, 15, nil)
	if err != nil {
		t.Fatalf("NodesNearPath: %v", err)
	}
	if len(got) != 1 || got[0].ID != node {
		t.Fatalf("got %+v, want the single node %d", got, node)
	}
	if got[0].SegIdx != 2 {
		t.Errorf("SegIdx = %d, want 2 (the foot is on the leg verts[2]→verts[3])", got[0].SegIdx)
	}
	if got[0].PointIdx != 3 {
		t.Errorf("PointIdx = %d, want 3 (the nearest VERTEX, 1 km away)", got[0].PointIdx)
	}
	if got[0].SegIdx == got[0].PointIdx {
		t.Errorf("SegIdx and PointIdx must not be interchangeable: both %d", got[0].SegIdx)
	}
	// The foot really is between the leg's two vertices — SegIdx is an
	// insertion position, so the claim has to hold geometrically and not
	// merely be a plausible integer.
	if got[0].ProjLon <= walk[got[0].SegIdx].Lon || got[0].ProjLon >= walk[got[0].SegIdx+1].Lon {
		t.Errorf("projection lon %v is not between verts[%d].Lon %v and verts[%d].Lon %v",
			got[0].ProjLon, got[0].SegIdx, walk[got[0].SegIdx].Lon,
			got[0].SegIdx+1, walk[got[0].SegIdx+1].Lon)
	}
}
