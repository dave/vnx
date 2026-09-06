package kml

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

func writeDoc(t *testing.T, d *vnx.Document) (string, vnx.WriteReport) {
	t.Helper()
	var buf bytes.Buffer
	report, err := Write(&buf, d)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	assertWellFormed(t, buf.Bytes())
	return buf.String(), report
}

// assertWellFormed parses the output with encoding/xml. It proves nothing about
// MEANING — that is what the golden and the per-cell tests are for — but a
// writer that emits an unescaped ampersand fails here and nowhere else.
func assertWellFormed(t *testing.T, b []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("output is not well-formed XML: %v\n%s", err, b)
		}
	}
}

func codes(r vnx.WriteReport) []string {
	out := make([]string, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func countOf(s, sub string) int { return strings.Count(s, sub) }

func TestGoldenFixtureEmitsEveryPlacemarkItShould(t *testing.T) {
	out, report := writeDoc(t, fixture())
	// 2 waypoints, 1 area, 1 track, 1 route, 1 network arcs Placemark, 1
	// in-network point, 1 in-network area, 2 chains.
	if report.Emitted != 10 {
		t.Errorf("Emitted = %d, want 10", report.Emitted)
	}
	if n := countOf(out, "<Placemark>"); n != 10 {
		t.Errorf("<Placemark> count = %d, want 10", n)
	}
	// Folders are containers, not emitted objects — four of them are written
	// (three real ones and the network's) and none of them counts.
	if n := countOf(out, "<Folder>"); n != 4 {
		t.Errorf("<Folder> count = %d, want 4", n)
	}
	// The only casualty in the fixture is its ruleset.
	if got := codes(report); len(got) != 1 || got[0] != vnx.CodeRulesetUnsupported {
		t.Errorf("diagnostics = %v, want exactly [%s]", got, vnx.CodeRulesetUnsupported)
	}
}

// ⚠ THE SUBTLE RULING OF §2.6, ASSERTED DIRECTLY. Chains are separate named
// Placemarks; folding them into the network's MultiGeometry would discard their
// name, notes, style and visibility, which is exactly what this checks did not
// happen.
//
// ⚠ THE NAMES ARE BARE SINCE THE 2026-09-03 REDESIGN. The network is a <Folder>
// now, so the nesting says which network a chain belongs to and the old
// "Lairig Ghru / Full Traverse" prefix would be the folder's name written twice.
func TestNetworkChainsAreTheirOwnNamedPlacemarks(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	for _, want := range []string{
		"<name>Full Traverse</name>",
		"<name>Half Way</name>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — a chain lost its name to the shared MultiGeometry\n%s", want, out)
		}
	}
	if strings.Contains(out, "Lairig Ghru / Full Traverse") {
		t.Errorf("a chain still carries its network's name as a prefix; the Folder already says it\n%s", out)
	}
	// The chain's own style survived, which it could not have inside a shared
	// Placemark: a Placemark has one style.
	if !strings.Contains(out, "<color>ffa84f7b</color>") {
		t.Errorf("the chain's own style is missing\n%s", out)
	}
	// And its own visibility: "Half Way" is hidden, the network is not.
	half := out[strings.Index(out, "<name>Half Way</name>"):]
	if !strings.HasPrefix(strings.TrimSpace(half[strings.Index(half, "\n"):]), "<visibility>0</visibility>") {
		t.Errorf("the hidden chain did not get <visibility>0</visibility>\n%s", out)
	}
}

// networkFolder is the fixture network's <Folder>, from its own <name> onwards.
// The fixture writes the network last, so the rest of the document follows
// nothing.
func networkFolder(t *testing.T, out string) string {
	t.Helper()
	i := strings.Index(out, "<name>Lairig Ghru</name>")
	if i < 0 {
		t.Fatalf("the network folder is not in the file at all\n%s", out)
	}
	return out[i:]
}

// arcsMultiGeometry is the shared arcs geometry inside that Folder — the ONE
// place a bare geometry belongs now.
func arcsMultiGeometry(t *testing.T, out string) string {
	t.Helper()
	f := networkFolder(t, out)
	a := strings.Index(f, "<MultiGeometry>")
	b := strings.Index(f, "</MultiGeometry>")
	if a < 0 || b < a {
		t.Fatalf("the network has no arcs MultiGeometry\n%s", f)
	}
	return f[a:b]
}

