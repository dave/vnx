package merge

import (
	"math"
	"testing"
)

// destPoint returns the point `distM` metres from (lat, lon) along
// `bearingDeg`, using the spherical direct formula with the same Earth
// radius as [HaversineMetres] — so a destPoint at exactly radiusM reads
// back as (near enough) exactly radiusM under the refine step. Longitude
// is normalised to [-180, 180], and a destination past the pole comes
// back with the reflected latitude and the flipped longitude, exactly as
// stored coordinates would be.
func destPoint(lat, lon, bearingDeg, distM float64) (float64, float64) {
	const R = 6371000.0
	angDist := distM / R
	bearing := bearingDeg * math.Pi / 180
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180

	sinLat2 := math.Sin(lat1)*math.Cos(angDist) +
		math.Cos(lat1)*math.Sin(angDist)*math.Cos(bearing)
	lat2 := math.Asin(sinLat2)
	y := math.Sin(bearing) * math.Sin(angDist) * math.Cos(lat1)
	x := math.Cos(angDist) - math.Sin(lat1)*sinLat2
	lon2 := lon1 + math.Atan2(y, x)

	outLon := lon2 * 180 / math.Pi
	outLon = math.Mod(outLon+540, 360) - 180
	return lat2 * 180 / math.Pi, outLon
}

// TestNodeSearchBox_NeverExcludesAcceptedPoint is the property that the
// whole two-stage design rests on: the bbox pre-filter is a TRUE
// SUPERSET of the metric disc, so it can never exclude a point the
// haversine refine would have accepted. A false negative here is silent
// — the query returns "no node nearby" and the caller cannot tell that
// from a genuine miss.
//
// A lat/lon box holds a disc if and only if it holds the disc's extreme
// points, so the test probes exactly those, ON the ring (radiusM, where
// the margin is thinnest): due north and due south (the latitude
// extremes; due north at the equator with r = 0.5 m is the old 1-ulp
// defect that roundingWiden fixes), and the two bearings where the ring
// reaches its largest longitude offset. The cos(centre latitude) defect
// shows only at those longitude peaks at extreme latitude.
//
// The cases hit each branch of nodeSearchBox and metresToDegBox:
//   - lat 0, 1e-7, 45, 89: a local box (polewardLat < 90).
//   - lat 89.99999, 90, -89.9999, -90 with r = 5000: polewardLat >= 90,
//     whole-world longitude.
//   - lon ±179.99999 with r = 5000: the box crosses the ±180 seam, so
//     it widens to the whole world; with r = 0.5 it does not cross.
func TestNodeSearchBox_NeverExcludesAcceptedPoint(t *testing.T) {
	lats := []float64{0, 1e-7, 45, 89, 89.99999, 90, -89.9999, -90}
	lons := []float64{0, 179.99999, -179.99999}
	radii := []float64{0.5, 5000}

	inBox := func(minLat, maxLat, minLon, maxLon, lat, lon float64) bool {
		return lat >= minLat && lat <= maxLat && lon >= minLon && lon <= maxLon
	}

	// lonPeakBearing returns the bearing (degrees) at which a ring of
	// angular radius delta around latitude lat reaches its largest
	// eastward longitude: cos(theta) = tan(delta) * tan(lat). When the
	// ring holds a pole there is no peak (every longitude is reached),
	// and 90 is as good a probe as any.
	lonPeakBearing := func(lat, radiusM float64) float64 {
		c := math.Tan(radiusM/6371000.0) * math.Tan(lat*math.Pi/180)
		if math.Abs(c) >= 1 || math.IsNaN(c) || math.IsInf(c, 0) {
			return 90
		}
		return math.Acos(c) * 180 / math.Pi
	}

	checked := 0
	cases := 0
	for _, lat := range lats {
		for _, lon := range lons {
			for _, radiusM := range radii {
				minLat, maxLat, minLon, maxLon := nodeSearchBox(lat, lon, radiusM)
				peak := lonPeakBearing(lat, radiusM)
				for _, bearing := range []float64{0, 180, peak, 360 - peak} {
					cases++
					// The farthest point on this bearing that the refine
					// accepts. The ring itself (dist = radiusM) is often a
					// rounding error past radiusM, so bisect between the
					// centre (always accepted) and the ring; lo stays
					// accepted, and 80 halvings leave it as close to the ring
					// as destPoint can place a point (on the equator, the
					// ring itself; within 1 m of a pole destPoint's asin
					// near 1 moves in steps of a few millimetres).
					accepted := func(dist float64) bool {
						cLat, cLon := destPoint(lat, lon, bearing, dist)
						return HaversineMetres(lat, lon, cLat, cLon) <= radiusM
					}
					lo, hi := 0.0, radiusM
					if accepted(hi) {
						lo = hi
					}
					for i := 0; i < 80 && lo < hi; i++ {
						if mid := (lo + hi) / 2; accepted(mid) {
							lo = mid
						} else {
							hi = mid
						}
					}
					if lo < radiusM*0.99 {
						continue // counted as unchecked below
					}
					cLat, cLon := destPoint(lat, lon, bearing, lo)
					checked++
					if !inBox(minLat, maxLat, minLon, maxLon, cLat, cLon) {
						t.Fatalf(
							"box excluded an accepted point: probe (%v, %v) r=%v, "+
								"bearing %v, candidate (%v, %v) at %v m, box lat [%v, %v] lon [%v, %v]",
							lat, lon, radiusM, bearing, cLat, cLon,
							HaversineMetres(lat, lon, cLat, cLon),
							minLat, maxLat, minLon, maxLon,
						)
					}
				}
			}
		}
	}
	// Every probe must reach an accepted point within 1% of the ring; one
	// that did not was not tested near the thin margin at all.
	if checked != cases {
		t.Fatalf("only %d of %d ring points reached an accepted point at the ring", checked, cases)
	}
}

