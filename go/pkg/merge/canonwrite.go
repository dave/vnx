package merge

import (
	"context"
	"fmt"
)

// NewArc is an arc ready to store. Start and End are its end nodes: a node
// the arc reuses (MintStart or MintEnd false; the node as Canon.Node read it)
// or a node to mint at that position (true; its ID is ignored). Polyline is
// the interior in walk order, and Stats and Bounds describe the walk from
// Start through Polyline to End.
type NewArc struct {
	Start, End         Node
	MintStart, MintEnd bool
	Polyline           []Pip
	Stats              ArcStats
	Bounds             Bounds
}

// AddedArc is the ids Canon.AddArc stored.
type AddedArc struct {
	ID          ArcID
	StartNodeID NodeID
	EndNodeID   NodeID
}

// SplitIDs is the ids Canon.SplitArc stored.
type SplitIDs struct {
	Node        NodeID
	Prior, Next ArcID
}

// splitResult is a split as splitArcAt made it: the new node (its position is
// the split's), the two halves, and whether the node took the place of one of
// the parent's interior pips (PolylineSplit.ConsumedParentVertex).
type splitResult struct {
	Node                 Node
	Prior, Next          ArcID
	ConsumedParentVertex bool
}

// PrepareArc computes the arc a leg's points make, writing nothing. startID
// and endID are each nil (mint a node at that end's point) or a node of c the
// arc must terminate on; such a node's position and elevation replace the
// point's. A named node that c does not own is refused, as is the same node
// named for both ends (an arc may not start and end on one node).
func PrepareArc(ctx context.Context, c Canon, points []Point, startID, endID *NodeID) (NewArc, error) {
	a, err := prepareArc(ctx, c, points, startID, endID)
	if err != nil {
		return a, apiError("prepare arc", err)
	}
	return a, nil
}

// prepareArc is [PrepareArc] without the error prefix the API adds; the merge calls it.
func prepareArc(ctx context.Context, c Canon, points []Point, startID, endID *NodeID) (NewArc, error) {
	if len(points) < 2 {
		return NewArc{}, fmt.Errorf("an arc needs at least 2 points, got %d", len(points))
	}
	if startID != nil && endID != nil && *startID == *endID {
		return NewArc{}, fmt.Errorf(
			"both endpoints name node %d: an arc may not start and end on one node",
			*startID)
	}
	start, mintStart, err := resolveArcEnd(ctx, c, points[0], startID)
	if err != nil {
		return NewArc{}, fmt.Errorf("start node: %w", err)
	}
	end, mintEnd, err := resolveArcEnd(ctx, c, points[len(points)-1], endID)
	if err != nil {
		return NewArc{}, fmt.Errorf("end node: %w", err)
	}
	interior, stats, bounds := arcFromPoints(points, start, end)
	return NewArc{
		Start: start, End: end,
		MintStart: mintStart, MintEnd: mintEnd,
		Polyline: interior,
		Stats:    stats,
		Bounds:   bounds,
	}, nil
}

// resolveArcEnd is one end of PrepareArc: the node to mint at pt when want is
// nil, or the canon's node want, read and checked.
func resolveArcEnd(ctx context.Context, c Canon, pt Point, want *NodeID) (Node, bool, error) {
	if want == nil {
		return Node{Lat: pt.Lat, Lon: pt.Lon, Ele: pt.Ele}, true, nil
	}
	n, inCanon, err := c.Node(ctx, *want)
	if err != nil {
		return Node{}, false, fmt.Errorf("read node %d: %w", *want, err)
	}
	if err := checkEndpointOwner(*want, inCanon, "another owner", "this canon"); err != nil {
		return Node{}, false, err
	}
	return n, false, nil
}

// arcFromPoints computes an arc from its points and its two resolved end
// nodes: the interior pips (points[1:len-1], see InteriorPips, with any run
// sitting exactly on a terminal node removed, see dropPipsOnTerminalNodes),
// and the stats and bounds of the walk from start through the interior to
// end.
func arcFromPoints(points []Point, start, end Node) (interior []Pip, stats ArcStats, bounds Bounds) {
	interior = dropPipsOnTerminalNodes(InteriorPips(points), start, end)
	stats, bounds = ArcWalkStatsAndBounds(start, end, interior)
	return interior, stats, bounds
}

// InteriorPips is points[1:len-1] as pips: the time is dropped (an arc never
// carries time; a canon is a map, not a recording), and a missing elevation
// becomes NaN.
func InteriorPips(points []Point) []Pip {
	interior := make([]Pip, 0, max(0, len(points)-2))
	for _, p := range points[1 : len(points)-1] {
		interior = append(interior, Pip{Lat: p.Lat, Lon: p.Lon, Ele: elevOrNaN(p.Ele)})
	}
	return interior
}

// splitArcAt splits arc id of c at fraction (in (0, 1), exclusive) of its
// walk: it reads the arc and its end nodes, computes the split
// (SplitPolylineAtFraction) and stores it with c.SplitArc.
func splitArcAt(ctx context.Context, c Canon, id ArcID, fraction float64) (splitResult, error) {
	if !(fraction > 0 && fraction < 1) {
		return splitResult{}, fmt.Errorf("split fraction %v must be in (0, 1) exclusive", fraction)
	}
	arc, err := c.Arc(ctx, id)
	if err != nil {
		return splitResult{}, err
	}
	start, _, err := c.Node(ctx, arc.StartNodeID)
	if err != nil {
		return splitResult{}, fmt.Errorf("read start node %d of arc %d: %w", arc.StartNodeID, id, err)
	}
	end, _, err := c.Node(ctx, arc.EndNodeID)
	if err != nil {
		return splitResult{}, fmt.Errorf("read end node %d of arc %d: %w", arc.EndNodeID, id, err)
	}
	s := SplitPolylineAtFraction(start, end, arc.Polyline, fraction)
	ids, err := c.SplitArc(ctx, id, s)
	if err != nil {
		return splitResult{}, err
	}
	return splitResult{
		Node:                 Node{ID: ids.Node, Lat: s.Lat, Lon: s.Lon, Ele: s.Ele},
		Prior:                ids.Prior,
		Next:                 ids.Next,
		ConsumedParentVertex: s.ConsumedParentVertex,
	}, nil
}