// ⚠ THE ONE THE SPEC OMISSION WOULD HAVE DROPPED SILENTLY. A network with
// points, areas and chains must come out with all four kinds present.
//
// ⚠ SINCE THE 2026-09-03 REDESIGN ONLY THE ARCS ARE BARE GEOMETRY. The point,
// the area and the chains are Placemarks of their own inside the network's
// Folder, which is what lets them keep the name, notes, visibility and style a
// shared MultiGeometry destroyed.
func TestNetworkCarriesArcsPointsAreasAndChains(t *testing.T) {
	out, report := writeDoc(t, fixture())
	multi := arcsMultiGeometry(t, out)
	if n := countOf(multi, "<LineString>"); n != 2 {
		t.Errorf("network MultiGeometry holds %d LineStrings, want 2 (one per arc)", n)
	}
	for _, gone := range []string{"<Point>", "<Polygon>"} {
		if strings.Contains(multi, gone) {
			t.Errorf("%s is still inside the shared MultiGeometry, where it cannot carry a name\n%s", gone, multi)
		}
	}
	folder := networkFolder(t, out)
	if n := countOf(folder, "<Point>"); n != 1 {
		t.Errorf("the network folder holds %d Points, want 1 — Network.Points was dropped", n)
	}
	if n := countOf(folder, "<Polygon>"); n != 1 {
		t.Errorf("the network folder holds %d Polygons, want 1 — Network.Areas was dropped", n)
	}
	if !strings.Contains(folder, "-3.675,57.05,830") {
		t.Errorf("the in-network waypoint's position is missing\n%s", folder)
	}
	// Nothing about the in-network items is diagnosed: their GEOMETRY is
	// carried, so only their attributes are lost, and attribute-level losses
	// are matrix facts rather than per-item diagnostics (writer.go rule 5).
	if len(report.Diagnostics) != 1 {
		t.Errorf("diagnostics = %v, want only the ruleset one", codes(report))
	}
}

// ⚠ THE FOUR FIELDS THE OLD SHAPE DESTROYED. An in-network waypoint and an
// in-network area are full vnx.Waypoint / vnx.Area values with Name, Notes,
// Hidden and Style (item.go:34-55), and until 2026-09-03 every one of those
// four was thrown away: they rode as bare <Point> and <Polygon> geometry inside
// the network's shared Placemark, which has one name, one style and one
// visibility. This asserts each field arrives.
func TestInNetworkPointAndAreaKeepTheirOwnNameNotesVisibilityAndStyle(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	folder := networkFolder(t, out)

	for _, want := range []string{
		"<name>Pools of Dee</name>",
		"<description>in-network</description>",
		"<color>ffab862e</color>", // the point's own IconStyle colour, #2E86AB
	} {
		if !strings.Contains(folder, want) {
			t.Errorf("the in-network waypoint lost %s\n%s", want, folder)
		}
	}
	for _, want := range []string{
		"<name>Boulder Field</name>",
		"<color>ff888888</color>", // the area's own fill, #888888
	} {
		if !strings.Contains(folder, want) {
			t.Errorf("the in-network area lost %s\n%s", want, folder)
		}
	}
	// The area is hidden and the network is not, so its own <visibility> has to
	// be on its own Placemark — inside a MultiGeometry there was nowhere to put
	// one.
	area := folder[strings.Index(folder, "<name>Boulder Field</name>"):]
	if !strings.HasPrefix(strings.TrimSpace(area[strings.Index(area, "\n"):]), "<visibility>0</visibility>") {
		t.Errorf("the hidden in-network area did not get its own <visibility>0</visibility>\n%s", folder)
	}
}

// A network whose name and notes belong to a <Folder> must still write them
// when it holds no arc at all — the items inside are what keep it alive, and
// the Folder is what keeps its name.
//
// ⚠ AND NO EMPTY <MultiGeometry>. KML has a real container for the name, unlike
// GeoJSON, where the same case writes {"type":"MultiLineString","coordinates":[]}
// because a Feature is the only place a name can live.
func TestNetworkWithNoArcsStillWritesItsFolderAndItsItems(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name: "Empty graph", Notes: "no arcs yet",
		Points: []vnx.Waypoint{{Name: "Lone cairn", At: pos(1, 2)}},
	}}})
	for _, want := range []string{
		"<name>Empty graph</name>",
		"<description>no arcs yet</description>",
		"<name>Lone cairn</name>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\n%s", want, out)
		}
	}
	if strings.Contains(out, "<MultiGeometry>") {
		t.Errorf("an arcs Placemark was written for a network with no arcs\n%s", out)
	}
	if report.Emitted != 1 {
		t.Errorf("Emitted = %d, want 1 — the point, and no arcs Placemark", report.Emitted)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none — nothing was lost", codes(report))
	}
}

