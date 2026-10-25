package vnx

// The tree decoder: the reader this package had before DecodeReader, kept
// verbatim as the oracle that TestDecodeReaderMatchesTreeDecode compares the
// streaming reader against. It unmarshals the whole document into a generic
// JSON tree first. Only the functions the streaming reader replaced are copied
// here; the helpers both readers share stay in the package.

import (
	"encoding/json"
	"fmt"
	"sort"
)

func decodeTree(data []byte) (*Document, []Diagnostic, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrMalformed, err)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: top level is %s, not an object", ErrNotVNX, jsonKind(raw))
	}

	declared, ok := obj["vnx"].(string)
	if !ok {
		if v, present := obj["vnx"]; present {
			return nil, nil, fmt.Errorf("%w: `vnx` is %s, not a string", ErrNotVNX, jsonKind(v))
		}
		return nil, nil, fmt.Errorf("%w: no `vnx` key", ErrNotVNX)
	}
	if err := checkVersion(declared); err != nil {
		return nil, nil, err
	}

	var diags []Diagnostic
	doc := &Document{Version: declared}
	doc.Generator, diags = stringField(obj, "generator", diags)
	doc.Created, diags = timeField(obj, "created", "", diags)
	if v, present := obj["folders"]; present {
		if arr, ok := v.([]any); ok {
			doc.Folders, diags = treeDecodeFolders(arr, diags)
		} else {
			diags = append(diags, Diagnostic{Severity: Warning, Path: "/folders",
				Message: fmt.Sprintf("`folders` is %s, expected an array — ignored", jsonKind(v))})
		}
	}
	if arr, ok := treeCollection(obj, "waypoints", &diags); ok {
		doc.Waypoints, diags = treeDecodeWaypoints(arr, "/waypoints", diags)
	}
	if arr, ok := treeCollection(obj, "networks", &diags); ok {
		doc.Networks, diags = treeDecodeNetworks(arr, "/networks", diags)
	}
	if arr, ok := treeCollection(obj, "plans", &diags); ok {
		doc.Plans, diags = treeDecodePlans(arr, "/plans", diags)
	}
	if arr, ok := treeCollection(obj, "tracks", &diags); ok {
		doc.Tracks, diags = treeDecodeTracks(arr, "/tracks", diags)
	}
	if arr, ok := treeCollection(obj, "areas", &diags); ok {
		doc.Areas, diags = treeDecodeAreas(arr, "/areas", diags)
	}
	doc.AppKeys = decodeAppKeys(obj)
	diags = reportUnknownKeysBy(obj, "", isDocumentKey, diags)
	return doc, diags, nil
}

func treeCollection(obj map[string]any, key string, diags *[]Diagnostic) ([]any, bool) {
	v, present := obj[key]
	if !present {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Severity: Warning, Path: "/" + key,
			Message: fmt.Sprintf("`%s` is %s, expected an array — ignored", key, jsonKind(v))})
		return nil, false
	}
	return arr, true
}

func treeDecodeFolders(arr []any, diags []Diagnostic) ([]Folder, []Diagnostic) {
	out := make([]Folder, 0, len(arr))
	seen := make(map[string]bool, len(arr))

	for i, item := range arr {
		at := fmt.Sprintf("/folders/%d", i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("folder is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		id, _ := obj["id"].(string)
		if id == "" {
			// Without an id nothing can reference it, so the folder cannot hold
			// anything. Unlike a dangling parent, dropping it loses nothing that
			// was reachable.
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: "folder has no `id` — dropped, nothing could reference it"})
			continue
		}
		if seen[id] {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("duplicate folder id %q — dropped, the first one wins", id)})
			continue
		}
		seen[id] = true

		var f Folder
		f.ID = id
		f.Name, diags = stringField(obj, "name", diags)
		f.Notes, diags = stringField(obj, "notes", diags)
		f.Parent, diags = stringField(obj, "parent", diags)
		f.Hidden, diags = boolField(obj, "hidden", at, diags)
		f.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, folderKeys, diags)
		out = append(out, f)
	}

	return resolveParents(out, diags)
}

