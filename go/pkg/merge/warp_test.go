package merge

import (
	"math"
	"testing"
)

// warpTestLeg builds a straight north-running leg of n points spaced
// spacingM apart, starting at (50, -3). Straight and evenly spaced so the
// measure-length arithmetic in the assertions is trivial to reason about.
func warpTestLeg(n int, spacingM float64) []Point {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := make([]Point, n)
	for k := range pts {
		pts[k] = Point{
			Lat: 50 + float64(k)*spacingM/metresPerDeg,
			Lon: -3,
		}
	}
	return pts
}

func warpBitsEqual(a, b Point) bool {
	return math.Float64bits(a.Lat) == math.Float64bits(b.Lat) &&
		math.Float64bits(a.Lon) == math.Float64bits(b.Lon)
}

func assertLegBitwiseIdentical(t *testing.T, got, want []Point, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d points, want %d", ctx, len(got), len(want))
	}
	for k := range got {
		if !warpBitsEqual(got[k], want[k]) {
			t.Fatalf("%s: point %d moved: got (%v,%v) want (%v,%v)",
				ctx, k, got[k].Lat, got[k].Lon, want[k].Lat, want[k].Lon)
		}
	}
}

// A leg with no targets must come back byte-identical. This is the reject
// side: an endpoint with no node inside the radius is untouched.
func TestWarpLeg_NoTargets_Identical(t *testing.T) {
	pts := warpTestLeg(200, 5)
	got, _, err := WarpLegEndpoints(pts, nil, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	assertLegBitwiseIdentical(t, got, pts, "no targets")
}

// A target that is already bitwise equal to the terminal is a no-op — there
// is no translation to blend, so nothing may move.
func TestWarpLeg_ZeroDisplacement_Identical(t *testing.T) {
	pts := warpTestLeg(200, 5)
	tgt := &WarpTarget{Lat: pts[0].Lat, Lon: pts[0].Lon}
	got, _, err := WarpLegEndpoints(pts, tgt, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	assertLegBitwiseIdentical(t, got, pts, "zero displacement")
}

// THE deliverable: the warped terminal is BITWISE equal to the target, not
// merely close. Computing it as p + (t-p)*1.0 instead of assigning t is not
// guaranteed to round to t, and a value that is one ulp off looks perfect in
// every log while silently failing the align step's `==` later.
//
// The base coordinates matter enormously here and it is not obvious why.
// Around lat 50 / lon -3 a computed terminal is bitwise correct ANYWAY: the
// target and the source terminal are within a factor of two of each other, so
// Sterbenz's lemma makes (t-p) exact, and p+(t-p) then lands exactly on t. A
// test sampling only ordinary trail coordinates therefore cannot tell the two
// implementations apart, and reports protection it does not provide.
//
// The difference appears where a coordinate is near ZERO — the prime meridian
// and the equator — because then p and t straddle wildly different exponents,
// (t-p) has to round, and the round trip loses the target.
//
// One leg per base, both ends warped. The terminals are set by exact
// assignment, not arithmetic, so a repeat at the same base takes the same
// path. Where a zero line runs through the base, both terminals sit on a
// (terminal, target) pair from TestWarpLeg_TerminalAssignedBitwise_ZeroLineStrips
// for which p+(t-p) != t, checked here so the case cannot silently lose its
// teeth; elsewhere the targets are ordinary 5–7 m offsets.
//
// Red proofs, 2026-10-05 (detached worktree): with applyWarpAtEnd's s == 0
// branch computing src + v*1 instead of assigning, this fails on "prime
// meridian: CheckTerminalsExact: start terminal … is not bit-exact"; with that
// assignment kept for the start only, it fails on the same leg's end
// terminal. The 120,000-case random sweep this replaced passed both
// mutations: its 1e-3° jitter moved every base out of the 11 m zero-line
// strip.
var warpBaseCoords = []struct {
	name     string
	lat, lon float64
}{
	{"ordinary trail", 50, -3},
	{"prime meridian", 51.4779, 0},
	{"equator", 0, 32.5},
	{"null island", 0, 0},
	{"antimeridian", -16.5, 179.9999},
	{"high latitude", 78.2232, 15.6469},
}

func TestWarpLeg_TerminalAssignedBitwise(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	// (terminal, target) coordinate pairs where p+(t-p) != t.
	const (
		p1, want1 = 7.887234586609073e-05, -1.6367304540859593e-06
		p2, want2 = 6.106703326626644e-05, 1.0852435376673728e-05
	)
	for _, pr := range [][2]float64{{p1, want1}, {p2, want2}} {
		if c := pr[0] + (pr[1] - pr[0]); math.Float64bits(c) == math.Float64bits(pr[1]) {
			t.Fatalf("pair (%v, %v) no longer distinguishes computed from assigned — find new counterexamples", pr[0], pr[1])
		}
	}
	for _, base := range warpBaseCoords {
		// A 60-point, 5 m leg running north from the base, or east when the
		// zero line is the equator (so the latitude stays on it).
		pts := make([]Point, 60)
		east := base.lat == 0
		for k := range pts {
			step := float64(k) * 5 / metresPerDeg
			pts[k] = Point{Lat: base.lat + step, Lon: base.lon}
			if east {
				pts[k] = Point{Lat: base.lat, Lon: base.lon + step}
			}
		}
		last := len(pts) - 1
		off := func(m float64) float64 { return m / metresPerDeg }
		startT := &WarpTarget{Lat: pts[0].Lat + off(5), Lon: pts[0].Lon - off(6)}
		endT := &WarpTarget{Lat: pts[last].Lat - off(7), Lon: pts[last].Lon + off(5)}
		switch base.name {
		case "prime meridian":
			// Longitudes on the Meridian Line at both terminals.
			for k := range pts {
				pts[k].Lon = p1
			}
			pts[last].Lon = p2
			startT.Lon, endT.Lon = want1, want2
		case "equator", "null island":
			// Latitudes on the equator at both terminals; at null island the
			// start's longitude sits on the prime meridian as well.
			for k := range pts {
				pts[k].Lat = p1
			}
			pts[last].Lat = p2
			startT.Lat, endT.Lat = want1, want2
			if base.name == "null island" {
				pts[0].Lon = p2
				startT.Lon = want2
			}
		}

		got, _, err := WarpLegEndpoints(pts, startT, endT)
		if err != nil {
			t.Fatalf("%s: %v", base.name, err)
		}
		if math.Float64bits(got[0].Lat) != math.Float64bits(startT.Lat) ||
			math.Float64bits(got[0].Lon) != math.Float64bits(startT.Lon) {
			t.Fatalf("%s: start terminal not bitwise equal to target: got (%v,%v) want (%v,%v)",
				base.name, got[0].Lat, got[0].Lon, startT.Lat, startT.Lon)
		}
		if math.Float64bits(got[last].Lat) != math.Float64bits(endT.Lat) ||
			math.Float64bits(got[last].Lon) != math.Float64bits(endT.Lon) {
			t.Fatalf("%s: end terminal not bitwise equal to target: got (%v,%v) want (%v,%v)",
				base.name, got[last].Lat, got[last].Lon, endT.Lat, endT.Lon)
		}
	}
}

// Deterministic teeth for "assigned, never computed", and the test that says
// WHERE this actually bites. Read the geography before touching it.
//
// The randomised sweep above cannot distinguish the two implementations at
// ordinary trail coordinates: there the target and terminal are within a factor
// of two of each other, Sterbenz makes (t-p) exact, and p+(t-p) lands back on t
// every time. Measured, 0 failures in 2,000,000 samples at lat 56.79.
//
// The hazard is not a point and it is not about straddling zero. It is TWO
// GREAT-CIRCLE STRIPS, each about 11 m wide:
//
//   - the PRIME MERIDIAN, pole to pole — Greenwich, France, Spain, Algeria,
//     Mali, Ghana;
//   - the EQUATOR, all the way round — Ecuador, Kenya, Uganda, Indonesia,
//     Brazil.
//
// Measured over 2,000,000 samples in each strip: 21.5% of computed warps are
// not bitwise exact, worst case over 7 MILLION ulp. Pairs on the SAME side of
// the zero line fail too (16.0%), so straddling is not the discriminator —
// magnitude ratio is. The band is sharp: 33 m from the zero line, 0 failures.
//
// This is not a fixture curiosity. The Meridian Line at the Royal Observatory
// is a marked footpath that people walk and record, and any GPX of it has
// longitudes inside the strip.
//
// The pairs below were found by exhaustive search and are placed as REALISTIC
// coordinates — as longitudes at a Greenwich latitude, and as latitudes at an
// equatorial longitude — precisely so this does not read as unreachable and get
// deleted by the next person through.
func TestWarpLeg_TerminalAssignedBitwise_ZeroLineStrips(t *testing.T) {
	// (terminal, target) pairs where p+(t-p) provably != t.
	pairs := []struct {
		name    string
		p, want float64
	}{
		{"straddling", 7.887234586609073e-05, -1.6367304540859593e-06},
		{"same side, both east", 6.106703326626644e-05, 1.0852435376673728e-05},
		{"same side, both west", -7.10089927010725e-05, -1.2561815457656289e-06},
		{"same side, near", -7.52273397661518e-05, -1.0166294772532415e-05},
		{"straddling, wide", 8.232274135831981e-05, -6.920142620530552e-06},
	}

	// Each pair is exercised twice: once as a LONGITUDE on the Meridian Line at
	// the Royal Observatory, once as a LATITUDE on the equator in Uganda.
	frames := []struct {
		name         string
		asLon        bool
		otherCoordAt float64
	}{
		{"prime meridian at Greenwich (lat 51.4779)", true, 51.4779},
		{"equator in Uganda (lon 32.5)", false, 32.5},
	}

	for _, fr := range frames {
		fr := fr
		t.Run(fr.name, func(t *testing.T) {
			for _, pr := range pairs {
				// These pairs are only meaningful while the computed form is
				// actually wrong for them. If a toolchain change makes
				// p+(t-p) exact here, this test has silently stopped testing
				// anything, and saying so is better than going quietly green.
				if c := pr.p + (pr.want - pr.p); math.Float64bits(c) == math.Float64bits(pr.want) {
					t.Fatalf("%s: pair no longer distinguishes computed from assigned — find new counterexamples", pr.name)
				}

				// A 1000 m leg, so the 100 m zone absorbs the ~9 m displacement
				// with room to spare under the 1.5x adequacy factor.
				pts := warpTestLeg(201, 5)
				var tgt *WarpTarget
				if fr.asLon {
					for k := range pts {
						pts[k].Lat += fr.otherCoordAt - 50
						pts[k].Lon = pr.p
					}
					tgt = &WarpTarget{Lat: pts[0].Lat, Lon: pr.want}
				} else {
					for k := range pts {
						pts[k].Lat += pr.p - 50
						pts[k].Lon = fr.otherCoordAt
					}
					tgt = &WarpTarget{Lat: pr.want, Lon: pts[0].Lon}
				}

				got, rep, err := WarpLegEndpoints(pts, tgt, nil)
				if err != nil {
					t.Fatalf("%s: %v", pr.name, err)
				}
				if !rep.StartWarped {
					t.Fatalf("%s: the start did not warp (report %+v) — fixture is not exercising anything", pr.name, rep)
				}
				if math.Float64bits(got[0].Lat) != math.Float64bits(tgt.Lat) ||
					math.Float64bits(got[0].Lon) != math.Float64bits(tgt.Lon) {
					t.Fatalf("%s: terminal is (%v,%v), want exactly (%v,%v) — lat %d ulp, lon %d ulp off",
						pr.name, got[0].Lat, got[0].Lon, tgt.Lat, tgt.Lon,
						int64(math.Float64bits(got[0].Lat))-int64(math.Float64bits(tgt.Lat)),
						int64(math.Float64bits(got[0].Lon))-int64(math.Float64bits(tgt.Lon)))
				}
			}
		})
	}
}

// A vertex sitting at the terminal's exact MEASURE POSITION — a duplicated first
// point, which is what a trace logging while stationary produces — must be
// ASSIGNED too, not computed.
//
// Keying the assignment on `idx == ti` instead of `s == 0` leaves these on the
// computed path: they get w == 1 like the terminal but miss its protection. In
// the source they were bitwise identical to the terminal; computing would pull
// them apart. This is the 193x-stationary-logging case, so it is
// present in real data.
func TestWarpLeg_DuplicateTerminalPointsAreAssignedToo(t *testing.T) {
	pts := warpTestLeg(201, 5)
	// Place the leg in the meridian strip, where computed and assigned differ.
	for k := range pts {
		pts[k].Lat += 51.4779 - 50
		pts[k].Lon = 6.106703326626644e-05
	}
	// Three coincident points at the start: the terminal and two duplicates.
	pts[1] = pts[0]
	pts[2] = pts[0]

	tgt := &WarpTarget{Lat: pts[0].Lat, Lon: 1.0852435376673728e-05}
	got, rep, err := WarpLegEndpoints(pts, tgt, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	if !rep.StartWarped {
		t.Fatalf("the start did not warp (report %+v)", rep)
	}
	for k := 0; k < 3; k++ {
		if math.Float64bits(got[k].Lon) != math.Float64bits(tgt.Lon) {
			t.Fatalf("point %d is at measure 0 and must land exactly on the target: got %v (%#x), want %v (%#x)",
				k, got[k].Lon, math.Float64bits(got[k].Lon), tgt.Lon, math.Float64bits(tgt.Lon))
		}
	}
	// They were coincident in the source and must remain coincident.
	if !warpBitsEqual(got[0], got[1]) || !warpBitsEqual(got[1], got[2]) {
		t.Fatal("points coincident in the source were pulled apart by the warp")
	}
}

// A zero-length leg is an error to WARP, but not an error to leave alone. The
// early return on a zero-magnitude translation is what separates the two, and
// without it a leg whose terminal already sits exactly on its node would be
// rejected for a degeneracy that never gets touched.
func TestWarpLeg_ZeroLengthLegWithCoincidentTarget_IsFine(t *testing.T) {
	flat := make([]Point, 10)
	for k := range flat {
		flat[k] = Point{Lat: 50, Lon: -3}
	}
	got, _, err := WarpLegEndpoints(flat, &WarpTarget{Lat: 50, Lon: -3}, nil)
	if err != nil {
		t.Fatalf("a coincident target on a zero-length leg must be a no-op, got: %v", err)
	}
	assertLegBitwiseIdentical(t, got, flat, "zero-length leg, coincident target")
}

// Nothing beyond the taper zone may move, bitwise — including on a leg that
// warps at BOTH ends.
func TestWarpLeg_OutsideZoneUntouched(t *testing.T) {
	const spacing = 5.0
	pts := warpTestLeg(201, spacing) // 200 gaps => 1000 m of measure
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 8/metresPerDeg, Lon: pts[0].Lon}
	last := len(pts) - 1
	endT := &WarpTarget{Lat: pts[last].Lat - 8/metresPerDeg, Lon: pts[last].Lon}

	got, _, err := WarpLegEndpoints(pts, startT, endT)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}

	// zoneFor's zone is min(legLen, widenedZoneFactor*displacement) =
	// min(1000, 5*8) = 40 m = 8 gaps. Index 8 (start) and 192 (end) sit
	// exactly on the zone edge, where the taper weight is zero only up to
	// haversine rounding, so they are not asserted either way. 9..191 must not
	// move: a zone one gap wider (45 m) reaches index 9 and fails here.
	for k := 9; k <= 191; k++ {
		if !warpBitsEqual(got[k], pts[k]) {
			t.Fatalf("point %d is outside both taper zones but moved: got (%v,%v) want (%v,%v)",
				k, got[k].Lat, got[k].Lon, pts[k].Lat, pts[k].Lon)
		}
	}
	// And the zones did their job at the terminals, and still reach one gap
	// inside the zone edge (index 7 / 193, 35 m in): a zone that stops short
	// of 35 m fails here.
	for _, k := range []int{0, 7, last - 7, last} {
		if warpBitsEqual(got[k], pts[k]) {
			t.Fatalf("point %d is inside a taper zone but did not move", k)
		}
	}
}

// Displacement must decay monotonically away from the terminal and never
// exceed the terminal's own displacement. This is what makes the result a
// blend rather than a kink.
func TestWarpLeg_DisplacementDecaysMonotonically(t *testing.T) {
	pts := warpTestLeg(201, 5)
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 8/metresPerDeg, Lon: pts[0].Lon + 3/metresPerDeg}

	got, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}

	prev := math.MaxFloat64
	for k := 0; k <= 21; k++ {
		d := HaversineMetres(pts[k].Lat, pts[k].Lon, got[k].Lat, got[k].Lon)
		if d > prev+1e-9 {
			t.Fatalf("displacement increased at point %d: %v m after %v m", k, d, prev)
		}
		prev = d
	}
	d0 := HaversineMetres(pts[0].Lat, pts[0].Lon, got[0].Lat, got[0].Lon)
	if d0 < 8 {
		t.Fatalf("terminal displacement %v m, expected ~8.5 m", d0)
	}
}

