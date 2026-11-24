package merge

// bandinterval_test.go — acceptance for the exact band solve.
//
// Three things are under test, in decreasing order of how badly they would
// hurt if wrong:
//
//  1. THE PROJECTION CONTRACT. BandInterval must solve in exactly the plane
//     ClosestPointOnSeg projects into. A wrong metric drifts by millimetres
//     over 15 m — below any sampled reference's resolution, so the sampled
//     comparison below cannot see it. TestBandIntervalProjectionContract pins
//     the frame's coefficients bit-for-bit (lenSq in ulps — see the FMA note
//     there), and TestBandIntervalEndpointsSitOnTheBand pins the solved roots
//     against production's own distance function, which is where a wrong
//     plane would actually show up.
//  2. AGREEMENT WITH THE THING BEING DELETED. The densified scan becomes the
//     brute-force reference (bruteBandScan below is a local copy of
//     DensifySource's sampling approach, deliberately not a call into it, so
//     DensifySource stays deletable).
//  3. DEGENERACY. Zero-length canon and source edges, parallel edges, tangency
//     at exactly d, wholly in and wholly out.
//
// Every case below is a hand-placed geometry, one per branch and edge. These
// tests were random sweeps of up to 200,000 cases; they were cut to this
// shape on 2026-10-05 (one case per logic branch, plus its edges), and each
// cut test was red-proofed against the mutation its sweep caught.

import (
	"math"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/detmath"
)

// ---- helpers ----------------------------------------------------------

// bandMetricPinM is the one assertion bound in this file that is a chosen
// number rather than a derived one, and it is chosen by bracketing rather than
// by taste: the metric pin has to sit well ABOVE the residual the solve
// actually carries (1.6e-9 m, measured over 98,512 roots) and well BELOW the
// smallest drift a wrong projection plane would introduce (1.22e-5 m for a
// probe-latitude cosLat, 1.4e-3 m for haversine — both measured in
// TestBandIntervalProjectionContract). A micron is the midpoint in log space,
// with ~3 orders of headroom either side. It is a TEST bound: nothing in
// bandinterval.go compares against an epsilon.
const bandMetricPinM = 1e-6

// offsetPt returns the point eastM metres east and northM metres north of
// (baseLat, baseLon), in the same equirectangular sense the matcher uses.
func offsetPt(baseLat, baseLon, eastM, northM float64) Point {
	return Point{
		Lat: baseLat + northM/MetresPerDeg,
		Lon: baseLon + eastM/(MetresPerDeg*detmath.Cos(baseLat*math.Pi/180)),
	}
}

// lerpPt is the moving point X(t) along the source edge: the lat/lon lerp,
// which is what DensifySource inserts and what BandInterval solves over. The
// float64 conversions keep arm64 from fusing it into an FMA, so a fixture
// measures the same point on every architecture (pkg/detmath).
func lerpPt(p, q Point, t float64) Point {
	return Point{
		Lat: p.Lat + float64(t*(q.Lat-p.Lat)),
		Lon: p.Lon + float64(t*(q.Lon-p.Lon)),
	}
}

// bruteBandScan is the reference implementation: DensifySource's sampling
// approach applied to one source edge. It returns the first and last sampled
// position within d of the canon edge, and the sampling resolution in t.
//
// Deliberately a local copy rather than a call into DensifySource — that
// function is scheduled for deletion, and this reference must outlive it.
func bruteBandScan(p, q, a, b Point, d float64, samples int) (lo, hi float64, ok bool, dt float64) {
	dt = 1 / float64(samples)
	for i := 0; i <= samples; i++ {
		t := float64(i) / float64(samples)
		x := lerpPt(p, q, t)
		dist, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, x.Lat, x.Lon)
		if dist <= d {
			if !ok {
				lo, ok = t, true
			}
			hi = t
		}
	}
	return lo, hi, ok, dt
}

// joinMargins reports the signed gap at each junction between consecutive
// regime pieces: positive is a gap (convexity violated), negative an overlap,
// zero the contiguity the design claims.
func joinMargins(p, q, a, b Point, d float64) ([]float64, error) {
	pieces, n, _, _, err := BandPieces(p, q, a, b, d)
	if err != nil {
		return nil, err
	}
	var out []float64
	for i := 1; i < n; i++ {
		out = append(out, pieces[i].Lo-pieces[i-1].Hi)
	}
	return out, nil
}

// bandCase is one hand-placed (P→Q, A→B, d) geometry.
type bandCase struct {
	name       string
	p, q, a, b Point
	d          float64
}

// bitsPt rebuilds a recorded point from its IEEE-754 bits, so a fixture is the
// exact geometry that was measured and not a decimal rounding of it.
func bitsPt(lat, lon uint64) Point {
	return Point{Lat: math.Float64frombits(lat), Lon: math.Float64frombits(lon)}
}

// boundaryCase is the engineered worst case for the regime join: a band root
// landing ON a regime boundary, crossed at a shallow angle so the near-vertex
// quadratic and the perpendicular linear form have the hardest time agreeing
// about the same distance. The canon edge runs canonLen metres east from A;
// the source edge passes through W, which sits exactly d north of A (atB
// false, the s=0 boundary) or of B (the s=1 boundary), shifted jitterM east,
// at angle φ, from l0 metres before W to l1 metres after it.
func boundaryCase(name string, d, canonLen float64, atB bool, jitterM, φ, l0, l1 float64) bandCase {
	const baseLat, baseLon = 53.3811, -1.8730
	wE := 0.0
	if atB {
		wE = canonLen
	}
	return bandCase{
		name: name,
		p:    offsetPt(baseLat, baseLon, wE+jitterM-float64(l0*detmath.Cos(φ)), d-float64(l0*detmath.Sin(φ))),
		q:    offsetPt(baseLat, baseLon, wE+jitterM+float64(l1*detmath.Cos(φ)), d+float64(l1*detmath.Sin(φ))),
		a:    offsetPt(baseLat, baseLon, 0, 0),
		b:    offsetPt(baseLat, baseLon, canonLen, 0),
		d:    d,
	}
}

// boundaryJoinCases put the band root exactly on each regime boundary, and a
// nanometre either side of it. Every one of them takes a regime join, decided
// IN band.
var boundaryJoinCases = []bandCase{
	boundaryCase("on s=0", 10, 200, false, 0, 0.01, 100, 100),
	boundaryCase("1 nm past s=0", 10, 200, false, 1e-9, 0.01, 100, 100),
	boundaryCase("1 nm before s=0", 10, 200, false, -1e-9, 0.01, 100, 100),
	boundaryCase("on s=1", 10, 200, true, 0, 0.01, 100, 100),
	boundaryCase("1 nm past s=1", 10, 200, true, 1e-9, 0.01, 100, 100),
	boundaryCase("1 nm before s=1", 10, 200, true, -1e-9, 0.01, 100, 100),
}

