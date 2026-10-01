package vnx

import (
	"fmt"
	"strings"
	"testing"
)

const simpleNetwork = `{"vnx":"0.8.0","networks":[{
  "folder":"approaches","name":"Lairig Ghru approaches",
  "nodes":{"node-1":[-3.6712,57.1180,340],"node-2":[-3.6605,57.1042,512.5],"node-3":[-3.6488,57.0977,604]},
  "arcs":{
    "arc-1":{"from":"node-1","to":"node-2","pips":[[-3.6644,57.1085,451.9]]},
    "arc-2":{"from":"node-2","to":"node-3","pips":[[-3.6540,57.1009,561]]}},
  "highlights":[{"name":"Chalamain Gap","walk":[
    {"arc":"arc-1","from":"node-1","pinned":true},
    {"arc":"arc-2","to":[-3.6540,57.1009]}]}]}]}`

func TestNetworkRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(simpleNetwork))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean network produced %v", diags)
	}
	n := doc.Networks[0]
	if len(n.Nodes) != 3 || len(n.Arcs) != 2 || len(n.Highlights) != 1 {
		t.Fatalf("got %d nodes, %d arcs, %d highlights", len(n.Nodes), len(n.Arcs), len(n.Highlights))
	}
	if a := n.Arcs["arc-1"]; a.From != "node-1" || a.To != "node-2" || len(a.Pips) != 1 {
		t.Fatalf("arc-1 = %+v, want node-1 to node-2 with its one interior pip", a)
	}
	w := n.Highlights[0].Walk
	if len(w) != 2 {
		t.Fatalf("got %d passes", len(w))
	}
	if w[0].From == nil || w[0].From.Node != "node-1" || w[0].To != nil || !w[0].Pinned {
		t.Fatalf("first pass wrong: %+v", w[0])
	}
	if w[1].From != nil || w[1].To == nil || w[1].To.At == nil || *w[1].To.At != (LonLat{Lon: -3.6540, Lat: 57.1009}) {
		t.Fatalf("last pass wrong: %+v", w[1])
	}
}

