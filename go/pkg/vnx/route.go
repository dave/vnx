package vnx

import "fmt"

// Route is a top-level item that OWNS its graph (SPEC §3.4): nodes and arcs of
// a network's shape, and one walk over them.
//
// Nothing outside the route can reference its nodes or arcs, and their keys
// are scoped to it. The walk runs node to node — no position, no turn, no pin
// — because a route's ends are its own nodes and nothing else holds a handle
// on them.
//
// ⚠ ONE SHAPE FOR BOTH OWNERS IS THE POINT. A route that carried an ordered
// array of pip lists could not say that it walks an arc backwards, so a
// reversed arc was written reversed and read back as a different arc. With
// the network's shape a route's arcs keep their direction and the walk says
// which way it goes, and one reader and one writer serve both.
type Route struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
	Style  Style
	Nodes  map[string]Vertex
	Arcs   map[string]Arc
	Walk   []Pass
	Data   []Record
}

var routeKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "style": true,
	"nodes": true, "arcs": true, "walk": true, "data": true}

func decodeRoutes(arr []any, path string, diags []Diagnostic) ([]Route, []Diagnostic) {
	out := make([]Route, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("route is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var r Route
		r.Folder, diags = stringField(obj, "folder", diags)
		r.Name, diags = stringField(obj, "name", diags)
		r.Notes, diags = stringField(obj, "notes", diags)
		r.Hidden, diags = boolField(obj, "hidden", at, diags)
		r.Style, diags = decodeStyle(obj, at, diags)
		r.Nodes, diags = decodeNodes(obj["nodes"], at+"/nodes", diags)
		r.Arcs, diags = decodeArcs(obj["arcs"], at+"/arcs", r.Nodes, diags)
		r.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, routeKeys, diags)
		// Where a chain would be dropped, the route is: it IS the chain.
		var walkOK bool
		r.Walk, walkOK, diags = decodeWalk(obj, at, r.Arcs, true, diags)
		if !walkOK {
			continue
		}
		out = append(out, r)
	}
	return out, diags
}
