package gpx

import (
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

// One test per GPX cell of engine-spec §8 that gpx_test.go does not already
// pin — Styles, Tags, Hidden state, Routing config, Moving time, and the
// Track.Started/Ended half of Timestamps. The T11 census (2026-09-02) found
// all six ratified from the writer CODE and unpinned by any test, and the
// fixture in gpx_test.go carried none of them, so the golden did not witness
// the drops either.
//
// ⚠ EACH TEST ASSERTS BOTH HALVES: that what the cell says is CARRIED is
// carried, and that what it says is LOST is lost. A test that checked only the
// second half passes on a writer that dropped the item entirely — and the
// GeoJSON Tags cell was worse than that, asserting the absence of records it
// never constructed.
//
// ⚠ NONE OF THESE SIX RAISES A DIAGNOSTIC, AND THAT IS THE RULING, not an
// omission. They are attribute-level losses on items the writer DOES export,
// which writer.go convention 5 puts in the matrix and on spec 6's screen
// rather than in a per-item row: one diagnostic per style, tag and hidden flag
// would be several rows for every item in the library, burying the real
// casualties. Each test asserts the empty diagnostic list, so "no diagnostic"
// is a pinned decision and not an accident.

// -- Styles: ❌ ---------------------------------------------------------------

// GPX 1.1 core has no styling vocabulary at all; the only place a colour could
// go is <extensions>, which §2.3 forbids.
func TestMatrixStylesAreNotCarried(t *testing.T) {
	out, rep := write(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{
			Name: "Hut", At: vnx.Position{Lon: 1, Lat: 2},
			Style: vnx.Style{"color": "#E04535", "icon": "mountain-hut", "icon-size": 27, "pin-shape": "teardrop", "label-position": "below", "label-visible": true, "importance": 5},
		}},
		Routes: []vnx.Route{route(vnx.Route{Name: "Ridge", Style: vnx.Style{"color": "#3A6FB0", "width": 4, "opacity": 0.75, "line-style": "dashed"}},
			lineArc{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}}},
		)},
		Tracks: []vnx.Track{{
			Name:     "Walk",
			Style:    vnx.Style{"color": "#E08A1A", "width": 3},
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
	})
	// CARRIED: the styled items themselves. A writer that refused to write a
	// styled item would also have no style in the file.
	for _, want := range []string{"<name>Hut</name>", "<name>Ridge</name>", "<name>Walk</name>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — the styled item must still be exported\n%s", want, out)
		}
	}
	if rep.Emitted != 3 {
		t.Errorf("emitted %d, want 3", rep.Emitted)
	}
	// LOST: every field of every style, and no <extensions> to smuggle one in.
	for _, gone := range []string{
		"E04535", "3A6FB0", "E08A1A", "mountain-hut", "teardrop", "below",
		"dashed", "27", "0.75", "extensions", "color", "Color", "opacity", "importance", "Importance",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; GPX 1.1 core has no styling and §2.3 forbids inventing one\n%s", gone, out)
		}
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none — a style lost on every item in the library is a matrix fact, not a per-item casualty (convention 5)", codesOf(rep))
	}
}

// -- Tags: ❌ -----------------------------------------------------------------

// Tags cross the VNX wire as `app.viatrail.tag` data records. GPX has nowhere
// to put a record, and §2.3 rules out the <extensions> block that would.
func TestMatrixTagsAreNotCarried(t *testing.T) {
	tag := func(v string) vnx.Record {
		return vnx.Record{Type: "app.viatrail.tag", Body: map[string]any{"value": v}}
	}
	out, rep := write(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "Munros", Data: []vnx.Record{tag("scotland")}}},
		Waypoints: []vnx.Waypoint{{
			Folder: "f", Name: "Cairn", At: vnx.Position{Lon: 1, Lat: 2},
			Data: []vnx.Record{tag("bagged"), tag("winter")},
		}},
		Tracks: []vnx.Track{{
			Name: "Walk", Data: []vnx.Record{tag("solo")},
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
		Routes: []vnx.Route{route(vnx.Route{Name: "Ridge", Data: []vnx.Record{tag("planned")}},
			lineArc{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}}},
		)},
	})
	// CARRIED: the tagged items, under their flattened names.
	for _, want := range []string{"<name>Munros / Cairn</name>", "<name>Walk</name>", "<name>Ridge</name>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — a tagged item must still be exported\n%s", want, out)
		}
	}
	if rep.Emitted != 3 {
		t.Errorf("emitted %d, want 3", rep.Emitted)
	}
	// LOST: every tag value, on every kind of item, and the record machinery
	// with them.
	for _, gone := range []string{
		"scotland", "bagged", "winter", "solo", "planned",
		"app.viatrail.tag", "@type", "extensions", "ExtendedData", "value",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; a data record has no home in GPX 1.1 core\n%s", gone, out)
		}
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none — tags are lost on every item, which is convention 5's matrix fact", codesOf(rep))
	}
}

