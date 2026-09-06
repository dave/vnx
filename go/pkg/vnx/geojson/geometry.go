package geojson

import (
	"fmt"

	"github.com/dave/vnx/go/pkg/vnx"
)

// ringFor validates an area's ring and returns it wound the way RFC 7946
// requires. ok is false when the ring cannot be a Polygon at all.
//
// ⚠ RFC 7946 §3.1.6 REQUIRES THE RIGHT-HAND RULE: an exterior ring runs
// COUNTERCLOCKWISE. Our areas carry no winding guarantee — the model stores
// whatever the user drew or an import produced — so a reversed ring emitted
// verbatim is a file that parses everywhere and is a hole rather than an
// island in anything that honours the rule. It is the cell of this writer most
// likely to be wrong and invisible, which is why there is a test with a
// deliberately reversed ring.
//
// ⚠ AN UNCLOSED RING IS REFUSED, NOT CLOSED FOR THE USER. Appending the first
// position to close it would invent a segment of geometry the document does
// not contain — a fallback hiding a malformed document, and the house rule
// says that becomes an error. It cannot arrive from a file: decodeRing refuses
// an unclosed ring, and every database write site validates closure. Only a
// hand-built Document reaches here with one.
func ringFor(ring []vnx.Position) ([]vnx.Position, bool) {
	if len(ring) < 4 {
		return nil, false
	}
	if ring[0].Lon != ring[len(ring)-1].Lon || ring[0].Lat != ring[len(ring)-1].Lat {
		return nil, false
	}
	if signedArea(ring) < 0 {
		return reversed(ring), true
	}
	// ⚠ A ZERO SIGNED AREA IS LEFT ALONE. A degenerate ring — every position
	// collinear, or the same point four times — has no winding to correct, and
	// reversing it or refusing it would both be inventing an answer to a
	// question the geometry does not ask. It is emitted as it stands; RFC 7946
	// asks for four closed positions, not for area.
	return ring, true
}

// ringProblem is the Message for a ring that ringFor refused. It says WHICH of
// the two rules failed: "no geometry" alone would send a reader looking for a
// missing polygon when the polygon is there and open.
func ringProblem(ring []vnx.Position) string {
	if len(ring) < 4 {
		return fmt.Sprintf("polygon has %d position(s); a GeoJSON linear ring needs at least 4", len(ring))
	}
	return "polygon is not closed: the last position must repeat the first"
}

// signedArea is the shoelace sum over lon/lat. Positive is counterclockwise —
// the right-hand rule — and negative is clockwise.
//
// ⚠ IT IS COMPUTED IN DEGREES, NOT ON THE SPHERE, and that is enough for what
// it is asked. The question is a SIGN, and a projection that preserves
// orientation preserves the sign; the magnitude is never used. Doing this
// properly on an ellipsoid would buy a number nothing reads.
//
// ⚠ IT IS COMPUTED ON THE ROUNDED COORDINATES, WHICH ARE THE ONES WRITTEN.
// This is the ONE place rounding has to come before a geometry decision, and
// the reason is that the decision is ABOUT the bytes: RFC 7946 §3.1.6 constrains
// the winding of the ring in the FILE, so a sign taken from coordinates the file
// does not contain answers the wrong question. It matters below about a metre —
// snapping to an 11 cm grid can move a vertex from one side of a near-collinear
// edge to the other, and a sliver that is counterclockwise at full precision is
// then written clockwise. The positions themselves are NOT rounded here; only
// the sign is, and rounding is idempotent, so emit's own rounding writes the
// same values either way.
func signedArea(ring []vnx.Position) float64 {
	var sum float64
	for i := 0; i < len(ring)-1; i++ {
		a, b := vnx.RoundPosition(ring[i]), vnx.RoundPosition(ring[i+1])
		sum += a.Lon*b.Lat - b.Lon*a.Lat
	}
	return sum
}

func reversed(ps []vnx.Position) []vnx.Position {
	out := make([]vnx.Position, len(ps))
	for i := range ps {
		out[i] = ps[len(ps)-1-i]
	}
	return out
}

