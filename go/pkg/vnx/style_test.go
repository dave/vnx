package vnx

import (
	"bytes"
	"testing"
)

// TestAStyleIsOpaque pins the two bugs the typed style structs had, and the
// second is the larger one.
//
// ⚠ THE EMPTY CASE IS WHAT SURFACED THIS AND THE DISCARDING IS THE REAL DEFECT.
// A struct with named fields lost presence — `"style": {}` and no `style` key
// were one value — and silently dropped every style key the format has not
// named, in a region spec §3.10 declares opaque. Nobody reported the second,
// because a dropped key looks exactly like a key that was never there.
func TestAStyleIsOpaque(t *testing.T) {
	const src = `{"vnx":"0.8.0","waypoints":[` +
		`{"name":"absent","at":[-3,57]},` +
		`{"name":"empty","at":[-3,57],"style":{}},` +
		`{"name":"unknown keys","at":[-3,57],"style":{"color":"#abcdef","halo-width":3,"glyph":"cairn"}}]}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("a style is opaque, so an unrecognised key inside one is not a diagnostic: %v", diags)
	}
	if doc.Waypoints[0].Style != nil {
		t.Error("an absent style decoded to a non-nil value")
	}
	if doc.Waypoints[1].Style == nil {
		t.Error("an empty style decoded to nil, so it cannot be told from an absent one")
	}
	if got := doc.Waypoints[2].Style["glyph"]; got != "cairn" {
		t.Errorf("an unrecognised style key was not carried: got %#v", got)
	}

	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	// §3.1 makes an empty container and an absent one the same content, so the
	// writer spells both as absence. What must survive is the style that has
	// keys in it.
	if n := bytes.Count(got, []byte(`"style"`)); n != 1 {
		t.Errorf("got %d style objects written, want exactly 1 — the one with keys in it:\n%s", n, got)
	}
	for _, key := range []string{"halo-width", "glyph"} {
		if !bytes.Contains(got, []byte(key)) {
			t.Errorf("%q did not survive a read-modify-write:\n%s", key, got)
		}
	}
}

// TestImportanceIsCheckedInAStyleAndNotInARecord pins §3.10's one exception to
// opacity, and its boundary.
//
// Opaque means unrecognised keys are not reported. It does not exempt a value
// from a range this format states normatively, and §3.6 states one for
// `importance`. But §6.2 forbids inspecting a data record body, and that
// prohibition is the mechanism by which an unrecognised record survives a
// read-modify-write — so a field named `importance` inside a record is carried
// untouched however wrong it looks.
func TestImportanceIsCheckedInAStyleAndNotInARecord(t *testing.T) {
	const src = `{"vnx":"0.8.0","waypoints":[` +
		`{"name":"bad style importance","at":[-3,57],"style":{"importance":9}},` +
		`{"name":"bad record importance","at":[-3,57],"data":[{"@type":"x.y","importance":9}]}]}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != CodeWrongTypeOptional {
		t.Fatalf("want exactly one %q diagnostic, got %v", CodeWrongTypeOptional, diags)
	}
	if _, present := doc.Waypoints[0].Style["importance"]; present {
		t.Error("an out-of-range style importance was kept")
	}
	if got := doc.Waypoints[1].Data[0].Body["importance"]; got != 9.0 {
		t.Errorf("a record body's importance was inspected; got %#v, want it carried untouched", got)
	}
	// `icon-size` has no stated range, so it is not checked at all.
	doc2, diags2, err := Decode([]byte(`{"vnx":"0.8.0","waypoints":[{"name":"n","at":[-3,57],"style":{"icon-size":"large"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags2) != 0 {
		t.Errorf("no range is stated for icon-size, so it is not checked: %v", diags2)
	}
	if got := doc2.Waypoints[0].Style["icon-size"]; got != "large" {
		t.Errorf("icon-size was not carried: %#v", got)
	}
}