// joinFiringCases are geometries on which the convexity assertion fires
// without the D2(i) join verdict: one join, where the lower solve reaches t_b
// and the upper does not, so without the verdict's assignment the pieces leave
// a gap. Seed 7 cases 1522, 11480 and 12702 are from the former 20,000-case
// boundary stress generator, recorded bit for bit on 2026-10-05 by re-running
// it with the verdict's assignment removed (gaps 5.884e-15, 2.33e-15 and
// 9.99e-16 in t). All take one join, decided IN band.
//
// Re-picked 2026-11-04, when the solve stopped fusing into FMAs on arm64
// (pkg/detmath): seed 7 case 3313 took a join only with arm64's fused
// rounding, and took none on amd64. Its place is "seed 20261104 case 177982":
// boundaryCase over seed 20261104 (d in {8,10,15}, canon 50–300 m, jitter
// 1e-12–1e-3 m log-uniform, φ ±0.5, legs 20–120 m, canon reversed half the
// time), the first lower-only join of 3M draws, which took 23 lower-only and
// 17 upper-only joins in all.
var joinFiringCases = []bandCase{
	{name: "seed 7 case 1522",
		p: bitsPt(0x404ab0bc0f183e82, 0xbffdf3382cf712dd), q: bitsPt(0x404ab0cab23ce8de, 0xbffddcbf37dbbef4),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffde7d98fabcc8d),
		d: 15},
	{name: "seed 20261104 case 177982",
		p: bitsPt(0x404ab0cd33ac7c32, 0xbffded91d8629faf), q: bitsPt(0x404ab0cbbd3e1989, 0xbffde1f57411844a),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffde6738787a2d2), b: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b),
		d: 15},
	{name: "seed 7 case 11480",
		p: bitsPt(0x404ab0ce0256e2d5, 0xbffdeac2c6af6445), q: bitsPt(0x404ab0c86869e948, 0xbffde1d5cad45834),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffde5b291fcd512),
		d: 10},
	{name: "seed 7 case 12702",
		p: bitsPt(0x404ab0ce3dd7d3d5, 0xbffdf8b8df7ca5a7), q: bitsPt(0x404ab0ca8046c9ff, 0xbffde49dab018fbc),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffdee5033073e66),
		d: 15},
}

// joinNearGapCase is a geometry whose two regimes leave their gap on the NEAR
// side of a boundary: the lower piece's own root stops short of t_b and only
// the upper solve reaches it. The firings above all leave it on the far side.
// Two joins, both decided IN band.
//
// Re-picked 2026-11-04 (pkg/detmath): the former case, seed 20260728 case
// 3667, had its near-side gap only with arm64's fused rounding; unfused, on
// every architecture, both solves reach its t_b. This is the first
// upper-only join of the joinFiringCases generator above (seed 20261104).
var joinNearGapCase = bandCase{name: "seed 20261104 case 330118, near-side gap",
	p: bitsPt(0x404ab0c9027b707f, 0xbffdf9432319d0af), q: bitsPt(0x404ab0cb452c6e6e, 0xbffdeec33d55aaf3),
	a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffdf389783d8d48),
	d: 8}

// joinBothReachCase is a band root on the s=1 boundary with d 4e-11 m below
// ClosestPointOnSeg's distance at the boundary point (7.9999999997988622 m):
// that function, measuring the rounded lat/lon lerp, puts the point outside
// the band, while both solves reach it. Before the verdict followed the solves
// it was decided OUT here, and the pieces met only because both were clipped to
// t_b by their own roots. Found 2026-10-05 by stepping d in 1e-12 relative
// steps over the former boundary generator's seed 7, case 1 (step −30).
var joinBothReachCase = bandCase{name: "closestPointOnSeg says out, both solves reach t_b",
	p: bitsPt(0x404ab0c498ea2ef3, 0xbffde8162ec7f9e8), q: bitsPt(0x404ab0c6aa8891a5, 0xbffde3f54bb1b9ae),
	a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffde63b19a82cad),
	d: math.Float64frombits(0x401ffffffffbe078)}

// falseConvexityErrorCases are geometries on which BandInterval returned a
// hard "convexity violated" error although the band is one interval. The
// verdict then measured ClosestPointOnSeg at the rounded lat/lon lerp X(t_b),
// which put t_b outside the band while one solve reached it and the other
// stopped 1e-15 to 2e-13 past it. Found 2026-10-05 by stepping d in 1e-12
// relative steps over the former boundary generator's seed 7 (case, step):
// 1567 +38, where the lower solve reaches t_b; 4101 −13, where the upper one
// does.
//
// 2026-11-04 (pkg/detmath): "seed 7 case 2335 step −15", a second lower-solve
// case, is gone. Its dispute existed only with arm64's fused rounding, and no
// replacement was found unfused: stepping d over ±2e-9 relative around it, and
// ±300 steps of 1e-12 over 3,000 generated join geometries, gave no disputed
// lower-only join. 1567 still covers that branch.
var falseConvexityErrorCases = []struct {
	bandCase
	lowReaches bool // which solve reaches t_b (the other stops short)
}{
	{bandCase{name: "seed 7 case 1567 step +38",
		p: bitsPt(0x404ab0cb335f9d5d, 0xbffe0276d140a3b9), q: bitsPt(0x404ab0ca381f3ddc, 0xbffdf78d1ea8427b),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffdef85a87e50b7),
		d: math.Float64frombits(0x4020000000029c81)}, true},
	{bandCase{name: "seed 7 case 4101 step −13",
		p: bitsPt(0x404ab0c62a2f592b, 0xbffe01f91604166a), q: bitsPt(0x404ab0cce6bfd2a8, 0xbffdf34cd0d1791f),
		a: bitsPt(0x404ab0c7e28240b8, 0xbffdf7ced916872b), b: bitsPt(0x404ab0c7e28240b8, 0xbffded4b0945cf5f),
		d: math.Float64frombits(0x4023fffffffee220)}, false},
}

// ---- 1. the projection contract ---------------------------------------

