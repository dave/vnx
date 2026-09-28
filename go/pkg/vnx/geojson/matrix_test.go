package geojson

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

// The lossiness matrix, one test per GeoJSON cell (spec §6.3).
//
// ⚠ A CELL IS NOT DONE BECAUSE THE BEHAVIOUR EXISTS. It is done when a test
// proves the loss is the STATED loss: the right Code, on the right Path, with
// the right args, and nothing of the item left in the file. A cell nobody
// tested is a guess in a table that looks authoritative — spec 5 shipped one
// and it was found at reconciliation.

func writeDoc(t *testing.T, d *vnx.Document) (string, vnx.WriteReport) {
	t.Helper()
	var buf bytes.Buffer
	report, err := Write(&buf, d)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if problems := validate(buf.Bytes()); len(problems) > 0 {
		t.Fatalf("output is not valid RFC 7946:\n  %s\n%s", strings.Join(problems, "\n  "), buf.String())
	}
	return buf.String(), report
}

// wantDiag asserts one diagnostic exactly: code, path and args. Asserting the
// code alone would pass on a diagnostic pointing at the wrong item.
func wantDiag(t *testing.T, got []vnx.Diagnostic, i int, code, path string, args ...string) {
	t.Helper()
	if i >= len(got) {
		t.Fatalf("wanted a diagnostic at [%d] (%s %s), got only %d: %v", i, code, path, len(got), got)
	}
	d := got[i]
	if d.Code != code || d.Path != path {
		t.Errorf("diagnostic[%d] is %s at %s, wanted %s at %s (message: %s)", i, d.Code, d.Path, code, path, d.Message)
	}
	if d.Severity != vnx.Warning {
		t.Errorf("diagnostic[%d] severity is %s, every writer diagnostic reports a DROP and must be a warning", i, d.Severity)
	}
	if len(d.Args) != len(args) {
		t.Fatalf("diagnostic[%d] carries args %q, wanted %q", i, d.Args, args)
	}
	for k := range args {
		if d.Args[k] != args[k] {
			t.Errorf("diagnostic[%d].Args[%d] is %q, wanted %q", i, k, d.Args[k], args[k])
		}
	}
	if d.Message == "" {
		t.Errorf("diagnostic[%d] has no message; the public CLI prints it beside the Code", i)
	}
}

// CELL: Application key → dropped, diagnosed, one per key.
func TestCellAppKeyIsDroppedAndDiagnosed(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "keep", At: pos(1, 2, nil)}},
		AppKeys: []vnx.AppKey{
			{Key: "app.viatrail.rulesets", Value: []any{map[string]any{"name": "Munros"}}},
			{Key: "com.example.corbetts", Value: []any{}},
		},
	})
	if strings.Contains(out, "Munros") || strings.Contains(out, "corbetts") {
		t.Errorf("an application key reached the file: %s", out)
	}
	if report.Emitted != 1 {
		t.Errorf("emitted %d, wanted only the waypoint", report.Emitted)
	}
	if len(report.Diagnostics) != 2 {
		t.Fatalf("wanted one diagnostic per application key, got %v", report.Diagnostics)
	}
	// Args[0] is the key: it is the application key's only name.
	wantDiag(t, report.Diagnostics, 0, vnx.CodeAppKeyUnsupported, "/app.viatrail.rulesets", "app.viatrail.rulesets")
	wantDiag(t, report.Diagnostics, 1, vnx.CodeAppKeyUnsupported, "/com.example.corbetts", "com.example.corbetts")
}

// CELL: Folder → path folded into the name; diagnosed ONLY if empty.
func TestCellNonEmptyFolderIsNotACasualty(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Folders:   []vnx.Folder{{ID: "a", Name: "Foo"}, {ID: "b", Name: "Bar", Parent: "a"}},
		Waypoints: []vnx.Waypoint{{Name: "Baz", Folder: "b", At: pos(1, 2, nil)}},
	})
	if !strings.Contains(out, `"name":"Foo / Bar / Baz"`) {
		t.Errorf("the folder path is not in the name: %s", out)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("a folder whose name survives in an item's name is NOT a casualty (§2.7), got %v", report.Diagnostics)
	}
}

