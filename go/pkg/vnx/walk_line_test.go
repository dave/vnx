package vnx

import (
	"fmt"
	"strings"
	"testing"
)

// walkLineNet is one straight arc running due east along latitude 0, from
// node w (lon 0) through interior pips at lon 1, 2 and 3 to node e (lon 4),
// plus a zero-pip arc ne from e north to n. Every leg below is read in whole
// degrees, so a leg that runs the wrong way, or past where it stops, is
// visible in the numbers.
func walkLineNet() (map[string]Vertex, map[string]Arc) {
	nodes := map[string]Vertex{"w": {Lon: 0, Lat: 0}, "e": {Lon: 4, Lat: 0}, "n": {Lon: 4, Lat: 1}}
	arcs := map[string]Arc{
		"we": {From: "w", To: "e", Pips: []Vertex{{Lon: 1}, {Lon: 2}, {Lon: 3}}},
		"en": {From: "e", To: "n"},
	}
	return nodes, arcs
}

func legString(legs [][]Position) string {
	var parts []string
	for _, leg := range legs {
		var ps []string
		for _, p := range leg {
			ps = append(ps, fmt.Sprintf("%g,%g", p.Lon, p.Lat))
		}
		parts = append(parts, strings.Join(ps, " "))
	}
	return strings.Join(parts, " | ")
}

func posEnd(lon, lat float64) *Endpoint { return &Endpoint{At: &LonLat{Lon: lon, Lat: lat}} }
func nd(id string) *Endpoint            { return &Endpoint{Node: id} }

// TestWalkLegsDrawEachFormTheWayItIsWalked is SPEC §3.3.3's "what a pass
// covers", one case per form: entry, then turn, then exit, along the arc's
// line whatever order those points lie in.
func TestWalkLegsDrawEachFormTheWayItIsWalked(t *testing.T) {
	for _, tc := range []struct {
		name string
		walk []Pass
		want string
	}{
		{"node to node", []Pass{{Arc: "we", From: nd("w"), To: nd("e")}},
			"0,0 1,0 2,0 3,0 4,0"},
		{"an arc walked against its direction is reversed", []Pass{{Arc: "we", From: nd("e"), To: nd("w")}},
			"4,0 3,0 2,0 1,0 0,0"},
		{"a mid-arc start is cut, not snapped", []Pass{{Arc: "we", From: posEnd(1.5, 0), To: nd("e")}},
			"1.5,0 2,0 3,0 4,0"},
		{"a mid-arc end", []Pass{{Arc: "we", From: nd("w")}, {Arc: "en", To: posEnd(4, 0.5)}},
			"0,0 1,0 2,0 3,0 4,0 | 4,0 4,0.5"},
		{"whole walk inside one arc, walked backwards", []Pass{{Arc: "we", From: posEnd(3.5, 0), To: posEnd(1.5, 0)}},
			"3.5,0 3,0 2,0 1.5,0"},
		{"out to a turn and back to the node it left", []Pass{{Arc: "we", From: nd("w"), Turn: &LonLat{Lon: 2.5}, To: nd("w")}},
			"0,0 1,0 2,0 2.5,0 2,0 1,0 0,0"},
		{"a turn with no to leaves by the node it entered", []Pass{{Arc: "en", From: nd("n")}, {Arc: "we", Turn: &LonLat{Lon: 2.5}}, {Arc: "en", To: nd("n")}},
			"4,1 4,0 | 4,0 3,0 2.5,0 3,0 4,0 | 4,0 4,1"},
		{"start mid-arc, turn back past the start, end at a node", []Pass{{Arc: "we", From: posEnd(2, 0), Turn: &LonLat{Lon: 0.5}, To: nd("e")}},
			"2,0 1,0 0.5,0 1,0 2,0 3,0 4,0"},
		{"turn, then stop part-way back", []Pass{{Arc: "we", From: nd("w"), Turn: &LonLat{Lon: 3.5}, To: posEnd(1.5, 0)}},
			"0,0 1,0 2,0 3,0 3.5,0 3,0 2,0 1.5,0"},
		{"a degenerate pass is a point", []Pass{{Arc: "we", From: posEnd(2.5, 0), To: posEnd(2.5, 0)}},
			"2.5,0 2.5,0"},
		{"a zero-pip arc is its two nodes", []Pass{{Arc: "en", From: nd("e"), To: nd("n")}},
			"4,0 4,1"},
		{"a position any distance off the arc lands on its nearest point", []Pass{{Arc: "we", From: nd("w"), To: posEnd(2.5, 40)}},
			"0,0 1,0 2,0 2.5,0"},
		{"an empty walk covers nothing", []Pass{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, arcs := walkLineNet()
			legs, err := WalkLegs(nodes, arcs, tc.walk)
			if err != nil {
				t.Fatal(err)
			}
			if got := legString(legs); got != tc.want {
				t.Fatalf("legs = %s\n want %s", got, tc.want)
			}
		})
	}
}

// WalkLine is the legs joined, with the point two legs share written once.
func TestWalkLineJoinsItsLegsOnce(t *testing.T) {
	nodes, arcs := walkLineNet()
	line, err := WalkLine(nodes, arcs, []Pass{{Arc: "we", From: nd("w")}, {Arc: "en", To: nd("n")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := legString([][]Position{line}); got != "0,0 1,0 2,0 3,0 4,0 4,1" {
		t.Fatalf("line = %s", got)
	}
}

// A walk that does not resolve is an error naming the pass and the rule. The
// fallback it replaces — the whole arc, forwards — drew a line nobody walked.
func TestWalkLegsRefuseAWalkThatDoesNotResolve(t *testing.T) {
	for _, tc := range []struct {
		name string
		walk []Pass
		want string
	}{
		{"an absent arc", []Pass{{Arc: "ghost", From: nd("w"), To: nd("e")}}, CodePassArcAbsent},
		{"a node that is not an end", []Pass{{Arc: "en", From: nd("w"), To: nd("n")}}, CodeWalkNotConnected},
		{"a walk that does not join", []Pass{{Arc: "we", From: nd("e")}, {Arc: "en", To: nd("n")}}, CodeWalkNotConnected},
		{"a missing to", []Pass{{Arc: "we", From: nd("w")}}, CodeWalkSpelling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, arcs := walkLineNet()
			_, err := WalkLegs(nodes, arcs, tc.walk)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %s", err, tc.want)
			}
		})
	}
	nodes, arcs := walkLineNet()
	arcs["wx"] = Arc{From: "w", To: "x"}
	if _, err := WalkLegs(nodes, arcs, []Pass{{Arc: "wx", From: nd("w"), To: nd("x")}}); err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Fatalf("an arc naming an absent node: err = %v", err)
	}
}

// Where two segments are equally near a position, the earlier one along the
// arc wins (SPEC §3.3.3). The arc runs out and back along one line, so a point
// beside it is equally near both runs, and the tie decides which half the walk
// stops on.
func TestNearestPointTiesGoToTheEarlierSegment(t *testing.T) {
	nodes := map[string]Vertex{"a": {Lon: 0, Lat: 0}, "b": {Lon: 0, Lat: 0.5}}
	arcs := map[string]Arc{"hairpin": {From: "a", To: "b", Pips: []Vertex{{Lon: 2, Lat: 0}, {Lon: 2, Lat: 0.5}}}}
	legs, err := WalkLegs(nodes, arcs, []Pass{{Arc: "hairpin", From: nd("a"), To: posEnd(1, 0.25)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := legString(legs); got != "0,0 1,0" {
		t.Fatalf("legs = %s, want the walk to stop on the first run of the hairpin", got)
	}
}
