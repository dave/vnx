// Command makenumbers generates numbers.tsv, the number-conformance table.
//
// The table answers two questions per double, and every VNX implementation must
// give the same two answers:
//
//	round6  the value after rounding to 6 decimal places  (SPEC 4.1, lon/lat)
//	round1  the value after rounding to 1 decimal place   (SPEC 4.1, elevation)
//
// The answers come from Go's strconv, which rounds correctly and breaks a tie
// to even. Generating them from anything else would be an implementation
// marking its own homework.
//
// # What the table is for
//
// It tests an OPERATION, not a spelling. How a number is written is the
// writer's own business, so the answer columns are read as values: a runner
// parses each column and compares bit patterns.
//
// A fixture pins a rule at one value; write/emit-boundary does that for the
// rounding tie. This table pins the rule over a chosen set of values, each of
// which separates a wrong rule from the right one:
//
//   - the two directions of a tie, at one place and at six;
//   - rounding against the double against scaling by a power of ten first;
//   - a negative zero a writer CREATES by rounding towards it, which no
//     document fixture can hand it.
//
// Every value here is chosen, and the guards below refuse to write a table in
// which a category stops discriminating. Usage, from this directory:
//
//	go run ./makenumbers
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
)

func main() {
	out := flag.String("out", "numbers.tsv", "output path")
	flag.Parse()

	var rows []row
	counts := map[string]int{}
	add := func(kind string, values ...float64) {
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				log.Fatalf("%v has no rounding to state", v)
			}
			rows = append(rows, makeRow(v, kind))
			counts[kind]++
		}
	}

	// 1. Real coordinates and elevations, of the shape the format carries.
	add("named",
		-3.6712345678901235, 57.11812345678901, -3.6712, 57.1201, -3.675,
		340, 512.5, 352.4,
	)

	// 2. The ends of each range, and zero. A coordinate runs to 180 and 90; an
	//    elevation runs from the lowest dry land to the highest summit. 1e-6 is
	//    the smallest coordinate the six-place rule keeps and 1e-7 the largest
	//    it discards.
	add("boundary",
		0, 180, -180, 90, -90, 8848.9, -430.5, 0.000001, 1e-7,
	)

	// 3. Negative values that collapse to zero once rounded. A writer reaches
	//    negative zero by ROUNDING towards it, which is a path no document
	//    fixture takes, and 4.1 requires the sign to be normalised away.
	//
	// ⚠ THE LAST ROW IS THE ONLY INSTRUMENT LEFT FOR THAT RULE. A tree
	// comparison is by value, and -0 and 0 are the same value, so no document
	// tier can see a negative zero at all. This table compares bit patterns.
	add("negative-zero", -1e-9, -4e-7, -0.04, -0.0000004999, math.Copysign(0, -1))

	// 4. Rounding ties at 1 decimal place, in both directions. The exact
	//    halfway values are the odd quarters, so any quarter-metre elevation.
	//    340.25 rounds down and 340.75 rounds up, and it takes the pair: a
	//    table carrying one of them leaves two wrong rules passing.
	add("round1-tie", 0.25, -0.25, 340.25, 340.75, -340.75, 1296.75)

	// 5. Rounding ties at 6 decimal places, in both directions. The exact
	//    halfway values are the odd multiples of 1/128, because 5e-7 is not a
	//    dyadic rational and j/128 is the only family whose exact expansion
	//    ends in a 5 at the seventh place.
	add("round6-tie",
		1.0/128, -1.0/128, 3.0/128, -3.0/128, 5.0/128,
		57+1.0/128, -3+1.0/128, 340+3.0/128,
	)

	// 6. Values that separate the two ways of rounding to a decimal place, at
	//    the magnitudes the format carries.
	//
	// SPEC 4.1 defines the rounded value as a RESULT — the value with that many
	// places nearest the double — and multiplying by 10^n, rounding, and
	// dividing back is a different operation. The scaled product is itself a
	// binary64, so the multiplication can manufacture a tie the true decimal
	// never had and destroy one it did. 0.15 is the value to put in front of
	// anyone who thinks this is a corner case: it is nearest 0.1, the scaling
	// route gives 0.2, and it is a tie under neither.
	//
	// The elevations are the seven values 4.1 argues from plus two the format's
	// own fixtures use; the coordinates are valid longitudes and latitudes
	// found by search, since no round number of degrees separates the routes.
	add("scaling-route",
		0.15, 0.05, 1.05, 0.45, 1.85, 0.95, 340.15, 512.45,
		-154.8520475, 27.7570615, -125.2855675, 28.7622735,
	)

	// The table is worthless if a category came out empty, and an empty category
	// is exactly the failure that would not announce itself.
	for _, kind := range []string{"named", "boundary", "negative-zero", "round1-tie", "round6-tie", "scaling-route"} {
		if counts[kind] == 0 {
			log.Fatalf("category %q is empty, so the table does not discriminate what it claims to", kind)
		}
	}
	assertDiscriminates(rows)
	assertCatchesScalingRoute(rows)
	assertReachesNegativeZero(rows)

	write(*out, rows, counts)
	log.Printf("wrote %s: %d rows %v", *out, len(rows), counts)
}

type row struct {
	bits           uint64
	value          float64
	kind           string
	round6, round1 string
}

func makeRow(v float64, kind string) row {
	return row{
		bits:   math.Float64bits(v),
		value:  v,
		kind:   kind,
		round6: spell(roundHalfEven(v, 6)),
		round1: spell(roundHalfEven(v, 1)),
	}
}

