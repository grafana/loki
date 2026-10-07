package decoder

import (
	"math"
	"math/bits"
)

//go:generate go run ../cmd/pow10table pow10_table.go

// pow10Entry is 10^q as a truncated 128-bit significand: ( hi*2^64 + lo ) * 2^exp2 <= 10^q, less than the
// next significand times 2^exp2.
type pow10Entry struct {
	hi, lo uint64
	exp2   int
}

// mulPow10 returns the float64 nearest to w * 10^q, rounded to even, and true; or false when that is not decided
// by the table, which the caller then leaves to strconv.
//
// The significand T of 10^q is truncated, so the exact product w * 10^q is in [w*T, w*T + w) times 2^exp2. Both
// ends are rounded to 53 bits: when they give the same float64, it is the one of every value between them, so it
// is the float64 of w * 10^q. Only a product very near a half-way point between two float64 values gives two,
// which is rare.
func mulPow10(w uint64, q int) (float64, bool) {
	if w == 0 {
		return 0, true
	}
	if q < minPow10Exp10 || q > maxPow10Exp10 {
		return 0, false
	}
	p := &pow10Table[q-minPow10Exp10]
	// the lower end: w * T, a number of 192 bits in p2, p1, p0.
	hi1, lo1 := bits.Mul64(w, p.lo)
	hi2, lo2 := bits.Mul64(w, p.hi)
	p0 := lo1
	p1, carry := bits.Add64(lo2, hi1, 0)
	p2 := hi2 + carry
	// the upper end: w * T + w.
	u0, c0 := bits.Add64(p0, w, 0)
	u1, c1 := bits.Add64(p1, 0, c0)
	u2 := p2 + c1
	m, e, ok := round192(p2, p1, p0)
	if !ok {
		return 0, false
	}
	mu, eu, ok := round192(u2, u1, u0)
	if !ok || mu != m || eu != e {
		return 0, false
	}
	// the value is m * 2^( e + exp2 ), and m has 53 bits.
	exp := e + p.exp2
	// a normal float64 is m * 2^exp with 53-bit m and exp from -1074 to 971.
	if exp < -1074 || exp > 971 {
		return 0, false
	}
	return math.Ldexp(float64(m), exp), true
}

// round192 rounds the 192-bit number x2*2^128 + x1*2^64 + x0, whose top word is not zero, to 53 bits, to even:
// it returns m and e such that the rounded number is m * 2^e with 2^52 <= m < 2^53.
func round192(x2, x1, x0 uint64) (uint64, int, bool) {
	if x2 == 0 {
		return 0, 0, false
	}
	lz := bits.LeadingZeros64(x2)
	// the top 64 bits of the number, and whether a bit below them is set.
	top := x2<<uint(lz) | x1>>uint(64-lz)
	if lz == 0 {
		top = x2
	}
	rest := x1<<uint(lz) | x0
	sticky := rest != 0 || top&(1<<10-1) != 0
	m := top >> 11
	half := top>>10&1 != 0
	if half && (sticky || m&1 != 0) {
		m++
		if m == 1<<53 {
			m >>= 1
			return m, 11 + 128 - lz + 1, true
		}
	}
	return m, 11 + 128 - lz, true
}
