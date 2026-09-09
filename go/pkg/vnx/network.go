package vnx

import (
	"fmt"
	"math"
	"sort"
)

// EndpointToleranceMetres is how far an inline chain endpoint may sit from the
// arc it names before the reader gives up on projecting it.
//
// PROVISIONAL. Spec §11 lists the tolerance as open and this value closes it by
// implementation, which the plan permits and requires writing back. A real
// export projects onto the polyline and lands sub-metre; the slack is for
// hand-authored and third-party files, whose coordinates are typed rather than
// computed. Too tight and legitimate files are refused; too loose and a
// genuinely misplaced endpoint snaps somewhere wrong and nothing says so.
const EndpointToleranceMetres = 50.0

// Network is the graph: nodes and arcs that chains traverse.
//
// ⚠ Nodes and arcs are MAPS because chains reference them by key, and those
// keys are scoped TO THIS NETWORK — a route elsewhere in the document may use
// the same names for entirely different geometry. A reader that flattens every
// id into one document-wide map silently merges unrelated things.
type Network struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
	Nodes  map[string]Position
	Arcs   map[string]Arc
	Chains []Chain
	Points []Waypoint
	Areas  []Area
	Data   []Record
}

// Chain is an ordered traversal of arcs the network owns. Unlike a Route, its
// geometry is BY REFERENCE — two chains crossing the same trail name the same
// arc, and that sharing is the whole reason this format exists.
type Chain struct {
	Name   string
	Notes  string
	Hidden bool
	Style  Style
	Links  []Link
	Data   []Record
}

// Link is one arc traversal between two typed endpoints. Direction is the
// endpoint order — there is no stored flag to drift out of step with it.
type Link struct {
	Arc  string
	From Endpoint
	To   Endpoint
}

// Endpoint is either a node key or an inline position — a handle: a point ON
// the named arc where the chain starts, ends or turns around.
//
// ⚠ Inline positions carry NO IDENTITY. Two links in one chain may share a
// handle (a turnaround pair), and the only way a reader can tell is that the
// coordinates are equal. Hence Same below, and hence the writer's obligation to
// emit byte-identical arrays for a shared endpoint.
type Endpoint struct {
	Node string    // "" when inline
	At   *Position // nil when a node key
}

// Same reports whether two endpoints denote the same point. Node keys compare
// as strings; inline positions compare BY COORDINATE VALUE, because that is all
// the identity they have.
func (e Endpoint) Same(f Endpoint) bool {
	if e.Node != "" || f.Node != "" {
		return e.Node == f.Node
	}
	if e.At == nil || f.At == nil {
		return false
	}
	return e.At.Lon == f.At.Lon && e.At.Lat == f.At.Lat
}

func (e Endpoint) String() string {
	if e.Node != "" {
		return e.Node
	}
	if e.At != nil {
		return e.At.String()
	}
	return "<none>"
}

var (
	networkKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true,
		"nodes": true, "arcs": true, "chains": true, "points": true, "areas": true, "data": true}
	chainKeys = map[string]bool{"name": true, "notes": true, "hidden": true, "style": true, "links": true, "data": true}
	linkKeys  = map[string]bool{"arc": true, "from": true, "to": true}
)

func decodeNetworks(arr []any, path string, diags []Diagnostic) ([]Network, []Diagnostic) {
	out := make([]Network, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("network is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var n Network
		n.Folder, diags = stringField(obj, "folder", diags)
		n.Name, diags = stringField(obj, "name", diags)
		n.Notes, diags = stringField(obj, "notes", diags)
		n.Hidden, diags = boolField(obj, "hidden", at, diags)
		n.Nodes, diags = decodeNodes(obj["nodes"], at+"/nodes", diags)
		n.Arcs, diags = decodeNamedArcs(obj["arcs"], at+"/arcs", n.Nodes, diags)
		n.Chains, diags = decodeChains(obj["chains"], at+"/chains", n.Nodes, n.Arcs, diags)
		if sub, ok := obj["points"].([]any); ok {
			n.Points, diags = decodeWaypoints(sub, at+"/points", diags)
		}
		if sub, ok := obj["areas"].([]any); ok {
			n.Areas, diags = decodeAreas(sub, at+"/areas", diags)
		}
		n.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, networkKeys, diags)
		out = append(out, n)
	}
	return out, diags
}