func TestCellEmptyFolderIsDiagnosed(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "a", Name: "Foo"},
			{ID: "b", Name: "Bar", Parent: "a"},
			{ID: "c", Name: "Hollow", Parent: "a"},
		},
		Waypoints: []vnx.Waypoint{{Name: "Baz", Folder: "b", At: pos(1, 2, nil)}},
	})
	// "Foo" and "Bar" survive inside "Foo / Bar / Baz"; "Hollow" has nowhere
	// to put its name.
	if len(report.Diagnostics) != 1 {
		t.Fatalf("wanted exactly the empty folder diagnosed, got %v", report.Diagnostics)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeEmptyFolderDropped, "/folders/2", "Hollow")
}

// A folder holding only an EMPTY folder is itself empty: nothing is anywhere
// beneath it, so its name reaches no feature either.
func TestCellFolderHoldingOnlyAnEmptyFolderIsAlsoEmpty(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{
		Folders:   []vnx.Folder{{ID: "a", Name: "Foo"}, {ID: "b", Name: "Bar", Parent: "a"}},
		Waypoints: []vnx.Waypoint{{Name: "root item", At: pos(1, 2, nil)}},
	})
	if len(report.Diagnostics) != 2 {
		t.Fatalf("both folders are empty and both should be diagnosed, got %v", report.Diagnostics)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeEmptyFolderDropped, "/folders/0", "Foo")
	wantDiag(t, report.Diagnostics, 1, vnx.CodeEmptyFolderDropped, "/folders/1", "Bar")
}

func TestCellUnavailableFolderPathKeepsTheItem(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "Cairn", Folder: "nowhere", At: pos(1, 2, nil)}},
	})
	if !strings.Contains(out, `"name":"Cairn"`) {
		t.Errorf("the item must be KEPT under its bare name: %s", out)
	}
	if report.Emitted != 1 {
		t.Errorf("emitted %d; losing a position in a tree GeoJSON cannot express is not worth dropping content over", report.Emitted)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeFolderPathUnavailable, "/waypoints/0", "Cairn")
}

// A cyclic parent chain reaches FolderPath only from a hand-built Document —
// resolveParents clears cycles at decode — but a writer must not hang on any
// input, and the item must still come out.
func TestCyclicFolderParentsDoNotHang(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "a", Name: "A", Parent: "b"},
			{ID: "b", Name: "B", Parent: "a"},
		},
		Waypoints: []vnx.Waypoint{{Name: "Cairn", Folder: "a", At: pos(1, 2, nil)}},
	})
	if !strings.Contains(out, `"name":"Cairn"`) {
		t.Errorf("the item must survive a cyclic tree: %s", out)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeFolderPathUnavailable, "/waypoints/0", "Cairn")
}

