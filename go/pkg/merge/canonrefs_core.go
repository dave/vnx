package merge

// The canon's records as the merge reads them. Each holds only the fields the
// merge's code reads; the caller fills them from its own storage.

// Node is a canon node: its ID, position and elevation. Ele is nil when the
// node has no elevation.
type Node struct {
	ID       NodeID
	Lat, Lon float64
	Ele      *float64
}

// Pip is one interior vertex of a canon arc. Ele is the raw stored value: an
// absent elevation is a NaN, kept as it is.
type Pip struct {
	Lat, Lon, Ele float64
}

// Arc is one arc as stored: the nodes it joins, its interior geometry in walk
// order (the terminal nodes are not included), and its length in metres.
type Arc struct {
	ID          ArcID
	StartNodeID NodeID
	EndNodeID   NodeID
	Polyline    []Pip
	DistanceM   float64
}

// NearbyCanonArc is a canon arc found near a probe point, with the probe's
// projection onto it.
//
// Exported for the reference application's tests; not part of the stable API.
type NearbyCanonArc struct {
	ID        ArcID
	DistM     float64 // haversine, probe -> projection
	Fraction  float64 // of the arc's cumulative walk distance
	Lat, Lon  float64 // the projection point
	PriorLenM float64 // walk length before the projection
	NextLenM  float64 // walk length after it
}

// NodeNearPath is a canon node found near a leg's path, with its projection
// onto the path.
//
// Exported for the reference application's tests; not part of the stable API.
type NodeNearPath struct {
	ID       NodeID
	Lat, Lon float64

	SegIdx   int
	PointIdx int

	DistM     float64
	ProjLat   float64
	ProjLon   float64
	PriorLenM float64
	NextLenM  float64
	Fraction  float64
}

// Bounds is a latitude/longitude box.
type Bounds struct {
	MinLat float64
	MinLon float64
	MaxLat float64
	MaxLon float64
}
