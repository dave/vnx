package vnx_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
	"github.com/dave/vnx/go/pkg/vnx/geojson"
	"github.com/dave/vnx/go/pkg/vnx/kml"
)

// A chain's INLINE ENDPOINT is a point ON an arc part-way along it — a handle
// where the chain starts, ends or turns around (network.go). It is almost never
// one of the arc's recorded pips, so a writer has to decide what to emit for it.
//
// ⚠ KML AND GEOJSON GAVE TWO DIFFERENT ANSWERS UNTIL 2026-09-04. KML cut the
// segment and interpolated the exact point; GeoJSON snapped to the nearest pip,
// which moves the end of the line by up to half a segment and gives the file a
// length the chain does not have. One library, one chain, two polylines. Both
// now call vnx.SubPolyline, and this file is the guard that says so.
//
// ⚠ NO GOLDEN COULD SEE THIS. Every writer fixture in the tree puts its chain
// endpoints on nodes or exactly on pips, where snapping and interpolation agree,
// so the two writers disagreed for as long as they did without reddening
// anything — a-golden-cannot-witness-what-its-fixture-never-carries. The
// document below exists to carry the one case they differ on.

// interpolatedEndpointDoc is one arc of three pips running due north, and one
// chain that walks it from its first node to a handle three quarters of the way
// along the SECOND segment.
//
//	pips:   57.000 ---- 57.002 ---- 57.004
//	handle:                    57.0035 ^
//
// The nearest pip to the handle is 57.004, so snapping and interpolation give
// visibly different lines and the test can name which one it got.
func interpolatedEndpointDoc() *vnx.Document {
	ele := func(v float64) *float64 { return &v }
	pips := []vnx.Position{
		{Lon: -3.5, Lat: 57.0, Ele: ele(300)},
		{Lon: -3.5, Lat: 57.002, Ele: ele(320)},
		{Lon: -3.5, Lat: 57.004, Ele: ele(340)},
	}
	handle := vnx.Position{Lon: -3.5, Lat: 57.0035}
	return &vnx.Document{Networks: []vnx.Network{{
		Name: "Net",
		Nodes: map[string]vnx.Position{
			"n1": {Lon: -3.5, Lat: 57.0},
			"n2": {Lon: -3.5, Lat: 57.004},
		},
		Arcs: map[string]vnx.Arc{"a": {From: "n1", To: "n2", Pips: pips}},
		Chains: []vnx.Chain{{Name: "Chain", Links: []vnx.Link{
			{Arc: "a", From: vnx.Endpoint{Node: "n1"}, To: vnx.Endpoint{At: &handle}},
		}}},
	}}}
}

// wantInterpolatedChain is the line the chain actually traverses: both pips it
// passes, then the cut point itself. The cut carries NO ELEVATION — we know
// where the walker turned round, not how high it was there, and a derived
// number would be indistinguishable downstream from a measured one (kml's `at`
// has always said so; GeoJSON now agrees).
var wantInterpolatedChain = []string{"-3.5,57,300", "-3.5,57.002,320", "-3.5,57.0035"}

// snappedChainLastPoint is what the OLD GeoJSON emitted for that endpoint: the
// nearest recorded pip, 0.0005° — about 55 m — further north than the walker
// went, carrying an elevation that belongs to a place the chain never reached.
const snappedChainLastPoint = "-3.5,57.004,340"

func TestKMLAndGeoJSONCutAChainAtTheSamePlace(t *testing.T) {
	doc := interpolatedEndpointDoc()

	got := map[string][]string{
		"geojson": geojsonChainCoords(t, doc),
		"kml":     kmlChainCoords(t, doc),
	}
	for _, name := range []string{"geojson", "kml"} {
		c := got[name]
		if strings.Join(c, " ") != strings.Join(wantInterpolatedChain, " ") {
			t.Errorf("%s chain line = %v, want %v", name, c, wantInterpolatedChain)
		}
		// Say the failure in the terms of the defect, not only as a diff: a
		// line ending on the nearest pip is the snapping this fix removed.
		if len(c) > 0 && c[len(c)-1] == snappedChainLastPoint {
			t.Errorf("%s ends the chain at %s — that is the nearest PIP, so the inline endpoint was snapped rather than interpolated",
				name, snappedChainLastPoint)
		}
	}
	if strings.Join(got["geojson"], " ") != strings.Join(got["kml"], " ") {
		t.Errorf("the two writers drew one chain differently:\n geojson %v\n kml     %v", got["geojson"], got["kml"])
	}
}

// geojsonChainCoords is the chain Feature's coordinates as "lon,lat[,ele]",
// spelled the way the writers spell numbers so the two formats compare directly.
func geojsonChainCoords(t *testing.T, doc *vnx.Document) []string {
	t.Helper()
	var b bytes.Buffer
	if _, err := geojson.Write(&b, doc); err != nil {
		t.Fatalf("geojson: %v", err)
	}
	var fc struct {
		Features []struct {
			Properties struct {
				Name string `json:"name"`
			} `json:"properties"`
			Geometry struct {
				Type string `json:"type"`
				// RawMessage because the sibling network Feature nests one
				// level deeper (MultiLineString); a typed field there fails
				// the whole decode and hides the chain this test is about.
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b.Bytes(), &fc); err != nil {
		t.Fatalf("geojson output is not a FeatureCollection this test can read: %v\n%s", err, b.String())
	}
	for _, f := range fc.Features {
		if f.Properties.Name != "Net / Chain" {
			continue
		}
		if f.Geometry.Type != "LineString" {
			t.Fatalf("the chain Feature is a %s, want LineString", f.Geometry.Type)
		}
		var coords [][]float64
		if err := json.Unmarshal(f.Geometry.Coordinates, &coords); err != nil {
			t.Fatalf("the chain Feature's coordinates are not a position list: %v", err)
		}
		out := make([]string, 0, len(coords))
		for _, c := range coords {
			parts := make([]string, len(c))
			for i, v := range c {
				parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
			}
			out = append(out, strings.Join(parts, ","))
		}
		return out
	}
	t.Fatalf("no Feature named %q in the GeoJSON — the extractor is broken, or the chain was dropped:\n%s", "Net / Chain", b.String())
	return nil
}

// kmlChainCoords is the chain Placemark's <coordinates>, already "lon,lat[,ele]"
// in the file.
//
// The Placemark is named "Chain" and not "Net / Chain" because KML has real
// <Folder>s and GeoJSON has none, so only GeoJSON flattens the path into the
// name (§2.7). That difference is the formats' shapes and is not the drift this
// file guards; only the COORDINATES are compared across the two.
func kmlChainCoords(t *testing.T, doc *vnx.Document) []string {
	t.Helper()
	var b bytes.Buffer
	if _, err := kml.Write(&b, doc); err != nil {
		t.Fatalf("kml: %v", err)
	}
	out := b.String()
	i := strings.Index(out, "<name>Chain</name>")
	if i < 0 {
		t.Fatalf("no Placemark named %q in the KML — the extractor is broken, or the chain was dropped:\n%s", "Chain", out)
	}
	rest := out[i:]
	start := strings.Index(rest, "<coordinates>")
	end := strings.Index(rest, "</coordinates>")
	if start < 0 || end < start {
		t.Fatalf("the chain Placemark carries no <coordinates>:\n%s", rest)
	}
	return strings.Fields(rest[start+len("<coordinates>") : end])
}
