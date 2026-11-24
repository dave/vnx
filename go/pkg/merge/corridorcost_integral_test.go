package merge

// corridorcost_integral_test.go — the emission integral is non-negative by
// construction and exact.
//
// The old integral expanded dist²(t)^m into power-basis coefficients and
// applied the power rule, Σ c_j/(j+1)·(hi^(j+1) − lo^(j+1)). On short pieces
// that cancels and went NEGATIVE on real tracks: 4,932 times on two real
// imports, down to −1.43e-8, and the negative arc costs let the solve improve
// states it had already popped.
// [integrateDistSqPow] is now an (m+1)-point Gauss–Legendre rule over a
// sum-of-squares integrand. These tests hold it to three things: it is never
// negative, including on the recorded pieces that broke the old one; it is
// exact, against an arbitrary-precision reference; and a negative cost, if one
// is ever formed, is an error.

import (
	"math"
	"math/big"
	"strings"
	"testing"
)

// ---- the old formula, frozen as the red-proof arm -------------------------

// legacyIntegrateDistSqPow is the old integral, verbatim in its arithmetic:
// power-basis expansion of the trinomial (near regimes) or of the linear form
// (span), then the power rule. It is here ONLY so the recorded failing pieces
// can be shown to still fail it — the fixtures' liveness — and so the new
// integral's accuracy is compared against what it replaced. Not live code.
func legacyIntegrateDistSqPow(bp BandPiece, m int) float64 {
	polyMul := func(x, y []float64) []float64 {
		out := make([]float64, len(x)+len(y)-1)
		for i, xi := range x {
			if xi == 0 {
				continue
			}
			for j, yj := range y {
				out[i+j] += xi * yj
			}
		}
		return out
	}
	quadPow := func(a, b, c float64, m int) []float64 {
		coeffs := []float64{1}
		base := []float64{c, b, a}
		for i := 0; i < m; i++ {
			coeffs = polyMul(coeffs, base)
		}
		return coeffs
	}
	integratePoly := func(coeffs []float64, lo, hi float64) float64 {
		sum := 0.0
		for deg, c := range coeffs {
			if c == 0 {
				continue
			}
			n := float64(deg + 1)
			sum += c / n * (math.Pow(hi, n) - math.Pow(lo, n))
		}
		return sum
	}
	if bp.Hi == bp.Lo {
		return 0
	}
	if bp.Kind == RegimeSpan {
		return integratePoly(quadPow(0, bp.SpanC1, bp.SpanC0, 2*m), bp.Lo, bp.Hi) / math.Pow(bp.SpanLenSq, float64(m))
	}
	return integratePoly(quadPow(bp.QuadA, bp.QuadB, bp.QuadC, m), bp.Lo, bp.Hi)
}

// ---- the arbitrary-precision reference ----------------------------------

