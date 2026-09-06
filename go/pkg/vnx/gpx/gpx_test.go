package gpx

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

func f(v float64) *float64 { return &v }
func ms(v int64) *int64    { return &v }

func tptr(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 6, 30, 0, 0, time.UTC)
	return &t
}

func tagRecord(v string) vnx.Record {
	return vnx.Record{Type: "app.viatrail.tag", Body: map[string]any{"value": v}}
}

// fixture is the document the golden is written from. It carries one of every
// row of §3.1's table — including the four GPX drops — so the golden itself is
// evidence that the drops emit nothing rather than something odd.
//
// ⚠ IT ALSO CARRIES THE ATTRIBUTES GPX DROPS SILENTLY: styles, tags and other
// data records, hidden state, routing config, and a track's Started/Ended
// stamps. It carried NONE of them until the T11 census (2026-09-02) found that
// the golden could not witness those six cells at all — a writer that started
// emitting an <extensions> block would have left every byte of this file
// unchanged. Because the writer drops them, adding them changed the golden by
// zero bytes, and the golden is now evidence for the drops as well as for the
// four diagnosed ones. Do not thin it back out.
func fixture() *vnx.Document {
	d := &vnx.Document{
		Version:   vnx.Version,
		Generator: "viatrail-test",
		Folders: []vnx.Folder{
			{ID: "f-scot", Name: "Scotland"},
			{ID: "f-cairn", Name: "Cairngorms", Parent: "f-scot",
				Data: []vnx.Record{tagRecord("scotland")}},
			{ID: "f-void", Name: "Nothing In Here"},
		},
		Waypoints: []vnx.Waypoint{
			// At the root: bare name, no path prefix (§2.7).
			{Name: "Car Park & Gate <north>", Notes: "Pay and display",
				At: vnx.Position{Lon: -3.669, Lat: 57.1174, Ele: f(320.5)}},
			// Two levels deep, and with no elevation: the <ele> must be absent,
			// never 0. It is also HIDDEN, STYLED and TAGGED: all three are ❌
			// cells, and a hidden item is EXPORTED — only the state is lost.
			{Folder: "f-cairn", Name: "Summit Cairn", Hidden: true,
				Style: vnx.PointStyle{Color: "#E04535", Icon: "mountain-hut",
					IconSize: iptr(27), PinShape: "teardrop", Importance: iptr(5)},
				Data: []vnx.Record{tagRecord("munro"), tagRecord("bagged")},
				At:   vnx.Position{Lon: -3.6689, Lat: 57.0704}},
			// A timed waypoint with sub-second precision.
			{Folder: "f-scot", Name: "Bothy", Notes: "",
				At: vnx.Position{Lon: -3.7, Lat: 57.05, Ele: f(0), Time: ms(1725200000123)}},
		},
		Areas: []vnx.Area{
			{Folder: "f-cairn", Name: "Restricted Zone", Polygon: []vnx.Position{
				{Lon: -3.7, Lat: 57.0}, {Lon: -3.6, Lat: 57.0},
				{Lon: -3.6, Lat: 57.1}, {Lon: -3.7, Lat: 57.0},
			}},
		},
		Tracks: []vnx.Track{{
			Folder: "f-cairn", Name: "Lairig Ghru", Notes: "Two days, one pause",
			// Started/Ended reach no lossy writer at all, and they are the ONLY
			// times a track imported from a GPX has. Deliberately not the first
			// and last pip times: they are a different channel (track.go).
			Started:  tptr(2024, 8, 31),
			Ended:    tptr(2024, 9, 2),
			Hidden:   true,
			Style:    vnx.ChainStyle{Color: "#E08A1A", Width: f(3), LineStyle: "dashed"},
			Data:     []vnx.Record{tagRecord("solo")},
			Segments: []vnx.Segment{{Pips: leg(0, 40, true)}, {Pips: leg(40, 40, true)}},
		}},
		Routes: []vnx.Route{{
			Folder: "f-scot", Name: "Planned Line",
			Style: vnx.ChainStyle{Color: "#3A6FB0", Opacity: f(0.75)},
			Data: []vnx.Record{{Type: "app.viatrail.routing",
				Body: map[string]any{"profile": "walking", "avoid": "roads"}}},
			Arcs: []vnx.Arc{
				{Pips: leg(200, 30, false)},
				// Arc 2 starts where arc 1 ended: the shared node is emitted once.
				{Pips: leg(229, 30, false)},
			},
		}},
		Networks: []vnx.Network{{Folder: "f-scot", Name: "Cairngorms Paths"}},
		Rulesets: []any{map[string]any{"name": "Hard climbs"}},
	}
	return d
}

