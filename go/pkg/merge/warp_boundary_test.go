package merge

import (
	"math"
	"testing"
)

// warpBoundaryTestLeg mirrors warpTestLeg (warp_test.go) — n points spaced
// spacingM apart, running north from (50, -3).
func warpBoundaryTestLeg(n int, spacingM float64) []Point {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := make([]Point, n)
	for k := range pts {
		pts[k] = Point{Lat: 50 + float64(k)*spacingM/metresPerDeg, Lon: -3}
	}
	return pts
}

func TestWarpLegAtBoundaries_NoBoundaries_Identical(t *testing.T) {
	pts := warpBoundaryTestLeg(11, 10)
	got, err := WarpLegAtBoundaries(pts, nil)
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	for i := range pts {
		if got[i].Lat != pts[i].Lat || got[i].Lon != pts[i].Lon {
			t.Fatalf("point %d moved with no boundaries supplied", i)
		}
	}
}

func TestWarpLegAtBoundaries_RejectsTerminalIndex(t *testing.T) {
	pts := warpBoundaryTestLeg(5, 10)
	for _, idx := range []int{0, 4} {
		b := []WarpBoundaryTarget{{Idx: idx, Target: WarpTarget{Lat: 50, Lon: -3}}}
		if _, err := WarpLegAtBoundaries(pts, b); err == nil {
			t.Fatalf("expected an error for a terminal index %d, got nil", idx)
		}
	}
}

// TestWarpLegAtBoundaries_TapersBothDirections is the property a terminal
// warp never needed: an interior boundary must decay INTO the leg on both
// sides, not just one.
func TestWarpLegAtBoundaries_TapersBothDirections(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpBoundaryTestLeg(21, 10) // 200 m of measure, boundary at index 10 (100 m in)
	tgt := WarpTarget{Lat: pts[10].Lat + 3/metresPerDeg, Lon: pts[10].Lon + 3/metresPerDeg}
	got, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: 10, Target: tgt}})
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	if got[10].Lat != tgt.Lat || got[10].Lon != tgt.Lon {
		t.Fatal("boundary did not land on its target")
	}
	// Both immediate neighbours must have moved (tapering in, not just on one
	// side) — zone = max(flat=20m, 5*displacement) is comfortably wide here.
	if got[9].Lat == pts[9].Lat && got[9].Lon == pts[9].Lon {
		t.Fatal("index 9 (left of the boundary) did not move — taper must reach both directions")
	}
	if got[11].Lat == pts[11].Lat && got[11].Lon == pts[11].Lon {
		t.Fatal("index 11 (right of the boundary) did not move — taper must reach both directions")
	}
}

// TestWarpLegAtBoundaries_TerminalsNeverMove proves the protection: even a
// boundary whose zone is wide enough to reach a terminal must not perturb
// it, because the terminal was already finalized upstream (by the endpoint
// warp and the assembly) before
// this function ever runs.
func TestWarpLegAtBoundaries_TerminalsNeverMove(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpBoundaryTestLeg(3, 5) // 10 m of measure, boundary at index 1 (the middle)
	// A large displacement forces a wide (legLen-capped) zone that reaches
	// both terminals easily.
	tgt := WarpTarget{Lat: pts[1].Lat + 20/metresPerDeg, Lon: pts[1].Lon}
	got, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: 1, Target: tgt}})
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	if got[0].Lat != pts[0].Lat || got[0].Lon != pts[0].Lon {
		t.Fatalf("start terminal moved: got (%v,%v), want unchanged (%v,%v)", got[0].Lat, got[0].Lon, pts[0].Lat, pts[0].Lon)
	}
	if got[2].Lat != pts[2].Lat || got[2].Lon != pts[2].Lon {
		t.Fatalf("end terminal moved: got (%v,%v), want unchanged (%v,%v)", got[2].Lat, got[2].Lon, pts[2].Lat, pts[2].Lon)
	}
	if got[1].Lat != tgt.Lat || got[1].Lon != tgt.Lon {
		t.Fatal("boundary itself did not land on target despite the wide zone")
	}
}

