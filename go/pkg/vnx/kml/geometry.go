package kml

import (
	"github.com/dave/vnx/go/pkg/vnx"
)

// The geometry a chain needs, and nothing more.
//
// ⚠ A CHAIN IS NOT A LIST OF WHOLE ARCS. Each of its links traverses one arc
// BETWEEN TWO ENDPOINTS, and an endpoint is either a node — the arc's own end —
// or an inline position part-way along it (vnx.Endpoint). Emitting the whole
// arc for a link that uses part of it would draw the chain running past where
// the user stopped, in the one object §2.6 exists to keep named and walkable.
// So the arc is cut at both endpoints and only the piece between them is kept.
//
// ⚠ THE CUT ITSELF LIVES IN vnx.SubPolyline, not here. It used to live in this
// file, and geojson carried a second answer that snapped an endpoint to the
// nearest pip — one library exporting one chain as two different polylines.
// The shared answer moved to pkg/vnx (2026-09-04); this package now only
// decides WHICH points to cut between.

// appendJoined adds ps to out, dropping ps's first position when it repeats the
// last one already there.
//
// ⚠ THE DUPLICATE IS REAL AND EXPECTED, not a defect being papered over. A
// route's arc k ends at the same position arc k+1 starts from (vnx.Route: node
// identity IS the shared endpoint), and a chain's consecutive links meet at a
// shared handle (vnx.checkChainContinuity). Emitting it twice would put a
// zero-length step in the middle of every join. Positions that do NOT match are
// kept as they are — a gap the document carries is not ours to close.
func appendJoined(out, ps []vnx.Position) []vnx.Position {
	if len(out) > 0 && len(ps) > 0 && out[len(out)-1].Lon == ps[0].Lon && out[len(out)-1].Lat == ps[0].Lat {
		ps = ps[1:]
	}
	return append(out, ps...)
}

// chainLine is the polyline a chain draws: each link's piece of its arc, joined.
//
// ok is false when the chain has no drawable geometry — no links, or none whose
// arc and endpoints resolve. ⚠ A DECODED DOCUMENT CANNOT REACH THAT: decodeLinks
// drops a link naming an unknown arc, and decodeEndpoint drops one naming an
// unknown node. A hand-built Document can, and pkg/vnx is a public package whose
// Document is an ordinary struct anyone may fill in.
//
// ⚠ lost IS REPORTED WHETHER OR NOT ok, and the CALLER decides what to do with
// it — there is exactly one place that knows whether the chain survived, and
// two would be two answers. When ok is false the chain is wholly gone, so its
// links are not "partly" lost and the caller must raise only the whole-item
// Code; raising both would report one loss twice in two contradicting senses.
//
// ⚠ lost NAMES THE LINKS THAT CONTRIBUTED NOTHING, and returning it is not
// bookkeeping. A chain that loses one link of four is still exported, so the
// caller owes the reader a [vnx.CodePartialGeometry] row saying which part is
// missing (ruled 2026-09-02). Dropping a link and saying nothing is the silent
// fallback the house rule forbids; saying "dropped" about a chain the reader can
// see in the file would be a different wrong answer.
func chainLine(n vnx.Network, c vnx.Chain) (line []vnx.Position, lost []int, ok bool) {
	var out []vnx.Position
	for i, l := range c.Links {
		arc, known := n.Arcs[l.Arc]
		if !known || len(arc.Pips) < 2 {
			lost = append(lost, i)
			continue
		}
		from, okFrom := endpointAt(n, l.From)
		to, okTo := endpointAt(n, l.To)
		if !okFrom || !okTo {
			lost = append(lost, i)
			continue
		}
		out = appendJoined(out, vnx.SubPolyline(arc.Pips, from, to))
	}
	if len(out) < 2 {
		return nil, lost, false
	}
	return out, lost, true
}

// endpointAt resolves an endpoint to a position. A node key that names no node
// in this network resolves to nothing — there is no position to fall back to,
// and picking one of the arc's ends would be a guess that draws.
func endpointAt(n vnx.Network, e vnx.Endpoint) (vnx.Position, bool) {
	if e.Node != "" {
		p, ok := n.Nodes[e.Node]
		return p, ok
	}
	if e.At == nil {
		return vnx.Position{}, false
	}
	return *e.At, true
}

// routeLine is the single polyline a route draws (spec 5b §3.2: a route is one
// <LineString>). ⚠ ARC BOUNDARIES ARE LOST HERE and that is the matrix's stated
// loss for this cell, not an accident: KML has no way to say "this path is made
// of four pieces, each with its own routing history".
func routeLine(r vnx.Route) ([]vnx.Position, bool) {
	var out []vnx.Position
	for _, a := range r.Arcs {
		out = appendJoined(out, a.Pips)
	}
	if len(out) < 2 {
		return nil, false
	}
	return out, true
}