// bigIntegrateDistSqPow is ∫ dist²(t)^m over [lo, hi] in 1024-bit arithmetic,
// from the piece's float64 inputs taken as exact. It shares no arithmetic with
// either float64 form: the integrand polynomial is expanded exactly (from the
// linear offset, not the float64 trinomial), and the power rule's differences
// of powers cannot cancel at this precision on any piece a float64 can name.
func bigIntegrateDistSqPow(bp BandPiece, m int) float64 {
	const prec = 1024
	nf := func(v float64) *big.Float { return new(big.Float).SetPrec(prec).SetFloat64(v) }
	mul := func(x, y []*big.Float) []*big.Float {
		out := make([]*big.Float, len(x)+len(y)-1)
		for i := range out {
			out[i] = nf(0)
		}
		for i, xi := range x {
			for j, yj := range y {
				out[i+j].Add(out[i+j], new(big.Float).SetPrec(prec).Mul(xi, yj))
			}
		}
		return out
	}
	var base []*big.Float // dist²(t), ascending coefficients
	var scale *big.Float
	switch bp.Kind {
	case RegimeSpan:
		lin := []*big.Float{nf(bp.SpanC0), nf(bp.SpanC1)}
		base = mul(lin, lin)
		scale = new(big.Float).SetPrec(prec).Quo(nf(1), nf(bp.SpanLenSq))
	default:
		x := []*big.Float{nf(bp.NearX0), nf(bp.NearX1)}
		y := []*big.Float{nf(bp.NearY0), nf(bp.NearY1)}
		xx, yy := mul(x, x), mul(y, y)
		base = []*big.Float{nf(0), nf(0), nf(0)}
		for i := range base {
			base[i].Add(xx[i], yy[i])
		}
		scale = nf(1)
	}
	for i := range base {
		base[i].Mul(base[i], scale)
	}
	poly := []*big.Float{nf(1)}
	for i := 0; i < m; i++ {
		poly = mul(poly, base)
	}
	lo, hi := nf(bp.Lo), nf(bp.Hi)
	sum := nf(0)
	hiPow, loPow := nf(1), nf(1)
	for j, c := range poly {
		hiPow.Mul(hiPow, hi)
		loPow.Mul(loPow, lo)
		d := new(big.Float).SetPrec(prec).Sub(hiPow, loPow)
		d.Mul(d, c)
		d.Quo(d, nf(float64(j+1)))
		sum.Add(sum, d)
	}
	out, _ := sum.Float64()
	return out
}

func relErr(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}

// ---- exactness on well-conditioned pieces -------------------------------------

// integralRelBound is the pass bar against the arbitrary-precision reference:
// a fixed relative error, not "no worse than the old formula", which would let
// a regression inside the old formula's own error pass. Gauss–Legendre with
// m+1 points is exact for the degree-2m integrand, so the only error is
// float64 rounding over a handful of operations — measured 2026-10-05 at
// worst 1.39e-15 on the cases below. The bound sits two orders above that and
// far below what a wrong rule gives (a one-point-short rule is off by 1.3e-2
// to 0.94 on the same cases).
const integralRelBound = 1e-13

// TestIntegrateDistSqPow_MatchesBigReference: the integral against the
// arbitrary-precision reference, for both forms the integrand takes (the span
// regime's linear form and a near regime's offset) at each power m the cost
// uses, plus the two degenerate widths.
//
// Red proof, 2026-10-05 (detached worktree): with the rule taken at m points
// instead of m+1, every power fails, first "span m=1: integral
// 0.39375000000000104, reference 6.8250000000000011 — relative error 0.942".
// The bar this replaced ("no worse than the old formula") would pass a
// regression that stayed inside the old formula's own error.
func TestIntegrateDistSqPow_MatchesBigReference(t *testing.T) {
	span := BandPiece{Lo: 0.1, Hi: 0.8, Kind: RegimeSpan, SpanC0: 1200, SpanC1: -3000, SpanLenSq: 4e4}
	near := nearPieceFromOffset(RegimeNearA, 0.2, 0.9, 12, -7, -30, 25)
	for _, m := range []int{1, 2, 3} {
		for _, c := range []struct {
			name string
			bp   BandPiece
		}{{"span", span}, {"near A", near}} {
			want := bigIntegrateDistSqPow(c.bp, m)
			got, err := integrateDistSqPow(c.bp, m)
			if err != nil {
				t.Fatalf("%s m=%d: %v", c.name, m, err)
			}
			e := relErr(got, want)
			t.Logf("%s m=%d: %.17g, reference %.17g, relative error %.3g", c.name, m, got, want, e)
			if !(want > 0) {
				t.Fatalf("%s m=%d: reference %v is not positive — the case integrates nothing", c.name, m, want)
			}
			if e > integralRelBound {
				t.Errorf("%s m=%d: integral %.17g, reference %.17g — relative error %.3g is above %.0e",
					c.name, m, got, want, e, integralRelBound)
			}
		}
	}

	// A zero-width piece (a tangency point) contributes nothing, and is not an
	// error.
	point := span
	point.Hi = point.Lo
	if got, err := integrateDistSqPow(point, 2); err != nil || got != 0 {
		t.Fatalf("hi == lo: got %v, %v; want 0 and no error", got, err)
	}
	// A reversed piece is an error, not a negative integral.
	reversed := span
	reversed.Lo, reversed.Hi = span.Hi, span.Lo
	if _, err := integrateDistSqPow(reversed, 2); err == nil || !strings.Contains(err.Error(), "< lo") {
		t.Fatalf("hi < lo: got err %v, want the reversed-piece error", err)
	}
}

