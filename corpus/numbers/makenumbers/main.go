// Command makenumbers generates numbers.tsv, the number-conformance table.
//
// The table answers three questions per double, and every VNX implementation
// must give the same three answers:
//
//	render  the canonical spelling of the value            (SPEC 4.5.3)
//	round6  the canonical spelling after rounding to 6 dp  (SPEC 4.5.4, lon/lat)
//	round1  the canonical spelling after rounding to 1 dp  (SPEC 4.5.4, elevation)
//
// The answers come from Go's encoding/json and strconv, which SPEC 4.5.3 names
// as the reference alongside JavaScript. Generating them from anything else
// would be an implementation marking its own homework.
//
// # Why this is not a corpus fixture
//
// ROUNDING ties are unreachable by the corpus: rounding happens only when a
// WRITER emits a model, and all four corpus tiers RE-RENDER an existing
// document, which SPEC 8.1 requires precisely because a writer rounds. The
// corpus's reach ends at the writer boundary. A conforming implementation can
// round every coordinate the wrong way and pass every tier.
//
// RENDERING ties ARE reachable, and an earlier version of this comment said they
// were not. The argument was that a tie is a value rather than a shape, so no
// fixture could carry one; format/number-tie then carried one, spelling the value
// one way in its input and the other in its expected output. A value-level
// property is reachable by a re-render whenever the two readings produce
// different TEXT for the same value. What is left to this table beyond the
// rounding tie is the shortest-spelling rule over the whole double range, the
// exponent boundaries, and a negative zero a writer creates by rounding.
//
// Measured 2026-09-09, both rules are live disagreements between real
// implementations:
//
//	render: 1.4978982117220732e15 is 1497898211722073.2 in V8 and Go,
//	        and ...73.3 if a tie rounds away from zero.
//	round1: 340.25 is 340.2 in Go and Python, and 340.3 in JavaScript's
//	        toFixed. Any quarter-metre elevation is such a value.
//
// Usage, from this directory:
//
//	go run ./makenumbers            # the committed table
//	go run ./makenumbers -sample 300000 -out deep.tsv
//
// The committed table is deliberately small enough to read and to diff. The
// -sample flag is the deep check: 460,007 doubles were compared this way on
// 2026-09-09 to settle the rendering rule.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
)

func main() {
	sample := flag.Int("sample", 2000, "uniform random doubles to include")
	seed := flag.Int64("seed", 20260909, "seed, so the table is reproducible")
	out := flag.String("out", "numbers.tsv", "output path")
	flag.Parse()

	var rows []row
	counts := map[string]int{}
	add := func(kind string, values ...float64) {
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			rows = append(rows, makeRow(v, kind))
			counts[kind]++
		}
	}

	// 1. Every value SPEC 4.5.3, SPEC 4.5.6 and the corpus name.
	add("named",
		340, 0.000001, 1e-7, 1778914991000, 1e21, -3.6712345678901235,
		57.118, -3.6712, 57.1201, -3.675, 0.9, 0.25, 512.5, 352.4,
	)

	// 2. Boundaries: the exponent switch points of SPEC 4.5.3, the extremes of
	//    binary64, and the powers of ten either side of both.
	add("boundary",
		1e20, 1e21, 1e22, 1e-5, 1e-6, 1e-7, 1e-8,
		math.MaxFloat64, math.SmallestNonzeroFloat64, 2.2250738585072014e-308,
		math.Float64frombits(0x0010000000000000), math.Float64frombits(0x000fffffffffffff),
		9007199254740991, 9007199254740992, 9007199254740993,
	)
	for e := -30; e <= 30; e++ {
		add("boundary", math.Pow(10, float64(e)))
	}

	// 3. Negative values that collapse to zero once rounded. SPEC 4.5.3 requires
	//    negative zero to render as `0`, and a writer reaches it by ROUNDING
	//    rather than by being handed one — which is a path no fixture takes.
	add("negative-zero", -1e-9, -4e-7, -0.04, -0.0000004999, math.Copysign(0, -1))

	// 4. Rendering ties: values with two equally short spellings that both
	//    round-trip. This is the discriminating set for SPEC 4.5.3.
	r := rand.New(rand.NewSource(*seed))
	for found := 0; found < 60; {
		v := math.Float64frombits(r.Uint64())
		if math.IsNaN(v) || math.IsInf(v, 0) || v == 0 {
			continue
		}
		if isRenderTie(v) {
			add("render-tie", v)
			found++
		}
	}

	// 5. Rounding ties at 1 decimal place: the exact halfway values are the odd
	//    quarters, which is every quarter-metre elevation.
	for i := 1; i <= 40; i += 2 {
		add("round1-tie", float64(i)/4, -float64(i)/4, 340+float64(i)/4, 1296+float64(i)/4)
	}

	// 6. Rounding ties at 6 decimal places: the exact halfway values are the odd
	//    multiples of 1/128, since 5e-7 is not a dyadic rational and j/128 is the
	//    only family whose exact expansion ends in a 5 at the seventh place.
	for i := 1; i <= 40; i += 2 {
		add("round6-tie", float64(i)/128, -float64(i)/128, -3+float64(i)/128, 57+float64(i)/128)
	}

	// 7. A uniform random sample over the whole double range, as the safety net
	//    for whatever the categories above did not think of.
	for found := 0; found < *sample; {
		v := math.Float64frombits(r.Uint64())
		if math.IsNaN(v) || math.IsInf(v, 0) || v == 0 {
			continue
		}
		add("uniform", v)
		found++
	}

	// 8. A sample from the ranges this format actually carries, where a defect
	//    reaches a user rather than a test.
	for i := 0; i < 750; i++ {
		add("in-range",
			r.Float64()*360-180,               // longitude
			r.Float64()*180-90,                // latitude
			r.Float64()*9500-500,              // elevation
			math.Trunc(r.Float64()*2e12+1e12), // epoch milliseconds
		)
	}

	// The table is worthless if a category came out empty, and an empty category
	// is exactly the failure that would not announce itself.
	for _, kind := range []string{"named", "boundary", "negative-zero", "render-tie", "round1-tie", "round6-tie", "uniform", "in-range"} {
		if counts[kind] == 0 {
			log.Fatalf("category %q is empty, so the table does not discriminate what it claims to", kind)
		}
	}
	// And a tie category that does not actually differ from rounding away from
	// zero would be a category in name only.
	assertDiscriminates(rows)

	write(*out, rows, counts, *seed, *sample)
	log.Printf("wrote %s: %d rows %v", *out, len(rows), counts)
}

