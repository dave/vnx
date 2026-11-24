package merge

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// MemCanon is a [Canon] held in memory. It answers every read the way a canon
// kept in SQLite tables (node and arc rows, an R*Tree of whole-arc boxes)
// answers it, so that a merge run on a MemCanon writes the same ids, nodes
// and arcs, bit for bit, as the same merge run on such a store:
//
//   - Values are stored as SQLite stores a REAL: -0.0 becomes +0.0, and a NaN
//     elevation becomes unknown (nil). A NaN latitude, longitude, stat or
//     bound is refused, as a NOT NULL column refuses it, and so is an arc
//     whose two ends are one node.
//   - Ids come from two counters, one for nodes and one for arcs, that only
//     go up: an id is never reused, even after its arc is deleted by a split.
//   - Whole-arc boxes are stored as float32, as SQLite's R*Tree stores them
//     (see [newBox32]), and ArcIDsInRect returns exactly the arcs whose
//     stored box meets the query, inclusive at both ends.
//   - NodesInBox filters exactly (inclusive) and orders by (lat, lon, id).
//   - ArcsNearPoint narrows through boxes of short stretches of each arc,
//     tested exactly in float64. It returns a superset of the arcs with a
//     stretch whose box meets the query, as the Canon contract allows.
//   - SplitArc computes each half's stats and bounds from the walk of the
//     stored end nodes, the half's pips and the new node (its elevation as
//     given, before storing), with [StatsFromPips] and [BoundsFromPips].
//   - An arc's box is built from its bounds as given, so a -0.0 bound gives
//     a -0.0f box bound, as arc_rtree keeps it, while the bounds read back
//     as +0.0. NewMemCanon builds each box from the snapshot's bounds, which
//     a store has already read back as +0.0; the bits then differ from such a
//     row, but no query does (-0.0 == +0.0).
//   - NodesInBox leaves Ele nil, and an Arc with no interior has an empty,
//     non-nil Polyline, as rows read from SQLite give them.
//
// Arc and ArcEnds error on an arc the MemCanon does not hold. A canon that
// reads its rows by id alone may instead return another owner's arc.
//
// Every read returns copies: changing a returned value never changes the
// canon. Every method checks its ctx first. The first error from any method,
// a cancelled ctx included, poisons the canon: that method and every later
// one return it.
//
// A MemCanon has no rollback. When [AddLeg] fails, the leg may have written
// part of its changes, and the canon must not be used again. To keep the
// canon as it was before a leg, take a [MemCanon.Snapshot] first and, on an
// error, build a new canon from it with [NewMemCanon]:
//
//	before, err := canon.Snapshot()
//	if err != nil { ... }
//	if _, err := merge.AddLeg(ctx, canon, pts, opts); err != nil {
//		canon, _ = merge.NewMemCanon(before) // the leg is undone
//	}
//
// A MemCanon is not safe for concurrent use.
type MemCanon struct {
	nodes    map[NodeID]*memNode
	arcs     map[ArcID]*memArc
	nextNode NodeID
	nextArc  ArcID

	nodeGrid map[cellKey][]NodeID
	byNode   map[NodeID][]ArcID // the arcs each node ends
	arcBoxes boxIndex[ArcID]
	stretch  boxIndex[stretchKey]

	err error
}

var _ Canon = (*MemCanon)(nil)

// MemCanonArc is one arc of a [MemCanonSnapshot]: its end nodes, its interior
// pips (elevation bits as stored, NaN for unknown), and its stored stats and
// bounds.
type MemCanonArc struct {
	ID          ArcID
	StartNodeID NodeID
	EndNodeID   NodeID
	Polyline    []Pip
	Stats       ArcStats
	Bounds      Bounds
}

// MemCanonSnapshot is a whole canon: its nodes and arcs, and the next id each
// counter will give. NextNodeID and NextArcID must be above every id in use
// (a counter that has given an id never gives it again, so they may be
// higher still).
type MemCanonSnapshot struct {
	Nodes      []Node
	Arcs       []MemCanonArc
	NextNodeID NodeID
	NextArcID  ArcID
}