// CELL: attribute losses are matrix facts, NOT per-item diagnostics
// (writer.go convention 5). properties carries name and description only.
func TestCellStyleAndHiddenAreDroppedWithoutDiagnostics(t *testing.T) {
	hidden := true
	out, report := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{
			Name: "Pinned", Notes: "note", Hidden: true, At: pos(1, 2, nil),
			Style: vnx.Style{"color": "#ff0000", "icon": "peak", "pin-shape": "round", "label-visible": hidden},
		}},
		Areas: []vnx.Area{{Name: "Painted", Hidden: true, Polygon: square(),
			Style: vnx.Style{"fill-color": "#00ff00", "stroke-color": "#0000ff"}}},
	})
	// CARRIED: the styled, hidden items themselves. A hidden item is EXPORTED
	// — GeoJSON has no visibility, so the STATE is lost and the item is not —
	// and a writer that skipped it would pass every absence assertion below.
	for _, want := range []string{`"name":"Pinned"`, `"name":"Painted"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s; only the style and the hidden state are lost, not the item:\n%s", want, out)
		}
	}
	if report.Emitted != 2 {
		t.Errorf("emitted %d, want 2 — hidden must not mean skipped", report.Emitted)
	}
	for _, leaked := range []string{"ff0000", "peak", "round", "hidden", "00ff00", "style", "Style"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q reached the file; properties carries name and description ONLY (§2.3):\n%s", leaked, out)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("attribute losses are matrix facts proven by this test, not per-item diagnostics (convention 5); got %v", report.Diagnostics)
	}
}

// CELL: Tags — ❌. Tags cross the VNX wire as `app.viatrail.tag` data records.
//
// ⚠ THIS TEST USED TO CONSTRUCT `Data: []vnx.Record{}` — AN EMPTY LIST — and
// then assert that no tag reached the file. It proved nothing: a writer that
// leaked every record in the document passed it, because the document held
// none. Found by the T11 census, 2026-09-02, and red-proved against a writer
// that emits the records. Records are now built on every kind that can hold
// one, because "the writer drops waypoint records" is not evidence that it
// drops a track's.
func TestCellTagsAreNotCarried(t *testing.T) {
	tag := func(v string) vnx.Record {
		return vnx.Record{Type: "app.viatrail.tag", Body: map[string]any{"value": v}}
	}
	routing := vnx.Record{Type: "app.viatrail.routing", Body: map[string]any{"profile": "walking"}}
	out, report := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "Munros", Data: []vnx.Record{tag("scotland")}}},
		Waypoints: []vnx.Waypoint{{
			Name: "Cairn", Folder: "f", At: pos(1, 2, nil),
			Data: []vnx.Record{tag("bagged"), tag("winter")},
		}},
		Areas: []vnx.Area{{Name: "Reserve", Polygon: square(), Data: []vnx.Record{tag("deer")}}},
		Tracks: []vnx.Track{{
			Name: "Walk", Data: []vnx.Record{tag("solo")},
			Segments: []vnx.Segment{{Pips: []vnx.Position{pos(1, 3, nil), pos(2, 3, nil)}}},
		}},
		Routes: []vnx.Route{{
			Name: "Ridge", Data: []vnx.Record{tag("planned"), routing},
			Arcs: []vnx.Arc{{Pips: []vnx.Position{pos(1, 2, nil), pos(3, 2, nil)}}},
		}},
	})
	// CARRIED: every tagged item, under its flattened name.
	for _, want := range []string{`"name":"Munros / Cairn"`, `"name":"Reserve"`, `"name":"Walk"`, `"name":"Ridge"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — a tagged item must still be exported:\n%s", want, out)
		}
	}
	if report.Emitted != 4 {
		t.Fatalf("emitted %d, want 4; this test cannot see a leak in a feature that was never written", report.Emitted)
	}
	// LOST: every record value, every record type, and the machinery.
	for _, leaked := range []string{
		"scotland", "bagged", "winter", "deer", "solo", "planned",
		"app.viatrail.tag", "app.viatrail.routing", "@type", "data", "value", "tags",
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q reached the file; `properties` carries name and description ONLY, and a data record in there is an <extensions> block wearing a different hat (§2.3):\n%s", leaked, out)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("tags are lost on every item in the library — a per-item row would bury the real casualties (convention 5); got %v", report.Diagnostics)
	}
}

