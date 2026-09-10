package vnx

import (
	"encoding/json"
	"reflect"
	"testing"
)

// §3.7: rulesets are DEFERRED, not designed. The subsystem is being redesigned
// around selection over data elements, so a 0.1 reader treats the block as
// opaque and preserves it. The accept tier round-trips a corpus file holding
// one, so without this that round trip rests on behaviour nothing tested.
func TestRulesetsArePreservedOpaquely(t *testing.T) {
	const src = `{"vnx":"0.1.0","rulesets":[{"name":"Hide minor","enabled":true,
	  "rules":[{"name":"r","priority":10,"applies-to":["waypoints"],
	    "conditions":[{"data-type":"app.viatrail.tag","field":"value","op":"equals","value":"minor"}],
	    "style":{"label-visible":false}}]}]}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("an opaque block must not be picked over: %v", diags)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(src), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Rulesets, want["rulesets"].([]any)) {
		t.Fatalf("rulesets not preserved verbatim:\n got %#v\nwant %#v", doc.Rulesets, want["rulesets"])
	}
}

// The vocabulary inside is fiction — `op: equals` does not exist in the real
// evaluator, which has contains / starts_with / matches_regex. A 0.1 reader
// must not validate it, precisely because it is about to change.
func TestRulesetContentsAreNotValidated(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.1.0","rulesets":[{"anything":{"at":["all"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("opaque means opaque, got %v", diags)
	}
}

// A ruleset survives a read-modify-write untouched. The block is opaque, so
// the encoder hands it to encoding/json exactly as it was read rather than
// walking into it.
func TestRulesetsSurviveAReadModifyWrite(t *testing.T) {
	const src = `{"vnx":"0.1.0","rulesets":[{"zebra":1,"apple":[2,{"nested":true}]}]}`
	doc, _, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := compareDocuments(got, []byte(src), treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("the ruleset did not survive: %s", diff)
	}
}