// TestBandIntervalProjectionContract pins the plane the solve works in to
// production's. The sampled comparison below cannot do this — a
// probe-latitude cosLat or a haversine distance drifts by millimetres over
// 15 m, far under its resolution — so every coefficient the solve consumes is
// checked here against the contract's own formulas, exactly.
//
// Each assertion pins one exact formula, so one ordinary edge fails a wrong
// formula. The edges add the latitudes where cosLat matters (the equator,
// where it is flat; 53°N; 85°N) and the one branch the frame has: a
// zero-length edge.
//
// Red proof, 2026-10-05 (detached worktree): with NewSegFrame's cosLat taken
// at A's latitude instead of the mid-latitude, this fails on the first edge —
// "equator: frame cosLat = 1, want the canon edge's mid-latitude cosine
// 0.99999999987681587".
func TestBandIntervalProjectionContract(t *testing.T) {
	edges := []struct {
		name string
		a, b Point
	}{
		{"equator", Point{Lat: 0, Lon: 10}, offsetPt(0, 10, 300, 200)},
		{"53N", Point{Lat: 53.3811, Lon: -1.8730}, offsetPt(53.3811, -1.8730, 300, 200)},
		{"85N", Point{Lat: 85, Lon: 40}, offsetPt(85, 40, -250, 400)},
		{"zero length", Point{Lat: 53.3811, Lon: -1.8730}, Point{Lat: 53.3811, Lon: -1.8730}},
	}
	for _, e := range edges {
		a, b := e.a, e.b
		f := NewSegFrame(a.Lat, a.Lon, b.Lat, b.Lon)

		// cosLat at the MID-LATITUDE of the arc being projected onto — not
		// the probe's, not per-leg.
		wantCos := detmath.Cos((a.Lat + b.Lat) * 0.5 * math.Pi / 180.0)
		if f.CosLat != wantCos {
			t.Fatalf("%s: frame cosLat = %.17g, want the canon edge's mid-latitude cosine %.17g (A=%.17g,%.17g B=%.17g,%.17g)",
				e.name, f.CosLat, wantCos, a.Lat, a.Lon, b.Lat, b.Lon)
		}
		// MetresPerDeg for latitude, MetresPerDeg·cosLat for longitude — planar,
		// never haversine.
		wantBx := float64((b.Lon - a.Lon) * MetresPerDeg * wantCos)
		wantBy := float64((b.Lat - a.Lat) * MetresPerDeg)
		if f.Bx != wantBx || f.By != wantBy {
			t.Fatalf("%s: frame basis = (%.17g,%.17g), want (%.17g,%.17g)", e.name, f.Bx, f.By, wantBx, wantBy)
		}
		// lenSq, bitwise. It was checked within 2 ulps while arm64 fused one
		// of `bx*bx + by*by`'s products into an FMA at the compiler's choice;
		// since 2026-11-04 every product is converted to float64, so nothing
		// fuses and the same text gives the same bits (pkg/detmath).
		wantLenSq := float64(wantBx*wantBx) + float64(wantBy*wantBy)
		if f.LenSq != wantLenSq {
			t.Fatalf("%s: frame lenSq = %.17g, want %.17g", e.name, f.LenSq, wantLenSq)
		}
		// The frame's degeneracy test is production's, so duplicate canon
		// vertices take the same branch in both.
		if (f.LenSq == 0) != (a.Lat == b.Lat && (a.Lon == b.Lon || wantCos == 0)) {
			t.Fatalf("%s: frame degeneracy disagrees with the geometry: lenSq=%.17g A=(%.17g,%.17g) B=(%.17g,%.17g)",
				e.name, f.LenSq, a.Lat, a.Lon, b.Lat, b.Lon)
		}

		// project is production's function, so it agrees by construction rather
		// than by luck. Checked anyway, so a future inlining of it fails here.
		x := offsetPt(a.Lat, a.Lon, 170, -60)
		gotD, gotT := f.Project(x.Lat, x.Lon)
		wantD, wantT := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, x.Lat, x.Lon)
		if gotD != wantD || gotT != wantT {
			t.Fatalf("%s: segFrame.project diverged from closestPointOnSeg: got (%.17g, %.17g) want (%.17g, %.17g)",
				e.name, gotD, gotT, wantD, wantT)
		}
	}

	// The contract has teeth, measured rather than asserted from the design: a
	// probe-latitude cosLat is a DIFFERENT plane, and the size of its drift is
	// what TestBandIntervalEndpointsSitOnTheBand's residual has to beat.
	a := Point{Lat: 53.3811, Lon: -1.8730}
	b := offsetPt(a.Lat, a.Lon, 300, 200)
	f := NewSegFrame(a.Lat, a.Lon, b.Lat, b.Lon)
	probe := offsetPt(a.Lat, a.Lon, 150, 115)
	right, _ := f.Project(probe.Lat, probe.Lon)

	wrongCos := detmath.Cos(probe.Lat * math.Pi / 180.0)
	wbx := float64((b.Lon - a.Lon) * MetresPerDeg * wrongCos)
	wby := float64((b.Lat - a.Lat) * MetresPerDeg)
	wpx := float64((probe.Lon - a.Lon) * MetresPerDeg * wrongCos)
	wpy := float64((probe.Lat - a.Lat) * MetresPerDeg)
	wt := (float64(wpx*wbx) + float64(wpy*wby)) / (float64(wbx*wbx) + float64(wby*wby))
	wrong := detmath.Hypot(wpx-float64(wt*wbx), wpy-float64(wt*wby))
	if right == wrong {
		t.Fatalf("probe-latitude frame produced an identical distance (%.17g); the contract pin has no teeth", right)
	}
	t.Logf("drift from a probe-latitude cosLat at 53.4N over a 360 m edge: %.3e m", math.Abs(right-wrong))

	// Haversine is the other wrong metric — reported for the same reason.
	hav := HaversineMetres(a.Lat, a.Lon, probe.Lat, probe.Lon)
	planar, _ := ClosestPointOnSeg(a.Lat, a.Lon, a.Lat, a.Lon, probe.Lat, probe.Lon)
	t.Logf("drift from haversine vs the planar frame over a 190 m offset: %.3e m", math.Abs(hav-planar))
}

// TestBandIntervalEndpointsSitOnTheBand checks the solved roots against
// production's own distance function: an interior endpoint must be at exactly
// d from the canon edge. This is where a wrong plane would surface as
// millimetres, so it is the metric pin with the sharpest teeth.
//
// A wrong plane drifts 1.2e-5 m on an ordinary 53°N edge, 12× the pin, so one
// interior root per regime fails it. The canon edge runs north-east, so its
// mid-latitude differs from both vertices' latitudes. The cases are the three
// regimes at the equator, 53°N and 67°N, plus a source that starts inside the
// band, whose first endpoint is clipped to t = 0 rather than solved.
//
// Red proof, 2026-10-05 (detached worktree): with NewSegFrame's cosLat taken
// at A's latitude instead of the mid-latitude, this fails on "53N near A" —
// "endpoint t=0.58759059507326317 sits 0.000166 m off the band".
func TestBandIntervalEndpointsSitOnTheBand(t *testing.T) {
	const d = 10.0
	// In the local metre frame of each base: the canon edge runs from A at the
	// origin to B at (300, 200); u is its unit direction and n its unit normal.
	const bE, bN = 300.0, 200.0
	l := math.Hypot(bE, bN)
	ux, uy := bE/l, bN/l
	nx, ny := -uy, ux
	at := func(along, across float64) (e, n float64) {
		return float64(along*ux) + float64(across*nx), float64(along*uy) + float64(across*ny)
	}
	type rootCase struct {
		name     string
		baseLat  float64
		p, q     [2]float64 // (along, across) in metres
		interior int        // interior endpoints the case must produce
	}
	var cases []rootCase
	for _, base := range []struct {
		name string
		lat  float64
	}{{"equator", 0}, {"53N", 53.3811}, {"67N", 67.28}} {
		cases = append(cases,
			// Slanted so each source stays inside one regime but takes the
			// three-range branch, not the perpendicular one.
			rootCase{base.name + " near A", base.lat, [2]float64{-6, -50}, [2]float64{-4, 50}, 2},
			rootCase{base.name + " span", base.lat, [2]float64{float64(l/2) - 10, -50}, [2]float64{float64(l/2) + 10, 50}, 2},
			rootCase{base.name + " near B", base.lat, [2]float64{l + 4, -50}, [2]float64{l + 6, 50}, 2},
		)
	}
	cases = append(cases, rootCase{"53N clipped start", 53.3811, [2]float64{l / 2, 2}, [2]float64{float64(l/2) + 10, 50}, 1})

	worst := 0.0
	for _, c := range cases {
		const baseLon = -1.8730
		a := offsetPt(c.baseLat, baseLon, 0, 0)
		b := offsetPt(c.baseLat, baseLon, bE, bN)
		pe, pn := at(c.p[0], c.p[1])
		qe, qn := at(c.q[0], c.q[1])
		p := offsetPt(c.baseLat, baseLon, pe, pn)
		q := offsetPt(c.baseLat, baseLon, qe, qn)

		t0, t1, ok, err := BandInterval(p, q, a, b, d)
		if err != nil {
			t.Fatalf("%s: bandInterval: %v", c.name, err)
		}
		if !ok {
			t.Fatalf("%s: no band interval, want one with %d interior endpoint(s)", c.name, c.interior)
		}
		interior := 0
		for _, tt := range [2]float64{t0, t1} {
			if tt <= 0 || tt >= 1 {
				continue // clipped to the source edge's end, not a root
			}
			interior++
			x := lerpPt(p, q, tt)
			dist, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, x.Lat, x.Lon)
			e := math.Abs(dist - d)
			worst = math.Max(worst, e)
			if e > bandMetricPinM {
				t.Fatalf("%s: endpoint t=%.17g sits %.3g m off the band — the solve is not in production's plane", c.name, tt, e)
			}
		}
		if interior != c.interior {
			t.Fatalf("%s: interval [%.17g,%.17g] has %d interior endpoint(s), want %d", c.name, t0, t1, interior, c.interior)
		}
		if c.interior == 1 && t0 != 0 {
			t.Fatalf("%s: a source starting inside the band must be clipped to t=0, got t0=%.17g", c.name, t0)
		}
	}
	t.Logf("%d cases; worst |dist − d| = %.3e m", len(cases), worst)
}