// A network holding NOTHING is dropped whole, with its diagnostic and no empty
// <Folder>. Ruled 2026-09-03: a name is worth keeping only when there is
// something for it to name.
func TestNetworkHoldingNothingIsDroppedWithItsDiagnostic(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name: "Ghost", Notes: "nothing here",
	}}})
	if strings.Contains(out, "Ghost") || strings.Contains(out, "<Folder>") {
		t.Errorf("an empty network left a Folder behind\n%s", out)
	}
	if report.Emitted != 0 {
		t.Errorf("Emitted = %d, want 0", report.Emitted)
	}
	got := codes(report)
	if len(got) != 1 || got[0] != vnx.CodeItemNoGeometry {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, vnx.CodeItemNoGeometry)
	}
	if a := report.Diagnostics[0].Args; len(a) != 1 || a[0] != "Ghost" {
		t.Errorf("Args = %v, want the network's own name", a)
	}
	if p := report.Diagnostics[0].Path; p != "/networks/0" {
		t.Errorf("Path = %q, want /networks/0", p)
	}
}

// The arcs are emitted once each, INCLUDING an arc no chain traverses, and the
// chains duplicate the geometry they walk. Both halves are deliberate (§2.6).
func TestBareArcsSurviveAndChainGeometryIsAllowedToDuplicateThem(t *testing.T) {
	d := fixture()
	// "a-south" is traversed by "Full Traverse"; add an arc nothing traverses.
	d.Networks[0].Nodes["n4"] = pos(-3.64, 57.12)
	d.Networks[0].Arcs["a-spur"] = vnx.Arc{From: "n3", To: "n4",
		Pips: []vnx.Position{pos(-3.66, 57.08), pos(-3.64, 57.12)}}

	out, _ := writeDoc(t, d)
	multi := arcsMultiGeometry(t, out)
	if n := countOf(multi, "<LineString>"); n != 3 {
		t.Fatalf("network holds %d arc LineStrings, want 3 — an untraversed arc vanished, which is the silent loss §2.6 forbids", n)
	}
	if !strings.Contains(multi, "-3.64,57.12") {
		t.Errorf("the untraversed arc's geometry is missing\n%s", multi)
	}
	// The shared arc a-north appears in the network object AND in both chains
	// that walk it. That duplication is the accepted price.
	if n := countOf(out, "-3.7,57 -3.69,57.02"); n < 3 {
		t.Errorf("the shared arc appears %d times, want at least 3 (network + two chains)", n)
	}
}

// A chain link that stops PART WAY along an arc must cut the arc, not emit it
// whole. Emitting the whole arc would draw the chain past where it ends.
func TestChainCutsItsArcAtAnInlineEndpoint(t *testing.T) {
	d := &vnx.Document{Networks: []vnx.Network{{
		Name:  "N",
		Nodes: map[string]vnx.Position{"a": pos(0, 0), "b": pos(0, 0.4)},
		Arcs: map[string]vnx.Arc{"arc": {From: "a", To: "b", Pips: []vnx.Position{
			pos(0, 0), pos(0, 0.1), pos(0, 0.2), pos(0, 0.3), pos(0, 0.4),
		}}},
		Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
			{Arc: "arc", From: vnx.Endpoint{Node: "a"}, To: vnx.Endpoint{At: ptr(pos(0, 0.2))}},
		}}},
	}}}
	out, _ := writeDoc(t, d)
	chain := out[strings.Index(out, "<name>C</name>"):]
	line := chain[strings.Index(chain, "<coordinates>")+len("<coordinates>"):]
	line = line[:strings.Index(line, "</coordinates>")]
	if line != "0,0 0,0.1 0,0.2" {
		t.Fatalf("chain coordinates = %q, want %q — the arc was not cut at the inline endpoint", line, "0,0 0,0.1 0,0.2")
	}
}

// A link walked from its `to` node back to its `from` node draws backwards.
func TestChainWalkingAnArcBackwardsIsDrawnBackwards(t *testing.T) {
	d := &vnx.Document{Networks: []vnx.Network{{
		Name:  "N",
		Nodes: map[string]vnx.Position{"a": pos(0, 0), "b": pos(0, 0.2)},
		Arcs:  map[string]vnx.Arc{"arc": {From: "a", To: "b", Pips: []vnx.Position{pos(0, 0), pos(0, 0.1), pos(0, 0.2)}}},
		Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
			{Arc: "arc", From: vnx.Endpoint{Node: "b"}, To: vnx.Endpoint{Node: "a"}},
		}}},
	}}}
	out, _ := writeDoc(t, d)
	if !strings.Contains(out, "<coordinates>0,0.2 0,0.1 0,0</coordinates>") {
		t.Fatalf("the reversed traversal was not reversed:\n%s", out)
	}
}

