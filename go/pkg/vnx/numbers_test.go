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
// ⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. How a number is spelled is
// the writer's own business (§4.2), and comparing spellings across
// implementations is the rule this format dropped. What is compared is the
// rounded VALUE, by bit pattern, which is where the one real defect was — the
// library disagreed with its own table on 0.15, rounding it to 0.2 where the
// table says 0.1, because round.go rounded by SCALING and the generator rounds
// by FORMATTING. Two operations that agree on the easy cases.
//
// ⚠ AND BY BIT PATTERN RATHER THAN BY TEXT. Comparing the rounded value as
// text would put a rendering rule back into the comparison through the side
// door, and would then pass or fail on how Go spells a double.
func TestNumberConformanceTable(t *testing.T) {
	f, err := os.Open("../../../corpus/numbers/numbers.tsv")
	if err != nil {
		t.Fatalf("the number table is part of the corpus and this library must run against it: %v", err)
	}
	defer f.Close()

	var rows, bad int
	declaredRows := -1
	declaredCounts := map[string]int{}
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			// The header declares how many rows and how many of each kind, and
			// the generator refuses to write a header that disagrees with its
			// body. Reading it is what makes a table that lost a whole category
			// a failure here rather than a silence: a hardcoded list of the
			// categories this test depends on would go stale the moment one is
			// added, and the newest category is always the one nothing pins.
			if rest, ok := strings.CutPrefix(line, "# rows="); ok {
				declaredRows, err = strconv.Atoi(strings.TrimSpace(rest))
				if err != nil {
					t.Fatalf("the header's row count is %q: %v", rest, err)
				}
			}
			if rest, ok := strings.CutPrefix(line, "# counts:"); ok {
				for _, pair := range strings.Fields(rest) {
					kind, n, found := strings.Cut(pair, "=")
					if !found {
						t.Fatalf("the header's counts line has %q, which is not kind=count", pair)
					}
					declaredCounts[kind], err = strconv.Atoi(n)
					if err != nil {
						t.Fatalf("the header declares %q for %s: %v", n, kind, err)
					}
				}
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 4 {
			t.Fatalf("row %q has %d columns, want 4", line, len(col))
		}
		bits, err := strconv.ParseUint(col[0], 16, 64)
		if err != nil {
			t.Fatalf("row %q: unreadable bits: %v", line, err)
		}
		v := math.Float64frombits(bits)
		rows++
		seen[col[1]]++

		for _, c := range []struct {
			what string
			got  float64
			want string
		}{
			{"round6", RoundCoordinate(v), col[2]},
			{"round1", RoundElevation(v), col[3]},
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
	if declaredRows < 1 {
		t.Fatal("the table's header declares no row count, so nothing here can tell a short table from a whole one")
	}
	if rows != declaredRows {
		t.Fatalf("%d rows read and the header declares %d", rows, declaredRows)
	}
	if len(declaredCounts) == 0 {
		t.Fatal("the table's header declares no per-kind counts")
	}
	for kind, want := range declaredCounts {
		if seen[kind] != want {
			t.Errorf("the header declares %d %s rows and the body holds %d", want, kind, seen[kind])
		}
	}
	for kind := range seen {
		if _, ok := declaredCounts[kind]; !ok {
			t.Errorf("the body holds %s rows the header does not declare", kind)
		}
	}
	t.Logf("%d rows compared, two rounding columns each", rows)
}
