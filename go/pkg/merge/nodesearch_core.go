package merge

import (
	"math"
	"slices"
)

// NearbyNode is a node found by a nearby-node search: its id, the
// coordinates as stored, and its measured distance from the probe.
//
// The coordinates are returned rather than left for the caller to look up
// because the search already read them, and because the endpoint warp needs
// the POSITION, not the identity — it copies the coordinates onto an incoming
// endpoint. A second lookup to fetch lat/lon would be both wasteful and an
// invitation to reuse the node, which is the align step's job and not the
// warp's.
//
// Exported for the reference application's tests; not part of the stable API.
type NearbyNode struct {
	ID    NodeID
	Lat   float64
	Lon   float64
	DistM float64
}

// nearestNearbyNode reduces a candidate list to its nearest node, or
// (zero, false) for an empty list.
func nearestNearbyNode(all []NearbyNode) (NearbyNode, bool) {
	// NEAREST WINS, TIES TO THE ROW THE QUERY YIELDED FIRST — the strict `<`
	// is what makes that true, and the caller's search preserves row
	// order for exactly this reason. This reduction IS the whole difference
	// between the singular and plural forms; nothing about the box, the
	// exclude rule or the `<= radiusM` boundary is decided here.
	var best NearbyNode
	bestD := math.MaxFloat64
	for _, n := range all {
		if n.DistM < bestD {
			bestD, best = n.DistM, n
		}
	}
	if best.ID == 0 {
		return NearbyNode{}, false
	}
	return best, true
}

// measureNodeCandidate measures one candidate row of a node box query
// against the probe (lat, lon): its haversine distance, whether `exclude`
// names it, and whether it is inside the search (not excluded, and
// `d <= radiusM`). n carries the row's id and coordinates and the distance
// whether or not it is inside.
func measureNodeCandidate(lat, lon, radiusM float64, id NodeID, nLat, nLon float64, exclude []NodeID) (n NearbyNode, excluded, inside bool) {
	d := HaversineMetres(lat, lon, nLat, nLon)
	isExcluded := slices.Contains(exclude, id)
	// Coordinates are carried through EXACTLY as stored — never
	// recomputed or rounded. The endpoint warp copies them onto an
	// incoming endpoint and the whole point is bitwise coincidence, so a
	// value that merely round-trips through a formula would
	// silently defeat the identity test the align step depends on.
	n = NearbyNode{ID: id, Lat: nLat, Lon: nLon, DistM: d}
	// EXCLUDED BEFORE "nearest" IS DECIDED, not after: an excluded row
	// competes for nothing, so a further-but-eligible candidate is what
	// wins, exactly as if the excluded row were never in the canon at
	// all. A short linear scan, not a set — every caller today excludes
	// at most two ids (a piece's own start/end), and a map for that
	// would cost more to build than it saves to query.
	if isExcluded {
		return n, true, false
	}
	return n, false, d <= radiusM
}

// NodeSearchBox returns the lat/lon window used to pre-filter candidate
// nodes for a metric disc of radius `radiusM` centred on (lat, lon). The
// window is a TRUE SUPERSET of that disc — it may admit points the
// haversine refine then rejects, but it must never exclude one the
// refine would have accepted, because that exclusion is invisible: the
// query would report "no node nearby" and the caller could not tell that
// from a genuine miss.
//
// Longitude widens to the whole world in two cases where no finite
// half-extent is a superset:
//
//   - the disc reaches a pole (|lat| + latDelta >= 90), where points a
//     couple of metres apart differ by up to 180° of longitude;
//   - the window would cross the ±180 seam, where a plain BETWEEN around
//     the probe excludes the far side entirely.
//
// This is a WIDENING, not a fallback: the refine still makes every
// accept/reject decision, and the only cost is a larger candidate set in
// two geographies trail data rarely visits.
//
// An earlier version instead clamped cos(lat) to a 1e-6 floor. That
// clamp does not do what its comment claimed — near a pole it yields a
// longitude half-extent of ~90° for a 10 m radius, which is NARROWER
// than the 180° such a disc genuinely spans, so it could exclude a node
// the refine would have accepted. Handling the two seams explicitly
// removes both the wrong box and the need for any epsilon.
//
// Exported for the reference application's tests; not part of the stable API.
func NodeSearchBox(lat, lon, radiusM float64) (minLat, maxLat, minLon, maxLon float64) {
	latDelta, lonDelta := metresToDegBox(lat, radiusM)
	minLat, maxLat = lat-latDelta, lat+latDelta
	if lonDelta >= 180 || lon-lonDelta < -180 || lon+lonDelta > 180 {
		return minLat, maxLat, -180, 180
	}
	return minLat, maxLat, lon - lonDelta, lon + lonDelta
}