// TestNodeSearchBox_TightAwayFromTheSeams guards the other direction:
// the superset must not be achieved by simply selecting the whole world
// every time. Away from the poles and the ±180 seam the box stays a
// small local window — otherwise every call degrades into a full scan of
// the network's nodes and the property test above would be vacuous.
func TestNodeSearchBox_TightAwayFromTheSeams(t *testing.T) {
	minLat, maxLat, minLon, maxLon := nodeSearchBox(54.4, -3.0, 10)
	if maxLat-minLat > 0.001 {
		t.Errorf("latitude window too wide: [%v, %v]", minLat, maxLat)
	}
	if maxLon-minLon > 0.001 {
		t.Errorf("longitude window too wide: [%v, %v]", minLon, maxLon)
	}
	// The cos(lat) widening is real: at 54.4° a degree of longitude is
	// ~0.58 of a degree of latitude, so the longitude window must be
	// WIDER than the latitude one, not equal to it.
	if maxLon-minLon <= maxLat-minLat {
		t.Errorf(
			"longitude window not widened by cos(lat): lat span %v, lon span %v",
			maxLat-minLat, maxLon-minLon,
		)
	}
}

func walkOf(pts ...[2]float64) []Point {
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[i] = Point{Lat: p[0], Lon: p[1]}
	}
	return out
}

