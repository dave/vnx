package geojson

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// libRoot is this library's own import path — see pkg/vnx/vnx_test.go.
const libRoot = "github.com/dave/vnx/go/pkg/vnx"

// TestImportsAreStdlibOnly mirrors pkg/vnx's own guard (spec §4.1: "All three
// are stdlib-only. No locerr, no db, no vtlog. A writer that needs one of
// those has taken a wrong turn."). pkg/vnx itself is the one permitted
// non-stdlib import: this package is a subpackage of the library being
// extracted, not a consumer of the app.
//
// ⚠ IT COVERS _test.go FILES, since 2026-09-06 — see pkg/vnx's own guard for
// why that hole mattered. A test file may reach the library's sibling
// packages; a source file may reach only the parent.
func TestImportsAreStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked, checkedTests := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		isTest := strings.HasSuffix(name, "_test.go")
		if isTest {
			checkedTests++
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == libRoot {
				continue
			}
			if isTest && strings.HasPrefix(path, libRoot+"/") {
				continue // a sibling package of the same library
			}
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — this writer must import stdlib and pkg/vnx only", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("guard inspected zero source files — dead probe, not a clean tree")
	}
	if checkedTests == 0 {
		t.Fatal("guard inspected zero test files — the _test.go coverage added on 2026-09-06 is gone")
	}
}

// A network emits ALL FOUR KINDS: the arcs as one MultiLineString Feature, a
// Point Feature per in-network point, a Polygon Feature per in-network area,
// and a LineString Feature per highlight — each named (§2.6, redesigned 2026-09-03).
//
// ⚠ Network.Points AND Network.Areas EXIST AND ARE POPULATED, and an earlier
// draft of the spec described arcs only — a writer built literally from it
// would have dropped them SILENTLY. This test is what stops that draft coming
// back.
//
// ⚠ AND THEY ARE NOT BARE GEOMETRIES IN A GeometryCollection ANY MORE. That
// shape kept their coordinates and destroyed their Name, because a
// GeometryCollection member has no properties of its own.
func TestNetworkEmitsArcsPointsAreasAndHighlights(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Net",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil))},
		Arcs: map[string]vnx.Arc{
			"a": {From: "n1", To: "n2"},
		},
		Points: []vnx.Point{{Name: "hut", At: pos(0.5, 0.5, nil)}},
		Areas:  []vnx.Area{{Name: "bog", Polygon: square()}},
		Highlights: []vnx.Highlight{{Name: "Traverse", Walk: []vnx.Pass{
			{Arc: "a", From: node("n1"), To: node("n2")},
		}}},
	}}})

	if report.Emitted != 4 {
		t.Errorf("emitted %d, wanted the arcs Feature and one each for the point, the area and the highlight", report.Emitted)
	}
	if strings.Contains(out, `"GeometryCollection"`) {
		t.Errorf("the GeometryCollection is back, and with it the loss of every in-network name:\n%s", out)
	}
	for _, want := range []string{
		`{"type":"Feature","properties":{"name":"Net"},"geometry":{"type":"MultiLineString"`,
		`{"type":"Feature","properties":{"name":"Net / hut"},"geometry":{"type":"Point"`,
		`{"type":"Feature","properties":{"name":"Net / bog"},"geometry":{"type":"Polygon"`,
		`{"type":"Feature","properties":{"name":"Net / Traverse"},"geometry":{"type":"LineString"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — an in-network kind was dropped or lost its name:\n%s", want, out)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("a whole network cost nothing here; got %v", report.Diagnostics)
	}
}

// ⚠ THE FIELD THE GeometryCollection DESTROYED, and the one GeoJSON can keep.
// Network.Points are full vnx.Points and Network.Areas full vnx.Areas, each
// with Name and Hidden. As bare geometries inside a shared collection both
// were lost; as Features of their own the name survives.
//
// ⚠ HIDDEN STILL DOES NOT, AND MUST NOT START TO. §2.3's properties lock
// is the name ONLY — splitting the items out does not open the door.
func TestInNetworkPointAndAreaKeepTheirName(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "Highlands"}},
		Networks: []vnx.Network{{
			Name: "Cairngorms", Folder: "f",
			Points: []vnx.Point{{Name: "Summit cairn", Hidden: true, At: pos(1, 2, nil)}},
			Areas:  []vnx.Area{{Name: "Deer reserve", Hidden: true, Polygon: square()}},
		}},
	})
	for _, want := range []string{
		`"properties":{"name":"Highlands / Cairngorms / Summit cairn"}`,
		`"properties":{"name":"Highlands / Cairngorms / Deer reserve"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — an in-network item lost its name:\n%s", want, out)
		}
	}
	for _, locked := range []string{"hidden", "Hidden"} {
		if strings.Contains(out, locked) {
			t.Errorf("%q reached properties; §2.3 allows the name only:\n%s", locked, out)
		}
	}
}

