package merge

// Tests for corridor_boundary.go.

import (
	"math"
	"testing"
)

// ---- sourceMeasure.eleAt / pointAt: the elevation contract -------------------

func TestSourceMeasureEleAt_LerpsBetweenBracketingVertices(t *testing.T) {
	pts := []Point{mPt(0, 0, 100), mPt(100, 0, 200)}
	sa := mustSourceMeasure(t, pts)
	ele, err := sa.EleAt(sourcePos{Edge: 0, Frac: 0.25})
	if err != nil {
		t.Fatalf("eleAt: %v", err)
	}
	if ele == nil {
		t.Fatal("eleAt: got nil, want a lerped value — both endpoints have elevation")
	}
	if want := 125.0; math.Abs(*ele-want) > 1e-9 {
		t.Fatalf("eleAt(frac 0.25) = %v, want %v", *ele, want)
	}
}

// TestSourceMeasureEleAt_NilPropagatesNeverFabricatesZero is the house rule
// ("no fallbacks... do not default a missing elevation to 0") pinned as a
// test: an edge with either endpoint's elevation unknown must answer
// "unknown" everywhere on it, never a fabricated sea-level 0.
func TestSourceMeasureEleAt_NilPropagatesNeverFabricatesZero(t *testing.T) {
	cases := []struct {
		name string
		a, b Point
	}{
		{"start unknown", mPt(0, 0), mPt(100, 0, 200)},
		{"end unknown", mPt(0, 0, 100), mPt(100, 0)},
		{"both unknown", mPt(0, 0), mPt(100, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sa := mustSourceMeasure(t, []Point{c.a, c.b})
			for _, frac := range []float64{0, 0.5, 1} {
				ele, err := sa.EleAt(sourcePos{Edge: 0, Frac: frac})
				if err != nil {
					t.Fatalf("eleAt(frac %v): %v", frac, err)
				}
				if ele != nil {
					t.Fatalf("eleAt(frac %v) = %v, want nil (a missing endpoint must not be fabricated as 0)", frac, *ele)
				}
			}
		})
	}
}

func TestSourceMeasurePointAt_CombinesLatLonAndEle(t *testing.T) {
	pts := []Point{mPt(0, 0, 100), mPt(100, 0, 300)}
	sa := mustSourceMeasure(t, pts)
	p := sourcePos{Edge: 0, Frac: 0.5}
	lat, lon, err := sa.LatLonAt(p)
	if err != nil {
		t.Fatalf("latLonAt: %v", err)
	}
	pt, err := sa.PointAt(p)
	if err != nil {
		t.Fatalf("pointAt: %v", err)
	}
	if pt.Lat != lat || pt.Lon != lon {
		t.Fatalf("pointAt position (%v,%v) disagrees with latLonAt (%v,%v)", pt.Lat, pt.Lon, lat, lon)
	}
	if pt.Ele == nil || math.Abs(*pt.Ele-200) > 1e-9 {
		t.Fatalf("pointAt elevation = %v, want 200", pt.Ele)
	}
}

// ---- corridorBoundaryPoint: the kind-A crossing vertex ----

// crossingFixture is a single source edge that starts 40 m off canon and
// closes to exactly 0 m — an off→on crossing, chosen so the R = 15 m crossing
// sits strictly inside the edge (t = 0.625, well clear of both ends).
func crossingFixture(t *testing.T) (sa *sourceMeasure, pc *PreppedCandidate, off, on Point) {
	t.Helper()
	off = mPt(0, 40, 500) // 40 m north of canon, elevation 500
	on = mPt(100, 0, 700) // on canon, elevation 700
	sa = mustSourceMeasure(t, []Point{off, on})
	pc = prepOne(t, mPt(0, 0), mPt(200, 0))
	return sa, pc, off, on
}

// TestCorridorBoundaryPoint_MatchesBisectedRoot pins the analytic boundary
// against an independent numerical root of the same definition. The corridor
// interval's ImportMeasureLo is solved in closed form; here the crossing is
// found instead by 40 bisection steps along the source edge on
// ClosestPointOnSeg's distance to the (single-edge) canon, building the point
// by lat/lon lerp and the elevation by lerpEle at the same fraction. The two
// methods share nothing but the fixture and the distance definition, so
// agreement is evidence that the closed form is right, not that it agrees
// with itself.
func TestCorridorBoundaryPoint_MatchesBisectedRoot(t *testing.T) {
	const R = 15.0
	sa, pc, off, on := crossingFixture(t)

	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil {
		t.Fatalf("candidateCorridorIntervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("got %d corridor intervals, want 1: %+v", len(ivs), ivs)
	}
	mine, err := corridorBoundaryPoint(sa, ivs[0].ImportMeasureLo)
	if err != nil {
		t.Fatalf("corridorBoundaryPoint: %v", err)
	}

	if len(pc.Verts) != 2 {
		t.Fatalf("fixture canon has %d vertices; this bisection measures distance to ONE edge", len(pc.Verts))
	}
	a, b := pc.Verts[0], pc.Verts[1]
	at := func(f float64) (lat, lon float64) {
		return off.Lat + f*(on.Lat-off.Lat), off.Lon + f*(on.Lon-off.Lon)
	}
	dist := func(f float64) float64 {
		lat, lon := at(f)
		d, _ := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, lat, lon)
		return d
	}
	if !(dist(0) > R) || !(dist(1) <= R) {
		t.Fatalf("fixture does not bracket the crossing: dist(off)=%.4g dist(on)=%.4g, R=%v", dist(0), dist(1), R)
	}
	flo, fhi := 0.0, 1.0 // off is above R, on is at/below
	for range 40 {
		mid := (flo + fhi) / 2
		if dist(mid) > R {
			flo = mid
		} else {
			fhi = mid
		}
	}
	f := (flo + fhi) / 2
	lat, lon := at(f)
	root := Point{Lat: lat, Lon: lon, Ele: lerpEle(off.Ele, on.Ele, f)}

	// Position: within the noise floor this package already sets its
	// tolerances at (corridorinterval_test.go's corridorTol, above the
	// equirectangular 0.026 m gap, not 1e-6).
	gap := HaversineMetres(mine.Lat, mine.Lon, root.Lat, root.Lon)
	if gap > corridorTol {
		t.Fatalf("analytic cp sits %.4g m from the bisected root, want <= %v m", gap, corridorTol)
	}
	// Elevation: both interpolate the SAME two source vertices at (to
	// noise-floor precision) the same fraction, so they must agree — the
	// assertion PreservesElevation's betweenness check cannot make.
	if mine.Ele == nil || root.Ele == nil {
		t.Fatalf("elevation dropped: mine=%v root=%v, want both set", mine.Ele, root.Ele)
	}
	if math.Abs(*mine.Ele-*root.Ele) > 1e-6 {
		t.Fatalf("elevation disagrees: mine=%.9g root=%.9g", *mine.Ele, *root.Ele)
	}
}