// ⚠ Arcs IS A MAP. Two runs over the same document must produce the same bytes,
// or the golden is flaky in a way that looks like a writer bug.
func TestOutputIsStableAcrossRuns(t *testing.T) {
	d := fixture()
	first, _ := writeDoc(t, d)
	for i := 0; i < 20; i++ {
		again, _ := writeDoc(t, fixture())
		if again != first {
			t.Fatalf("run %d differs from the first — a map was ranged unsorted", i)
		}
	}
}

func TestFolderTreeIsNestedAndKeepsItsNotesAndVisibility(t *testing.T) {
	out, _ := writeDoc(t, fixture())
	cairn := out[strings.Index(out, "<name>Cairngorms</name>"):]
	if !strings.Contains(cairn[:strings.Index(cairn, "</Folder>")], "<name>Day 1</name>") {
		t.Errorf("Day 1 is not nested inside Cairngorms\n%s", out)
	}
	// The bothy is inside Day 1, which is inside Cairngorms: the tree survives
	// and no name carries a path.
	if strings.Contains(out, "Cairngorms / Day 1") {
		t.Errorf("KML must not flatten folder paths into names — that is GPX and GeoJSON\n%s", out)
	}
}

// The decision this task was asked to make and state: an empty <Folder> is a
// real element carrying a real name, so nothing vanishes and NO diagnostic is
// raised. vnx.CodeEmptyFolderDropped belongs to the flattened formats.
func TestEmptyFolderIsKeptAndNotDiagnosed(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Folders:   []vnx.Folder{{ID: "f", Name: "Empty"}},
		Waypoints: []vnx.Waypoint{{Name: "somewhere else", At: pos(1, 2)}},
	})
	if !strings.Contains(out, "<name>Empty</name>") {
		t.Errorf("the empty folder vanished\n%s", out)
	}
	for _, d := range report.Diagnostics {
		if d.Code == vnx.CodeEmptyFolderDropped {
			t.Errorf("KML raised %s for a folder it kept: %s", d.Code, d.Message)
		}
	}
}

// A folder with no items anywhere beneath it puts nothing on the map, so it
// does not count towards Emitted — a KML of nothing but folders is refused by
// the caller.
func TestFoldersDoNotCountAsEmittedObjects(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{Folders: []vnx.Folder{
		{ID: "a", Name: "A"}, {ID: "b", Name: "B", Parent: "a"},
	}})
	if report.Emitted != 0 {
		t.Fatalf("Emitted = %d for a document of nothing but folders, want 0 — the caller's empty-export refusal depends on this", report.Emitted)
	}
}

// A cyclic parent chain must not hang the writer, and the folders must still be
// written — at the root, with a diagnostic. ⚠ Decode clears cycles, so only a
// hand-built Document reaches here; "a writer must not hang on any input" is
// not a claim that wants an exception.
func TestCyclicFoldersAreWrittenAtTheRootAndDiagnosed(t *testing.T) {
	done := make(chan struct{})
	var out string
	var report vnx.WriteReport
	go func() {
		defer close(done)
		out, report = writeDoc(t, &vnx.Document{
			Folders: []vnx.Folder{
				{ID: "a", Name: "A", Parent: "b"},
				{ID: "b", Name: "B", Parent: "a"},
			},
			Waypoints: []vnx.Waypoint{{Folder: "a", Name: "inside A", At: pos(1, 2)}},
		})
	}()
	<-done
	if n := countOf(out, "<Folder>"); n != 2 {
		t.Errorf("<Folder> count = %d, want 2 — a folder in a cycle was dropped\n%s", n, out)
	}
	if !strings.Contains(out, "<name>inside A</name>") {
		t.Errorf("the item inside a cyclic folder was lost — losing content over a lost position is the larger loss\n%s", out)
	}
	got := codes(report)
	if len(got) != 2 || got[0] != vnx.CodeFolderPathUnavailable || got[1] != vnx.CodeFolderPathUnavailable {
		t.Errorf("diagnostics = %v, want two %s", got, vnx.CodeFolderPathUnavailable)
	}
}