// ⚠ IDS ARE SCOPED TO THEIR CONTAINING OBJECT, not the document. A route's
// node-1 and a network's node-1 are different nodes. A reader that builds one
// global map silently merges them, and the geometry it draws is a blend of two
// unrelated things.
func TestNodeIDsAreScopedToTheirContainer(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.8.0",
	  "networks":[{"name":"net","nodes":{"node-1":[-3.67,57.11],"node-2":[-3.66,57.10]},
	    "arcs":{"arc-1":{"from":"node-1","to":"node-2","pips":[]}}}],
	  "routes":[{"name":"r","nodes":{"node-1":[-100.0,10.0],"node-2":[-100.1,10.1]},
	    "arcs":{"arc-1":{"from":"node-1","to":"node-2","pips":[]}},
	    "walk":[{"arc":"arc-1","from":"node-1","to":"node-2"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("got %v", diags)
	}
	if doc.Networks[0].Nodes["node-1"].Lon == doc.Routes[0].Nodes["node-1"].Lon {
		t.Fatal("the network's node-1 and the route's node-1 must not be conflated")
	}
}

// walkNetwork is a network of six nodes on a line and five arcs between them,
// a..f, arc ab from a to b and so on, with arc cb running BACKWARDS from c to
// b so that a walk crossing it covers it in reverse. It exists so each case
// below states only its walk.
func walkNetwork(highlights string) string {
	return `{"vnx":"0.8.0","networks":[{"name":"n",` +
		`"nodes":{"a":[-3.60,57.10],"b":[-3.59,57.10],"c":[-3.58,57.10],"d":[-3.57,57.10],"e":[-3.56,57.10],"f":[-3.55,57.10]},` +
		`"arcs":{"ab":{"from":"a","to":"b","pips":[[-3.595,57.101]]},"cb":{"from":"c","to":"b","pips":[]},` +
		`"cd":{"from":"c","to":"d","pips":[[-3.575,57.102],[-3.572,57.101]]},"de":{"from":"d","to":"e","pips":[]},` +
		`"ef":{"from":"e","to":"f","pips":[]}},` +
		`"highlights":[` + highlights + `]}]}`
}

// Every walk form SPEC §3.3.3 names reads clean: the examples it gives, a
// degenerate pass, an empty walk and a zero-pip arc. A rule written too tight
// refuses one of these, which is the half a refusal test cannot see.
func TestEveryWalkFormReadsClean(t *testing.T) {
	for _, tc := range []struct{ name, walk string }{
		{"node to node", `[{"arc":"ab","from":"a"},{"arc":"cb"},{"arc":"cd","to":"d"}]`},
		{"mid-arc start, pinned, turn, revisit, mid-arc end",
			`[{"arc":"ab","from":[-3.597,57.1005],"to":"b"},{"arc":"cb","pinned":true},{"arc":"cd","turn":[-3.574,57.102]},` +
				`{"arc":"cb"},{"arc":"ab","to":[-3.598,57.1002]}]`},
		{"whole walk inside one arc", `[{"arc":"cd","from":[-3.576,57.101],"to":[-3.573,57.101]}]`},
		{"start mid-arc, turn back past the start, end at a node",
			`[{"arc":"cd","from":[-3.575,57.102],"turn":[-3.579,57.1005],"to":"d"}]`},
		{"turn then stop mid-arc", `[{"arc":"de","from":"d"},{"arc":"ef","turn":[-3.551,57.10],"to":[-3.559,57.10]}]`},
		{"out and back to the node it left", `[{"arc":"de","from":"d","turn":[-3.565,57.10],"to":"d"}]`},
		{"degenerate: entry and exit at one point", `[{"arc":"cd","from":[-3.575,57.102],"to":[-3.575,57.102]}]`},
		{"a node end with no turn is the other end", `[{"arc":"de","from":"e"},{"arc":"cd","to":"c"}]`},
		{"empty walk", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, diags, err := Decode([]byte(walkNetwork(`{"name":"c","walk":` + tc.walk + `}`)))
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 0 {
				t.Fatalf("a valid walk produced %v", diags)
			}
			if len(doc.Networks[0].Highlights) != 1 {
				t.Fatal("a valid walk's highlight was dropped")
			}
		})
	}
}

// TestEachWalkRemedy is one case per SPEC §6.2 row that concerns a network:
// the code, the path, and what survives. The network always carries a second,
// valid highlight named "keep", so a remedy that dropped more than its highlight goes
// red here, not only one that dropped less.
func TestEachWalkRemedy(t *testing.T) {
	const keep = `{"name":"keep","walk":[{"arc":"de","from":"d","to":"e"}]}`
	type diag struct{ path, code string }
	for _, tc := range []struct {
		name       string
		doc        string
		want       []diag
		highlights int // highlights that survive, "keep" included
	}{
		{"a pass names an absent arc", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","from":"a"},{"arc":"ghost","to":"c"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/1", CodePassArcAbsent}}, 1},
		{"a pass that is not an object", walkNetwork(keep + `,{"name":"c","walk":["ab"]}`),
			[]diag{{"/networks/0/highlights/1/walk/0", CodePassArcAbsent}}, 1},
		{"a turn that is not a position", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","turn":"d","to":"c"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/turn", CodePositionUnusable}}, 1},
		{"a from that will not parse", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":[-3.57],"to":"d"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/from", CodePositionUnusable}}, 1},
		{"a from off the globe", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":[-3.57,91],"to":"d"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/from", CodePositionUnusable}}, 1},
		{"a to that is a number", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","to":7}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/to", CodePositionUnusable}}, 1},
		{"a turn with an elevation keeps the highlight", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","turn":[-3.575,57.102,900],"to":"c"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/turn", CodeExtraChannel}}, 2},
		{"a pinned pass with a turn", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","turn":[-3.575,57.102],"to":"c","pinned":true}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0", CodePinnedTurn}}, 1},
		{"from on a pass after the first", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","from":"a"},{"arc":"cb","from":"b","to":"c"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/1/from", CodeWalkSpelling}}, 1},
		{"a first pass without from", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","to":"b"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0", CodeWalkSpelling}}, 1},
		{"a last pass without to", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","from":"a"},{"arc":"cb"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/1", CodeWalkSpelling}}, 1},
		{"to on a pass that is neither last nor from a position", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","from":"a","to":"b"},{"arc":"cb","to":"c"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/to", CodeWalkSpelling}}, 1},
		{"a position from without a to", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":[-3.575,57.102]},{"arc":"de","to":"e"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0", CodeWalkSpelling}}, 1},
		{"a from node that is not an end of its arc", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"a","to":"d"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/from", CodeWalkNotConnected}}, 1},
		{"a from node that is in no arc of the network", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"ghost","to":"d"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/from", CodeWalkNotConnected}}, 1},
		{"a pass whose arc does not touch the walk", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","from":"a"},{"arc":"de","to":"e"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/1", CodeWalkNotConnected}}, 1},
		{"a to node that is not an end of its arc", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","to":"e"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/to", CodeWalkNotConnected}}, 1},
		{"a pass other than the last leaving at a position", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":[-3.575,57.102],"to":[-3.572,57.101]},{"arc":"de","to":"e"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/to", CodeWalkNotConnected}}, 1},
		{"pinned of the wrong type drops the field only", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","to":"d","pinned":"yes"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/pinned", CodeWrongTypeOptional}}, 2},
		{"a highlight with no walk", walkNetwork(keep + `,{"name":"c"}`),
			[]diag{{"/networks/0/highlights/1/walk", CodeWrongTypeRequired}}, 1},
		{"a walk that is not an array", walkNetwork(keep + `,{"name":"c","walk":{}}`),
			[]diag{{"/networks/0/highlights/1/walk", CodeWrongTypeRequired}}, 1},
		{"an unknown key on a pass keeps the highlight", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"cd","from":"c","to":"d","via":true}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0/via", CodeUnknownKey}}, 2},
		{"only the first fault in a walk is reported", walkNetwork(keep + `,{"name":"c","walk":[{"arc":"ab","to":"b"},{"arc":"ghost"}]}`),
			[]diag{{"/networks/0/highlights/1/walk/0", CodeWalkSpelling}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, diags, err := Decode([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			var got []diag
			for _, d := range diags {
				got = append(got, diag{d.Path, d.Code})
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("diagnostics = %v, want %v\n%v", got, tc.want, diags)
			}
			if n := len(doc.Networks[0].Highlights); n != tc.highlights {
				t.Fatalf("%d highlight(s) survive, want %d", n, tc.highlights)
			}
			if doc.Networks[0].Highlights[0].Name != "keep" {
				t.Fatal("the valid highlight was dropped")
			}
		})
	}
}