// -- Routing config: ❌ -------------------------------------------------------

// Same mechanism as tags — it is a data record — but it is its own matrix row
// and its own cell, and a writer could plausibly decide the routing profile was
// worth a <cmt>. It is not: nothing that reads GPX knows what to do with it.
func TestMatrixRoutingConfigIsNotCarried(t *testing.T) {
	out, rep := write(t, &vnx.Document{
		Routes: []vnx.Route{route(vnx.Route{
			Name: "Ridge",
			Data: []vnx.Record{{Type: "app.viatrail.routing", Body: map[string]any{"profile": "walking", "avoid": "roads"}}}},
			lineArc{
				Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}},
				Data: []vnx.Record{{Type: "app.viatrail.compute-failed", Body: map[string]any{"reason": "no-route"}}},
			},
		)},
	})
	// CARRIED: the route, as a <trk> with its geometry (§3.1's ruling).
	if !strings.Contains(out, "<name>Ridge</name>") || !strings.Contains(out, "<trkseg>") {
		t.Errorf("the route was not exported\n%s", out)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted %d, want 1", rep.Emitted)
	}
	// LOST: the whole routing record, on the route AND on the arc.
	for _, gone := range []string{
		"app.viatrail.routing", "app.viatrail.compute-failed",
		"walking", "roads", "no-route", "profile", "avoid", "reason", "extensions",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; a re-imported Viatrail GPX comes back with no routing config, which is the ruling (§2.3)\n%s", gone, out)
		}
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", codesOf(rep))
	}
}

// -- Hidden state: ❌ ---------------------------------------------------------

// ⚠ THE CARRIED HALF IS THE IMPORTANT ONE HERE. GPX has no visibility concept,
// so a hidden item is exported as though visible — the STATE is lost, the ITEM
// is not. A writer that "respected" hidden by skipping the item would lose the
// geometry too, and would pass any test that only checked for the word
// "hidden".
func TestMatrixHiddenStateIsLostAndTheItemIsStillExported(t *testing.T) {
	out, rep := write(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "Secret Cairn", Hidden: true, At: vnx.Position{Lon: 1, Lat: 2}}},
		Tracks: []vnx.Track{{
			Name: "Secret Walk", Hidden: true,
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
		Routes: []vnx.Route{route(vnx.Route{Name: "Secret Ridge", Hidden: true},
			lineArc{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}}},
		)},
	})
	for _, want := range []string{"<name>Secret Cairn</name>", "<name>Secret Walk</name>", "<name>Secret Ridge</name>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — a hidden item is EXPORTED; only the hidden state is lost\n%s", want, out)
		}
	}
	if rep.Emitted != 3 {
		t.Fatalf("emitted %d, want 3 — hidden must not mean skipped", rep.Emitted)
	}
	for _, gone := range []string{"hidden", "Hidden", "visibility", "extensions"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; GPX has no visibility element\n%s", gone, out)
		}
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", codesOf(rep))
	}
}

// -- Moving time: ⚠ derivable, not stated ------------------------------------