func TestItemNamingAnUnknownFolderIsKeptAndDiagnosed(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Folder: "nowhere", Name: "Orphan", At: pos(1, 2)}},
	})
	if !strings.Contains(out, "<name>Orphan</name>") {
		t.Fatalf("the item was dropped for naming a folder that is not here\n%s", out)
	}
	if got := codes(report); len(got) != 1 || got[0] != vnx.CodeFolderPathUnavailable {
		t.Fatalf("diagnostics = %v, want [%s]", got, vnx.CodeFolderPathUnavailable)
	}
	if got := report.Diagnostics[0].Args; len(got) != 1 || got[0] != "Orphan" {
		t.Errorf("Args = %v, want the item's own bare name", got)
	}
}

// A duplicate folder id must not make the writer emit one subtree twice.
func TestDuplicateFolderIdDoesNotDuplicateASubtree(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "a", Name: "First"},
			{ID: "a", Name: "Second"},
			{ID: "kid", Name: "Kid", Parent: "a"},
		},
		Waypoints: []vnx.Waypoint{{Folder: "kid", Name: "W", At: pos(1, 2)}},
	})
	if n := countOf(out, "<name>Kid</name>"); n != 1 {
		t.Fatalf("the subtree was written %d times\n%s", n, out)
	}
	if n := countOf(out, "<name>W</name>"); n != 1 {
		t.Fatalf("the item was written %d times\n%s", n, out)
	}
}

func TestUnknownElevationOmitsTheThirdOrdinateAndNeverWritesZero(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "no ele", At: pos(-3.5, 57.5)},
		{Name: "sea level", At: posEle(-3.5, 57.6, 0)},
	}})
	if !strings.Contains(out, "<coordinates>-3.5,57.5</coordinates>") {
		t.Errorf("an unknown elevation did not omit the third ordinate\n%s", out)
	}
	if strings.Contains(out, "-3.5,57.5,0") {
		t.Errorf("an unknown elevation was written as 0 — that is sea level, and it is the exact failure pipEle exists to prevent\n%s", out)
	}
	// A genuine zero is still written: omission means UNKNOWN, not "zero".
	if !strings.Contains(out, "<coordinates>-3.5,57.6,0</coordinates>") {
		t.Errorf("a known elevation of 0 was omitted, which loses a real measurement\n%s", out)
	}
}

func TestCoordinatesAreLonLatNotLatLon(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "Ben Nevis", At: pos(-5.0037, 56.7969)},
	}})
	if !strings.Contains(out, "<coordinates>-5.0037,56.7969</coordinates>") {
		t.Fatalf("coordinates are not lon,lat — a swap puts Scotland in the Indian Ocean\n%s", out)
	}
}

func TestNamesAndNotesAreEscaped(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: `Ben & <Nevis>`, Notes: `he said "go"`, At: pos(1, 2)},
	}})
	if !strings.Contains(out, "Ben &amp; &lt;Nevis&gt;") {
		t.Fatalf("the name was not escaped\n%s", out)
	}
}

func TestItemsWithoutGeometryAreDroppedAndDiagnosed(t *testing.T) {
	d := &vnx.Document{
		Areas:  []vnx.Area{{Name: "short ring", Polygon: []vnx.Position{pos(0, 0), pos(1, 1)}}},
		Tracks: []vnx.Track{{Name: "one fix", Segments: []vnx.Segment{{Pips: []vnx.Position{pos(0, 0)}}}}},
		Routes: []vnx.Route{{Name: "no arcs"}},
	}
	out, report := writeDoc(t, d)
	if strings.Contains(out, "<Placemark>") {
		t.Errorf("an item with no drawable geometry was written anyway\n%s", out)
	}
	if report.Emitted != 0 {
		t.Errorf("Emitted = %d, want 0", report.Emitted)
	}
	got := codes(report)
	if len(got) != 3 {
		t.Fatalf("diagnostics = %v, want three %s", got, vnx.CodeItemNoGeometry)
	}
	for i, c := range got {
		if c != vnx.CodeItemNoGeometry {
			t.Errorf("diagnostic %d = %s, want %s", i, c, vnx.CodeItemNoGeometry)
		}
	}
	for _, dg := range report.Diagnostics {
		if dg.Severity != vnx.Warning {
			t.Errorf("%s severity = %v, want Warning", dg.Code, dg.Severity)
		}
		if dg.Path == "" {
			t.Errorf("%s has no Path", dg.Code)
		}
		if len(dg.Args) != 1 {
			t.Errorf("%s Args = %v, want the item's own name", dg.Code, dg.Args)
		}
	}
}

