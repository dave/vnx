package geojson

import "github.com/dave/vnx/go/pkg/vnx"

// fixture is the one Document the golden is captured from. It is deliberately
// NOT clean: every casualty this writer can produce is in here, because a
// golden captured from a document with nothing wrong pins only the happy path
// and the matrix cells are exactly the cells nobody looks at.
//
// ⚠ IT IS A LITERAL, WITH NO DATABASE ANYWHERE. That is spec §2.4's whole
// point — a writer's input is a Document, so it is testable without a fixture
// database, and this package stays stdlib-only.
//
// ⚠ IT ALSO CARRIES THE ATTRIBUTES THIS WRITER DROPS WITHOUT A DIAGNOSTIC:
// styles, tags and routing records, and hidden state. Added at the T11 census
// (2026-09-02), when the golden was found unable to witness those cells — a
// writer that started putting a style in `properties` would have left every
// byte of the golden unchanged. Because the writer drops them, adding them
// changed the golden by zero bytes.
func fixture() *vnx.Document {
	return &vnx.Document{
		Version:   vnx.Version,
		Generator: "viatrail-test",
		Created:   "2026-09-02T00:00:00Z",
		Folders: []vnx.Folder{
			{ID: "f-hi", Name: "Highlands"},
			{ID: "f-cg", Name: "Cairngorms", Parent: "f-hi"},
			// Holds nothing anywhere beneath it: the one folder that is a
			// casualty (§2.7).
			{ID: "f-empty", Name: "Nothing Here", Parent: "f-hi"},
		},
		Waypoints: []vnx.Waypoint{
			{Name: "Corrour Bothy", Notes: "Shelter", Folder: "f-cg",
				Style: vnx.PointStyle{Color: "#E04535", Icon: "mountain-hut", PinShape: "teardrop"},
				Data:  []vnx.Record{tagRecord("bothy"), tagRecord("shelter")},
				At:    pos(-3.7003, 56.9932, ptr(733.0))},
			// No elevation: the position must be TWO elements.
			{Name: "Unnamed Spring", Folder: "f-hi", At: pos(-3.6210, 57.0704, nil)},
			// Names a folder this document does not hold: kept, under its bare
			// name, with CodeFolderPathUnavailable.
			{Name: "Orphan Cairn", Folder: "f-ghost", At: pos(-3.5000, 57.1000, nil)},
		},
		Areas: []vnx.Area{
			// ⚠ WOUND CLOCKWISE ON PURPOSE. RFC 7946 wants counterclockwise,
			// so the golden pins the REVERSED order and a writer that stopped
			// normalising would show up here as well as in TestWinding.
			{Name: "Deer Reserve", Notes: "No dogs", Folder: "f-hi", Hidden: true,
				Style: vnx.AreaStyle{FillColor: "#1E9C45", StrokeColor: "#123456"},
				Data:  []vnx.Record{tagRecord("wildlife")},
				Polygon: []vnx.Position{
					pos(-3.80, 57.00, nil), pos(-3.80, 57.10, nil),
					pos(-3.70, 57.10, nil), pos(-3.70, 57.00, nil),
					pos(-3.80, 57.00, nil),
				}},
			// Three positions: not a linear ring, dropped and diagnosed.
			{Name: "Sliver", Folder: "f-hi", Polygon: []vnx.Position{
				pos(-3.9, 57.0, nil), pos(-3.9, 57.1, nil), pos(-3.9, 57.0, nil),
			}},
		},
		Tracks: []vnx.Track{
			{Name: "Lairig Ghru walk", Folder: "f-cg", Hidden: true, Segments: []vnx.Segment{
				{Pips: []vnx.Position{
					pos(-3.700, 56.990, ptr(700.0)), pos(-3.701, 56.995, ptr(720.0)),
				}},
				// A pause, then a single fix: too little for a LineString.
				{Pips: []vnx.Position{pos(-3.702, 57.000, nil)}},
				{Pips: []vnx.Position{
					pos(-3.703, 57.005, nil), pos(-3.704, 57.010, ptr(760.0)),
				}},
			}},
		},
		Routes: []vnx.Route{
			// Two arcs meeting at one position: the join must not double it.
			{Name: "Planned line",
				Style: vnx.ChainStyle{Color: "#3A6FB0", LineStyle: "dashed"},
				Data: []vnx.Record{{Type: "app.viatrail.routing",
					Body: map[string]any{"profile": "walking"}}},
				Arcs: []vnx.Arc{
					{Pips: []vnx.Position{pos(-3.60, 57.00, nil), pos(-3.61, 57.01, nil)}},
					{Pips: []vnx.Position{pos(-3.61, 57.01, nil), pos(-3.62, 57.02, nil)}},
				}},
		},
		Networks: []vnx.Network{{
			Name: "Cairngorm paths", Notes: "The graph", Folder: "f-cg",
			Nodes: map[string]vnx.Position{
				"n1": pos(-3.50, 57.00, nil),
				"n2": pos(-3.51, 57.02, nil),
				"n3": pos(-3.52, 57.04, nil),
			},
			// Declared out of order: the golden pins the SORTED order, so a
			// writer that ranged the map straight would go flaky here.
			Arcs: map[string]vnx.Arc{
				"arc-b": {From: "n2", To: "n3", Pips: []vnx.Position{
					pos(-3.51, 57.02, nil), pos(-3.515, 57.03, nil), pos(-3.52, 57.04, nil),
				}},
				"arc-a": {From: "n1", To: "n2", Pips: []vnx.Position{
					pos(-3.50, 57.00, nil), pos(-3.505, 57.01, nil), pos(-3.51, 57.02, nil),
				}},
			},
			Points: []vnx.Waypoint{{Name: "Junction hut", At: pos(-3.51, 57.02, ptr(500.0))}},
			Areas: []vnx.Area{{Name: "Boggy bit", Polygon: []vnx.Position{
				pos(-3.53, 57.00, nil), pos(-3.52, 57.00, nil),
				pos(-3.52, 57.01, nil), pos(-3.53, 57.00, nil),
			}}},
			Chains: []vnx.Chain{{
				Name: "North traverse", Notes: "n1 to n3",
				Links: []vnx.Link{
					{Arc: "arc-a", From: node("n1"), To: node("n2")},
					{Arc: "arc-b", From: node("n2"), To: node("n3")},
				},
			}},
		}},
		Rulesets: []any{map[string]any{"name": "Munros"}},
	}
}

func pos(lon, lat float64, ele *float64) vnx.Position {
	return vnx.Position{Lon: lon, Lat: lat, Ele: ele}
}

func ptr(f float64) *float64 { return &f }

func node(id string) vnx.Endpoint { return vnx.Endpoint{Node: id} }

func tagRecord(v string) vnx.Record {
	return vnx.Record{Type: "app.viatrail.tag", Body: map[string]any{"value": v}}
}
