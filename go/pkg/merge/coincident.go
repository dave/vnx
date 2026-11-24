package merge

// Coincident points are dropped only when importing into a network. Merging
// into a network throws time data away, so points stacked on the same
// coordinate carry no information there.
//
// ⚠ NETWORK PATH ONLY. The ingest converters are shared with a plain import
// that keeps timestamps, where a repeated coordinate is a PAUSE: real data
// that a de-dupe would silently delete. So the caller applies this at its
// network call sites rather than inside the converters.
//
// ⚠ THE POINTS REACHING DropCoincidentPoints MAY CARRY TIME. The caller
// strips it later, when it stores the arc. So "network geometry carries no
// time" describes what is STORED, not what this function is handed — see the
// elevation note below, which is the same distinction.

// DropCoincidentPoints removes each point that repeats its predecessor's
// coordinate exactly, keeping the first of every run.
//
// EXACT equality, no tolerance, matching the zero-length source-edge test in
// candidateCorridorIntervals: the claim is "these are the same coordinate",
// which is a fact about the bytes a GPS wrote, not a measurement. A
// short-but-real step carries real geometry and is none of this function's
// business.
//
// Elevation is taken from the FIRST point of a run and the rest discarded,
// and so is the time — dropping a run to its first member keeps whichever
// fields that member had. Nothing is lost that survives to storage: this
// function is meant only for network geometry, which is stored with no time
// at all. And a DEM fill at import resamples a missing elevation from
// the coordinate anyway, which is identical for every member of a run.
//
// A leg left with fewer than two points has no geometry at all and commits
// nothing — the callers' existing len(pts) < 2 skip, which already treats a
// degenerate leg as an expected absence rather than an error.
//
// The input is never mutated: callers hold parsed data shared with the
// other paths and with the batch's other passes.
func DropCoincidentPoints(pts []Point) []Point {
	if len(pts) < 2 {
		return pts
	}
	out := make([]Point, 0, len(pts))
	out = append(out, pts[0])
	for _, p := range pts[1:] {
		if prev := out[len(out)-1]; p.Lat == prev.Lat && p.Lon == prev.Lon {
			continue
		}
		out = append(out, p)
	}
	return out
}