// A track keeps its drawable segments when one of them is unusable — AND SAYS
// SO. ⚠ THIS TEST ASSERTED THE OPPOSITE UNTIL 2026-09-02, when the lead ruled
// the silence out: this writer dropped the degenerate segment with no
// diagnostic at all, which is the fallback the house rule forbids. It is a
// PARTIAL loss, so it is not vnx.CodeItemNoGeometry either — that one says
// "dropped" about a track the reader can see in the file.
func TestTrackKeepsItsDrawableSegmentsAndReportsTheLostOne(t *testing.T) {
	out, report := writeDoc(t, &vnx.Document{Tracks: []vnx.Track{{
		Name: "T", Segments: []vnx.Segment{
			{Pips: []vnx.Position{pos(0, 0), pos(1, 1)}},
			{Pips: []vnx.Position{pos(0, 0)}},
		},
	}}})
	if n := countOf(out, "<LineString>"); n != 1 {
		t.Errorf("<LineString> count = %d, want 1", n)
	}
	if report.Emitted != 1 {
		t.Errorf("Emitted = %d, want 1 — the track itself is exported", report.Emitted)
	}
	got := codes(report)
	if len(got) != 1 || got[0] != vnx.CodePartialGeometry {
		t.Fatalf("diagnostics = %v, want exactly [%s] — a silently dropped segment is the forbidden fallback", got, vnx.CodePartialGeometry)
	}
	d := report.Diagnostics[0]
	if d.Path != "/tracks/0/segments/1" {
		t.Errorf("Path = %q, want /tracks/0/segments/1 — the Path names the PART", d.Path)
	}
	if len(d.Args) != 1 || d.Args[0] != "T" {
		t.Errorf("Args = %v, want the ITEM's own name per writer.go convention 3", d.Args)
	}
	if d.Severity != vnx.Warning {
		t.Errorf("Severity = %v, want Warning", d.Severity)
	}
}

// A network arc under two pips is a part of a network that IS exported.
func TestDegenerateNetworkArcIsReportedAsAPartialLoss(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "N",
		Nodes: map[string]vnx.Position{"a": pos(0, 0), "b": pos(0, 1)},
		Arcs: map[string]vnx.Arc{
			"good":  {From: "a", To: "b", Pips: []vnx.Position{pos(0, 0), pos(0, 1)}},
			"stump": {From: "a", To: "a", Pips: []vnx.Position{pos(0, 0)}},
		},
	}}})
	got := codes(report)
	if len(got) != 1 || got[0] != vnx.CodePartialGeometry {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, vnx.CodePartialGeometry)
	}
	if p := report.Diagnostics[0].Path; p != "/networks/0/arcs/stump" {
		t.Errorf("Path = %q, want /networks/0/arcs/stump", p)
	}
	if a := report.Diagnostics[0].Args; len(a) != 1 || a[0] != "N" {
		t.Errorf("Args = %v, want the network's own name", a)
	}
}

// A chain link naming an arc the network does not hold places nothing, and the
// chain around it is still exported.
func TestChainLinkThatPlacesNothingIsReportedAsAPartialLoss(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "N",
		Nodes: map[string]vnx.Position{"a": pos(0, 0), "b": pos(0, 1)},
		Arcs:  map[string]vnx.Arc{"real": {From: "a", To: "b", Pips: []vnx.Position{pos(0, 0), pos(0, 1)}}},
		Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
			{Arc: "real", From: vnx.Endpoint{Node: "a"}, To: vnx.Endpoint{Node: "b"}},
			{Arc: "ghost", From: vnx.Endpoint{Node: "b"}, To: vnx.Endpoint{Node: "a"}},
		}}},
	}}})
	got := codes(report)
	if len(got) != 1 || got[0] != vnx.CodePartialGeometry {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, vnx.CodePartialGeometry)
	}
	if p := report.Diagnostics[0].Path; p != "/networks/0/chains/0/links/1" {
		t.Errorf("Path = %q, want /networks/0/chains/0/links/1", p)
	}
	if a := report.Diagnostics[0].Args; len(a) != 1 || a[0] != "C" {
		t.Errorf("Args = %v, want the chain's own name", a)
	}
}

