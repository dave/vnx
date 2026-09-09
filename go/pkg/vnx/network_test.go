package vnx

import "testing"

const simpleNetwork = `{"vnx":"0.1.0","networks":[{
  "folder":"approaches","name":"Lairig Ghru approaches",
  "nodes":{"node-1":[-3.6712,57.1180,340],"node-2":[-3.6605,57.1042,512.5],"node-3":[-3.6488,57.0977,604]},
  "arcs":{
    "arc-1":{"from":"node-1","to":"node-2","pips":[[-3.6712,57.1180,340],[-3.6644,57.1085,451.9],[-3.6605,57.1042,512.5]]},
    "arc-2":{"from":"node-2","to":"node-3","pips":[[-3.6605,57.1042,512.5],[-3.6540,57.1009,561],[-3.6488,57.0977,604]]}},
  "chains":[{"name":"Chalamain Gap","links":[
    {"arc":"arc-1","from":"node-1","to":"node-2"},
    {"arc":"arc-2","from":"node-2","to":[-3.6540,57.1009]}]}]}]}`

func TestNetworkRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(simpleNetwork))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean network produced %v", diags)
	}
	n := doc.Networks[0]
	if len(n.Nodes) != 3 || len(n.Arcs) != 2 || len(n.Chains) != 1 {
		t.Fatalf("got %d nodes, %d arcs, %d chains", len(n.Nodes), len(n.Arcs), len(n.Chains))
	}
	ch := n.Chains[0]
	if len(ch.Links) != 2 {
		t.Fatalf("got %d links", len(ch.Links))
	}
	if ch.Links[0].From.Node != "node-1" || ch.Links[0].To.Node != "node-2" {
		t.Fatalf("node endpoints wrong: %+v", ch.Links[0])
	}
	if ch.Links[1].To.Node != "" || ch.Links[1].To.At == nil {
		t.Fatalf("inline endpoint not read: %+v", ch.Links[1].To)
	}
}

