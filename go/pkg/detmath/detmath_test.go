package detmath

import (
	"hash/fnv"
	"math"
	"math/rand"
	"runtime"
	"testing"
)

// detmathCase is one function under test and the inputs it is hashed over.
type detmathCase struct {
	name string
	det  func(x, y float64) float64
	// golden is the FNV-64a hash of the results' bits over detmathInputs. It
	// is the same on every architecture.
	golden uint64
}

var detmathCases = []detmathCase{
	{"Sin", func(x, _ float64) float64 { return Sin(x) }, 0x351b0ce3d9bc0cf4},
	{"Cos", func(x, _ float64) float64 { return Cos(x) }, 0x1a52ece1996213de},
	{"Atan", func(x, _ float64) float64 { return Atan(x) }, 0x9edf4bea7be0140e},
	{"Atan2", Atan2, 0x8e920f47f034758c},
	{"Hypot", Hypot, 0xac04430448c6035f},
}

// detmathInputs are seeded inputs: radians and ratios in [-8, 8], degrees
// scaled to radians as the geometry uses them, and a few huge arguments that
// take trigReduce.
func detmathInputs() [][2]float64 {
	r := rand.New(rand.NewSource(20261104))
	var in [][2]float64
	for range 500_000 {
		in = append(in, [2]float64{(r.Float64()*2 - 1) * 8, (r.Float64()*2 - 1) * 8})
	}
	for range 100_000 {
		lat := (r.Float64()*2 - 1) * 90
		in = append(in, [2]float64{lat * math.Pi / 180, (r.Float64()*2 - 1) * 1e-3})
	}
	for range 1000 {
		in = append(in, [2]float64{(r.Float64() + 1) * (1 << 40), r.Float64()})
	}
	return in
}

func hashOf(fn func(x, y float64) float64, in [][2]float64) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for _, p := range in {
		v := math.Float64bits(fn(p[0], p[1]))
		for k := range 8 {
			buf[k] = byte(v >> (8 * k))
		}
		h.Write(buf[:])
	}
	return h.Sum64()
}

// TestDetmathGolden pins each function's results over detmathInputs to one
// hash, the same on every architecture. The hashes were measured on
// linux/amd64 and darwin/arm64, which agreed. On amd64 they are also math's
// own hashes for every function here (math is pure Go there and nothing
// fuses), which shows detmath is math's algorithm. A bare x*y+z in detmath.go
// compiles to an FMA on arm64 and fails this there: with `float64(...)`
// removed from cosPoly's last line, Sin and Cos (both use it) fail on
// darwin/arm64.
func TestDetmathGolden(t *testing.T) {
	in := detmathInputs()
	for _, c := range detmathCases {
		if got := hashOf(c.det, in); got != c.golden {
			t.Errorf("%s on %s: hash %#016x, want %#016x — detmath no longer gives the same bits on every architecture",
				c.name, runtime.GOARCH, got, c.golden)
		}
	}
}

// TestDetmathSpecialValues: the edge cases math documents, unchanged.
func TestDetmathSpecialValues(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	checks := []struct {
		name      string
		got, want float64
	}{
		{"Sin(-0)", Sin(math.Copysign(0, -1)), math.Copysign(0, -1)},
		{"Sin(+Inf)", Sin(inf), nan},
		{"Cos(NaN)", Cos(nan), nan},
		{"Cos(0)", Cos(0), 1},
		{"Atan2(0, -1)", Atan2(0, -1), math.Pi},
		{"Atan2(1, 0)", Atan2(1, 0), math.Pi / 2},
		{"Atan2(+Inf, +Inf)", Atan2(inf, inf), math.Pi / 4},
		{"Hypot(+Inf, NaN)", Hypot(inf, nan), inf},
		{"Hypot(3, 4)", Hypot(3, 4), 5},
		{"Hypot(0, 0)", Hypot(0, 0), 0},
	}
	for _, c := range checks {
		if math.Float64bits(c.got) != math.Float64bits(c.want) && !(math.IsNaN(c.got) && math.IsNaN(c.want)) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
