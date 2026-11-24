package merge

// WarpLineEnds moves a line's ends onto the given points with the
// merge's own taper: the same WarpLegEndpoints the merge runs on every leg
// it joins to a node within EndpointWarpRadiusM. A nil start or end leaves
// that end, and its taper zone, untouched bitwise. The input slice is never
// mutated; the result has the same length, and each point keeps its Ele and
// Time pointers.
//
// It finds no target and joins nothing: the caller chooses where each end
// goes. It exists for callers outside the merge that want to join line ends
// with the same move the merge makes.
func WarpLineEnds(pts []Point, start, end *LatLon) ([]Point, error) {
	out, _, err := WarpLegEndpoints(pts, WarpTargetAt(start), WarpTargetAt(end))
	return out, err
}

// WarpTargetAt is p as a warp target with no node identity: WarpLineEnds
// moves geometry only.
func WarpTargetAt(p *LatLon) *WarpTarget {
	if p == nil {
		return nil
	}
	return &WarpTarget{Lat: p.Lat, Lon: p.Lon}
}