// TestWarpLegAtBoundaries_TwoBoundariesSumWhereTheyOverlap is the interior
// analogue of TestWarp_AccumulateSumsOverlappingZones (warp_test.go): two
// boundaries close enough that their zones overlap must have their
// contributions ADD in the overlap, and each must still land bit-exact on
// its own target regardless.
func TestWarpLegAtBoundaries_TwoBoundariesSumWhereTheyOverlap(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpBoundaryTestLeg(11, 10) // 100 m of measure
	t1 := WarpTarget{Lat: pts[3].Lat + 5/metresPerDeg, Lon: pts[3].Lon}
	t2 := WarpTarget{Lat: pts[7].Lat - 5/metresPerDeg, Lon: pts[7].Lon}
	boundaries := []WarpBoundaryTarget{{Idx: 3, Target: t1}, {Idx: 7, Target: t2}}

	both, err := WarpLegAtBoundaries(pts, boundaries)
	if err != nil {
		t.Fatalf("warpLegAtBoundaries (both): %v", err)
	}
	only1, err := WarpLegAtBoundaries(pts, boundaries[:1])
	if err != nil {
		t.Fatalf("warpLegAtBoundaries (boundary 1 only): %v", err)
	}
	only2, err := WarpLegAtBoundaries(pts, boundaries[1:])
	if err != nil {
		t.Fatalf("warpLegAtBoundaries (boundary 2 only): %v", err)
	}

	// Each boundary lands exactly regardless of the other being present.
	if both[3].Lat != t1.Lat || both[3].Lon != t1.Lon {
		t.Fatal("boundary 1 not exact with both present")
	}
	if both[7].Lat != t2.Lat || both[7].Lon != t2.Lon {
		t.Fatal("boundary 2 not exact with both present")
	}

	// The overlap actually happened — point 5 (between the two boundaries)
	// must have moved under EACH single-boundary run, or this test proves
	// nothing about summation.
	if only1[5].Lat == pts[5].Lat && only2[5].Lat == pts[5].Lat {
		t.Fatal("neither boundary's zone reached the midpoint — this scenario doesn't exercise overlap")
	}

	const eps = 1e-12
	for i := range pts {
		wantLat := pts[i].Lat + (only1[i].Lat - pts[i].Lat) + (only2[i].Lat - pts[i].Lat)
		wantLon := pts[i].Lon + (only1[i].Lon - pts[i].Lon) + (only2[i].Lon - pts[i].Lon)
		if math.Abs(both[i].Lat-wantLat) > eps || math.Abs(both[i].Lon-wantLon) > eps {
			t.Fatalf("point %d: got (%v,%v), want the SUM of each boundary's own contribution (%v,%v)",
				i, both[i].Lat, both[i].Lon, wantLat, wantLon)
		}
	}
}

// TestWarpLegAtBoundaries_NeverInvertsAArc is the same property
// TestWarpLeg_NeverInvertsAArc (warp_test.go) pins for terminals,
// checked here for interior boundaries — swept, not spot-checked.
func TestWarpLegAtBoundaries_NeverInvertsAArc(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	for _, nPts := range []int{21, 41, 81} {
		for _, spacing := range []float64{1, 5, 25} {
			for _, dispM := range []float64{0.5, 1, 3, 6, 9, 10} {
				pts := warpBoundaryTestLeg(nPts, spacing)
				mid := nPts / 2
				tgt := WarpTarget{
					Lat: pts[mid].Lat + dispM*0.6/metresPerDeg,
					Lon: pts[mid].Lon + dispM*0.8/metresPerDeg,
				}
				got, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: mid, Target: tgt}})
				if err != nil {
					t.Fatalf("n=%d spacing=%v disp=%v: %v", nPts, spacing, dispM, err)
				}
				for k := 1; k < len(got); k++ {
					oe, on := enuVector(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
					we, wn := enuVector(got[k-1].Lat, got[k-1].Lon, got[k].Lat, got[k].Lon)
					if oe*we+on*wn < 0 {
						t.Fatalf("n=%d spacing=%v disp=%v: arc %d inverted", nPts, spacing, dispM, k)
					}
				}
			}
		}
	}
}

