package vnx

import "testing"

func TestWaypointCarriesEveryStyleField(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","waypoints":[{
	  "folder":"cairngorms","name":"Car park","notes":"Pay and display.",
	  "at":[-3.6750,57.1201,320],
	  "style":{"color":"#444444","icon":"parking","icon-size":16,
	           "pin-shape":"teardrop","label-position":"right",
	           "label-visible":true,"importance":2}
	}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean waypoint produced %v", diags)
	}
	w := doc.Waypoints[0]
	if w.Name != "Car park" || w.Folder != "cairngorms" || w.At.Lon != -3.6750 {
		t.Fatalf("got %+v", w)
	}
	// A style is OPAQUE (spec §3.10), so this reads it by key. Every value is
	// carried through as it arrived: a string stays a string, a JSON number
	// stays a float64, and nothing is coerced into a field of a fixed shape.
	s := w.Style
	for key, want := range map[string]any{
		"color": "#444444", "icon": "parking", "pin-shape": "teardrop",
		"label-position": "right", "icon-size": 16.0, "importance": 2.0,
		"label-visible": true,
	} {
		if got := s[key]; got != want {
			t.Errorf("style[%q] = %#v, want %#v", key, got, want)
		}
	}
}

// label-visible is a TRISTATE internally (*int: nil=Auto, 1=show, 0=hide) and
// absence is meaningful on the wire too. A reader that defaults absent to false
// silently flattens Auto to hidden — a silent default, and the house-rule
// violation the audit caught.
func TestLabelVisibleAbsentMeansAutoNotFalse(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.2.0","waypoints":[{"name":"n","at":[-3.6,57.1],"style":{"color":"#fff"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if lv, present := doc.Waypoints[0].Style["label-visible"]; present {
		t.Fatalf("absent label-visible must stay ABSENT, which is Auto; got %v", lv)
	}
}

// §3.8: omitted means visible, and a writer emits the key only when true.
func TestHiddenAbsentMeansVisible(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.2.0","waypoints":[
	  {"name":"plain","at":[-3.6,57.1]},
	  {"name":"tucked away","at":[-3.6,57.1],"hidden":true}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Waypoints[0].Hidden {
		t.Fatal("absent hidden must mean visible")
	}
	if !doc.Waypoints[1].Hidden {
		t.Fatal("hidden:true must be carried")
	}
}

func TestWaypointWithUnusablePositionIsDroppedAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","waypoints":[
	  {"name":"bad","at":[-3.6]},{"name":"good","at":[-3.6,57.1]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Waypoints) != 1 || doc.Waypoints[0].Name != "good" {
		t.Fatalf("got %+v", doc.Waypoints)
	}
	if len(diags) != 1 || diags[0].Path != "/waypoints/0/at" {
		t.Fatalf("got %v", diags)
	}
}

// §3.7: ONE open ring of at least three distinct positions. The model has no
// holes and no multi-ring, so the format must not promise them.
func TestAreaRing(t *testing.T) {
	ring := `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.67,57.10]]`
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","areas":[{"name":"Bog","polygon":` + ring + `}]}`))
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
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","areas":[{"name":"Bog","polygon":` + ring + `}]}`))
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
			doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","areas":[{"name":"x","polygon":` + c.polygon + `}]}`))
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
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","areas":[{"name":"x","polygon":` + nested + `}]}`))
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