// TestCorridorBoundaryPoint_PreservesElevation is the direct statement of
// the acceptance criterion "elevation answered explicitly" — cp carries the
// source's own recorded elevation at the crossing, not a zero and not a
// dropped field.
func TestCorridorBoundaryPoint_PreservesElevation(t *testing.T) {
	const R = 15.0
	sa, pc, off, on := crossingFixture(t)
	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil || len(ivs) != 1 {
		t.Fatalf("candidateCorridorIntervals: ivs=%v err=%v", ivs, err)
	}
	cp, err := corridorBoundaryPoint(sa, ivs[0].ImportMeasureLo)
	if err != nil {
		t.Fatalf("corridorBoundaryPoint: %v", err)
	}
	if cp.Ele == nil {
		t.Fatal("committed boundary vertex has no elevation — this is the data-loss bug the boundary point exists to prevent")
	}
	// The crossing sits strictly between off and on (t≈0.625), so its
	// elevation must sit strictly between theirs — never clamped to either
	// end and never 0.
	if !(*off.Ele < *cp.Ele && *cp.Ele < *on.Ele) {
		t.Fatalf("cp elevation %.6g not strictly between off %.6g and on %.6g", *cp.Ele, *off.Ele, *on.Ele)
	}
}

// TestCorridorBoundaryPoint_DroppingItMovesTheSeam is the third
// criterion: "if cp is dropped, measure the seam-geometry change — do not
// assume it is nil." It measures how far the analytic crossing sits off the
// straight chord a caller would commit instead if cp were simply omitted —
// jumping directly from the OFF run's last verbatim point to the ON run's
// first on-canon point.
func TestCorridorBoundaryPoint_DroppingItMovesTheSeam(t *testing.T) {
	const R = 15.0
	sa, pc, off, _ := crossingFixture(t)
	ivs, err := candidateCorridorIntervals(t.Context(), sa, pc, 0, R)
	if err != nil || len(ivs) != 1 {
		t.Fatalf("candidateCorridorIntervals: ivs=%v err=%v", ivs, err)
	}
	iv := ivs[0]
	cp, err := corridorBoundaryPoint(sa, iv.ImportMeasureLo)
	if err != nil {
		t.Fatalf("corridorBoundaryPoint: %v", err)
	}

	// The first ON-canon point a run beginning at this boundary would
	// commit — assembleOnRun's own anchor, interpOnCanon at the interval's
	// own canon-measure answer for this boundary.
	anchorLat, anchorLon := pc.interpOnCanon(iv.Pieces[0].CanonMeasureAtLo)

	// The chord that replaces cp→anchor if cp is dropped: off (the OFF run's
	// last verbatim point) straight to anchor.
	gapM, _ := ClosestPointOnSeg(off.Lat, off.Lon, anchorLat, anchorLon, cp.Lat, cp.Lon)

	t.Logf("dropping cp would move the seam by %.3g m (R=%g m)", gapM, R)
	// Measured, not assumed: geometry (40 m departure closing to 0 over
	// 100 m, R=15) puts this at ≈12.6 m — comparable to R itself, the same
	// shape as the design's own worked failure ("the leg bridges it with its
	// own geometry — the 8.372 m parting"). A bound well below that,
	// generous to floating-point/projection noise, still proves the change
	// is not negligible and cp cannot simply be dropped.
	const minExpectedM = 5.0
	if gapM < minExpectedM {
		t.Fatalf("seam-geometry change from dropping cp measured at only %.4g m, want > %v m — fixture no longer exercises a real departure", gapM, minExpectedM)
	}
}

// corridorBoundaryPoint mints the committed vertex at an exact import-measure
// position on the source polyline — the crossing vertex `cp` at a kind-A
// (clean OFF↔ON) boundary.
//
// importMeasure is expected to be a corridor interval's own boundary
// (corridorInterval.ImportMeasureLo/ImportMeasureHi, or a corridorRun's), not
// an arbitrary position: those are the values the interval builder certifies as sitting exactly on the R-metre band, which is what makes
// the position half of this replacement exact rather than merely close.
//
// ⚠ TEST-ONLY (moved from corridor_boundary.go on 2026-09-26: no production code that
// only tests call). Production does not call it.
// follow_corridor_assemble.go builds the same point directly from
// sa.pointAt(run.Start/End), one measure round-trip cheaper.
func corridorBoundaryPoint(sa *sourceMeasure, importMeasure float64) (Point, error) {
	pos, err := sa.PosAtMeasure(importMeasure)
	if err != nil {
		return Point{}, err
	}
	return sa.PointAt(pos)
}