// The channels a walk position and a node do not carry are dropped, and what
// is kept is exactly longitude and latitude (and a node's elevation).
func TestExtraChannelsAreDroppedFromNodesAndWalkPositions(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.8.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.60,57.10,340,1778569391000],"b":[-3.59,57.10]},
	  "arcs":{"ab":{"from":"a","to":"b","pips":[]}},
	  "highlights":[{"name":"c","walk":[{"arc":"ab","from":[-3.598,57.10,null,1778569391000],"to":"b"}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"/networks/0/nodes/a": CodeExtraChannel, "/networks/0/highlights/0/walk/0/from": CodeExtraChannel}
	if len(diags) != len(want) {
		t.Fatalf("got %v, want %v", diags, want)
	}
	for _, d := range diags {
		if want[d.Path] != d.Code {
			t.Fatalf("unexpected %v", d)
		}
	}
	n := doc.Networks[0]
	if a := n.Nodes["a"]; a.Ele == nil || *a.Ele != 340 {
		t.Fatalf("node a lost its elevation along with its time: %+v", a)
	}
	if got := *n.Highlights[0].Walk[0].From.At; got != (LonLat{Lon: -3.598, Lat: 57.10}) {
		t.Fatalf("from = %v", got)
	}
}

// TestEachArcRemedy: an arc that cannot be used is dropped, and every highlight
// walking it goes with it under pass-arc-absent — two diagnostics, because two
// things are gone. A highlight that does not walk it survives.
func TestEachArcRemedy(t *testing.T) {
	const keep = `{"name":"keep","walk":[{"arc":"ok","from":"a","to":"b"}]}`
	const walker = `{"name":"walker","walk":[{"arc":"bad","from":"b","to":"c"}]}`
	net := func(badArc, nodes string) string {
		return `{"vnx":"0.8.0","networks":[{"name":"n","nodes":{` + nodes + `},` +
			`"arcs":{"ok":{"from":"a","to":"b","pips":[]},"bad":` + badArc + `},` +
			`"highlights":[` + keep + `,` + walker + `]}]}`
	}
	const abc = `"a":[-3.60,57.10],"b":[-3.59,57.10],"c":[-3.58,57.10]`
	type diag struct{ path, code string }
	walkerGone := diag{"/networks/0/highlights/1/walk/0", CodePassArcAbsent}
	for _, tc := range []struct {
		name string
		doc  string
		want []diag
	}{
		{"a self-loop", net(`{"from":"b","to":"b","pips":[[-3.585,57.11]]}`, abc),
			[]diag{{"/networks/0/arcs/bad", CodeSelfLoopArc}, walkerGone}},
		{"an arc naming an absent node", net(`{"from":"b","to":"ghost","pips":[]}`, abc),
			[]diag{{"/networks/0/arcs/bad/to", CodeArcNodeAbsent}, walkerGone}},
		{"an arc with no pips key", net(`{"from":"b","to":"c"}`, abc),
			[]diag{{"/networks/0/arcs/bad/pips", CodeWrongTypeRequired}, walkerGone}},
		{"an arc with no from", net(`{"to":"c","pips":[]}`, abc),
			[]diag{{"/networks/0/arcs/bad/from", CodeWrongTypeRequired}, walkerGone}},
		{"a pip that will not parse", net(`{"from":"b","to":"c","pips":[[-3.585]]}`, abc),
			[]diag{{"/networks/0/arcs/bad/pips/0", CodePositionUnusable}, walkerGone}},
		{"a node that will not parse drops its arcs, which drop their highlights",
			net(`{"from":"b","to":"c","pips":[]}`, `"a":[-3.60,57.10],"b":[-3.59,57.10],"c":"here"`),
			[]diag{{"/networks/0/nodes/c", CodePositionUnusable}, {"/networks/0/arcs/bad/to", CodeArcNodeAbsent}, walkerGone}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, diags, err := Decode([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			var got []diag
			for _, d := range diags {
				got = append(got, diag{d.Path, d.Code})
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("diagnostics = %v, want %v\n%v", got, tc.want, diags)
			}
			n := doc.Networks[0]
			if _, ok := n.Arcs["bad"]; ok {
				t.Fatal("the bad arc survived")
			}
			if _, ok := n.Arcs["ok"]; !ok {
				t.Fatal("the good arc was dropped")
			}
			if len(n.Highlights) != 1 || n.Highlights[0].Name != "keep" {
				t.Fatalf("highlights = %+v, want only keep", n.Highlights)
			}
		})
	}
}

// A time on an arc pip drops the time channel of every pip that carries one,
// and says so ONCE for the arc: a thousand-pip recorded arc is one fact, not a
// thousand. The pips themselves, and their elevations, are kept.
func TestArcPipTimeIsDroppedOncePerArc(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.8.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.60,57.10],"b":[-3.59,57.10]},
	  "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.598,57.101,300,1778569391000],[-3.596,57.102],[-3.594,57.103,null,1778569392000]]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != CodeArcPipTime || diags[0].Path != "/networks/0/arcs/ab/pips" {
		t.Fatalf("got %v, want one %s at the arc's pips", diags, CodeArcPipTime)
	}
	pips := doc.Networks[0].Arcs["ab"].Pips
	if len(pips) != 3 || pips[0].Ele == nil || *pips[0].Ele != 300 {
		t.Fatalf("pips = %+v, want all three kept, the first with its elevation", pips)
	}
}