// The box is a TRUE SUPERSET of the metric neighbourhood of the walk: if
// the refine would accept a point, the box must contain it. A box that is
// too narrow fails INVISIBLY — the caller reads "no node near this leg" and
// cannot tell it from a genuine miss.
//
// The worst case is known, so the probes go there rather than to random
// spots that mostly land well inside the box: a full radius out from the
// middle of a long leg, where no vertex's own box reaches (the narrowing
// walkSearchBox's comment warns about). Each walk is an L: a 2 km leg along
// a parallel, then a 2 km leg poleward along a meridian, so the second leg's
// middle sits poleward of its first vertex and needs the wider longitude
// half-width of the poleward-most latitude. Probes go out at 8 bearings from
// each leg's middle, at the latitudes where longitude stretches least and
// most.
func TestWalkSearchBox_NeverExcludesAcceptedPoint(t *testing.T) {
	const radiusM = 10.0
	// A hair inside the radius, so float rounding in the refine cannot
	// reject the probe; any narrowing worth catching is millimetres.
	const probeM = radiusM * (1 - 1e-9)
	const legM = 2000.0
	const lon0 = 10.0

	for _, lat0 := range []float64{0, 54.4, 71.0, -71.0} {
		poleward := 0.0 // bearing of the second leg
		if lat0 < 0 {
			poleward = 180
		}
		_, lon1 := destPoint(lat0, lon0, 90, legM)
		lat2, _ := destPoint(lat0, lon1, poleward, legM)
		// Exactly along a parallel, then exactly along a meridian.
		walk := []Point{{Lat: lat0, Lon: lon0}, {Lat: lat0, Lon: lon1}, {Lat: lat2, Lon: lon1}}

		minLat, maxLat, minLon, maxLon := walkSearchBox(walk, radiusM)

		for leg := 1; leg < len(walk); leg++ {
			// The middle as ProjectProbeOntoWalk interpolates it.
			a, b := walk[leg-1], walk[leg]
			midLat, midLon := (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2
			for bearing := 0.0; bearing < 360; bearing += 45 {
				pLat, pLon := destPoint(midLat, midLon, bearing, probeM)
				proj, err := ProjectProbeOntoWalk(walk, pLat, pLon)
				if err != nil {
					t.Fatalf("lat %v leg %d bearing %v: ProjectProbeOntoWalk: %v", lat0, leg, bearing, err)
				}
				if proj.DistM > radiusM {
					t.Fatalf("lat %v leg %d bearing %v: fixture is wrong: the refine rejects the probe at %v m",
						lat0, leg, bearing, proj.DistM)
				}
				if pLat < minLat || pLat > maxLat {
					t.Errorf("lat %v leg %d bearing %v: accepted point lat %v outside box [%v, %v]",
						lat0, leg, bearing, pLat, minLat, maxLat)
				}
				if pLon < minLon || pLon > maxLon {
					t.Errorf("lat %v leg %d bearing %v: accepted point lon %v outside box [%v, %v]",
						lat0, leg, bearing, pLon, minLon, maxLon)
				}
			}
		}
	}
}

// A walk that steps across the ±180 seam widens longitude to the whole
// world rather than producing a window that excludes one side of it. The
// refine still decides.
func TestWalkSearchBox_SeamCrossingWalkWidensToWholeWorld(t *testing.T) {
	walk := walkOf([2]float64{-16.5, 179.99}, [2]float64{-16.5, -179.99})
	_, _, minLon, maxLon := walkSearchBox(walk, 15)
	if minLon != -180 || maxLon != 180 {
		t.Fatalf("seam-crossing walk: lon box [%v, %v], want the whole world", minLon, maxLon)
	}
}

// nearestVertexIndex answers a DIFFERENT question from the projection, and
// ties go to the lower index — which is what makes PointIdx independent of
// the order coincident vertices happen to appear in.
func TestNearestVertexIndex_TiesGoLow(t *testing.T) {
	// Vertices 0 and 2 are the same point; the probe sits on it.
	walk := walkOf(
		[2]float64{54.4, -3.0},
		[2]float64{54.41, -3.0},
		[2]float64{54.4, -3.0},
	)
	if got := nearestVertexIndex(walk, 54.4, -3.0); got != 0 {
		t.Fatalf("nearestVertexIndex = %d, want 0", got)
	}
	if got := nearestVertexIndex(walk, 54.41, -3.0); got != 1 {
		t.Fatalf("nearestVertexIndex = %d, want 1", got)
	}
}
