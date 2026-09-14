package vnx

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// The rounding helper (round.go), which all four writers share.

func TestRoundCoordinateAndElevation(t *testing.T) {
	for _, c := range []struct {
		name     string
		in, want float64
		round    func(float64) float64
	}{
		{"a coordinate keeps six places", -3.6712345678901235, -3.671235, RoundCoordinate},
		{"rounds, never truncates", 57.1181259, 57.118126, RoundCoordinate},
		{"computed noise disappears", 57.102000000000004, 57.102, RoundCoordinate},
		{"below the grid collapses to zero", 1e-7, 0, RoundCoordinate},
		{"an exact six-place value is untouched", -3.671234, -3.671234, RoundCoordinate},
		{"an elevation keeps one place", 328.03075289469285, 328, RoundElevation},
		{"an elevation rounds up", 328.06075289469285, 328.1, RoundElevation},
		{"a negative elevation rounds away from zero", -3.269679000000008, -3.3, RoundElevation},
		{"a whole elevation is untouched", 610, 610, RoundElevation},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.round(c.in); got != c.want {
				t.Errorf("round(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// ⚠ HAZARD 4. A rounding helper must not change WHETHER a document can be
// written. Every writer refuses a non-finite coordinate by name; if roundTo
// mapped NaN to 0 the refusal would become a point off the coast of Africa,
// reported as a successful export.
func TestRoundingLeavesNonFiniteValuesAloneSoTheRefusalsStillFire(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, got := range []float64{RoundCoordinate(v), RoundElevation(v)} {
			if math.IsNaN(v) {
				if !math.IsNaN(got) {
					t.Errorf("rounding NaN gave %v — a refusal became a value", got)
				}
				continue
			}
			if got != v {
				t.Errorf("rounding %v gave %v — a refusal became a value", v, got)
			}
		}
	}
	// And the refusal itself still reaches the caller through the encoder.
	_, err := Encode(&Document{Waypoints: []Waypoint{{Name: "n", At: Position{Lon: math.NaN(), Lat: 1}}}})
	if err == nil || !strings.Contains(err.Error(), "longitude") {
		t.Fatalf("Encode of a NaN longitude = %v, want a refusal naming the channel", err)
	}
}

// The other half of hazard 4: a value too large to scale must not overflow to
// +Inf and turn a writable elevation into a refused document. The oracle case
// `extreme-numbers` carries math.MaxFloat64 as an elevation.
func TestRoundingNeverManufacturesAnInfinity(t *testing.T) {
	for _, v := range []float64{math.MaxFloat64, -math.MaxFloat64, 1e21, 9007199254740991} {
		if got := RoundElevation(v); got != v {
			t.Errorf("RoundElevation(%v) = %v, want it left alone", v, got)
		}
		if got := RoundCoordinate(v); got != v {
			t.Errorf("RoundCoordinate(%v) = %v, want it left alone", v, got)
		}
	}
}

// ⚠ NEGATIVE ZERO. A writer spells -0 as `0`; the helper normalises so all
// four writers agree without each remembering to, and so that a tiny negative
// coordinate does not become `-0` in one format and `0` in another.
func TestRoundingNormalisesNegativeZero(t *testing.T) {
	for _, v := range []float64{negZero(), -0.0000004, -0.04} {
		if got := RoundCoordinate(v); math.Signbit(got) || got != 0 {
			if v == -0.04 {
				continue // -0.04 is not below the coordinate grid
			}
			t.Errorf("RoundCoordinate(%v) = %v (signbit %v), want +0", v, got, math.Signbit(got))
		}
	}
	if got := RoundElevation(-0.04); math.Signbit(got) || got != 0 {
		t.Errorf("RoundElevation(-0.04) = %v (signbit %v), want +0", got, math.Signbit(got))
	}
}

// ⚠ HAZARD 5. An absent elevation must stay absent. Rounding nil into 0.0
// would put every unknown-elevation point at sea level — the fallback the
// house rules forbid, and the one the nil *float64 exists to prevent.
func TestRoundPositionKeepsAnAbsentElevationAbsent(t *testing.T) {
	got := RoundPosition(Position{Lon: 1.23456789, Lat: 2.3456789})
	if got.Ele != nil {
		t.Fatalf("elevation became %v, want it to stay absent", *got.Ele)
	}
	if got.Time != nil {
		t.Fatalf("time became %v, want it to stay absent", *got.Time)
	}
	// A present zero elevation is NOT absence and must survive as a value.
	z := 0.04
	got = RoundPosition(Position{Lon: 1, Lat: 2, Ele: &z})
	if got.Ele == nil || *got.Ele != 0 {
		t.Fatalf("a present 0.04 elevation became %v, want a present 0", got.Ele)
	}
}

// The Document keeps full precision in memory: rounding happens at the write.
// A helper that wrote through the elevation pointer would round the caller's
// model, and the second export of one library would differ from the first.
func TestRoundPositionDoesNotTouchItsInput(t *testing.T) {
	ele := 328.03075289469285
	in := Position{Lon: -3.6712345678901235, Lat: 57.11812345678901, Ele: &ele}
	out := RoundPosition(in)
	if in.Lon != -3.6712345678901235 || in.Lat != 57.11812345678901 || *in.Ele != 328.03075289469285 {
		t.Fatalf("the input was mutated: %v", in)
	}
	if out.Ele == in.Ele {
		t.Fatal("the rounded position shares the caller's elevation pointer")
	}
}

// Rounding is deterministic, which is what hazard 3 (ring closure) rests on:
// equal inputs give byte-identical outputs, so a ring's first and last
// position cannot drift apart at the write.
func TestRoundingIsDeterministic(t *testing.T) {
	p := Position{Lon: -3.67123456789, Lat: 57.1181234567}
	a, b := RoundPosition(p), RoundPosition(p)
	if !a.Equal(b) {
		t.Fatalf("%v != %v", a, b)
	}
}

// A rounded value's SHORTEST round-tripping spelling — which is what every
// writer here emits — must have no more places than we rounded to. That is the
// mechanism by which the digits actually leave the file, and it is a property
// of the division, not something the format code can enforce.
func TestARoundedValueSpellsNoMoreDigitsThanItWasRoundedTo(t *testing.T) {
	for _, v := range []float64{
		-3.6712345678901235, 57.11812345678901, 0.1, 1.0 / 3.0,
		-179.99999949999, 12.3456785, -0.0000005,
	} {
		if places(t, RoundCoordinate(v)) > CoordinateDecimals {
			t.Errorf("RoundCoordinate(%v) spells %s", v, spell(RoundCoordinate(v)))
		}
		if places(t, RoundElevation(v)) > ElevationDecimals {
			t.Errorf("RoundElevation(%v) spells %s", v, spell(RoundElevation(v)))
		}
	}
}

// spell is how the value reaches a file: json.Marshal is vnx's and geojson's
// renderer, and it is the stricter of the two — strconv.FormatFloat(v,'f',-1)
// in gpx and kml never uses exponent form, so a value this one spells without
// an exponent is spelled the same way by all four.
func spell(v float64) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func places(t *testing.T, v float64) int {
	t.Helper()
	s := spell(v)
	if strings.ContainsAny(s, "eE") {
		t.Fatalf("%v rendered in exponent form (%s) — no rounded value should", v, s)
	}
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	return len(s) - i - 1
}

// VNX's own side of the same test the other three formats have.
//
// ⚠ VNX ROUNDS TOO, AND THAT IS THE PART THAT NEEDS SAYING. It is the format
// described as keeping everything, and at six places it is no longer bit-exact
// — see the note on Encode. Everything that MEANS anything survives; the
// seventh decimal place of a latitude does not.
func TestRoundingTiesGoToEven(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want float64
		note string
	}{
		// ⚠ THE FIRST SIX SEPARATE THE TWO ROUTES AND THE LAST FOUR DO NOT.
		// This test held only 340.25, 340.35 and their negatives, and every one
		// of those gives the same answer whether you round by formatting the
		// exact value or by scaling, rounding and dividing back. So the guard
		// was correctly reasoned, named its own importance accurately, and its
		// data could not separate the readings it guarded — the library rounded
		// by scaling and disagreed with corpus/numbers/ on 175 rows while this
		// stayed green.
		//
		// These six are the values spec §4.1 argues from, and each one the
		// scaling route gets wrong.
		{0.15, 0.1, "the scaling route gives 0.2"},
		{0.05, 0.1, "the scaling route gives 0"},
		{0.45, 0.5, "the scaling route gives 0.4"},
		{0.95, 0.9, "the scaling route gives 1"},
		{1.05, 1.1, "the scaling route gives 1"},
		{1.85, 1.9, "the scaling route gives 1.8"},

		// And the four that do not discriminate, kept because they are the
		// ordinary quarter-metre ties and a reader looking for those should
		// find them here.
		{340.25, 340.2, "round half to EVEN, not away from zero"},
		{340.35, 340.4, "the even neighbour is upward here"},
		{-340.25, -340.2, "and the sign does not change the rule"},
		{-340.35, -340.4, ""},
	} {
		if got := RoundElevation(c.in); got != c.want {
			t.Errorf("RoundElevation(%v) = %v, want %v %s", c.in, got, c.want, c.note)
		}
	}
	// A tie that rounding CREATES a negative zero from. No document fixture can
	// hand a writer one, because a tree comparison is by value and -0 equals 0;
	// corpus/numbers/ compares bit patterns and is the corpus's whole coverage
	// for this rule.
	if got := RoundElevation(-0.04); math.Signbit(got) {
		t.Errorf("RoundElevation(-0.04) = %v with the sign bit set; §4.1 applies the "+
			"negative-zero rule AFTER rounding, because rounding is what creates this one", got)
	}
}