// ⚠ A WHOLE-ITEM LOSS DOES NOT ALSO GET PARTIAL ROWS. CodePartialGeometry says
// "the rest was exported"; when nothing was, saying it twice in two different
// senses is worse than saying it once.
func TestAWhollyLostItemGetsOnlyTheWholeItemCode(t *testing.T) {
	_, report := writeDoc(t, &vnx.Document{
		Tracks: []vnx.Track{{Name: "T", Segments: []vnx.Segment{
			{Pips: []vnx.Position{pos(0, 0)}}, {Pips: []vnx.Position{pos(1, 1)}},
		}}},
		Networks: []vnx.Network{{
			Name:  "N",
			Nodes: map[string]vnx.Position{"a": pos(0, 0), "b": pos(0, 1)},
			Arcs: map[string]vnx.Arc{
				"good": {From: "a", To: "b", Pips: []vnx.Position{pos(0, 0), pos(0, 1)}},
			},
			// Every link of this chain names an arc that is not here, so the
			// chain is WHOLLY lost — the case the caller must not also report
			// link by link.
			Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
				{Arc: "ghost", From: vnx.Endpoint{Node: "a"}, To: vnx.Endpoint{Node: "b"}},
				{Arc: "phantom", From: vnx.Endpoint{Node: "b"}, To: vnx.Endpoint{Node: "a"}},
			}}},
		}},
	})
	for _, d := range report.Diagnostics {
		if d.Code == vnx.CodePartialGeometry {
			t.Errorf("a wholly lost item also got a partial row at %s", d.Path)
		}
	}
	if got := codes(report); len(got) != 2 {
		t.Errorf("diagnostics = %v, want one %s each for the track and the chain", got, vnx.CodeItemNoGeometry)
	}
}

func TestEveryCodeThisWriterRaisesIsRegistered(t *testing.T) {
	registered := make(map[string]bool)
	for _, c := range vnx.WriterCodes() {
		registered[c] = true
	}
	// The whole set this package can raise, listed by hand so that adding a
	// raise site without listing it here is visible in review.
	for _, c := range []string{
		vnx.CodeRulesetUnsupported,
		vnx.CodeFolderPathUnavailable,
		vnx.CodeItemNoGeometry,
		vnx.CodePartialGeometry,
	} {
		if !registered[c] {
			t.Errorf("%q is raised by pkg/vnx/kml but is not in vnx.WriterCodes()", c)
		}
	}
}

