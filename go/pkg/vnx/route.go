package vnx

import "fmt"

// Arc is a run of geometry. On a top-level route it is anonymous and owned
// outright; inside a network it is named and shared (see F8).
//
// ⚠ Pips are the COMPLETE point list, first point to last, INCLUDING both
// endpoint positions. This differs from internal storage, where an
// arc.polyline blob holds only interior pips and the reader prepends and
// appends the node positions — a difference migration 071 records as silent
// when got wrong: "a reader that treats one as the other loses or invents the
// two endpoints without erroring". The format carries whole polylines so that
// no reader ever prepends anything.
type Arc struct {
	Pips []Position
	Data []Record

	// From and To name the nodes a NETWORK arc runs between. They are the
	// arc's identity in the graph: two chains crossing the same trail name the
	// same arc, and the arc names the same two nodes.
	//
	// ⚠ THEY ARE EMPTY ON A ROUTE'S ARCS, and that is the difference between
	// the two owners rather than an omission. A route owns its arcs outright,
	// shares them with nothing, and needs no names for them — node identity
	// there is simply the shared endpoint present in both neighbouring pip
	// lists.
	//
	// An earlier version of this struct had neither field, and decodeNamedArcs
	// read `from` and `to` only to check the arc met its nodes and then threw
	// them away. Nothing failed: the endpoint check still worked, and the loss
	// only surfaced when something tried to WRITE a decoded network back out
	// and had no nodes to name.
	From string
	To   string
}

// Route is a top-level chain: an ORDERED ARRAY OF ARCS, with no nodes map, no
// links, and no arc ids.
//
// A route OWNS its arcs — node and arc own-alternate between a network and a
// single chain, so nothing else can ever reference them. Nothing shared means
// nothing needs naming, and three things the database models explicitly fall
// out for free:
//
//   - node identity is the shared endpoint between arc k and arc k+1, present
//     in both pip lists;
//   - direction is inherent in pip order;
//   - parallel arcs between the same pair of points — legitimate and expected
//     from GPX import — need no disambiguation at all.
//
// Arc BOUNDARIES still matter and the array preserves them, so a route that is
// part hand-drawn and part engine-routed survives with each arc's own routing
// record intact.
type Route struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
	Style  Style
	Arcs   []Arc
	Data   []Record
}

var (
	routeKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "style": true, "arcs": true, "data": true}
	arcKeys   = map[string]bool{"pips": true, "data": true, "from": true, "to": true}
)

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
		r.Arcs, diags = decodeArcList(obj["arcs"], at+"/arcs", diags)
		r.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, routeKeys, diags)
		diags = checkArcsJoin(r.Arcs, at+"/arcs", diags)
		out = append(out, r)
	}
	return out, diags
}

func decodeArcList(v any, path string, diags []Diagnostic) ([]Arc, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`arcs` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Arc, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var a Arc
		a.Pips, diags = decodePips(obj["pips"], at+"/pips", diags)
		a.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, arcKeys, diags)
		if len(a.Pips) < 2 {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("arc has %d usable pips, needs at least 2 to be geometry — dropped", len(a.Pips))})
			continue
		}
		out = append(out, a)
	}
	return out, diags
}

// checkArcsJoin reports where consecutive arcs do not share an endpoint.
//
// The arcs are KEPT. The geometry is real and drawable, and a gap the writer
// intended is not ours to delete — reporting it lets a reader decide, which is
// the whole point of the diagnostics channel. Comparison is by VALUE, since
// inline positions carry no identity.
func checkArcsJoin(arcs []Arc, path string, diags []Diagnostic) []Diagnostic {
	for i := 1; i < len(arcs); i++ {
		prev := arcs[i-1].Pips[len(arcs[i-1].Pips)-1]
		next := arcs[i].Pips[0]
		if prev.Lon != next.Lon || prev.Lat != next.Lat {
			diags = append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Message: fmt.Sprintf("this arc starts at %s but the previous one ends at %s — the route is not one connected walk", next, prev)})
		}
	}
	return diags
}
