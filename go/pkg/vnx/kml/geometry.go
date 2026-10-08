package kml

import (
	"github.com/dave/vnx/go/pkg/vnx"
)

// The geometry a KML file needs that VNX does not store in KML's shape.
//
// ⚠ A HIGHLIGHT'S OR A PLAN'S LINE IS vnx.WalkLine's, not this package's. A walk
// is cut where it enters, turns and leaves each arc, and every writer draws it
// through the one implementation in pkg/vnx, so one library cannot export one
// highlight as two different polylines.

// closedRing is an area's ring as KML writes it: VNX stores a ring OPEN (SPEC
// §3.7) and a KML <LinearRing> repeats its first position last. ok is false
// when the ring cannot be one: fewer than three positions, or a ring that
// arrives already closed.
//
// ⚠ AN ALREADY-CLOSED RING IS REFUSED, NOT CLOSED TWICE. It cannot come from a
// file — the reader drops the repeat — so only a hand-built Document reaches
// here with one, and doubling its closing position would hide that.
func closedRing(ring []vnx.Position) ([]vnx.Position, bool) {
	n := len(ring)
	if n < 3 || (ring[0].Lon == ring[n-1].Lon && ring[0].Lat == ring[n-1].Lat) {
		return nil, false
	}
	out := make([]vnx.Position, 0, n+1)
	return append(append(out, ring...), ring[0]), true
}