// Box32 is a box as SQLite's R*Tree stores it: four float32 bounds.
type Box32 struct {
	MinLat, MaxLat, MinLon, MaxLon float32
}

// newBox32 is the box SQLite's R*Tree stores for b. Each bound becomes the
// nearest float32 when that does not move it inward; otherwise it becomes
// the nearest float32 to the bound scaled outward by a factor of 1 ± 2^-23
// (rtree.c's rtreeValueDown and rtreeValueUp). That is usually the next
// float32 outward, but not always: it can be two steps out.
func newBox32(b Bounds) Box32 {
	return Box32{
		MinLat: rtreeDown(b.MinLat), MaxLat: rtreeUp(b.MaxLat),
		MinLon: rtreeDown(b.MinLon), MaxLon: rtreeUp(b.MaxLon),
	}
}

// rtreeNudge is 2^-23, the relative step rtree.c widens a bound by.
const rtreeNudge = 1.0 / 8388608.0

func rtreeDown(d float64) float32 {
	f := float32(d)
	if float64(f) > d {
		if d < 0 {
			return float32(d * (1 + rtreeNudge))
		}
		return float32(d * (1 - rtreeNudge))
	}
	return f
}

func rtreeUp(d float64) float32 {
	f := float32(d)
	if float64(f) < d {
		if d < 0 {
			return float32(d * (1 - rtreeNudge))
		}
		return float32(d * (1 + rtreeNudge))
	}
	return f
}

func (b Box32) box64() box64 {
	return box64{minLat: float64(b.MinLat), maxLat: float64(b.MaxLat), minLon: float64(b.MinLon), maxLon: float64(b.MaxLon)}
}

type memNode struct {
	lat, lon float64
	ele      *float64 // never NaN, never -0.0
}

type memArc struct {
	start, end NodeID
	pips       []Pip
	stats      ArcStats
	bounds     Bounds
	box        Box32
	stretches  int
}

type stretchKey struct {
	arc ArcID
	i   int
}

// memStretchVerts is how many vertices one stretch box covers. Consecutive
// stretches share a vertex, so every segment lies inside one box. The size
// moves speed only: ArcsNearPoint may return any superset.
const memStretchVerts = 32

// NewMemCanon returns a MemCanon holding s. Its values are stored as
// SQLite stores them (see [MemCanon]); a NaN where SQLite refuses one, a
// repeated id, an id at or above its counter, an id below 1, an arc end that
// names no node, or an arc whose ends are one node, is an error.
//
// A zero NextNodeID or NextArcID means 1, so NewMemCanon(MemCanonSnapshot{})
// is an empty canon whose first node and first arc get id 1.
func NewMemCanon(s MemCanonSnapshot) (*MemCanon, error) {
	c, err := newMemCanon(s)
	if err != nil {
		return nil, apiError("new memory canon", err)
	}
	return c, nil
}