// Solving both ends against the UNWARPED leg means the two ends cannot
// influence each other. Warping both at once must equal warping each alone
// and taking each end's half.
func TestWarpLeg_EndsAreIndependent(t *testing.T) {
	pts := warpTestLeg(201, 5)
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 6/metresPerDeg, Lon: pts[0].Lon}
	last := len(pts) - 1
	endT := &WarpTarget{Lat: pts[last].Lat - 9/metresPerDeg, Lon: pts[last].Lon}

	both, _, err := WarpLegEndpoints(pts, startT, endT)
	if err != nil {
		t.Fatalf("both: %v", err)
	}
	onlyStart, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("start only: %v", err)
	}
	onlyEnd, _, err := WarpLegEndpoints(pts, nil, endT)
	if err != nil {
		t.Fatalf("end only: %v", err)
	}

	for k := 0; k <= 100; k++ {
		if !warpBitsEqual(both[k], onlyStart[k]) {
			t.Fatalf("point %d: warping the end changed the start half", k)
		}
	}
	for k := 101; k < len(pts); k++ {
		if !warpBitsEqual(both[k], onlyEnd[k]) {
			t.Fatalf("point %d: warping the start changed the end half", k)
		}
	}
}

// Re-warping an already-warped leg to the same target changes nothing: the
// terminal now sits exactly on the target, so the translation is zero. This
// is what stops repeated imports from drifting.
func TestWarpLeg_Idempotent(t *testing.T) {
	pts := warpTestLeg(201, 5)
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 7/metresPerDeg, Lon: pts[0].Lon + 2/metresPerDeg}

	once, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("first warp: %v", err)
	}
	twice, _, err := WarpLegEndpoints(once, startT, nil)
	if err != nil {
		t.Fatalf("second warp: %v", err)
	}
	assertLegBitwiseIdentical(t, twice, once, "idempotence")
}