// A network with no arcs gets NO Feature of its own, and the loss is SAID.
//
// ⚠ THIS REVERSES AN EARLIER RULING AND THE REVERSAL IS THE POINT. The first
// draft wrote {"type":"MultiLineString","coordinates":[]} so the network's name
// survived. Dave withdrew it 2026-09-03: "a network is designed to
// have arcs, and if it doesn't have arcs, then it's fine to have the network
// name and description lost." Do not put the empty Feature back without a new
// ruling — and note that restoring it also means re-relaxing the RFC 7946
// validator, which went back to strict when this landed.
//
// ⚠ THE NAME IS NOT WHOLLY LOST, WHICH IS WHY THE CODE IS THE PARTIAL ONE. The
// points and areas carry it in their own names. The network as a Feature of its
// own has nowhere to go, so part of the network reached the file and part did
// not — writer.go convention 5's reading of CodePartialGeometry exactly.
func TestNetworkWithNoArcsWritesNoFeatureOfItsOwnAndSaysSo(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:   "Empty graph",
		Points: []vnx.Point{{Name: "Lone cairn", At: pos(1, 2, nil)}},
	}}})
	// No Feature of its own, in either of the two shapes that were considered.
	if strings.Contains(out, `"coordinates":[]`) {
		t.Errorf("the empty-coordinates Feature is back; the ruling that wanted it was withdrawn:\n%s", out)
	}
	if strings.Contains(out, `"geometry":null`) {
		t.Errorf("a null geometry was written; that shape was never ruled in:\n%s", out)
	}
	// The point IS written, and the network's name is inside its name.
	const want = `{"type":"Feature","properties":{"name":"Empty graph / Lone cairn"},"geometry":{"type":"Point","coordinates":[1,2]}}`
	if !strings.Contains(out, want) {
		t.Errorf("wanted\n  %s\ngot\n  %s", want, out)
	}
	if report.Emitted != 1 {
		t.Errorf("emitted %d, want 1 (the point alone)", report.Emitted)
	}
	// ⚠ AND IT IS SAID. Dave ruled the loss acceptable, not silent.
	if len(report.Diagnostics) != 1 {
		t.Fatalf("diagnostics %v, want exactly one for the network's own Feature, which was not written", report.Diagnostics)
	}
	d := report.Diagnostics[0]
	if d.Code != vnx.CodePartialGeometry || d.Path != "/networks/0" || len(d.Args) != 1 || d.Args[0] != "Empty graph" {
		t.Errorf("diagnostic = %+v, want %s at /networks/0 naming Empty graph", d, vnx.CodePartialGeometry)
	}
}

// A network holding NOTHING AT ALL is dropped with its diagnostic. Ruled
// 2026-09-03: a name is worth keeping only when there is something for it to
// name, so the empty-coordinates case above is narrower than it reads.
func TestNetworkHoldingNothingIsDroppedWithItsDiagnostic(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name: "Ghost",
	}}})
	if strings.Contains(out, "Ghost") {
		t.Errorf("an empty network was written anyway:\n%s", out)
	}
	if report.Emitted != 0 {
		t.Errorf("emitted %d, want 0", report.Emitted)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("diagnostics %v, want exactly one", report.Diagnostics)
	}
	d := report.Diagnostics[0]
	if d.Code != vnx.CodeItemNoGeometry || d.Path != "/networks/0" || len(d.Args) != 1 || d.Args[0] != "Ghost" {
		t.Errorf("diagnostic = %+v, want %s at /networks/0 naming Ghost", d, vnx.CodeItemNoGeometry)
	}
}

