package vnx

import "testing"

func TestWaypointCarriesEveryStyleField(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","waypoints":[{
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
	s := w.Style
	if s.Color != "#444444" || s.Icon != "parking" || s.PinShape != "teardrop" || s.LabelPosition != "right" {
		t.Fatalf("string style fields wrong: %+v", s)
	}
	// icon-size is POINTS and importance is 1..5 — both integers in the model,
	// not the words an earlier draft of the example used.
	if s.IconSize == nil || *s.IconSize != 16 {
		t.Fatalf("icon-size = %v, want 16", s.IconSize)
	}
	if s.Importance == nil || *s.Importance != 2 {
		t.Fatalf("importance = %v, want 2", s.Importance)
	}
	if s.LabelVisible == nil || *s.LabelVisible != true {
		t.Fatalf("label-visible = %v, want true", s.LabelVisible)
	}
}

// label-visible is a TRISTATE internally (*int: nil=Auto, 1=show, 0=hide) and
// absence is meaningful on the wire too. A reader that defaults absent to false
// silently flattens Auto to hidden — a silent default, and the house-rule
// violation the audit caught.
func TestLabelVisibleAbsentMeansAutoNotFalse(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.1.0","waypoints":[{"name":"n","at":[-3.6,57.1],"style":{"color":"#fff"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if lv := doc.Waypoints[0].Style.LabelVisible; lv != nil {
		t.Fatalf("absent label-visible must stay nil (Auto), got %v", *lv)
	}
}

// §3.9: omitted means visible, and a writer emits the key only when true.
func TestHiddenAbsentMeansVisible(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.1.0","waypoints":[
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
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","waypoints":[
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

// §3.6: ONE closed ring, at least four positions, first == last. The model has
// no holes and no multi-ring, so the format must not promise them.
func TestAreaRing(t *testing.T) {
	ring := `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.67,57.11]]`
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","areas":[{"name":"Bog","polygon":` + ring + `}]}`))
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

func TestAreaRingRefusals(t *testing.T) {
	for _, c := range []struct{ name, polygon string }{
		{"too few positions", `[[-3.67,57.11],[-3.66,57.11],[-3.67,57.11]]`},
		{"not closed", `[[-3.67,57.11],[-3.66,57.11],[-3.66,57.10],[-3.65,57.09]]`},
		{"empty", `[]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","areas":[{"name":"x","polygon":` + c.polygon + `}]}`))
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
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","areas":[{"name":"x","polygon":` + nested + `}]}`))
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