// ---- 2. agreement with the densified scan --------------------------------

// TestBandIntervalMatchesDenseSampling pits the exact interval against the
// substrate it replaces. Two independent claims are checked per case:
//
//   - membership: every sample inside the analytic interval is within d, and
//     every sample within d is inside the analytic interval (resolution-free,
//     so this is the strong one);
//   - endpoints: the analytic interval's ends agree with the first/last
//     in-band sample to within one sampling step (resolution-bound).
//
// The cases take each regime (near A, span, near B) through crossing, tangent,
// wholly in and wholly out, plus one source through all three, and the three
// degenerate shapes. The span regime has no tangent case: its distance is
// linear in t, so its band edge is crossed, never touched.
//
// Red proofs, 2026-10-05 (detached worktree): with SolveAbsLE's upper root
// written (m + c0)/c1, this fails on "span crossing" — "t=0 is inside the
// analytic interval [0,0.39999999999946101] but 50 m from canon"; with the
// near-B offset taken from p + b instead of p − b, it fails on "near B
// crossing" — "t=0.41249999999999998 is 9.99215 m from canon (band 10 m) but
// outside the analytic interval [0,0] ok=false".
func TestBandIntervalMatchesDenseSampling(t *testing.T) {
	const (
		baseLat, baseLon = 53.3811, -1.8730
		d                = 10.0
		canonLen         = 200.0
		samples          = 4000
	)
	pt := func(e, n float64) Point { return offsetPt(baseLat, baseLon, e, n) }
	a, b := pt(0, 0), pt(canonLen, 0)
	// Tangent sources: a line exactly d from the vertex, leaning at α so it
	// stays in that vertex's regime, touching the disc at t = 1/3 — which is
	// not a sample position, so no sample sits on the knife edge.
	const α = 0.05
	tanA := func(s float64) Point {
		return pt(-float64(d*detmath.Cos(α))+float64(s*detmath.Sin(α)), float64(d*detmath.Sin(α))+float64(s*detmath.Cos(α)))
	}
	tanB := func(s float64) Point {
		return pt(canonLen+float64(d*detmath.Cos(α))-float64(s*detmath.Sin(α)), float64(d*detmath.Sin(α))+float64(s*detmath.Cos(α)))
	}
	// Every case states its canon edge: only "zero-length canon" differs.
	on := func(name string, p, q, b Point) bandCase {
		return bandCase{name: name, p: p, q: q, a: a, b: b, d: d}
	}
	cases := []bandCase{
		on("near A crossing", pt(-6, -50), pt(-4, 50), b),
		on("near A tangent", tanA(-30), tanA(60), b),
		on("near A wholly in", pt(-3, -2), pt(-2, 3), b),
		on("near A wholly out", pt(-30, -50), pt(-29, 50), b),
		on("span crossing", pt(90, -50), pt(110, 50), b),
		on("span wholly in", pt(50, 3), pt(150, -3), b),
		on("span wholly out", pt(50, 30), pt(150, 40), b),
		on("near B crossing", pt(canonLen+4, -50), pt(canonLen+6, 50), b),
		on("near B tangent", tanB(-30), tanB(60), b),
		on("near B wholly in", pt(canonLen+2, -2), pt(canonLen+3, 3), b),
		on("near B wholly out", pt(canonLen+30, -50), pt(canonLen+31, 50), b),
		on("all three regimes", pt(-50, 5), pt(canonLen+50, 6), b),
		on("zero-length source", pt(100, 5), pt(100, 5), b),
		on("zero-length canon", pt(-100, 6), pt(100, 6), a),
		on("parallel", pt(50, 5), pt(250, 5), b),
	}
	for _, c := range cases {
		p, q := c.p, c.q

		t0, t1, ok, err := BandInterval(p, q, c.a, c.b, d)
		if err != nil {
			t.Fatalf("%s: bandInterval errored: %v", c.name, err)
		}
		sLo, sHi, sOK, dt := bruteBandScan(p, q, c.a, c.b, d, samples)

		// Membership, resolution-free. Exact, with no allowance of any kind:
		// membership in the analytic interval and membership in the band are
		// the same statement, so ANY disagreement fails.
		for i := 0; i <= samples; i++ {
			tt := float64(i) / float64(samples)
			x := lerpPt(p, q, tt)
			dist, _ := ClosestPointOnSeg(c.a.Lat, c.a.Lon, c.b.Lat, c.b.Lon, x.Lat, x.Lon)
			inside := ok && tt >= t0 && tt <= t1
			switch {
			case inside && dist > d:
				t.Fatalf("%s: t=%.17g is inside the analytic interval [%.17g,%.17g] but %.6g m from canon (band %g m)\n%s",
					c.name, tt, t0, t1, dist, d, BandGeomStr(p, q, c.a, c.b, d))
			case !inside && dist <= d:
				t.Fatalf("%s: t=%.17g is %.6g m from canon (band %g m) but outside the analytic interval [%.17g,%.17g] ok=%v\n%s",
					c.name, tt, dist, d, t0, t1, ok, BandGeomStr(p, q, c.a, c.b, d))
			}
		}

		// Endpoints, to within the sampling resolution.
		switch {
		case sOK && !ok:
			t.Fatalf("%s: the sampled scan found band coverage [%.6g,%.6g] but the analytic solve found none", c.name, sLo, sHi)
		case !sOK && ok:
			// Legitimate only when the interval is finer than the sampling could
			// resolve; anything wider means the scan should have seen it.
			if t1-t0 > dt {
				t.Fatalf("%s: analytic interval [%.6g,%.6g] spans %.3g of the edge but no sample fell in band", c.name, t0, t1, t1-t0)
			}
		case sOK && ok:
			// Containment is a consequence of the solve being exact, and the
			// overhang bound is the scan's OWN step: the sample one step before
			// sLo was out of band, so the true boundary lies within one step.
			if sLo < t0 || sHi > t1 {
				t.Fatalf("%s: analytic [%.17g,%.17g] does not contain sampled [%.17g,%.17g]", c.name, t0, t1, sLo, sHi)
			}
			if sLo-t0 > dt || t1-sHi > dt {
				t.Fatalf("%s: analytic [%.17g,%.17g] overhangs sampled [%.17g,%.17g] by more than one step (%.3g)",
					c.name, t0, t1, sLo, sHi, dt)
			}
		}
	}
}

