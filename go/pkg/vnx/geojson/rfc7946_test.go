package geojson

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// The RFC 7946 validator.
//
// ⚠ IT EXISTS BECAUSE THERE IS NO GeoJSON READER IN THIS REPO to round-trip
// against (spec §6.2). GPX and KML get the strongest test available — write,
// re-import, compare geometry — and GeoJSON cannot have it. Schema validation
// is the substitute, so it has to be REAL: a check that the output "looks like
// GeoJSON" would pass on a file with null ordinates, open rings and clockwise
// polygons, which are exactly the three things most likely to be wrong here
// and invisible.
//
// ⚠ THE VALIDATOR IS ITSELF TESTED (TestValidatorRejects…). An instrument that
// answers fluently is worse than no instrument: a validator with a typo in a
// member name would pass every document forever and read exactly like a clean
// suite.

// validate reports every way b departs from RFC 7946, plus the two rules spec
// 5b adds on top (§2.3's properties, and no foreign members anywhere).
func validate(b []byte) []string {
	var v validator
	var root any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&root); err != nil {
		return []string{fmt.Sprintf("$: not JSON: %v", err)}
	}
	if dec.More() {
		v.bad("$", "trailing content after the document")
	}
	v.collection("$", root)
	return v.problems
}

type validator struct{ problems []string }

func (v *validator) bad(path, format string, args ...any) {
	v.problems = append(v.problems, path+": "+fmt.Sprintf(format, args...))
}

// members checks an object holds EXACTLY the named keys. RFC 7946 §6.1 permits
// foreign members; this writer emits none, and §2.3 is why — an invented
// member is an <extensions> block by another name.
func (v *validator) members(path string, obj map[string]any, want ...string) {
	allowed := map[string]bool{}
	for _, w := range want {
		allowed[w] = true
		if _, ok := obj[w]; !ok {
			v.bad(path, "missing member %q", w)
		}
	}
	for k := range obj {
		if !allowed[k] {
			v.bad(path, "foreign member %q (RFC 7946 permits it; spec 5b §2.3 does not)", k)
		}
	}
}

func (v *validator) object(path string, x any) (map[string]any, bool) {
	obj, ok := x.(map[string]any)
	if !ok {
		v.bad(path, "expected an object, found %T", x)
	}
	return obj, ok
}

func (v *validator) array(path string, x any) ([]any, bool) {
	arr, ok := x.([]any)
	if !ok {
		v.bad(path, "expected an array, found %T", x)
	}
	return arr, ok
}

func (v *validator) collection(path string, x any) {
	obj, ok := v.object(path, x)
	if !ok {
		return
	}
	v.members(path, obj, "type", "features")
	if obj["type"] != "FeatureCollection" {
		v.bad(path, "type is %v, expected \"FeatureCollection\"", obj["type"])
	}
	arr, ok := v.array(path+".features", obj["features"])
	if !ok {
		return
	}
	for i, f := range arr {
		v.feature(fmt.Sprintf("%s.features[%d]", path, i), f)
	}
}

func (v *validator) feature(path string, x any) {
	obj, ok := v.object(path, x)
	if !ok {
		return
	}
	v.members(path, obj, "type", "properties", "geometry")
	if obj["type"] != "Feature" {
		v.bad(path, "type is %v, expected \"Feature\"", obj["type"])
	}
	if props, ok := v.object(path+".properties", obj["properties"]); ok {
		for k, val := range props {
			// §2.3: name and description ONLY. Not tags, not style, not hidden.
			if k != "name" && k != "description" {
				v.bad(path+".properties", "property %q is neither name nor description (spec 5b §2.3)", k)
			}
			if _, isString := val.(string); !isString {
				v.bad(path+".properties."+k, "expected a string, found %T", val)
			}
		}
	}
	v.geometry(path+".geometry", obj["geometry"], false)
}

