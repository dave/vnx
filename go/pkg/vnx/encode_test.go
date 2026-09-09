package vnx

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"
)

// The round trip that matters: read the corpus example, write it back, and
// compare BYTES.
//
// This is the strongest test in the package, and it is strong for a specific
// reason — it fails if the encoder forgets a single field the decoder read.
// Every weaker version of it (compare a few fields, compare struct equality on
// what was decoded) passes happily while silently dropping whatever neither
// side happens to touch, which is the exact defect that let network arcs lose
// their `from` and `to` for four commits.
//
// The example is canonical, and Encode always emits canonical form, so byte
// equality is the right comparison rather than an unreasonably strict one.
func TestExampleRoundTripsByteForByte(t *testing.T) {
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
	if !bytes.Equal(got, want) {
		i := firstDiff(got, want)
		t.Fatalf("round trip changed the document at byte %d\n got: %s\nwant: %s",
			i, excerpt(got, i), excerpt(want, i))
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
// null, null] and [lon, lat] say the same thing and the short spelling is
// canonical — but [lon, lat, null, t] cannot lose its null without moving the
// time into the elevation channel.
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
			got, err := emitter{}.positionTree(c.p)
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

func excerpt(b []byte, i int) string {
	lo := max(0, i-40)
	hi := min(len(b), i+40)
	return string(b[lo:hi])
}

// Two spellings of the same fact, and the writer picks one.
//
// [lon, lat, null] and [lon, lat] both say "no elevation". A READER accepts
// either — corpus/accept/absent-elevation-and-time.vnx is stored in the long
// spelling on purpose to keep that tolerance under test — but a WRITER emits
// the short one, for the same reason numbers use their shortest round-trip
// spelling: canonical form exists so that two exports of the same library are
// byte-identical, and it can only do that if every fact has exactly one
// spelling on the way out.
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
			if got := formatTimestamp(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			// And what we write, we must read back to the same instant.
			back, err := parseTimestamp(c.want)
			if err != nil {
				t.Fatalf("our own output does not parse: %v", err)
			}
			if !back.Equal(c.in) {
				t.Fatalf("round trip changed the instant: %v vs %v", back, c.in)
			}
		})
	}
}
