package kml

import (
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// One test per non-✅ cell of the KML column of engine-spec §8 (spec 5b §6.3).
//
// ⚠ A CELL NOBODY TESTED IS A GUESS IN A TABLE THAT LOOKS AUTHORITATIVE. Each
// test below asserts that the loss is EXACTLY the stated loss — that what the
// cell says is carried IS carried, and what it says is lost IS lost. A test
// that only checked the second half would pass on a writer that dropped the
// item entirely.
//
// ⚠ SIX ROWS OF §8 HAVE NO TEST HERE AND CANNOT: plans, offline maps, durable
// identity, in-progress recording, app settings and download-job state are ❌
// in the VNX column too, so a vnx.Document cannot hold one and there is nothing
// for a writer test to construct. That is the plan's own rescoping of T11, not
// an omission here.

// Row: Network topology — "a Folder holding an arcs Placemark and one named
// Placemark per in-network point, area and highlight; SHARING lost".
func TestMatrixNetworkTopologyLosesOnlyTheSharing(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	// CARRIED: every arc, once, in one Placemark; every in-network item and
	// every highlight, named, with its own style and visibility. Asserted in
	// write_test.go's network tests.
	//
	// ⚠ TWICE, NOT ONCE, AND ON PURPOSE (ruled 2026-09-03). The Folder carries
	// the network's name and the arcs Placemark repeats it: it mirrors GeoJSON,
	// where the arcs feature IS the network, and an unnamed Placemark renders in
	// Google Earth as a blank sidebar row nobody can identify or search for.
	// This is not redundancy to tidy away.
	if n := countOf(out, "<name>Lairig Ghru</name>"); n != 2 {
		t.Errorf("the network name appears %d times, want 2 (its Folder and its arcs Placemark)", n)
	}
	if n := countOf(out, "<MultiGeometry>"); n != 2 {
		t.Errorf("<MultiGeometry> count = %d, want 2 (the fixture track and the network's arcs)", n)
	}
	// LOST: the graph. Nothing in the output says which arcs a highlight traverses,
	// or that two highlights traverse the same one — a highlight's line is a flat list
	// of coordinates, and the arc ids appear nowhere.
	for _, id := range []string{"a-north", "a-south"} {
		if strings.Contains(out, id) {
			t.Errorf("arc id %q reached the file — KML has no way to express arc identity, and putting one there would be an invented extension (§2.3)", id)
		}
	}
	if strings.Contains(out, "<Link>") || strings.Contains(out, "<walk") || strings.Contains(out, "pinned") {
		t.Errorf("the traversal structure reached the file\n%s", out)
	}
}

// Row: Route — "one path, boundaries lost".
func TestMatrixRouteLosesItsArcBoundaries(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Routes: []vnx.Route{route(vnx.Route{Name: "Ridge"},
		lineArc{Pips: []vnx.Position{pos(0, 0), pos(0, 1)}},
		lineArc{Pips: []vnx.Position{pos(0, 1), pos(0, 2)}},
		lineArc{Pips: []vnx.Position{pos(0, 2), pos(0, 3)}},
	)}})
	// CARRIED: every position, in order, joined at the shared endpoints.
	if !strings.Contains(out, "<coordinates>0,0 0,1 0,2 0,3</coordinates>") {
		t.Fatalf("the route's geometry did not survive as one path\n%s", out)
	}
	// LOST: that it was three arcs. One LineString, not three, and no marker of
	// where one ended.
	if n := countOf(out, "<LineString>"); n != 1 {
		t.Errorf("<LineString> count = %d, want 1 — a route is one path in KML", n)
	}
	if report.Emitted != 1 {
		t.Errorf("Emitted = %d, want 1", report.Emitted)
	}
}

// Row: Folders — "Folder, nesting only".
//
// ⚠ THIS CELL IS NOW UNDERSTATED AND T11 SHOULD RATIFY IT WIDER. <Folder> is a
// KML Feature, so it carries <name>, <description> and <visibility> at no cost,
// and this writer emits all three. What is genuinely lost is the folder's ID
// and its data records — nothing a reader of a KML file could use.
func TestMatrixFolderKeepsTreeNameNotesAndVisibilityAndLosesIdAndData(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{{
			ID: "f-secret-id", Name: "Cairngorms", Notes: "the big ones", Hidden: true,
			Data: []vnx.Record{{Type: "app.viatrail.tag", Body: map[string]any{"value": "scotland"}}},
		}},
		Waypoints: []vnx.Waypoint{{Folder: "f-secret-id", Name: "W", At: pos(1, 2)}},
	})
	for _, want := range []string{"<name>Cairngorms</name>", "<description>the big ones</description>", "<visibility>0</visibility>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\n%s", want, out)
		}
	}
	if strings.Contains(out, "f-secret-id") {
		t.Errorf("the folder's id reached the file — KML has nowhere to put one\n%s", out)
	}
	if strings.Contains(out, "scotland") {
		t.Errorf("a data record reached the file — that would be an <ExtendedData> extension (§2.3)\n%s", out)
	}
}