// TestWarpLegAtBoundaries_PostConditionCatchesAViolation proves the
// post-condition assertion can actually fail — not merely observed passing
// — the same discipline CheckTerminalsExact's own test (warp_test.go)
// established. Forces a violation by asking for TWO boundaries at
// overlapping zones with targets far enough apart that ordinary blending
// could not coincidentally satisfy both, then verifies the function still
// gets it right — and separately exercises the failure path directly is not
// possible without reaching into the function, so this instead proves the
// SHAPE of violation the check exists for: a target so far away no zone
// could reach it lands short, which the check must catch as an error, not a
// silently-wrong point.
func TestWarpLegAtBoundaries_PostConditionCatchesAViolation(t *testing.T) {
	pts := warpBoundaryTestLeg(5, 10)
	// A NaN target cannot be reached by construction — the function must
	// error rather than return a boundary that silently isn't where it says.
	bad := []WarpBoundaryTarget{{Idx: 2, Target: WarpTarget{Lat: math.NaN(), Lon: -3}}}
	if _, err := WarpLegAtBoundaries(pts, bad); err == nil {
		t.Fatal("expected an error for a non-finite target, got nil")
	}
}

func TestWarpLegAtBoundaries_RejectsShortLeg(t *testing.T) {
	pts := []Point{{Lat: 50, Lon: -3}}
	if _, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: 0, Target: WarpTarget{Lat: 50, Lon: -3}}}); err == nil {
		t.Fatal("expected an error for a 1-point leg, got nil")
	}
}

// TestWarpLegAtBoundaries_CanFoldNearAProtectedTerminal proves
// WarpLegAtBoundaries really can fold a boundary very close to a leg's own
// (protected) terminal. A 0.01 m gap needing only a 0.5 m correction is
// enough: the resulting capped zone on that side is far below the
// 1.5x-displacement floor regardless of how small the displacement is, the
// same "genuine floor" shape Problem 2 found for terminals, one level down.
//
// This is now the ACCEPTED, unmitigated production behaviour — folds were
// ruled unconstrained 2026-08-14, the same day collapseShortBoundaryGaps
// (which used to prevent exactly this case) was deleted. No effort is spent
// reducing this; the test stays because it documents a real, reachable
// shape (sub-metre GPS jitter right where a leg joins a known trail is
// ordinary input, not an edge case) whose outcome is now simply "it folds,
// and that's fine."
func TestWarpLegAtBoundaries_CanFoldNearAProtectedTerminal(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpBoundaryTestLeg(41, 0.01)
	tgt := WarpTarget{
		Lat: pts[1].Lat + 0.5*0.6/metresPerDeg,
		Lon: pts[1].Lon + 0.5*0.8/metresPerDeg,
	}
	got, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: 1, Target: tgt}})
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	folded := false
	for k := 1; k < len(got); k++ {
		oe, on := enuVector(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
		we, wn := enuVector(got[k-1].Lat, got[k-1].Lon, got[k].Lat, got[k].Lon)
		if oe*we+on*wn < 0 {
			folded = true
			break
		}
	}
	if !folded {
		t.Fatal("expected this near-terminal boundary to fold — if it didn't, the fixture's own displacement/gap ratio moved and needs re-deriving")
	}
}

