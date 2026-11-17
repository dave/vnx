package vnx

import "testing"

// The format defines no style (§3.6), so `style` is an unrecognised key like
// any other: reported, skipped, and the point kept with everything else.
func TestAStyleKeyIsAnUnrecognisedKey(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[{"id":"cairngorms"}],"points":[{
	  "folder":"cairngorms","name":"Car park","notes":"Pay and display.",
	  "at":[-3.6750,57.1201,320],
	  "style":{"color":"#444444","importance":2}
	}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != CodeUnknownKey || diags[0].Path != "/points/0/style" {
		t.Fatalf("diagnostics = %v, want one unknown-key at /points/0/style", diags)
	}
	w := doc.Points[0]
	if w.Name != "Car park" || w.Folder != "cairngorms" || w.Notes != "Pay and display." || w.At.Lon != -3.6750 {
		t.Fatalf("got %+v", w)
	}
}

// Issue #942: a wrong-typed optional string is `wrong-type-optional` at its
// FULL path. It was reported at "/name", with no item prefix and no code, so a
// caller could not tell which item lost the field.
func TestAWrongTypedOptionalStringIsReportedAtItsFullPath(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.11.0",
	  "folders":[{"id":"f","name":1,"notes":true,"parent":[]}],
	  "points":[{"name":2,"notes":{},"folder":3,"at":[1,2]}],
	  "networks":[{"name":4,"nodes":{},"arcs":{},"highlights":[{"name":5,"walk":[]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/folders/0/name": true, "/folders/0/notes": true, "/folders/0/parent": true,
		"/points/0/name": true, "/points/0/notes": true, "/points/0/folder": true,
		"/networks/0/name": true,
	}
	for _, d := range diags {
		if !want[d.Path] {
			continue
		}
		if d.Code != CodeWrongTypeOptional {
			t.Errorf("%s: code %q, want %q", d.Path, d.Code, CodeWrongTypeOptional)
		}
		delete(want, d.Path)
	}
	for path := range want {
		t.Errorf("no diagnostic at %s; got %v", path, diags)
	}
}

// A network's points and areas carry no `folder` of their own (§3.10): they
// sit in their network's folder. On them `folder` is an unrecognised key.
func TestANetworkPointOrAreaFolderIsAnUnrecognisedKey(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[{"id":"f"}],"networks":[{"nodes":{},"arcs":{},
	  "points":[{"at":[1,2],"folder":"f"}],
	  "areas":[{"polygon":[[0,0],[1,0],[1,1]],"folder":"f"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/networks/0/points/0/folder": true, "/networks/0/areas/0/folder": true}
	for _, d := range diags {
		if want[d.Path] && d.Code == CodeUnknownKey {
			delete(want, d.Path)
		}
	}
	for path := range want {
		t.Errorf("no unknown-key at %s; got %v", path, diags)
	}
	n := doc.Networks[0]
	if len(n.Points) != 1 || n.Points[0].Folder != "" || len(n.Areas) != 1 || n.Areas[0].Folder != "" {
		t.Fatalf("network items = %+v %+v, want both kept with no folder", n.Points, n.Areas)
	}
}

// §3.8: omitted means visible, and a writer emits the key only when true.
func TestHiddenAbsentMeansVisible(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.11.0","points":[
	  {"name":"plain","at":[-3.6,57.1]},
	  {"name":"tucked away","at":[-3.6,57.1],"hidden":true}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Points[0].Hidden {
		t.Fatal("absent hidden must mean visible")
	}
	if !doc.Points[1].Hidden {
		t.Fatal("hidden:true must be carried")
	}
}

func TestPointWithUnusablePositionIsDroppedAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","points":[
	  {"name":"bad","at":[-3.6]},{"name":"good","at":[-3.6,57.1]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Points) != 1 || doc.Points[0].Name != "good" {
		t.Fatalf("got %+v", doc.Points)
	}
	if len(diags) != 1 || diags[0].Path != "/points/0/at" {
		t.Fatalf("got %v", diags)
	}
}

// §3.7: ONE open ring of at least three distinct positions. The model has no
// holes and no multi-ring, so the format must not promise them.
func TestAreaRing(t *testing.T) {
	ring := `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.67,57.10]]`
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","areas":[{"name":"Bog","polygon":` + ring + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("a valid ring produced %v", diags)
	}
	if len(doc.Areas) != 1 || len(doc.Areas[0].Polygon) != 4 {
		t.Fatalf("got %+v", doc.Areas)
	}
}

// A closed ring is repaired, not dropped: its repeated last position is one
// fact written twice, so the reader drops the repeat, keeps the area, and says
// so at the position it dropped.
func TestAClosedRingLosesItsRepeat(t *testing.T) {
	ring := `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.67,57.11]]`
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","areas":[{"name":"Bog","polygon":` + ring + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != CodeRingClosed || diags[0].Path != "/areas/0/polygon/3" {
		t.Fatalf("got %v, want one %s at the repeated position", diags, CodeRingClosed)
	}
	if len(doc.Areas) != 1 || len(doc.Areas[0].Polygon) != 3 {
		t.Fatalf("got %+v", doc.Areas)
	}
}

func TestAreaRingRefusals(t *testing.T) {
	for _, c := range []struct{ name, polygon string }{
		{"two positions", `[[-3.67,57.11],[-3.66,57.11]]`},
		{"three positions, two distinct", `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.11]]`},
		{"closed on two distinct", `[[-3.67,57.11],[-3.66,57.11],[-3.67,57.11]]`},
		{"empty", `[]`},
		{"missing", `null`},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","areas":[{"name":"x","polygon":` + c.polygon + `}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Areas) != 0 {
				t.Fatalf("an unusable ring drops the area, got %+v", doc.Areas)
			}
			if len(diags) == 0 {
				t.Fatal("and says so")
			}
		})
	}
}

// The example once carried GeoJSON-style nested rings, promising holes the
// model cannot hold. A reader meeting them must report and drop, never silently
// ignore and never silently merge.
func TestNestedRingsAreDiagnosed(t *testing.T) {
	nested := `[[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.67,57.11]]]`
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","areas":[{"name":"x","polygon":` + nested + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Areas) != 0 {
		t.Fatalf("nested rings are not this model's shape: %+v", doc.Areas)
	}
	if len(diags) == 0 {
		t.Fatal("nested rings must be diagnosed, not silently flattened")
	}
}

// The top-level collection is `points` (§3.6). The key `waypoints` has no
// meaning at the top level, so it is an unknown key like any other: reported
// and ignored, and nothing in it is read.
func TestTopLevelWaypointsIsAnUnknownKey(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","waypoints":[{"name":"n","at":[-3.6,57.1]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Points) != 0 {
		t.Fatalf("read %d points from `waypoints`, want 0", len(doc.Points))
	}
	if len(diags) != 1 || diags[0].Path != "/waypoints" || diags[0].Code != CodeUnknownKey {
		t.Fatalf("got %v, want one unknown-key diagnostic at /waypoints", diags)
	}
}

