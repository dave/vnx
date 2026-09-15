package vnx

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"
)

const examplePath = "../../../corpus/accept/example-network.vnx"

// The round trip that matters: read the corpus example, write it back, and
// compare the two as JSON TREES.
//
// This is the strongest test in the package, and it is strong for a specific
// reason — it fails if the encoder forgets a single field the decoder read.
// Every weaker version of it (compare a few fields, compare struct equality on
// what was decoded) passes happily while silently dropping whatever neither
// side happens to touch, which is the exact defect that let network arcs lose
// their `from` and `to` for four commits.
func TestExampleRoundTripsAsATree(t *testing.T) {
	want, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	doc, diags, err := Decode(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("the example must read cleanly: %v", diags)
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	diff, err := compareDocuments(got, want, treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("the round trip changed the document: %s", diff)
	}
}

func TestEncodeRefusesNonFinite(t *testing.T) {
	nan := math.NaN()
	doc := &Document{Waypoints: []Waypoint{{Name: "x", At: Position{Lon: 1, Lat: 2, Ele: &nan}}}}
	if _, err := Encode(doc); err == nil {
		t.Fatal("a non-finite elevation must be refused at write time, not written as a NaN literal no reader accepts")
	}
}

// A network arc without its nodes is not writable — the graph is the point.
// Refusing here beats emitting an arc a reader then drops with a diagnostic.
func TestEncodeRefusesANetworkArcWithNoNodes(t *testing.T) {
	doc := &Document{Networks: []Network{{
		Nodes: map[string]Position{"a": {Lon: 1, Lat: 2}},
		Arcs:  map[string]Arc{"x": {Pips: []Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}}},
	}}}
	if _, err := Encode(doc); err == nil {
		t.Fatal("a network arc naming no nodes must be refused")
	}
}

// Trailing absence is omitted; interior absence keeps its null. [lon, lat,
// null, null] and [lon, lat] say the same thing and §4.1 has a writer emit the
// short spelling — but [lon, lat, null, t] cannot lose its null without moving
// the time into the elevation channel.
func TestPositionTrailingNullsAreTrimmedButInteriorOnesKept(t *testing.T) {
	ms := int64(1778569391000)
	for _, c := range []struct {
		name string
		p    Position
		want int
	}{
		{"lon/lat only", Position{Lon: 1, Lat: 2}, 2},
		{"absent ele, absent time", Position{Lon: 1, Lat: 2}, 2},
		{"ele only", Position{Lon: 1, Lat: 2, Ele: ptrF(340)}, 3},
		{"absent ele WITH time", Position{Lon: 1, Lat: 2, Time: &ms}, 4},
		{"ele and time", Position{Lon: 1, Lat: 2, Ele: ptrF(340), Time: &ms}, 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := positionWire(c.p)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.want {
				t.Fatalf("arity %d, want %d: %v", len(got), c.want, got)
			}
			if c.want == 4 && c.p.Ele == nil && got[2] != nil {
				t.Fatalf("interior absence lost its null: %v", got)
			}
		})
	}
}

func ptrF(f float64) *float64 { return &f }

// firstDiff is the index of the first byte two slices disagree on, or the
// length of the shorter where one is a prefix of the other.
func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func excerpt(b []byte, i int) string {
	lo := max(0, i-40)
	hi := min(len(b), i+40)
	return string(b[lo:hi])
}

// Two spellings of the same fact, and the writer picks one.
//
// [lon, lat, null] and [lon, lat] both say "no elevation". A READER accepts
// either — corpus/accept/absent-elevation-and-time.vnx is stored in the long
// spelling on purpose to keep that tolerance under test — and §4.1 has a WRITER
// emit the short one.
func TestTrailingNullDecodesAndReEncodesShort(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","waypoints":[{"name":"x","at":[-3.64,57.09,null]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("the long spelling is legal and must read cleanly: %v", diags)
	}
	if doc.Waypoints[0].At.Ele != nil {
		t.Fatal("null elevation must decode as ABSENT, not as a zero")
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("null")) {
		t.Fatalf("the writer should have emitted the short spelling:\n%s", got)
	}
}

