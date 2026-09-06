package vnx

import "fmt"

// PointStyle is a point's appearance. Seven fields, matching the model's
// PointStyle one for one — an earlier draft of the example carried three and
// spelled two of them as words.
//
// ⚠ LabelVisible is a TRISTATE and its absence is meaningful. Internally it is
// *int: nil means Auto (the renderer decides from importance and zoom), 1 is
// forced visible, 0 forced hidden. A reader that reads absence as false
// silently flattens Auto to hidden — a silent default, and exactly the failure
// this format's rules exist to prevent.
type PointStyle struct {
	Color         string
	Icon          string
	IconSize      *int // points; the model's domain is 16 / 21 / 27 / 35 / 46
	PinShape      string
	LabelPosition string
	LabelVisible  *bool // nil = Auto
	Importance    *int  // 1..5
}

// AreaStyle is an area's fill and stroke.
type AreaStyle struct {
	FillColor   string
	FillOpacity *float64
	StrokeColor string
	StrokeWidth *float64
}

// Waypoint is a top-level point; Point is the in-network one. They share a
// shape, so they share a type.
type Waypoint struct {
	Folder string
	Name   string
	Notes  string
	Hidden bool
	At     Position
	Style  PointStyle
	Data   []Record
}

// Area is a polygon. ⚠ ONE CLOSED RING — no holes, no multi-ring. The model is
// a single ring of at least four positions with first == last, validated at
// every write site, so the format must not promise more than that.
type Area struct {
	Folder  string
	Name    string
	Notes   string
	Hidden  bool
	Polygon []Position
	Style   AreaStyle
	Data    []Record
}

var (
	waypointKeys   = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "at": true, "style": true, "data": true}
	areaKeys       = map[string]bool{"folder": true, "name": true, "notes": true, "hidden": true, "polygon": true, "style": true, "data": true}
	pointStyleKeys = map[string]bool{"color": true, "icon": true, "icon-size": true, "pin-shape": true, "label-position": true, "label-visible": true, "importance": true}
	areaStyleKeys  = map[string]bool{"fill-color": true, "fill-opacity": true, "stroke-color": true, "stroke-width": true}
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
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/at",
				Message: "missing or malformed `at` — item dropped"})
			continue
		}
		pos, err := decodePosition(posArr, at+"/at")
		if err != nil {
			// A point IS its position; without one there is nothing to keep.
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/at",
				Message: err.Error() + " — item dropped"})
			continue
		}
		var w Waypoint
		w.At = pos
		w.Folder, diags = stringField(obj, "folder", diags)
		w.Name, diags = stringField(obj, "name", diags)
		w.Notes, diags = stringField(obj, "notes", diags)
		w.Hidden, diags = boolField(obj, "hidden", at, diags)
		w.Style, diags = decodePointStyle(obj, at, diags)
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
		a.Style, diags = decodeAreaStyle(obj, at, diags)
		a.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, areaKeys, diags)
		out = append(out, a)
	}
	return out, diags
}

// decodeRing reads the single closed ring an Area's polygon is. Nested rings —
// the GeoJSON shape an earlier draft of the example promised — are reported and
// the area dropped, never silently flattened to the outer ring and never
// silently merged. A reader that quietly kept ring 1 would draw an area whose
// holes had vanished, with nothing to say so.
func decodeRing(v any, path string, diags []Diagnostic) ([]Position, bool, []Diagnostic) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("polygon is %s, expected an array of positions — area dropped", jsonKind(v))})
	}
	if len(arr) > 0 {
		if first, isArr := arr[0].([]any); isArr && len(first) > 0 {
			if _, nested := first[0].([]any); nested {
				return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
					Message: "polygon holds nested rings; this format carries ONE closed ring and cannot express holes — area dropped"})
			}
		}
	}
	if len(arr) < 4 {
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("polygon has %d positions, needs at least 4 (3 distinct plus the closing one) — area dropped", len(arr))})
	}
	ring := make([]Position, 0, len(arr))
	for i, item := range arr {
		posArr, isArr := item.([]any)
		if !isArr {
			return nil, false, append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Message: fmt.Sprintf("ring position is %s — area dropped", jsonKind(item))})
		}
		p, err := decodePosition(posArr, fmt.Sprintf("%s/%d", path, i))
		if err != nil {
			return nil, false, append(diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Message: err.Error() + " — area dropped"})
		}
		ring = append(ring, p)
	}
	if ring[0].Lon != ring[len(ring)-1].Lon || ring[0].Lat != ring[len(ring)-1].Lat {
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: "polygon is not closed: the last position must repeat the first — area dropped"})
	}
	return ring, true, diags
}

func decodePointStyle(obj map[string]any, path string, diags []Diagnostic) (PointStyle, []Diagnostic) {
	var s PointStyle
	st, ok := obj["style"].(map[string]any)
	if !ok {
		return s, diags
	}
	at := path + "/style"
	s.Color, diags = stringField(st, "color", diags)
	s.Icon, diags = stringField(st, "icon", diags)
	s.PinShape, diags = stringField(st, "pin-shape", diags)
	s.LabelPosition, diags = stringField(st, "label-position", diags)
	s.IconSize, diags = intField(st, "icon-size", at, diags)
	s.Importance, diags = intField(st, "importance", at, diags)
	if v, present := st["label-visible"]; present {
		if b, isBool := v.(bool); isBool {
			s.LabelVisible = &b
		} else {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/label-visible",
				Message: fmt.Sprintf("expected a boolean, found %s — left as Auto", jsonKind(v))})
		}
	}
	diags = reportUnknownKeys(st, at, pointStyleKeys, diags)
	return s, diags
}

func decodeAreaStyle(obj map[string]any, path string, diags []Diagnostic) (AreaStyle, []Diagnostic) {
	var s AreaStyle
	st, ok := obj["style"].(map[string]any)
	if !ok {
		return s, diags
	}
	at := path + "/style"
	s.FillColor, diags = stringField(st, "fill-color", diags)
	s.StrokeColor, diags = stringField(st, "stroke-color", diags)
	s.FillOpacity, diags = floatField(st, "fill-opacity", at, diags)
	s.StrokeWidth, diags = floatField(st, "stroke-width", at, diags)
	diags = reportUnknownKeys(st, at, areaStyleKeys, diags)
	return s, diags
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
