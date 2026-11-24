package merge

// corridorcost_envelope_equiv_test.go — the equivalence guard for the
// performance rewrite of [envelopeMinCost].
//
// [envelopeMinCost] was 29% of a synthetic-trail import's CPU (profiled 2026-08-21,
// with pieceCrossings a further 16% and runtime.madvise 13% of samples from
// its allocation churn), so it was rewritten for speed alone: quadratic
// coefficients hoisted out of the O(n^2) pair loop, crossings returned in a
// fixed array instead of a freshly allocated slice per pair, and the
// breakpoint set collected in a sorted slice instead of a map[float64]bool.
//
// NONE of that is allowed to change a number. The function below is the
// PRE-REWRITE implementation, copied verbatim on 2026-08-21 and frozen. It is
// the reference the rewrite is graded against, bit for bit — not
// approximately, because every caller of this is a cost the solver compares
// against another cost, and a change in the last bit can flip a Dijkstra
// tie-break and move committed geometry.
//
// ⚠ DO NOT "FIX" OR TIDY THIS COPY. It is a historical record, not live code.
// If a genuine behaviour change to envelopeMinCost is ever wanted, this file
// is what has to be deliberately re-baselined, with the reason written down —
// which is the point of freezing it rather than trusting a memory that the
// rewrite was safe.

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

func envelopeMinCostFrozenReference(pieces []BandPiece, globalLo, globalHi, r float64, k int) (float64, error) {
	if err := validateEvenK(k); err != nil {
		return 0, err
	}
	if !(r > 0) || !allFinite(r) {
		return 0, fmt.Errorf("corridorcost: gap radius must be finite and positive, got %v", r)
	}
	if len(pieces) == 0 || globalHi <= globalLo {
		return 0, nil
	}

	breaks := map[float64]bool{globalLo: true, globalHi: true}
	for _, p := range pieces {
		if p.Lo > globalLo && p.Lo < globalHi {
			breaks[p.Lo] = true
		}
		if p.Hi > globalLo && p.Hi < globalHi {
			breaks[p.Hi] = true
		}
	}
	for i := 0; i < len(pieces); i++ {
		for j := i + 1; j < len(pieces); j++ {
			lo := max(pieces[i].Lo, pieces[j].Lo, globalLo)
			hi := min(pieces[i].Hi, pieces[j].Hi, globalHi)
			if lo >= hi {
				continue
			}
			for _, x := range pieceCrossings(pieces[i], pieces[j], lo, hi) {
				breaks[x] = true
			}
		}
	}
	sorted := make([]float64, 0, len(breaks))
	for b := range breaks {
		sorted = append(sorted, b)
	}
	sort.Float64s(sorted)

	m := k / 2
	total := 0.0
	for i := 0; i+1 < len(sorted); i++ {
		lo, hi := sorted[i], sorted[i+1]
		if hi <= lo {
			continue
		}
		mid := (lo + hi) / 2
		best := -1
		bestVal := math.Inf(1)
		for pi, p := range pieces {
			if mid < p.Lo || mid > p.Hi {
				continue
			}
			if v := p.distSq(mid); v < bestVal {
				bestVal, best = v, pi
			}
		}
		if best < 0 {
			return 0, fmt.Errorf("corridorcost: no candidate piece covers t=%.17g in [%.17g,%.17g] — the gap's candidate set is incomplete", mid, lo, hi)
		}
		win := pieces[best]
		win.Lo, win.Hi = lo, hi
		integral, err := integrateDistSqPow(win, m)
		if err != nil {
			return 0, err
		}
		total += integral
	}
	return total / math.Pow(r, float64(k)), nil
}

// randomBandPiece builds a piece with the same shape the band solve produces:
// either a RegimeSpan (a linear form squared over a positive length) or a
// plain upward quadratic. Coefficients are drawn wide enough that pairs
// genuinely cross inside the range rather than being trivially ordered.
func randomBandPiece(rng *rand.Rand, lo, hi float64) BandPiece {
	a := lo + rng.Float64()*(hi-lo)
	b := lo + rng.Float64()*(hi-lo)
	if b < a {
		a, b = b, a
	}
	if b == a {
		b = a + 1e-3
	}
	if rng.Intn(2) == 0 {
		return BandPiece{
			Lo: a, Hi: b, Kind: RegimeSpan,
			SpanC0: rng.NormFloat64() * 10, SpanC1: rng.NormFloat64() * 10,
			SpanLenSq: 0.5 + rng.Float64()*20,
		}
	}
	// A near-regime piece, built from its linear offset as bandPieces builds
	// one: the integral evaluates the
	// sum-of-squares form, so a trinomial with no offset behind it is not a
	// piece the band solve can produce.
	return nearPieceFromOffset(RegimeNearA, a, b,
		rng.NormFloat64()*4, rng.NormFloat64()*4, 0.1+rng.Float64()*2, rng.NormFloat64()*2)
}