// Row: Styles — "per-item Style, partial".
func TestMatrixStyleCarriesColourAndWidthAndLosesTheRest(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{
			Name: "W", At: pos(1, 2),
			Style: vnx.Style{"color": "#E04535", "icon": "mountain-hut", "icon-size": 27, "pin-shape": "teardrop", "label-position": "below", "label-visible": true, "importance": 5},
		}},
		Routes: []vnx.Route{route(vnx.Route{
			Name:  "R",
			Style: vnx.Style{"color": "#3A6FB0", "width": 4.0, "line-style": "dashed"}},
			lineArc{Pips: []vnx.Position{pos(0, 0), pos(0, 1)}},
		)},
	})
	// CARRIED: icon colour, line colour, line width.
	for _, want := range []string{"<IconStyle>", "<color>ff3545e0</color>", "<color>ffb06f3a</color>", "<width>4</width>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — the cell says a Style IS written\n%s", want, out)
		}
	}
	// LOST: everything else. KML has no vocabulary for our icon names, pin
	// shapes, label rules or importance, and inventing one would be §2.3's
	// forbidden extension.
	for _, gone := range []string{"mountain-hut", "teardrop", "below", "dashed", "27", "Importance", "importance"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; the cell says the Style is PARTIAL\n%s", gone, out)
		}
	}
}

// Row: Tags — "❌".
func TestMatrixTagsAreNotCarried(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Waypoints: []vnx.Waypoint{{
		Name: "W", At: pos(1, 2),
		Data: []vnx.Record{
			{Type: "app.viatrail.tag", Body: map[string]any{"value": "munro"}},
			{Type: "app.viatrail.tag", Body: map[string]any{"value": "bagged"}},
		},
	}}})
	for _, gone := range []string{"munro", "bagged", "app.viatrail.tag", "ExtendedData"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file\n%s", gone, out)
		}
	}
	// ⚠ AND NO DIAGNOSTIC. An attribute lost on every item in the library would
	// be hundreds of rows nobody can act on; it is a matrix fact, disclosed by
	// the export screen, not a per-item diagnostic (vnx/writer.go rule 5).
	if len(report.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none — a per-item tag diagnostic would bury the real casualties", codes(report))
	}
}

// Row: Routing config — "❌". Same mechanism as tags: it is a data record.
func TestMatrixRoutingConfigIsNotCarried(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Routes: []vnx.Route{route(vnx.Route{Name: "R"},
		lineArc{
			Pips: []vnx.Position{pos(0, 0), pos(0, 1)},
			Data: []vnx.Record{{Type: "app.viatrail.routing", Body: map[string]any{"profile": "walking"}}},
		},
	)}})
	for _, gone := range []string{"app.viatrail.routing", "walking", "profile"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file\n%s", gone, out)
		}
	}
}

// Row: Hidden state — "<visibility>, on every named thing".
//
// ⚠ THIS CELL GOT WIDER ON 2026-09-03 AND THE TEST SAYS SO. It used to be a ⚠:
// an in-network point or area rode as bare geometry inside the network's shared
// MultiGeometry, and a MultiGeometry member cannot carry a <visibility> — so
// their hidden state was destroyed. Each is its own Placemark now and keeps it.
func TestMatrixHiddenIsCarriedOnEveryNamedThingIncludingInNetworkItems(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	// CARRIED: on every Placemark and Folder that has one of its own — an empty
	// folder, a waypoint, a highlight, and the in-network area that used to lose it.
	if n := countOf(out, "<visibility>0</visibility>"); n != 4 {
		t.Errorf("<visibility>0</visibility> count = %d, want 4 (a folder, a waypoint, a highlight, an in-network area)\n%s", n, out)
	}
	// The arcs are the ONE thing left that cannot carry visibility, and they
	// carry none of their own to lose: vnx.Arc has no Hidden field.
	multi := arcsMultiGeometry(t, out)
	if strings.Contains(multi, "<visibility>") {
		t.Errorf("a geometry inside a MultiGeometry cannot carry visibility, yet one was written\n%s", multi)
	}
	// The hidden in-network area is still DRAWN as well as marked. Hiding is a
	// state, not a reason to drop geometry.
	folder := networkFolder(t, out)
	if !strings.Contains(folder, "-3.685,57.03") {
		t.Errorf("the hidden in-network area's geometry was dropped\n%s", folder)
	}
}