// Elevation is not part of the translation vector and must survive untouched,
// including inside the taper zone.
func TestWarpLeg_ElevationUntouched(t *testing.T) {
	pts := warpTestLeg(201, 5)
	for k := range pts {
		e := float64(k) * 1.5
		pts[k].Ele = &e
	}
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 8/metresPerDeg, Lon: pts[0].Lon}

	got, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	for k := range got {
		if got[k].Ele != pts[k].Ele {
			t.Fatalf("point %d: elevation pointer changed", k)
		}
		if *got[k].Ele != float64(k)*1.5 {
			t.Fatalf("point %d: elevation value changed to %v", k, *got[k].Ele)
		}
	}
}

// ⚠ THREE FIXTURE TESTS USED TO STAND HERE, PINNING THE WARP'S OLD FLAT-ONLY ZONE
// DECLINING/SNAPPING ON SHORT LEGS — TestWarpLeg_ZoneTooShortAndPastNeighbour_DeclinesAndCountsIt,
// TestWarpLeg_ZoneTooShort_SnapsWhenUnderNeighbourGap, TestWarpLeg_SnapPastNeighbour_Declines.
// Deleted 2026-08-14: the ruling ("all warping should change to this
// rule") means their exact fixtures now TAPER instead, under the one
// universal zone formula — the assertions were the thing that changed, not a
// regression. The same three shapes live on, reused verbatim (same point
// counts/spacings/displacements, same names as sub-tests), in
// TestWarp_NeverDeclinesOrSnapsWhereFlatRuleUsedTo below, which now asserts
// the OPPOSITE outcome deliberately — read that test's doc for why the
// before/after matters more than either snapshot alone.
//
// TestWarpLeg_ZoneGuardUsesSmoothstepMaxSlope also used to stand here: it
// threaded a real leg fixture through the whole pipeline to demonstrate
// WarpZoneCanAbsorb's 1.5x-not-1x margin, sized against the OLD flat-only
// zone (60 m for a 600 m leg). Under the new formula that same leg's zone is
// far wider (5*50=250 m, not 60 m) so the fixture no longer sits near the
// boundary it was built to probe. Replaced below with
// TestWarpZoneCanAbsorb_1_5xMargin, a direct unit test of the pure function
// — decoupled from zoneFor's SIZING entirely, which is the right scope for a
// test about WarpZoneCanAbsorb's own inequality.

