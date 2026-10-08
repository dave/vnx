package vnx

import (
	"fmt"
	"sort"
)

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
	Nodes      map[string]Vertex
	Arcs       map[string]Arc
	Highlights []Highlight
	Points     []Waypoint
	Areas      []Area
	Data       []Record
}

// Highlight is a walk over arcs the network owns. Unlike a Plan, its geometry is
// BY REFERENCE — two highlights crossing the same trail name the same arc, and
// that sharing is the whole reason this format exists.
type Highlight struct {
	Name   string
	Notes  string
	Hidden bool
	Style  Style
	Walk   []Pass
	Data   []Record
}

var (
	networkKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true,
		"nodes": true, "arcs": true, "highlights": true, "points": true, "areas": true, "data": true}
	highlightKeys = map[string]bool{"name": true, "notes": true, "hidden": true, "style": true, "walk": true, "data": true}
	arcKeys       = map[string]bool{"from": true, "to": true, "pips": true, "data": true}
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
		n.Arcs, diags = decodeArcs(obj["arcs"], at+"/arcs", n.Nodes, diags)
		n.Highlights, diags = decodeHighlights(obj["highlights"], at+"/highlights", n.Arcs, diags)
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

// decodeNodes reads a network's or a plan's `nodes`. A node that will not
// parse is dropped (§6.2); an arc naming it is then dropped under
// arc-node-absent, so the loss surfaces on everything that depended on it.
func decodeNodes(v any, path string, diags []Diagnostic) (map[string]Vertex, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`nodes` is %s, expected an object — ignored", jsonKind(v))})
	}
	nodes := make(map[string]Vertex, len(obj))
	for _, id := range sortedKeys(obj) {
		at := path + "/" + escapePointerToken(id)
		arr, isArr := obj[id].([]any)
		if !isArr {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable,
				Message: fmt.Sprintf("node is %s, expected a position array — node dropped", jsonKind(obj[id]))})
			continue
		}
		p, err := decodePosition(arr, at)
		if err != nil {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable,
				Message: err.Error() + " — node dropped"})
			continue
		}
		if p.Time != nil {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeExtraChannel,
				Message: "a node carries no time — the time channel is dropped and the node kept"})
		}
		nodes[id] = Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele}
	}
	return nodes, diags
}

// decodeArcs reads a network's or a plan's `arcs`.
//
// ⚠ AN ARC THAT IS DROPPED TAKES EVERY HIGHLIGHT WALKING IT WITH IT, but not here:
// the highlight is dropped when its walk is read, under pass-arc-absent, with a
// diagnostic of its own (§6.2). That is why nodes and arcs are read before any
// walk — a walk can only be checked against the arcs that survived.
func decodeArcs(v any, path string, nodes map[string]Vertex, diags []Diagnostic) (map[string]Arc, []Diagnostic) {
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
		at := path + "/" + escapePointerToken(id)
		aObj, isObj := obj[id].(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc is %s, expected an object — dropped", jsonKind(obj[id]))})
			continue
		}
		diags = reportUnknownKeys(aObj, at, arcKeys, diags)
		a, ok, diags2 := decodeArc(aObj, at, nodes, diags)
		diags = diags2
		if ok {
			arcs[id] = a
		}
	}
	return arcs, diags
}

func decodeArc(obj map[string]any, at string, nodes map[string]Vertex, diags []Diagnostic) (Arc, bool, []Diagnostic) {
	var a Arc
	var ok bool
	for _, end := range []struct {
		key string
		dst *string
	}{{"from", &a.From}, {"to", &a.To}} {
		*end.dst, ok = obj[end.key].(string)
		if !ok {
			what := "is missing"
			if _, present := obj[end.key]; present {
				what = fmt.Sprintf("is %s, expected a node key", jsonKind(obj[end.key]))
			}
			return Arc{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/" + end.key,
				Code: CodeWrongTypeRequired, Message: fmt.Sprintf("arc `%s` %s — arc dropped", end.key, what)})
		}
	}
	pipsArr, isArr := obj["pips"].([]any)
	if !isArr {
		what := "is missing"
		if _, present := obj["pips"]; present {
			what = fmt.Sprintf("is %s, expected an array", jsonKind(obj["pips"]))
		}
		return Arc{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/pips",
			Code: CodeWrongTypeRequired, Message: fmt.Sprintf("arc `pips` %s — arc dropped", what)})
	}
	pips, diags := decodePips(pipsArr, at+"/pips", diags)
	if pips == nil {
		// decodePips reported the unusable position and returned nil to say
		// "drop the arc".
		return Arc{}, false, diags
	}
	a.Pips = make([]Vertex, len(pips))
	timed := false
	for i, p := range pips {
		timed = timed || p.Time != nil
		a.Pips[i] = Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele}
	}
	if timed {
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
		c.Name, diags = stringField(obj, "name", diags)
		c.Notes, diags = stringField(obj, "notes", diags)
		c.Hidden, diags = boolField(obj, "hidden", at, diags)
		c.Style, diags = decodeStyle(obj, at, diags)
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