// leg makes a deterministic run of pips. Deterministic because the golden is
// compared byte for byte.
func leg(start, n int, timed bool) []vnx.Position {
	out := make([]vnx.Position, 0, n)
	for i := 0; i < n; i++ {
		k := start + i
		// Built from integer microdegrees rather than by accumulating floats:
		// -3.7 + 3*0.001 is -3.6990000000000003, and a golden full of
		// seventeen-digit noise hides the one digit that ever matters.
		p := vnx.Position{
			Lon: float64(-3700000+k*1000) / 1e6,
			Lat: float64(57000000+k*500) / 1e6,
		}
		if k%3 != 0 { // every third pip has no elevation: <ele> must be absent
			p.Ele = f(400 + float64(k))
		}
		if timed {
			p.Time = ms(1725200000000 + int64(k)*15000)
		}
		out = append(out, p)
	}
	return out
}

func write(t *testing.T, d *vnx.Document) (string, vnx.WriteReport) {
	t.Helper()
	var buf bytes.Buffer
	rep, err := Write(&buf, d)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.String(), rep
}

// -- the golden --------------------------------------------------------------

const goldenRegenerateEnv = "VNX_GPX_GOLDEN_REGENERATE"

// TestGolden compares byte for byte.
//
// ⚠ REGENERATION DELIBERATELY FAILS THE RUN. A golden that could be refreshed
// as a side effect of making a suite green is not a golden; the failure forces
// the new bytes to be read by a person before they are committed.
func TestGolden(t *testing.T) {
	path := filepath.Join("testdata", "library.golden.gpx")
	got, _ := write(t, fixture())
	if os.Getenv(goldenRegenerateEnv) != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("regenerated %s from %s — READ THE DIFF, then re-run without the variable",
			path, goldenRegenerateEnv)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (set %s=1 to write it)", err, goldenRegenerateEnv)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n%s", path, firstDiff(got, string(want)))
	}
}

func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d:\n  got  %q\n  want %q", i+1, g[i], w[i])
		}
	}
	return fmt.Sprintf("got %d lines, want %d", len(g), len(w))
}

// TestGoldenIsBigEnoughToStreamThrough keeps the truncation test honest: a
// subject smaller than the largest failure offset would make those cases
// vacuous.
func TestGoldenIsBigEnoughToStreamThrough(t *testing.T) {
	got, _ := write(t, fixture())
	if len(got) < 4096 {
		t.Fatalf("the fixture renders to %d bytes; the truncation test fails the sink at 4096", len(got))
	}
}

// -- one test per matrix cell (§6.3) -----------------------------------------

// codesOf lists the diagnostic codes in order, so a cell test can assert the
// loss is EXACTLY the stated loss and not merely among the losses.
func codesOf(rep vnx.WriteReport) []string {
	out := make([]string, 0, len(rep.Diagnostics))
	for _, d := range rep.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func wantCodes(t *testing.T, rep vnx.WriteReport, want ...string) {
	t.Helper()
	got := codesOf(rep)
	if len(got) != len(want) {
		t.Fatalf("diagnostics %v, want exactly %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("diagnostics %v, want exactly %v", got, want)
		}
	}
}

func TestAreaRaisesExactlyTheAreaDiagnostic(t *testing.T) {
	d := &vnx.Document{Areas: []vnx.Area{{Name: "Zone", Polygon: []vnx.Position{
		{Lon: 1, Lat: 1}, {Lon: 2, Lat: 1}, {Lon: 2, Lat: 2}, {Lon: 1, Lat: 1}}}}}
	out, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeGPXAreaUnsupported)
	if rep.Diagnostics[0].Path != "/areas/0" {
		t.Errorf("path %q, want /areas/0", rep.Diagnostics[0].Path)
	}
	if len(rep.Diagnostics[0].Args) != 1 || rep.Diagnostics[0].Args[0] != "Zone" {
		t.Errorf("args %v, want [Zone]", rep.Diagnostics[0].Args)
	}
	if rep.Emitted != 0 {
		t.Errorf("emitted %d objects for an areas-only document", rep.Emitted)
	}
	// And the loss is a real loss: no polygon leaked out under another name.
	for _, tag := range []string{"<trk", "<wpt", "<rte", "Zone"} {
		if strings.Contains(out, tag) {
			t.Errorf("output carries %q:\n%s", tag, out)
		}
	}
}