// §6.3 rule 1: a REQUIRED key that is absent names the object that lacks it;
// one present with the wrong type names the key. Both drop the object
// (wrong-type-required). The corpus pins a point and a track; these are the
// other required keys.
func TestARequiredKeyIsNamedAtTheObjectWhenAbsentAndAtTheKeyWhenWrongTyped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0",
	  "areas":[{"name":"no polygon"},{"polygon":7}],
	  "tracks":[{"segments":{}},{"segments":[{},{"pips":"x"},{"pips":[[1,2],[3,4]]}]}],
	  "points":[{"at":[1,2],"data":[{"v":1},{"@type":3},{"@type":"ok.type"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/areas/0": true, "/areas/1/polygon": true,
		"/tracks/0/segments": true, "/tracks/1/segments/0": true, "/tracks/1/segments/1/pips": true,
		"/points/0/data/0": true, "/points/0/data/1/@type": true,
	}
	for _, d := range diags {
		if d.Code != CodeWrongTypeRequired {
			t.Errorf("unexpected %s %q", d.Path, d.Code)
			continue
		}
		if !want[d.Path] {
			t.Errorf("wrong-type-required at %s, which is not expected", d.Path)
		}
		delete(want, d.Path)
	}
	for path := range want {
		t.Errorf("no wrong-type-required at %s; got %v", path, diags)
	}
	if len(doc.Areas) != 0 || len(doc.Tracks) != 1 || len(doc.Tracks[0].Segments) != 1 || len(doc.Points[0].Data) != 1 {
		t.Fatalf("got %d areas, %+v tracks, %d records", len(doc.Areas), doc.Tracks, len(doc.Points[0].Data))
	}
}