// TestBandIntervalJoinStressAtRegimeBoundary holds the regime join on the
// geometries that broke it. A band root landing exactly ON a regime boundary,
// crossed at a shallow angle, makes the near-vertex quadratic and the
// perpendicular linear form disagree in the last bits about one distance.
// Before the D2(i) join verdict existed, 20,000 such cases (seed 7) fired the
// convexity assertion 4 times, worst gap 5.884e-15 in t. Those four
// geometries are joinFiringCases; boundaryJoinCases add the root exactly on
// each boundary. Every join must close with a zero margin and no error.
//
// Red proof, 2026-10-05 (detached worktree): with the IN-band assignment in
// BandPieces removed, this fails on "seed 7 case 1522" — "convexity violated:
// … leave a gap of 5.8841820305133297e-15 in t".
func TestBandIntervalJoinStressAtRegimeBoundary(t *testing.T) {
	cases := append(append([]bandCase{}, joinFiringCases...), boundaryJoinCases...)
	for _, c := range cases {
		margins, err := joinMargins(c.p, c.q, c.a, c.b, c.d)
		if err != nil {
			t.Fatalf("%s: bandPieces failed outright: %v", c.name, err)
		}
		if len(margins) == 0 {
			t.Fatalf("%s: no regime join was taken — the case no longer exercises the join", c.name)
		}
		if _, _, _, err := BandInterval(c.p, c.q, c.a, c.b, c.d); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, m := range margins {
			if m != 0 {
				t.Fatalf("%s: join margin %.3e in t — the regime-join verdict did not make the adjacent pieces meet", c.name, m)
			}
		}
	}
}

// TestBandIntervalRegimeJoinVerdict pins D2(i)'s CONSTRUCTION, not merely its
// symptom. TestBandIntervalJoinStressAtRegimeBoundary asserts that no gap is
// reported, which a tolerance would also satisfy; this asserts the things that
// make the construction epsilon-free:
//
//  1. the boundary's membership is the solves' own: it is IN exactly when the
//     lower or the upper regime's solve reaches t_b, and both sides of that
//     "or" occur among the cases;
//  2. it is decided ONCE: when it says IN BAND both adjacent pieces end
//     exactly at t_b, bitwise, and the returned interval contains t_b.
//
// The OUT branch (neither solve reaches t_b, yet each found band on its own
// side) has no case: 4.84M near-boundary bands and 200,000 random ones took no
// OUT verdict once the verdict followed the solves. It is the arithmetic
// contradicting itself twice at one point, and it stays a hard error.
//
// Red proofs, 2026-10-05 (detached worktree). With the IN-band assignment
// moving only the far piece, this fails on the near-side gap case; moving only
// the near piece, on "seed 7 case 1522" — both "BandInterval failed: …
// convexity violated". With the verdict taking only the lower solve's reach, it
// fails on the near-side gap case (then seed 20260728 case 3667); only the upper solve's,
// on "seed 7 case 1522".
func TestBandIntervalRegimeJoinVerdict(t *testing.T) {
	cases := append(append([]bandCase{}, boundaryJoinCases...), joinFiringCases...)
	cases = append(cases, joinNearGapCase, joinBothReachCase)
	for _, c := range falseConvexityErrorCases {
		cases = append(cases, c.bandCase)
	}
	joins, onlyLow, onlyHigh := 0, 0, 0
	for _, c := range cases {
		p, q, a, b, d := c.p, c.q, c.a, c.b, c.d
		pieces, n, js, nj, err := BandPieces(p, q, a, b, d)
		if err != nil {
			t.Fatalf("%s: bandPieces failed: %v", c.name, err)
		}
		if nj == 0 {
			t.Fatalf("%s: no regime join was taken — the case no longer exercises the verdict", c.name)
		}
		t0, t1, ok, err := BandInterval(p, q, a, b, d)
		if err != nil {
			t.Fatalf("%s: bandInterval failed: %v", c.name, err)
		}
		for _, j := range js[:nj] {
			joins++
			// (1) the verdict is the solves' own.
			if j.InBand != (j.LowReaches || j.HighReaches) {
				t.Fatalf("%s: join at t=%.17g says inBand=%v, but the lower solve reaches it %v and the upper %v",
					c.name, j.T, j.InBand, j.LowReaches, j.HighReaches)
			}
			if !j.InBand {
				t.Fatalf("%s: join at t=%.17g decided OUT — neither solve reaches it, which no case here should produce: %v",
					c.name, j.T, pieces[:n])
			}
			switch {
			case j.LowReaches && !j.HighReaches:
				onlyLow++
			case j.HighReaches && !j.LowReaches:
				onlyHigh++
			}

			// (2) both adjacent regimes were made to agree with it, and the union
			// therefore contains t_b.
			met := false
			for i := 0; i+1 < n; i++ {
				if pieces[i].Hi == j.T && pieces[i+1].Lo == j.T {
					met = true
					break
				}
			}
			if !met {
				t.Fatalf("%s: verdict at t=%.17g says IN BAND but no two pieces meet there: %v", c.name, j.T, pieces[:n])
			}
			if !ok || j.T < t0 || j.T > t1 {
				t.Fatalf("%s: verdict at t=%.17g says IN BAND but the returned interval is [%.17g,%.17g] ok=%v", c.name, j.T, t0, t1, ok)
			}
		}
	}
	if onlyLow == 0 || onlyHigh == 0 {
		t.Fatalf("over %d joins, %d were reached by the lower solve alone and %d by the upper alone — "+
			"each side of the verdict's \"or\" needs a case", joins, onlyLow, onlyHigh)
	}
}

// TestBandIntervalJoinVerdictOnHandMadePieces pins both halves of the join
// verdict on hand-made pieces, because no geometry found reaches its OUT half:
// the boundary is IN when either adjacent solve reaches it, and when neither
// does, both pieces keep their own roots and the gap stays a hard error.
//
// Red proof, 2026-10-05 (detached worktree): with the verdict set to
// `inBand = true`, this fails on "neither solve reaches t_b" — every geometry
// test in this file still passes under that mutation.
func TestBandIntervalJoinVerdictOnHandMadePieces(t *testing.T) {
	const tb = 0.4
	ranges := [3]BandRegimeRange{
		{Kind: RegimeNearA, Lo: math.Inf(-1), Hi: tb},
		{Kind: RegimeSpan, Lo: tb, Hi: 0.7},
		{Kind: RegimeNearB, Lo: 0.7, Hi: math.Inf(1)},
	}
	short, past := math.Nextafter(tb, 0), math.Nextafter(tb, 1)
	cases := []struct {
		name            string
		lowHi, highLo   float64 // the pieces' own ends at the boundary
		wantIn, wantErr bool
	}{
		{"neither solve reaches t_b", short, past, false, true},
		{"only the lower solve reaches t_b", tb, past, true, false},
		{"only the upper solve reaches t_b", short, tb, true, false},
		{"both solves reach t_b", tb, tb, true, false},
	}
	for _, c := range cases {
		got := [3]BandPiece{
			{Kind: RegimeNearA, Lo: 0.1, Hi: c.lowHi},
			{Kind: RegimeSpan, Lo: c.highLo, Hi: 0.6},
		}
		has := [3]bool{true, true, false}
		joins, nj, err := JoinRegimePieces(ranges, 3, &got, has)
		if err != nil {
			t.Fatalf("%s: joinRegimePieces: %v", c.name, err)
		}
		if nj != 1 || joins[0].T != tb {
			t.Fatalf("%s: joins %v, want one at t=%v", c.name, joins[:nj], tb)
		}
		if joins[0].InBand != c.wantIn {
			t.Fatalf("%s: verdict inBand=%v, want %v", c.name, joins[0].InBand, c.wantIn)
		}
		if c.wantIn && (got[0].Hi != tb || got[1].Lo != tb) {
			t.Fatalf("%s: IN verdict but the pieces end at %.17g and %.17g, not on t_b", c.name, got[0].Hi, got[1].Lo)
		}
		if !c.wantIn && (got[0].Hi != c.lowHi || got[1].Lo != c.highLo) {
			t.Fatalf("%s: OUT verdict moved the pieces to %.17g and %.17g", c.name, got[0].Hi, got[1].Lo)
		}
		lo, hi, ok, err := UnionOneInterval(got[:2])
		if c.wantErr {
			if err == nil || !strings.Contains(err.Error(), "convexity violated") {
				t.Fatalf("%s: union of [%v,%v] and [%v,%v] gave [%v,%v] ok=%v err=%v, want the convexity error",
					c.name, got[0].Lo, got[0].Hi, got[1].Lo, got[1].Hi, lo, hi, ok, err)
			}
			continue
		}
		if err != nil || !ok || lo != 0.1 || hi != 0.6 {
			t.Fatalf("%s: union gave [%v,%v] ok=%v err=%v, want [0.1,0.6]", c.name, lo, hi, ok, err)
		}
	}
}

