package vnx

// Plan is a top-level item that OWNS its graph (SPEC §3.4): nodes and arcs of
// a network's shape, and one walk over them.
//
// Nothing outside the plan can reference its nodes or arcs, and their keys
// are scoped to it. The walk runs node to node — no position, no turn, no pin
// — because a plan's ends are its own nodes and nothing else holds a handle
// on them.
//
// ⚠ ONE SHAPE FOR BOTH OWNERS IS THE POINT. A plan that carried an ordered
// array of pip lists could not say that it walks an arc backwards, so a
// reversed arc was written reversed and read back as a different arc. With
// the network's shape a plan's arcs keep their direction and the walk says
// which way it goes, and one reader and one writer serve both.
type Plan struct {
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

var planKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "style": true,
	"nodes": true, "arcs": true, "walk": true, "data": true}

// planFromObject builds one plan from its object, read with every key but
// `nodes` and `arcs`, which g holds. ok is false when the plan is dropped.
func planFromObject(obj map[string]any, at string, g *graphRead, diags []Diagnostic) (Plan, bool, []Diagnostic) {
	var r Plan
	r.Folder, diags = stringField(obj, "folder", diags)
	r.Name, diags = stringField(obj, "name", diags)
	r.Notes, diags = stringField(obj, "notes", diags)
	r.Hidden, diags = boolField(obj, "hidden", at, diags)
	r.Style, diags = decodeStyle(obj, at, diags)
	r.Nodes, r.Arcs, diags = g.finish(at, diags)
	r.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, planKeys, diags)
	// Where a highlight would be dropped, the plan is: it IS the highlight.
	var walkOK bool
	r.Walk, walkOK, diags = decodeWalk(obj, at, r.Arcs, true, diags)
	return r, walkOK, diags
}
