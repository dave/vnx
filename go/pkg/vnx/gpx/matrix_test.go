package gpx

import (
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

// One test per GPX cell of engine-spec §8 that gpx_test.go does not already
// pin — Tags, Hidden state, Routing config, Moving time, and the
// Track.Started/Ended half of Timestamps. The T11 census (2026-09-02) found
// all of them ratified from the writer CODE and unpinned by any test, and the
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

// -- Tags: ❌ -----------------------------------------------------------------

// Tags cross the VNX wire as `app.viatrail.tag` data records. GPX has nowhere
// to put a record, and §2.3 rules out the <extensions> block that would.
func TestMatrixTagsAreNotCarried(t *testing.T) {
	tag := func(v string) vnx.Record {
		return vnx.Record{Type: "app.viatrail.tag", Body: map[string]any{"value": v}}
	}
	out, rep := write(t, &vnx.Document{
		Folders: []vnx.Folder{{ID: "f", Name: "Munros"}},
		Points: []vnx.Point{{
			Folder: "f", Name: "Cairn", At: vnx.Position{Lon: 1, Lat: 2},
			Data: []vnx.Record{tag("bagged"), tag("winter")},
		}},
		Tracks: []vnx.Track{{
			Name: "Walk", Data: []vnx.Record{tag("solo")},
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
		Plans: []vnx.Plan{plan(vnx.Plan{Name: "Ridge", Data: []vnx.Record{tag("planned")}},
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
		"bagged", "winter", "solo", "planned",
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
		Plans: []vnx.Plan{plan(vnx.Plan{
			Name: "Ridge",
			Data: []vnx.Record{{Type: "app.viatrail.routing", Body: map[string]any{"profile": "walking", "avoid": "roads"}}}},
			lineArc{
				Pips: []vnx.Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 2}},
				Data: []vnx.Record{{Type: "app.viatrail.compute-failed", Body: map[string]any{"reason": "no-route"}}},
			},
		)},
	})
	// CARRIED: the plan, as a <rte> with its geometry (Dave, 2026-10-08).
	if !strings.Contains(out, "<name>Ridge</name>") || !strings.Contains(out, "<rtept") {
		t.Errorf("the plan was not exported\n%s", out)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted %d, want 1", rep.Emitted)
	}
	// LOST: the whole routing record, on the plan AND on the arc.
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
		Points: []vnx.Point{{Name: "Secret Cairn", Hidden: true, At: vnx.Position{Lon: 1, Lat: 2}}},
		Tracks: []vnx.Track{{
			Name: "Secret Walk", Hidden: true,
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}},
		}},
		Plans: []vnx.Plan{plan(vnx.Plan{Name: "Secret Ridge", Hidden: true},
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

// -- Creator -----------------------------------------------------------------

// GPX 1.1 requires a `creator` attribute on <gpx>, naming the software that
// made the GPX file. That is this converter. A VNX document does not say which
// application wrote it (SPEC §3.1), so an application name here would be a
// guess, and a named application would claim a file it did not write.
func TestTheCreatorIsThisLibrary(t *testing.T) {
	out, _ := write(t, &vnx.Document{
		Points: []vnx.Point{{Name: "Hut", At: vnx.Position{Lon: 1, Lat: 2}}},
	})
	if !strings.Contains(out, `<gpx version="1.1" creator="vnx" `) {
		t.Errorf("the <gpx> element does not name vnx as its creator\n%s", out)
	}
	if strings.Contains(out, "Viatrail") {
		t.Errorf("the file names an application; the creator is the converter\n%s", out)
	}
	if strings.Contains(out, "<desc") || strings.Contains(out, "<cmt") {
		t.Errorf("a description element was written; VNX has no description to carry\n%s", out)
	}
}

func iptr(v int) *int         { return &v }
func bptr(v bool) *bool       { return &v }
func fptr(v float64) *float64 { return &v }
