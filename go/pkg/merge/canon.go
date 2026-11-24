package merge

import (
	"context"
	"fmt"
	"math"
)

// Canon is the network a leg merges into, as the merge reads it. Every
// method answers from the canon as it stands at the call, including
// everything written earlier in the same merge, so an implementation must not
// serve a read from a copy taken before a write.
//
// Row order is part of the contract wherever a method states one: the
// searches built on these reads break ties by the order rows arrive in.
//
// AddNode and AddArc store what they are given and compute nothing about the
// geometry: the merge computes node positions, interior pips, stats and
// bounds first (PrepareArc). SplitArc is told where to split (splitArcAt
// computes the split point) but computes the stats and bounds of both halves
// itself; its doc gives the exact rule. Ids are allocated in a fixed order, and the order is part of
// the contract: AddArc mints its start node (when asked), then its end node
// (when asked), then the arc; SplitArc mints the node, then the prior half,
// then the next half, then deletes the parent.
//
// An error from any method during a merge poisons the canon: what has been
// written so far is unknown, and the caller must discard every write the merge
// made. A canon kept in a database transaction does this by rolling the
// transaction back.
type Canon interface {
	// NodesInBox returns every node with MinLat <= lat <= MaxLat and
	// MinLon <= lon <= MaxLon, ordered by (lat, lon, id). Only ID, Lat and
	// Lon are needed: Ele may be left nil, and no search reads it.
	NodesInBox(ctx context.Context, b Bounds) ([]Node, error)

	// Node returns one node, and whether it belongs to this canon (a node
	// id can name a node another owner holds). A missing node is an error.
	Node(ctx context.Context, id NodeID) (n Node, inCanon bool, err error)

	// ArcIDsInRect returns the id of every arc whose bounding box meets b,
	// inclusive at both ends, in no stated order. A canon may store the
	// boxes coarser than float64; SQLite's R*Tree stores each bound as a
	// float32 moved outward when the nearest float32 would fall inside the
	// box (see [newBox32] for the exact rule, which is not always the next
	// float32), and compares that float32 with the float64 query. So the set
	// can hold arcs that a float64 box test would miss, never fewer. Two
	// canons give the same set only when they store the same float32 boxes.
	ArcIDsInRect(ctx context.Context, b Bounds) ([]ArcID, error)

	// ArcsInRect returns the same arcs as ArcIDsInRect, with their full
	// vertex lists, ordered by id.
	ArcsInRect(ctx context.Context, b Bounds) ([]CanonArc, error)

	// ArcsNearPoint returns, ordered by id, every arc with a stretch whose
	// box meets b. It may return more arcs than that (any superset is
	// correct); callers measure every candidate.
	ArcsNearPoint(ctx context.Context, b Bounds) ([]CanonArc, error)

	// Arc returns one arc as stored: its end node ids, its interior pips with
	// their elevation bits unchanged (NaN for an unknown elevation), and its
	// length. A missing arc is an error. A canon need not check that the
	// arc is its own: one that reads rows by id alone may return another
	// owner's arc, while [MemCanon], which holds only its own, errors.
	Arc(ctx context.Context, id ArcID) (Arc, error)

	// CanonArc returns one arc of this canon with its full vertex list, or
	// false when the canon has no such arc.
	CanonArc(ctx context.Context, id ArcID) (CanonArc, bool, error)

	// ArcEnds returns an arc's start and end node ids. A missing arc is an
	// error. As with Arc, a canon need not check that the arc is its own.
	ArcEnds(ctx context.Context, id ArcID) (start, end NodeID, err error)

	// ArcsBetween returns every arc of this canon joining nodes a and b, in
	// either direction, ordered by id.
	ArcsBetween(ctx context.Context, a, b NodeID) ([]Arc, error)

	// AddNode stores a node of this canon at (lat, lon) with elevation ele
	// (nil when unknown) and returns its id.
	AddNode(ctx context.Context, lat, lon float64, ele *float64) (NodeID, error)

	// AddArc stores a new arc, minting the ends a.MintStart and a.MintEnd ask
	// for, and returns the ids it stored.
	AddArc(ctx context.Context, a NewArc) (AddedArc, error)

	// SplitArc replaces arc id with two halves joined at a new node, as s
	// gives them (s was computed by SplitPolylineAtFraction from the arc as
	// Arc and Node read it): the node at (s.Lat, s.Lon, s.Ele), the prior
	// half from the arc's start node through Polyline[:s.PriorEnd] to the
	// node, and the next half from the node through Polyline[s.NextStart:]
	// to the arc's end node (0 <= PriorEnd <= NextStart <= len(Polyline);
	// a zero-length arc gives PriorEnd 0 and NextStart len(Polyline), so
	// both halves have empty interiors). s holds no pips: the canon slices
	// the halves from its own copy of the parent. Anything the canon keeps
	// about the parent beyond its geometry is the canon's to carry over to
	// the halves.
	//
	// The canon computes each half's stats and bounds, and must compute
	// them exactly so, bit for bit. Each half's walk is a []Pip:
	//
	//   - prior: the arc's start node, Polyline[:s.PriorEnd], the new node;
	//   - next: the new node, Polyline[s.NextStart:], the arc's end node.
	//
	// An end node's pip is its position and elevation as stored (after the
	// store's own normalisation, such as -0.0 to +0.0), with NaN for an
	// unknown elevation ([NodeEle]). The new node's pip is (s.Lat, s.Lon)
	// and [elevOrNaN](s.Ele), as given, before the store normalises them.
	// The half's stats are [StatsFromPips](walk) and its bounds
	// [BoundsFromPips](walk); an error from BoundsFromPips fails the split.
	SplitArc(ctx context.Context, id ArcID, s PolylineSplit) (SplitIDs, error)
}

