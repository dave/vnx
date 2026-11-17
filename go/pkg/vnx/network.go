package vnx

import "fmt"

// Network is the graph: nodes and arcs that highlights walk.
//
// ⚠ Nodes and arcs are MAPS because walks reference them by key, and those
// keys are scoped TO THIS NETWORK — a plan elsewhere in the document may use
// the same names for entirely different geometry. A reader that flattens every
// id into one document-wide map silently merges unrelated things.
type Network struct {
	Folder     string
	Name       string
	Notes      string
	Hidden     bool
	Pinned     Timestamp // "" is not pinned (§3.8)
	Nodes      map[string]Vertex
	Arcs       map[string]Arc
	Highlights []Highlight
	Points     []Point
	Areas      []Area
	Data       []Record
}

// Highlight is a lightweight container for data objects along a walk of its
// network's arcs (§3.3.3). It has an optional Name, "" for none, and no notes
// of its own: what it is, beyond its name, is what its Data says.
// Unlike a Plan, its geometry is BY REFERENCE — two highlights crossing the
// same trail name the same arc, and that sharing is the whole reason this
// format exists.
type Highlight struct {
	Name   string
	Hidden bool
	Pinned Timestamp // "" is not pinned (§3.8)
	Walk   []Pass
	Data   []Record
}

var (
	networkKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "pinned": true,
		"nodes": true, "arcs": true, "highlights": true, "points": true, "areas": true, "data": true}
	highlightKeys = map[string]bool{"name": true, "hidden": true, "pinned": true, "walk": true, "data": true}
	arcKeys       = map[string]bool{"from": true, "to": true, "pips": true, "data": true}
)

// networkFromObject builds one network from its object, read with every key
// but `nodes`, `arcs` and `areas`, which g and areas hold.
func networkFromObject(obj map[string]any, at string, g *graphRead, areas *areasRead, diags []Diagnostic) (Network, []Diagnostic) {
	var n Network
	n.Folder, diags = stringField(obj, "folder", at, diags)
	n.Name, diags = stringField(obj, "name", at, diags)
	n.Notes, diags = stringField(obj, "notes", at, diags)
	n.Hidden, diags = boolField(obj, "hidden", at, diags)
	n.Pinned, diags = timeField(obj, "pinned", at, diags)
	n.Nodes, n.Arcs, diags = g.finish(at, diags)
	n.Highlights, diags = decodeHighlights(obj["highlights"], at+"/highlights", n.Arcs, diags)
	if sub, ok := obj["points"].([]any); ok {
		n.Points, diags = decodePoints(sub, at+"/points", diags)
	}
	// A network's `areas` that is not an array is ignored without a word, as
	// its `points` is.
	if areas.isArray {
		n.Areas, diags = areas.items, append(diags, areas.diags...)
	}
	n.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, networkKeys, diags)
	return n, diags
}

// nodeFromValue reads one node of a network's or a plan's `nodes`. A node that
// will not parse is dropped (§6.2); an arc naming it is then dropped under
// arc-node-absent, so the loss surfaces on everything that depended on it.
func nodeFromValue(v any, at string, diags []Diagnostic) (Vertex, bool, []Diagnostic) {
	arr, isArr := v.([]any)
	if !isArr {
		return Vertex{}, false, append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable,
			Message: fmt.Sprintf("node is %s, expected a position array — node dropped", jsonKind(v))})
	}
	p, err := decodePosition(arr, at)
	if err != nil {
		return Vertex{}, false, append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable,
			Message: err.Error() + " — node dropped"})
	}
	if p.Time != nil {
		diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeExtraChannel,
			Message: "a node carries no time — the time channel is dropped and the node kept"})
	}
	return Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele}, true, diags
}

// arcFromObject builds one arc from its object, read with every key but
// `pips`, which pr holds.
//
// ⚠ AN ARC THAT IS DROPPED TAKES EVERY HIGHLIGHT WALKING IT WITH IT, but not here:
// the highlight is dropped when its walk is read, under pass-arc-absent, with a
// diagnostic of its own (§6.2). That is why nodes and arcs are read before any
// walk — a walk can only be checked against the arcs that survived.
func arcFromObject(obj map[string]any, at string, nodes map[string]Vertex, pr *pipsRead, diags []Diagnostic) (Arc, bool, []Diagnostic) {
	var a Arc
	var ok bool
	for _, end := range []struct {
		key string
		dst *string
	}{{"from", &a.From}, {"to", &a.To}} {
		*end.dst, ok = obj[end.key].(string)
		if !ok {
			what, where := "is missing", at
			if _, present := obj[end.key]; present {
				what, where = fmt.Sprintf("is %s, expected a node key", jsonKind(obj[end.key])), at+"/"+end.key
			}
			return Arc{}, false, append(diags, Diagnostic{Severity: Warning, Path: where,
				Code: CodeWrongTypeRequired, Message: fmt.Sprintf("arc `%s` %s — arc dropped", end.key, what)})
		}
	}
	if !pr.isArray {
		what, where := "is missing", at
		if pr.present {
			what, where = fmt.Sprintf("is %s, expected an array", pr.kind), at+"/pips"
		}
		return Arc{}, false, append(diags, Diagnostic{Severity: Warning, Path: where,
			Code: CodeWrongTypeRequired, Message: fmt.Sprintf("arc `pips` %s — arc dropped", what)})
	}
	diags = append(diags, pr.diags...)
	if pr.failed {
		// The pips reader reported the unusable position; it drops the arc.
		return Arc{}, false, diags
	}
	a.Pips = pr.vertices
	if pr.timed {
		diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/pips", Code: CodeArcPipTime,
			Message: "an arc pip carries no time — every time channel on this arc is dropped and its pips kept"})
	}
	a.Data, diags = dataField(obj, at, diags)
	if a.From == a.To {
		return Arc{}, false, append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeSelfLoopArc,
			Message: fmt.Sprintf("arc runs from node %q to itself; a self-loop cannot be walked — arc dropped", a.From)})
	}
	dropped := false
	for _, end := range []struct{ key, node string }{{"from", a.From}, {"to", a.To}} {
		if _, known := nodes[end.node]; !known {
			dropped = true
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/" + end.key, Code: CodeArcNodeAbsent,
				Message: fmt.Sprintf("arc names %s node %q, which is not here — arc dropped", end.key, end.node)})
		}
	}
	if dropped {
		return Arc{}, false, diags
	}
	return a, true, diags
}

func decodeHighlights(v any, path string, arcs map[string]Arc, diags []Diagnostic) ([]Highlight, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`highlights` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Highlight, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("highlight is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var c Highlight
		c.Name, diags = stringField(obj, "name", at, diags)
		c.Hidden, diags = boolField(obj, "hidden", at, diags)
		c.Pinned, diags = timeField(obj, "pinned", at, diags)
		c.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, highlightKeys, diags)
		var walkOK bool
		c.Walk, walkOK, diags = decodeWalk(obj, at, arcs, false, diags)
		if !walkOK {
			continue
		}
		out = append(out, c)
	}
	return out, diags
}
