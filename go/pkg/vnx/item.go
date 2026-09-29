package vnx

import "fmt"

// Waypoint is a top-level point; Point is the in-network one. They share a
// shape, so they share a type.
type Waypoint struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
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
	Polygon []Position
	Style   Style
	Data    []Record
}

var (
	waypointKeys = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "at": true, "style": true, "data": true}
	areaKeys     = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "polygon": true, "style": true, "data": true}
)

func decodeWaypoints(arr []any, path string, diags []Diagnostic) ([]Waypoint, []Diagnostic) {
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

func decodeAreas(arr []any, path string, diags []Diagnostic) ([]Area, []Diagnostic) {
	out := make([]Area, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("item is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		ring, ok, diags2 := decodeRing(obj["polygon"], at+"/polygon", diags)
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

// decodeRing reads the single OPEN ring an Area's polygon is (SPEC §3.7).
// Nested rings — the GeoJSON shape an earlier draft of the example promised —
// are reported and the area dropped, never silently flattened to the outer
// ring and never silently merged. A reader that quietly kept ring 1 would draw
// an area whose holes had vanished, with nothing to say so.
//
// ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED. Its last position repeating its
// first is one fact written twice, so dropping the repeat loses nothing; the
// diagnostic is kept so that a writer that closes rings can be seen. The count
// of distinct positions is taken after that, so [A, B, A] is two points and
// its area goes.
func decodeRing(v any, path string, diags []Diagnostic) ([]Position, bool, []Diagnostic) {
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
