package merge

import (
	"math"
	"testing"
)

// ---- metric test frame -------------------------------------------------
//
// Fixtures are authored in a local east/north metre frame around a Lake
// District base point and converted to lat/lon so distances in the frame
// are (very close to) metres. Band-edge cases that must be exact do NOT
// rely on this approximation — they measure the real projected distance
// with the same primitive the matcher uses (ClosestPointOnSeg) and pin
// the band to that value, so the `≤` boundary is tested without any
// floating-point slack.

const (
	testBaseLat = 54.0
	testBaseLon = -3.0
	// Metres per degree latitude / longitude at the base. Only used to
	// place fixtures; the matcher never sees these numbers.
	mPerDegLat = 111320.0
)

func mPerDegLon() float64 { return mPerDegLat * math.Cos(testBaseLat*math.Pi/180) }

// mPt places a point at (xM east, yM north) metres from the base. An
// optional elevation may be supplied; omitted means nil ("unknown").
func mPt(xM, yM float64, ele ...float64) Point {
	p := Point{
		Lat: testBaseLat + yM/mPerDegLat,
		Lon: testBaseLon + xM/mPerDegLon(),
	}
	if len(ele) == 1 {
		e := ele[0]
		p.Ele = &e
	}
	return p
}

func mCanon(id int64, verts ...Point) CanonArc {
	return CanonArc{ID: ArcID(id), Vertices: verts}
}

// straightLegX builds a west→east leg of n points spanning [0,lenM] at
// north offset yM, evenly spaced.
func straightLegX(n int, lenM, yM float64) []Point {
	pts := make([]Point, n)
	for i := range pts {
		x := lenM * float64(i) / float64(n-1)
		pts[i] = mPt(x, yM)
	}
	return pts
}

// TestFollowHintPolyMatchesBruteForce pins the perf primitive: hintPoly's
// prefix/suffix-box-pruned nearestDist must return the exact same distance as a
// full ptToPolylineDistM scan, for ANY seed hint (including out-of-range ones)
// and across self-approaching shapes where a naive monotone walk would miss the
// true nearest. This is the byte-identity guarantee the assembly
// post-conditions rely on.
//
// The only way to get it wrong is for a box prune to skip the true edge. The
// doubling-back polyline puts the true nearest edge three edges from the hint
// and directly west of the probe, so the box distance that must not prune it
// is a pure longitude span — which is what cosMin has to keep a lower bound.
// At 78°N an overstated longitude span (cos 78° ≈ 0.21) passes the 100 m hint
// edge; at 53°N it does not, so that copy is the ordinary case. The reverse
// copy makes the walk run backwards, through the prefix boxes.
//
// South of the equator the largest-MAGNITUDE latitude is the most negative
// one. The 56.8°S copy puts the west edge 99.995 m away against the 100 m hint
// edge, and its polyline spans 400 m of latitude. A cosMin taken at the
// largest signed latitude (the northern end, |lat| smaller) overstates the
// longitude span by ~7e-5 — 7 mm here — and prunes the true edge.
//
// Red proofs, 2026-10-05 (detached worktree): with cosMin set to 1, this
// fails on "78N doubling back hint=-3: nearestDist=99.999999999461 !=
// ptToPolylineDistM=49.9962433386727"; with the backward walk testing the
// prefix box that ends at edge j's FIRST vertex, it fails on "53N doubling
// back, reversed hint=2"; with the n == 0 return removed, it panics (index out
// of range) on "no vertices", which the old random sweep never drew. With
// both math.Abs calls removed from newHintPoly's cosMin, it fails on the 56.8°S
// case (the old sweep, at −56.8°: "nearestDist=36.1198793847503 !=
// ptToPolylineDistM=36.1198007023733").
func TestFollowHintPolyMatchesBruteForce(t *testing.T) {
	// The doubling-back shape, in metres east and north of the probe: an edge
	// 100 m north (the hint), out and back, then an edge westM due west.
	poly := func(baseLat, westM float64, reverse bool) []Point {
		shape := [][2]float64{{0, 100}, {200, 100}, {200, 300}, {-westM, 300}, {-westM, -100}}
		out := make([]Point, len(shape))
		for i, v := range shape {
			j := i
			if reverse {
				j = len(shape) - 1 - i
			}
			out[j] = offsetPt(baseLat, -3, v[0], v[1])
		}
		return out
	}
	cases := []struct {
		name     string
		baseLat  float64
		poly     []Point
		probeE   float64 // probe offset east of (baseLat, -3), metres
		wantEdge int     // the edge holding the true nearest; -1 when there is none
	}{
		{"no vertices", 53.4, nil, 0, -1},
		{"one vertex", 53.4, []Point{offsetPt(53.4, -3, 30, 40)}, 0, -1},
		{"one edge", 53.4, []Point{offsetPt(53.4, -3, -20, 10), offsetPt(53.4, -3, 20, 10)}, 0, 0},
		{"53N doubling back", 53.4, poly(53.4, 50, false), 0, 3},
		{"53N doubling back, reversed", 53.4, poly(53.4, 50, true), 0, 0},
		{"78N doubling back", 78.2, poly(78.2, 50, false), 0, 3},
		{"78N doubling back, reversed", 78.2, poly(78.2, 50, true), 0, 0},
		{"56.8S doubling back, west edge 5 mm inside the hint", -56.8, poly(-56.8, 99.995, false), 0, 3},
		{"56.8S doubling back, west edge 5 mm inside the hint, reversed", -56.8, poly(-56.8, 99.995, true), 0, 0},
	}
	for _, c := range cases {
		hp := newHintPoly(c.poly)
		probe := offsetPt(c.baseLat, -3, c.probeE, 0)
		want := ptToPolylineDistM(probe.Lat, probe.Lon, c.poly)
		nedges := len(c.poly) - 1
		for _, h := range []int{-3, 0, nedges / 2, nedges - 1, nedges + 5} {
			got, edge := hp.NearestDist(probe.Lat, probe.Lon, h)
			if got != want {
				t.Fatalf("%s hint=%d: nearestDist=%.15g != ptToPolylineDistM=%.15g (edge %d)", c.name, h, got, want, edge)
			}
			if c.wantEdge >= 0 && edge != c.wantEdge {
				t.Fatalf("%s hint=%d: nearest edge %d, want %d", c.name, h, edge, c.wantEdge)
			}
		}
	}
}