// nearbyNodes returns every node within radiusM (great-circle, `<=`) of
// (lat, lon) that exclude does not name, in the order NodesInBox yields them.
func nearbyNodes(ctx context.Context, c Canon, lat, lon, radiusM float64, exclude []NodeID) ([]nearbyNode, error) {
	if err := checkProbe(lat, lon, radiusM); err != nil {
		return nil, err
	}
	minLat, maxLat, minLon, maxLon := nodeSearchBox(lat, lon, radiusM)
	rows, err := c.NodesInBox(ctx, Bounds{MinLat: minLat, MaxLat: maxLat, MinLon: minLon, MaxLon: maxLon})
	if err != nil {
		return nil, fmt.Errorf("nearby nodes: %w", err)
	}
	var out []nearbyNode
	for _, r := range rows {
		n, _, inside := measureNodeCandidate(lat, lon, radiusM, r.ID, r.Lat, r.Lon, exclude)
		if inside {
			out = append(out, n)
		}
	}
	return out, nil
}

// nearestNode returns the nearest node nearbyNodes finds, ties to the
// earlier row, or false when it finds none.
func nearestNode(ctx context.Context, c Canon, lat, lon, radiusM float64, exclude []NodeID) (nearbyNode, bool, error) {
	all, err := nearbyNodes(ctx, c, lat, lon, radiusM, exclude)
	if err != nil {
		return nearbyNode{}, false, err
	}
	best, ok := nearestNearbyNode(all)
	return best, ok, nil
}

// nodesNearPath returns every node within radiusM of the walk verts that
// exclude does not name, in walk order (see sortNodesNearPath). A walk
// with no length fails with an error that wraps ErrWalkHasNoLength.
func nodesNearPath(ctx context.Context, c Canon, verts []Point, radiusM float64, exclude []NodeID) ([]nodeNearPath, error) {
	if math.IsNaN(radiusM) || math.IsInf(radiusM, 0) || radiusM <= 0 {
		return nil, fmt.Errorf("search radius %v m must be finite and positive", radiusM)
	}
	if err := checkWalk(verts); err != nil {
		return nil, err
	}
	minLat, maxLat, minLon, maxLon := walkSearchBox(verts, radiusM)
	rows, err := c.NodesInBox(ctx, Bounds{MinLat: minLat, MaxLat: maxLat, MinLon: minLon, MaxLon: maxLon})
	if err != nil {
		return nil, fmt.Errorf("nodes near path: %w", err)
	}
	var out []nodeNearPath
	for _, r := range rows {
		n, inside, err := measureNodeNearPath(verts, radiusM, exclude, r.ID, r.Lat, r.Lon)
		if err != nil {
			return nil, fmt.Errorf("project node %d onto walk of %d vertices: %w", r.ID, len(verts), err)
		}
		if inside {
			out = append(out, n)
		}
	}
	sortNodesNearPath(out)
	return out, nil
}

