package vnx

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestNumberConformanceTable runs this library against corpus/numbers/.
//
// ⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. The table's `render`
// column is not read: how a number is spelled is the writer's own business
// (§4.2), and comparing spellings across implementations is the rule this
// format dropped. What is compared is the rounded VALUE, by bit pattern, which
// is where the one real defect was — the library disagreed with its own table
// on 175 of its rows, 0.15 rounding to 0.2 where the table says 0.1, because
// round.go rounded by SCALING and the generator rounds by FORMATTING. Two
// operations that agree on the easy cases.
//
// ⚠ AND BY BIT PATTERN RATHER THAN BY TEXT. Comparing the rounded value as
// text would put the rendering rule back into the comparison through the side
// door, and would then pass or fail on how Go spells a double.
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
			got  float64
			want string
		}{
			{"round6", RoundCoordinate(v), col[3]},
			{"round1", RoundElevation(v), col[4]},
		} {
			want, err := strconv.ParseFloat(c.want, 64)
			if err != nil {
				t.Fatalf("row %q: column %s is %q, which is not a number: %v", col[0], c.what, c.want, err)
			}
			// Bit patterns, so the comparison is over the value and not over
			// how either side spells it. -0 and 0 are DIFFERENT bit patterns
			// and the same value; §4.1 has rounding normalise the sign, so the
			// table and the library agree on +0 and a mismatch here is real.
			if math.Float64bits(c.got) != math.Float64bits(want) {
				bad++
				if bad <= 10 {
					t.Errorf("%s: %s = %v (%016x), table says %v (%016x) (kind %s)",
						col[0], c.what, c.got, math.Float64bits(c.got), want, math.Float64bits(want), col[1])
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
	t.Logf("%d rows compared, two rounding columns each", rows)
}