type row struct {
	bits                   uint64
	value                  float64
	kind                   string
	render, round6, round1 string
}

func makeRow(v float64, kind string) row {
	return row{
		bits:   math.Float64bits(v),
		value:  v,
		kind:   kind,
		render: canonical(v),
		round6: canonical(roundHalfEven(v, 6)),
		round1: canonical(roundHalfEven(v, 1)),
	}
}

// canonical renders a double as SPEC 4.5.3 requires: ECMAScript
// Number::toString, which is what encoding/json emits, with negative zero
// collapsed to `0` — the one correction the spec names against Go.
func canonical(v float64) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("marshalling %v: %v", v, err)
	}
	s := string(b)
	if s == "-0" {
		return "0"
	}
	return s
}

// roundHalfEven rounds to places decimal places over the EXACT binary value.
// strconv.FormatFloat is correctly rounded and breaks a tie to even, which is
// what Go and Python both do and what JavaScript's toFixed does not.
func roundHalfEven(v float64, places int) float64 {
	f, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', places, 64), 64)
	if err != nil {
		log.Fatalf("rounding %v: %v", v, err)
	}
	return f
}

// roundHalfAwayFromZero is here only so the generator can prove its tie
// categories discriminate. Nothing in the format uses it.
func roundHalfAwayFromZero(v float64, places int) string {
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		return ""
	}
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil))
	scaled := new(big.Rat).Mul(r, scale)
	half := big.NewRat(1, 2)
	if scaled.Sign() < 0 {
		scaled.Sub(scaled, half)
	} else {
		scaled.Add(scaled, half)
	}
	// Truncate towards zero.
	n := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	result := new(big.Rat).SetFrac(n, scale.Num())
	f, _ := result.Float64()
	return canonical(f)
}

// isRenderTie reports whether the choice of tie rule is VISIBLE for v: whether
// the shortest round-tripping decimal computed with ties-to-even differs from
// the one computed with ties-away-from-zero.
//
// This is defined AS the thing under test rather than approximated by it. An
// earlier version asked the weaker question "does a second, equally short
// spelling also round-trip", which is true whenever the double's rounding
// interval spans two candidates and not only at an exact tie. It mined 60 rows
// that looked like ties, and reverting the tie rule left every one of them
// green. A category that cannot fail is not a category.
func isRenderTie(v float64) bool {
	even, ok1 := shortest(v, false)
	away, ok2 := shortest(v, true)
	return ok1 && ok2 && even != away
}