func decodeNodes(v any, path string, diags []Diagnostic) (map[string]Position, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`nodes` is %s, expected an object — ignored", jsonKind(v))})
	}
	nodes := make(map[string]Position, len(obj))
	for _, id := range sortedKeys(obj) {
		at := path + "/" + id
		arr, isArr := obj[id].([]any)
		if !isArr {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("node is %s, expected a position array — dropped", jsonKind(obj[id]))})
			continue
		}
		p, err := decodePosition(arr, at)
		if err != nil {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: err.Error() + " — node dropped"})
			continue
		}
		nodes[id] = p
	}
	return nodes, diags
}

func decodeNamedArcs(v any, path string, nodes map[string]Position, diags []Diagnostic) (map[string]Arc, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`arcs` is %s, expected an object — ignored", jsonKind(v))})
	}
	arcs := make(map[string]Arc, len(obj))
	for _, id := range sortedKeys(obj) {
		at := path + "/" + id
		aObj, isObj := obj[id].(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc is %s, expected an object — dropped", jsonKind(obj[id]))})
			continue
		}
		var a Arc
		a.Pips, diags = decodePips(aObj["pips"], at+"/pips", diags)
		a.Data, diags = dataField(aObj, at, diags)
		from, _ := aObj["from"].(string)
		to, _ := aObj["to"].(string)
		diags = reportUnknownKeys(aObj, at, arcKeys, diags)
		if len(a.Pips) < 2 {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc has %d usable pips, needs at least 2 — dropped", len(a.Pips))})
			continue
		}
		diags = checkArcMeetsNodes(a, from, to, at, nodes, diags)
		a.From, a.To = from, to
		arcs[id] = a
	}
	return arcs, diags
}

// checkArcMeetsNodes enforces the whole-polyline rule at the reading end.
//
// An arc's pips are its COMPLETE point list, so its first pip must be its start
// node and its last its end node. Internally the blob holds only INTERIOR pips
// and the reader prepends the nodes; migration 071 records what happens when
// the two conventions are confused — "loses or invents the two endpoints
// without erroring". Checking it here is what stops that arriving silently.
func checkArcMeetsNodes(a Arc, from, to, at string, nodes map[string]Position, diags []Diagnostic) []Diagnostic {
	check := func(nodeID string, p Position, which string) {
		if nodeID == "" {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc has no `%s` node", which)})
			return
		}
		np, ok := nodes[nodeID]
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc names %s node %q, which is not in this network", which, nodeID)})
			return
		}
		if metresBetween(np, p) > EndpointToleranceMetres {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc's %s pip %s is %.0f m from node %q at %s — an arc's pips are its COMPLETE point list and must meet its nodes",
					which, p, metresBetween(np, p), nodeID, np)})
		}
	}
	check(from, a.Pips[0], "from")
	check(to, a.Pips[len(a.Pips)-1], "to")
	return diags
}

func decodeChains(v any, path string, nodes map[string]Position, arcs map[string]Arc, diags []Diagnostic) ([]Chain, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`chains` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Chain, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("chain is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var c Chain
		c.Name, diags = stringField(obj, "name", diags)
		c.Notes, diags = stringField(obj, "notes", diags)
		c.Hidden, diags = boolField(obj, "hidden", at, diags)
		c.Style, diags = decodeStyle(obj, at, diags)
		c.Data, diags = dataField(obj, at, diags)
		c.Links, diags = decodeLinks(obj["links"], at+"/links", nodes, arcs, diags)
		diags = reportUnknownKeys(obj, at, chainKeys, diags)
		out = append(out, c)
	}
	return out, diags
}

func decodeLinks(v any, path string, nodes map[string]Position, arcs map[string]Arc, diags []Diagnostic) ([]Link, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`links` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Link, 0, len(arr))
	kept := make([]int, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("link is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		arcID, _ := obj["arc"].(string)
		arc, known := arcs[arcID]
		if !known {
			// The link's whole meaning is the arc it traverses. Without it there
			// is no geometry to draw and nothing to keep.
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Code: CodeLinkArcAbsent, Message: fmt.Sprintf("link names arc %q, which is not in this network — dropped", arcID)})
			continue
		}
		from, okFrom, diags2 := decodeEndpoint(obj["from"], at+"/from", nodes, arc, diags)
		diags = diags2
		to, okTo, diags3 := decodeEndpoint(obj["to"], at+"/to", nodes, arc, diags)
		diags = diags3
		diags = reportUnknownKeys(obj, at, linkKeys, diags)
		if !okFrom || !okTo {
			continue
		}
		out = append(out, Link{Arc: arcID, From: from, To: to})
		kept = append(kept, i)
	}
	return checkChainContinuity(out, kept, path, diags)
}