func treeDecodeWaypoints(arr []any, path string, diags []Diagnostic) ([]Waypoint, []Diagnostic) {
	out := make([]Waypoint, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("item is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		posArr, ok := obj["at"].([]any)
		if !ok {
			// ⚠ `at` MISSING AND `at` WRONG-TYPED TAKE ONE REMEDY AND ARE NOT ONE
			// FACT. §3.10 makes `at` required, so both drop the item, but a
			// reader told "missing or malformed" learns neither. Distinguished
			// here; the code is the same because the outcome is.
			what := "is missing"
			if _, present := obj["at"]; present {
				what = fmt.Sprintf("is %s, expected a position array", jsonKind(obj["at"]))
			}
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/at",
				Code:    CodeWrongTypeRequired,
				Message: fmt.Sprintf("`at` %s — item dropped", what)})
			continue
		}
		pos, err := decodePosition(posArr, at+"/at")
		if err != nil {
			// A point IS its position; without one there is nothing to keep.
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/at", Code: CodePositionUnusable,
				Message: err.Error() + " — item dropped"})
			continue
		}
		var w Waypoint
		w.At = pos
		w.Folder, diags = stringField(obj, "folder", diags)
		w.Name, diags = stringField(obj, "name", diags)
		w.Notes, diags = stringField(obj, "notes", diags)
		w.Hidden, diags = boolField(obj, "hidden", at, diags)
		w.Style, diags = decodeStyle(obj, at, diags)
		w.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, waypointKeys, diags)
		out = append(out, w)
	}
	return out, diags
}

func treeDecodeAreas(arr []any, path string, diags []Diagnostic) ([]Area, []Diagnostic) {
	out := make([]Area, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("item is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		ring, ok, diags2 := treeDecodeRing(obj["polygon"], at+"/polygon", diags)
		diags = diags2
		if !ok {
			continue
		}
		var a Area
		a.Polygon = ring
		a.Folder, diags = stringField(obj, "folder", diags)
		a.Name, diags = stringField(obj, "name", diags)
		a.Notes, diags = stringField(obj, "notes", diags)
		a.Hidden, diags = boolField(obj, "hidden", at, diags)
		a.Style, diags = decodeStyle(obj, at, diags)
		a.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, areaKeys, diags)
		out = append(out, a)
	}
	return out, diags
}

func treeDecodeRing(v any, path string, diags []Diagnostic) ([]Position, bool, []Diagnostic) {
	arr, ok := v.([]any)
	if !ok {
		what := "is missing"
		if v != nil {
			what = fmt.Sprintf("is %s, expected an array of positions", jsonKind(v))
		}
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path, Code: CodeWrongTypeRequired,
			Message: fmt.Sprintf("polygon %s — area dropped", what)})
	}
	if len(arr) > 0 {
		if first, isArr := arr[0].([]any); isArr && len(first) > 0 {
			if _, nested := first[0].([]any); nested {
				return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
					Code: CodePolygonNestedRings, Message: "polygon holds nested rings; this format carries ONE ring and cannot express holes — area dropped"})
			}
		}
	}
	ring := make([]Position, 0, len(arr))
	for i, item := range arr {
		posArr, isArr := item.([]any)
		if !isArr {
			return nil, false, append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Code:    CodePositionUnusable,
				Message: fmt.Sprintf("ring position is %s — area dropped", jsonKind(item))})
		}
		p, err := decodePosition(posArr, fmt.Sprintf("%s/%d", path, i))
		if err != nil {
			return nil, false, append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Code: CodePositionUnusable, Message: err.Error() + " — area dropped"})
		}
		ring = append(ring, p)
	}
	if n := len(ring); n >= 2 && ring[0].Lon == ring[n-1].Lon && ring[0].Lat == ring[n-1].Lat {
		diags = append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, n-1), Code: CodeRingClosed,
			Message: "a ring is stored open, and this last position repeats the first — the repeat is dropped"})
		ring = ring[:n-1]
	}
	if d := distinctLonLat(ring); d < 3 {
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path, Code: CodeRingTooFewPoints,
			Message: fmt.Sprintf("ring has %d distinct position(s), needs at least 3 — area dropped", d)})
	}
	return ring, true, diags
}

func treeDecodeTracks(arr []any, path string, diags []Diagnostic) ([]Track, []Diagnostic) {
	out := make([]Track, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("track is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var tr Track
		tr.Folder, diags = stringField(obj, "folder", diags)
		tr.Name, diags = stringField(obj, "name", diags)
		tr.Notes, diags = stringField(obj, "notes", diags)
		tr.Hidden, diags = boolField(obj, "hidden", at, diags)
		tr.Started, diags = timeField(obj, "started", at, diags)
		tr.Ended, diags = timeField(obj, "ended", at, diags)
		tr.Style, diags = decodeStyle(obj, at, diags)
		tr.Segments, diags = treeDecodeSegments(obj["segments"], at+"/segments", diags)
		tr.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, trackKeys, diags)
		out = append(out, tr)
	}
	return out, diags
}