// The cell says "derivable, not stated" and BOTH halves of that are asserted:
// the pip <time>s a reader would recompute the figure from are in the file, and
// the figure itself is not.
func TestMatrixMovingTimeIsDerivableAndNotStated(t *testing.T) {
	d := &vnx.Document{Tracks: []vnx.Track{{
		Name: "Walk",
		Segments: []vnx.Segment{{Pips: []vnx.Position{
			{Lon: 1, Lat: 2, Time: ms(1725200000000)},
			{Lon: 3, Lat: 4, Time: ms(1725203600000)}, // exactly one hour later
		}}},
	}}}
	moving, known := d.Tracks[0].MovingTime()
	if !known || moving != time.Hour {
		t.Fatalf("the fixture's moving time is (%v, %v); this test would prove nothing about a figure that does not exist", moving, known)
	}
	out, rep := write(t, d)
	// DERIVABLE: the two pip times are both written, so a reader can subtract
	// them. This is the one thing GPX has that KML and GeoJSON do not, and it
	// is why this cell is ⚠ in the GPX column and ❌ in the other two.
	for _, want := range []string{"<time>2024-09-01T14:13:20Z</time>", "<time>2024-09-01T15:13:20Z</time>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — without the pip times the figure is not derivable either, and this cell would be ❌\n%s", want, out)
		}
	}
	// NOT STATED: no total anywhere, in any spelling.
	for _, gone := range []string{"3600", "movingTime", "moving_time", "moving-time", "duration", "PT1H", "extensions"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the file; the total is derived, never carried — a cached figure a reader must trust over its own arithmetic is what track.go declines to write\n%s", gone, out)
		}
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", codesOf(rep))
	}
}

// -- Timestamps: ⚠ pip <time> yes, Track.Started/Ended lost -------------------

// ⚠ VERDICT, 2026-09-02: THIS DROP TAKES NO DIAGNOSTIC, and the reasoning is
// recorded here because the census asked the question and a later reader will
// ask it again.
//
//  1. It is an ATTRIBUTE of an item the writer DOES export, which is exactly
//     what writer.go convention 5 assigns to the matrix and to spec 6's screen
//     rather than to a per-item row. Every track in a library has these two
//     stamps, so a diagnostic here is one or two rows per track on every
//     export — the noise convention 5 exists to keep out.
//  2. It is UNCONDITIONAL. A conditional diagnostic — raised only for a track
//     with no timed pips — would assert by its silence that nothing was lost
//     for a timed one, and that is false: Started/Ended are stored wall-clock
//     bounds, not the first and last pip times (track.go's MovingTime says so
//     explicitly — "wall clock is recoverable from the two stamps", and moving
//     time is the thing that is not). A recording that ran before its first
//     fix has a Started no pip carries.
//  3. Saying it in GPX ALONE would be the shape CodePartialGeometry was minted
//     to end. KML and GeoJSON lose these same two stamps and say nothing
//     (kml/matrix_test.go's Timestamps cell); one document exported three ways,
//     two disclosures, would be "neither right" again.
//  4. It is NOT SILENT. §8's matrix is normative on this cell, this test pins
//     it in both halves, and the class-level disclosure belongs to the export
//     screen — the same place §8 already sends the network SHARING loss, which
//     is also real, also unconditional, and also not a per-item diagnostic.
//
// If that is overruled, the code is a new writerCode in vnx/diagnostic.go and
// it needs a translation key from Dave; nothing here works around its absence.
func TestMatrixTrackStartedAndEndedReachNoGPX(t *testing.T) {
	started := time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC)
	ended := time.Date(2019, 3, 4, 9, 10, 11, 0, time.UTC)

	t.Run("a timed track keeps its pip times and loses both stamps", func(t *testing.T) {
		out, rep := write(t, &vnx.Document{Tracks: []vnx.Track{{
			Name: "Walk", Started: vnx.TimestampOf(started), Ended: vnx.TimestampOf(ended),
			Segments: []vnx.Segment{{Pips: []vnx.Position{
				{Lon: 1, Lat: 2, Time: ms(1725200000000)},
				{Lon: 3, Lat: 4, Time: ms(1725203600000)},
			}}},
		}}})
		// CARRIED: the pip times. The stamps are a DIFFERENT channel and the
		// year proves which one is in the file.
		if !strings.Contains(out, "<time>2024-09-01T14:13:20Z</time>") {
			t.Errorf("the pip times did not survive\n%s", out)
		}
		// LOST: the two stamps, in every spelling GPX could have taken them —
		// <metadata><time>, a trk-level element, an extension.
		for _, gone := range []string{"2019", "05:06:07", "09:10:11", "<metadata", "started", "ended", "extensions"} {
			if strings.Contains(out, gone) {
				t.Errorf("%q reached the file; no GPX writer touches Track.Started/Ended and trkType has no element for either\n%s", gone, out)
			}
		}
		if rep.Emitted != 1 {
			t.Errorf("emitted %d, want 1 — the track is carried, only its stamps are lost", rep.Emitted)
		}
	})

	t.Run("a timeless track loses every time it had, and is still exported", func(t *testing.T) {
		// ⚠ THE WORST CASE, AND THE ONE THE CENSUS ASKED ABOUT: an imported GPX
		// track has no pip times, so Started/Ended are the only two times it
		// has and the export keeps neither.
		d := &vnx.Document{Tracks: []vnx.Track{{
			Name: "Imported walk", Started: vnx.TimestampOf(started), Ended: vnx.TimestampOf(ended),
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}}},
		}}}
		if _, known := d.Tracks[0].MovingTime(); known {
			t.Fatal("the fixture track has pip times, so it is not the timeless case this asserts")
		}
		out, rep := write(t, d)
		if !strings.Contains(out, "<name>Imported walk</name>") || rep.Emitted != 1 {
			t.Fatalf("the track was not exported (emitted %d)\n%s", rep.Emitted, out)
		}
		if strings.Contains(out, "<time>") || strings.Contains(out, "2019") {
			t.Errorf("a time reached the file for a track whose only times are the two stamps\n%s", out)
		}
		// And nothing is said about it — see the verdict above. This assertion
		// is the pinned decision: if a diagnostic is ever ruled in, this is the
		// line that must be changed deliberately rather than a silence that
		// nobody chose.
		if len(rep.Diagnostics) != 0 {
			t.Errorf("diagnostics %v, want none — the stamps are disclosed by §8's matrix and spec 6's screen, not per track", codesOf(rep))
		}
	})
}