// The encoder refuses what the reader would drop: a writer never emits a
// document its own reader would lose part of.
func TestEncoderRefusesWhatTheReaderWouldDrop(t *testing.T) {
	good := func() Network {
		return Network{Name: "n",
			Nodes:      map[string]Vertex{"a": {Lon: -3.60, Lat: 57.10}, "b": {Lon: -3.59, Lat: 57.10}},
			Arcs:       map[string]Arc{"ab": {From: "a", To: "b"}},
			Highlights: []Highlight{{Name: "c", Walk: []Pass{{Arc: "ab", From: &Endpoint{Node: "a"}, To: &Endpoint{Node: "b"}}}}},
		}
	}
	if _, err := Encode(&Document{Networks: []Network{good()}}); err != nil {
		t.Fatalf("the control network was refused: %v", err)
	}
	for _, tc := range []struct {
		name   string
		break_ func(n *Network)
		want   string
	}{
		{"a self-loop", func(n *Network) { n.Arcs["ab"] = Arc{From: "a", To: "a"} }, "self-loop"},
		{"an absent node", func(n *Network) { n.Arcs["ab"] = Arc{From: "a", To: "ghost"} }, "ghost"},
		{"an absent arc", func(n *Network) { n.Highlights[0].Walk[0].Arc = "ghost" }, CodePassArcAbsent},
		{"a missing to", func(n *Network) { n.Highlights[0].Walk[0].To = nil }, CodeWalkSpelling},
		{"a pinned turn", func(n *Network) {
			n.Highlights[0].Walk[0].Pinned, n.Highlights[0].Walk[0].Turn = true, &LonLat{Lon: -3.595, Lat: 57.10}
		}, CodePinnedTurn},
		{"a to that is not an end", func(n *Network) {
			n.Highlights[0].Walk[0].To = &Endpoint{Node: "a"}
			n.Highlights[0].Walk[0].From = &Endpoint{Node: "ghost"}
		}, CodeWalkNotConnected},
		{"a closed ring", func(n *Network) {
			n.Areas = []Area{{Polygon: []Position{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}, {Lon: 0, Lat: 0}}}}
		}, "repeats its first"},
		{"a two-point ring", func(n *Network) {
			n.Areas = []Area{{Polygon: []Position{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 0}}}}
		}, "3 distinct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := good()
			tc.break_(&n)
			_, err := Encode(&Document{Networks: []Network{n}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %q", err, tc.want)
			}
		})
	}
}
