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
// Point Feature per in-network waypoint, a Polygon Feature per in-network area,
// and a LineString Feature per chain — each named (§2.6, redesigned 2026-09-03).
//
// ⚠ Network.Points AND Network.Areas EXIST AND ARE POPULATED, and an earlier
// draft of the spec described arcs only — a writer built literally from it
// would have dropped them SILENTLY. This test is what stops that draft coming
// back.
//
// ⚠ AND THEY ARE NOT BARE GEOMETRIES IN A GeometryCollection ANY MORE. That
// shape kept their coordinates and destroyed their Name and Notes, because a
// GeometryCollection member has no properties of its own.
func TestNetworkEmitsArcsPointsAreasAndChains(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name: "Net", Notes: "graph",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil))},
		Arcs: map[string]vnx.Arc{
			"a": {From: "n1", To: "n2"},
		},
		Points: []vnx.Waypoint{{Name: "hut", At: pos(0.5, 0.5, nil)}},
		Areas:  []vnx.Area{{Name: "bog", Polygon: square()}},
		Chains: []vnx.Chain{{Name: "Traverse", Walk: []vnx.Pass{
			{Arc: "a", From: node("n1"), To: node("n2")},
		}}},
	}}})

	if report.Emitted != 4 {
		t.Errorf("emitted %d, wanted the arcs Feature and one each for the point, the area and the chain", report.Emitted)
	}
	if strings.Contains(out, `"GeometryCollection"`) {
		t.Errorf("the GeometryCollection is back, and with it the loss of every in-network name and note:\n%s", out)
	}
	for _, want := range []string{
		`{"type":"Feature","properties":{"name":"Net","description":"graph"},"geometry":{"type":"MultiLineString"`,
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

// ⚠ THE FOUR FIELDS THE GeometryCollection DESTROYED, and the two GeoJSON can
// keep. Network.Points are full vnx.Waypoints and Network.Areas full vnx.Areas,
// each with Name, Notes, Hidden and Style (item.go:34-55). As bare geometries
// inside a shared collection all four were lost; as Features of their own the
// name and the notes survive.
//
// ⚠ HIDDEN AND STYLE STILL DO NOT, AND MUST NOT START TO. §2.3's properties lock
// is name and description ONLY — splitting the items out does not open the door.
func TestInNetworkPointAndAreaKeepTheirNameAndNotes(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "Highlands"}},
		Networks: []vnx.Network{{
			Name: "Cairngorms", Folder: "f",
			Points: []vnx.Waypoint{{Name: "Summit cairn", Notes: "big pile", Hidden: true,
				Style: vnx.Style{"color": "#E04535"}, At: pos(1, 2, nil)}},
			Areas: []vnx.Area{{Name: "Deer reserve", Notes: "no dogs", Hidden: true,
				Style: vnx.Style{"fill-color": "#1E9C45"}, Polygon: square()}},
		}},
	})
	for _, want := range []string{
		`"name":"Highlands / Cairngorms / Summit cairn","description":"big pile"`,
		`"name":"Highlands / Cairngorms / Deer reserve","description":"no dogs"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — an in-network item lost its name or its notes:\n%s", want, out)
		}
	}
	for _, locked := range []string{"hidden", "Hidden", "E04535", "1E9C45", "style", "color"} {
		if strings.Contains(out, locked) {
			t.Errorf("%q reached properties; §2.3 allows name and description only:\n%s", locked, out)
		}
	}
}

// A network with no arcs gets NO Feature of its own, and the loss is SAID.
//
// ⚠ THIS REVERSES AN EARLIER RULING AND THE REVERSAL IS THE POINT. The first
// draft wrote {"type":"MultiLineString","coordinates":[]} so the network's name
// and notes survived. Dave withdrew it 2026-09-03: "a network is designed to
// have arcs, and if it doesn't have arcs, then it's fine to have the network
// name and description lost." Do not put the empty Feature back without a new
// ruling — and note that restoring it also means re-relaxing the RFC 7946
// validator, which went back to strict when this landed.
//
// ⚠ THE NAME IS NOT WHOLLY LOST, WHICH IS WHY THE CODE IS THE PARTIAL ONE. The
// points and areas carry it in their own names. Only the NOTES have nowhere to
// go, so part of the network reached the file and part did not — writer.go
// convention 5's reading of CodePartialGeometry exactly.
func TestNetworkWithNoArcsWritesNoFeatureOfItsOwnAndSaysSo(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name: "Empty graph", Notes: "no arcs yet",
		Points: []vnx.Waypoint{{Name: "Lone cairn", At: pos(1, 2, nil)}},
	}}})
	// No Feature of its own, in either of the two shapes that were considered.
	if strings.Contains(out, `"coordinates":[]`) {
		t.Errorf("the empty-coordinates Feature is back; the ruling that wanted it was withdrawn:\n%s", out)
	}
	if strings.Contains(out, `"geometry":null`) {
		t.Errorf("a null geometry was written; that shape was never ruled in:\n%s", out)
	}
	if strings.Contains(out, `"description":"no arcs yet"`) {
		t.Errorf("the notes were written somewhere; nothing carries them when there are no arcs:\n%s", out)
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
		t.Fatalf("diagnostics %v, want exactly one for the notes that were dropped", report.Diagnostics)
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
		Name: "Ghost", Notes: "nothing here",
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

// ⚠ THE BARE ARCS ARE NOT DROPPED TO AVOID DUPLICATING THE CHAIN'S GEOMETRY.
// An arc no chain traverses would then vanish silently — the same class of
// loss §2.6 exists to prevent.
func TestAnArcNoChainTraversesStillReachesTheFile(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Net",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil))},
		Arcs: map[string]vnx.Arc{
			"walked": {From: "n1", To: "n2"},
			"lonely": {From: "n1", To: "n2", Pips: vs(pos(5, 5, nil), pos(6, 6, nil))},
		},
		Chains: []vnx.Chain{{Name: "c", Walk: []vnx.Pass{{Arc: "walked", From: node("n1"), To: node("n2")}}}},
	}}})
	if !strings.Contains(out, `[[0,0],[5,5],[6,6],[1,1]]`) {
		t.Errorf("the untraversed arc is not in the file:\n%s", out)
	}
}

// A chain walks its passes in order, reversing an arc it walks backwards, and
// does not double the position two passes share.
func TestChainGeometryFollowsItsWalk(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Net",
		Nodes: map[string]vnx.Vertex{"n1": vx(pos(0, 0, nil)), "n2": vx(pos(1, 1, nil)), "n3": vx(pos(2, 2, nil))},
		Arcs: map[string]vnx.Arc{
			"a": {From: "n1", To: "n2", Pips: vs(pos(0.5, 0.5, nil))},
			// Stored n3 -> n2; the chain walks it the other way.
			"b": {From: "n3", To: "n2", Pips: vs(pos(1.5, 1.5, nil))},
		},
		Chains: []vnx.Chain{{Name: "c", Walk: []vnx.Pass{
			{Arc: "a", From: node("n1")},
			{Arc: "b", To: node("n3")},
		}}},
	}}})
	const want = `[[0,0],[0.5,0.5],[1,1],[1.5,1.5],[2,2]]`
	if !strings.Contains(out, want) {
		t.Errorf("chain geometry is not the walked line.\nwanted %s in\n%s", want, out)
	}
}

// A route is ONE LineString and the position two arcs share is written once.
// Arc boundaries are the matrix cell; a doubled vertex is not.
func TestRouteArcsJoinWithoutDoublingTheSharedPosition(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Routes: []vnx.Route{route(vnx.Route{Name: "r"},
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
	out, _ := writeDoc(t, &vnx.Document{Waypoints: []vnx.Waypoint{{At: pos(1, 2, nil)}}})
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
		Folders:   []vnx.Folder{{ID: "f", Name: "A / B"}},
		Waypoints: []vnx.Waypoint{{Name: "C", Folder: "f", At: pos(1, 2, nil)}},
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
