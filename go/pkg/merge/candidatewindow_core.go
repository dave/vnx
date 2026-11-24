package merge

import (
	"math"
)

// ArcBBoxNearAnyEdge reports whether any source EDGE of pts could lie within
// stayM metres of the axis-aligned bounding box of verts.
//
// It tests edges, not points, because the corridor solver prices distance as
// an integral along each source EDGE, so an arc can be beyond stayM of every
// source vertex and still be within stayM of a long edge's interior. On
// hand-drawn plans almost every source edge is long enough to hide an arc that
// way at a 10 m radius.
//
// SOUNDNESS comes from RectsBeyond: a box CONTAINS its geometry, so the
// distance between two boxes lower-bounds the distance between what is inside
// them, and a pair the test calls "beyond" cannot hold a qualifying pair. The
// corridor's own edge pruning rests on the same argument, which is why this
// reuses that function rather than growing a second copy of the arithmetic.
//
// NO NEW CONSTANT: stayM is the caller's own radius.
func ArcBBoxNearAnyEdge(verts, pts []Point, stayM float64) bool {
	if len(pts) < 2 {
		// No source edges, so no edge can be near anything. NOT a fallback to
		// a vertex test: a leg of fewer than two points has no corridor to find
		// at all — the corridor intervals are built per source EDGE and contribute
		// nothing for a leg that has none — so the honest answer is "nothing to
		// bind", and the caller's loop already refuses such an item before it
		// reaches here.
		return false
	}
	aMinLat, aMaxLat := verts[0].Lat, verts[0].Lat
	aMinLon, aMaxLon := verts[0].Lon, verts[0].Lon
	for _, v := range verts[1:] {
		aMinLat = math.Min(aMinLat, v.Lat)
		aMaxLat = math.Max(aMaxLat, v.Lat)
		aMinLon = math.Min(aMinLon, v.Lon)
		aMaxLon = math.Max(aMaxLon, v.Lon)
	}
	for i := 0; i+1 < len(pts); i++ {
		bMinLat := math.Min(pts[i].Lat, pts[i+1].Lat)
		bMaxLat := math.Max(pts[i].Lat, pts[i+1].Lat)
		bMinLon := math.Min(pts[i].Lon, pts[i+1].Lon)
		bMaxLon := math.Max(pts[i].Lon, pts[i+1].Lon)
		if !RectsBeyond(aMinLat, aMaxLat, aMinLon, aMaxLon, bMinLat, bMaxLat, bMinLon, bMaxLon, stayM) {
			return true
		}
	}
	return false
}

// WindowStayRect is the AABB of a window of leg points expanded by stayM metres
// in every direction. The longitude pad is scaled by the window's highest-|lat|
// cosine so the box is at least stayM metres wide on the ground everywhere in the
// window (never under-covers). MetresPerDeg is the shared great-circle
// constant. The window always has at least one point.
func WindowStayRect(pts []Point, stayM float64) Bounds {
	minLat, maxLat := pts[0].Lat, pts[0].Lat
	minLon, maxLon := pts[0].Lon, pts[0].Lon
	for _, p := range pts[1:] {
		if p.Lat < minLat {
			minLat = p.Lat
		}
		if p.Lat > maxLat {
			maxLat = p.Lat
		}
		if p.Lon < minLon {
			minLon = p.Lon
		}
		if p.Lon > maxLon {
			maxLon = p.Lon
		}
	}
	latForCos := math.Max(math.Abs(minLat), math.Abs(maxLat))
	dLat := stayM / MetresPerDeg
	dLon := stayM / (MetresPerDeg * math.Cos(latForCos*math.Pi/180.0))
	return Bounds{
		MinLat: minLat - dLat,
		MaxLat: maxLat + dLat,
		MinLon: minLon - dLon,
		MaxLon: maxLon + dLon,
	}
}
