package kml

import "github.com/dave/vnx/go/pkg/vnx"

// The shared fixture the golden is taken from (spec 5b §6.1).
//
// ⚠ IT IS BUILT AS A Document LITERAL AND THERE IS NO DATABASE ANYWHERE. That
// is §2.4's third consequence made concrete: a writer's input is a
// *vnx.Document, so testing one needs no fixture database, no migrations and no
// build tag.
//
// ⚠ EVERY FIELD HERE IS LOAD-BEARING. The fixture deliberately carries the
// things that are easy to drop silently: an empty folder, a name with an XML
// metacharacter in it, a hidden item, a pip with no elevation beside pips that
// have one, pip times (which KML cannot carry), a two-arc route, a two-segment
// track, in-network points AND areas, two chains SHARING an arc, and a ruleset.
// A fixture without them makes a golden that cannot see the losses this format
// is supposed to have.

func ptr[T any](v T) *T { return &v }

func pos(lon, lat float64) vnx.Position { return vnx.Position{Lon: lon, Lat: lat} }

func posEle(lon, lat, ele float64) vnx.Position {
	return vnx.Position{Lon: lon, Lat: lat, Ele: ptr(ele)}
}

func posTime(lon, lat float64, ms int64) vnx.Position {
	return vnx.Position{Lon: lon, Lat: lat, Time: ptr(ms)}
}

func fixture() *vnx.Document {
	return &vnx.Document{
		Version:   vnx.Version,
		Generator: "viatrail-test",
		Folders: []vnx.Folder{
			{ID: "f-cairn", Name: "Cairngorms"},
			{ID: "f-day1", Name: "Day 1", Parent: "f-cairn", Notes: "the walk in"},
			// An EMPTY folder, and a HIDDEN one. KML keeps both, which is the
			// decision this fixture exists to pin.
			{ID: "f-empty", Name: "Nothing Here", Hidden: true},
		},
		Waypoints: []vnx.Waypoint{
			{
				Folder: "f-day1", Name: "Corrour Bothy", Notes: "locked in winter",
				At:    posEle(-3.6512, 57.0421, 720),
				Style: vnx.Style{"color": "#E04535", "icon": "hut", "pin-shape": "pin", "importance": 4},
			},
			{
				// The ampersand proves the escaping, and the missing elevation
				// proves the third ordinate is omitted rather than zeroed.
				Name: "Ben & Nevis", Hidden: true, At: pos(-5.0037, 56.7969),
			},
		},
		Areas: []vnx.Area{{
			Folder: "f-cairn", Name: "Loch Avon", Notes: "swim here",
			Polygon: []vnx.Position{
				pos(-3.66, 57.07), pos(-3.64, 57.07), pos(-3.64, 57.08), pos(-3.66, 57.07),
			},
			Style: vnx.Style{"fill-color": "#1E9C45", "fill-opacity": 0.5, "stroke-color": "#123456", "stroke-width": 2.0},
		}},
		Tracks: []vnx.Track{{
			Folder: "f-day1", Name: "Walk In", Notes: "stopped for lunch",
			Style: vnx.Style{"color": "#E08A1A", "width": 3.0},
			Segments: []vnx.Segment{
				// Pip times are carried in the Document and are a ❌ cell in KML:
				// they must not appear anywhere in the output.
				{Pips: []vnx.Position{posTime(-3.70, 57.00, 1_700_000_000_000), posTime(-3.69, 57.01, 1_700_000_600_000)}},
				{Pips: []vnx.Position{pos(-3.68, 57.02), posEle(-3.67, 57.03, 640)}},
			},
		}},
		Routes: []vnx.Route{{
			Name: "Ridge Line", Style: vnx.Style{"color": "#3A6FB0", "opacity": 0.75},
			// Two arcs meeting at a shared endpoint: the boundary between them
			// is what KML loses.
			Arcs: []vnx.Arc{
				{Pips: []vnx.Position{pos(-3.60, 57.10), pos(-3.59, 57.11)}},
				{Pips: []vnx.Position{pos(-3.59, 57.11), pos(-3.58, 57.12)}},
			},
		}},
		Networks: []vnx.Network{{
			Name: "Lairig Ghru", Notes: "the through route",
			Nodes: map[string]vnx.Position{
				"n1": pos(-3.70, 57.00),
				"n2": pos(-3.68, 57.04),
				"n3": pos(-3.66, 57.08),
			},
			Arcs: map[string]vnx.Arc{
				// Deliberately declared out of order: the writer sorts them, and
				// a golden taken from an unsorted walk would be flaky.
				"a-south": {From: "n2", To: "n3", Pips: []vnx.Position{pos(-3.68, 57.04), pos(-3.67, 57.06), pos(-3.66, 57.08)}},
				"a-north": {From: "n1", To: "n2", Pips: []vnx.Position{pos(-3.70, 57.00), pos(-3.69, 57.02), pos(-3.68, 57.04)}},
			},
			Chains: []vnx.Chain{
				{
					Name: "Full Traverse", Notes: "north to south",
					Style: vnx.Style{"color": "#7B4FA8", "width": 4.0},
					Links: []vnx.Link{
						{Arc: "a-north", From: vnx.Endpoint{Node: "n1"}, To: vnx.Endpoint{Node: "n2"}},
						{Arc: "a-south", From: vnx.Endpoint{Node: "n2"}, To: vnx.Endpoint{Node: "n3"}},
					},
				},
				{
					// SHARES a-north with the chain above, and stops part-way
					// along it at an inline endpoint. Both facts matter: the
					// sharing is what KML cannot express, and the inline endpoint
					// is what proves the arc is cut rather than emitted whole.
					Name: "Half Way", Hidden: true,
					Links: []vnx.Link{
						{Arc: "a-north", From: vnx.Endpoint{Node: "n1"}, To: vnx.Endpoint{At: ptr(pos(-3.69, 57.02))}},
					},
				},
			},
			Points: []vnx.Waypoint{{
				Name: "Pools of Dee", Notes: "in-network", At: posEle(-3.675, 57.05, 830),
				Style: vnx.Style{"color": "#2E86AB"},
			}},
			Areas: []vnx.Area{{
				Name: "Boulder Field", Hidden: true,
				Polygon: []vnx.Position{
					pos(-3.685, 57.03), pos(-3.675, 57.03), pos(-3.675, 57.04), pos(-3.685, 57.03),
				},
				Style: vnx.Style{"fill-color": "#888888"},
			}},
		}},
		Rulesets: []any{map[string]any{"id": "r1", "name": "Munros"}},
	}
}