// Row: Timestamps — "❌".
func TestMatrixTimestampsAreNotCarried(t *testing.T) {
	started := mustTime(t, "2024-05-01T09:00:00Z")
	ended := mustTime(t, "2024-05-01T17:00:00Z")
	out, report := writeDoc(t, &vnx.Document{Tracks: []vnx.Track{{
		Name: "T", Started: vnx.TimestampOf(started), Ended: vnx.TimestampOf(ended),
		Segments: []vnx.Segment{{Pips: []vnx.Position{
			posTime(0, 0, 1_714_554_000_000), posTime(0, 1, 1_714_582_800_000),
		}}},
	}}})
	for _, gone := range []string{"2024", "TimeStamp", "TimeSpan", "1714554000000", "<when>"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file — KML 2.2 core has no place for a pip time in a LineString\n%s", gone, out)
		}
	}
	if report.Emitted != 1 {
		t.Errorf("Emitted = %d, want 1 — the track itself is carried, only its times are lost", report.Emitted)
	}
}

// Row: Moving time — "❌". It is derived from pip times, which are not carried,
// so nothing downstream can recompute it.
func TestMatrixMovingTimeIsNotCarried(t *testing.T) {
	d := &vnx.Document{Tracks: []vnx.Track{{
		Name: "T", Segments: []vnx.Segment{{Pips: []vnx.Position{
			posTime(0, 0, 1_714_554_000_000), posTime(0, 1, 1_714_557_600_000),
		}}},
	}}}
	if _, known := d.Tracks[0].MovingTime(); !known {
		t.Fatal("the fixture track has no moving time, so this test would prove nothing")
	}
	out, _ := writeDoc(t, d)
	for _, gone := range []string{"3600", "duration", "movingTime", "moving_time"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file\n%s", gone, out)
		}
	}
}

// Row: Application keys — "❌", dropped and DIAGNOSED. This is the one ❌ row
// in the KML column that raises a per-item diagnostic, because an application
// key is a whole kind of content, not an attribute of something else.
func TestMatrixAppKeyIsDroppedAndDiagnosed(t *testing.T) {
	keys := []vnx.AppKey{
		{Key: "app.viatrail.rulesets", Value: []any{map[string]any{"id": "r1"}}},
		{Key: "com.example.r2", Value: nil},
	}
	out, report := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "W", At: pos(1, 2)}},
		AppKeys:   keys,
	})
	if strings.Contains(out, "r1") || strings.Contains(out, "r2") {
		t.Errorf("an application key reached the file\n%s", out)
	}
	got := codes(report)
	if len(got) != 2 || got[0] != vnx.CodeAppKeyUnsupported || got[1] != vnx.CodeAppKeyUnsupported {
		t.Fatalf("diagnostics = %v, want two %s", got, vnx.CodeAppKeyUnsupported)
	}
	for i, dg := range report.Diagnostics {
		// Args[0] is the key: it is the application key's only name.
		if len(dg.Args) != 1 || dg.Args[0] != keys[i].Key {
			t.Errorf("application-key diagnostic %d carries Args %v, want [%s]", i, dg.Args, keys[i].Key)
		}
		if dg.Path != "/"+keys[i].Key {
			t.Errorf("application-key diagnostic %d has Path %q, want /%s", i, dg.Path, keys[i].Key)
		}
	}
}

// Rows: Track, Waypoint, Area — the ✅ cells. They are asserted too, because a
// ✅ that nobody checked is the same guess as a ⚠ that nobody checked.
func TestMatrixTrackPausesWaypointsAndAreasAreCarriedWhole(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	track := out[strings.Index(out, "<name>Walk In</name>"):]
	track = track[:strings.Index(track, "</Placemark>")]
	if n := countOf(track, "<LineString>"); n != 2 {
		t.Errorf("the track has %d LineStrings, want one per segment — concatenating them draws a line across the lunch stop", n)
	}
	if !strings.Contains(out, "<Polygon>") || !strings.Contains(out, "<outerBoundaryIs>") {
		t.Errorf("the area is not a Polygon\n%s", out)
	}
	if !strings.Contains(out, "<description>swim here</description>") {
		t.Errorf("notes did not survive as <description>\n%s", out)
	}
}
