// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file of the Go distribution.
//
// Modified: every product that feeds an addition or a subtraction is wrapped
// in an explicit float64 conversion.

// Package detmath gives the same float64 bits on every architecture for Sin,
// Cos, Atan, Atan2 and Hypot, the functions geometry code typically needs.
//
// Why it exists. Go's math package does not: measured over 2M seeded inputs,
// Cos, Sin, Atan2 and Hypot each differ between darwin/arm64 and linux/amd64
// (only Sqrt, one IEEE instruction on both, agrees). Two causes. (1) The
// compiler may fuse x*y+z into one FMA instruction; arm64 does it and amd64
// (GOAMD64=v1) cannot, so the pure-Go polynomials in math round differently.
// (2) math.Hypot is assembly on amd64 and Go elsewhere. A result that sits on
// an exact float boundary, or that two machines must agree on, then depends
// on the CPU.
//
// The fix is the Go spec's own rule: "An explicit floating-point type
// conversion rounds to the precision of the target type, preventing fusion
// that would discard that rounding." Each function here is the math package's
// pure-Go algorithm with that conversion on every product that meets an add
// or a subtract. So on amd64, where nothing fuses, Sin, Cos, Atan and Atan2
// equal math's bit for bit, and on arm64 they equal amd64's. Hypot is math's
// pure-Go algorithm, which amd64's assembly is not; it is the same on both.
//
// TestDetmathGolden pins every function to one hash on every architecture.
// When you edit this file, keep every product converted: a bare x*y+z
// compiles to an FMA on arm64 and the golden fails there.
package detmath

import (
	"math"
	"math/bits"
)

var _sin = [...]float64{
	1.58962301576546568060e-10, // 0x3de5d8fd1fd19ccd
	-2.50507477628578072866e-8, // 0xbe5ae5e5a9291f5d
	2.75573136213857245213e-6,  // 0x3ec71de3567d48a1
	-1.98412698295895385996e-4, // 0xbf2a01a019bfdf03
	8.33333333332211858878e-3,  // 0x3f8111111110f7d0
	-1.66666666666666307295e-1, // 0xbfc5555555555548
}

var _cos = [...]float64{
	-1.13585365213876817300e-11, // 0xbda8fa49a0861a9b
	2.08757008419747316778e-9,   // 0x3e21ee9d7b4e3f05
	-2.75573141792967388112e-7,  // 0xbe927e4f7eac4bc6
	2.48015872888517045348e-5,   // 0x3efa01a019c844f5
	-1.38888888888730564116e-3,  // 0xbf56c16c16c14f91
	4.16666666666665929218e-2,   // 0x3fa555555555554b
}

const (
	pi4A = 7.85398125648498535156e-1  // 0x3fe921fb40000000, Pi/4 split into three parts
	pi4B = 3.77489470793079817668e-8  // 0x3e64442d00000000,
	pi4C = 2.69515142907905952645e-15 // 0x3ce8469898cc5170,
)

// octant reduces x ≥ 0 to its octant j and the remainder z, as math does.
func octant(x float64) (j uint64, z float64) {
	if x >= reduceThreshold {
		return trigReduce(x)
	}
	j = uint64(x * (4 / math.Pi)) // integer part of x/(Pi/4)
	y := float64(j)
	if j&1 == 1 {
		j++
		y++
	}
	j &= 7
	z = ((x - float64(y*pi4A)) - float64(y*pi4B)) - float64(y*pi4C) // extended precision modular arithmetic
	return j, z
}

func sinPoly(z, zz float64) float64 {
	p := float64(_sin[0] * zz)
	p = float64((p+_sin[1])*zz) + _sin[2]
	p = float64(p*zz) + _sin[3]
	p = float64(p*zz) + _sin[4]
	p = float64(p*zz) + _sin[5]
	return z + float64(float64(z*zz)*p)
}

func cosPoly(zz float64) float64 {
	p := float64(_cos[0] * zz)
	p = float64((p+_cos[1])*zz) + _cos[2]
	p = float64(p*zz) + _cos[3]
	p = float64(p*zz) + _cos[4]
	p = float64(p*zz) + _cos[5]
	return 1.0 - float64(0.5*zz) + float64(float64(zz*zz)*p)
}

// Cos returns the cosine of the radian argument x.
func Cos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	sign := false
	x = math.Abs(x)
	j, z := octant(x)
	if j > 3 {
		j -= 4
		sign = !sign
	}
	if j > 1 {
		sign = !sign
	}
	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = sinPoly(z, zz)
	} else {
		y = cosPoly(zz)
	}
	if sign {
		y = -y
	}
	return y
}

// Sin returns the sine of the radian argument x.
func Sin(x float64) float64 {
	switch {
	case x == 0 || math.IsNaN(x):
		return x
	case math.IsInf(x, 0):
		return math.NaN()
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	j, z := octant(x)
	if j > 3 {
		sign = !sign
		j -= 4
	}
	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = cosPoly(zz)
	} else {
		y = sinPoly(z, zz)
	}
	if sign {
		y = -y
	}
	return y
}

const reduceThreshold = 1 << 29

const (
	mask  = 0x7FF
	shift = 64 - 11 - 1
	bias  = 1023
)