// TestBandIntervalNoFalseConvexityErrorAtABoundary holds the geometries on
// which BandInterval reported "convexity violated" for a band that is one
// interval. Each has its band root within ~1e-10 m of a regime boundary: one
// solve reaches t_b, the other stops just past it, and ClosestPointOnSeg at
// the rounded lerp X(t_b) — the old verdict — put t_b outside the band, so the
// gap was left and reported. The interval must now come back whole and contain
// t_b.
//
// Each case also asserts its own liveness: ClosestPointOnSeg still puts t_b
// outside the band, and the solve that reaches t_b is the recorded one, so the
// case still sits where the old verdict and the solves disagree.
//
// Red proof, 2026-10-05: on the code before the fix, with the
// verdict taken from ClosestPointOnSeg, this fails on "seed 7 case 1567 step
// +38" — "convexity violated: regime pieces … leave a gap of
// 1.5543122344752192e-15 in t".
func TestBandIntervalNoFalseConvexityErrorAtABoundary(t *testing.T) {
	for _, c := range falseConvexityErrorCases {
		p, q, a, b, d := c.p, c.q, c.a, c.b, c.d
		t0, t1, ok, err := BandInterval(p, q, a, b, d)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		pieces, n, js, nj, err := BandPieces(p, q, a, b, d)
		if err != nil {
			t.Fatalf("%s: bandPieces: %v", c.name, err)
		}
		// The join the old verdict got wrong is the one ClosestPointOnSeg puts
		// outside the band; there must be exactly one.
		var j BandJoin
		disputed := 0
		for _, jj := range js[:nj] {
			x := lerpPt(p, q, jj.T)
			if dist, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, x.Lat, x.Lon); dist > d {
				j = jj
				disputed++
			}
		}
		if disputed != 1 {
			t.Fatalf("%s: %d of %d joins sit outside the band by closestPointOnSeg, want exactly 1 — the case no longer sits where the old verdict and the solves disagree",
				c.name, disputed, nj)
		}
		if j.LowReaches != c.lowReaches || j.HighReaches == c.lowReaches {
			t.Fatalf("%s: lower solve reaches t_b %v, upper %v; recorded: only the %s one",
				c.name, j.LowReaches, j.HighReaches, map[bool]string{true: "lower", false: "upper"}[c.lowReaches])
		}
		if !ok || j.T < t0 || j.T > t1 {
			t.Fatalf("%s: t_b=%.17g is not in the returned interval [%.17g,%.17g] ok=%v (pieces %v)", c.name, j.T, t0, t1, ok, pieces[:n])
		}
	}
}

// ---- 3. degenerate and structural cases --------------------------------

// TestBandIntervalZeroLengthCanonEdge: duplicate consecutive canon vertices
// are pinned, supported behaviour, so a zero-length canon edge is the exact
// point-distance interval and NOT an error.
func TestBandIntervalZeroLengthCanonEdge(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := a // duplicate vertex

	// Source runs east from 100 m west of A to 100 m east, passing 6 m north.
	p := offsetPt(baseLat, baseLon, -100, 6)
	q := offsetPt(baseLat, baseLon, 100, 6)

	t0, t1, ok, err := BandInterval(p, q, a, b, 10)
	if err != nil {
		t.Fatalf("zero-length canon edge must be solved, not refused: %v", err)
	}
	if !ok {
		t.Fatal("expected the source to cross the disc around the duplicated vertex")
	}
	// The disc of radius 10 around A meets the line y=6 at x = ±8.
	wantLo, wantHi := (100-8)/200.0, (100+8)/200.0
	if math.Abs(t0-wantLo) > 1e-9 || math.Abs(t1-wantHi) > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want ≈[%.17g,%.17g]", t0, t1, wantLo, wantHi)
	}

	// And it agrees with ClosestPointOnSeg's own degenerate handling.
	for _, tt := range []float64{t0, t1} {
		x := lerpPt(p, q, tt)
		dist, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, x.Lat, x.Lon)
		if math.Abs(dist-10) > bandMetricPinM {
			t.Fatalf("endpoint t=%.17g is %.9g m from the duplicated vertex, want 10", tt, dist)
		}
	}

	// Wholly outside the disc.
	far0 := offsetPt(baseLat, baseLon, -100, 40)
	far1 := offsetPt(baseLat, baseLon, 100, 40)
	if _, _, ok, err := BandInterval(far0, far1, a, b, 10); err != nil || ok {
		t.Fatalf("expected empty, got ok=%v err=%v", ok, err)
	}
}

// TestBandIntervalZeroLengthSourceEdge: p == q degenerates to a point test, so
// the answer is the whole of [0,1] or nothing.
func TestBandIntervalZeroLengthSourceEdge(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 200, 0)

	in := offsetPt(baseLat, baseLon, 100, 5)
	t0, t1, ok, err := BandInterval(in, in, a, b, 8)
	if err != nil || !ok || t0 != 0 || t1 != 1 {
		t.Fatalf("point inside the band: got [%v,%v] ok=%v err=%v, want the whole edge", t0, t1, ok, err)
	}

	out := offsetPt(baseLat, baseLon, 100, 25)
	if _, _, ok, err := BandInterval(out, out, a, b, 8); err != nil || ok {
		t.Fatalf("point outside the band: got ok=%v err=%v, want empty", ok, err)
	}

	// Beyond the far end, where the projection clamps to B.
	past := offsetPt(baseLat, baseLon, 204, 3)
	if _, _, ok, err := BandInterval(past, past, a, b, 8); err != nil || !ok {
		t.Fatalf("point 5 m past B: got ok=%v err=%v, want inside", ok, err)
	}
}