// TestWarpZoneCanAbsorb_1_5xMargin is THE GUARD'S THRESHOLD IS 1.5x, NOT 1x,
// AND THIS IS WHY, unit-tested directly against the pure function rather
// than through a sized leg fixture (see the comment above for why that
// decoupling matters after 2026-08-14).
//
// The displacement profile is D(s) = v*w(s) with w a smoothstep, whose
// derivative peaks at 1.5 (f'(t) = 6t(1-t), max at t = 0.5). So the leg is
// locally stretched by up to 1.5*d/zone, and consecutive vertices can swap
// order as soon as that exceeds 1 — which happens for the whole band
// d <= zone < 1.5d that a naive `zone >= displacement` guard admits.
//
// A linear ramp (max slope 1.0) would make `zone >= d` correct. The design picks
// smoothstep to kill the boundary kink, and this is the price.
func TestWarpZoneCanAbsorb_1_5xMargin(t *testing.T) {
	// zone=60, d=50: a 1x guard (60>=50) would pass; the real 1.5x margin
	// needs 75, so this must fail.
	if WarpZoneCanAbsorb(60, 50) {
		t.Fatal("zone 60 absorbed displacement 50 — that is the 1x guard, and it admits inversion")
	}
	// Just inside the 1.5x bound: 60/1.5 = 40, so 39 must pass.
	if !WarpZoneCanAbsorb(60, 39) {
		t.Fatal("zone 60 should absorb displacement 39 (within the 1.5x margin) but did not")
	}
	// Exactly at the boundary (1.5*d == zone) passes — the inequality is <=.
	if !WarpZoneCanAbsorb(60, 40) {
		t.Fatal("zone 60, displacement 40 (exactly 1.5*40=60) is the boundary and must still pass (<=, not <)")
	}
}

