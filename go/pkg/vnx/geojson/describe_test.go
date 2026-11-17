package geojson

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// The caller supplies descriptions (SPEC §3.10 has no description key).
// GeoJSON asks about every Feature it writes, with the item's own name and its
// path, and writes the text as properties.description. A folder is not a
// Feature, so it is never asked about.
func TestDescribeGivesEachWrittenFeatureItsDescription(t *testing.T) {
	var asked []string
	var buf bytes.Buffer
	if _, err := WriteWith(&buf, describedDoc(), noteDescriber(&asked)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if problems := validate(buf.Bytes()); len(problems) > 0 {
		t.Fatalf("output is not valid RFC 7946:\n  %s\n%s", strings.Join(problems, "\n  "), out)
	}
	sameSet(t, asked, []string{
		"point /points/0 Hut", "area /areas/0 Bog", "track /tracks/0 Walk", "plan /plans/0 Ridge",
		"network /networks/0 Paths", "point /networks/0/points/0 Cairn", "area /networks/0/areas/0 Moss",
		"highlight /networks/0/highlights/0 Traverse",
	})
	for _, want := range []string{
		`"properties":{"name":"Hills / Hut","description":"hut note"}`,
		`"properties":{"name":"Bog","description":"bog note"}`,
		`"properties":{"name":"Walk","description":"walk note"}`,
		`"properties":{"name":"Ridge","description":"ridge note"}`,
		`"properties":{"name":"Paths","description":"paths note"}`,
		`"properties":{"name":"Paths / Cairn","description":"cairn note"}`,
		`"properties":{"name":"Paths / Moss","description":"moss note"}`,
		`"properties":{"name":"Paths / Traverse","description":"traverse note"}`,
	} {
		if strings.Count(out, want) != 1 {
			t.Errorf("want %s once\n%s", want, out)
		}
	}
	plain, _ := writeDoc(t, describedDoc())
	if strings.Contains(plain, "description") {
		t.Errorf("a description was written with no Describe\n%s", plain)
	}
}

// describedDoc carries one item of every kind, each with a record holding the
// text a caller would turn into its description.
func describedDoc() *vnx.Document {
	note := func(text string) []vnx.Record {
		return []vnx.Record{{Type: "org.example.note", Body: map[string]any{"text": text}}}
	}
	ring := []vnx.Position{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}}
	nodes := func() map[string]vnx.Vertex {
		return map[string]vnx.Vertex{"a": {Lon: 0, Lat: 0}, "b": {Lon: 1, Lat: 1}}
	}
	arcs := func() map[string]vnx.Arc { return map[string]vnx.Arc{"x": {From: "a", To: "b"}} }
	walk := []vnx.Pass{{Arc: "x", From: &vnx.Endpoint{Node: "a"}, To: &vnx.Endpoint{Node: "b"}}}
	return &vnx.Document{
		Version: vnx.Version,
		Folders: []vnx.Folder{{ID: "f", Name: "Hills"}},
		Points:  []vnx.Point{{Folder: "f", Name: "Hut", At: vnx.Position{Lon: 1, Lat: 2}, Data: note("hut note")}},
		Areas:   []vnx.Area{{Name: "Bog", Polygon: ring, Data: note("bog note")}},
		Tracks: []vnx.Track{{Name: "Walk", Data: note("walk note"),
			Segments: []vnx.Segment{{Pips: []vnx.Position{{Lon: 1, Lat: 3}, {Lon: 2, Lat: 3}}}}}},
		Plans: []vnx.Plan{{Name: "Ridge", Nodes: nodes(), Arcs: arcs(), Walk: walk, Data: note("ridge note")}},
		Networks: []vnx.Network{{
			Name: "Paths", Nodes: nodes(), Arcs: arcs(), Data: note("paths note"),
			Points:     []vnx.Point{{Name: "Cairn", At: vnx.Position{Lon: 0.5, Lat: 0.5}, Data: note("cairn note")}},
			Areas:      []vnx.Area{{Name: "Moss", Polygon: ring, Data: note("moss note")}},
			Highlights: []vnx.Highlight{{Name: "Traverse", Walk: walk, Data: note("traverse note")}},
		}},
	}
}

// noteDescriber describes an item by its note record, and a folder, which has
// no records, by its name. It records every item it was asked about.
func noteDescriber(asked *[]string) vnx.WriteOptions {
	return vnx.WriteOptions{Describe: func(it vnx.DescribedItem) (string, error) {
		*asked = append(*asked, string(it.Kind)+" "+it.Path+" "+it.Name)
		if it.Kind == vnx.ItemFolder {
			return "folder " + it.Name, nil
		}
		if len(it.Data) == 0 {
			return "", nil
		}
		text, _ := it.Data[0].Body["text"].(string)
		return text, nil
	}}
}

func sameSet(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, "\n") != strings.Join(w, "\n") {
		t.Errorf("Describe was asked about\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// A Describe error stops the write, and WriteWith returns it naming the item.
func TestDescribeErrorFailsTheWrite(t *testing.T) {
	var buf bytes.Buffer
	_, err := WriteWith(&buf, describedDoc(), vnx.WriteOptions{Describe: func(vnx.DescribedItem) (string, error) {
		return "", errors.New("no such note")
	}})
	if err == nil || !strings.Contains(err.Error(), "no such note") || !strings.Contains(err.Error(), "describe /") {
		t.Fatalf("err = %v, want the Describe error naming the item's path", err)
	}
}