// checkChainContinuity reports where consecutive links do not meet.
//
// This is what gives Endpoint.Same a caller, and without it the shared-handle
// rule is decoration: a turnaround pair would "pass" any test simply because
// nothing ever compared its endpoints. A method with no call site is not a
// guarantee, however carefully it is written.
//
// The links are KEPT when they do not meet — the geometry is real, and a gap
// the writer intended is not ours to delete. Reporting lets the reader decide.
//
// ⚠ A DROPPED LINK DOES NOT MAKE ITS NEIGHBOURS ADJACENT. `kept` carries each
// surviving link's index in the DOCUMENT, and two survivors are compared only
// when those indices are consecutive. Comparing across a hole turns one defect
// into two: a document with a dangling arc reference reports the drop AND a
// spurious break, and a reader cannot tell how much is actually wrong. Spec
// §3.3.3 states the rule.
//
// The same slice fixes a second defect it hid. The diagnostic's path used the
// SURVIVING index, so once anything had been dropped the path named the wrong
// link — pointing a reader, very often an agent, at a link that is fine.
func checkChainContinuity(links []Link, kept []int, path string, diags []Diagnostic) ([]Link, []Diagnostic) {
	for i := 1; i < len(links); i++ {
		if kept[i] != kept[i-1]+1 {
			continue
		}
		if !links[i-1].To.Same(links[i].From) {
			diags = append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, kept[i]),
				Code: CodeChainDiscontinuity, Message: fmt.Sprintf("this link starts at %s but the previous one ends at %s — the chain is not one connected walk",
					links[i].From, links[i-1].To)})
		}
	}
	return links, diags
}

// decodeEndpoint reads a node key or an inline position, and checks an inline
// one actually lies on the arc it belongs to. A position nowhere near its arc
// cannot be projected onto it, so it is an unusable value — the link goes, and
// says so, rather than snapping to whatever happens to be nearest.
func decodeEndpoint(v any, path string, nodes map[string]Position, arc Arc, diags []Diagnostic) (Endpoint, bool, []Diagnostic) {
	switch t := v.(type) {
	case string:
		if _, ok := nodes[t]; !ok {
			return Endpoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: path,
				Message: fmt.Sprintf("endpoint names node %q, which is not in this network — link dropped", t)})
		}
		return Endpoint{Node: t}, true, diags
	case []any:
		p, err := decodePosition(t, path)
		if err != nil {
			return Endpoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: path,
				Message: err.Error() + " — link dropped"})
		}
		if d := distanceToArc(p, arc); d > EndpointToleranceMetres {
			return Endpoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: path,
				Code: CodeEndpointOffArc, Message: fmt.Sprintf("inline endpoint %s is %.0f m from its arc, beyond the %.0f m tolerance — link dropped", p, d, EndpointToleranceMetres)})
		}
		return Endpoint{At: &p}, true, diags
	default:
		return Endpoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("endpoint is %s, expected a node id or a position — link dropped", jsonKind(v))})
	}
}

// distanceToArc is the shortest distance from p to the arc's polyline.
func distanceToArc(p Position, arc Arc) float64 {
	best := math.Inf(1)
	for i := 0; i+1 < len(arc.Pips); i++ {
		if d := distanceToSegment(p, arc.Pips[i], arc.Pips[i+1]); d < best {
			best = d
		}
	}
	return best
}

// distanceToSegment is how far p lies from the segment ab, in metres. It is
// the distance half of polyline.go's nearestOnSegment — the tolerance check
// here and the cut the writers take must measure the same geometry, so they
// share the projection rather than each carrying a copy of it.
func distanceToSegment(p, a, b Position) float64 {
	_, metres := nearestOnSegment(p, a, b)
	return metres
}

func metresBetween(a, b Position) float64 {
	return distanceToSegment(a, b, b)
}

// sortedKeys keeps map iteration deterministic, so diagnostics come out in a
// stable order rather than Go's randomised one.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