// joinArcs flattens a route's ordered arcs into the one LineString GeoJSON
// gives it.
//
// ⚠ ARC BOUNDARIES ARE LOST HERE, AND THAT IS THE MATRIX CELL, not a defect to
// work around. A route owns its arcs outright and its node identity IS the
// shared endpoint present in both neighbouring pip lists (route.go), so
// concatenating the lists verbatim would repeat that position — a doubled
// vertex that no reader can distinguish from a genuine stationary pair. The
// join drops the duplicate.
func joinArcs(arcs []vnx.Arc) []vnx.Position {
	var out []vnx.Position
	for i := range arcs {
		out = appendJoined(out, arcs[i].Pips)
	}
	return out
}

// appendJoined appends next to line, dropping next's first position when it
// repeats the last one already there. Identity is lon/lat, which is the only
// identity an endpoint has in this format ([vnx.Endpoint.Same] says so).
func appendJoined(line, next []vnx.Position) []vnx.Position {
	if len(next) == 0 {
		return line
	}
	if n := len(line); n > 0 && line[n-1].Lon == next[0].Lon && line[n-1].Lat == next[0].Lat {
		next = next[1:]
	}
	return append(line, next...)
}

// traverse is the run of an arc's pips one link walks, in the direction the
// link walks it.
//
// ⚠ AN INLINE ENDPOINT IS INTERPOLATED, NOT SNAPPED to the nearest recorded
// pip, and the cut itself is vnx.SubPolyline — the same function kml calls, so
// one library cannot export one chain as two different polylines. A link's
// endpoint may be an INLINE position part-way along the arc (network.go: a
// handle, a point ON the arc where the chain starts, ends or turns around),
// and decode has already checked it lies within EndpointToleranceMetres of the
// polyline.
//
// ⚠ THIS WAS SNAPPING UNTIL 2026-09-04, and the comment that justified it was
// wrong on its facts, so it is replaced rather than kept. It argued that
// interpolating would make two chains sharing a turnaround emit two slightly
// different coordinates for one handle; the projection is a pure function of
// the handle and the arc, so they emit the SAME coordinate — kml had been
// interpolating all along and proves it. What snapping actually cost was the
// truth of the line: it moved a chain's end by up to half a segment and gave
// the file a length the chain does not have.
func traverse(arc vnx.Arc, l vnx.Link) []vnx.Position {
	if len(arc.Pips) == 0 {
		return nil
	}
	last := len(arc.Pips) - 1
	from := endpointPosition(arc, l.From, arc.Pips[0])
	to := endpointPosition(arc, l.To, arc.Pips[last])
	return vnx.SubPolyline(arc.Pips, from, to)
}

// endpointPosition is the position an endpoint denotes. A node key resolves by
// the arc's own From/To — the arc's identity in the graph — and anything else
// resolves to the inline position itself, which vnx.SubPolyline projects onto
// the arc. Resolving a node by the arc rather than by the network's node table
// (kml.endpointAt) is the same answer: an arc's pips are its complete point
// list and must meet its nodes (vnx.checkArcMeetsNodes).
//
// fallback is the position to use when the endpoint denotes nothing this arc
// knows: the arc's start for a link's From, its end for a link's To. That
// walks the WHOLE arc forwards, which is the reading that loses no geometry —
// the alternatives are dropping the link or guessing a direction, and a wrong
// guess draws the chain backwards with nothing to say so.
//
// ⚠ IT IS LATENT, NOT UNREACHABLE, and the older comment here claimed less
// than the callers need. It is unreachable from a DECODED document —
// decodeEndpoint refuses a node key the network does not hold, and an inline
// endpoint more than EndpointToleranceMetres off the arc. But the app's own
// writer is handed a Document that vnxbridge BUILT, which never passed through
// decodeEndpoint, and pkg/vnx is a public package whose Document is an ordinary
// struct anyone may fill in. So this branch has no current trigger rather than
// no possible one. kml degrades the same shape differently — it drops the link
// and raises CodePartialGeometry — and the two have not been reconciled.
func endpointPosition(arc vnx.Arc, e vnx.Endpoint, fallback vnx.Position) vnx.Position {
	if e.Node != "" {
		switch e.Node {
		case arc.From:
			return arc.Pips[0]
		case arc.To:
			return arc.Pips[len(arc.Pips)-1]
		}
		return fallback
	}
	if e.At == nil {
		return fallback
	}
	return *e.At
}