func TestNetworkRaisesExactlyTheNetworkDiagnostic(t *testing.T) {
	arc := vnx.Arc{Pips: []vnx.Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}, From: "n1", To: "n2"}
	d := &vnx.Document{Networks: []vnx.Network{{
		Name:   "Paths",
		Nodes:  map[string]vnx.Position{"n1": {Lon: 1, Lat: 1}, "n2": {Lon: 2, Lat: 2}},
		Arcs:   map[string]vnx.Arc{"a1": arc},
		Chains: []vnx.Chain{{Name: "Through Route"}},
		Points: []vnx.Waypoint{{Name: "In-network point", At: vnx.Position{Lon: 1.5, Lat: 1.5}}},
	}}}
	out, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeGPXNetworkUnsupported)
	if rep.Diagnostics[0].Path != "/networks/0" {
		t.Errorf("path %q, want /networks/0", rep.Diagnostics[0].Path)
	}
	if rep.Emitted != 0 {
		t.Errorf("emitted %d objects for a networks-only document", rep.Emitted)
	}
	// ⚠ NOT FLATTENED TO BE HELPFUL (§2.6). A writer that emitted the arcs or
	// the in-network points would pass a diagnostic-only assertion.
	for _, tag := range []string{"<trk", "<wpt", "<rte", "Through Route", "In-network"} {
		if strings.Contains(out, tag) {
			t.Errorf("output carries %q — networks are excluded, not flattened:\n%s", tag, out)
		}
	}
}

func TestRulesetRaisesExactlyTheRulesetDiagnostic(t *testing.T) {
	d := &vnx.Document{Rulesets: []any{map[string]any{"name": "r"}, "opaque"}}
	_, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeRulesetUnsupported, vnx.CodeRulesetUnsupported)
	if rep.Diagnostics[1].Path != "/rulesets/1" {
		t.Errorf("path %q, want /rulesets/1", rep.Diagnostics[1].Path)
	}
	// A ruleset carries no name at all, so it passes NO args (writer.go
	// convention 3). An empty-string arg would reach the bridge as a template
	// parameter it would happily interpolate as nothing.
	if len(rep.Diagnostics[0].Args) != 0 {
		t.Errorf("args %v, want none", rep.Diagnostics[0].Args)
	}
}

func TestEmptyFolderRaisesExactlyTheFolderDiagnostic(t *testing.T) {
	d := &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "held", Name: "Held"},
			{ID: "void", Name: "Void"},
		},
		Waypoints: []vnx.Waypoint{{Folder: "held", Name: "W", At: vnx.Position{Lon: 1, Lat: 2}}},
	}
	out, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeEmptyFolderDropped)
	if rep.Diagnostics[0].Path != "/folders/1" || rep.Diagnostics[0].Args[0] != "Void" {
		t.Errorf("diagnostic %+v, want /folders/1 for Void", rep.Diagnostics[0])
	}
	// The occupied folder is NOT a casualty: its name survives in the item.
	if !strings.Contains(out, "<name>Held / W</name>") {
		t.Errorf("the held folder's name did not reach the item:\n%s", out)
	}
}