// TestEnvelopeMinCost_MatchesFrozenReferenceBitForBit is the whole guard: the
// rewrite must be a pure speed change. Bit-identical, not "within epsilon" —
// see the file header for why the last bit matters here.
//
// A change in the order of operations shows on the first non-trivial input, so
// the cases are one per shape the function handles, each at k=2 and k=4: one
// piece over the whole range; two and three overlapping pieces whose dist²
// curves cross inside the range (crossings become breakpoints); two pieces
// whose dist² is equal everywhere, so every winner is a tie and every
// breakpoint a duplicate; and a hole no piece covers, where both must refuse.
//
// Red proofs, 2026-10-05 (detached worktree): with the pair crossings left out
// of the breakpoints, this fails on "two crossing pieces k=2"; with the
// sub-interval integrals summed last to first, it fails on the same case one
// bit apart (0.014754695077583507 vs …506); with an uncovered midpoint skipped
// instead of refused, it fails on "a hole k=2" — which the old random sweep
// did not catch.
func TestEnvelopeMinCost_MatchesFrozenReferenceBitForBit(t *testing.T) {
	const (
		globalLo = 0.0
		globalHi = 1.0
		r        = 15.0
	)
	whole := BandPiece{Lo: 0, Hi: 1, Kind: RegimeSpan, SpanC0: 4, SpanC1: -6, SpanLenSq: 1} // (4−6t)², zero at t=2/3
	nearMid := nearPieceFromOffset(RegimeNearA, 0.2, 0.9, 1, 0.5, 0.5, 0.2)                 // ≈ 1.1–2.4 m², crosses whole twice
	nearEarly := nearPieceFromOffset(RegimeNearA, 0.1, 0.6, -2, 1, 3, 0.5)
	cases := []struct {
		name      string
		pieces    []BandPiece
		crossing  bool // the first two pieces cross inside their overlap
		wantError bool
	}{
		{"one whole-range piece", []BandPiece{whole}, false, false},
		{"two crossing pieces", []BandPiece{whole, nearMid}, true, false},
		{"three crossing pieces", []BandPiece{whole, nearMid, nearEarly}, true, false},
		{"an exact tie everywhere", []BandPiece{
			{Lo: 0, Hi: 1, Kind: RegimeSpan, SpanC0: 3, SpanC1: 4, SpanLenSq: 1},
			nearPieceFromOffset(RegimeNearA, 0, 1, 3, 0, 4, 0)}, false, false},
		{"a hole", []BandPiece{
			nearPieceFromOffset(RegimeNearA, 0, 0.4, 1, 1, 1, 1),
			nearPieceFromOffset(RegimeNearA, 0.6, 1, 1, 1, 1, 1)}, false, true},
	}
	for _, c := range cases {
		if c.crossing {
			p, q := c.pieces[0], c.pieces[1]
			if len(pieceCrossings(p, q, max(p.Lo, q.Lo), min(p.Hi, q.Hi))) == 0 {
				t.Fatalf("%s: the first two pieces do not cross — the case no longer makes crossing breakpoints", c.name)
			}
		}
		for _, k := range []int{2, 4} {
			want, wErr := envelopeMinCostFrozenReference(c.pieces, globalLo, globalHi, r, k)
			got, gErr := envelopeMinCost(context.Background(), c.pieces, globalLo, globalHi, r, k)
			if (wErr != nil) != c.wantError {
				t.Fatalf("%s k=%d: reference err=%v, want an error: %v", c.name, k, wErr, c.wantError)
			}
			if (wErr == nil) != (gErr == nil) {
				t.Fatalf("%s k=%d: reference err=%v but rewrite err=%v — the rewrite must fail exactly where the reference fails",
					c.name, k, wErr, gErr)
			}
			if wErr != nil {
				continue
			}
			if !(want > 0) {
				t.Fatalf("%s k=%d: reference cost %v is not positive — the case prices nothing", c.name, k, want)
			}
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("%s k=%d: rewrite returned %.17g, frozen reference returned %.17g — "+
					"this rewrite is a SPEED change and is not permitted to move a cost, because the solver "+
					"compares these against each other and the last bit can flip a tie-break",
					c.name, k, got, want)
			}
		}
	}
}

// BenchmarkEnvelopeMinCost_RewriteVsFrozen measures the point of the rewrite
// against the implementation it replaced, on the same inputs, in the same
// process — so a contended box slows both arms equally instead of deciding
// the answer. Report ns/op AND allocs/op: the allocation count is the part
// that showed up as runtime.madvise in the import profile.
func BenchmarkEnvelopeMinCost_RewriteVsFrozen(b *testing.B) {
	rng := rand.New(rand.NewSource(20260821))
	sets := make([][]BandPiece, 64)
	for s := range sets {
		n := 3 + rng.Intn(6)
		p := make([]BandPiece, n)
		for i := range p {
			p[i] = randomBandPiece(rng, 0, 1)
		}
		p[0].Lo, p[0].Hi = 0, 1
		sets[s] = p
	}
	b.Run("frozen", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := envelopeMinCostFrozenReference(sets[i%len(sets)], 0, 1, 15, 2); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("rewrite", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := envelopeMinCost(context.Background(), sets[i%len(sets)], 0, 1, 15, 2); err != nil {
				b.Fatal(err)
			}
		}
	})
}