// spell writes the answer as decimal text a runner parses back to a double. The
// text is a carrier and not an expectation: the comparison is over the value.
// Negative zero is written as `0` because 4.1 normalises the sign away, and a
// runner comparing bit patterns is what checks that it did.
func spell(v float64) string {
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
	return spell(f)
}

// assertDiscriminates refuses to write a table whose tie categories agree with
// rounding away from zero. A category that names a defect and does not catch it
// is worse than no category, because it reads as coverage.
//
// ⚠ EVERY GUARD IN THIS FILE EXISTS BECAUSE THAT HAPPENED. A tie category once
// mined with a round-trip proxy held sixty rows, every one of which stayed green
// when the tie rule was reverted.
func assertDiscriminates(rows []row) {
	var round1, round6 int
	for _, r := range rows {
		switch r.kind {
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
	if round1 == 0 || round6 == 0 {
		log.Fatalf("a tie category does not discriminate: round1=%d round6=%d", round1, round6)
	}
	log.Printf("discriminating rows: round1-tie=%d round6-tie=%d", round1, round6)
}

// assertCatchesScalingRoute is assertDiscriminates for the OTHER rounding
// disagreement: not which way a tie goes, but whether the value was a tie at
// all.
//
// The scaled route is written here the way an implementer would write it —
// multiply, round half-to-even, divide — and every row of the category must
// disagree with the reference answer at one place or at six. A value that agrees
// under both routes tests nothing and must not sit in this category wearing its
// name.
func assertCatchesScalingRoute(rows []row) {
	caught := 0
	for _, r := range rows {
		if r.kind != "scaling-route" {
			continue
		}
		if scaledRoute(r.value, 1) != r.round1 || scaledRoute(r.value, 6) != r.round6 {
			caught++
			continue
		}
		log.Fatalf("scaling-route row %v agrees with the scaled route at both places, so it discriminates nothing", r.value)
	}
	if caught == 0 {
		log.Fatal("no scaling-route row separates the two rounding routes")
	}
	log.Printf("discriminating rows: scaling-route=%d", caught)
}

// scaledRoute is the non-conforming route SPEC 4.1 rules out: multiply by a
// power of ten, round the product to even, and divide back. It exists only so
// the guard above can prove the category catches it.
func scaledRoute(v float64, places int) string {
	scale := math.Pow(10, float64(places))
	p := v * scale
	if math.IsInf(p, 0) || math.IsNaN(p) {
		return spell(v)
	}
	return spell(math.RoundToEven(p) / scale)
}

// assertReachesNegativeZero refuses to write a table that cannot see a writer
// which leaves the sign on a rounded zero.
//
// ⚠ THIS TABLE IS THE WHOLE OF THE CORPUS'S COVERAGE FOR THAT RULE. Every
// document tier compares trees by value, and -0 equals 0, so a negative zero a
// writer created is invisible there. Here the answer column is `0` and a runner
// compares bit patterns, so -0 fails.
func assertReachesNegativeZero(rows []row) {
	reached := 0
	for _, r := range rows {
		for _, places := range []int{1, 6} {
			if v := roundHalfEven(r.value, places); v == 0 && math.Signbit(v) {
				reached++
			}
		}
	}
	if reached == 0 {
		log.Fatal("no row rounds to a negative zero, so the sign rule of SPEC 4.1 is unreachable from this table")
	}
	log.Printf("rows rounding to a negative zero: %d", reached)
}

func write(path string, rows []row, counts map[string]int) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintln(w, "# VNX number conformance table. Generated by makenumbers; do not edit by hand.")
	fmt.Fprintln(w, "# Columns, tab separated:  bits  kind  round6  round1")
	fmt.Fprintln(w, "#   bits    the double, as 16 hexadecimal digits of its IEEE 754 binary64 encoding")
	fmt.Fprintln(w, "#   kind    which part of the sample this row is, for diagnosis only")
	fmt.Fprintln(w, "#   round6  the value rounded to 6 places (SPEC 4.1, lon and lat)")
	fmt.Fprintln(w, "#   round1  the value rounded to 1 place  (SPEC 4.1, elevation)")
	fmt.Fprintln(w, "# The answer columns are decimal text carrying a VALUE. Parse them and compare")
	fmt.Fprintln(w, "# bit patterns; comparing the text would test a spelling, which is not a rule.")
	fmt.Fprintln(w, "# Ties round to even in both columns.")
	// ⚠ THE NEXT TWO LINES ARE A CONTRACT, NOT A COMMENT. A runner parses
	// `rows=` and the `counts:` line and asserts that what it observed matches
	// what they declare. That is the only completeness check that survives a
	// new category: a hand-written list of "the categories I depend on" ages
	// badly, and the newest category is always the one nothing pins.
	//
	// `counts:` carries its own label so a parser can find it without counting
	// header lines or matching on which keys it happens to contain.
	fmt.Fprintf(w, "# rows=%d\n", len(rows))
	// Sorted, because Go randomises map iteration and this file must be
	// byte-reproducible — a table whose header changes on every run is a table
	// nobody can diff, which is most of what it is for.
	var kinds []string
	total := 0
	for k, n := range counts {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, n))
		total += n
	}
	sort.Strings(kinds)
	// A header that disagrees with the body is worse than no header, because a
	// runner asserting against it would fail for a reason that is not its own.
	if total != len(rows) {
		log.Fatalf("declared counts sum to %d but the table has %d rows", total, len(rows))
	}
	fmt.Fprintf(w, "# counts: %s\n", strings.Join(kinds, " "))

	for _, r := range rows {
		fmt.Fprintf(w, "%016x\t%s\t%s\t%s\n", r.bits, r.kind, r.round6, r.round1)
	}
}
