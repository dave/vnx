package geojson

import (
	"fmt"

	"github.com/dave/vnx/go/pkg/vnx"
)

// lineArc is one arc of a test route written as its whole line, first point
// to last, so a fixture reads the way the route is walked.
type lineArc struct {
	Pips []vnx.Position
	Data []vnx.Record
}

// route gives r the graph and walk its lines make (SPEC §3.4): a node at each
// end of each line, the interior points as the arc's pips, and a walk from the
// first line's start to the last line's end. A line that starts where the
// previous one ended shares its node; one that does not starts a node of its
// own, and the walk then does not join, which the writers must refuse.
func route(r vnx.Route, arcs ...lineArc) vnx.Route {
	r.Nodes = map[string]vnx.Vertex{}
	r.Arcs = map[string]vnx.Arc{}
	r.Walk = []vnx.Pass{}
	nodeAt := func(p vnx.Position) string {
		for id, v := range r.Nodes {
			if v.Lon == p.Lon && v.Lat == p.Lat {
				return id
			}
		}
		id := fmt.Sprintf("node-%d", len(r.Nodes)+1)
		r.Nodes[id] = vnx.Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele}
		return id
	}
	for i, a := range arcs {
		id := fmt.Sprintf("arc-%d", i+1)
		from, to := nodeAt(a.Pips[0]), nodeAt(a.Pips[len(a.Pips)-1])
		r.Arcs[id] = vnx.Arc{From: from, To: to, Pips: vs(a.Pips[1 : len(a.Pips)-1]...), Data: a.Data}
		r.Walk = append(r.Walk, vnx.Pass{Arc: id})
	}
	if n := len(r.Walk); n > 0 {
		r.Walk[0].From = &vnx.Endpoint{Node: r.Arcs[r.Walk[0].Arc].From}
		r.Walk[n-1].To = &vnx.Endpoint{Node: r.Arcs[r.Walk[n-1].Arc].To}
	}
	return r
}

// vs is positions as the vertices of an arc or the nodes of a graph: no time.
func vs(ps ...vnx.Position) []vnx.Vertex {
	out := make([]vnx.Vertex, len(ps))
	for i, p := range ps {
		out[i] = vnx.Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele}
	}
	return out
}

// vx is one position as a vertex.
func vx(p vnx.Position) vnx.Vertex { return vs(p)[0] }

// brokenWalks are hand-built documents whose walk or arc does not resolve. A
// reader drops each of them, so only a caller filling in a Document by hand
// reaches a writer with one, and the writer fails rather than drawing a guess.
func brokenWalks() map[string]*vnx.Document {
	nodes := func() map[string]vnx.Vertex {
		return map[string]vnx.Vertex{"a": {Lon: 0, Lat: 0}, "b": {Lon: 1, Lat: 0}, "c": {Lon: 2, Lat: 0}}
	}
	arcs := func() map[string]vnx.Arc {
		return map[string]vnx.Arc{"ab": {From: "a", To: "b"}, "bc": {From: "b", To: "c"}}
	}
	chain := func(walk ...vnx.Pass) *vnx.Document {
		return &vnx.Document{Networks: []vnx.Network{{Name: "Net", Nodes: nodes(), Arcs: arcs(),
			Chains: []vnx.Chain{{Name: "Broken", Walk: walk}}}}}
	}
	at := func(n string) *vnx.Endpoint { return &vnx.Endpoint{Node: n} }
	return map[string]*vnx.Document{
		"a chain naming an absent arc":                  chain(vnx.Pass{Arc: "ghost", From: at("a"), To: at("b")}),
		"a chain entering at a node that is not an end": chain(vnx.Pass{Arc: "bc", From: at("a"), To: at("c")}),
		"a chain that does not join":                    chain(vnx.Pass{Arc: "ab", From: at("b")}, vnx.Pass{Arc: "bc", To: at("c")}),
		"a route that does not join": {Routes: []vnx.Route{{Name: "Broken", Nodes: nodes(), Arcs: arcs(),
			Walk: []vnx.Pass{{Arc: "ab", From: at("b")}, {Arc: "bc", To: at("c")}}}}},
		"an arc naming an absent node": {Networks: []vnx.Network{{Name: "Net", Nodes: nodes(),
			Arcs: map[string]vnx.Arc{"ax": {From: "a", To: "x"}}}}},
	}
}