// metresToDegBox returns the (latitude, longitude) half-extents in
// degrees of a box that contains every point within `radiusM` metres of
// latitude `lat`, on the same sphere [HaversineMetres] measures on
// (R = 6 371 000 m).
//
// Latitude is exact: a degree of latitude is a fixed arc, so radiusM
// metres is radiusM/metresPerDeg degrees.
//
// Longitude scales by 1/cos(latitude), and the latitude that matters is
// the one CLOSEST TO THE POLE that the disc reaches — |lat| + latDelta —
// not the centre. Using cos(centre) leaves the box marginally too narrow
// on the poleward side, which at extreme latitude is enough to drop a
// point sitting on the ring; using the poleward edge is a strict
// superset at every latitude. The result saturates at 180 (the whole
// world) rather than diverging as the poleward edge reaches 90°, which
// is what makes the polar singularity a wide box instead of a division
// by zero.
//
// `roundingWiden` is NOT a geometric tolerance and must never be tuned
// as one — the refine still decides every accept and reject, to the
// metre the caller asked for. It exists because the box half-extent and
// the haversine distance are two independently-rounded float64
// computations of the same quantity: without it, a node due north at
// EXACTLY radiusM lands one ULP outside a mathematically exact box while
// the refine accepts it (observed at the equator with a 0.5 m radius). One part
// in 1e12 is ~1000x the worst accumulated float64 error in either chain
// and, at a 10 m radius, widens the box by 10 picometres.
func metresToDegBox(lat, radiusM float64) (latDelta, lonDelta float64) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	const roundingWiden = 1 + 1e-12

	latDelta = radiusM / metresPerDeg * roundingWiden

	polewardLat := math.Abs(lat) + latDelta
	if polewardLat >= 90 {
		return latDelta, 180
	}
	cosLat := math.Cos(polewardLat * math.Pi / 180.0)
	lonDelta = radiusM / (metresPerDeg * cosLat) * roundingWiden
	if lonDelta > 180 {
		lonDelta = 180
	}
	return latDelta, lonDelta
}

// measureNodeNearPath measures one candidate node (id, nLat, nLon) against the walk:
// excluded or beyond radiusM gives (zero, false, nil). The error is the
// projection's own, unwrapped.
func measureNodeNearPath(verts []Point, radiusM float64, exclude []NodeID, id NodeID, nLat, nLon float64) (NodeNearPath, bool, error) {
	if slices.Contains(exclude, id) {
		return NodeNearPath{}, false, nil
	}
	proj, err := ProjectProbeOntoWalk(verts, nLat, nLon)
	if err != nil {
		return NodeNearPath{}, false, err
	}
	if proj.DistM > radiusM {
		return NodeNearPath{}, false, nil
	}
	return NodeNearPath{
		ID:        id,
		Lat:       nLat,
		Lon:       nLon,
		SegIdx:    proj.SegIdx,
		PointIdx:  NearestVertexIndex(verts, nLat, nLon),
		DistM:     proj.DistM,
		ProjLat:   proj.Lat,
		ProjLon:   proj.Lon,
		PriorLenM: proj.PriorLenM,
		NextLenM:  proj.NextLenM,
		Fraction:  proj.Fraction,
	}, true, nil
}