// -- Notes: ✅ <desc> ---------------------------------------------------------

// ⚠ A ✅ THAT NOBODY CHECKED IS THE SAME GUESS AS A ⚠ THAT NOBODY CHECKED
// (kml/matrix_test.go says so, and has this test). The T11 census listed six
// GPX gaps and this was a seventh: no test in this package asserted <desc> at
// all, so the whole Notes cell rested on the golden — and a golden is a
// regression check, not a claim about which element the notes went into.
func TestMatrixNotesAreCarriedAsDesc(t *testing.T) {
	out, rep := write(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "Hut", Notes: "locked in winter", At: vnx.Position{Lon: 1, Lat: 2}}},
		Tracks: []vnx.Track{{
			Name: "Walk", Notes: "stopped for lunch",
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
		Routes: []vnx.Route{route(vnx.Route{Name: "Ridge", Notes: "north to south"},
			lineArc{Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}}},
		)},
	})
	// CARRIED: whole, in <desc>, on all three kinds.
	for _, want := range []string{
		"<desc>locked in winter</desc>", "<desc>stopped for lunch</desc>", "<desc>north to south</desc>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s — the cell says notes survive as <desc>\n%s", want, out)
		}
	}
	// NOT in <cmt>: <cmt> and <desc> are different elements with different
	// meanings, and a reader showing one does not show the other.
	if strings.Contains(out, "<cmt>") {
		t.Errorf("notes were written as <cmt>; the cell says <desc>\n%s", out)
	}
	if rep.Emitted != 3 || len(rep.Diagnostics) != 0 {
		t.Errorf("emitted %d with diagnostics %v, want 3 and none", rep.Emitted, codesOf(rep))
	}
	// An ABSENT note omits the element rather than writing an empty one: an
	// empty <desc/> says "described as nothing", which is not what absence
	// means.
	bare, _ := write(t, &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "Unnoted", At: vnx.Position{Lon: 1, Lat: 2}}},
	})
	if strings.Contains(bare, "<desc") {
		t.Errorf("an item with no notes wrote a <desc>\n%s", bare)
	}
}

func iptr(v int) *int         { return &v }
func bptr(v bool) *bool       { return &v }
func fptr(v float64) *float64 { return &v }