// shortest returns the shortest decimal digit string that round-trips to v,
// rounded either to even or away from zero, as "digits@exponent". Exact
// rational arithmetic throughout: the point is to be right about the halfway
// case, which is the only case where the two answers differ.
func shortest(v float64, awayFromZero bool) (string, bool) {
	if v == 0 {
		return "0@1", true
	}
	exact := new(big.Rat).SetFloat64(math.Abs(v))
	if exact == nil {
		return "", false
	}
	// n is the decimal exponent: 10^(n-1) <= |v| < 10^n. Derived from a log and
	// then corrected, because a log is not exact near a power of ten.
	n := int(math.Floor(math.Log10(math.Abs(v)))) + 1
	for i := 0; i < 4; i++ {
		if exact.Cmp(pow10(n)) >= 0 {
			n++
		} else if exact.Cmp(pow10(n-1)) < 0 {
			n--
		} else {
			break
		}
	}
	for p := 1; p <= 17; p++ {
		scaled := new(big.Rat).Mul(exact, pow10(p-n))
		digits := roundRat(scaled, awayFromZero)
		back := new(big.Rat).Quo(new(big.Rat).SetInt(digits), pow10(p-n))
		f, _ := back.Float64()
		if math.Signbit(v) {
			f = -f
		}
		if f == v {
			return digits.String() + "@" + strconv.Itoa(n), true
		}
	}
	return "", false
}

func pow10(e int) *big.Rat {
	magnitude := e
	if magnitude < 0 {
		magnitude = -magnitude
	}
	p := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(magnitude)), nil))
	if e < 0 {
		return p.Inv(p)
	}
	return p
}

// roundRat rounds a non-negative rational to an integer, half to even or half
// away from zero.
func roundRat(r *big.Rat, awayFromZero bool) *big.Int {
	floor := new(big.Int).Div(r.Num(), r.Denom())
	remainder := new(big.Rat).Sub(r, new(big.Rat).SetInt(floor))
	switch remainder.Cmp(big.NewRat(1, 2)) {
	case -1:
		return floor
	case 1:
		return new(big.Int).Add(floor, big.NewInt(1))
	default:
		if awayFromZero || floor.Bit(0) == 1 {
			return new(big.Int).Add(floor, big.NewInt(1))
		}
		return floor
	}
}

func assertDiscriminates(rows []row) {
	var render, round1, round6 int
	for _, r := range rows {
		switch r.kind {
		case "render-tie":
			if isRenderTie(r.value) {
				render++
			}
		case "round1-tie":
			if roundHalfAwayFromZero(r.value, 1) != r.round1 {
				round1++
			}
		case "round6-tie":
			if roundHalfAwayFromZero(r.value, 6) != r.round6 {
				round6++
			}
		}
	}
	if render == 0 || round1 == 0 || round6 == 0 {
		log.Fatalf("a tie category does not discriminate: render=%d round1=%d round6=%d", render, round1, round6)
	}
	log.Printf("discriminating rows: render-tie=%d round1-tie=%d round6-tie=%d", render, round1, round6)
}

func write(path string, rows []row, counts map[string]int, seed int64, sample int) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintln(w, "# VNX number conformance table. Generated by makenumbers; do not edit by hand.")
	fmt.Fprintln(w, "# Columns, tab separated:  bits  kind  render  round6  round1")
	fmt.Fprintln(w, "#   bits    the double, as 16 hexadecimal digits of its IEEE 754 binary64 encoding")
	fmt.Fprintln(w, "#   kind    which part of the sample this row is, for diagnosis only")
	fmt.Fprintln(w, "#   render  its canonical spelling                     (SPEC 4.5.3)")
	fmt.Fprintln(w, "#   round6  its canonical spelling rounded to 6 places (SPEC 4.5.4, lon and lat)")
	fmt.Fprintln(w, "#   round1  its canonical spelling rounded to 1 place  (SPEC 4.5.4, elevation)")
	fmt.Fprintln(w, "# Answers are from Go's encoding/json and strconv, which SPEC 4.5.3 names as")
	fmt.Fprintln(w, "# the reference. Ties round to even in both columns.")
	fmt.Fprintf(w, "# seed=%d sample=%d rows=%d\n", seed, sample, len(rows))
	// Sorted, because Go randomises map iteration and this file must be
	// byte-reproducible — a table whose header changes on every run is a table
	// nobody can diff, which is most of what it is for.
	var kinds []string
	for k, n := range counts {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(kinds)
	fmt.Fprintf(w, "# %s\n", strings.Join(kinds, " "))

	for _, r := range rows {
		fmt.Fprintf(w, "%016x\t%s\t%s\t%s\t%s\n", r.bits, r.kind, r.render, r.round6, r.round1)
	}
}