// TestPreppedCandidateProjectRangeCeil_SouthernSuffixBox pins the candidate's
// own cosMin (PrepCandidates), which projectRangeCeil's suffix-box early-out
// and chunkBeyond scale longitude spans by. It must be the cosine of the
// largest-MAGNITUDE latitude, and south of the equator that is the most
// negative one. The case is the hint-poly test's 56.8°S doubling-back shape:
// the west edge sits 99.995 m from the point against 100 m for the first
// edge, and the polyline spans 400 m of latitude, so a cosMin taken at the
// largest SIGNED latitude overstates the longitude span by ~7e-5 (7 mm) and
// the scan stops before the west edge.
//
// Red proof, 2026-10-05 (detached worktree): with both math.Abs calls removed
// from PrepCandidates' cosMin, this fails — "projectRangeCeil found
// 100.000000000251 (ok=true), a full scan finds 99.9973984867598". Before this
// test, the whole package passed under that change.
func TestPreppedCandidateProjectRangeCeil_SouthernSuffixBox(t *testing.T) {
	const baseLat, baseLon = -56.8, -3.0
	shape := [][2]float64{{0, 100}, {200, 100}, {200, 300}, {-99.995, 300}, {-99.995, -100}}
	verts := make([]Point, len(shape))
	for i, v := range shape {
		verts[i] = offsetPt(baseLat, baseLon, v[0], v[1])
	}
	pc := PrepCandidates([]CanonArc{{ID: 1, Vertices: verts}})[0]
	if pc == nil {
		t.Fatal("prepCandidates dropped the candidate")
	}
	probe := offsetPt(baseLat, baseLon, 0, 0)
	_, got, _, _, ok := pc.ProjectRangeCeil(probe.Lat, probe.Lon, 0, pc.Total, math.Inf(1))
	want := ptToPolylineDistM(probe.Lat, probe.Lon, verts)
	if !ok || got != want {
		t.Fatalf("projectRangeCeil found %.15g (ok=%v), a full scan finds %.15g", got, ok, want)
	}
	if !(want < 100) {
		t.Fatalf("the west edge is %.15g m away, not inside the 100 m first edge — the case no longer tests the early-out", want)
	}
}

// ptToPolylineDistM is the straightforward full-scan minimum planar distance in
// metres from (lat, lon) to the polyline `poly` (joined arcs; a single
// vertex degenerates to point-to-point). Production code no longer calls it —
// hintPoly.nearestDist replaced it — so it lives here as the brute-force
// reference TestFollowHintPolyMatchesBruteForce checks the pruned primitive
// against.
func ptToPolylineDistM(lat, lon float64, poly []Point) float64 {
	if len(poly) == 0 {
		return math.Inf(1)
	}
	if len(poly) == 1 {
		return HaversineMetres(lat, lon, poly[0].Lat, poly[0].Lon)
	}
	minD := math.Inf(1)
	for i := 0; i < len(poly)-1; i++ {
		d, _ := ClosestPointOnSeg(poly[i].Lat, poly[i].Lon, poly[i+1].Lat, poly[i+1].Lon, lat, lon)
		if d < minD {
			minD = d
		}
	}
	return minD
}
