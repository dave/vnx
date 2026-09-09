package vnx

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestNumberConformanceTable runs this library against corpus/numbers/, which
// it did not do until 2026-09-10.
//
// ⚠ THE REFERENCE IMPLEMENTATION GENERATED THE SHARED TABLE AND NEVER RAN
// AGAINST IT. Five other libraries did. Measured when somebody finally
// compared: the library disagreed with its own table on 175 of 5,329 rows —
// 0.15 rounded to 0.2 where the table says 0.1, 0.05 to 0, 0.45 to 0.4 — every
// one of them a value spec §4.5.4 argues from.
//
// The cause was that round.go rounded by SCALING and the generator rounds by
// FORMATTING, which are different operations that agree on the easy cases. The
// guard that should have caught it, TestRoundingTiesGoToEven, uses 340.25 and
// 340.35, and both routes give the same answer for those — a correctly reasoned
// test, naming its own importance accurately, whose data could not separate the
// readings it guarded.
//
// This test is the fix for the class rather than for the instance: the library
// and the table now perform the same comparison every other implementation
// performs, over every row.
func TestNumberConformanceTable(t *testing.T) {
	f, err := os.Open("../../../corpus/numbers/numbers.tsv")
	if err != nil {
		t.Fatalf("the number table is part of the corpus and this library must run against it: %v", err)
	}
	defer f.Close()

	var rows, bad int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 5 {
			t.Fatalf("row %q has %d columns, want 5", line, len(col))
		}
		bits, err := strconv.ParseUint(col[0], 16, 64)
		if err != nil {
			t.Fatalf("row %q: unreadable bits: %v", line, err)
		}
		v := math.Float64frombits(bits)
		rows++

		for _, c := range []struct {
			what string
			got  string
			want string
		}{
			{"render", renderNumber(t, v), col[2]},
			{"round6", renderNumber(t, RoundCoordinate(v)), col[3]},
			{"round1", renderNumber(t, RoundElevation(v)), col[4]},
		} {
			if c.got != c.want {
				bad++
				if bad <= 10 {
					t.Errorf("%s: %s = %q, table says %q (kind %s)", col[0], c.what, c.got, c.want, col[1])
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if bad > 10 {
		t.Errorf("... and %d further disagreements", bad-10)
	}
	// LIVENESS. A table that failed to load, or a loop that skipped every row,
	// is the same green as agreement on all of them.
	if rows < 1000 {
		t.Fatalf("only %d rows compared; the table holds thousands, so this passed vacuously", rows)
	}
	t.Logf("%d rows compared, three columns each", rows)
}

// renderNumber spells v the way a document would, by going through the same
// writeNumber the encoder uses rather than a second implementation of it.
func renderNumber(t *testing.T, v float64) string {
	t.Helper()
	var b strings.Builder
	if err := writeNumber(&b, json.Number(strconv.FormatFloat(v, 'g', -1, 64))); err != nil {
		t.Fatalf("rendering %v: %v", v, err)
	}
	return b.String()
}
