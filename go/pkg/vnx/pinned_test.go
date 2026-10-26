package vnx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// pinnedDocument pins one item of every kind that carries `pinned` (§3.8),
// each with its own value, and leaves one waypoint unpinned.
func pinnedDocument() *Document {
	ring := []Position{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 1}, {Lon: 2, Lat: 2}}
	nodes := map[string]Vertex{"n1": {Lon: 1, Lat: 1}, "n2": {Lon: 1, Lat: 2}}
	arcs := map[string]Arc{"a1": {From: "n1", To: "n2"}}
	n1, n2 := Endpoint{Node: "n1"}, Endpoint{Node: "n2"}
	walk := []Pass{{Arc: "a1", From: &n1, To: &n2}}
	return &Document{
		Version: "0.10.0",
		Networks: []Network{{
			Name: "network", Pinned: "2026-10-20T08:15:00Z", Nodes: nodes, Arcs: arcs,
			Highlights: []Highlight{{Name: "highlight", Pinned: "2026-10-21T09:00:00.250Z", Walk: walk}},
			Points:     []Waypoint{{Name: "point", Pinned: "2026-10-22T10:30:45.5Z", At: Position{Lon: 1, Lat: 1.5}}},
			Areas:      []Area{{Name: "network area", Pinned: "2026-10-23T11:00:00Z", Polygon: ring}},
		}},
		Plans:  []Plan{{Name: "plan", Pinned: "2026-10-24T12:00:00Z", Nodes: nodes, Arcs: arcs, Walk: walk}},
		Tracks: []Track{{Name: "track", Pinned: "2026-10-25T13:00:00.001Z", Segments: []Segment{{Pips: ring}}}},
		Waypoints: []Waypoint{
			{Name: "waypoint", Pinned: "2026-10-26T14:00:00Z", At: Position{Lon: 3, Lat: 3}},
			{Name: "unpinned", At: Position{Lon: 4, Lat: 4}},
		},
		Areas: []Area{{Name: "area", Pinned: "2026-13-45T99:99:99Z", Polygon: ring}},
	}
}

// pinnedValues lists every item's `pinned` by item name.
func pinnedValues(d *Document) map[string]Timestamp {
	out := map[string]Timestamp{}
	for _, n := range d.Networks {
		out[n.Name] = n.Pinned
		for _, h := range n.Highlights {
			out[h.Name] = h.Pinned
		}
		for _, p := range n.Points {
			out[p.Name] = p.Pinned
		}
		for _, a := range n.Areas {
			out[a.Name] = a.Pinned
		}
	}
	for _, p := range d.Plans {
		out[p.Name] = p.Pinned
	}
	for _, t := range d.Tracks {
		out[t.Name] = t.Pinned
	}
	for _, w := range d.Waypoints {
		out[w.Name] = w.Pinned
	}
	for _, a := range d.Areas {
		out[a.Name] = a.Pinned
	}
	return out
}

// Every kind of item keeps its `pinned` through Encode, the streaming reader
// and the tree reader, verbatim: a calendar-invalid value included (§4.3).
func TestPinnedRoundTripsOnEveryItemKind(t *testing.T) {
	in := pinnedDocument()
	want := pinnedValues(in)
	data, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	for name, decode := range map[string]func([]byte) (*Document, []Diagnostic, error){
		"Decode":     Decode,
		"decodeTree": decodeTree,
		"DecodeReader": func(b []byte) (*Document, []Diagnostic, error) {
			return DecodeReader(bytes.NewReader(b))
		},
	} {
		out, diags, err := decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(diags) != 0 {
			t.Fatalf("%s: a clean document produced %v", name, diags)
		}
		got := pinnedValues(out)
		if len(got) != len(want) {
			t.Fatalf("%s: read %d items, want %d", name, len(got), len(want))
		}
		for item, w := range want {
			if got[item] != w {
				t.Errorf("%s: %s pinned = %q, want %q", name, item, got[item], w)
			}
		}
	}
}

// §3.8: a writer emits `pinned` only when the item is pinned.
func TestAnUnpinnedItemIsWrittenWithoutPinned(t *testing.T) {
	data, err := Encode(pinnedDocument())
	if err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Waypoints []map[string]any `json:"waypoints"`
	}
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	if v, present := tree.Waypoints[1]["pinned"]; present {
		t.Fatalf("an unpinned waypoint was written with pinned = %#v", v)
	}
	if tree.Waypoints[0]["pinned"] != "2026-10-26T14:00:00Z" {
		t.Fatalf("a pinned waypoint was written with pinned = %#v", tree.Waypoints[0]["pinned"])
	}
}

// A caller-built `pinned` outside §4.3's grammar is refused on every kind, the
// network's streamed member as much as the others.
func TestEncodeRefusesAMalformedPinned(t *testing.T) {
	const bad = Timestamp("2026-10-20T09:15:00+01:00")
	for name, spoil := range map[string]func(*Document){
		"network":      func(d *Document) { d.Networks[0].Pinned = bad },
		"highlight":    func(d *Document) { d.Networks[0].Highlights[0].Pinned = bad },
		"point":        func(d *Document) { d.Networks[0].Points[0].Pinned = bad },
		"network area": func(d *Document) { d.Networks[0].Areas[0].Pinned = bad },
		"plan":         func(d *Document) { d.Plans[0].Pinned = bad },
		"track":        func(d *Document) { d.Tracks[0].Pinned = bad },
		"waypoint":     func(d *Document) { d.Waypoints[0].Pinned = bad },
		"area":         func(d *Document) { d.Areas[0].Pinned = bad },
	} {
		d := pinnedDocument()
		spoil(d)
		_, err := Encode(d)
		if err == nil || !strings.Contains(err.Error(), "pinned") {
			t.Errorf("%s: Encode = %v, want a refusal naming pinned", name, err)
		}
	}
}