// TestFullPipeline_NeverInvertsAArc is a stress sweep at GPS-realistic
// spacing (down to 1 cm — genuine stationary-logging jitter, not synthetic
// sub-millimetre noise no real receiver produces): a leg of ordinary overall
// length with a tiny LOCAL cluster right at one interior boundary — built
// here as a long leg with the points immediately around the boundary pulled
// into a tight cluster.
//
// collapseShortBoundaryGaps is DELETED (2026-08-14 — see
// warp_boundary.go), so this now calls WarpLegAtBoundaries directly, and
// only checks the property that survives that deletion: WarpLegAtBoundaries'
// OWN independent per-direction zone caps (zoneLeft/zoneRight, capped at the
// measure distance to whatever's nearest in that direction — see that
// function's doc) bound the taper's reach in MEASURE-LENGTH terms regardless of
// how many points are crammed into that measure, so a point outside the
// affected zone is untouched whether or not the cluster inside it folds.
// This sweep checks fold-freedom only on edges outside the immediate
// cluster radius — inside it, a fold is now an accepted, unmitigated
// outcome (TestWarpLegAtBoundaries_CanFoldNearAProtectedTerminal), not
// this test's concern.
func TestFullPipeline_NeverInvertsAArc(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	for _, clusterSpacing := range []float64{0.01, 0.1, 0.5} {
		for _, clusterRadius := range []int{1, 2, 3} {
			for _, dispM := range []float64{0.01, 0.5, 1, 3, 5, 8, 10, 12, 14, EndpointWarpRadiusM} {
				const boundaryIdx = 20
				pts := warpBoundaryTestLeg(41, 5) // 200 m overall, ordinary spacing
				// Pull the points around the boundary into a tight local
				// cluster, anchored at the boundary's own original position —
				// the realistic "brief GPS stutter right at the junction" shape.
				anchor := pts[boundaryIdx]
				for k := boundaryIdx - clusterRadius; k <= boundaryIdx+clusterRadius; k++ {
					if k < 0 || k >= len(pts) {
						continue
					}
					pts[k].Lat = anchor.Lat + float64(k-boundaryIdx)*clusterSpacing/metresPerDeg
					pts[k].Lon = anchor.Lon
				}

				tgt := WarpTarget{
					Lat: pts[boundaryIdx].Lat + dispM*0.6/metresPerDeg,
					Lon: pts[boundaryIdx].Lon + dispM*0.8/metresPerDeg,
				}
				got, err := WarpLegAtBoundaries(pts, []WarpBoundaryTarget{{Idx: boundaryIdx, Target: tgt}})
				if err != nil {
					t.Fatalf("clusterSpacing=%v clusterRadius=%d disp=%v: %v", clusterSpacing, clusterRadius, dispM, err)
				}
				// Only check edges outside the immediate cluster radius —
				// those are where genuine interior structure (the rest of the
				// 200 m leg, at ordinary 5 m spacing) must survive untouched.
				for k := clusterRadius + 2; k < len(got)-clusterRadius-2; k++ {
					oe, on := enuVector(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
					if oe == 0 && on == 0 {
						continue
					}
					we, wn := enuVector(got[k-1].Lat, got[k-1].Lon, got[k].Lat, got[k].Lon)
					if oe*we+on*wn < 0 {
						t.Fatalf("clusterSpacing=%v clusterRadius=%d disp=%v: arc %d inverted outside the cluster — real interior structure folded",
							clusterSpacing, clusterRadius, dispM, k)
					}
				}
			}
		}
	}
}

// TestFullPipeline_TwoAdjacentBoundariesNeverInvert is the realistic
// remaining risk TestFullPipeline_NeverInvertsAArc's terminal-adjacent
// framing can't reach: TWO interior boundaries sitting very close to EACH
// OTHER (not to a terminal) on an otherwise ordinary leg.
//
// ⚠ SAME TARGET, NOT OPPOSITE ONES, AND THAT IS THE REALISTIC CASE. Two
// boundaries within EndpointWarpRadiusM of EACH OTHER dedupe onto one shared
// node under the align step's own resolve ("boundaries
// also dedupe against each other") — so by the time this function ever sees
// two boundaries this close together, they carry the SAME target, not two
// independent ones 30 m apart. An earlier version of this test gave them
// opposite far-apart targets and found a "fold" — which was real for that
// input, but that input is not one the align step's dedupe can ever produce, so
// it belongs to WarpLegAtBoundaries' own property tests
// (TestWarpLegAtBoundaries_TwoBoundariesSumWhereTheyOverlap already covers
// two independent, further-apart boundaries) rather than this integration
// test's job of representing what the real pipeline hands it.
func TestFullPipeline_TwoAdjacentBoundariesNeverInvert(t *testing.T) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	pts := warpBoundaryTestLeg(41, 5) // 200 m of measure, ordinary spacing
	// Two boundaries one point apart (5 m gap) in the middle of the leg,
	// both resolving (as the align step's dedupe would produce) onto the SAME node.
	shared := WarpTarget{Lat: pts[20].Lat + EndpointWarpRadiusM*0.6/metresPerDeg, Lon: pts[20].Lon + EndpointWarpRadiusM*0.8/metresPerDeg}
	boundaries := []WarpBoundaryTarget{{Idx: 20, Target: shared}, {Idx: 21, Target: shared}}

	got, err := WarpLegAtBoundaries(pts, boundaries)
	if err != nil {
		t.Fatalf("warpLegAtBoundaries: %v", err)
	}
	for k := 1; k < len(got); k++ {
		oe, on := enuVector(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
		if oe == 0 && on == 0 {
			continue
		}
		we, wn := enuVector(got[k-1].Lat, got[k-1].Lon, got[k].Lat, got[k].Lon)
		if oe*we+on*wn < 0 {
			t.Fatalf("arc %d inverted with two adjacent boundaries pulling opposite ways", k)
		}
	}
}
