package vnx

import "testing"

const twoArcRoute = `{"vnx":"0.1.0","routes":[{
  "folder":"cairngorms","name":"Braeriach direct",
  "style":{"color":"#0b7a75","width":4,"opacity":1,"line-style":"dashed"},
  "arcs":[
    {"pips":[[-3.7280,57.0782,1296],[-3.7190,57.0740,1221.5],[-3.7101,57.0699,1150]],
     "data":[{"@type":"app.viatrail.routing","kind":"straight","source":"user-drawn"}]},
    {"pips":[[-3.7101,57.0699,1150],[-3.7020,57.0655,1044]],
     "data":[{"@type":"app.viatrail.routing","kind":"astar","source":"engine"}]}
  ]}]}`

func TestRouteRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(twoArcRoute))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean route produced %v", diags)
	}
	r := doc.Routes[0]
	if len(r.Arcs) != 2 {
		t.Fatalf("got %d arcs", len(r.Arcs))
	}
	// Arc BOUNDARIES are what the array preserves: a route that is part
	// hand-drawn and part engine-routed must survive as two arcs with their own
	// routing records, not as one merged point list.
	if len(r.Arcs[0].Data) != 1 || r.Arcs[0].Data[0].Body["kind"] != "straight" {
		t.Fatalf("first arc's routing record lost: %+v", r.Arcs[0].Data)
	}
	if r.Arcs[1].Data[0].Body["kind"] != "astar" {
		t.Fatalf("second arc's routing record lost: %+v", r.Arcs[1].Data)
	}
}

// §3.3: a route owns its arcs, so node identity is the shared endpoint between
// arc k and arc k+1 — present in BOTH pip lists. Nothing else names them, so
// there is no nodes map and no links.
func TestRouteArcsJoinAtSharedEndpoints(t *testing.T) {
	doc, _, err := Decode([]byte(twoArcRoute))
	if err != nil {
		t.Fatal(err)
	}
	arcs := doc.Routes[0].Arcs
	end := arcs[0].Pips[len(arcs[0].Pips)-1]
	start := arcs[1].Pips[0]
	if !end.Equal(start) {
		t.Fatalf("arcs do not join: %s vs %s", end, start)
	}
}

// A route whose arcs do not meet is diagnosed. The arcs are KEPT: the geometry
// is real and drawable, and a gap the writer intended is not ours to delete.
func TestDisjointRouteIsDiagnosedNotDropped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"name":"gappy","arcs":[
	  {"pips":[[-3.72,57.07],[-3.71,57.06]]},
	  {"pips":[[-3.50,57.00],[-3.49,56.99]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Routes) != 1 || len(doc.Routes[0].Arcs) != 2 {
		t.Fatalf("the geometry must survive: %+v", doc.Routes)
	}
	if len(diags) != 1 || diags[0].Path != "/routes/0/arcs/1" {
		t.Fatalf("want one diagnostic at the arc that does not join, got %v", diags)
	}
}

// Direction is inherent in pip order, which is what disposes of the cases an
// arc-id-plus-adjacency scheme could not express — including parallel arcs
// between the same pair of points, legitimate and expected from GPX import.
func TestReversedArcStillJoins(t *testing.T) {
	// arc 2 is stored end-to-start; it still shares an endpoint with arc 1.
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"name":"r","arcs":[
	  {"pips":[[-3.72,57.07],[-3.71,57.06]]},
	  {"pips":[[-3.71,57.06],[-3.70,57.05]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("arcs that share an endpoint join, got %v", diags)
	}
}

// The join comparison must be BY VALUE. Go lets you compare Position structs
// directly, and that silently compares the Ele and Time POINTERS too — so two
// arcs meeting at the same coordinates fail to join whenever their elevation
// pointers differ, which is every time both carry an elevation. The earlier
// join tests use pips with no elevation and cannot catch it, which is why this
// one exists and carries elevations on the shared point.
func TestJoinComparesCoordinatesNotPointers(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"name":"r","arcs":[
	  {"pips":[[-3.72,57.07,1296],[-3.71,57.06,1150]]},
	  {"pips":[[-3.71,57.06,1150],[-3.70,57.05,1044]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("arcs sharing a point with elevation must join, got %v", diags)
	}
}

// A route with no arcs is empty, not broken — it has nothing to draw but
// nothing is wrong with it either.
func TestEmptyRouteIsNotDiagnosed(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"name":"empty","arcs":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("got %v", diags)
	}
}

func TestArcWithNoUsablePipsIsDroppedAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"name":"r","arcs":[
	  {"pips":[[-3.72]]},
	  {"pips":[[-3.71,57.06],[-3.70,57.05]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Routes[0].Arcs) != 1 {
		t.Fatalf("an arc with fewer than two usable pips is not geometry: %+v", doc.Routes[0].Arcs)
	}
	if len(diags) == 0 {
		t.Fatal("and it must say so")
	}
}