// ⚠ THE BARE ARCS ARE NOT DROPPED TO AVOID DUPLICATING THE HIGHLIGHT'S GEOMETRY.
// An arc no highlight traverses would then vanish silently — the same class of
// loss §2.6 exists to prevent.
func TestAnArcNoHighlightTraversesStillReachesTheFile(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Net",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil))},
		Arcs: map[string]vnx.Arc{
			"walked": {From: "n1", To: "n2"},
			"lonely": {From: "n1", To: "n2", Pips: vs(pos(5, 5, nil), pos(6, 6, nil))},
		},
		Highlights: []vnx.Highlight{{Walk: []vnx.Pass{{Arc: "walked", From: node("n1"), To: node("n2")}}}},
	}}})
	if !strings.Contains(out, `[[0,0],[5,5],[6,6],[1,1]]`) {
		t.Errorf("the untraversed arc is not in the file:\n%s", out)
	}
}

// A highlight walks its passes in order, reversing an arc it walks backwards, and
// does not double the position two passes share.
func TestHighlightGeometryFollowsItsWalk(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Net",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil)), "n3": vx(pos(2, 2, nil))},
		Arcs: map[string]vnx.Arc{
			"a": {From: "n1", To: "n2", Pips: vs(pos(0.5, 0.5, nil))},
			// Stored n3 -> n2; the highlight walks it the other way.
			"b": {From: "n3", To: "n2", Pips: vs(pos(1.5, 1.5, nil))},
		},
		Highlights: []vnx.Highlight{{Walk: []vnx.Pass{
			{Arc: "a", From: node("n1")},
			{Arc: "b", To: node("n3")},
		}}},
	}}})
	const want = `[[0,0],[0.5,0.5],[1,1],[1.5,1.5],[2,2]]`
	if !strings.Contains(out, want) {
		t.Errorf("highlight geometry is not the walked line.\nwanted %s in\n%s", want, out)
	}
}

// A plan is ONE LineString and the position two arcs share is written once.
// Arc boundaries are the matrix cell; a doubled vertex is not.
func TestPlanArcsJoinWithoutDoublingTheSharedPosition(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Plans: []vnx.Plan{plan(vnx.Plan{Name: "r"},
		lineArc{Pips: []vnx.Position{pos(0, 0, nil), pos(1, 1, nil)}},
		lineArc{Pips: []vnx.Position{pos(1, 1, nil), pos(2, 2, nil)}},
	)}})
	if !strings.Contains(out, `"coordinates":[[0,0],[1,1],[2,2]]`) {
		t.Errorf("wanted the joined line, got %s", out)
	}
}

// ⚠ Network.Arcs IS A MAP. Ranging over it emits in random order and the
// byte-compared golden goes flaky — a failure that looks like a bug in the
// writer and is not.
func TestOutputIsDeterministicAcrossRuns(t *testing.T) {
	var first string
	for i := 0; i < 50; i++ {
		var buf bytes.Buffer
		if _, err := Write(&buf, fixture()); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = buf.String()
			continue
		}
		if buf.String() != first {
			t.Fatalf("run %d differed from run 0 — a map was ranged unsorted:\n%s\n%s", i, first, buf.String())
		}
	}
	// The control: the fixture must actually hold a map with more than one key,
	// or this test proves nothing.
	if len(fixture().Networks[0].Arcs) < 2 {
		t.Fatal("the fixture network holds fewer than two arcs, so map order was never at stake")
	}
}

// The document is FLAT: one FeatureCollection, no nesting, no folder member
// anywhere (RFC 7946 has no folder concept and §2.3 forbids inventing one).
func TestTheCollectionIsFlat(t *testing.T) {
	var buf bytes.Buffer
	if _, err := Write(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Type != "FeatureCollection" {
		t.Errorf("root type is %q", doc.Type)
	}
	if len(doc.Features) != 10 {
		t.Errorf("wanted 10 features, got %d", len(doc.Features))
	}
	for _, bad := range []string{"folder", "Folder", "FeatureCollection\",\"features\":[{\"type\":\"FeatureCollection"} {
		if strings.Contains(strings.TrimPrefix(buf.String(), `{"type":"FeatureCollection`), bad) {
			t.Errorf("%q appears in the output; the collection must be flat with no folder member:\n%s", bad, buf.String())
		}
	}
}

// An item with no name emits no `name` property rather than an empty one: the
// item has no name, and asserting it has a blank one is a different claim.
func TestAnUnnamedItemHasNoNameProperty(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Points: []vnx.Point{{At: pos(1, 2, nil)}}})
	if strings.Contains(out, `"name"`) {
		t.Errorf("an empty name was written: %s", out)
	}
	if !strings.Contains(out, `"properties":{}`) {
		t.Errorf("wanted empty properties, got %s", out)
	}
}

