package vnx

import (
	"bytes"
	"testing"
)

// TestAcceptTierCatchesAReaderThatDiscardsWhatItRead freezes the proof that the
// accept tier's check is worth what §8.1 claims for it.
//
// ⚠ THIS IS WHY THE TIER USES Reformat AND NOT Canonical. Canonical is bytes in
// and bytes out; it never builds a model, so it cannot observe anything about
// what a model kept. The Python implementation measured that on 2026-09-09 by
// disabling one thing at a time in its decoder: a reader that discarded every
// data record, every ruleset, every style or every `notes` passed the whole
// accept tier GREEN. That is the entire extensibility mechanism, all styling
// and all user prose, silently droppable by a "conforming" implementation.
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
			got, err := Reformat(doc)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(got, data) {
				t.Errorf("a reader that %s reproduced the file byte for byte — the check cannot see it", tc.name)
			} else {
				t.Logf("caught: %s", tc.name)
			}
		})
	}
}
