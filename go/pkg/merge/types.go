// Package merge is the geometry core of the network merge: it joins GPS
// tracks into a shared network of nodes and arcs, so that a path walked twice
// is stored once.
//
// The network is reached only through [Canon]: nine reads and three writes.
// AddNode and AddArc store what they are given; SplitArc also computes the
// stats and bounds of its two halves (see [Canon.SplitArc]). [AddLeg] merges one leg into a Canon,
// running every step of the merge in order. An error from AddLeg or from any
// Canon method poisons the canon: the caller must discard every write that
// leg made, for example by rolling back its transaction.
//
// AddLeg is safe for concurrent use on separate canons. Tests observe a
// call through [WithTestHooks] on its context.
//
// The API is not stable yet. Names and signatures will change while the core
// settles; depend on it only at a pinned version.
//
// The package imports the standard library and pkg/detmath only.
// HaversineMetres and every detmath call give the same float64 bits on every
// architecture; the rest of the package does not promise that.
package merge

import (
	"math"
	"time"

	"github.com/dave/vnx/go/pkg/detmath"
)

// NodeID identifies a node of the network.
type NodeID int64

// ArcID identifies an arc of the network.
type ArcID int64

// Point is one track point: latitude and longitude in degrees, and an
// optional elevation in metres and time.
type Point struct {
	Lat  float64
	Lon  float64
	Ele  *float64
	Time *time.Time
}

// LatLon is a latitude and longitude in degrees.
type LatLon struct {
	Lat float64
	Lon float64
}

// CanonArc is an arc already in the network, with its full ordered vertex
// list: start node, interior points, end node.
type CanonArc struct {
	ID       ArcID
	Vertices []Point
}

// earthRadiusM is the Earth radius every great-circle sum here uses.
const earthRadiusM = 6371000.0

// HaversineMetres is the great-circle distance between two lat/lon points,
// in metres, on a sphere of radius 6,371,000 m.
//
// It gives the same bits on every architecture: the trig is detmath's, and
// each product that meets an add is converted to float64, so arm64's FMA
// fusion cannot round it differently from amd64.
func HaversineMetres(lat1, lon1, lat2, lon2 float64) float64 {
	const R = earthRadiusM
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	sLat := detmath.Sin(dLat / 2)
	sLon := detmath.Sin(dLon / 2)
	a := float64(sLat*sLat) +
		float64(detmath.Cos(lat1*math.Pi/180)*detmath.Cos(lat2*math.Pi/180)*sLon*sLon)
	return R * 2 * detmath.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
