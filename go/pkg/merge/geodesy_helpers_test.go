package merge

// geodesy_helpers_test.go — an independent distance function for the graders.
//
// ⚠ THE INDEPENDENCE IS THE POINT, so do not "simplify" this to a call to
// HaversineMetres. It is spelled out here so the invariants
// measure the committed geometry independently of whatever the import itself
// used to decide anything. Reaching for production's own function would have
// a grader grade production with production's own arithmetic —
// a guard sharing its subject's premise.

import (
	"math"
)

// enuVector is a local ENU (east/north) approximation, accurate enough over
// the metre-to-kilometre spans one leg covers, used to get the SIGN of
// direction right.
//
// A raw lat/lon dot product can flip sign because longitude's metric scale
// varies with cos(latitude) — exactly the class of bug an instrument checking
// for direction reversals must not itself have.
//
// Caller: warp_boundary_test.go.
func enuVector(fromLat, fromLon, toLat, toLon float64) (east, north float64) {
	const metresPerDegreeLat = 111320.0
	north = (toLat - fromLat) * metresPerDegreeLat
	east = (toLon - fromLon) * metresPerDegreeLat * math.Cos(fromLat*math.Pi/180)
	return east, north
}
