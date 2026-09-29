package vnx

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }
func i(v int64) *int64     { return &v }

func TestDecodePositionArity(t *testing.T) {
	cases := []struct {
		name string
		in   []any
		want Position
	}{
		{"arity 2", []any{-3.6712, 57.1180}, Position{Lon: -3.6712, Lat: 57.1180}},
		{"arity 3", []any{-3.6712, 57.1180, 340.0}, Position{Lon: -3.6712, Lat: 57.1180, Ele: f(340)}},
		{"arity 4", []any{-3.6712, 57.1180, 340.0, 1778569391000.0},
			Position{Lon: -3.6712, Lat: 57.1180, Ele: f(340), Time: i(1778569391000)}},
		// §4.1: null marks an absent value from index 2 up. Absent elevation is a
		// first-class internal state (canonical NaN in the pip blob, SQL NULL on a
		// node row) and MUST be expressible.
		{"null elevation", []any{-3.6712, 57.1180, nil}, Position{Lon: -3.6712, Lat: 57.1180}},
		{"null time in a timed segment", []any{-3.6712, 57.1180, 340.0, nil},
			Position{Lon: -3.6712, Lat: 57.1180, Ele: f(340)}},
		{"null elevation with time", []any{-3.6712, 57.1180, nil, 1778569391000.0},
			Position{Lon: -3.6712, Lat: 57.1180, Time: i(1778569391000)}},
		// Trailing positions beyond those we know MUST be ignored, so a future
		// fifth channel is additive rather than a flag day.
		{"unknown fifth channel ignored", []any{-3.6712, 57.1180, 340.0, 1778569391000.0, 99.0},
			Position{Lon: -3.6712, Lat: 57.1180, Ele: f(340), Time: i(1778569391000)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := decodePosition(c.in, "/p")
			if err != nil {
				t.Fatalf("decodePosition: %v", err)
			}
			if !got.Equal(c.want) {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestDecodePositionRefusals(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []any
	}{
		{"too short", []any{-3.6712}},
		{"empty", []any{}},
		{"null longitude", []any{nil, 57.1}},
		{"string longitude", []any{"-3.6712", 57.1}},
		{"latitude out of range", []any{-3.6712, 91.0}},
		{"longitude out of range", []any{-181.0, 57.1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodePosition(c.in, "/p"); err == nil {
				t.Fatalf("expected refusal for %v", c.in)
			}
		})
	}
}

// §4.2: non-finite numbers are banned file-wide. Python's json emits bare NaN
// and Infinity by default and accepts them on read, so without this rule the
// Python implementation writes files nothing else can parse.
func TestNonFiniteRefused(t *testing.T) {
	for _, src := range []string{
		`{"vnx":"0.2.0","generator":NaN}`,
		`{"vnx":"0.2.0","generator":Infinity}`,
		`{"vnx":"0.2.0","generator":-Infinity}`,
	} {
		if _, _, err := Decode([]byte(src)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: got %v, want ErrMalformed", src, err)
		}
	}
}

// §4.3: ISO 8601, UTC with a Z suffix. Offsets are not permitted — pinning the
// grammar is cheaper than five libraries each discovering Swift's
// ISO8601DateFormatter behaviour independently. SHAPE ONLY: a value naming no
// calendar instant matches the grammar and is valid.
func TestTimestampGrammar(t *testing.T) {
	ok := []string{"2026-05-12T07:03:11Z", "2026-05-12T07:03:11.250Z",
		"2026-13-45T99:99:99Z", "0000-00-00T00:00:00Z", "9999-99-99T99:99:99.000Z"}
	bad := []string{"2026-05-12T07:03:11+01:00", "2026-05-12T07:03:11", "2026-05-12", "12/05/2026", "",
		"2026-05-12 07:03:11Z", "2026-05-12T07:03:11.Z", "2026-5-12T07:03:11Z", "２０２６-05-12T07:03:11Z"}
	for _, s := range ok {
		if err := Timestamp(s).Valid(); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := Timestamp(s).Valid(); err == nil {
			t.Errorf("%q should be refused", s)
		}
	}
}

// The calendar is the caller's question: Time answers it, Valid does not.
func TestTimestampTimeRefusesWhatNoCalendarHas(t *testing.T) {
	if _, err := Timestamp("2026-13-45T99:99:99Z").Time(); err == nil {
		t.Fatal("Time must fail on a calendar-invalid value")
	}
	got, err := Timestamp("2026-05-12T07:03:11.250Z").Time()
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 5, 12, 7, 3, 11, 250000000, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Decode keeps a record-level timestamp verbatim, calendar or not, and Encode
// writes it back unchanged: the trailing zero of .250 survives.
func TestTimestampsAreKeptVerbatim(t *testing.T) {
	for _, s := range []string{"2026-05-12T07:03:11.250Z", "2026-13-45T99:99:99Z"} {
		src := `{"vnx":"0.2.0","tracks":[{"started":"` + s + `","segments":[{"pips":[[-3.67,57.11]]}]}]}`
		doc, diags, err := Decode([]byte(src))
		if err != nil || len(diags) != 0 {
			t.Fatalf("%s: err %v, diags %v", s, err, diags)
		}
		if doc.Tracks[0].Started != Timestamp(s) {
			t.Fatalf("decoded %q, want %q", doc.Tracks[0].Started, s)
		}
		out, err := Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out, []byte(`"`+s+`"`)) {
			t.Fatalf("%s was not written verbatim:\n%s", s, out)
		}
	}
}

// A caller-built value outside the grammar is an error at Encode, never a
// document §4.3 forbids — on a track and on the document header alike.
func TestEncodeRefusesAMalformedTimestamp(t *testing.T) {
	for name, doc := range map[string]*Document{
		"track ended": {Version: "0.2.0", Tracks: []Track{{Name: "t", Ended: "2026-05-12T07:03:11+01:00",
			Segments: []Segment{{Pips: []Position{{Lon: 1, Lat: 2}}}}}}},
		"document created": {Version: "0.2.0", Created: "2026-05-12T07:03:11+01:00"},
	} {
		if _, err := Encode(doc); err == nil {
			t.Errorf("%s: Encode wrote a timestamp outside §4.3's grammar", name)
		}
	}
}

// §4.3 names `created` beside `started` and `ended`: the same shape check, the
// same drop-and-diagnose, and a calendar-invalid value kept verbatim.
func TestCreatedIsARecordLevelTimestamp(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.2.0","created":"2026-05-12T08:03:11+01:00"}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Created != "" {
		t.Fatalf("the unusable stamp must not be kept, got %q", doc.Created)
	}
	if len(diags) != 1 || diags[0].Path != "/created" || diags[0].Code != CodeTimestampNotUTC {
		t.Fatalf("got %v", diags)
	}
	const odd = "2026-13-45T99:99:99Z"
	doc, diags, err = Decode([]byte(`{"vnx":"0.2.0","created":"` + odd + `"}`))
	if err != nil || len(diags) != 0 || doc.Created != odd {
		t.Fatalf("err %v, diags %v, created %q", err, diags, doc.Created)
	}
}

// The corpus needs a value that only survives at full binary64 precision; this
// is its unit-level twin. Comparing bits, not formatted strings — a formatted
// comparison would pass against a decoder that truncated to six places.
func TestFullPrecisionSurvivesRoundTrip(t *testing.T) {
	const lon = -3.6712345678901234
	p, err := decodePosition([]any{lon, 57.1}, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if math.Float64bits(p.Lon) != math.Float64bits(lon) {
		t.Fatalf("precision lost: got %b, want %b", math.Float64bits(p.Lon), math.Float64bits(lon))
	}
}