func treeDecodeSegments(v any, path string, diags []Diagnostic) ([]Segment, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`segments` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Segment, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("segment is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		diags = reportUnknownKeys(obj, at, segmentKeys, diags)
		pips, diags2 := treeDecodePips(obj["pips"], at+"/pips", diags)
		diags = diags2
		if pips == nil && obj["pips"] != nil {
			// decodePips reported an unusable position and returned nil to say
			// "drop the owner". For a track that owner is the SEGMENT: spec
			// §3.5 already makes the interval between two segments a pause
			// rather than geometry, so a missing segment leaves a gap a
			// consumer can see, where a missing pip inside one leaves a
			// shorter, plausible, wrong line.
			continue
		}
		out = append(out, Segment{Pips: pips})
	}
	return out, diags
}

func treeDecodePips(v any, path string, diags []Diagnostic) ([]Position, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`pips` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Position, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		posArr, isArr := item.([]any)
		if !isArr {
			return nil, append(diags, Diagnostic{Severity: Warning, Path: at,
				Code:    CodePositionUnusable,
				Message: fmt.Sprintf("pip is %s, expected an array", jsonKind(item))})
		}
		p, err := decodePosition(posArr, at)
		if err != nil {
			return nil, append(diags, Diagnostic{Severity: Warning, Path: at,
				Code: CodePositionUnusable, Message: err.Error()})
		}
		out = append(out, p)
	}
	return out, diags
}

func treeDecodePlans(arr []any, path string, diags []Diagnostic) ([]Plan, []Diagnostic) {
	out := make([]Plan, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("plan is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var r Plan
		r.Folder, diags = stringField(obj, "folder", diags)
		r.Name, diags = stringField(obj, "name", diags)
		r.Notes, diags = stringField(obj, "notes", diags)
		r.Hidden, diags = boolField(obj, "hidden", at, diags)
		r.Style, diags = decodeStyle(obj, at, diags)
		r.Nodes, diags = treeDecodeNodes(obj["nodes"], at+"/nodes", diags)
		r.Arcs, diags = treeDecodeArcs(obj["arcs"], at+"/arcs", r.Nodes, diags)
		r.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, planKeys, diags)
		// Where a highlight would be dropped, the plan is: it IS the highlight.
		var walkOK bool
		r.Walk, walkOK, diags = decodeWalk(obj, at, r.Arcs, true, diags)
		if !walkOK {
			continue
		}
		out = append(out, r)
	}
	return out, diags
}

func treeDecodeNetworks(arr []any, path string, diags []Diagnostic) ([]Network, []Diagnostic) {
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
		n.Nodes, diags = treeDecodeNodes(obj["nodes"], at+"/nodes", diags)
		n.Arcs, diags = treeDecodeArcs(obj["arcs"], at+"/arcs", n.Nodes, diags)
		n.Highlights, diags = decodeHighlights(obj["highlights"], at+"/highlights", n.Arcs, diags)
		if sub, ok := obj["points"].([]any); ok {
			n.Points, diags = treeDecodeWaypoints(sub, at+"/points", diags)
		}
		if sub, ok := obj["areas"].([]any); ok {
			n.Areas, diags = treeDecodeAreas(sub, at+"/areas", diags)
		}
		n.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, networkKeys, diags)
		out = append(out, n)
	}
	return out, diags
}

func treeDecodeNodes(v any, path string, diags []Diagnostic) (map[string]Vertex, []Diagnostic) {
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

func treeDecodeArcs(v any, path string, nodes map[string]Vertex, diags []Diagnostic) (map[string]Arc, []Diagnostic) {
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
		a, ok, diags2 := treeDecodeArc(aObj, at, nodes, diags)
		diags = diags2
		if ok {
			arcs[id] = a
		}
	}
	return arcs, diags
}

func treeDecodeArc(obj map[string]any, at string, nodes map[string]Vertex, diags []Diagnostic) (Arc, bool, []Diagnostic) {
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
	pips, diags := treeDecodePips(pipsArr, at+"/pips", diags)
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