// The property the guard exists to protect: the warp must never reverse the
// direction of travel between consecutive vertices. If it does, the leg folds
// back on itself — exactly the last-arc inversion the blend was introduced
// to prevent, reintroduced in the middle of the taper.
//
// Swept across the whole admissible range rather than at one point, because the
// worst case is at the zone midpoint, not at the terminal.
func TestWarpLeg_NeverInvertsAArc(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	for _, nPts := range []int{21, 41, 121, 201} {
		for _, spacing := range []float64{1, 5, 25} {
			for _, dispM := range []float64{0.5, 1, 3, 6, 9, 10} {
				pts := warpTestLeg(nPts, spacing)
				tgt := &WarpTarget{
					Lat: pts[0].Lat + dispM*0.6/metresPerDeg,
					// Push mostly ANTIPARALLEL to the leg's own direction,
					// which is the orientation that folds.
					Lon: pts[0].Lon + dispM*0.8/metresPerDeg,
				}
				got, rep, err := WarpLegEndpoints(pts, tgt, nil)
				if err != nil {
					t.Fatalf("n=%d spacing=%v disp=%v: %v", nPts, spacing, dispM, err)
				}
				if !rep.StartWarped {
					continue // skipped by the guard, which is the point
				}
				// Every consecutive pair must still advance northward, the
				// leg's original direction.
				for k := 1; k < len(got); k++ {
					if got[k].Lat <= got[k-1].Lat {
						t.Fatalf("n=%d spacing=%v disp=%v: arc %d inverted — lat went %v -> %v",
							nPts, spacing, dispM, k, got[k-1].Lat, got[k].Lat)
					}
				}
			}
		}
	}
}

func TestWarpLeg_RejectsBadInput(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	good := warpTestLeg(200, 5)

	t.Run("too few points", func(t *testing.T) {
		one := []Point{{Lat: 50, Lon: -3}}
		if _, _, err := WarpLegEndpoints(one, &WarpTarget{Lat: 50, Lon: -3}, nil); err == nil {
			t.Fatal("expected an error for a 1-point leg, got nil")
		}
	})

	t.Run("zero-length leg with a target moves every point to it", func(t *testing.T) {
		// RESOLVED 2026-08-14: decline is gone, WarpLegEndpoints always tapers.
		// A zero-length leg's zone is 0 (min(legLen=0, ...) = 0), so every
		// point's measure distance from the terminal is 0 too — the s==0 exact-
		// assignment branch fires for all of them, not just the terminal. This
		// is SAFE without any pre-collapse: every point was already coincident
		// (a zero-length leg has no ordering to invert), so moving all of them
		// to the same target preserves that, just at a new coordinate. A
		// genuinely production-shaped call goes through
		// warpLegEndpointsToNodes's collapse first (see warp.go's file
		// comment, "PROBLEM 2") — this test calls WarpLegEndpoints directly,
		// which has no such protection and doesn't need it for this shape.
		flat := make([]Point, 10)
		for k := range flat {
			flat[k] = Point{Lat: 50, Lon: -3}
		}
		tgt := &WarpTarget{Lat: 50 + 5/metresPerDeg, Lon: -3}
		got, rep, err := WarpLegEndpoints(flat, tgt, nil)
		if err != nil {
			t.Fatalf("a zero-length leg has a zero zone, but that's still a valid taper — it must not error: %v", err)
		}
		if !rep.StartWarped {
			t.Fatalf("expected the taper to fire, got %+v", rep)
		}
		for i, p := range got {
			if p.Lat != tgt.Lat || p.Lon != tgt.Lon {
				t.Fatalf("point %d is (%v,%v), want the target (%v,%v) exactly", i, p.Lat, p.Lon, tgt.Lat, tgt.Lon)
			}
		}
	})

	// A NaN in the SOURCE must error, not sail through. Every comparison
	// against NaN is false, so guards written the obvious way round
	// (`legLen <= 0`, `zone < displacement`) all pass and the whole taper zone
	// comes out NaN — surfacing much later with an error naming the wrong stage.
	t.Run("non-finite source terminal", func(t *testing.T) {
		for _, bad := range []Point{
			{Lat: math.NaN(), Lon: -3},
			{Lat: 50, Lon: math.NaN()},
			{Lat: math.Inf(-1), Lon: -3},
		} {
			pts := warpTestLeg(200, 5)
			pts[0] = bad
			tgt := &WarpTarget{Lat: 50, Lon: -3}
			if _, _, err := WarpLegEndpoints(pts, tgt, nil); err == nil {
				t.Fatalf("expected an error for a non-finite source terminal %v, got nil", bad)
			}
		}
	})

	// A NaN in the leg's INTERIOR — the case only the measure-length guard sees.
	//
	// Worth its own subtest because it is what makes that guard load-bearing
	// rather than decorative. The terminal-finiteness check in applyWarpAtEnd
	// inspects only src[ti], so it cannot see this; the measure-length check can,
	// because a NaN anywhere in the leg poisons the cumulative sum. Mutate the
	// measure-length guard to a non-total form (`legLen < 0`) and only this subtest
	// goes red.
	t.Run("non-finite source interior", func(t *testing.T) {
		const metresPerDeg = math.Pi * 6371000.0 / 180.0
		pts := warpTestLeg(200, 5)
		pts[97].Lat = math.NaN()
		tgt := &WarpTarget{Lat: pts[0].Lat + 5/metresPerDeg, Lon: pts[0].Lon}
		if _, _, err := WarpLegEndpoints(pts, tgt, nil); err == nil {
			t.Fatal("expected an error for a non-finite interior point, got nil")
		}
	})

	t.Run("non-finite target", func(t *testing.T) {
		for _, bad := range []WarpTarget{
			{Lat: math.NaN(), Lon: -3},
			{Lat: 50, Lon: math.NaN()},
			{Lat: math.Inf(1), Lon: -3},
		} {
			bad := bad
			if _, _, err := WarpLegEndpoints(good, &bad, nil); err == nil {
				t.Fatalf("expected an error for target %v, got nil", bad)
			}
		}
	})
}

