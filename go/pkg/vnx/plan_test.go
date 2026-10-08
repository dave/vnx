package vnx

import (
	"fmt"
	"testing"
)

// twoArcPlan is half hand-drawn, half routed, and walks its second arc
// BACKWARDS: arc "routed" runs from col to top, and the walk goes top to col.
const twoArcPlan = `{"vnx":"0.9.0","plans":[{
  "folder":"cairngorms","name":"Braeriach direct",
  "style":{"color":"#0b7a75","width":4,"opacity":1,"line-style":"dashed"},
  "nodes":{"start":[-3.7280,57.0782,1296],"top":[-3.7101,57.0699,1150],"col":[-3.7020,57.0655,1044]},
  "arcs":{
    "drawn":{"from":"start","to":"top","pips":[[-3.7190,57.0740,1221.5]],
     "data":[{"@type":"app.viatrail.routing","kind":"straight","source":"user-drawn"}]},
    "routed":{"from":"col","to":"top","pips":[],
     "data":[{"@type":"app.viatrail.routing","kind":"astar","source":"engine"}]}},
  "walk":[{"arc":"drawn","from":"start"},{"arc":"routed","to":"col"}]}]}`

func TestPlanRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(twoArcPlan))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean plan produced %v", diags)
	}
	r := doc.Plans[0]
	if len(r.Nodes) != 3 || len(r.Arcs) != 2 || len(r.Walk) != 2 {
		t.Fatalf("got %d nodes, %d arcs, %d passes", len(r.Nodes), len(r.Arcs), len(r.Walk))
	}
	// Arc BOUNDARIES survive with their own routing records, not as one merged
	// point list.
	if r.Arcs["drawn"].Data[0].Body["kind"] != "straight" || r.Arcs["routed"].Data[0].Body["kind"] != "astar" {
		t.Fatalf("a routing record was lost: %+v", r.Arcs)
	}
	out, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	again, diags, err := Decode(out)
	if err != nil || len(diags) != 0 {
		t.Fatalf("re-read: %v %v", err, diags)
	}
	if fmt.Sprint(again.Plans[0].Walk[1]) != fmt.Sprint(r.Walk[1]) {
		t.Fatalf("the walk did not survive a round trip: %v vs %v", again.Plans[0].Walk, r.Walk)
	}
}

// A plan walks its arcs in either direction, and its line is drawn the way it
// is walked: the reversed arc comes out col-last, not top-last.
func TestAPlanDrawsAReversedArcTheWayItIsWalked(t *testing.T) {
	doc, _, err := Decode([]byte(twoArcPlan))
	if err != nil {
		t.Fatal(err)
	}
	r := doc.Plans[0]
	line, err := WalkLine(r.Nodes, r.Arcs, r.Walk)
	if err != nil {
		t.Fatal(err)
	}
	want := []LonLat{{-3.7280, 57.0782}, {-3.7190, 57.0740}, {-3.7101, 57.0699}, {-3.7020, 57.0655}}
	if len(line) != len(want) {
		t.Fatalf("line = %v", line)
	}
	for i, p := range line {
		if (LonLat{p.Lon, p.Lat}) != want[i] {
			t.Fatalf("point %d = %v, want %v (line %v)", i, p, want[i], line)
		}
	}
}

// A plan is its own highlight: where a highlight would be dropped, the plan is. Its
// walk runs node to node, so a position, a turn or a pin drops it too.
func TestEachPlanRemedy(t *testing.T) {
	plan := func(walk string) string {
		return `{"vnx":"0.9.0","plans":[{"name":"keep","nodes":{"a":[-3.72,57.07],"b":[-3.71,57.06]},` +
			`"arcs":{"ab":{"from":"a","to":"b","pips":[]}},"walk":[{"arc":"ab","from":"a","to":"b"}]},` +
			`{"name":"r","nodes":{"a":[-3.72,57.07],"b":[-3.71,57.06],"c":[-3.70,57.05]},` +
			`"arcs":{"ab":{"from":"a","to":"b","pips":[]},"bc":{"from":"b","to":"c","pips":[]}},` +
			`"walk":` + walk + `}]}`
	}
	type diag struct{ path, code string }
	for _, tc := range []struct {
		name string
		doc  string
		want []diag
	}{
		{"a position from", plan(`[{"arc":"ab","from":[-3.715,57.065],"to":"b"}]`),
			[]diag{{"/plans/1/walk/0/from", CodePlanPinOrPosition}}},
		{"a position to", plan(`[{"arc":"ab","from":"a","to":[-3.715,57.065]}]`),
			[]diag{{"/plans/1/walk/0/to", CodePlanPinOrPosition}}},
		{"a turn", plan(`[{"arc":"ab","from":"a","turn":[-3.715,57.065],"to":"a"}]`),
			[]diag{{"/plans/1/walk/0/turn", CodePlanPinOrPosition}}},
		{"a pin", plan(`[{"arc":"ab","from":"a"},{"arc":"bc","pin":[-3.715,57.065],"to":"c"}]`),
			[]diag{{"/plans/1/walk/1/pin", CodePlanPinOrPosition}}},
		{"a walk that does not join", plan(`[{"arc":"ab","from":"b"},{"arc":"bc","to":"c"}]`),
			[]diag{{"/plans/1/walk/1", CodeWalkNotConnected}}},
		{"a pass naming an arc the plan does not own", plan(`[{"arc":"cd","from":"c","to":"d"}]`),
			[]diag{{"/plans/1/walk/0", CodePassArcAbsent}}},
		{"no walk", `{"vnx":"0.9.0","plans":[{"name":"keep","walk":[]},{"name":"r"}]}`,
			[]diag{{"/plans/1/walk", CodeWrongTypeRequired}}},
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
			if len(doc.Plans) != 1 || doc.Plans[0].Name != "keep" {
				t.Fatalf("plans = %+v, want only keep", doc.Plans)
			}
		})
	}
}

// A plan with an empty walk is empty, not broken — it has nothing to draw but
// nothing is wrong with it either.
func TestEmptyPlanIsNotDiagnosed(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.9.0","plans":[{"name":"empty","walk":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("got %v", diags)
	}
}
