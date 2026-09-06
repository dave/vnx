package vnx

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decodeData(t *testing.T, src string) ([]Record, []Diagnostic) {
	t.Helper()
	var raw any
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		t.Fatal(err)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("test fixture is not an array")
	}
	return decodeRecords(arr, "/data", nil)
}

// The extension mechanism's whole value is that a record we do not understand
// survives untouched. If it does not, third parties cannot use the format.
func TestUnknownRecordSurvivesRoundTrip(t *testing.T) {
	const src = `[
	  {"@type":"com.example.whatever","nested":{"deep":[1,2,{"x":null}]},"n":1.5,"s":"hi","b":true,"z":null}
	]`
	recs, diags := decodeData(t, src)
	if len(diags) != 0 {
		t.Fatalf("an unknown type must be inert, got %v", diags)
	}
	if len(recs) != 1 || recs[0].Type != "com.example.whatever" {
		t.Fatalf("got %+v", recs)
	}

	var want any
	_ = json.Unmarshal([]byte(src), &want)
	got := encodeRecords(recs)
	if !reflect.DeepEqual(got, want.([]any)) {
		t.Fatalf("record not preserved at value level:\n got %#v\nwant %#v", got, want)
	}
}

// Dave's ruling, 2026-08-31: duplicates of one type are fully supported and
// what a reader does with them is the reader's business. The format does not
// legislate it, so the codec must not quietly collapse them either.
func TestDuplicateTypesAllKept(t *testing.T) {
	recs, diags := decodeData(t, `[
	  {"@type":"app.viatrail.tag","value":"scotland"},
	  {"@type":"app.viatrail.tag","value":"highlands"},
	  {"@type":"app.viatrail.tag","value":"winter"}
	]`)
	if len(diags) != 0 {
		t.Fatalf("duplicates are legal, got diagnostics %v", diags)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3 — duplicates must not be collapsed", len(recs))
	}
	for i, want := range []string{"scotland", "highlands", "winter"} {
		if got := recs[i].Body["value"]; got != want {
			t.Errorf("record %d value = %v, want %q — order must be preserved too", i, got, want)
		}
	}
}

// @type carries a sigil precisely so a vocabulary entry may have a field called
// `type`. That freedom is the reason the sigil was restored, so it gets a test.
func TestRecordMayCarryAFieldNamedType(t *testing.T) {
	recs, _ := decodeData(t, `[{"@type":"com.example.thing","type":"the payload one"}]`)
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if recs[0].Type != "com.example.thing" {
		t.Fatalf("discriminator = %q", recs[0].Type)
	}
	if recs[0].Body["type"] != "the payload one" {
		t.Fatalf("payload `type` lost: %#v", recs[0].Body)
	}
}

// A record with no @type cannot be dispatched on. Per §6.2 that is an unusable
// element: dropped, diagnosed, and the read continues.
func TestRecordWithoutTypeIsDroppedAndDiagnosed(t *testing.T) {
	recs, diags := decodeData(t, `[{"value":"orphan"},{"@type":"app.viatrail.tag","value":"kept"}]`)
	if len(recs) != 1 || recs[0].Body["value"] != "kept" {
		t.Fatalf("the good record must survive, got %+v", recs)
	}
	if len(diags) != 1 {
		t.Fatalf("want exactly one diagnostic, got %v", diags)
	}
	if diags[0].Path != "/data/0" {
		t.Fatalf("diagnostic path = %q, want /data/0 — a path is what makes it actionable", diags[0].Path)
	}
	if diags[0].Severity != Warning {
		t.Fatalf("dropping an element is a Warning, got %v", diags[0].Severity)
	}
}

func TestNonObjectRecordDropped(t *testing.T) {
	recs, diags := decodeData(t, `["not an object",{"@type":"app.viatrail.tag","value":"kept"}]`)
	if len(recs) != 1 {
		t.Fatalf("got %+v", recs)
	}
	if len(diags) != 1 || diags[0].Path != "/data/0" {
		t.Fatalf("got %v", diags)
	}
}