// ---- the recorded failing pieces ------------------------------------------------

// negativeEmissionFixtures are real pieces on which the old integral came out
// negative: the 8 most negative of each regime among the 1,959 distinct pieces
// recorded on two real imports, 2026-09-24. Each is the (p, q, a, b, d) that bandPieces was called
// with, bit for bit, plus the clipped [lo, hi] of the corridor piece's regime
// and the emission the old formula gave it. kind is the BandRegime.
var negativeEmissionFixtures = []struct {
	kind       BandRegime
	lo, hi     uint64
	p, q, a, b [2]uint64 // Lat, Lon bits
	d          uint64
	oldE       float64
}{
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04e5a002696, 0xbffe2db37c99ae92}, b: [2]uint64{0x404ab04e3ffef391, 0xbffe2dc7d43f87d5}, d: 0x4024000000000000, oldE: -1.4331766249883562e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04e7ee9142b, 0xbffe2d9eef43f01a}, b: [2]uint64{0x404ab04e5a002696, 0xbffe2db37c99ae92}, d: 0x4024000000000000, oldE: -1.4329429831351659e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04eb56fb8f5, 0xbffe2d8ae825eea7}, b: [2]uint64{0x404ab04e7ee9142b, 0xbffe2d9eef43f01a}, d: 0x4024000000000000, oldE: -1.4326387541669534e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04ee89b5f6c, 0xbffe2d77b7c7820a}, b: [2]uint64{0x404ab04eb56fb8f5, 0xbffe2d8ae825eea7}, d: 0x4024000000000000, oldE: -1.4321670973830016e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04f10df4b55, 0xbffe2d64bd18faa3}, b: [2]uint64{0x404ab04ee89b5f6c, 0xbffe2d77b7c7820a}, d: 0x4024000000000000, oldE: -1.4315011340876974e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04f4d829dde, 0xbffe2d49593f0dd3}, b: [2]uint64{0x404ab04d0904fb2d, 0xbffe2cdda8cadff3}, d: 0x4024000000000000, oldE: -1.4302793817193897e-08},
	{kind: 0, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04f4d829dde, 0xbffe2d49593f0dd3}, b: [2]uint64{0x404ab04f39f9f6d2, 0xbffe2d5171e29b6b}, d: 0x4024000000000000, oldE: -1.4302771355933348e-08},
	{kind: 2, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04fc610f0e9, 0xbffe2d1a147e3c46}, b: [2]uint64{0x404ab04f97edc7ef, 0xbffe2d2b46d5a767}, d: 0x4024000000000000, oldE: -1.4285711221890979e-08},
	{kind: 2, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab04ff1afdb24, 0xbffe2d0a74ce0836}, b: [2]uint64{0x404ab04fc610f0e9, 0xbffe2d1a147e3c46}, d: 0x4024000000000000, oldE: -1.4274149085546306e-08},
	{kind: 2, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab0501fd3041e, 0xbffe2cf992fe74e6}, b: [2]uint64{0x404ab04ff1afdb24, 0xbffe2d0a74ce0836}, d: 0x4024000000000000, oldE: -1.4262447359817947e-08},
	{kind: 2, lo: 0x3fe7b5f870a6e3d4, hi: 0x3fe7b5f870a6e455, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab050dc9165ee, 0xbffe2cb6accfd743}, b: [2]uint64{0x404ab050af44fc89, 0xbffe2cc5c6484e4d}, d: 0x4024000000000000, oldE: -1.4197360108797978e-08},
	{kind: 2, lo: 0x3fe83785d833919d, hi: 0x3fe83785d8339220, p: [2]uint64{0x404ab05138d7b7e2, 0xbffda05773845c1b}, q: [2]uint64{0x404ab05139ae7777, 0xbffe5a95c853c148}, a: [2]uint64{0x404ab0505227eb01, 0xbffe2ce7da6f4cbf}, b: [2]uint64{0x404ab0501fd3041e, 0xbffe2cf992fe74e6}, d: 0x4024000000000000, oldE: -1.227712899984461e-08},
	{kind: 0, lo: 0x3fe86d5fed1284ef, hi: 0x3fe86d5fed128623, p: [2]uint64{0x404ab051dfc6a495, 0xbffda09fb2b84e91}, q: [2]uint64{0x404ab051e24ae353, 0xbffe588094e5d5b2}, a: [2]uint64{0x404ab0505227eb01, 0xbffe2ce7da6f4cbf}, b: [2]uint64{0x404ab0501fd3041e, 0xbffe2cf992fe74e6}, d: 0x4024000000000000, oldE: -1.1648772778039562e-08},
	{kind: 2, lo: 0x3fe86d5fed1284eb, hi: 0x3fe86d5fed12861f, p: [2]uint64{0x404ab051dfc6a495, 0xbffda09fb2b84e91}, q: [2]uint64{0x404ab051e24ae353, 0xbffe588094e5d5b2}, a: [2]uint64{0x404ab05154866a12, 0xbffe2c9b697d721c}, b: [2]uint64{0x404ab05113eeca4e, 0xbffe2ca9c70e46eb}, d: 0x4024000000000000, oldE: -1.1613424051441483e-08},
	{kind: 2, lo: 0x3fe86d5fed128500, hi: 0x3fe86d5fed128634, p: [2]uint64{0x404ab051dfc6a495, 0xbffda09fb2b84e91}, q: [2]uint64{0x404ab051e24ae353, 0xbffe588094e5d5b2}, a: [2]uint64{0x404ab052007dd441, 0xbffe2c80262b0cf4}, b: [2]uint64{0x404ab051a8694035, 0xbffe2c8bff7d2341}, d: 0x4024000000000000, oldE: -1.1582232697439355e-08},
	{kind: 2, lo: 0x3fe822ee8c352273, hi: 0x3fe822ee8c352321, p: [2]uint64{0x404ab05138d7b7e2, 0xbffda05773845c1b}, q: [2]uint64{0x404ab05139ae7777, 0xbffe5a95c853c148}, a: [2]uint64{0x404ab05113eeca4e, 0xbffe2ca9c70e46eb}, b: [2]uint64{0x404ab050dc9165ee, 0xbffe2cb6accfd743}, d: 0x4024000000000000, oldE: -1.156365279432589e-08},
	{kind: 1, lo: 0x3fe8560781878e95, hi: 0x3fe8560781996625, p: [2]uint64{0x404ab051dfc6a495, 0xbffda09fb2b84e91}, q: [2]uint64{0x404ab051e24ae353, 0xbffe588094e5d5b2}, a: [2]uint64{0x404ab052007dd441, 0xbffe2c80262b0cf4}, b: [2]uint64{0x404ab051a8694035, 0xbffe2c8bff7d2341}, d: 0x4024000000000000, oldE: -7.6384373571983076e-09},
	{kind: 1, lo: 0x3fe83371fe5bf73a, hi: 0x3fe83371fe6e6152, p: [2]uint64{0x404ab051ee095d77, 0xbffda0e36fa68109}, q: [2]uint64{0x404ab051f311daf4, 0xbffe59767903211d}, a: [2]uint64{0x404ab05252b32b3b, 0xbffe2c76d117b528}, b: [2]uint64{0x404ab052007dd441, 0xbffe2c80262b0cf4}, d: 0x4024000000000000, oldE: -5.3893611720681378e-09},
	{kind: 1, lo: 0x3fe5addd23924a95, hi: 0x3fe5addd239268ba, p: [2]uint64{0x404ab051ee095d77, 0xbffda0e36fa68109}, q: [2]uint64{0x404ab051f311daf4, 0xbffe59767903211d}, a: [2]uint64{0x404ab05305609211, 0xbffe1df86e3b46fe}, b: [2]uint64{0x404ab051a5e50177, 0xbffe1e33e2859bb6}, d: 0x4024000000000000, oldE: -5.0926325960945396e-09},
	{kind: 1, lo: 0x3fe2f886c164ebc0, hi: 0x3fe2f886c164ebc1, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab0504816f007, 0xbffe1131ec0b5675}, b: [2]uint64{0x404ab04fb97bb731, 0xbffe114e3bcd35a8}, d: 0x4024000000000000, oldE: -4.6600322452485244e-09},
	{kind: 1, lo: 0x3fe2f886c164ebc0, hi: 0x3fe2f886c164ebc1, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab050b524399b, 0xbffe111be6e65387}, b: [2]uint64{0x404ab0504816f007, 0xbffe1131ec0b5675}, d: 0x4024000000000000, oldE: -4.5853036251645285e-09},
	{kind: 1, lo: 0x3fe2f886c164ebc0, hi: 0x3fe2f886c164ebc1, p: [2]uint64{0x404ab04fd453a9cb, 0xbffda04a72ead927}, q: [2]uint64{0x404ab04fde64a4c5, 0xbffe5e93ef35ad61}, a: [2]uint64{0x404ab0504816f007, 0xbffe1131ec0b5675}, b: [2]uint64{0x404ab050b524399b, 0xbffe111be6e65387}, d: 0x4024000000000000, oldE: -4.585303625164526e-09},
	{kind: 1, lo: 0x3fe822ee8c352279, hi: 0x3fe822ee8c352327, p: [2]uint64{0x404ab05138d7b7e2, 0xbffda05773845c1b}, q: [2]uint64{0x404ab05139ae7777, 0xbffe5a95c853c148}, a: [2]uint64{0x404ab04f4d829dde, 0xbffe2d49593f0dd3}, b: [2]uint64{0x404ab04d0904fb2d, 0xbffe2cdda8cadff3}, d: 0x4024000000000000, oldE: -3.6264045653833674e-09},
	{kind: 1, lo: 0x3feff6a4dd19dd85, hi: 0x3feff6a5e8ccddc1, p: [2]uint64{0x404ab006a6e32e38, 0xbffd9a8262456f76}, q: [2]uint64{0x404ab006b7aa25d9, 0xbffe1bc33f85510d}, a: [2]uint64{0x404ab005b39e279e, 0xbffe1ba2be0589ad}, b: [2]uint64{0x404ab006b7aa25d9, 0xbffe1b9d7fd82774}, d: 0x4024000000000000, oldE: -3.5378846586424612e-09},
}