// TestAnAncestorOfAHeldFolderIsNotEmpty pins the "anywhere beneath it" half of
// the rule. A parent whose only child is a folder holding an item is occupied;
// counting only DIRECT members would report it dropped and be wrong.
func TestAnAncestorOfAHeldFolderIsNotEmpty(t *testing.T) {
	d := &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "top", Name: "Top"},
			{ID: "mid", Name: "Mid", Parent: "top"},
			{ID: "leaf", Name: "Leaf", Parent: "mid"},
		},
		Waypoints: []vnx.Waypoint{{Folder: "leaf", Name: "W", At: vnx.Position{Lon: 1, Lat: 2}}},
	}
	out, rep := write(t, d)
	wantCodes(t, rep)
	if !strings.Contains(out, "<name>Top / Mid / Leaf / W</name>") {
		t.Errorf("path not folded into the name:\n%s", out)
	}
}

func TestFolderPathUnavailableKeepsTheItemUnderItsBareName(t *testing.T) {
	d := &vnx.Document{
		Waypoints: []vnx.Waypoint{{Folder: "ghost", Name: "Orphan", At: vnx.Position{Lon: 1, Lat: 2}}},
	}
	out, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeFolderPathUnavailable)
	if rep.Diagnostics[0].Path != "/waypoints/0" || rep.Diagnostics[0].Args[0] != "Orphan" {
		t.Errorf("diagnostic %+v", rep.Diagnostics[0])
	}
	if !strings.Contains(out, "<name>Orphan</name>") {
		t.Errorf("the item was not kept under its bare name:\n%s", out)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted %d, want 1 — an unbuildable path must not drop the item", rep.Emitted)
	}
}

// TestACyclicFolderChainDoesNotHang would time out rather than fail if the
// walks were unbounded, which is the only way this class of defect shows up.
func TestACyclicFolderChainDoesNotHang(t *testing.T) {
	d := &vnx.Document{
		Folders: []vnx.Folder{
			{ID: "a", Name: "A", Parent: "b"},
			{ID: "b", Name: "B", Parent: "a"},
		},
		Routes: []vnx.Route{{Folder: "a", Name: "R", Arcs: []vnx.Arc{
			{Pips: []vnx.Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}}}}},
	}
	out, rep := write(t, d)
	// The cycle makes the path unbuildable; the route is kept, bare.
	wantCodes(t, rep, vnx.CodeFolderPathUnavailable)
	if !strings.Contains(out, "<name>R</name>") {
		t.Errorf("route lost:\n%s", out)
	}
}

func TestItemsWithNoGeometryAreDiagnosedNotEmitted(t *testing.T) {
	d := &vnx.Document{
		Tracks: []vnx.Track{
			{Name: "No segments"},
			{Name: "Only empty segments", Segments: []vnx.Segment{{}, {}}},
		},
		Routes: []vnx.Route{{Name: "No arcs"}},
	}
	out, rep := write(t, d)
	wantCodes(t, rep, vnx.CodeItemNoGeometry, vnx.CodeItemNoGeometry, vnx.CodeItemNoGeometry)
	if rep.Emitted != 0 {
		t.Errorf("emitted %d, want 0", rep.Emitted)
	}
	if strings.Contains(out, "<trk>") {
		t.Errorf("an empty <trk> was written:\n%s", out)
	}
}

// TestOnlyUnsupportedItemsEmitNothing is the case vnxbridge refuses on
// (formatwriter.go). The refusal is not this package's, but the zero it acts
// on is.
func TestOnlyUnsupportedItemsEmitNothing(t *testing.T) {
	d := &vnx.Document{
		Areas:    []vnx.Area{{Name: "A", Polygon: []vnx.Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 1}, {Lon: 2, Lat: 2}, {Lon: 1, Lat: 1}}}},
		Networks: []vnx.Network{{Name: "N"}},
		Rulesets: []any{"r"},
	}
	_, rep := write(t, d)
	if rep.Emitted != 0 {
		t.Fatalf("emitted %d for a document of only networks, areas and rulesets", rep.Emitted)
	}
	wantCodes(t, rep, vnx.CodeGPXAreaUnsupported, vnx.CodeGPXNetworkUnsupported, vnx.CodeRulesetUnsupported)
}

