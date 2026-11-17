package vnx

import "fmt"

// Point is a point: a top-level one in Document.Points, or an in-network one
// in Network.Points. The two share a shape (§3.6), so they share a type.
type Point struct {
	Folder string
	Name   string
	Hidden bool
	Pinned Timestamp // "" is not pinned (§3.8)
	At     Position
	Data   []Record
}

// Area is a polygon. ⚠ ONE OPEN RING — no holes, no multi-ring (SPEC §3.7).
// Polygon holds at least three distinct positions and its last does NOT
// repeat its first: the boundary closes from the last back to the first.
// Formats that need a closed ring (GeoJSON, KML) close it when they write it.
type Area struct {
	Folder  string
	Name    string
	Hidden  bool
	Pinned  Timestamp // "" is not pinned (§3.8)
	Polygon []Position
	Data    []Record
}

var (
	pointKeys = map[string]bool{"folder": true, "name": true, "hidden": true, "pinned": true, "at": true, "data": true}
	areaKeys  = map[string]bool{"folder": true, "name": true, "hidden": true, "pinned": true, "polygon": true, "data": true}
	// A network's points and areas sit in their network's folder and carry no
	// `folder` of their own (§3.10), so on them `folder` is an unrecognised
	// key.
	networkPointKeys = withoutKey(pointKeys, "folder")
	networkAreaKeys  = withoutKey(areaKeys, "folder")
)

// withoutKey is a copy of keys that lacks key.
func withoutKey(keys map[string]bool, key string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for k, v := range keys {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// decodePoints reads a network's `points`, which arrive as a generic tree.
func decodePoints(arr []any, path string, diags []Diagnostic) ([]Point, []Diagnostic) {
	out := make([]Point, 0, len(arr))
	for i, item := range arr {
		w, ok, d := pointFromValue(item, fmt.Sprintf("%s/%d", path, i), true, diags)
		diags = d
		if ok {
			out = append(out, w)
		}
	}
	return out, diags
}

// pointFromValue reads one point, top-level or in a network (inNetwork). ok is
// false when it is dropped.
func pointFromValue(item any, at string, inNetwork bool, diags []Diagnostic) (Point, bool, []Diagnostic) {
	obj, ok := item.(map[string]any)
	if !ok {
		return Point{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("item is %s, expected an object — dropped", jsonKind(item))})
	}
	posArr, ok := obj["at"].([]any)
	if !ok {
		// ⚠ `at` MISSING AND `at` WRONG-TYPED TAKE ONE REMEDY AND ARE NOT ONE
		// FACT. §3.10 makes `at` required, so both drop the item, but a
		// reader told "missing or malformed" learns neither. Distinguished
		// here; the code is the same because the outcome is.
		// §6.3 rule 1: an absent key has no element of its own, so the point
		// is named; a present one of the wrong type is named at the key.
		what, where := "is missing", at
		if _, present := obj["at"]; present {
			what, where = fmt.Sprintf("is %s, expected a position array", jsonKind(obj["at"])), at+"/at"
		}
		return Point{}, false, append(diags, Diagnostic{Severity: Warning, Path: where,
			Code:    CodeWrongTypeRequired,
			Message: fmt.Sprintf("`at` %s — item dropped", what)})
	}
	pos, err := decodePosition(posArr, at+"/at")
	if err != nil {
		// A point IS its position; without one there is nothing to keep.
		return Point{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/at", Code: CodePositionUnusable,
			Message: err.Error() + " — item dropped"})
	}
	var w Point
	w.At = pos
	keys := pointKeys
	if inNetwork {
		keys = networkPointKeys
	} else {
		w.Folder, diags = stringField(obj, "folder", at, diags)
	}
	w.Name, diags = stringField(obj, "name", at, diags)
	w.Hidden, diags = boolField(obj, "hidden", at, diags)
	w.Pinned, diags = timeField(obj, "pinned", at, diags)
	w.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, keys, diags)
	return w, true, diags
}

// areaFromObject builds one area from its object, read with every key but
// `polygon`, which rr holds. inNetwork says whether it is a network's area. ok
// is false when the area is dropped.
func areaFromObject(obj map[string]any, at string, rr *ringRead, inNetwork bool, diags []Diagnostic) (Area, bool, []Diagnostic) {
	diags = append(diags, rr.diags...)
	if !rr.ok {
		return Area{}, false, diags
	}
	var a Area
	a.Polygon = rr.ring
	keys := areaKeys
	if inNetwork {
		keys = networkAreaKeys
	} else {
		a.Folder, diags = stringField(obj, "folder", at, diags)
	}
	a.Name, diags = stringField(obj, "name", at, diags)
	a.Hidden, diags = boolField(obj, "hidden", at, diags)
	a.Pinned, diags = timeField(obj, "pinned", at, diags)
	a.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, keys, diags)
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