// The blend is a SMOOTHSTEP, not a linear ramp, and this is the test that
// knows the difference. A linear ramp passes every other assertion in this
// file — it is monotone, bounded, exact at the terminal and zero at the zone
// edge — while leaving a residual kink where the taper stops.
//
// Sampling the weight at a quarter and three-quarters of the way through the
// zone separates them cleanly: smoothstep gives 0.84375 / 0.15625 where a
// linear ramp gives 0.75 / 0.25.
//
// ⚠ THE ZONE WIDTH, AND SO THE INDICES, CHANGED 2026-08-14 WITH THE FLOOR'S
// DELETION. Zone was min(legLen, max(flat 10%, 5*displacement)); on this
// leg's 1000 m measure the 10% floor (100 m) used to dominate 5*8=40 m, giving a
// 100 m / 20-gap zone (quarter at index 5, three-quarters at index 15). With
// the floor gone the zone is exactly 5*displacement = 40 m / 8 gaps at this
// leg's 5 m spacing, so those fractions now land at index 2 and index 6 —
// updated below, same leg, same displacement, same formula otherwise.
func TestWarpLeg_BlendIsSmoothstepNotLinear(t *testing.T) {
	pts := warpTestLeg(201, 5) // 1000 m of measure, 8 m displacement => a 40 m zone => 8 gaps
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	startT := &WarpTarget{Lat: pts[0].Lat + 8/metresPerDeg, Lon: pts[0].Lon}

	got, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}

	full := startT.Lat - pts[0].Lat
	weightAt := func(idx int) float64 { return (got[idx].Lat - pts[idx].Lat) / full }

	// Index 2 is a quarter of the way through the zone.
	if w := weightAt(2); w < 0.80 {
		t.Fatalf("weight at 1/4 of the zone is %v; smoothstep gives 0.84375, a linear ramp 0.75", w)
	}
	// Index 6 is three quarters through.
	if w := weightAt(6); w > 0.20 {
		t.Fatalf("weight at 3/4 of the zone is %v; smoothstep gives 0.15625, a linear ramp 0.25", w)
	}
}

// TestWarpLeg_ZonesNeverOverlap DELETED — 2026-08-14, same day it was
// written. It verified the FLOOR CANDIDATE's own zones — [0, 0.1L] and
// [0.9L, L] — were disjoint by construction, guarding that arithmetic
// against a rounding path that could let them meet on a short leg. Its own
// comment already said this was not a claim about the runtime zone (which
// could be far wider than 0.1L even before today) — and now the floor term
// it was testing is gone from the formula entirely (endpointTaperFraction's
// own doc, warp.go, has the measurement that killed it), so the arithmetic
// this test checked no longer exists to overlap or not. The runtime
// disjointness this test was never actually a proxy for stays covered by
// TestWarp_DisjointnessRefusesOverlap / TestWarp_DisjointnessAllowsAdequateLeg,
// which check the ACTUAL zone at every call, unchanged by this deletion.

// --- the widened, capped zone rule (2026-08-14) — ONE rule now, endpoints included ---
//
// ⚠ THIS SECTION USED TO BE SCOPED TO "node-in-place moves only", AND
// TestWarpLegWidened_DoesNotChangeS2 stood here asserting the opposite of
// what's true now. It was ruled the same day, after this section first landed:
// "Yeah, all warping should change to this rule." So every test below now
// exercises WarpLegEndpoints directly — the ONE entry point, used by the
// endpoint warp and by node-in-place moves alike — and the old
// endpoints-must-not-move test is
// deleted rather than kept as a stale pin.

// TestWarp_AlwaysTapersRegardlessOfFloor replaces both
// TestWarp_NeverDeclinesOrSnapsWhereFlatRuleUsedTo and
// TestWarp_TapersAboveFloorFallsBelowIt (RESOLVED 2026-08-14: decline and the
// plain-snap outcome are deleted — "there is one method for moving a node,"
// see warp_core.go's file comment). There is
// no longer a floor WarpLegEndpoints itself respects; given a target, it
// always tapers, whether the leg is comfortably above the genuine
// 1.5x-displacement floor or nowhere near it.
//
// Run on 2-point legs specifically: a 2-point leg is exactly one edge, and one
// edge cannot invert against itself, so tapering it is safe regardless of how
// far below the floor it sits. See TestWarp_MultiPointBelowFloorCanFold for the
// multi-point case, which has no such protection and is allowed to fold —
// folds were ruled unconstrained 2026-08-14, the same day
// collapseLegIfBelowTaperFloor (which used to rescue this case upstream)
// was deleted.
//
// The only length branch left is zoneFor's min(legLen, widenedZoneFactor*d),
// so the cases sit on each side of the 1.5d floor and of the 5d cap.
func TestWarp_AlwaysTapersRegardlessOfFloor(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	const displacement = 8.0
	for _, legLen := range []float64{
		0.1 * displacement, // far below the 1.5d floor; legLen caps the zone
		1.5 * displacement, // exactly on the floor
		5 * displacement,   // legLen == widenedZoneFactor*d: both terms of min agree
		6 * displacement,   // just past the cap: 5d sets the zone
		100 * displacement, // a long leg, the zone a small part of it
	} {
		pts := warpTestLeg(2, legLen)
		startT := &WarpTarget{Lat: pts[0].Lat + displacement/metresPerDeg, Lon: pts[0].Lon}
		got, rep, err := WarpLegEndpoints(pts, startT, nil)
		if err != nil {
			t.Fatalf("legLen=%.3f displacement=%.3f: %v", legLen, displacement, err)
		}
		if !rep.StartWarped {
			t.Fatalf("legLen=%.3f displacement=%.3f: got %+v, want a taper — there is no other outcome",
				legLen, displacement, rep)
		}
		if got[0].Lat != startT.Lat || got[0].Lon != startT.Lon {
			t.Fatalf("legLen=%.3f displacement=%.3f: terminal did not land on the target", legLen, displacement)
		}
	}
}