// sortNodesNearPath puts nodes near a path in walk order: nearest vertex,
// then distance along the walk, then node id.
func sortNodesNearPath(out []NodeNearPath) {
	slices.SortFunc(out, func(a, b NodeNearPath) int {
		if a.PointIdx != b.PointIdx {
			return a.PointIdx - b.PointIdx
		}
		if c := cmpFloat(a.PriorLenM, b.PriorLenM); c != 0 {
			return c
		}
		return int(a.ID) - int(b.ID)
	})
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// NearestVertexIndex returns the index of the walk vertex closest to
// (lat, lon), measured with [HaversineMetres] — the same distance function
// the projection's own DistM is measured with. Ties go to the LOWER index,
// which is what makes the answer independent of vertex order among
// coincident vertices.
//
// This is a distance to VERTICES, a different question from the distance to
// the walk, and the two have different answers; see [NodeNearPath].
//
// Exported for the reference application's tests; not part of the stable API.
func NearestVertexIndex(verts []Point, lat, lon float64) int {
	best, bestD := 0, math.MaxFloat64
	for i, v := range verts {
		d := HaversineMetres(lat, lon, v.Lat, v.Lon)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// WalkSearchBox returns the lat/lon window used to pre-filter candidate
// nodes for the metric neighbourhood of `verts` at radius `radiusM`: the
// walk's own bounding box, grown by the box half-extents [metresToDegBox]
// gives for that radius.
//
// It is a TRUE SUPERSET of that neighbourhood — it may admit nodes the
// refine then rejects, but it must never exclude one the refine would have
// accepted, because that exclusion is INVISIBLE: the caller would read "no
// node near this leg" and could not tell it from a genuine miss.
//
// Growing the WALK'S box is not the same as taking the union of a
// [NodeSearchBox] per vertex, and the difference is the whole point: a node
// 15 m from the middle of a 5 km leg is inside the walk's grown box and
// inside NO vertex's box. That union would have been a silent narrowing.
//
// Longitude widens to the whole world in three cases where no finite window
// is a superset — the two [NodeSearchBox] handles (the neighbourhood reaches
// a pole, or the window would cross the ±180 seam), plus one that only a
// walk has: a longitude span wider than 180°, which is what a walk stepping
// across the seam looks like to a plain min/max. Distinguishing a
// seam-crossing walk from a genuinely hemisphere-wide one would need a
// judgement this primitive has no business making, so both widen. This is a
// WIDENING, not a fallback: the refine still makes every decision, and the
// only cost is a larger candidate set in geographies trail data rarely
// visits.
//
// Exported for the reference application's tests; not part of the stable API.
func WalkSearchBox(verts []Point, radiusM float64) (minLat, maxLat, minLon, maxLon float64) {
	minLat, maxLat = verts[0].Lat, verts[0].Lat
	minLon, maxLon = verts[0].Lon, verts[0].Lon
	for _, v := range verts[1:] {
		minLat = math.Min(minLat, v.Lat)
		maxLat = math.Max(maxLat, v.Lat)
		minLon = math.Min(minLon, v.Lon)
		maxLon = math.Max(maxLon, v.Lon)
	}

	// The latitude that widens longitude most is the poleward-most one the
	// walk reaches; metresToDegBox grows it by its own latDelta again.
	polewardLat := math.Max(math.Abs(minLat), math.Abs(maxLat))
	latDelta, lonDelta := metresToDegBox(polewardLat, radiusM)

	minLat, maxLat = minLat-latDelta, maxLat+latDelta
	if maxLon-minLon > 180 || lonDelta >= 180 ||
		minLon-lonDelta < -180 || maxLon+lonDelta > 180 {
		return minLat, maxLat, -180, 180
	}
	return minLat, maxLat, minLon - lonDelta, maxLon + lonDelta
}

// NodeAtVertex is one node whose stored (lat, lon) is BITWISE EQUAL to the
// walk vertex at VertexIdx.
//
// A node can appear more than once in the result — a walk that comes back to
// the same coordinate hits it again — and two different nodes can never share
// one VertexIdx unless the canon holds two nodes at one coordinate, which is
// its own defect and not this search's to hide. Both are returned as found.
type NodeAtVertex struct {
	ID        NodeID
	Lat, Lon  float64
	VertexIdx int
}

// walkVertexBox is the walk's own bounding box, with no margin: a node at a
// vertex is inside it by construction.
func walkVertexBox(verts []Point) (minLat, maxLat, minLon, maxLon float64) {
	minLat, maxLat = verts[0].Lat, verts[0].Lat
	minLon, maxLon = verts[0].Lon, verts[0].Lon
	for _, v := range verts {
		if v.Lat < minLat {
			minLat = v.Lat
		}
		if v.Lat > maxLat {
			maxLat = v.Lat
		}
		if v.Lon < minLon {
			minLon = v.Lon
		}
		if v.Lon > maxLon {
			maxLon = v.Lon
		}
	}
	return minLat, maxLat, minLon, maxLon
}

// walkVertexIndex maps each exact vertex coordinate of the walk to the
// indices it occurs at, in walk order.
func walkVertexIndex(verts []Point) map[[2]float64][]int {
	at := make(map[[2]float64][]int, len(verts))
	for i, v := range verts {
		k := [2]float64{v.Lat, v.Lon}
		at[k] = append(at[k], i)
	}
	return at
}

// appendNodeAtVertices appends one NodeAtVertex for each walk vertex the node
// (id, nLat, nLon) sits on exactly, unless exclude names it.
func appendNodeAtVertices(out []NodeAtVertex, at map[[2]float64][]int, exclude []NodeID, id NodeID, nLat, nLon float64) []NodeAtVertex {
	if slices.Contains(exclude, id) {
		return out
	}
	for _, idx := range at[[2]float64{nLat, nLon}] {
		out = append(out, NodeAtVertex{ID: id, Lat: nLat, Lon: nLon, VertexIdx: idx})
	}
	return out
}

// sortNodesAtVertices orders nodes at walk vertices by vertex index, then
// node id.
func sortNodesAtVertices(out []NodeAtVertex) {
	slices.SortFunc(out, func(a, b NodeAtVertex) int {
		if a.VertexIdx != b.VertexIdx {
			return a.VertexIdx - b.VertexIdx
		}
		return int(a.ID) - int(b.ID)
	})
}