// §4.3 allows optional fractional seconds, and the column behind a track's
// times is UnixMilli — so a seconds-only layout threw the milliseconds away.
// The reader then parsed a different instant back, and because BOTH sides
// truncated, the byte-comparison round trip could not see it.
func TestTimestampsKeepTheirMilliseconds(t *testing.T) {
	for _, c := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"whole second stays short", time.Date(2026, 5, 12, 7, 3, 11, 0, time.UTC), "2026-05-12T07:03:11Z"},
		{"milliseconds survive", time.Date(2026, 5, 12, 7, 3, 11, 456000000, time.UTC), "2026-05-12T07:03:11.456Z"},
		{"trailing zeros trimmed", time.Date(2026, 5, 12, 7, 3, 11, 500000000, time.UTC), "2026-05-12T07:03:11.5Z"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := TimestampOf(c.in); string(got) != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			// And what we write, we must read back to the same instant.
			back, err := Timestamp(c.want).Time()
			if err != nil {
				t.Fatalf("our own output does not parse: %v", err)
			}
			if !back.Equal(c.in) {
				t.Fatalf("round trip changed the instant: %v vs %v", back, c.in)
			}
		})
	}
}

// TestInNetworkItemsCarryNoFolder pins the one thing that stops the encoder
// emitting a document spec §3.10 forbids.
//
// ⚠ waypointTree AND areaTree ARE SHARED between top-level and in-network use,
// so nothing about the model distinguishes the two cases — a network's point is
// the same struct as a waypoint, Folder field and all. Before this, only the
// caller's discipline kept `folder` off an in-network item, and the caller is
// in another package. A network already contains its points and areas, so a
// folder there would place an item that is not placed.
func TestInNetworkItemsCarryNoFolder(t *testing.T) {
	doc := &Document{
		Version: "0.1.0",
		// The same Folder value on both, so the test cannot pass by the
		// in-network one simply being empty.
		Waypoints: []Waypoint{{Name: "top", Folder: "f", At: Position{Lon: -3, Lat: 57}}},
		Areas:     []Area{{Name: "top area", Folder: "f", Polygon: ring()}},
		Networks: []Network{{
			Name:   "n",
			Points: []Waypoint{{Name: "inside", Folder: "f", At: Position{Lon: -3, Lat: 57}}},
			Areas:  []Area{{Name: "inside area", Folder: "f", Polygon: ring()}},
		}},
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, []byte(`"folder":"f"`)); n != 2 {
		t.Errorf("got %d `folder` keys, want 2 — the top-level waypoint and the top-level area only:\n%s", n, got)
	}
}

func ring() []Position {
	return []Position{
		{Lon: -3, Lat: 57}, {Lon: -2.9, Lat: 57}, {Lon: -2.9, Lat: 57.1}, {Lon: -3, Lat: 57},
	}
}

// TestAnEmptyDataBagIsWrittenAsAnAbsentOne pins §3.1 at the `data` bag.
//
// §3.1 makes an absent container and an empty one the same content, and a
// reader must treat them alike. The MODEL still tells them apart — an absent
// `data` decodes to nil and `[]` decodes to a non-nil empty bag — because that
// is what a caller reading the document sees. The WRITER spells both as
// absence, which is what `omitempty` does and what §3.1 permits.
func TestAnEmptyDataBagIsWrittenAsAnAbsentOne(t *testing.T) {
	const src = `{"vnx":"0.1.0","waypoints":[` +
		`{"name":"absent","at":[-3,57]},` +
		`{"name":"empty","at":[-3,57],"data":[]}]}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("an empty data bag is legal: %v", diags)
	}
	if doc.Waypoints[0].Data != nil {
		t.Error("an absent `data` decoded to a non-nil bag")
	}
	if doc.Waypoints[1].Data == nil {
		t.Error("an empty `data` decoded to nil, so a caller cannot tell the two apart")
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte(`"data"`)) {
		t.Errorf("an empty data bag was written out; §3.1 lets a writer omit it:\n%s", got)
	}
	// And the file still round-trips, which is the half that says the omission
	// lost nothing a reader needed.
	diff, err := compareDocuments(got, []byte(src), treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("omitting the empty bag changed the document: %s", diff)
	}
}