// TestWarp_MultiPointBelowFloorCanFold is the other half of the contract
// change: WarpLegEndpoints has no protection against folding a MULTI-point
// leg below the floor, and now nothing upstream rescues it either —
// collapseLegIfBelowTaperFloor, which used to reduce such a leg to its two
// terminals first, is DELETED (2026-08-14: "get rid of this whole
// idea of collapse... if the pips double back over each other, it's fine").
// A leg with interior points, displaced by more than 1.5x its own length,
// inverts on a real production call, and that is now an accepted outcome,
// not a defect this test exists to flag.
func TestWarp_MultiPointBelowFloorCanFold(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	// 21 points, 1 m apart = 20 m of measure; a 30 m displacement is well past
	// the floor (1.5*30 = 45 m > 20 m).
	pts := warpTestLeg(21, 1)
	startT := &WarpTarget{Lat: pts[0].Lat + 30/metresPerDeg, Lon: pts[0].Lon}
	got, rep, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	if !rep.StartWarped {
		t.Fatalf("expected a taper (there is no other outcome now): %+v", rep)
	}
	folded := false
	for k := 1; k < len(got); k++ {
		if got[k].Lat <= got[k-1].Lat {
			folded = true
			break
		}
	}
	if !folded {
		t.Fatal("expected this below-floor multi-point leg to fold under a direct call — if it didn't, the fixture's own displacement/legLen ratio moved and needs re-deriving")
	}
}

// TestWarp_FarEndNeverMovesWhenZoneIsCapped is the direct proof a
// review asked for: a leg short enough that the zone must be capped at
// legLen still leaves the FAR (unmoved) end's own point exactly where it
// was — smoothstep(1 - legLen/zone) = smoothstep(0) = 0 exactly whenever the
// cap binds, so the far end's weight is provably zero rather than merely
// small.
func TestWarp_FarEndNeverMovesWhenZoneIsCapped(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	// legLen=20, displacement=5: widenedZoneFactor*d = 25 > legLen, so the cap
	// binds (zone = legLen = 20) — but 20 >= 1.5*5 = 7.5, comfortably above
	// the floor, so it still tapers; the cap alone must not push it into
	// decline, and the far end must not move.
	pts := warpTestLeg(2, 20)
	startT := &WarpTarget{Lat: pts[0].Lat + 5/metresPerDeg, Lon: pts[0].Lon}
	got, rep, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	if !rep.StartWarped {
		t.Fatalf("expected a taper (legLen=20 >= 1.5*5=7.5, well above the floor): %+v", rep)
	}
	far := len(got) - 1
	if got[far].Lat != pts[far].Lat || got[far].Lon != pts[far].Lon {
		t.Fatalf("far end moved even though the zone was capped at the leg's own length: got (%v,%v), want unchanged (%v,%v)",
			got[far].Lat, got[far].Lon, pts[far].Lat, pts[far].Lon)
	}
}

// TestWarp_AccumulateSumsOverlappingZones replaces
// TestWarp_DisjointnessScalesDownProportionally now that overlap is handled
// by SUMMING each end's displacement field rather than by proportionally
// shrinking the two zones (WarpLegEndpoints' "THE FIX: accumulate" comment).
// 100 m leg, 15 m at each end: zoneFor gives each end
// min(100, max(10, 5*15)) = 75 m — genuinely overlapping (75+75=150 > 100),
// covering the ENTIRE interior, not just a boundary case.
//
// The property under test is additivity: because each end's contribution
// (applyWarpAtEnd) reads only pts/cum and its own target — never `out`, never
// the other end — running with both targets set must produce, at every
// point, EXACTLY the sum of what running with only start set and only end set
// would each produce alone. That is what "accumulate" means; if either end's
// write ever clobbered the other's instead of adding to it, this would fail.
//
// This shape is reachable from the endpoint warp's OWN entry point
// (warpLegEndpointsToNodes passes both ends when both terminals are near
// existing nodes) — a REAL 5.8 cm leg in real data hit an overlapping
// shape like this and aborted an entire file's import before accumulation
// replaced the disjointness error.
func TestWarp_AccumulateSumsOverlappingZones(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	// 11 points at 10 m spacing = 100 m of measure.
	pts := warpTestLeg(11, 10)
	startT := &WarpTarget{Lat: pts[0].Lat + 15/metresPerDeg, Lon: pts[0].Lon}
	last := len(pts) - 1
	endT := &WarpTarget{Lat: pts[last].Lat - 15/metresPerDeg, Lon: pts[last].Lon}

	both, rep, err := WarpLegEndpoints(pts, startT, endT)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v (must accumulate, not error)", err)
	}
	if !rep.StartWarped || !rep.EndWarped {
		t.Fatalf("expected both ends to taper (75 m zones, above the 22.5 m floor): %+v", rep)
	}

	startOnly, _, err := WarpLegEndpoints(pts, startT, nil)
	if err != nil {
		t.Fatalf("warpLegEndpoints (start only): %v", err)
	}
	endOnly, _, err := WarpLegEndpoints(pts, nil, endT)
	if err != nil {
		t.Fatalf("warpLegEndpoints (end only): %v", err)
	}

	// The overlap must be real, or this test would pass vacuously (the same
	// failure mode the fold instrument hit earlier in development — a zero
	// result that looks like success but means the scenario never exercised
	// the thing being tested). Confirm the interior actually moved under
	// EACH single-end run at a shared point before checking additivity.
	mid := 5
	if startOnly[mid].Lat == pts[mid].Lat && endOnly[mid].Lat == pts[mid].Lat {
		t.Fatalf("neither end's zone reached the midpoint — this scenario doesn't exercise overlap")
	}

	// Compared with a tiny tolerance, not bit-exactly: this "want" is computed
	// by a DIFFERENT arithmetic path than production (subtracting pts back out
	// of each single-end result, rather than production's own
	// dispLat[i]+=vLat*w accumulation), so the two can differ by up to a few
	// ULP even when accumulation is working correctly. The property under
	// test is that the two contributions ADD (not that this test's own
	// re-derivation of the sum matches bit-for-bit) — see
	// TestWarp_TerminalExactRegardlessOfOverlap for the bit-exact claim,
	// which only applies at the terminals themselves.
	const epsDeg = 1e-12
	for i := range pts {
		wantLat := pts[i].Lat + (startOnly[i].Lat - pts[i].Lat) + (endOnly[i].Lat - pts[i].Lat)
		wantLon := pts[i].Lon + (startOnly[i].Lon - pts[i].Lon) + (endOnly[i].Lon - pts[i].Lon)
		if math.Abs(both[i].Lat-wantLat) > epsDeg || math.Abs(both[i].Lon-wantLon) > epsDeg {
			t.Fatalf("point %d: got (%v,%v), want the SUM of each end's own contribution (%v,%v) — "+
				"one end's write clobbered the other's instead of adding to it",
				i, both[i].Lat, both[i].Lon, wantLat, wantLon)
		}
	}
}

