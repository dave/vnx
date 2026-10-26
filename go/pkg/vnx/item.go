package vnx

import "fmt"

// Waypoint is a top-level point; Point is the in-network one. They share a
// shape, so they share a type.
type Waypoint struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
	Pinned Timestamp // "" is not pinned (§3.8)
	At     Position
	Style  Style
	Data   []Record
}

// Area is a polygon. ⚠ ONE OPEN RING — no holes, no multi-ring (SPEC §3.7).
// Polygon holds at least three distinct positions and its last does NOT
// repeat its first: the boundary closes from the last back to the first.
// Formats that need a closed ring (GeoJSON, KML) close it when they write it.
type Area struct {
	Folder  string
	Name    string
	Notes   string
	Hidden  bool
	Pinned  Timestamp // "" is not pinned (§3.8)
	Polygon []Position
	Style   Style
	Data    []Record
}

var (
	waypointKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "pinned": true, "at": true, "style": true, "data": true}
	areaKeys     = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "pinned": true, "polygon": true, "style": true, "data": true}
)

// decodeWaypoints reads a network's `points`, which arrive as a generic tree.
func decodeWaypoints(arr []any, path string, diags []Diagnostic) ([]Waypoint, []Diagnostic) {
	out := make([]Waypoint, 0, len(arr))
	for i, item := range arr {
		w, ok, d := waypointFromValue(item, fmt.Sprintf("%s/%d", path, i), diags)
		diags = d
		if ok {
			out = append(out, w)
		}
	}
	return out, diags
}

// waypointFromValue reads one waypoint or network point. ok is false when it
// is dropped.
func waypointFromValue(item any, at string, diags []Diagnostic) (Waypoint, bool, []Diagnostic) {
	obj, ok := item.(map[string]any)
	if !ok {
		return Waypoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("item is %s, expected an object — dropped", jsonKind(item))})
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
		return Waypoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/at",
			Code:    CodeWrongTypeRequired,
			Message: fmt.Sprintf("`at` %s — item dropped", what)})
	}
	pos, err := decodePosition(posArr, at+"/at")
	if err != nil {
		// A point IS its position; without one there is nothing to keep.
		return Waypoint{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/at", Code: CodePositionUnusable,
			Message: err.Error() + " — item dropped"})
	}
	var w Waypoint
	w.At = pos
	w.Folder, diags = stringField(obj, "folder", diags)
	w.Name, diags = stringField(obj, "name", diags)
	w.Notes, diags = stringField(obj, "notes", diags)
	w.Hidden, diags = boolField(obj, "hidden", at, diags)
	w.Pinned, diags = timeField(obj, "pinned", at, diags)
	w.Style, diags = decodeStyle(obj, at, diags)
	w.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, waypointKeys, diags)
	return w, true, diags
}

// areaFromObject builds one area from its object, read with every key but
// `polygon`, which rr holds. ok is false when the area is dropped.
func areaFromObject(obj map[string]any, at string, rr *ringRead, diags []Diagnostic) (Area, bool, []Diagnostic) {
	diags = append(diags, rr.diags...)
	if !rr.ok {
		return Area{}, false, diags
	}
	var a Area
	a.Polygon = rr.ring
	a.Folder, diags = stringField(obj, "folder", diags)
	a.Name, diags = stringField(obj, "name", diags)
	a.Notes, diags = stringField(obj, "notes", diags)
	a.Hidden, diags = boolField(obj, "hidden", at, diags)
	a.Pinned, diags = timeField(obj, "pinned", at, diags)
	a.Style, diags = decodeStyle(obj, at, diags)
	a.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, areaKeys, diags)
	return a, true, diags
}

// distinctLonLat counts the positions of ps that differ in longitude or
// latitude; no other channel takes part (SPEC §3.7).
func distinctLonLat(ps []Position) int {
	seen := make(map[[2]float64]bool, len(ps))
	for _, p := range ps {
		seen[[2]float64{p.Lon, p.Lat}] = true
	}
	return len(seen)
}

func intField(obj map[string]any, key, path string, diags []Diagnostic) (*int, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return nil, diags
	}
	f, ok := v.(float64)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Message: fmt.Sprintf("expected a number, found %s — ignored", jsonKind(v))})
	}
	n := int(f)
	if float64(n) != f {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Message: fmt.Sprintf("%v is not a whole number — ignored", f)})
	}
	return &n, diags
}

func floatField(obj map[string]any, key, path string, diags []Diagnostic) (*float64, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return nil, diags
	}
	f, ok := v.(float64)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Message: fmt.Sprintf("expected a number, found %s — ignored", jsonKind(v))})
	}
	return &f, diags
}