// TestDiagnosticsAreInDocumentOrder pins writer.go convention 6 against the
// emission order, which is the schema's and differs (see writer.run).
func TestDiagnosticsAreInDocumentOrder(t *testing.T) {
	_, rep := write(t, fixture())
	wantCodes(t, rep,
		vnx.CodeEmptyFolderDropped,    // /folders/2
		vnx.CodeGPXAreaUnsupported,    // /areas/0
		vnx.CodeGPXNetworkUnsupported, // /networks/0
		vnx.CodeRulesetUnsupported,    // /rulesets/0
	)
}

func TestEveryDiagnosticSetsCodeAndPath(t *testing.T) {
	docs := []*vnx.Document{fixture(),
		{Waypoints: []vnx.Waypoint{{Folder: "ghost", At: vnx.Position{Lon: 1, Lat: 1}}}},
		{Tracks: []vnx.Track{{Name: "empty"}}},
	}
	seen := 0
	for _, d := range docs {
		_, rep := write(t, d)
		for _, diag := range rep.Diagnostics {
			seen++
			if diag.Code == "" {
				t.Errorf("%+v has no Code — the bridge can only translate through it", diag)
			}
			if diag.Path == "" {
				t.Errorf("%+v has no Path", diag)
			}
			if diag.Severity != vnx.Warning {
				t.Errorf("%+v is not a Warning; every writer site reports a drop", diag)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no diagnostics inspected — dead probe")
	}
}

// TestEveryCodeRaisedIsRegistered catches a Code spelled at a raise site that
// pkg/vnx never declared, which would reach the bridge unmapped.
func TestEveryCodeRaisedIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range vnx.WriterCodes() {
		registered[c] = true
	}
	for _, c := range []string{
		vnx.CodeGPXNetworkUnsupported, vnx.CodeGPXAreaUnsupported,
		vnx.CodeRulesetUnsupported, vnx.CodeEmptyFolderDropped,
		vnx.CodeFolderPathUnavailable, vnx.CodeItemNoGeometry,
	} {
		if !registered[c] {
			t.Errorf("gpx raises %q, which is not in vnx.WriterCodes()", c)
		}
	}
}

// -- the geometry rulings ----------------------------------------------------

// TestARouteIsATrackNotARoute is §3.1's load-bearing ruling. <rte> is a list of
// turn cues; a dense Viatrail route written as <rtept>s validates and is
// useless on a device.
func TestARouteIsATrackNotARoute(t *testing.T) {
	d := &vnx.Document{Routes: []vnx.Route{{Name: "Planned", Arcs: []vnx.Arc{
		{Pips: []vnx.Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}}}}}}
	out, rep := write(t, d)
	if strings.Contains(out, "<rte") || strings.Contains(out, "rtept") {
		t.Fatalf("the route was written as <rte>:\n%s", out)
	}
	if !strings.Contains(out, "<trk>") || !strings.Contains(out, "<trkseg>") {
		t.Fatalf("the route was not written as a <trk> with a <trkseg>:\n%s", out)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted %d, want 1", rep.Emitted)
	}
}

// TestARouteBecomesOneSegmentWithTheSharedNodeWrittenOnce pins the arc join.
func TestARouteBecomesOneSegmentWithTheSharedNodeWrittenOnce(t *testing.T) {
	shared := vnx.Position{Lon: 2, Lat: 2}
	d := &vnx.Document{Routes: []vnx.Route{{Name: "Two arcs", Arcs: []vnx.Arc{
		{Pips: []vnx.Position{{Lon: 1, Lat: 1}, shared}},
		{Pips: []vnx.Position{shared, {Lon: 3, Lat: 3}}},
	}}}}
	out, _ := write(t, d)
	if n := strings.Count(out, "<trkseg>"); n != 1 {
		t.Errorf("%d <trkseg>s, want 1 — a route's arcs are not pauses", n)
	}
	if n := strings.Count(out, `lat="2" lon="2"`); n != 1 {
		t.Errorf("the shared node appears %d times, want 1", n)
	}
	if n := strings.Count(out, "<trkpt"); n != 3 {
		t.Errorf("%d <trkpt>s, want 3", n)
	}
}