func (v *validator) geometry(path string, x any, nested bool) {
	obj, ok := v.object(path, x)
	if !ok {
		return
	}
	kind, _ := obj["type"].(string)
	if kind == "GeometryCollection" {
		if nested {
			// RFC 7946 §3.1.8: a GeometryCollection must not contain another.
			v.bad(path, "nested GeometryCollection")
		}
		v.members(path, obj, "type", "geometries")
		arr, ok := v.array(path+".geometries", obj["geometries"])
		if !ok {
			return
		}
		for i, g := range arr {
			v.geometry(fmt.Sprintf("%s.geometries[%d]", path, i), g, true)
		}
		return
	}
	v.members(path, obj, "type", "coordinates")
	c := obj["coordinates"]
	cp := path + ".coordinates"
	switch kind {
	case "Point":
		v.position(cp, c)
	case "MultiPoint":
		v.each(cp, c, 0, func(p string, e any) { v.position(p, e) })
	case "LineString":
		v.line(cp, c)
	case "MultiLineString":
		v.each(cp, c, 1, func(p string, e any) { v.line(p, e) })
	case "Polygon":
		v.rings(cp, c)
	case "MultiPolygon":
		v.each(cp, c, 1, func(p string, e any) { v.rings(p, e) })
	default:
		v.bad(path, "type is %v, which is not one of RFC 7946's seven geometry types", obj["type"])
	}
}

func (v *validator) each(path string, x any, min int, f func(string, any)) {
	arr, ok := v.array(path, x)
	if !ok {
		return
	}
	if len(arr) < min {
		v.bad(path, "holds %d elements, needs at least %d", len(arr), min)
	}
	for i, e := range arr {
		f(fmt.Sprintf("%s[%d]", path, i), e)
	}
}

// line is RFC 7946 §3.1.4: two or more positions.
func (v *validator) line(path string, x any) {
	arr, ok := v.array(path, x)
	if !ok {
		return
	}
	if len(arr) < 2 {
		v.bad(path, "LineString has %d position(s), RFC 7946 §3.1.4 needs 2 or more", len(arr))
	}
	for i, p := range arr {
		v.position(fmt.Sprintf("%s[%d]", path, i), p)
	}
}

// rings is RFC 7946 §3.1.6: each ring closed, four or more positions, the
// exterior counterclockwise and any interior clockwise.
func (v *validator) rings(path string, x any) {
	arr, ok := v.array(path, x)
	if !ok {
		return
	}
	if len(arr) == 0 {
		v.bad(path, "Polygon has no rings")
	}
	for i, r := range arr {
		rp := fmt.Sprintf("%s[%d]", path, i)
		ring, ok := v.array(rp, r)
		if !ok {
			continue
		}
		if len(ring) < 4 {
			v.bad(rp, "linear ring has %d position(s), RFC 7946 §3.1.6 needs 4 or more", len(ring))
		}
		coords := make([][]float64, 0, len(ring))
		for j, p := range ring {
			coords = append(coords, v.position(fmt.Sprintf("%s[%d]", rp, j), p))
		}
		if len(ring) >= 2 {
			first, last := coords[0], coords[len(coords)-1]
			if len(first) < 2 || len(last) < 2 || first[0] != last[0] || first[1] != last[1] {
				v.bad(rp, "linear ring is not closed: the last position must repeat the first")
			}
		}
		var area float64
		for j := 0; j+1 < len(coords); j++ {
			if len(coords[j]) < 2 || len(coords[j+1]) < 2 {
				area = math.NaN()
				break
			}
			area += coords[j][0]*coords[j+1][1] - coords[j+1][0]*coords[j][1]
		}
		if math.IsNaN(area) {
			continue
		}
		// A degenerate ring has no winding to check; a real one must obey the
		// right-hand rule.
		if area < 0 && i == 0 {
			v.bad(rp, "exterior ring is clockwise; RFC 7946 §3.1.6 requires the right-hand rule (counterclockwise)")
		}
		if area > 0 && i > 0 {
			v.bad(rp, "interior ring is counterclockwise; RFC 7946 §3.1.6 requires clockwise")
		}
	}
}