// TestBandIntervalParallelEdges: the span regime's inequality collapses to a
// constant. Exact, not an error.
func TestBandIntervalParallelEdges(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 300, 0)

	// Parallel and inside: the whole source edge qualifies.
	p := offsetPt(baseLat, baseLon, 50, 5)
	q := offsetPt(baseLat, baseLon, 250, 5)
	t0, t1, ok, err := BandInterval(p, q, a, b, 8)
	if err != nil || !ok || t0 != 0 || t1 != 1 {
		t.Fatalf("parallel inside: got [%v,%v] ok=%v err=%v, want the whole edge", t0, t1, ok, err)
	}

	// Parallel and outside.
	p2 := offsetPt(baseLat, baseLon, 50, 30)
	q2 := offsetPt(baseLat, baseLon, 250, 30)
	if _, _, ok, err := BandInterval(p2, q2, a, b, 8); err != nil || ok {
		t.Fatalf("parallel outside: got ok=%v err=%v, want empty", ok, err)
	}

	// Anti-parallel (reversed direction) and inside — direction is not the band
	// solve's business.
	t0, t1, ok, err = BandInterval(q, p, a, b, 8)
	if err != nil || !ok || t0 != 0 || t1 != 1 {
		t.Fatalf("anti-parallel inside: got [%v,%v] ok=%v err=%v", t0, t1, ok, err)
	}

	// Parallel at a distance that overshoots both canon ends: the span regime
	// contributes nothing and the two end regimes carry the answer.
	p3 := offsetPt(baseLat, baseLon, -60, 5)
	q3 := offsetPt(baseLat, baseLon, 360, 5)
	t0, t1, ok, err = BandInterval(p3, q3, a, b, 8)
	if err != nil || !ok {
		t.Fatalf("parallel overhanging both ends: ok=%v err=%v", ok, err)
	}
	// Inside the band from 6.245 m before A to 6.245 m past B (√(8²−5²)).
	half := math.Sqrt(64 - 25)
	wantLo := (60 - half) / 420
	wantHi := (60 + 300 + half) / 420
	if math.Abs(t0-wantLo) > 1e-9 || math.Abs(t1-wantHi) > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want ≈[%.17g,%.17g]", t0, t1, wantLo, wantHi)
	}
}

// TestBandIntervalTangency: the band comparison is ≤, so a position at exactly
// d is INSIDE and a tangency is a double root — a single-point interval, not
// an empty one.
//
// "Exactly d" is only a well-posed statement where the arithmetic is exact,
// which for lat/lon inputs it is not: converting 8 metres to degrees and back
// lands within an ulp of 8, not on it, so a geometrically-tangent fixture sits
// on a knife edge by construction and its emptiness is a coin flip. The
// closed-ness claim is therefore pinned on the solvers, where the equality is
// real, and the geometric fixtures pin the behaviour either side of it.
func TestBandIntervalTangency(t *testing.T) {
	// Double root: (t−1)² ≤ 0 is the single point t=1, not empty.
	lo, hi, ok, err := SolveQuadLE(1, -2, 1, 0, 2)
	if err != nil || !ok || lo != 1 || hi != 1 {
		t.Fatalf("double root: got [%v,%v] ok=%v err=%v, want the single point 1", lo, hi, ok, err)
	}
	// Equality in the span regime's linear form is inside, not outside.
	if lo, hi, ok = SolveAbsLE(5, 0, 5, 0, 1); !ok || lo != 0 || hi != 1 {
		t.Fatalf("|c0| == m: got [%v,%v] ok=%v, want the whole range — the band comparison is ≤", lo, hi, ok)
	}
	if _, _, ok = SolveAbsLE(5, 0, math.Nextafter(5, 0), 0, 1); ok {
		t.Fatal("|c0| one ulp above m must be outside")
	}

	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 200, 0)

	// A source edge running due north from the 8 m offset, i.e. grazing the band
	// at its southern end and departing immediately. The band is taken from
	// production's own measurement of that endpoint, so "exactly d" is exact
	// here and the ± cases straddle a real boundary rather than a rounding one.
	p := offsetPt(baseLat, baseLon, 100, 8)
	q := offsetPt(baseLat, baseLon, 100, 108)
	graze, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, p.Lat, p.Lon)

	t0, t1, ok, err := BandInterval(p, q, a, b, graze*(1+1e-12))
	if err != nil || !ok {
		t.Fatalf("grazing just inside the band: ok=%v err=%v, want a sliver", ok, err)
	}
	if t0 != 0 || t1 > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want a sliver at the southern end", t0, t1)
	}
	if _, _, ok, err := BandInterval(p, q, a, b, graze*(1-1e-12)); err != nil || ok {
		t.Fatalf("grazing just outside the band: ok=%v err=%v, want empty", ok, err)
	}
	// At exactly the distance ClosestPointOnSeg measures, the answer is a point
	// or nothing — never an error, never a real interval. Which of the two is
	// decided by an ulp: the solve's span form (|c0 + c1·t| ≤ d·|AB|) and
	// ClosestPointOnSeg's hypot form are algebraically the same number but not
	// bitwise, so a grazing position can land either side. That is a property of
	// any solver that is not literally ClosestPointOnSeg, and the ≤ semantics
	// are pinned above where the equality is exact.
	t0, t1, ok, err = BandInterval(p, q, a, b, graze)
	if err != nil {
		t.Fatalf("tangency must solve, not error: %v", err)
	}
	if ok && t1-t0 > 1e-9 {
		t.Fatalf("tangency produced a real interval [%.17g,%.17g], not a point", t0, t1)
	}
	t.Logf("graze at exactly the measured %.17g m: ok=%v interval=[%.17g,%.17g]", graze, ok, t0, t1)

	// Tangency to the disc around A, approached from outside the canon edge: a
	// source running east at y=8 enters the band exactly at the s=0 regime
	// boundary and stays in it. The root is maximally ill-conditioned — the
	// distance is flat there, so a band a nanometre wider moves the entry by a
	// third of a millimetre — which is the point of testing it.
	p2 := offsetPt(baseLat, baseLon, -100, 8)
	q2 := offsetPt(baseLat, baseLon, 100, 8)
	t0, t1, ok, err = BandInterval(p2, q2, a, b, 8*(1+1e-9))
	if err != nil {
		t.Fatalf("regime-boundary tangency must solve: %v", err)
	}
	if !ok {
		t.Fatal("the second half of the edge lies at the band offset and must be inside")
	}
	if math.Abs(t0-0.5) > 1e-5 || t1 != 1 {
		t.Fatalf("got [%.17g,%.17g], want ≈[0.5,1]", t0, t1)
	}
	t.Logf("tangential entry at a regime boundary: t0 = %.17g (0.5 ± %.1e)", t0, math.Abs(t0-0.5))
}