// TestATrackKeepsOneSegmentPerSegment — the gap between segments is a PAUSE and
// concatenating them draws a line across it.
func TestATrackKeepsOneSegmentPerSegment(t *testing.T) {
	d := &vnx.Document{Tracks: []vnx.Track{{Name: "Paused", Segments: []vnx.Segment{
		{Pips: []vnx.Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}},
		{Pips: []vnx.Position{{Lon: 9, Lat: 9}, {Lon: 10, Lat: 10}}},
	}}}}
	out, rep := write(t, d)
	if n := strings.Count(out, "<trkseg>"); n != 2 {
		t.Errorf("%d <trkseg>s, want 2", n)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted %d, want 1 — a track is ONE object however many segments it holds", rep.Emitted)
	}
}

// TestUnknownElevationOmitsTheElement. Never 0: that puts every unknown point
// at sea level and reads as data rather than absence.
func TestUnknownElevationOmitsTheElement(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "unknown", At: vnx.Position{Lon: 1, Lat: 1}},
		{Name: "known-zero", At: vnx.Position{Lon: 2, Lat: 2, Ele: f(0)}},
	}}
	out, _ := write(t, d)
	if n := strings.Count(out, "<ele>"); n != 1 {
		t.Fatalf("%d <ele> elements, want 1 (only the known one):\n%s", n, out)
	}
	// A KNOWN zero still writes 0 — the rule is about absence, not about the
	// value, and suppressing a real sea-level reading would be the same lie in
	// the other direction.
	if !strings.Contains(out, "<ele>0</ele>") {
		t.Errorf("a known elevation of 0 was not written:\n%s", out)
	}
}

func TestAbsentTimeOmitsTheElement(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "untimed", At: vnx.Position{Lon: 1, Lat: 1}},
		{Name: "whole second", At: vnx.Position{Lon: 2, Lat: 2, Time: ms(1725200000000)}},
		{Name: "millis", At: vnx.Position{Lon: 3, Lat: 3, Time: ms(1725200000123)}},
	}}
	out, _ := write(t, d)
	if n := strings.Count(out, "<time>"); n != 2 {
		t.Fatalf("%d <time> elements, want 2:\n%s", n, out)
	}
	for _, want := range []string{"<time>2024-09-01T14:13:20Z</time>", "<time>2024-09-01T14:13:20.123Z</time>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s:\n%s", want, out)
		}
	}
}

// TestWaypointsPrecedeTracks — gpxType is a sequence, so interleaving produces
// a file that some readers open and any validator rejects.
func TestWaypointsPrecedeTracks(t *testing.T) {
	d := &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "W", At: vnx.Position{Lon: 1, Lat: 1}}},
		Tracks:    []vnx.Track{{Name: "T", Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 1}}}}}},
	}
	out, _ := write(t, d)
	if strings.Index(out, "<wpt") > strings.Index(out, "<trk>") {
		t.Errorf("a <trk> precedes a <wpt>:\n%s", out)
	}
}

func TestTextIsEscaped(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: `Gate <north> & "east"`, Notes: "a & b", At: vnx.Position{Lon: 1, Lat: 1}}}}
	out, _ := write(t, d)
	if strings.Contains(out, "<north>") {
		t.Errorf("raw markup reached the file:\n%s", out)
	}
	if !strings.Contains(out, "&amp;") || !strings.Contains(out, "&lt;north&gt;") {
		t.Errorf("name not escaped:\n%s", out)
	}
}

// TestCoordinatesNeverUseExponentNotation: 1e-7 is a legal float64 and an
// illegal xsd:decimal, so an exponent makes the file invalid.
func TestCoordinatesNeverUseExponentNotation(t *testing.T) {
	d := &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "tiny", At: vnx.Position{Lon: 0.0000001, Lat: -0.00000002, Ele: f(0.0000003)}}}}
	out, _ := write(t, d)
	if strings.Contains(out, "e-") || strings.Contains(out, "E-") {
		t.Errorf("exponent notation in the output:\n%s", out)
	}
}

