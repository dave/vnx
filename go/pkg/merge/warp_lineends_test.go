package merge

import (
	"math"
	"strings"
	"testing"
)

// WarpLineEnds is the import's own end move, nothing else: for every mix of
// targets its points equal warpLegEndpoints' bitwise, Ele pointers included,
// and its error is the same (a sample builder calls it directly).
func TestWarpLineEndsEqualsTheImportWarp(t *testing.T) {
	ele := func(v float64) *float64 { return &v }
	leg := []Point{
		{Lat: -38.6450, Lon: -71.0900, Ele: ele(812)},
		{Lat: -38.6455, Lon: -71.0905},
		{Lat: -38.6460, Lon: -71.0910, Ele: ele(830)},
		{Lat: -38.6465, Lon: -71.0912, Ele: ele(841.5)},
		{Lat: -38.6470, Lon: -71.0920, Ele: ele(850)},
	}
	start := &LatLon{Lat: -38.64503, Lon: -71.08992} // ~7 m off
	end := &LatLon{Lat: -38.64702, Lon: -71.09203}   // ~3.5 m off
	cases := []struct {
		name       string
		start, end *LatLon
	}{
		{"neither", nil, nil},
		{"start", start, nil},
		{"end", nil, end},
		{"both", start, end},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := WarpLineEnds(leg, c.start, c.end)
			if err != nil {
				t.Fatal(err)
			}
			want, _, err := warpLegEndpoints(leg, warpTargetAt(c.start), warpTargetAt(c.end))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("%d points, want %d", len(got), len(want))
			}
			for i := range got {
				if math.Float64bits(got[i].Lat) != math.Float64bits(want[i].Lat) ||
					math.Float64bits(got[i].Lon) != math.Float64bits(want[i].Lon) || got[i].Ele != want[i].Ele {
					t.Errorf("point %d: %+v, want %+v", i, got[i], want[i])
				}
			}
			// The control: a target moves its end exactly onto it.
			if c.start != nil && (got[0].Lat != c.start.Lat || got[0].Lon != c.start.Lon) {
				t.Errorf("start at %v,%v, want %v", got[0].Lat, got[0].Lon, *c.start)
			}
			if c.end != nil && (got[len(got)-1].Lat != c.end.Lat || got[len(got)-1].Lon != c.end.Lon) {
				t.Errorf("end at %v,%v, want %v", got[len(got)-1].Lat, got[len(got)-1].Lon, *c.end)
			}
		})
	}
	if _, err := WarpLineEnds(leg[:1], start, nil); err == nil || !strings.Contains(err.Error(), "need at least 2") {
		t.Errorf("one point: error %v, want warpLegEndpoints' 'need at least 2'", err)
	}
}
