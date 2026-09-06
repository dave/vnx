package vnx

import (
	"errors"
	"math"
	"testing"
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
		`{"vnx":"0.1.0","generator":NaN}`,
		`{"vnx":"0.1.0","generator":Infinity}`,
		`{"vnx":"0.1.0","generator":-Infinity}`,
	} {
		if _, _, err := Decode([]byte(src)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: got %v, want ErrMalformed", src, err)
		}
	}
}

// §4.3: ISO 8601, UTC with a Z suffix. Offsets are not permitted — pinning the
// grammar is cheaper than five libraries each discovering Swift's
// ISO8601DateFormatter behaviour independently.
func TestTimestampGrammar(t *testing.T) {
	ok := []string{"2026-05-12T07:03:11Z", "2026-05-12T07:03:11.250Z"}
	bad := []string{"2026-05-12T07:03:11+01:00", "2026-05-12T07:03:11", "2026-05-12", "12/05/2026", ""}
	for _, s := range ok {
		if _, err := parseTimestamp(s); err != nil {
			t.Errorf("%q should parse: %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := parseTimestamp(s); err == nil {
			t.Errorf("%q should be refused", s)
		}
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