// trigReduce is math's Payne-Hanek reduction for huge arguments. It is
// integer arithmetic and needs no conversions.
func trigReduce(x float64) (j uint64, z float64) {
	const PI4 = math.Pi / 4
	if x < PI4 {
		return 0, x
	}
	ix := math.Float64bits(x)
	exp := int(ix>>shift&mask) - bias - shift
	ix &^= mask << shift
	ix |= 1 << shift
	digit, bitshift := uint(exp+61)/64, uint(exp+61)%64
	z0 := (mPi4[digit] << bitshift) | (mPi4[digit+1] >> (64 - bitshift))
	z1 := (mPi4[digit+1] << bitshift) | (mPi4[digit+2] >> (64 - bitshift))
	z2 := (mPi4[digit+2] << bitshift) | (mPi4[digit+3] >> (64 - bitshift))
	z2hi, _ := bits.Mul64(z2, ix)
	z1hi, z1lo := bits.Mul64(z1, ix)
	z0lo := z0 * ix
	lo, c := bits.Add64(z1lo, z2hi, 0)
	hi, _ := bits.Add64(z0lo, z1hi, c)
	j = hi >> 61
	hi = hi<<3 | lo>>61
	lz := uint(bits.LeadingZeros64(hi))
	e := uint64(bias - (lz + 1))
	hi = (hi << (lz + 1)) | (lo >> (64 - (lz + 1)))
	hi >>= 64 - shift
	hi |= e << shift
	z = math.Float64frombits(hi)
	if j&1 == 1 {
		j++
		j &= 7
		z--
	}
	return j, z * PI4
}

var mPi4 = [...]uint64{
	0x0000000000000001,
	0x45f306dc9c882a53,
	0xf84eafa3ea69bb81,
	0xb6c52b3278872083,
	0xfca2c757bd778ac3,
	0x6e48dc74849ba5c0,
	0x0c925dd413a32439,
	0xfc3bd63962534e7d,
	0xd1046bea5d768909,
	0xd338e04d68befc82,
	0x7323ac7306a673e9,
	0x3908bf177bf25076,
	0x3ff12fffbc0b301f,
	0xde5e2316b414da3e,
	0xda6cfd9e4f96136e,
	0x9e8c7ecd3cbfd45a,
	0xea4f758fd7cbe2f6,
	0x7a0e73ef14a525d4,
	0xd7f6bf623f1aba10,
	0xac06608df8f6d757,
}

func xatan(x float64) float64 {
	const (
		P0 = -8.750608600031904122785e-01
		P1 = -1.615753718733365076637e+01
		P2 = -7.500855792314704667340e+01
		P3 = -1.228866684490136173410e+02
		P4 = -6.485021904942025371773e+01
		Q0 = +2.485846490142306297962e+01
		Q1 = +1.650270098316988542046e+02
		Q2 = +4.328810604912902668951e+02
		Q3 = +4.853903996359136964868e+02
		Q4 = +1.945506571482613964425e+02
	)
	z := float64(x * x)
	num := float64(P0*z) + P1
	num = float64(num*z) + P2
	num = float64(num*z) + P3
	num = float64(num*z) + P4
	den := float64((z+Q0)*z) + Q1
	den = float64(den*z) + Q2
	den = float64(den*z) + Q3
	den = float64(den*z) + Q4
	z = z * num / den
	return float64(x*z) + x
}

func satan(x float64) float64 {
	const (
		Morebits = 6.123233995736765886130e-17 // pi/2 = PIO2 + Morebits
		Tan3pio8 = 2.41421356237309504880      // tan(3*pi/8)
	)
	if x <= 0.66 {
		return xatan(x)
	}
	if x > Tan3pio8 {
		return math.Pi/2 - xatan(1/x) + Morebits
	}
	return math.Pi/4 + xatan((x-1)/(x+1)) + 0.5*Morebits
}

// Atan returns the arctangent, in radians, of x.
func Atan(x float64) float64 {
	if x == 0 {
		return x
	}
	if x > 0 {
		return satan(x)
	}
	return -satan(-x)
}

// Atan2 returns the arc tangent of y/x, using the signs of the two to
// determine the quadrant of the return value.
func Atan2(y, x float64) float64 {
	switch {
	case math.IsNaN(y) || math.IsNaN(x):
		return math.NaN()
	case y == 0:
		if x >= 0 && !math.Signbit(x) {
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case x == 0:
		return math.Copysign(math.Pi/2, y)
	case math.IsInf(x, 0):
		if math.IsInf(x, 1) {
			if math.IsInf(y, 0) {
				return math.Copysign(math.Pi/4, y)
			}
			return math.Copysign(0, y)
		}
		if math.IsInf(y, 0) {
			return math.Copysign(3*math.Pi/4, y)
		}
		return math.Copysign(math.Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(math.Pi/2, y)
	}
	q := Atan(y / x)
	if x < 0 {
		if q <= 0 {
			return q + math.Pi
		}
		return q - math.Pi
	}
	return q
}

// Hypot returns sqrt(p*p + q*q), taking care to avoid overflow. It is math's
// pure-Go algorithm, not amd64's assembly.
func Hypot(p, q float64) float64 {
	p, q = math.Abs(p), math.Abs(q)
	switch {
	case math.IsInf(p, 1) || math.IsInf(q, 1):
		return math.Inf(1)
	case math.IsNaN(p) || math.IsNaN(q):
		return math.NaN()
	}
	if p < q {
		p, q = q, p
	}
	if p == 0 {
		return 0
	}
	q = q / p
	return p * math.Sqrt(1+float64(q*q))
}
