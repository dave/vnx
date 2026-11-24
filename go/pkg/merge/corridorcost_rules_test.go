package merge

import (
	"math"
	"testing"
)

// TestGaussLegendreRules checks what IntegrateDistSqPow's non-negativity and
// exactness rest on: every weight is strictly positive, and each n-point rule
// integrates x^j over [−1, 1] for every j ≤ 2n−1 to within a few ulps of the
// exact 2/(j+1) (even j) or 0 (odd j).
func TestGaussLegendreRules(t *testing.T) {
	for n := 1; n <= GaussLegendreMaxPoints; n++ {
		r := gaussLegendreRules[n]
		if len(r.X) != n || len(r.W) != n {
			t.Fatalf("n=%d: rule has %d nodes and %d weights", n, len(r.X), len(r.W))
		}
		for i := range r.W {
			if !(r.W[i] > 0) || !(r.X[i] > -1 && r.X[i] < 1) {
				t.Fatalf("n=%d: node %d is x=%v w=%v — every weight must be > 0 and every node inside (−1, 1)", n, i, r.X[i], r.W[i])
			}
		}
		for j := 0; j <= 2*n-1; j++ {
			got := 0.0
			for i := range r.X {
				got += r.W[i] * math.Pow(r.X[i], float64(j))
			}
			want := 0.0
			if j%2 == 0 {
				want = 2 / float64(j+1)
			}
			if math.Abs(got-want) > 1e-14 {
				t.Errorf("n=%d: ∫x^%d = %.17g, want %.17g", n, j, got, want)
			}
		}
	}
}