// ⚠ IDS ARE SCOPED TO THEIR CONTAINING OBJECT, not the document. A route's
// node-1 and a network's node-1 are different nodes. A reader that builds one
// global map silently merges them, and the geometry it draws is a blend of two
// unrelated things.
func TestNodeIDsAreScopedToTheirContainer(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0",
	  "networks":[{"name":"net","nodes":{"node-1":[-3.67,57.11],"node-2":[-3.66,57.10]},
	    "arcs":{"arc-1":{"from":"node-1","to":"node-2","pips":[[-3.67,57.11],[-3.66,57.10]]}}}],
	  "routes":[{"name":"r","arcs":[{"pips":[[-100.0,10.0],[-100.1,10.1]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("got %v", diags)
	}
	netNode := doc.Networks[0].Nodes["node-1"]
	routePip := doc.Routes[0].Arcs[0].Pips[0]
	if netNode.Lon == routePip.Lon {
		t.Fatal("the network's node-1 and the route's geometry must not be conflated")
	}
}

// §3.2: two links in ONE chain may share a handle — a turnaround pair. Inline
// positions carry no identity, so the reader must treat value-equal positions
// on the same arc as the same endpoint, or a walk that is in fact continuous
// gets refused.
func TestTurnaroundPairSharingOneHandleIsContinuous(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.6712,57.1180],"b":[-3.6605,57.1042]},
	  "arcs":{"arc-1":{"from":"a","to":"b","pips":[[-3.6712,57.1180],[-3.6660,57.1110],[-3.6605,57.1042]]}},
	  "chains":[{"name":"out and back","links":[
	    {"arc":"arc-1","from":"a","to":[-3.6660,57.1110]},
	    {"arc":"arc-1","from":[-3.6660,57.1110],"to":"a"}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("a turnaround sharing one handle is continuous, got %v", diags)
	}
}

func TestLinkNamingAnAbsentArcIsDroppedAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.67,57.11],"b":[-3.66,57.10]},
	  "arcs":{"arc-1":{"from":"a","to":"b","pips":[[-3.67,57.11],[-3.66,57.10]]}},
	  "chains":[{"name":"c","links":[
	    {"arc":"ghost","from":"a","to":"b"},
	    {"arc":"arc-1","from":"a","to":"b"}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Networks[0].Chains[0].Links) != 1 {
		t.Fatalf("the link naming a missing arc must go: %+v", doc.Networks[0].Chains[0].Links)
	}
	if len(diags) != 1 || diags[0].Path != "/networks/0/chains/0/links/0" {
		t.Fatalf("got %v", diags)
	}
}

func TestLinkNamingAnAbsentNodeIsDroppedAndDiagnosed(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.67,57.11],"b":[-3.66,57.10]},
	  "arcs":{"arc-1":{"from":"a","to":"b","pips":[[-3.67,57.11],[-3.66,57.10]]}},
	  "chains":[{"name":"c","links":[{"arc":"arc-1","from":"a","to":"ghost"}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %v", diags)
	}
}

// An inline endpoint MUST lie on the named arc. One that is nowhere near it is
// an unusable value: the reader cannot project it, so the link goes and says so.
func TestInlineEndpointFarFromItsArcIsDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.67,57.11],"b":[-3.66,57.10]},
	  "arcs":{"arc-1":{"from":"a","to":"b","pips":[[-3.67,57.11],[-3.66,57.10]]}},
	  "chains":[{"name":"c","links":[{"arc":"arc-1","from":"a","to":[-2.00,55.00]}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Networks[0].Chains[0].Links) != 0 {
		t.Fatal("an endpoint that is not on its arc cannot be projected — the link goes")
	}
	if len(diags) != 1 {
		t.Fatalf("got %v", diags)
	}
}

// An arc whose pips do not start and end at its named nodes is the
// whole-polyline rule being broken — migration 071's silent trap, made loud.
func TestArcPipsMustMeetItsNodes(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.67,57.11],"b":[-3.66,57.10]},
	  "arcs":{"arc-1":{"from":"a","to":"b","pips":[[-3.60,57.00],[-3.59,56.99]]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("an arc whose pips do not meet its nodes must be reported")
	}
}

func TestArcNamingAnAbsentNodeIsDiagnosed(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{"name":"n",
	  "nodes":{"a":[-3.67,57.11]},
	  "arcs":{"arc-1":{"from":"a","to":"ghost","pips":[[-3.67,57.11],[-3.66,57.10]]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("an arc naming a node that is not there must be reported")
	}
}

// A network arc must keep the nodes it names. decodeNamedArcs used to read
// `from` and `to` purely to check the arc met its endpoints and then discard
// them — which nothing noticed, because the check itself still worked. The loss
// only appears when something tries to write a decoded network back out and
// finds it has no nodes to name.
func TestNamedArcKeepsItsNodes(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","networks":[{
		"nodes": {"a": [-3.68, 57.12], "b": [-3.66, 57.10]},
		"arcs": {"ab": {"from": "a", "to": "b", "pips": [[-3.68,57.12],[-3.66,57.10]]}}
	}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	arc := doc.Networks[0].Arcs["ab"]
	if arc.From != "a" || arc.To != "b" {
		t.Fatalf("arc runs from %q to %q, want a to b", arc.From, arc.To)
	}
}

// A ROUTE's arcs deliberately have none. Asserting the absence stops someone
// "fixing" it later by inventing ids a route does not have and cannot need.
func TestRouteArcsNameNoNodes(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.1.0","routes":[{"arcs":[
		{"pips": [[-3.68,57.12],[-3.66,57.10]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if arc := doc.Routes[0].Arcs[0]; arc.From != "" || arc.To != "" {
		t.Fatalf("a route's arc named nodes %q/%q; it owns its geometry and names nothing", arc.From, arc.To)
	}
}

// TestADroppedLinkDoesNotMakeItsNeighboursAdjacent pins spec §3.3.3's
// one-defect-must-not-become-two rule directly, rather than only through
// diagnose/link-names-absent-arc.vnx.
//
// ⚠ THE CORPUS FIXTURE COULD NOT TEST THIS FOR MOST OF ITS LIFE. It had two
// links, so dropping one left no consecutive pair and no reader could report a
// break — the expectation of exactly one diagnostic was guaranteed by arity
// rather than by care, and its note claimed a property the file could not
// check. A three-link version whose survivors happen to MEET is blind too. This
// test states the property directly so it does not depend on a fixture's shape
// staying right.
func TestADroppedLinkDoesNotMakeItsNeighboursAdjacent(t *testing.T) {
	const src = `{"vnx":"0.1.0","networks":[{"name":"n",` +
		`"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1],"n3":[-3.2,56.1],"n4":[-3.19,56.09]},` +
		`"arcs":{"near":{"from":"n1","to":"n2","pips":[[-3.68,57.12],[-3.66,57.1]]},` +
		`"far":{"from":"n3","to":"n4","pips":[[-3.2,56.1],[-3.19,56.09]]}},` +
		`"chains":[{"name":"c","links":[` +
		`{"arc":"near","from":"n1","to":"n2"},` +
		`{"arc":"ghost","from":"n2","to":"n3"},` +
		`{"arc":"far","from":"n3","to":"n4"}]}]}]}`
	_, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The survivors are 60 km apart, so a reader comparing across the hole
	// cannot fail to notice. If this test ever sees only one link surviving,
	// it has stopped measuring.
	var codes []string
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	if len(diags) != 1 || diags[0].Code != CodeLinkArcAbsent {
		t.Fatalf("want exactly one %q diagnostic, got %d %v:\n%v", CodeLinkArcAbsent, len(diags), codes, diags)
	}
	// And the path names the link as the DOCUMENT numbers it, not as the
	// surviving slice does. Before 2026-09-09 this said /links/1.
	if want := "/networks/0/chains/0/links/1"; diags[0].Path != want {
		t.Errorf("path = %q, want %q", diags[0].Path, want)
	}
}

// TestContinuityIsReportedAtTheDocumentsIndex pins the second half: when a
// break IS real and something earlier was dropped, the path still names the
// link the document numbers, not the one the surviving slice does.
func TestContinuityIsReportedAtTheDocumentsIndex(t *testing.T) {
	const src = `{"vnx":"0.1.0","networks":[{"name":"n",` +
		`"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1],"n3":[-3.2,56.1],"n4":[-3.19,56.09]},` +
		`"arcs":{"near":{"from":"n1","to":"n2","pips":[[-3.68,57.12],[-3.66,57.1]]},` +
		`"far":{"from":"n3","to":"n4","pips":[[-3.2,56.1],[-3.19,56.09]]}},` +
		`"chains":[{"name":"c","links":[` +
		`{"arc":"ghost","from":"n1","to":"n2"},` +
		`{"arc":"near","from":"n1","to":"n2"},` +
		`{"arc":"far","from":"n3","to":"n4"}]}]}]}`
	_, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var broke *Diagnostic
	for i := range diags {
		if diags[i].Code == CodeChainDiscontinuity {
			broke = &diags[i]
		}
	}
	if broke == nil {
		t.Fatalf("links 1 and 2 are consecutive in the document and do not meet; want a %q diagnostic, got %v",
			CodeChainDiscontinuity, diags)
	}
	if want := "/networks/0/chains/0/links/2"; broke.Path != want {
		t.Errorf("path = %q, want %q — the surviving index would have said /links/1", broke.Path, want)
	}
}