// A name containing the separator makes the path ambiguous, and that is
// ACCEPTED rather than escaped (§2.7): an escape scheme would invent a
// convention no other application reads, to protect a round-trip these formats
// cannot make anyway.
func TestASeparatorInANameIsNotEscaped(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "A / B"}},
		Points:  []vnx.Point{{Name: "C", Folder: "f", At: pos(1, 2, nil)}},
	})
	if !strings.Contains(out, `"name":"A / B / C"`) {
		t.Errorf("wanted the ambiguous path verbatim, got %s", out)
	}
	if strings.Contains(out, `\`) {
		t.Errorf("something was escaped: %s", out)
	}
}

// A walk that does not resolve fails the write. The "walk the whole arc
// forwards" answer it replaces drew a line nobody walked and reported success.
func TestAWalkThatDoesNotResolveFailsTheWrite(t *testing.T) {
	for name, d := range brokenWalks() {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := Write(&buf, d); err == nil {
				t.Fatalf("the write succeeded:\n%s", buf.String())
			}
		})
	}
}

// Every in-network feature is named "{network} / {label}" (Dave, 2026-11-03):
// a highlight, point or area by its own name, and one with no name by its
// integer within the network. No feature is named "{network} / " with nothing
// after the separator.
func TestInNetworkFeaturesAreNamedByNameOrInteger(t *testing.T) {
	d := &vnx.Document{Networks: []vnx.Network{{
		Name:   "Ridges",
		Nodes:  map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil))},
		Arcs:   map[string]vnx.Arc{"a": {From: "n1", To: "n2"}},
		Points: []vnx.Point{{At: pos(0.5, 0.5, nil)}},
		Areas:  []vnx.Area{{Name: "Bog", Polygon: square()}},
		Highlights: []vnx.Highlight{
			{Name: "West ridge", Walk: []vnx.Pass{{Arc: "a", From: node("n1"), To: node("n2")}}},
			{Walk: []vnx.Pass{{Arc: "a", From: node("n2"), To: node("n1")}}},
		},
	}}}
	out, _ := writeDoc(t, d)
	for _, want := range []string{
		`"name":"Ridges / 1"`,          // the unnamed point
		`"name":"Ridges / Bog"`,        // the area's own name
		`"name":"Ridges / West ridge"`, // the named highlight
		`"name":"Ridges / 2"`,          // the unnamed highlight
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"name":"Ridges / "`) {
		t.Errorf("a feature is named by its network alone:\n%s", out)
	}
}

// A write error names its owner as the walk errors of every vnx library do
// (#321): the network's name as a JSON string literal, then the highlight's
// 0-based index or the arc's id, also as a JSON string literal. Go's %q is
// not JSON for a control character or for U+2028.
func TestAWriteErrorNamesItsOwnerAsJSON(t *testing.T) {
	const name = "R\"i\\d\u0001\u2028ge"
	const quoted = `"R\"i\\d\u0001` + "\u2028" + `ge"`
	nodes := map[string]vnx.Vertex{"a": {Lon: 0, Lat: 0}, "b": {Lon: 1, Lat: 0}}
	at := func(n string) *vnx.Endpoint { return &vnx.Endpoint{Node: n} }
	for _, tc := range []struct {
		label string
		d     *vnx.Document
		want  string
	}{
		{"highlight", &vnx.Document{Networks: []vnx.Network{{Name: name, Nodes: nodes,
			Arcs: map[string]vnx.Arc{"ab": {From: "a", To: "b"}},
			Highlights: []vnx.Highlight{
				{Walk: []vnx.Pass{{Arc: "ab", From: at("a"), To: at("b")}}},
				{Walk: []vnx.Pass{{Arc: "ghost", From: at("a"), To: at("b")}}},
			}}}}, "geojson: network " + quoted + " highlight 1: "},
		{"arc", &vnx.Document{Networks: []vnx.Network{{Name: name, Nodes: nodes,
			Arcs: map[string]vnx.Arc{"a\"x": {From: "a", To: "x"}}}}},
			"geojson: network " + quoted + ` arc "a\"x": `},
	} {
		t.Run(tc.label, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := Write(&buf, tc.d)
			if err == nil {
				t.Fatal("the write succeeded")
			}
			if !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("error %q, want it to start %q", err, tc.want)
			}
		})
	}
}