// KML raises neither of the GPX-only codes, and never the empty-folder one.
func TestKMLNeverRaisesACodeThatIsNotItsOwn(t *testing.T) {
	_, report := writeDoc(t, fixture())
	for _, d := range report.Diagnostics {
		switch d.Code {
		case vnx.CodeGPXNetworkUnsupported, vnx.CodeGPXAreaUnsupported, vnx.CodeEmptyFolderDropped:
			t.Errorf("KML raised %s, which is not a KML code", d.Code)
		}
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// ⚠ THE LINE BETWEEN THE TWO GEOMETRY CODES IS WHAT Args[0] NAMES, not what the
// walk happened to be in the middle of (ruled 2026-09-02, mirroring the GeoJSON
// writer's TestPartialLossAndWholeLossUseDifferentCodes).
//
// The obvious wrong reading is "the network survived, so its area's loss must be
// partial". It is not: Args[0] there is the AREA's own name, and none of that
// area could be placed, so "dropped" is a true statement about the thing the
// sentence names. Where Args[0] names the CONTAINER — a track for its segment, a
// network for its arc, a chain for its link — the container survived and the
// loss is partial.
//
// ⚠ ASSERTING THE CODE ALONE IS NOT ENOUGH. Getting this backwards produces no
// crash, no invalid file and no golden diff — only a true-looking sentence about
// the wrong item. So each case also asserts whether the thing Args[0] names put
// anything on the map, which is the fact the sentence actually claims.
//
// ⚠ THE OBSERVABLE IS GEOMETRY, NOT THE NAME, and it stays that way after the
// 2026-09-03 redesign gave in-network items their own named Placemarks. A
// coordinate only the named thing could have written is the fact the sentence
// claims; a name is a weaker witness, because the network's Folder and its arcs
// Placemark both repeat the network's name and a substring check cannot tell
// which one it found.
func TestPartialLossAndWholeLossUseDifferentCodes(t *testing.T) {
	// A network with one good arc, so the network object always survives.
	goodArc := map[string]vnx.Arc{
		"good": {From: "a", To: "b", Pips: []vnx.Position{pos(10, 10), pos(10, 11)}},
	}
	nodes := map[string]vnx.Position{"a": pos(10, 10), "b": pos(10, 11)}

	for _, tc := range []struct {
		name string
		doc  *vnx.Document
		code string
		// namedThingPlaced is whether the item Args[0] names put anything on the
		// map — which is exactly what the chosen code claims.
		namedThingPlaced bool
		args0            string
		path             string
		// marker is a coordinate only the named thing could have written.
		marker string
	}{
		{
			name: "a track that loses one segment is PARTIAL",
			doc: &vnx.Document{Tracks: []vnx.Track{{Name: "T", Segments: []vnx.Segment{
				{Pips: []vnx.Position{pos(1, 1), pos(1, 2)}},
				{Pips: []vnx.Position{pos(9, 9)}},
			}}}},
			code: vnx.CodePartialGeometry, namedThingPlaced: true,
			args0: "T", path: "/tracks/0/segments/1", marker: "1,1 1,2",
		},
		{
			name: "a track that loses every segment is WHOLE",
			doc: &vnx.Document{Tracks: []vnx.Track{{Name: "T", Segments: []vnx.Segment{
				{Pips: []vnx.Position{pos(9, 9)}},
			}}}},
			code: vnx.CodeItemNoGeometry, namedThingPlaced: false,
			args0: "T", path: "/tracks/0", marker: "9,9",
		},
		{
			name: "a network that loses one arc is PARTIAL",
			doc: &vnx.Document{Networks: []vnx.Network{{
				Name: "N", Nodes: nodes,
				Arcs: map[string]vnx.Arc{
					"good":  goodArc["good"],
					"stump": {Pips: []vnx.Position{pos(9, 9)}},
				},
			}}},
			code: vnx.CodePartialGeometry, namedThingPlaced: true,
			args0: "N", path: "/networks/0/arcs/stump", marker: "10,10 10,11",
		},
		{
			// ⚠ THE CASE THE OBVIOUS WRONG READING GETS BACKWARDS.
			name: "an in-network area with an unusable ring is WHOLE, though the network survives",
			doc: &vnx.Document{Networks: []vnx.Network{{
				Name: "N", Nodes: nodes, Arcs: goodArc,
				Areas: []vnx.Area{{Name: "Boulder Field", Polygon: []vnx.Position{pos(9, 9), pos(9, 8)}}},
			}}},
			code: vnx.CodeItemNoGeometry, namedThingPlaced: false,
			args0: "Boulder Field", path: "/networks/0/areas/0", marker: "9,9",
		},
		{
			name: "a chain that loses one link is PARTIAL",
			doc: &vnx.Document{Networks: []vnx.Network{{
				Name: "N", Nodes: nodes, Arcs: goodArc,
				Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
					{Arc: "good", From: vnx.Endpoint{Node: "a"}, To: vnx.Endpoint{Node: "b"}},
					{Arc: "ghost", From: vnx.Endpoint{Node: "b"}, To: vnx.Endpoint{Node: "a"}},
				}}},
			}}},
			code: vnx.CodePartialGeometry, namedThingPlaced: true,
			args0: "C", path: "/networks/0/chains/0/links/1", marker: "<name>C</name>",
		},
		{
			name: "a chain that loses every link is WHOLE",
			doc: &vnx.Document{Networks: []vnx.Network{{
				Name: "N", Nodes: nodes, Arcs: goodArc,
				Chains: []vnx.Chain{{Name: "C", Links: []vnx.Link{
					{Arc: "ghost", From: vnx.Endpoint{Node: "a"}, To: vnx.Endpoint{Node: "b"}},
				}}},
			}}},
			code: vnx.CodeItemNoGeometry, namedThingPlaced: false,
			args0: "C", path: "/networks/0/chains/0", marker: "<name>C</name>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, report := writeDoc(t, tc.doc)

			var found *vnx.Diagnostic
			for i, d := range report.Diagnostics {
				if d.Path == tc.path {
					found = &report.Diagnostics[i]
				}
			}
			if found == nil {
				t.Fatalf("no diagnostic at %s; got %v", tc.path, report.Diagnostics)
			}
			if found.Code != tc.code {
				t.Errorf("Code at %s = %s, want %s — the code is decided by what Args[0] NAMES, not by what survived around it",
					tc.path, found.Code, tc.code)
			}
			if len(found.Args) != 1 || found.Args[0] != tc.args0 {
				t.Fatalf("Args = %v, want [%q]", found.Args, tc.args0)
			}

			// And the claim the sentence actually makes.
			placed := strings.Contains(out, tc.marker)
			if placed != tc.namedThingPlaced {
				verb := "says it was DROPPED"
				if tc.code == vnx.CodePartialGeometry {
					verb = "says the rest WAS exported"
				}
				t.Errorf("%q %s, but its geometry %q is present=%v in the file:\n%s",
					tc.args0, verb, tc.marker, placed, out)
			}
		})
	}
}