// newMemCanon is [NewMemCanon] without the error prefix the API adds; the merge calls it.
func newMemCanon(s MemCanonSnapshot) (*MemCanon, error) {
	if s.NextNodeID == 0 {
		s.NextNodeID = 1
	}
	if s.NextArcID == 0 {
		s.NextArcID = 1
	}
	if s.NextNodeID < 1 || s.NextArcID < 1 {
		return nil, fmt.Errorf("next node id %d and next arc id %d must be at least 1", s.NextNodeID, s.NextArcID)
	}
	c := &MemCanon{
		nodes:    make(map[NodeID]*memNode, len(s.Nodes)),
		arcs:     make(map[ArcID]*memArc, len(s.Arcs)),
		nextNode: s.NextNodeID,
		nextArc:  s.NextArcID,
		nodeGrid: map[cellKey][]NodeID{},
		byNode:   map[NodeID][]ArcID{},
		arcBoxes: newBoxIndex[ArcID](),
		stretch:  newBoxIndex[stretchKey](),
	}
	for _, n := range s.Nodes {
		if n.ID < 1 || n.ID >= s.NextNodeID {
			return nil, fmt.Errorf("node %d is outside [1, %d), below the next node id", n.ID, s.NextNodeID)
		}
		if _, dup := c.nodes[n.ID]; dup {
			return nil, fmt.Errorf("node %d appears twice", n.ID)
		}
		if err := c.putNode(n.ID, n.Lat, n.Lon, n.Ele); err != nil {
			return nil, err
		}
	}
	for _, a := range s.Arcs {
		if a.ID < 1 || a.ID >= s.NextArcID {
			return nil, fmt.Errorf("arc %d is outside [1, %d), below the next arc id", a.ID, s.NextArcID)
		}
		if _, dup := c.arcs[a.ID]; dup {
			return nil, fmt.Errorf("arc %d appears twice", a.ID)
		}
		if err := c.putArc(a.ID, a.StartNodeID, a.EndNodeID, append([]Pip(nil), a.Polyline...), a.Stats, a.Bounds); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Err returns the error that poisoned the canon, or nil.
func (c *MemCanon) Err() error { return c.err }

// enter is every method's first step: the poison, then ctx.
func (c *MemCanon) enter(ctx context.Context) error {
	if c.err != nil {
		return c.err
	}
	if err := ctx.Err(); err != nil {
		return c.fail(err)
	}
	return nil
}

// fail poisons the canon with err (unless it already is) and returns the
// poison.
func (c *MemCanon) fail(err error) error {
	if c.err == nil {
		c.err = fmt.Errorf("memory canon: %w", err)
	}
	return c.err
}

// storedReal is v as a NOT NULL REAL column stores it.
func storedReal(v float64, what string) (float64, error) {
	if math.IsNaN(v) {
		return 0, fmt.Errorf("%s is NaN", what)
	}
	if v == 0 {
		return 0, nil // -0.0 is stored as +0.0
	}
	return v, nil
}

// storedEle is an elevation as a nullable REAL column stores it, in a fresh
// pointer.
func storedEle(e *float64) *float64 {
	if e == nil || math.IsNaN(*e) {
		return nil
	}
	v := *e
	if v == 0 {
		v = 0
	}
	return &v
}

func copyEle(e *float64) *float64 {
	if e == nil {
		return nil
	}
	v := *e
	return &v
}

func storedStats(s ArcStats) (ArcStats, error) {
	var err error
	var out ArcStats
	if out.DistanceM, err = storedReal(s.DistanceM, "distance"); err != nil {
		return ArcStats{}, err
	}
	if out.ElevGainM, err = storedReal(s.ElevGainM, "elevation gain"); err != nil {
		return ArcStats{}, err
	}
	if out.ElevLossM, err = storedReal(s.ElevLossM, "elevation loss"); err != nil {
		return ArcStats{}, err
	}
	return out, nil
}

func storedBounds(b Bounds) (Bounds, error) {
	var err error
	var out Bounds
	if out.MinLat, err = storedReal(b.MinLat, "bounds min latitude"); err != nil {
		return Bounds{}, err
	}
	if out.MinLon, err = storedReal(b.MinLon, "bounds min longitude"); err != nil {
		return Bounds{}, err
	}
	if out.MaxLat, err = storedReal(b.MaxLat, "bounds max latitude"); err != nil {
		return Bounds{}, err
	}
	if out.MaxLon, err = storedReal(b.MaxLon, "bounds max longitude"); err != nil {
		return Bounds{}, err
	}
	return out, nil
}

// putNode stores node id, normalising its values.
func (c *MemCanon) putNode(id NodeID, lat, lon float64, ele *float64) error {
	sLat, err := storedReal(lat, fmt.Sprintf("node %d latitude", id))
	if err != nil {
		return err
	}
	sLon, err := storedReal(lon, fmt.Sprintf("node %d longitude", id))
	if err != nil {
		return err
	}
	c.nodes[id] = &memNode{lat: sLat, lon: sLon, ele: storedEle(ele)}
	k := cellKey{cellOf(sLat), cellOf(sLon)}
	c.nodeGrid[k] = append(c.nodeGrid[k], id)
	return nil
}

// putArc stores arc id, taking ownership of pips and normalising stats and
// bounds.
func (c *MemCanon) putArc(id ArcID, start, end NodeID, pips []Pip, stats ArcStats, bounds Bounds) error {
	if start == end {
		return fmt.Errorf("arc %d starts and ends on node %d", id, start)
	}
	sn, ok := c.nodes[start]
	if !ok {
		return fmt.Errorf("arc %d start node %d does not exist", id, start)
	}
	en, ok := c.nodes[end]
	if !ok {
		return fmt.Errorf("arc %d end node %d does not exist", id, end)
	}
	st, err := storedStats(stats)
	if err != nil {
		return fmt.Errorf("arc %d: %w", id, err)
	}
	bd, err := storedBounds(bounds)
	if err != nil {
		return fmt.Errorf("arc %d: %w", id, err)
	}
	// The box is built from the bounds as given, before the REAL columns
	// store -0.0 as +0.0: arc_rtree is written from the same values, and
	// keeps a -0.0 bound as -0.0f (bits 0x80000000).
	a := &memArc{start: start, end: end, pips: pips, stats: st, bounds: bd, box: newBox32(bounds)}
	c.arcs[id] = a
	c.byNode[start] = append(c.byNode[start], id)
	c.byNode[end] = append(c.byNode[end], id)
	c.arcBoxes.add(id, a.box.box64())

	// Stretch boxes over the walk [start, pips..., end], sharing end vertices.
	n := len(pips) + 2
	vert := func(i int) (float64, float64) {
		switch i {
		case 0:
			return sn.lat, sn.lon
		case n - 1:
			return en.lat, en.lon
		}
		return pips[i-1].Lat, pips[i-1].Lon
	}
	for from := 0; from < n-1; from += memStretchVerts - 1 {
		to := min(from+memStretchVerts-1, n-1)
		lat, lon := vert(from)
		b := box64{minLat: lat, maxLat: lat, minLon: lon, maxLon: lon}
		for i := from + 1; i <= to; i++ {
			lat, lon := vert(i)
			b.minLat, b.maxLat = math.Min(b.minLat, lat), math.Max(b.maxLat, lat)
			b.minLon, b.maxLon = math.Min(b.minLon, lon), math.Max(b.maxLon, lon)
		}
		c.stretch.add(stretchKey{id, a.stretches}, b)
		a.stretches++
	}
	return nil
}

func (c *MemCanon) dropArc(id ArcID) {
	a := c.arcs[id]
	delete(c.arcs, id)
	c.arcBoxes.remove(id)
	for i := range a.stretches {
		c.stretch.remove(stretchKey{id, i})
	}
	for _, n := range []NodeID{a.start, a.end} {
		ids := c.byNode[n]
		for i, have := range ids {
			if have == id {
				ids = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		c.byNode[n] = ids
	}
}

func (c *MemCanon) mintNode(lat, lon float64, ele *float64) (NodeID, error) {
	id := c.nextNode
	c.nextNode++
	if err := c.putNode(id, lat, lon, ele); err != nil {
		return 0, err
	}
	return id, nil
}

func (c *MemCanon) mintArc(start, end NodeID, pips []Pip, stats ArcStats, bounds Bounds) (ArcID, error) {
	id := c.nextArc
	c.nextArc++
	if err := c.putArc(id, start, end, pips, stats, bounds); err != nil {
		return 0, err
	}
	return id, nil
}

func (n *memNode) node(id NodeID) Node {
	return Node{ID: id, Lat: n.lat, Lon: n.lon, Ele: copyEle(n.ele)}
}

// canonArc is arc id as a vertex list: unknown elevations (nil on a node,
// NaN on a pip) are nil.
func (c *MemCanon) canonArc(id ArcID) CanonArc {
	a := c.arcs[id]
	sn, en := c.nodes[a.start], c.nodes[a.end]
	v := make([]Point, 0, len(a.pips)+2)
	v = append(v, Point{Lat: sn.lat, Lon: sn.lon, Ele: copyEle(sn.ele)})
	for _, p := range a.pips {
		var ele *float64
		if !math.IsNaN(p.Ele) {
			e := p.Ele
			ele = &e
		}
		v = append(v, Point{Lat: p.Lat, Lon: p.Lon, Ele: ele})
	}
	v = append(v, Point{Lat: en.lat, Lon: en.lon, Ele: copyEle(en.ele)})
	return CanonArc{ID: id, Vertices: v}
}

func (c *MemCanon) arcRow(id ArcID) Arc {
	a := c.arcs[id]
	return Arc{
		ID: id, StartNodeID: a.start, EndNodeID: a.end,
		Polyline:  copyPips(a.pips),
		DistanceM: a.stats.DistanceM,
	}
}

// copyPips copies pips into a slice that is never nil, as a decoded row
// gives (empty, not nil, for an arc with no interior).
func copyPips(pips []Pip) []Pip {
	out := make([]Pip, len(pips))
	copy(out, pips)
	return out
}

func sortedArcIDs(set map[ArcID]struct{}) []ArcID {
	out := make([]ArcID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NodesInBox implements [Canon]. Like a canon whose box query selects only
// id, lat and lon, it leaves Ele nil.
func (c *MemCanon) NodesInBox(ctx context.Context, b Bounds) ([]Node, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	in := func(n *memNode) bool {
		return n.lat >= b.MinLat && n.lat <= b.MaxLat && n.lon >= b.MinLon && n.lon <= b.MaxLon
	}
	var out []Node
	q := boundsBox(b)
	if q.hasNaN() || b.MinLat > b.MaxLat || b.MinLon > b.MaxLon {
		return nil, nil
	}
	la0, la1, nLat := cellSpan(b.MinLat, b.MaxLat)
	lo0, lo1, nLon := cellSpan(b.MinLon, b.MaxLon)
	if nLat*nLon > float64(len(c.nodes)) {
		for id, n := range c.nodes {
			if in(n) {
				out = append(out, Node{ID: id, Lat: n.lat, Lon: n.lon})
			}
		}
	} else {
		for la := la0; la <= la1; la++ {
			for lo := lo0; lo <= lo1; lo++ {
				for _, id := range c.nodeGrid[cellKey{la, lo}] {
					if n := c.nodes[id]; in(n) {
						out = append(out, Node{ID: id, Lat: n.lat, Lon: n.lon})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Lat != y.Lat {
			return x.Lat < y.Lat
		}
		if x.Lon != y.Lon {
			return x.Lon < y.Lon
		}
		return x.ID < y.ID
	})
	return out, nil
}

// Node implements [Canon]. Every node a MemCanon holds is its own.
func (c *MemCanon) Node(ctx context.Context, id NodeID) (Node, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Node{}, false, err
	}
	n, ok := c.nodes[id]
	if !ok {
		return Node{}, false, c.fail(fmt.Errorf("node %d does not exist", id))
	}
	return n.node(id), true, nil
}

func (c *MemCanon) arcIDsInRect(b Bounds) []ArcID {
	set := map[ArcID]struct{}{}
	c.arcBoxes.query(boundsBox(b), func(id ArcID) { set[id] = struct{}{} })
	if len(set) == 0 {
		return nil // as a row scan into a nil slice gives
	}
	return sortedArcIDs(set)
}

// ArcIDsInRect implements [Canon]: exactly the arcs whose stored float32 box
// (see [newBox32]) meets b, inclusive at both ends, ordered by id.
func (c *MemCanon) ArcIDsInRect(ctx context.Context, b Bounds) ([]ArcID, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	return c.arcIDsInRect(b), nil
}

// ArcsInRect implements [Canon].
func (c *MemCanon) ArcsInRect(ctx context.Context, b Bounds) ([]CanonArc, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	ids := c.arcIDsInRect(b)
	out := make([]CanonArc, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.canonArc(id))
	}
	return out, nil
}

// ArcsNearPoint implements [Canon].
func (c *MemCanon) ArcsNearPoint(ctx context.Context, b Bounds) ([]CanonArc, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	set := map[ArcID]struct{}{}
	c.stretch.query(boundsBox(b), func(k stretchKey) { set[k.arc] = struct{}{} })
	out := make([]CanonArc, 0, len(set))
	for _, id := range sortedArcIDs(set) {
		out = append(out, c.canonArc(id))
	}
	return out, nil
}

// Arc implements [Canon]. An arc the MemCanon does not hold is an error.
func (c *MemCanon) Arc(ctx context.Context, id ArcID) (Arc, error) {
	if err := c.enter(ctx); err != nil {
		return Arc{}, err
	}
	if _, ok := c.arcs[id]; !ok {
		return Arc{}, c.fail(fmt.Errorf("arc %d does not exist", id))
	}
	return c.arcRow(id), nil
}

// CanonArc implements [Canon].
func (c *MemCanon) CanonArc(ctx context.Context, id ArcID) (CanonArc, bool, error) {
	if err := c.enter(ctx); err != nil {
		return CanonArc{}, false, err
	}
	if _, ok := c.arcs[id]; !ok {
		return CanonArc{}, false, nil
	}
	return c.canonArc(id), true, nil
}

// ArcEnds implements [Canon]. An arc the MemCanon does not hold is an error.
func (c *MemCanon) ArcEnds(ctx context.Context, id ArcID) (NodeID, NodeID, error) {
	if err := c.enter(ctx); err != nil {
		return 0, 0, err
	}
	a, ok := c.arcs[id]
	if !ok {
		return 0, 0, c.fail(fmt.Errorf("arc %d does not exist", id))
	}
	return a.start, a.end, nil
}

// ArcsBetween implements [Canon].
func (c *MemCanon) ArcsBetween(ctx context.Context, a, b NodeID) ([]Arc, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	set := map[ArcID]struct{}{}
	for _, id := range c.byNode[a] {
		arc := c.arcs[id]
		if (arc.start == a && arc.end == b) || (arc.start == b && arc.end == a) {
			set[id] = struct{}{}
		}
	}
	var out []Arc
	for _, id := range sortedArcIDs(set) {
		out = append(out, c.arcRow(id))
	}
	return out, nil
}

// AddNode implements [Canon].
func (c *MemCanon) AddNode(ctx context.Context, lat, lon float64, ele *float64) (NodeID, error) {
	if err := c.enter(ctx); err != nil {
		return 0, err
	}
	id, err := c.mintNode(lat, lon, ele)
	if err != nil {
		return 0, c.fail(err)
	}
	return id, nil
}

// AddArc implements [Canon]: the start node when minted, then the end node
// when minted, then the arc. A reused end must be a node the MemCanon holds.
func (c *MemCanon) AddArc(ctx context.Context, a NewArc) (AddedArc, error) {
	if err := c.enter(ctx); err != nil {
		return AddedArc{}, err
	}
	start, end := a.Start.ID, a.End.ID
	var err error
	if a.MintStart {
		if start, err = c.mintNode(a.Start.Lat, a.Start.Lon, a.Start.Ele); err != nil {
			return AddedArc{}, c.fail(fmt.Errorf("start node: %w", err))
		}
	}
	if a.MintEnd {
		if end, err = c.mintNode(a.End.Lat, a.End.Lon, a.End.Ele); err != nil {
			return AddedArc{}, c.fail(fmt.Errorf("end node: %w", err))
		}
	}
	id, err := c.mintArc(start, end, append([]Pip(nil), a.Polyline...), a.Stats, a.Bounds)
	if err != nil {
		return AddedArc{}, c.fail(err)
	}
	return AddedArc{ID: id, StartNodeID: start, EndNodeID: end}, nil
}

// SplitArc implements [Canon]: the node, then the prior half, then the next
// half, then the parent is deleted. Each half's stats and bounds are
// computed from its walk: the stored end node, its pips, and the new node at
// (s.Lat, s.Lon) with elevation s.Ele as given.
func (c *MemCanon) SplitArc(ctx context.Context, id ArcID, s PolylineSplit) (SplitIDs, error) {
	if err := c.enter(ctx); err != nil {
		return SplitIDs{}, err
	}
	parent, ok := c.arcs[id]
	if !ok {
		return SplitIDs{}, c.fail(fmt.Errorf("split arc %d: arc does not exist", id))
	}
	n := len(parent.pips)
	if s.PriorEnd < 0 || s.PriorEnd > n || s.NextStart < 0 || s.NextStart > n || s.PriorEnd > s.NextStart {
		return SplitIDs{}, c.fail(fmt.Errorf("split arc %d: halves end at pip %d and start at pip %d of %d, out of range",
			id, s.PriorEnd, s.NextStart, n))
	}
	nodeID, err := c.mintNode(s.Lat, s.Lon, s.Ele)
	if err != nil {
		return SplitIDs{}, c.fail(fmt.Errorf("split arc %d: node: %w", id, err))
	}
	join := Pip{Lat: s.Lat, Lon: s.Lon, Ele: elevOrNaN(s.Ele)}
	half := func(from NodeID, pips []Pip, to NodeID, fromJoin bool) (ArcID, error) {
		walk := make([]Pip, 0, len(pips)+2)
		if fromJoin {
			walk = append(walk, join)
		} else {
			walk = append(walk, c.storedPip(from))
		}
		walk = append(walk, pips...)
		if fromJoin {
			walk = append(walk, c.storedPip(to))
		} else {
			walk = append(walk, join)
		}
		bounds, err := boundsFromPips(walk)
		if err != nil {
			return 0, err
		}
		return c.mintArc(from, to, pips, StatsFromPips(walk), bounds)
	}
	prior, err := half(parent.start, append([]Pip(nil), parent.pips[:s.PriorEnd]...), nodeID, false)
	if err != nil {
		return SplitIDs{}, c.fail(fmt.Errorf("split arc %d: prior half: %w", id, err))
	}
	next, err := half(nodeID, append([]Pip(nil), parent.pips[s.NextStart:]...), parent.end, true)
	if err != nil {
		return SplitIDs{}, c.fail(fmt.Errorf("split arc %d: next half: %w", id, err))
	}
	c.dropArc(id)
	return SplitIDs{Node: nodeID, Prior: prior, Next: next}, nil
}

// storedPip is node id as a walk vertex: its stored position and elevation,
// NaN for unknown.
func (c *MemCanon) storedPip(id NodeID) Pip {
	n := c.nodes[id]
	return Pip{Lat: n.lat, Lon: n.lon, Ele: NodeEle(Node{Ele: n.ele})}
}

// Snapshot returns a copy of the whole canon, nodes and arcs ordered by id.
// A poisoned canon returns its poison.
func (c *MemCanon) Snapshot() (MemCanonSnapshot, error) {
	if c.err != nil {
		return MemCanonSnapshot{}, c.err
	}
	out := MemCanonSnapshot{NextNodeID: c.nextNode, NextArcID: c.nextArc}
	for id, n := range c.nodes {
		out.Nodes = append(out.Nodes, n.node(id))
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	for id, a := range c.arcs {
		out.Arcs = append(out.Arcs, MemCanonArc{
			ID: id, StartNodeID: a.start, EndNodeID: a.end,
			Polyline: copyPips(a.pips),
			Stats:    a.stats, Bounds: a.bounds,
		})
	}
	sort.Slice(out.Arcs, func(i, j int) bool { return out.Arcs[i].ID < out.Arcs[j].ID })
	return out, nil
}

// ArcBox returns the float32 box the MemCanon stores for arc id, and false
// when it holds no such arc. A poisoned canon returns its poison.
func (c *MemCanon) ArcBox(id ArcID) (Box32, bool, error) {
	if c.err != nil {
		return Box32{}, false, c.err
	}
	a, ok := c.arcs[id]
	if !ok {
		return Box32{}, false, nil
	}
	return a.box, true, nil
}
