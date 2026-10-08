package gpx

import (
	"encoding/xml"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// The read side of Dave's ruling of 2026-10-08 ("Plans should definitely be
// exported as GPX routes"): a file written from a route DECODES as a GPX
// route, and a file written from a track decodes as a GPX track.
//
// ⚠ VNX HAS NO GPX READER, so this decodes with encoding/xml into the GPX 1.1
// element names. A substring test (gpx_test.go) passes on a <rtept> written
// inside a <trk>; a decode puts every point under the element that holds it,
// so it cannot. Viatrail's importer is the real reader, and its round-trip test
// lives in Viatrail beside the importer.

type gpxFile struct {
	XMLName xml.Name `xml:"http://www.topografix.com/GPX/1/1 gpx"`
	Rtes    []gpxRte `xml:"rte"`
	Trks    []gpxTrk `xml:"trk"`
}

type gpxRte struct {
	Name   string   `xml:"name"`
	Desc   string   `xml:"desc"`
	Points []gpxPt  `xml:"rtept"`
	Stray  []gpxSeg `xml:"trkseg"`
}

type gpxTrk struct {
	Name string   `xml:"name"`
	Segs []gpxSeg `xml:"trkseg"`
}

type gpxSeg struct {
	Points []gpxPt `xml:"trkpt"`
}

type gpxPt struct {
	Lat  string `xml:"lat,attr"`
	Lon  string `xml:"lon,attr"`
	Ele  string `xml:"ele"`
	Time string `xml:"time"`
}

func TestARouteReadsBackAsAGPXRoute(t *testing.T) {
	d := &vnx.Document{
		Tracks: []vnx.Track{{Name: "Walked", Segments: []vnx.Segment{
			{Pips: []vnx.Position{{Lon: 5, Lat: 5}, {Lon: 6, Lat: 6}}},
			{Pips: []vnx.Position{{Lon: 7, Lat: 7}}},
		}}},
		Routes: []vnx.Route{route(vnx.Route{Name: "Planned", Notes: "two days"},
			lineArc{Pips: []vnx.Position{
				{Lon: 1, Lat: 1, Ele: f(100)},
				{Lon: 2, Lat: 2},
			}},
			lineArc{Pips: []vnx.Position{
				{Lon: 2, Lat: 2},
				{Lon: 3, Lat: 3, Ele: f(300)},
			}},
		)},
	}
	out, _ := write(t, d)
	var g gpxFile
	if err := xml.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("the file does not decode as GPX 1.1: %v\n%s", err, out)
	}

	if len(g.Rtes) != 1 {
		t.Fatalf("%d <rte>s decoded, want 1\n%s", len(g.Rtes), out)
	}
	r := g.Rtes[0]
	if r.Name != "Planned" || r.Desc != "two days" {
		t.Errorf("<rte> name %q desc %q, want %q and %q", r.Name, r.Desc, "Planned", "two days")
	}
	if len(r.Stray) != 0 {
		t.Errorf("a <trkseg> was written inside the <rte>\n%s", out)
	}
	want := []gpxPt{
		{Lat: "1", Lon: "1", Ele: "100"},
		{Lat: "2", Lon: "2"},
		{Lat: "3", Lon: "3", Ele: "300"},
	}
	if len(r.Points) != len(want) {
		t.Fatalf("%d <rtept>s decoded, want %d\n%s", len(r.Points), len(want), out)
	}
	for i := range want {
		if r.Points[i] != want[i] {
			t.Errorf("<rtept> %d is %+v, want %+v", i, r.Points[i], want[i])
		}
	}

	// The track did not move: it still decodes as a <trk>, its pause intact.
	if len(g.Trks) != 1 || g.Trks[0].Name != "Walked" {
		t.Fatalf("the track did not decode as one <trk> named Walked: %+v\n%s", g.Trks, out)
	}
	if n := len(g.Trks[0].Segs); n != 2 {
		t.Errorf("the track decoded with %d <trkseg>s, want 2", n)
	}
	for _, tr := range g.Trks {
		if tr.Name == "Planned" {
			t.Errorf("the route ALSO decoded as a <trk>\n%s", out)
		}
	}
}