// TestANonFiniteCoordinateIsRefused. Decode rejects NaN, so only a hand-built
// Document reaches here with one — and writing "NaN" into an attribute yields
// an invalid file reported as a successful export.
func TestANonFiniteCoordinateIsRefused(t *testing.T) {
	for _, bad := range []vnx.Position{
		{Lon: math.NaN(), Lat: 1},
		{Lon: 1, Lat: math.Inf(1)},
		{Lon: 1, Lat: 1, Ele: f(math.NaN())},
	} {
		d := &vnx.Document{Waypoints: []vnx.Waypoint{{Name: "bad", At: bad}}}
		var buf bytes.Buffer
		if _, err := Write(&buf, d); err == nil {
			t.Errorf("%v was written without an error: %s", bad, buf.String())
		}
	}
	// The working case, so a writer that refused everything would not pass.
	var buf bytes.Buffer
	if _, err := Write(&buf, &vnx.Document{Waypoints: []vnx.Waypoint{
		{Name: "ok", At: vnx.Position{Lon: 1, Lat: 1}}}}); err != nil {
		t.Fatalf("a finite document was refused: %v", err)
	}
}

func TestAnEmptyDocumentIsStillAValidGPX(t *testing.T) {
	out, rep := write(t, &vnx.Document{})
	if rep.Emitted != 0 || len(rep.Diagnostics) != 0 {
		t.Errorf("report %+v, want an empty one", rep)
	}
	if out != header+footer {
		t.Errorf("output %q", out)
	}
}

// -- the short-write red-proof ------------------------------------------------

var errSinkFull = errors.New("sink full")

// failAfter accepts n bytes and then stops. It reports the SHORT COUNT with no
// error on the boundary write, which is the case a wrapper checking only
// `err != nil` walks straight past.
type failAfter struct {
	n       int
	written int
}

func (w *failAfter) Write(p []byte) (int, error) {
	room := w.n - w.written
	if room <= 0 {
		return 0, errSinkFull
	}
	if len(p) <= room {
		w.written += len(p)
		return len(p), nil
	}
	w.written += room
	return room, nil // short, and NO error: io.Writer permits it
}

// TestWriteFailsRatherThanTruncating is 4b's pattern. An ignored short write
// means Write returns nil, the sink Syncs and renames, and a corrupt file lands
// as a success.
func TestWriteFailsRatherThanTruncating(t *testing.T) {
	doc := fixture()
	var full bytes.Buffer
	if _, err := Write(&full, doc); err != nil {
		t.Fatal(err)
	}
	n := full.Len()
	if n < 4096 {
		t.Fatalf("the subject is %d bytes; too small for a mid-document failure to be interesting", n)
	}
	for _, limit := range []int{0, 1, 17, 512, 4096, n - 1} {
		w := &failAfter{n: limit}
		rep, err := Write(w, doc)
		if err == nil {
			t.Errorf("a sink that stopped accepting bytes after %d of %d returned NO error (report %+v)", limit, n, rep)
			continue
		}
		if !errors.Is(err, errSinkFull) && !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("limit %d: %v", limit, err)
		}
	}
	// And the working case, so a latch that never lets go cannot be mistaken
	// for correct error handling.
	w := &failAfter{n: n}
	if _, err := Write(w, doc); err != nil {
		t.Fatalf("a sink with room for the whole document failed: %v", err)
	}
	if w.written != n {
		t.Fatalf("wrote %d bytes, the document is %d", w.written, n)
	}
}

// -- the purity guard --------------------------------------------------------

// libRoot is this library's own import path — see pkg/vnx/vnx_test.go.
const libRoot = "github.com/dave/vnx/go/pkg/vnx"

// TestImportsAreStdlibOnly is the local half of cmd/vnx/purity_test.go. That
// guard enumerates the reference CLI's whole in-module dependency set; this one
// fails in the package that broke it, which is where the fix belongs.
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
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
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
				continue // the parent, and the only in-module import allowed
			}
			if isTest && strings.HasPrefix(path, libRoot+"/") {
				continue // a sibling package of the same library
			}
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — pkg/vnx/gpx may import stdlib and pkg/vnx only", name, path)
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