// nodesAtWalkVertices returns every node that sits exactly on a vertex of
// verts and that exclude does not name (see sortNodesAtVertices for
// the order).
func nodesAtWalkVertices(ctx context.Context, c Canon, verts []Point, exclude []NodeID) ([]NodeAtVertex, error) {
	if len(verts) == 0 {
		return nil, fmt.Errorf("walk has no vertices")
	}
	minLat, maxLat, minLon, maxLon := walkVertexBox(verts)
	rows, err := c.NodesInBox(ctx, Bounds{MinLat: minLat, MaxLat: maxLat, MinLon: minLon, MaxLon: maxLon})
	if err != nil {
		return nil, fmt.Errorf("nodes at walk vertices: %w", err)
	}
	at := walkVertexIndex(verts)
	var out []NodeAtVertex
	for _, r := range rows {
		out = appendNodeAtVertices(out, at, exclude, r.ID, r.Lat, r.Lon)
	}
	sortNodesAtVertices(out)
	return out, nil
}

// nearestArc returns the arc whose vertex walk passes nearest (lat, lon),
// within radiusM, ties to the lower id, or false when none does.
func nearestArc(ctx context.Context, c Canon, lat, lon, radiusM float64) (nearbyCanonArc, bool, error) {
	if err := checkProbe(lat, lon, radiusM); err != nil {
		return nearbyCanonArc{}, false, err
	}
	minLat, maxLat, minLon, maxLon := nodeSearchBox(lat, lon, radiusM)
	candidates, err := c.ArcsNearPoint(ctx, Bounds{MinLat: minLat, MaxLat: maxLat, MinLon: minLon, MaxLon: maxLon})
	if err != nil {
		return nearbyCanonArc{}, false, fmt.Errorf("nearby arcs: %w", err)
	}
	return nearestCanonArc(candidates, lat, lon, radiusM)
}

func checkProbe(lat, lon, radiusM float64) error {
	if math.IsNaN(lat) || lat < -90 || lat > 90 {
		return fmt.Errorf("probe latitude %v must be in [-90, 90]", lat)
	}
	if math.IsNaN(lon) || lon < -180 || lon > 180 {
		return fmt.Errorf("probe longitude %v must be in [-180, 180]", lon)
	}
	if math.IsNaN(radiusM) || math.IsInf(radiusM, 0) || radiusM <= 0 {
		return fmt.Errorf("search radius %v m must be finite and positive", radiusM)
	}
	return nil
}

func checkWalk(verts []Point) error {
	if len(verts) < 2 {
		return fmt.Errorf("walk has %d vertices, want at least 2", len(verts))
	}
	for i, v := range verts {
		if math.IsNaN(v.Lat) || math.IsInf(v.Lat, 0) || v.Lat < -90 || v.Lat > 90 {
			return fmt.Errorf("walk vertex %d latitude %v is not a coordinate in [-90, 90]", i, v.Lat)
		}
		if math.IsNaN(v.Lon) || math.IsInf(v.Lon, 0) || v.Lon < -180 || v.Lon > 180 {
			return fmt.Errorf("walk vertex %d longitude %v is not a coordinate in [-180, 180]", i, v.Lon)
		}
	}
	total := 0.0
	for i := 1; i < len(verts); i++ {
		total += HaversineMetres(verts[i-1].Lat, verts[i-1].Lon, verts[i].Lat, verts[i].Lon)
	}
	if total == 0 {
		return fmt.Errorf("walk has no length: every one of its %d vertices is at (%v, %v): %w",
			len(verts), verts[0].Lat, verts[0].Lon, ErrWalkHasNoLength)
	}
	return nil
}