// TestWarp_DisjointnessAllowsAdequateLeg is the boundary sibling: 150 m is
// exactly where two 15 m moves' zones (5*15=75 m each, neither capped) sum to
// the leg's own length — the zones TOUCH at the midpoint but do not overlap,
// so this is the edge case that proves zoneFor's cap arithmetic is exact
// right at equality, with no accumulation actually exercised (contrast
// TestWarp_AccumulateSumsOverlappingZones, which is the same target
// displacements on a shorter, genuinely overlapping leg).
func TestWarp_DisjointnessAllowsAdequateLeg(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpTestLeg(2, 150)
	startT := &WarpTarget{Lat: pts[0].Lat + 15/metresPerDeg, Lon: pts[0].Lon}
	endT := &WarpTarget{Lat: pts[len(pts)-1].Lat - 15/metresPerDeg, Lon: pts[len(pts)-1].Lon}

	_, rep, err := WarpLegEndpoints(pts, startT, endT)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	if !rep.StartWarped || !rep.EndWarped {
		t.Fatalf("expected both ends to taper on a 150 m leg: %+v", rep)
	}
}

// TestWarp_EachEndsZoneIndependentOfTheOther is DELETED (2026-08-14). It
// proved that one end's SNAP/DECLINE outcome didn't leak from the other end
// being set — both outcomes are gone, so there is nothing left to leak. The
// deeper property (positions, not outcomes, are independent per end and
// SUM where zones overlap) is already TestWarp_AccumulateSumsOverlappingZones,
// which this would have duplicated.

// TestWarp_TerminalExactRegardlessOfOverlap is the constructive proof
// WarpLegEndpoints' file comment claims ("WHY THIS IS SAFE AT THE ENDPOINTS
// BY CONSTRUCTION, NOT BY ASSERTION"): even on the heavily-overlapping 100 m
// leg from TestWarp_AccumulateSumsOverlappingZones, where the interior is
// touched by BOTH ends' accumulators, each terminal must still land
// bit-exact on its own target — because at each terminal the OTHER end's
// weight is provably zero (index 0 sits outside end's zone entirely, and
// symmetrically for the far end), so the exact-assignment list, not the
// accumulator sum, decides that point.
func TestWarp_TerminalExactRegardlessOfOverlap(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpTestLeg(11, 10)
	startT := &WarpTarget{Lat: pts[0].Lat + 15/metresPerDeg, Lon: pts[0].Lon}
	last := len(pts) - 1
	endT := &WarpTarget{Lat: pts[last].Lat - 15/metresPerDeg, Lon: pts[last].Lon}

	got, rep, err := WarpLegEndpoints(pts, startT, endT)
	if err != nil {
		t.Fatalf("warpLegEndpoints: %v", err)
	}
	if !rep.StartWarped || !rep.EndWarped {
		t.Fatalf("test setup: expected both ends to taper (overlapping zones): %+v", rep)
	}
	if got[0].Lat != startT.Lat || got[0].Lon != startT.Lon {
		t.Fatalf("start terminal (%v,%v) is not bit-exact on its target (%v,%v) despite the overlap",
			got[0].Lat, got[0].Lon, startT.Lat, startT.Lon)
	}
	if got[last].Lat != endT.Lat || got[last].Lon != endT.Lon {
		t.Fatalf("end terminal (%v,%v) is not bit-exact on its target (%v,%v) despite the overlap",
			got[last].Lat, got[last].Lon, endT.Lat, endT.Lon)
	}
}

// TestCheckTerminalsExact_CatchesAViolation proves CheckTerminalsExact's
// failure branch actually fires — a guard that has only ever been observed
// passing is not proven able to fail (see its doc comment). Hands it a
// deliberately wrong `out` directly, bypassing WarpLegEndpoints entirely, so
// this is independent of whatever the accumulator currently computes.
func TestCheckTerminalsExact_CatchesAViolation(t *testing.T) {
	start := &WarpTarget{Lat: 51.0, Lon: -3.0}
	end := &WarpTarget{Lat: 51.001, Lon: -3.001}
	out := []Point{
		{Lat: start.Lat + 1e-9, Lon: start.Lon}, // wrong: off by a hair
		{Lat: 51.0005, Lon: -3.0005},
		{Lat: end.Lat, Lon: end.Lon}, // correct
	}

	if err := CheckTerminalsExact(out, start, end); err == nil {
		t.Fatal("expected an error for a start terminal that is not bit-exact on its target, got nil")
	}

	// Fix the start terminal; must now pass.
	out[0] = Point{Lat: start.Lat, Lon: start.Lon}
	if err := CheckTerminalsExact(out, start, end); err != nil {
		t.Fatalf("expected no error once both terminals are bit-exact: %v", err)
	}

	// Break the end terminal instead.
	out[2] = Point{Lat: end.Lat, Lon: end.Lon + 1e-9}
	if err := CheckTerminalsExact(out, start, end); err == nil {
		t.Fatal("expected an error for an end terminal that is not bit-exact on its target, got nil")
	}

	// RESOLVED 2026-08-14: decline is gone, so there is no longer an exempt
	// case — a nil target (not passed at all) is the only way to skip this
	// check now, and that's exercised by every other test that only sets one
	// of start/end.
	out[2] = Point{Lat: end.Lat, Lon: end.Lon}
	if err := CheckTerminalsExact(out, start, nil); err != nil {
		t.Fatalf("expected no error when end has no target at all: %v", err)
	}
}
