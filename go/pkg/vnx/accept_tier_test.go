package vnx

import "testing"

// TestAcceptTierCatchesAReaderThatDiscardsWhatItRead freezes the proof that the
// accept tier's check is worth what §8.1 claims for it.
//
// ⚠ THIS IS WHY THE TIER COMPARES AGAINST THE ORIGINAL FILE AND NOT AGAINST
// ANOTHER MODEL. A reader that discarded every data record, every ruleset,
// every style or every `notes` produces a model that compares equal to itself
// and passes a model-versus-model tier GREEN. That is the entire extensibility
// mechanism, all styling and all user prose, silently droppable by a
// "conforming" implementation. Measured, by disabling one thing at a time in a
// decoder.
//
// Each case below is one of those readers. If any of them stops being caught,
// the tier has gone back to proving nothing about the model.
func TestAcceptTierCatchesAReaderThatDiscardsWhatItRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(*Document)
	}{
		{"drops every data record", func(d *Document) {
			d.Waypoints[0].Data = nil
			for i := range d.Networks {
				d.Networks[i].Data = nil
				for k, a := range d.Networks[i].Arcs {
					a.Data = nil
					d.Networks[i].Arcs[k] = a
				}
				for j := range d.Networks[i].Chains {
					d.Networks[i].Chains[j].Data = nil
				}
				for j := range d.Networks[i].Points {
					d.Networks[i].Points[j].Data = nil
				}
			}
		}},
		{"drops every ruleset", func(d *Document) { d.Rulesets = nil }},
		{"drops every style", func(d *Document) {
			for i := range d.Networks {
				for j := range d.Networks[i].Chains {
					d.Networks[i].Chains[j].Style = Style{}
				}
			}
		}},
		{"drops every notes", func(d *Document) {
			for i := range d.Networks {
				d.Networks[i].Notes = ""
				for j := range d.Networks[i].Chains {
					d.Networks[i].Chains[j].Notes = ""
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cases := loadCorpus(t)
			var data []byte
			for _, c := range cases {
				if c.name == "accept/example-network.vnx" {
					data = c.data
				}
			}
			if data == nil {
				t.Fatal("example-network not found")
			}
			doc, _, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			tc.spoil(doc)
			got, err := Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			diff, err := compareDocuments(got, data, treeOptions{emitBoundary: true})
			if err != nil {
				t.Fatal(err)
			}
			if diff == "" {
				t.Errorf("a reader that %s reproduced the file — the check cannot see it", tc.name)
			} else {
				t.Logf("caught: %s — %s", tc.name, diff)
			}
		})
	}
}