// CELL: Routing config — ❌. Same mechanism as tags, but its own matrix row and
// its own temptation: a writer could plausibly decide the routing profile was
// worth a property. Nothing that reads GeoJSON knows what to do with one.
func TestCellRoutingConfigIsNotCarried(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Routes: []vnx.Route{{
		Name: "Ridge",
		Data: []vnx.Record{{Type: "app.viatrail.routing", Body: map[string]any{"profile": "walking", "avoid": "roads"}}},
		Arcs: []vnx.Arc{{
			Pips: []vnx.Position{pos(1, 2, nil), pos(3, 2, nil)},
			Data: []vnx.Record{{Type: "app.viatrail.compute-failed", Body: map[string]any{"reason": "no-route"}}},
		}},
	}}})
	// CARRIED: the route, as one LineString.
	if !strings.Contains(out, `"name":"Ridge"`) || !strings.Contains(out, `"type":"LineString"`) {
		t.Fatalf("the route was not exported:\n%s", out)
	}
	// LOST: the record on the route AND the one on the arc.
	for _, leaked := range []string{
		"app.viatrail.routing", "app.viatrail.compute-failed",
		"walking", "roads", "no-route", "profile", "avoid", "reason",
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q reached the file; a re-imported Viatrail GeoJSON comes back with no routing config, which is the ruling (§2.3):\n%s", leaked, out)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", report.Diagnostics)
	}
}

// CELL: Moving time — ❌, and this cell is ❌ rather than GPX's ⚠ precisely
// because the pip times it is derived from are not carried either. Both halves
// are asserted: the figure is absent, and so is every input a reader could
// recompute it from.
func TestCellMovingTimeIsNotCarriedAndIsNotDerivable(t *testing.T) {
	start, hourLater := int64(1725200000000), int64(1725203600000)
	d := &vnx.Document{Tracks: []vnx.Track{{
		Name: "Walk",
		Segments: []vnx.Segment{{Pips: []vnx.Position{
			{Lon: 1, Lat: 2, Time: &start}, {Lon: 3, Lat: 4, Time: &hourLater},
		}}},
	}}}
	moving, known := d.Tracks[0].MovingTime()
	if !known || moving != time.Hour {
		t.Fatalf("the fixture's moving time is (%v, %v); this test would prove nothing about a figure that does not exist", moving, known)
	}
	out, report := writeDoc(t, d)
	if !strings.Contains(out, `"name":"Walk"`) {
		t.Fatalf("the track was not exported:\n%s", out)
	}
	for _, leaked := range []string{
		"3600", "movingTime", "moving_time", "moving-time", "duration",
		"1725200000000", "1725203600000", "2024-09-01", // the inputs, too
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q reached the file; the total is derived and never carried, and GeoJSON carries no pip time to re-derive it from:\n%s", leaked, out)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", report.Diagnostics)
	}
}