// position is RFC 7946 §3.1.1: two or three numbers, lon then lat then
// optional elevation. NOT a null third ordinate — the RFC does not allow one,
// and it is the shape a writer reaches for when elevation is unknown.
func (v *validator) position(path string, x any) []float64 {
	arr, ok := v.array(path, x)
	if !ok {
		return nil
	}
	if len(arr) < 2 || len(arr) > 3 {
		v.bad(path, "position has %d elements, RFC 7946 §3.1.1 allows 2 or 3", len(arr))
	}
	out := make([]float64, 0, len(arr))
	for i, e := range arr {
		n, isNumber := e.(float64)
		if !isNumber {
			if e == nil {
				v.bad(path, "ordinate %d is null; RFC 7946 has no null ordinate — a position is 2 or 3 numbers", i)
			} else {
				v.bad(path, "ordinate %d is %T, expected a number", i, e)
			}
			return out
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			v.bad(path, "ordinate %d is not finite", i)
		}
		out = append(out, n)
	}
	if len(out) >= 2 {
		if out[0] < -180 || out[0] > 180 {
			v.bad(path, "longitude %v is off the globe", out[0])
		}
		if out[1] < -90 || out[1] > 90 {
			v.bad(path, "latitude %v is off the globe", out[1])
		}
	}
	return out
}

func TestFixtureOutputIsValidRFC7946(t *testing.T) {
	var b strings.Builder
	if _, err := Write(&b, fixture()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if problems := validate([]byte(b.String())); len(problems) > 0 {
		t.Fatalf("the fixture export is not valid RFC 7946:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestValidatorRejects is the validator's own red-proof. Every case is a real
// GeoJSON defect this writer could plausibly ship, and each must be CAUGHT —
// otherwise the test above is an instrument that answers fluently and the
// whole acceptance rests on nothing.
func TestValidatorRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"null third ordinate",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1,2,null]}}]}`,
			"null"},
		{"clockwise exterior ring",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[1,0],[0,0]]]}}]}`,
			"clockwise"},
		{"unclosed ring",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1]]]}}]}`,
			"not closed"},
		{"short ring",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}}]}`,
			"needs 4 or more"},
		{"one-position LineString",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[0,0]]}}]}`,
			"needs 2 or more"},
		{"four-element position",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1,2,3,4]}}]}`,
			"allows 2 or 3"},
		{"foreign property",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tags":"a"},"geometry":{"type":"Point","coordinates":[1,2]}}]}`,
			"§2.3"},
		{"foreign member on the feature",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1,2]},"folder":"x"}]}`,
			"foreign member"},
		{"nested GeometryCollection",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"GeometryCollection","geometries":[{"type":"GeometryCollection","geometries":[]}]}}]}`,
			"nested"},
		{"unknown geometry type",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Circle","coordinates":[1,2]}}]}`,
			"seven geometry types"},
		{"longitude off the globe",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[181,2]}}]}`,
			"off the globe"},
		{"latitude off the globe",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1,91]}}]}`,
			"off the globe"},
		{"non-string property value",
			`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":7},"geometry":{"type":"Point","coordinates":[1,2]}}]}`,
			"expected a string"},
		{"wrong root type",
			`{"type":"Feature","features":[]}`,
			"FeatureCollection"},
		{"not JSON at all",
			`{"type":`,
			"not JSON"},
		{"trailing content",
			`{"type":"FeatureCollection","features":[]} {}`,
			"trailing content"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := validate([]byte(c.doc))
			if len(problems) == 0 {
				t.Fatalf("the validator accepted an invalid document; it cannot be trusted to accept a valid one.\ndocument: %s", c.doc)
			}
			if !strings.Contains(strings.Join(problems, "\n"), c.want) {
				t.Fatalf("caught something, but not the defect under test (%q):\n  %s", c.want, strings.Join(problems, "\n  "))
			}
		})
	}
}

// TestValidatorAcceptsAValidDocument stops the test above passing because the
// validator rejects everything.
func TestValidatorAcceptsAValidDocument(t *testing.T) {
	const good = `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","properties":{"name":"a","description":"b"},"geometry":{"type":"Point","coordinates":[1,2,3]}},` +
		`{"type":"Feature","properties":{},"geometry":{"type":"MultiLineString","coordinates":[[[0,0],[1,1]]]}},` +
		`{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}},` +
		`{"type":"Feature","properties":{},"geometry":{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,2]}]}}` +
		`]}`
	if problems := validate([]byte(good)); len(problems) > 0 {
		t.Fatalf("the validator rejected a valid document:\n  %s", strings.Join(problems, "\n  "))
	}
}
