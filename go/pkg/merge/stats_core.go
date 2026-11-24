package merge

import (
	"errors"
	"math"
)

// ArcWalkStatsAndBounds is StatsFromPips and BoundsFromPips of the walk
// [start, interior..., end], without building it: the same walk, the same
// pairs in the same order, so the same numbers bit for bit. It is for arcs
// whose interior can be millions of pips, where a joined copy of the line
// (86 MB for 2.8M pips) would be held while the arc is stored. One walk
// accessor feeds both derivations, so they cannot walk different geometry.
func ArcWalkStatsAndBounds(start, end Node, interior []Pip) (ArcStats, Bounds) {
	first := Pip{Lat: start.Lat, Lon: start.Lon, Ele: NodeEle(start)}
	last := Pip{Lat: end.Lat, Lon: end.Lon, Ele: NodeEle(end)}
	n := len(interior) + 2 //nolint:mnd // the two endpoint nodes
	at := func(i int) Pip {
		switch i {
		case 0:
			return first
		case n - 1:
			return last
		}
		return interior[i-1]
	}
	var stats ArcStats
	b := Bounds{MinLat: first.Lat, MaxLat: first.Lat, MinLon: first.Lon, MaxLon: first.Lon}
	for i := 0; i+1 < n; i++ {
		a, c := at(i), at(i+1)
		stats.DistanceM += HaversineMetres(a.Lat, a.Lon, c.Lat, c.Lon)
		g, l := ElevDelta(a.Ele, c.Ele)
		stats.ElevGainM += g
		stats.ElevLossM += l
		ExpandBounds(&b, c.Lat, c.Lon)
	}
	return stats, b
}

// ElevDelta returns the (gain, loss) contribution of stepping from
// elevation a to b. Either may be NaN ("unknown" on a Pip) — in that
// case the step contributes zero to both totals so unknown-ele pips
// are silently ignored rather than dragging stats down to noise.
func ElevDelta(a, b float64) (gain, loss float64) {
	if math.IsNaN(a) || math.IsNaN(b) {
		return 0, 0
	}
	d := b - a
	if d > 0 {
		return d, 0
	}
	return 0, -d
}

// NodeEle returns the node's elevation as a float64 with NaN standing
// in for a nil Ele pointer. Matching Pip's "NaN means unknown"
// convention lets the rest of the stats code treat both inputs
// uniformly.
func NodeEle(n Node) float64 {
	if n.Ele == nil {
		return math.NaN()
	}
	return *n.Ele
}

// ExpandBounds widens b in-place to include the (lat, lon) point.
func ExpandBounds(b *Bounds, lat, lon float64) {
	if lat < b.MinLat {
		b.MinLat = lat
	}
	if lat > b.MaxLat {
		b.MaxLat = lat
	}
	if lon < b.MinLon {
		b.MinLon = lon
	}
	if lon > b.MaxLon {
		b.MaxLon = lon
	}
}

// StatsFromPips computes distance / elevation gain / loss across a complete
// point list.
//
// A list of fewer than two pips yields the zero ArcStats: there is no pairwise
// step to accumulate. That is arithmetic rather than a fallback — no
// information is being substituted for missing information. A stored track
// segment always has at least two pips, so it never reaches this case.
//
// Missing elevation (NaN on a Pip) contributes zero to both totals, so a
// segment with no elevation data at all reports 0 m of gain. That is the
// intended meaning of 0, not an accident: 0 means "no accountable climb",
// covering genuinely flat and no-elevation-data alike.
//
// It reads any pip type through [PipReader], so a caller holding its own pips
// computes the stats without copying them into [Pip].
func StatsFromPips[P PipReader](pips []P) ArcStats {
	var stats ArcStats
	for i := 0; i+1 < len(pips); i++ {
		aLat, aLon, aEle := pips[i].LatLonEle()
		bLat, bLon, bEle := pips[i+1].LatLonEle()
		stats.DistanceM += HaversineMetres(aLat, aLon, bLat, bLon)
		g, l := ElevDelta(aEle, bEle)
		stats.ElevGainM += g
		stats.ElevLossM += l
	}
	return stats
}

// PipReader is a pip in any type: its latitude, longitude and elevation (NaN
// for unknown). [StatsFromPips] and [BoundsFromPips] read pips through it.
type PipReader interface {
	LatLonEle() (lat, lon, ele float64)
}

// LatLonEle returns the pip's fields as they are.
func (p Pip) LatLonEle() (lat, lon, ele float64) { return p.Lat, p.Lon, p.Ele }

// ErrNoPips is BoundsFromPips's error for an empty list.
var ErrNoPips = errors.New("no pips: an empty point list has no bounding box, and the zero box is a real place in the Atlantic")

// BoundsFromPips returns the axis-aligned lat/lon bounding box containing
// every pip.
//
// ⚠ AN EMPTY LIST IS AN ERROR, AND THAT IS THE WHOLE POINT OF THE SIGNATURE.
// The obvious implementation returns the zero Bounds for no input — and
// (0, 0, 0, 0) is a REAL PLACE, in the Atlantic off the coast of Ghana. A
// caller that stored it would get a track whose map camera flies to the Gulf
// of Guinea, with no error anywhere to explain why. There is no defensible
// bounding box for no points, so this returns an error rather than inventing
// one.
//
// Like [StatsFromPips], it reads any pip type through [PipReader].
func BoundsFromPips[P PipReader](pips []P) (Bounds, error) {
	if len(pips) == 0 {
		return Bounds{}, ErrNoPips
	}
	lat0, lon0, _ := pips[0].LatLonEle()
	b := Bounds{
		MinLat: lat0, MaxLat: lat0,
		MinLon: lon0, MaxLon: lon0,
	}
	for _, p := range pips[1:] {
		lat, lon, _ := p.LatLonEle()
		ExpandBounds(&b, lat, lon)
	}
	return b, nil
}

// ArcStats is an arc's distance / elevation summary, computed from the
// fully expanded geometry — start node, then interior pips, then end node.
type ArcStats struct {
	DistanceM float64
	ElevGainM float64
	ElevLossM float64
}