// CELL: Network topology — "one MultiLineString Feature for the arcs, one named
// Feature per in-network point, area and chain; SHARING lost". The SHAPE is
// pinned in geojson_test.go; what
// was never asserted is the loss itself, which is the half the matrix cell
// exists to state. KML's equivalent is kml/matrix_test.go's network test.
func TestCellNetworkTopologyLosesOnlyTheSharing(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "Lairig Ghru",
		Nodes: map[string]vnx.Position{"node-north": pos(0, 0, nil), "node-mid": pos(0, 1, nil), "node-south": pos(0, 2, nil)},
		Arcs: map[string]vnx.Arc{
			"arc-north": {From: "node-north", To: "node-mid", Pips: []vnx.Position{pos(0, 0, nil), pos(0, 1, nil)}},
			"arc-south": {From: "node-mid", To: "node-south", Pips: []vnx.Position{pos(0, 1, nil), pos(0, 2, nil)}},
		},
		Chains: []vnx.Chain{
			{Name: "Full Traverse", Links: []vnx.Link{
				{Arc: "arc-north", From: node("node-north"), To: node("node-mid")},
				{Arc: "arc-south", From: node("node-mid"), To: node("node-south")},
			}},
			// SHARES arc-north with the chain above. That sharing is the whole
			// of what this cell loses.
			{Name: "Half Way", Links: []vnx.Link{
				{Arc: "arc-north", From: node("node-north"), To: node("node-mid")},
			}},
		},
	}}})
	// CARRIED: one Feature for the network, holding every arc once, and one
	// NAMED Feature per chain.
	if n := strings.Count(out, `"name":"Lairig Ghru"`); n != 1 {
		t.Errorf("the network is written %d times, want exactly one Feature:\n%s", n, out)
	}
	for _, want := range []string{`"name":"Lairig Ghru / Full Traverse"`, `"name":"Lairig Ghru / Half Way"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — a chain is its own named Feature (§2.6):\n%s", want, out)
		}
	}
	if report.Emitted != 3 {
		t.Errorf("emitted %d, want 3 (the network and its two chains)", report.Emitted)
	}
	// LOST: the graph. Nothing in the file says which arcs a chain traverses,
	// or that both chains traverse arc-north — a chain's line is a flat list of
	// coordinates, and no arc or node id appears anywhere. Putting one there
	// would be an invented vendor property (§2.3).
	for _, leaked := range []string{"arc-north", "arc-south", "node-north", "node-mid", "node-south", "links", "arcs", "nodes"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q reached the file; GeoJSON has no way to express arc identity and the sharing is what this cell loses:\n%s", leaked, out)
		}
	}
}

// CELL: elevation unknown → a TWO-element position. Never a null ordinate,
// never 0.
func TestCellUnknownElevationEmitsTwoOrdinates(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{
			{Name: "known", At: pos(1, 2, ptr(300))},
			{Name: "unknown", At: pos(3, 4, nil)},
		},
	})
	if !strings.Contains(out, `"coordinates":[1,2,300]`) {
		t.Errorf("a known elevation must be the third ordinate: %s", out)
	}
	if !strings.Contains(out, `"coordinates":[3,4]`) {
		t.Errorf("an unknown elevation must produce a TWO-element position: %s", out)
	}
	if strings.Contains(out, "null") {
		t.Errorf("a null ordinate reached the file; RFC 7946 does not allow one: %s", out)
	}
	if strings.Contains(out, `[3,4,0]`) {
		t.Errorf("0 was written for an unknown elevation, putting the point at sea level: %s", out)
	}
}

// CELL: time is not a GeoJSON position channel and is dropped without a
// per-item diagnostic.
func TestCellPipTimeIsDropped(t *testing.T) {
	ms := int64(1756771200000)
	out, report := writeDoc(t, &vnx.Document{
		Tracks: []vnx.Track{{Name: "timed", Segments: []vnx.Segment{{Pips: []vnx.Position{
			{Lon: 1, Lat: 2, Time: &ms}, {Lon: 3, Lat: 4, Time: &ms},
		}}}}},
	})
	if strings.Contains(out, "17567") {
		t.Errorf("a timestamp reached the file: %s", out)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("time is a matrix fact, not a casualty row; got %v", report.Diagnostics)
	}
}

// CELL: an item with too little geometry is dropped and diagnosed rather than
// written as an invalid GeoJSON object.
func TestCellItemsWithoutGeometry(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{
		Areas: []vnx.Area{
			{Name: "short ring", Polygon: []vnx.Position{pos(0, 0, nil), pos(1, 0, nil), pos(0, 0, nil)}},
			{Name: "open ring", Polygon: []vnx.Position{pos(0, 0, nil), pos(1, 0, nil), pos(1, 1, nil), pos(0, 1, nil)}},
		},
		Tracks: []vnx.Track{{Name: "no segments"}},
		Routes: []vnx.Route{{Name: "no arcs"}},
	})
	if len(report.Diagnostics) != 4 {
		t.Fatalf("wanted one diagnostic per unplaceable item, got %v", report.Diagnostics)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeItemNoGeometry, "/areas/0/polygon", "short ring")
	wantDiag(t, report.Diagnostics, 1, vnx.CodeItemNoGeometry, "/areas/1/polygon", "open ring")
	wantDiag(t, report.Diagnostics, 2, vnx.CodeItemNoGeometry, "/tracks/0", "no segments")
	wantDiag(t, report.Diagnostics, 3, vnx.CodeItemNoGeometry, "/routes/0", "no arcs")
	// The message must say WHICH rule failed: "no geometry" alone sends a
	// reader looking for a missing polygon when the polygon is there and open.
	if !strings.Contains(report.Diagnostics[1].Message, "not closed") {
		t.Errorf("an open ring must be reported as open, got %q", report.Diagnostics[1].Message)
	}
}

// The fixture's own casualties, in document order (writer.go convention 6).
func TestFixtureDiagnostics(t *testing.T) {
	_, report := writeDoc(t, fixture())
	// 3 waypoints, 1 area, 1 track, 1 route, then the network's arcs Feature,
	// its in-network point, its in-network area and its one chain.
	if report.Emitted != 10 {
		t.Errorf("emitted %d features, wanted 10", report.Emitted)
	}
	if len(report.Diagnostics) != 5 {
		t.Fatalf("wanted 5 diagnostics, got %d: %v", len(report.Diagnostics), report.Diagnostics)
	}
	wantDiag(t, report.Diagnostics, 0, vnx.CodeEmptyFolderDropped, "/folders/2", "Nothing Here")
	wantDiag(t, report.Diagnostics, 1, vnx.CodeFolderPathUnavailable, "/waypoints/2", "Orphan Cairn")
	wantDiag(t, report.Diagnostics, 2, vnx.CodeItemNoGeometry, "/areas/1/polygon", "Sliver")
	wantDiag(t, report.Diagnostics, 3, vnx.CodePartialGeometry, "/tracks/0/segments/1", "Lairig Ghru walk")
	wantDiag(t, report.Diagnostics, 4, vnx.CodeAppKeyUnsupported, "/app.viatrail.rulesets", "app.viatrail.rulesets")
}

// Every Code this writer raises must be one pkg/vnx declared. A Code minted
// here would have no mapping in the bridge and would reach a user as silence.
func TestEveryCodeRaisedIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range vnx.WriterCodes() {
		registered[c] = true
	}
	docs := []*vnx.Document{fixture(), {
		Folders: []vnx.Folder{{ID: "a", Name: "empty"}},
		Areas:   []vnx.Area{{Name: "x", Folder: "ghost", Polygon: square()}},
		Tracks:  []vnx.Track{{Name: "y"}},
	}}
	seen := 0
	for _, d := range docs {
		var buf bytes.Buffer
		report, err := Write(&buf, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, dg := range report.Diagnostics {
			seen++
			if dg.Code == "" {
				t.Errorf("a writer diagnostic with no Code (%s): the bridge can only translate through the Code", dg.Path)
			} else if !registered[dg.Code] {
				t.Errorf("Code %q is not in vnx.WriterCodes(); it has no mapping and would reach a user as silence", dg.Code)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no diagnostics were produced, so this test proved nothing")
	}
}

func square() []vnx.Position {
	return []vnx.Position{pos(0, 0, nil), pos(1, 0, nil), pos(1, 1, nil), pos(0, 1, nil), pos(0, 0, nil)}
}

// The two geometry codes say different things and the difference is the whole
// reason the second one exists (vnx.CodePartialGeometry, ruled 2026-09-02).
//
//	CodeItemNoGeometry  "%q has no usable geometry — dropped"
//	CodePartialGeometry "part of %q could not be placed — the rest was exported"
//
// ⚠ THE TEST IS WHETHER THE THING Args[0] NAMES IS IN THE FILE. Getting this
// backwards is not a crash and not an invalid file: it is a true-looking
// sentence about the wrong item, which no schema check and no golden can see.
// So each case asserts the code AND whether the named item survived.
func TestPartialLossAndWholeLossUseDifferentCodes(t *testing.T) {
	good := vnx.Segment{Pips: []vnx.Position{pos(0, 0, nil), pos(1, 1, nil)}}
	stub := vnx.Segment{Pips: []vnx.Position{pos(2, 2, nil)}}

	t.Run("track keeps its name in the file when part of it is lost", func(t *testing.T) {
		out, report := writeDoc(t, &vnx.Document{Tracks: []vnx.Track{
			{Name: "Half a walk", Segments: []vnx.Segment{good, stub}},
		}})
		if !strings.Contains(out, `"name":"Half a walk"`) {
			t.Fatalf("the track was exported, so it must be in the file: %s", out)
		}
		wantDiag(t, report.Diagnostics, 0, vnx.CodePartialGeometry, "/tracks/0/segments/1", "Half a walk")
	})

	t.Run("track that lost everything is the whole-item code", func(t *testing.T) {
		out, report := writeDoc(t, &vnx.Document{Tracks: []vnx.Track{
			{Name: "No walk", Segments: []vnx.Segment{stub}},
		}})
		if strings.Contains(out, "No walk") {
			t.Fatalf("the track is diagnosed as dropped, so it must NOT be in the file: %s", out)
		}
		wantDiag(t, report.Diagnostics, 0, vnx.CodeItemNoGeometry, "/tracks/0", "No walk")
	})

	t.Run("network keeps its name when one arc is lost", func(t *testing.T) {
		out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
			Name: "Net",
			Arcs: map[string]vnx.Arc{
				"good": {Pips: []vnx.Position{pos(0, 0, nil), pos(1, 1, nil)}},
				"stub": {Pips: []vnx.Position{pos(2, 2, nil)}},
			},
		}}})
		if !strings.Contains(out, `"name":"Net"`) {
			t.Fatalf("the network was exported: %s", out)
		}
		wantDiag(t, report.Diagnostics, 0, vnx.CodePartialGeometry, "/networks/0/arcs/stub", "Net")
	})

	t.Run("chain keeps its name when one link names a missing arc", func(t *testing.T) {
		out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
			Name:  "Net",
			Nodes: map[string]vnx.Position{"n1": pos(0, 0, nil), "n2": pos(1, 1, nil)},
			Arcs: map[string]vnx.Arc{
				"a": {From: "n1", To: "n2", Pips: []vnx.Position{pos(0, 0, nil), pos(1, 1, nil)}},
			},
			Chains: []vnx.Chain{{Name: "Traverse", Links: []vnx.Link{
				{Arc: "a", From: node("n1"), To: node("n2")},
				{Arc: "vanished", From: node("n2"), To: node("n1")},
			}}},
		}}})
		if !strings.Contains(out, `"name":"Net / Traverse"`) {
			t.Fatalf("the chain still draws its remaining link, so it is in the file: %s", out)
		}
		wantDiag(t, report.Diagnostics, 0, vnx.CodePartialGeometry, "/networks/0/chains/0/links/1", "Traverse")
	})

	// ⚠ AN IN-NETWORK AREA IS THE WHOLE-ITEM CODE even though the network
	// survives, and this case is here so nobody "fixes" it into the partial
	// one. Args[0] is the AREA's name and none of the area could be placed.
	t.Run("an in-network area that is wholly unplaceable is the whole-item code", func(t *testing.T) {
		out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
			Name:  "Net",
			Arcs:  map[string]vnx.Arc{"a": {Pips: []vnx.Position{pos(0, 0, nil), pos(1, 1, nil)}}},
			Areas: []vnx.Area{{Name: "Sliver", Polygon: []vnx.Position{pos(0, 0, nil), pos(1, 0, nil), pos(0, 0, nil)}}},
		}}})
		if strings.Contains(out, "Sliver") {
			t.Fatalf("the area is diagnosed as dropped, so it must NOT be in the file: %s", out)
		}
		wantDiag(t, report.Diagnostics, 0, vnx.CodeItemNoGeometry, "/networks/0/areas/0/polygon", "Sliver")
	})
}