// TestBandIntervalWhollyInsideOrOutside covers the obvious cases explicitly,
// plus the three-regime span the convexity assertion is really about.
func TestBandIntervalWhollyInsideOrOutside(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 100, 0)

	// Wholly inside.
	p := offsetPt(baseLat, baseLon, 20, 3)
	q := offsetPt(baseLat, baseLon, 80, -3)
	t0, t1, ok, err := BandInterval(p, q, a, b, 8)
	if err != nil || !ok || t0 != 0 || t1 != 1 {
		t.Fatalf("wholly inside: got [%v,%v] ok=%v err=%v", t0, t1, ok, err)
	}

	// Wholly outside.
	p2 := offsetPt(baseLat, baseLon, 20, 300)
	q2 := offsetPt(baseLat, baseLon, 80, 400)
	if _, _, ok, err := BandInterval(p2, q2, a, b, 8); err != nil || ok {
		t.Fatalf("wholly outside: got ok=%v err=%v", ok, err)
	}

	// All three regimes contribute: a source edge running parallel 5 m off,
	// starting well before A and ending well past B. Every join must be exact.
	p3 := offsetPt(baseLat, baseLon, -200, 5)
	q3 := offsetPt(baseLat, baseLon, 300, 5)
	pieces, n, joins, nj, err := BandPieces(p3, q3, a, b, 8)
	if err != nil {
		t.Fatalf("bandPieces: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected all three regimes to contribute, got %d pieces: %v", n, pieces[:n])
	}
	// Three contributing regimes means two joins, and a source edge running 5 m
	// off a canon edge inside an 8 m band puts both boundary points squarely in
	// the band — so both verdicts are decided, and decided IN.
	if nj != 2 {
		t.Fatalf("expected two regime joins, got %d: %v", nj, joins[:nj])
	}
	for _, j := range joins[:nj] {
		if !j.InBand {
			t.Fatalf("join at t=%.17g, 5 m off the canon edge in an 8 m band, was called OUT of band", j.T)
		}
	}
	for i := 1; i < n; i++ {
		if pieces[i].Lo != pieces[i-1].Hi {
			t.Fatalf("regime pieces %d and %d do not meet exactly: %.17g vs %.17g (margin %.3e)",
				i-1, i, pieces[i-1].Hi, pieces[i].Lo, pieces[i].Lo-pieces[i-1].Hi)
		}
	}
	t0, t1, ok, err = BandInterval(p3, q3, a, b, 8)
	if err != nil || !ok {
		t.Fatalf("three-regime span: ok=%v err=%v", ok, err)
	}
	half := math.Sqrt(64 - 25)
	wantLo := (200 - half) / 500
	wantHi := (200 + 100 + half) / 500
	if math.Abs(t0-wantLo) > 1e-9 || math.Abs(t1-wantHi) > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want ≈[%.17g,%.17g]", t0, t1, wantLo, wantHi)
	}
}

// TestBandIntervalPerpendicularSource covers s1 == 0 exactly: a source edge
// perpendicular to the canon edge never changes regime.
func TestBandIntervalPerpendicularSource(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 200, 0)

	// Due north through the middle of the canon edge: pure span regime.
	p := offsetPt(baseLat, baseLon, 100, -50)
	q := offsetPt(baseLat, baseLon, 100, 50)
	t0, t1, ok, err := BandInterval(p, q, a, b, 8)
	if err != nil || !ok {
		t.Fatalf("perpendicular crossing: ok=%v err=%v", ok, err)
	}
	if math.Abs(t0-0.42) > 1e-9 || math.Abs(t1-0.58) > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want ≈[0.42,0.58]", t0, t1)
	}

	// Due north beyond B: pure near-B regime, and the band is the disc around B.
	p2 := offsetPt(baseLat, baseLon, 204, -50)
	q2 := offsetPt(baseLat, baseLon, 204, 50)
	t0, t1, ok, err = BandInterval(p2, q2, a, b, 8)
	if err != nil || !ok {
		t.Fatalf("perpendicular past B: ok=%v err=%v", ok, err)
	}
	half := math.Sqrt(64 - 16)
	if math.Abs(t0-(50-half)/100) > 1e-9 || math.Abs(t1-(50+half)/100) > 1e-9 {
		t.Fatalf("got [%.17g,%.17g], want ≈[%.17g,%.17g]", t0, t1, (50-half)/100, (50+half)/100)
	}
}

// ---- 4. the assertions themselves --------------------------------------

// TestBandIntervalConvexityCheckFires: the union step is an assertion, not a
// merge. Fed inconsistent regime output it must error and name the magnitude,
// never quietly bridge the gap.
func TestBandIntervalConvexityCheckFires(t *testing.T) {
	// A gap — the band is not one interval.
	if _, _, _, err := UnionOneInterval([]BandPiece{{Lo: 0, Hi: 0.25}, {Lo: 0.3, Hi: 0.6}}); err == nil {
		t.Fatal("a gap between regime pieces must be an error, not a silent merge")
	} else {
		t.Logf("gap error: %v", err)
	}

	// The smallest possible gap still fires: there is no epsilon here by design.
	hi := 0.25
	if _, _, _, err := UnionOneInterval([]BandPiece{{Lo: 0, Hi: hi}, {Lo: math.Nextafter(hi, 1), Hi: 0.6}}); err == nil {
		t.Fatal("a one-ulp gap must still fire — no join epsilon is licensed")
	}

	// An overlap — regimes share only their endpoints.
	if _, _, _, err := UnionOneInterval([]BandPiece{{Lo: 0, Hi: 0.5}, {Lo: 0.4, Hi: 0.6}}); err == nil {
		t.Fatal("overlapping regime pieces must be an error")
	} else {
		t.Logf("overlap error: %v", err)
	}

	// Contiguous pieces union cleanly.
	lo, hiOut, ok, err := UnionOneInterval([]BandPiece{{Lo: 0, Hi: 0.25}, {Lo: 0.25, Hi: 0.6}, {Lo: 0.6, Hi: 1}})
	if err != nil || !ok || lo != 0 || hiOut != 1 {
		t.Fatalf("contiguous pieces: got [%v,%v] ok=%v err=%v", lo, hiOut, ok, err)
	}

	// No pieces is empty, not an error.
	if _, _, ok, err := UnionOneInterval(nil); err != nil || ok {
		t.Fatalf("no pieces: ok=%v err=%v, want empty and no error", ok, err)
	}
}

// TestBandIntervalRejectsUnsolvableInput: bad input is an error that
// propagates, never a default.
func TestBandIntervalRejectsUnsolvableInput(t *testing.T) {
	const baseLat, baseLon = 53.3811, -1.8730
	a := offsetPt(baseLat, baseLon, 0, 0)
	b := offsetPt(baseLat, baseLon, 200, 0)
	p := offsetPt(baseLat, baseLon, 50, 5)
	q := offsetPt(baseLat, baseLon, 150, 5)

	if _, _, _, err := BandInterval(p, q, a, b, -1); err == nil {
		t.Fatal("a negative band must be an error")
	}
	nan := Point{Lat: math.NaN(), Lon: baseLon}
	if _, _, _, err := BandInterval(nan, q, a, b, 8); err == nil {
		t.Fatal("a NaN coordinate must be an error")
	}
	inf := Point{Lat: baseLat, Lon: math.Inf(1)}
	if _, _, _, err := BandInterval(p, q, inf, b, 8); err == nil {
		t.Fatal("an infinite coordinate must be an error")
	}
	if _, _, _, err := BandInterval(p, q, a, b, math.NaN()); err == nil {
		t.Fatal("a NaN band must be an error")
	}

	// d == 0 is legal: the band is the zero-distance set.
	onLine0 := offsetPt(baseLat, baseLon, 50, 0)
	onLine1 := offsetPt(baseLat, baseLon, 150, 0)
	if _, _, ok, err := BandInterval(onLine0, onLine1, a, b, 0); err != nil {
		t.Fatalf("d=0 must solve: %v", err)
	} else if !ok {
		t.Fatal("a source lying on the canon edge is within a zero band")
	}
}