func fixturePoint(bits [2]uint64) Point {
	return Point{Lat: math.Float64frombits(bits[0]), Lon: math.Float64frombits(bits[1])}
}

// fixturePiece rebuilds the recorded piece through the production band solve.
func fixturePiece(t *testing.T, i int) BandPiece {
	t.Helper()
	f := negativeEmissionFixtures[i]
	pieces, n, _, _, err := bandPieces(fixturePoint(f.p), fixturePoint(f.q), fixturePoint(f.a), fixturePoint(f.b), math.Float64frombits(f.d))
	if err != nil {
		t.Fatalf("fixture %d: bandPieces: %v", i, err)
	}
	for _, bp := range pieces[:n] {
		if bp.Kind == f.kind {
			bp.Lo, bp.Hi = math.Float64frombits(f.lo), math.Float64frombits(f.hi)
			return bp
		}
	}
	t.Fatalf("fixture %d: bandPieces produced no piece of regime %d — the fixture no longer rebuilds", i, f.kind)
	return BandPiece{}
}

// TestIntegrateDistSqPow_RecordedFailingPiecesAreNonNegative is the defect's own
// regression test. On every recorded piece the new integral is ≥ 0 and agrees
// with the arbitrary-precision reference to within a few parts in 1e9 of the
// piece's own scale, where the old formula was negative.
//
// ⚠ ITS LIVENESS IS ASSERTED: the old formula must still be negative on each
// fixture. If a change to bandPieces stopped these pieces reproducing, this
// test would otherwise pass on pieces that no longer exercise the defect.
//
// Red proof, 2026-09-24 (detached worktree): with integrateDistSqPow put back
// to the power-rule form, this fails on fixture 0 with a negative integral.
func TestIntegrateDistSqPow_RecordedFailingPiecesAreNonNegative(t *testing.T) {
	if len(negativeEmissionFixtures) != 24 {
		t.Fatalf("%d fixtures, want 24 (8 per regime)", len(negativeEmissionFixtures))
	}
	regimes := map[BandRegime]int{}
	for i := range negativeEmissionFixtures {
		bp := fixturePiece(t, i)
		regimes[bp.Kind]++
		old := legacyIntegrateDistSqPow(bp, emissionK/2)
		if !(old < 0) {
			t.Fatalf("fixture %d: the old formula gives %v, not a negative — the fixture no longer witnesses the defect", i, old)
		}
		got, err := integrateDistSqPow(bp, emissionK/2)
		if err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
		if got < 0 {
			t.Fatalf("fixture %d (regime %d, t [%.17g, %.17g]): integral %v is negative (old formula %v)", i, bp.Kind, bp.Lo, bp.Hi, got, old)
		}
		want := bigIntegrateDistSqPow(bp, emissionK/2)
		// Absolute, scaled by the piece: the reference is itself tiny (a
		// near-zero width), so a relative error against it is not the question.
		// The question is whether the new value is as close to the truth as
		// the piece's own float inputs allow, where the old one was off by its
		// whole magnitude and more.
		scale := (bp.Hi - bp.Lo) * math.Max(1, math.Abs(want)/(bp.Hi-bp.Lo))
		if math.Abs(got-want) > 1e-9*scale {
			t.Errorf("fixture %d: integral %.17g, reference %.17g (old %.3g) — off by %.3g against a scale of %.3g", i, got, want, old, math.Abs(got-want), scale)
		}
	}
	if len(regimes) != 3 {
		t.Fatalf("fixtures cover regimes %v, want all three", regimes)
	}
}

// ---- a negative cost is an error where it is formed --------------------------------

// TestEmissionCost_NegativeIsAnError injects a negative emission — a piece whose
// import measure runs backwards against t, so its rate is negative — and checks
// that emissionCost refuses it rather than hand it to the solve. (ArcCost
// carries the same check on the sum it forms, but a backwards piece never
// reaches it: clipCorridorPiece refuses to clip one first.)
//
// Red proof, 2026-09-24 (detached worktree): with emissionCost's check removed,
// the cost comes back as a negative number and this fails.
func TestEmissionCost_NegativeIsAnError(t *testing.T) {
	bp := nearPieceFromOffset(RegimeNearA, 0.2, 0.6, 3, 1, 2, -1)
	cp := CorridorPiece{Regime: bp, ImportMeasureAtLo: 10, ImportMeasureAtHi: 6} // backwards
	_, err := emissionCost(cp, CorridorRadiusM, emissionK)
	if err == nil {
		t.Fatal("emissionCost accepted a piece whose emission is negative")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("emissionCost refused for another reason: %v", err)
	}
}
