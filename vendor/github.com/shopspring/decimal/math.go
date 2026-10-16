package decimal

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"sync"
)

var factorials = []Decimal{New(1, 0)}
var factorialsMutex sync.RWMutex

// Pow returns d to the power of d2.
// The result is exact for non-negative integer exponents. For negative or non-integer exponents it is
// rounded half away from zero to PowPrecisionNegativeExponent places after the decimal point. A result
// that would round to 0 keeps PowPrecisionNegativeExponent significant digits instead (at least one).
//
// Pow returns 0 (zero-value of Decimal) instead of error for power operation edge cases, to handle those edge cases use PowWithPrecision.
// Edge cases not handled by Pow:
//   - 0 ** 0 => undefined value
//   - 0 ** y, where y < 0 => infinity
//   - x ** y, where x < 0 and y is non-integer decimal => imaginary value
//   - x ** y, where the magnitude of the result or the precision is too large to represent
//
// Example:
//
//	d1 := decimal.NewFromFloat(4.0)
//	d2 := decimal.NewFromFloat(4.0)
//	res1 := d1.Pow(d2)
//	res1.String() // output: "256"
//
//	d3 := decimal.NewFromFloat(5.0)
//	d4 := decimal.NewFromFloat(5.73)
//	res2 := d3.Pow(d4)
//	res2.String() // output: "10118.0803715950193171"
//
//	d5 := decimal.NewFromInt(10)
//	d6 := decimal.NewFromInt(-18)
//	res3 := d5.Pow(d6)
//	res3.String() // output: "0.000000000000000001"
func (d Decimal) Pow(d2 Decimal) Decimal {
	res, err := d.PowWithPrecision(d2, int32(PowPrecisionNegativeExponent))
	if err != nil {
		return Decimal{}
	}
	return res
}

// PowWithPrecision returns d to the power of d2.
// The result is exact for non-negative integer exponents. For negative or non-integer exponents it is
// rounded half away from zero to precision places after the decimal point. A result that would round
// to 0 keeps precision significant digits instead (at least one).
//
// PowWithPrecision returns error when:
//   - 0 ** 0 => undefined value
//   - 0 ** y, where y < 0 => infinity
//   - x ** y, where x < 0 and y is non-integer decimal => imaginary value
//   - x ** y, where the magnitude of the result or the precision is too large to represent
//
// Example:
//
//	d1 := decimal.NewFromFloat(4.0)
//	d2 := decimal.NewFromFloat(4.0)
//	res1, err := d1.PowWithPrecision(d2, 2)
//	res1.String() // output: "256"
//
//	d3 := decimal.NewFromFloat(5.0)
//	d4 := decimal.NewFromFloat(5.73)
//	res2, err := d3.PowWithPrecision(d4, 5)
//	res2.String() // output: "10118.08037"
//
//	d5 := decimal.NewFromFloat(-3.0)
//	d6 := decimal.NewFromFloat(-6.0)
//	res3, err := d5.PowWithPrecision(d6, 10)
//	res3.String() // output: "0.0013717421"
func (d Decimal) PowWithPrecision(d2 Decimal, precision int32) (Decimal, error) {
	if d.IsZero() {
		if d2.IsZero() {
			return Decimal{}, fmt.Errorf("cannot represent undefined value of 0**0")
		}
		if d2.Sign() < 0 {
			return Decimal{}, fmt.Errorf("cannot represent infinity value of 0 ** y, where y < 0")
		}
		return Decimal{zeroInt, 0}, nil
	}

	if d2.IsInteger() {
		return d.powBigIntWithPrecision(d2.BigInt(), precision)
	}

	if d.Sign() < 0 {
		return Decimal{}, fmt.Errorf("cannot represent imaginary value of x ** y, where x < 0 and y is non-integer decimal")
	}

	return d.powFrac(d2, precision)
}

// PowInt32 returns d to the power of exp, where exp is int32.
// Returns error for 0 ** 0, 0 ** exp where exp < 0, and results whose magnitude is too large to represent.
//
// For negative exponents the result is rounded half away from zero to PowPrecisionNegativeExponent places
// after the decimal point. A result that would round to 0 keeps PowPrecisionNegativeExponent significant
// digits instead (at least one).
//
// Example:
//
//	d1, err := decimal.NewFromFloat(4.0).PowInt32(4)
//	d1.String() // output: "256"
//
//	d2, err := decimal.NewFromFloat(3.13).PowInt32(5)
//	d2.String() // output: "300.4150512793"
func (d Decimal) PowInt32(exp int32) (Decimal, error) {
	return d.PowBigInt(big.NewInt(int64(exp)))
}

// PowBigInt returns d to the power of exp, where exp is big.Int.
// Returns error for 0 ** 0, 0 ** exp where exp < 0, and results whose magnitude is too large to represent.
//
// For negative exponents the result is rounded half away from zero to PowPrecisionNegativeExponent places
// after the decimal point. A result that would round to 0 keeps PowPrecisionNegativeExponent significant
// digits instead (at least one).
//
// Example:
//
//	d1, err := decimal.NewFromFloat(3.0).PowBigInt(big.NewInt(3))
//	d1.String() // output: "27"
//
//	d2, err := decimal.NewFromFloat(629.25).PowBigInt(big.NewInt(5))
//	d2.String() // output: "98654323103449.5673828125"
func (d Decimal) PowBigInt(exp *big.Int) (Decimal, error) {
	return d.powBigIntWithPrecision(exp, int32(PowPrecisionNegativeExponent))
}

var errPowOutOfRange = fmt.Errorf("cannot represent result of power operation, its magnitude or precision is too large")

// powExactBits is the largest estimated cost of computing d^n exactly for negative n.
// Bigger powers are approximated with truncated intermediates, which is cheaper.
const powExactBits = 3000

func (d Decimal) powBigIntWithPrecision(exp *big.Int, precision int32) (Decimal, error) {
	switch {
	case exp.Sign() > 0:
		return d.powExact(exp)
	case d.IsZero() && exp.Sign() == 0:
		return Decimal{}, fmt.Errorf("cannot represent undefined value of 0**0")
	case exp.Sign() == 0:
		return Decimal{oneInt, 0}, nil
	case d.IsZero():
		return Decimal{}, fmt.Errorf("cannot represent infinity value of 0 ** y, where y < 0")
	}
	return d.powNegInt(new(big.Int).Neg(exp), precision)
}

// powExact returns d^n for n > 0.
func (d Decimal) powExact(n *big.Int) (Decimal, error) {
	exp := new(big.Int).Mul(n, big.NewInt(int64(d.exp)))
	if !exp.IsInt64() || exp.Int64() > math.MaxInt32 || exp.Int64() < math.MinInt32 {
		return Decimal{}, errPowOutOfRange
	}
	return Decimal{new(big.Int).Exp(d.getValue(), n, nil), int32(exp.Int64())}, nil
}

// powNegInt returns d^-n for n > 0, rounded as described in PowWithPrecision.
func (d Decimal) powNegInt(n *big.Int, precision int32) (Decimal, error) {
	if d.absIsOne() {
		// only the parity of n matters
		n = big.NewInt(2 - int64(n.Bit(0)))
	}

	// small powers are cheap to compute exactly, and |log10(d^-n)| <= cost keeps them in range
	if n.IsInt64() && n.Int64() <= powExactBits {
		cost := n.Int64() * (int64(d.getValue().BitLen()) + abs64(int64(d.exp)))
		if cost <= powExactBits && powInRange(float64(cost), precision) {
			return d.powNegIntExact(n, precision)
		}
	}

	nf, _ := new(big.Float).SetInt(n).Float64()
	lg := -nf * d.log10Abs()
	if !powInRange(lg, precision) {
		return Decimal{}, errPowOutOfRange
	}

	// the digit shifts of the truncated c^n must fit into int64
	if nd := int64(d.NumDigits()); n.IsInt64() && n.Int64() <= math.MaxInt64/(4*nd) {
		c := new(big.Int).Abs(d.getValue())
		w := powSigDigits(lg, precision) + int64(len(n.String())) + 6
		// each try adds 20 digits, failing 5 times means an exact tie or a value extremely close to one
		for i := 0; i < 5; i, w = i+1, w+20 {
			if res, ok := powNegIntApprox(c, int64(d.exp), n, w, precision); ok {
				if d.Sign() < 0 && n.Bit(0) == 1 {
					res.value.Neg(res.value)
				}
				return res, nil
			}
		}
	}

	// same as for non-integer exponents, which handles any n and exact ties
	res, err := d.Abs().powFrac(Decimal{value: new(big.Int).Neg(n)}, precision)
	if err == nil && d.Sign() < 0 && n.Bit(0) == 1 {
		res.value.Neg(res.value)
	}
	return res, err
}

// powNegIntExact returns d^-n for n > 0, dividing by the exact d^n.
func (d Decimal) powNegIntExact(n *big.Int, precision int32) (Decimal, error) {
	x, err := d.powExact(n)
	if err != nil {
		return Decimal{}, err
	}
	res := New(1, 0).DivRound(x, precision)
	if res.IsZero() {
		// first significant digit of 1/x is at place NumDigits+exp, one earlier when x is a power of ten
		nd := int32(x.NumDigits())
		places := int32(powMinSig(precision)) - 1 + nd + x.exp
		if x.getValue().CmpAbs(pow10(int64(nd)-1)) == 0 {
			places--
		}
		res = New(1, 0).DivRound(x, places)
	}
	return res, nil
}

// powNegIntApprox rounds |1 / (c^n * 10^(e*n))| as described in PowWithPrecision. c^n is approximated
// from below by m*10^s, truncating m to w digits after every multiplication. Each truncation shrinks
// the result by a factor of at most 1-10^(1-w), and the errors add up to less than 5n*10^(1-w),
// lo below allows ten times that.
func powNegIntApprox(c *big.Int, e int64, n *big.Int, w int64, precision int32) (Decimal, bool) {
	cw := new(big.Int).Set(c)
	cs, exact := truncDigits(cw, w)

	m, s := big.NewInt(1), int64(0)
	for i := n.BitLen() - 1; i >= 0; i-- {
		m.Mul(m, m)
		ds, ex := truncDigits(m, w)
		s, exact = 2*s+ds, exact && ex
		if n.Bit(i) == 1 {
			m.Mul(m, cw)
			ds, ex = truncDigits(m, w)
			s, exact = s+cs+ds, exact && ex
		}
	}

	// 10^scale / (m * 10^(s+e*n)) = 10^z / m
	z := int64(Decimal{value: m}.NumDigits()) + w + 2
	scale := z + s + e*n.Int64()
	q, rem := new(big.Int).QuoRem(pow10(z), m, new(big.Int))
	lo, hi := q, q
	if !exact || rem.Sign() != 0 {
		hi = new(big.Int).Add(q, oneInt)
	}
	if !exact {
		lo = new(big.Int).Mul(q, big.NewInt(5))
		lo.Mul(lo, n).Quo(lo, pow10(w-2))
		lo.Sub(q, lo).Sub(lo, oneInt)
	}
	return roundBounds(lo, hi, scale, precision)
}

// truncDigits truncates m > 0 to at least w digits, returning the number of dropped digits
// and whether they were all zeros.
func truncDigits(m *big.Int, w int64) (int64, bool) {
	// m >= 2^(BitLen-1), so m has at least floor((BitLen-1)*log10(2))+1 digits
	drop := int64(float64(m.BitLen()-1)*math.Log10(2)-1e-9) + 1 - w
	if drop <= 0 {
		return 0, true
	}
	var r big.Int
	m.QuoRem(m, pow10(drop), &r)
	return drop, r.Sign() == 0
}

// powFrac returns d^y for d > 0 and non-integer (or negative integer) y, rounded as described in
// PowWithPrecision. It evaluates d^y = 10^q * e^s, where y*ln(d) = q*ln(10) + s and 0 <= s < ln(10),
// in binary fixed-point arithmetic, adding working digits until the error bounds decide the rounding.
func (d Decimal) powFrac(y Decimal, precision int32) (Decimal, error) {
	if d.absIsOne() {
		// 1^y = 1, also for y beyond float64 range
		if !powInRange(0, precision) {
			return Decimal{}, errPowOutOfRange
		}
		k := int64(abs(precision)) + 2
		res, _ := roundBounds(pow10(k), pow10(k), k, precision)
		return res, nil
	}

	yf := y.InexactFloat64()
	lg := yf * d.log10Abs()
	if !powInRange(lg, precision) {
		return Decimal{}, errPowOutOfRange
	}

	k := int64(d.NumDigits()) - 1 + int64(d.exp)
	// covers the working error amplified by |y| and |k|, see powFracFixed
	extra := int64(math.Log10(math.Abs(yf)*(math.Abs(float64(k))+64)+math.Abs(lg)+1)) + 4
	// 9 guard digits make a retry unlikely, each retry adds 20 more
	f := powSigDigits(lg, precision) + 9
	exactTie := false
	for i := 0; ; i, f = i+1, f+20 {
		if res, ok := powFracFixed(d, y, k, f, extra, precision, exactTie); ok {
			return res, nil
		}
		// only a terminating result can sit exactly on a rounding boundary, which no working
		// precision resolves, then the upper bound rounds it away from zero
		if i == 8 {
			exactTie = powIsTerminating(d, y)
		}
	}
}

// powIsTerminating reports whether d^y is a terminating decimal, for d > 0.
func powIsTerminating(d, y Decimal) bool {
	p, q := powRatio(y)
	num, den := powRatio(d)
	if !q.IsInt64() {
		return false
	}
	// d^(p/q) is rational only when num and den are perfect q-th powers
	rn, ok := iroot(num, q.Int64())
	if !ok {
		return false
	}
	rd, ok := iroot(den, q.Int64())
	if !ok {
		return false
	}
	// (rn/rd)^p terminates when its denominator has no prime factors other than 2 and 5
	if p.Sign() < 0 {
		rd = rn
	}
	rd = new(big.Int).Set(rd)
	for _, f := range []*big.Int{twoInt, fiveInt} {
		for r := new(big.Int); rd.Cmp(oneInt) > 0 && r.Rem(rd, f).Sign() == 0; {
			rd.Quo(rd, f)
		}
	}
	return rd.Cmp(oneInt) == 0
}

// powRatio returns d as a fraction p/q in lowest terms, q > 0.
func powRatio(d Decimal) (*big.Int, *big.Int) {
	if d.exp >= 0 {
		return new(big.Int).Mul(d.getValue(), pow10(int64(d.exp))), big.NewInt(1)
	}
	p, q := new(big.Int).Set(d.getValue()), new(big.Int).Set(pow10(-int64(d.exp)))
	g := new(big.Int).GCD(nil, nil, new(big.Int).Abs(p), q)
	return p.Quo(p, g), q.Quo(q, g)
}

// iroot returns the integer k-th root of x >= 1 and whether it is exact.
func iroot(x *big.Int, k int64) (*big.Int, bool) {
	if k == 1 || x.Cmp(oneInt) == 0 {
		return x, true
	}
	if int64(x.BitLen()) <= k {
		// 1 < x < 2^k has no integer k-th root
		return nil, false
	}
	// Newton's method from above converges to the floor of the root
	kb, km1 := big.NewInt(k), big.NewInt(k-1)
	r := new(big.Int).Lsh(oneInt, uint((int64(x.BitLen())+k-1)/k))
	t, u := new(big.Int), new(big.Int)
	for {
		t.Quo(x, t.Exp(r, km1, nil))
		t.Add(t, u.Mul(r, km1)).Quo(t, kb)
		if t.Cmp(r) >= 0 {
			break
		}
		r.Set(t)
	}
	return r, t.Exp(r, kb, nil).Cmp(x) == 0
}

// powFracFixed computes d^y (see powFrac) as an interval of e^s * 10^f with f+extra working digits
// and rounds it. With force the upper bound is used as the exact value.
func powFracFixed(d, y Decimal, k, f, extra int64, precision int32, force bool) (Decimal, bool) {
	b := uint(float64(f+extra)*math.Log2(10)) + 1

	// d = m * 10^k, 1 <= m < 10
	m := new(big.Int).Lsh(d.getValue(), b)
	m.Quo(m, pow10(int64(d.NumDigits())-1))
	t, lnErr := lnFixed(m, b)
	ln10, ln10Err := ln10Fixed(b)
	t.Add(t, new(big.Int).Mul(big.NewInt(k), ln10))
	tErr := big.NewInt(lnErr + 1 + abs64(k)*ln10Err)

	// t = y * ln(d)
	t.Mul(t, y.getValue())
	if y.exp >= 0 {
		t.Mul(t, pow10(int64(y.exp)))
	} else {
		t.Quo(t, pow10(-int64(y.exp)))
	}
	tErr.Mul(tErr, y.Abs().Ceil().getValue()).Add(tErr, oneInt)

	q, s := new(big.Int).DivMod(t, ln10, new(big.Int))
	sErr := new(big.Int).Abs(q)
	sErr.Mul(sErr, big.NewInt(ln10Err)).Add(sErr, tErr)

	// e^s < 11, so an error in s grows at most 11 times
	r, rErr := expFixed(s, b)
	sErr.Mul(sErr, big.NewInt(11)).Add(sErr, big.NewInt(rErr+1))

	// v * 10^(f-q) = e^s * 10^f
	p := pow10(f)
	hi := new(big.Int).Add(r, sErr)
	hi.Mul(hi, p).Rsh(hi, b).Add(hi, oneInt)
	lo := hi
	if !force {
		lo = new(big.Int).Sub(r, sErr)
		lo.Mul(lo, p).Rsh(lo, b)
		if lo.Sign() < 0 {
			lo.SetInt64(0)
		}
	}
	return roundBounds(lo, hi, f-q.Int64(), precision)
}

// lnFixed returns ln(m / 2^b) * 2^b for m / 2^b in [1, 10], with its error bound in ulps.
func lnFixed(m *big.Int, b uint) (*big.Int, int64) {
	// a = math.Log(m) is accurate to ~1e-15, so ln(m) = a + ln(1+z), where z = m*e^-a - 1 is tiny
	// and the series ln(1+z) = z - z^2/2 + z^3/3 - ... gains ~50 bits per term.
	top, sh := new(big.Int), m.BitLen()-60
	if sh > 0 {
		top.Rsh(m, uint(sh))
	} else {
		top.Lsh(m, uint(-sh))
	}
	a := big.NewInt(int64(math.Ldexp(math.Log(math.Ldexp(float64(top.Uint64()), sh-int(b))), 52)))
	if b >= 52 {
		a.Lsh(a, b-52)
	} else {
		a.Rsh(a, 52-b)
	}

	e, eErr := expFixed(new(big.Int).Neg(a), b)
	z := e.Mul(e, m).Rsh(e, b)
	z.Sub(z, new(big.Int).Lsh(oneInt, b))

	sum, p, term, iv := new(big.Int).Set(z), new(big.Int).Set(z), new(big.Int), new(big.Int)
	terms := int64(1)
	for i := int64(2); ; i++ {
		p.Mul(p, z).Rsh(p, b)
		if p.Sign() == 0 {
			break
		}
		term.Quo(p, iv.SetInt64(i))
		if i%2 == 0 {
			sum.Sub(sum, term)
		} else {
			sum.Add(sum, term)
		}
		terms++
	}
	// m <= 10 amplifies the error of e^-a at most 10 times
	return sum.Add(sum, a), 10*eErr + 2*terms + 3
}

// expFixed returns e^(x / 2^b) * 2^b for |x / 2^b| <= 2.4, with its error bound in ulps.
func expFixed(x *big.Int, b uint) (*big.Int, int64) {
	// e^x = (e^(x/2^j))^(2^j): Taylor series of the reduced argument, then j squarings.
	// Guard bits cover the error of up to ~g Taylor terms, amplified up to 11*2^j times by squaring.
	j := uint(math.Sqrt(float64(b))) + 1
	g := b + j + uint(bits.Len(33*(b+j+64)+363)) + 2

	xr := new(big.Int).Lsh(x, g-b)
	xr.Rsh(xr, j)
	sum, term, iv := new(big.Int).Lsh(oneInt, g), new(big.Int).Lsh(oneInt, g), new(big.Int)
	for i := int64(1); ; i++ {
		term.Mul(term, xr).Rsh(term, g)
		term.Quo(term, iv.SetInt64(i))
		if term.Sign() == 0 {
			break
		}
		sum.Add(sum, term)
	}
	for ; j > 0; j-- {
		sum.Mul(sum, sum).Rsh(sum, g)
	}
	return sum.Rsh(sum, g-b), 2
}

var ln10Cache struct {
	once sync.Once
	v    *big.Int
}

const ln10CacheBits = 2048

// ln10Fixed returns ln(10) * 2^b, with its error bound in ulps.
func ln10Fixed(b uint) (*big.Int, int64) {
	if b <= ln10CacheBits {
		ln10Cache.once.Do(func() { ln10Cache.v, _ = ln10FromDigits(ln10CacheBits) })
		return new(big.Int).Rsh(ln10Cache.v, ln10CacheBits-b), 3
	}
	if v, ok := ln10FromDigits(b); ok {
		return v, 2
	}
	return lnFixed(new(big.Int).Lsh(big.NewInt(10), b), b)
}

// ln10FromDigits returns ln(10) * 2^b computed from strLn10, if it has enough digits.
func ln10FromDigits(b uint) (*big.Int, bool) {
	digits := int(float64(b)*math.Log10(2)) + 3
	if digits+2 > len(strLn10) {
		return nil, false
	}
	v, _ := new(big.Int).SetString(strLn10[:1]+strLn10[2:digits+2], 10)
	v.Lsh(v, b)
	return v.Quo(v, pow10(int64(digits))), true
}

// roundBounds rounds v, known only by lo <= v*10^scale <= hi (lo >= 0), half away from zero to
// precision places after the decimal point, or to max(precision, 1) significant digits when that
// would be 0. It reports false when lo and hi do not round to the same value.
func roundBounds(lo, hi *big.Int, scale int64, precision int32) (Decimal, bool) {
	g := scale - int64(precision)
	if g < 1 {
		return Decimal{}, false
	}
	dhi := int64(Decimal{value: hi}.NumDigits())
	// with fewer than g digits hi rounds to 0, skip building 10^g for tiny results
	if g <= dhi {
		if q := roundHalfUp(hi, g); q.Sign() != 0 {
			return Decimal{q, -precision}, q.Cmp(roundHalfUp(lo, g)) == 0
		}
	}

	sig := powMinSig(precision)
	g = dhi - sig
	if g < 2 {
		return Decimal{}, false
	}
	q := roundHalfUp(hi, g)
	res := Decimal{q, int32(g - scale)}
	if int64(Decimal{value: lo}.NumDigits()) == dhi {
		return res, q.Cmp(roundHalfUp(lo, g)) == 0
	}
	// lo < 10^(dhi-1) <= hi, decided only when both round to that power of ten
	return res, q.Cmp(pow10(sig-1)) == 0 && roundHalfUp(lo, g-1).Cmp(pow10(sig)) == 0
}

// roundHalfUp returns x / 10^g rounded half away from zero, for x >= 0 and g >= 1.
func roundHalfUp(x *big.Int, g int64) *big.Int {
	p := pow10(g)
	q, r := new(big.Int).QuoRem(x, p, new(big.Int))
	if r.Lsh(r, 1).Cmp(p) >= 0 {
		q.Add(q, oneInt)
	}
	return q
}

// powSigDigits returns the number of significant digits of a result of magnitude 10^lg rounded to
// precision places, at least powMinSig(precision).
func powSigDigits(lg float64, precision int32) int64 {
	if n := int64(math.Floor(lg)) + 1 + int64(precision); n > powMinSig(precision) {
		return n
	}
	return powMinSig(precision)
}

// powMinSig returns the significant digits kept by results that would round to zero.
func powMinSig(precision int32) int64 {
	if precision < 1 {
		return 1
	}
	return int64(precision)
}

// powInRange reports whether a result of magnitude 10^lg rounded to precision places has an exponent
// that fits into int32.
func powInRange(lg float64, precision int32) bool {
	return math.Abs(lg)+math.Abs(float64(precision)) < math.MaxInt32/2
}

// absIsOne reports whether |d| = 1.
func (d Decimal) absIsOne() bool {
	nd := int64(d.NumDigits())
	return nd-1+int64(d.exp) == 0 && d.getValue().CmpAbs(pow10(nd-1)) == 0
}

// log10Abs approximates log10(|d|) for d != 0.
func (d Decimal) log10Abs() float64 {
	var m big.Float
	e2 := new(big.Float).SetInt(d.getValue()).MantExp(&m)
	mf, _ := m.Float64()
	return math.Log10(math.Abs(mf)) + float64(e2)*math.Log10(2) + float64(d.exp)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ExpHullAbrham calculates the natural exponent of decimal (e to the power of d) using Hull-Abraham algorithm.
// OverallPrecision argument specifies the overall precision of the result (integer part + decimal part).
//
// ExpHullAbrham is faster than ExpTaylor for small precision values, but it is much slower for large precision values.
//
// Example:
//
//	NewFromFloat(26.1).ExpHullAbrham(2).String()    // output: "220000000000"
//	NewFromFloat(26.1).ExpHullAbrham(20).String()   // output: "216314672147.05767284"
func (d Decimal) ExpHullAbrham(overallPrecision uint32) (Decimal, error) {
	// Algorithm based on Variable precision exponential function.
	// ACM Transactions on Mathematical Software by T. E. Hull & A. Abrham.
	if d.IsZero() {
		return Decimal{oneInt, 0}, nil
	}

	currentPrecision := overallPrecision

	// Algorithm does not work if currentPrecision * 23 < |x|.
	// Precision is automatically increased in such cases, so the value can be calculated precisely.
	// If newly calculated precision is higher than ExpMaxIterations the currentPrecision will not be changed.
	f := d.Abs().InexactFloat64()
	if ncp := f / 23; ncp > float64(currentPrecision) && ncp < float64(ExpMaxIterations) {
		currentPrecision = uint32(math.Ceil(ncp))
	}

	// fail if abs(d) beyond an over/underflow threshold
	overflowThreshold := New(23*int64(currentPrecision), 0)
	if d.Abs().Cmp(overflowThreshold) > 0 {
		return Decimal{}, fmt.Errorf("over/underflow threshold, exp(x) cannot be calculated precisely")
	}

	// Return 1 if abs(d) small enough; this also avoids later over/underflow
	overflowThreshold2 := New(9, -int32(currentPrecision)-1)
	if d.Abs().Cmp(overflowThreshold2) <= 0 {
		return Decimal{oneInt, 0}, nil
	}

	// t is the smallest integer >= 0 such that the corresponding abs(d/k) < 1
	t := d.exp + int32(d.NumDigits()) // Add d.NumDigits because the paper assumes that d.value [0.1, 1)

	if t < 0 {
		t = 0
	}

	k := New(1, t)                                          // reduction factor
	r := Decimal{new(big.Int).Set(d.getValue()), d.exp - t} // reduced argument
	p := int32(currentPrecision) + t + 2                    // precision for calculating the sum

	// Determine n, the number of therms for calculating sum
	// use first Newton step (1.435p - 1.182) / log10(p/abs(r))
	// for solving appropriate equation, along with directed
	// roundings and simple rational bound for log10(p/abs(r))
	rf := r.Abs().InexactFloat64()
	pf := float64(p)
	nf := math.Ceil((1.453*pf - 1.182) / math.Log10(pf/rf))
	if nf > float64(ExpMaxIterations) || math.IsNaN(nf) {
		return Decimal{}, fmt.Errorf("exact value cannot be calculated in <=ExpMaxIterations iterations")
	}
	n := int64(nf)

	tmp := New(0, 0)
	sum := New(1, 0)
	one := New(1, 0)
	for i := n - 1; i > 0; i-- {
		tmp.value.SetInt64(i)
		sum = sum.Mul(r.DivRound(tmp, p))
		sum = sum.Add(one)
	}

	// res = sum^ki, the same value ki repeated multiplications would give
	ki := k.IntPart()
	expInt64 := int64(sum.exp) * ki
	if expInt64 > math.MaxInt32 || expInt64 < math.MinInt32 {
		panic(fmt.Sprintf("exponent %v overflows an int32!", expInt64))
	}
	res := Decimal{new(big.Int).Exp(sum.getValue(), big.NewInt(ki), nil), int32(expInt64)}

	resNumDigits := int32(res.NumDigits())

	var roundDigits int32
	if resNumDigits > abs(res.exp) {
		roundDigits = int32(currentPrecision) - resNumDigits - res.exp
	} else {
		roundDigits = int32(currentPrecision)
	}

	res = res.Round(roundDigits)

	return res, nil
}

// ExpTaylor calculates the natural exponent of decimal (e to the power of d) using Taylor series expansion.
// Precision argument specifies how precise the result must be (number of digits after decimal point).
// Negative precision is allowed.
//
// ExpTaylor is much faster for large precision values than ExpHullAbrham.
//
// Example:
//
//	d, err := NewFromFloat(26.1).ExpTaylor(2).String()
//	d.String()  // output: "216314672147.06"
//
//	NewFromFloat(26.1).ExpTaylor(20).String()
//	d.String()  // output: "216314672147.05767284062928674083"
//
//	NewFromFloat(26.1).ExpTaylor(-10).String()
//	d.String()  // output: "220000000000"
func (d Decimal) ExpTaylor(precision int32) (Decimal, error) {
	// Note(mwoss): Implementation can be optimized by exclusively using big.Int API only
	if d.IsZero() {
		return Decimal{oneInt, 0}.Round(precision), nil
	}

	var epsilon Decimal
	var divPrecision int32
	if precision < 0 {
		epsilon = New(1, -1)
		divPrecision = 8
	} else {
		epsilon = New(1, -precision-1)
		divPrecision = precision + 1
	}

	decAbs := d.Abs()
	pow := d.Abs()
	factorial := New(1, 0)

	result := New(1, 0)

	for i := int64(1); ; {
		step := pow.DivRound(factorial, divPrecision)
		result = result.Add(step)

		// Stop Taylor series when current step is smaller than epsilon
		if step.Cmp(epsilon) < 0 {
			break
		}

		pow = pow.Mul(decAbs)

		i++

		// Calculate next factorial number or retrieve cached value
		factorialsMutex.RLock()
		if len(factorials) >= int(i) && !factorials[i-1].IsZero() {
			factorial = factorials[i-1]
			factorialsMutex.RUnlock()
		} else {
			prevFactorial := factorials[i-2]
			factorialsMutex.RUnlock()
			factorial = prevFactorial.Mul(New(i, 0))
			factorialsMutex.Lock()
			// Check again in case another goroutine already added it.
			if len(factorials) < int(i) || factorials[i-1].IsZero() {
				factorials = append(factorials, Zero)
				factorials[i-1] = factorial
			}
			factorialsMutex.Unlock()
		}
	}

	if d.Sign() < 0 {
		result = New(1, 0).DivRound(result, precision+1)
	}

	result = result.Round(precision)
	return result, nil
}

// Ln calculates natural logarithm of d.
// Precision argument specifies how precise the result must be (number of digits after decimal point).
// Negative precision is allowed.
//
// Example:
//
//	d1, err := NewFromFloat(13.3).Ln(2)
//	d1.String()  // output: "2.59"
//
//	d2, err := NewFromFloat(579.161).Ln(10)
//	d2.String()  // output: "6.3615805046"
func (d Decimal) Ln(precision int32) (Decimal, error) {
	// Algorithm based on The Use of Iteration Methods for Approximating the Natural Logarithm,
	// James F. Epperson, The American Mathematical Monthly, Vol. 96, No. 9, November 1989, pp. 831-835.
	if d.IsNegative() {
		return Decimal{}, fmt.Errorf("cannot calculate natural logarithm for negative decimals")
	}

	if d.IsZero() {
		return Decimal{}, fmt.Errorf("cannot represent natural logarithm of 0, result: -infinity")
	}

	calcPrecision := precision + 2
	z := d.Copy()

	var comp1, comp3, comp2, comp4, reduceAdjust Decimal
	comp1 = z.Sub(Decimal{oneInt, 0})
	comp3 = Decimal{oneInt, -1}

	// for decimal in range [0.9, 1.1] where ln(d) is close to 0
	usePowerSeries := false

	if comp1.Abs().Cmp(comp3) <= 0 {
		usePowerSeries = true
	} else {
		// reduce input decimal to range [0.1, 1)
		expDelta := int32(z.NumDigits()) + z.exp
		z.exp -= expDelta

		// Input decimal was reduced by factor of 10^expDelta, thus we will need to add
		// ln(10^expDelta) = expDelta * ln(10)
		// to the result to compensate that
		ln10 := ln10.withPrecision(calcPrecision)
		reduceAdjust = NewFromInt32(expDelta)
		reduceAdjust = reduceAdjust.Mul(ln10)

		comp1 = z.Sub(Decimal{oneInt, 0})

		if comp1.Abs().Cmp(comp3) <= 0 {
			usePowerSeries = true
		} else {
			// initial estimate using floats
			zFloat := z.InexactFloat64()
			comp1 = NewFromFloat(math.Log(zFloat))
		}
	}

	epsilon := Decimal{oneInt, -calcPrecision}

	if usePowerSeries {
		// Power Series - https://en.wikipedia.org/wiki/Logarithm#Power_series
		// Calculating n-th term of formula: ln(z+1) = 2 sum [ 1 / (2n+1) * (z / (z+2))^(2n+1) ]
		// until the difference between current and next term is smaller than epsilon.
		// Coverage quite fast for decimals close to 1.0

		// z + 2
		comp2 = comp1.Add(Decimal{twoInt, 0})
		// z / (z + 2)
		comp3 = comp1.DivRound(comp2, calcPrecision)
		// 2 * (z / (z + 2))
		comp1 = comp3.Add(comp3)
		comp2 = comp1.Copy()

		for n := 1; ; n++ {
			// 2 * (z / (z+2))^(2n+1)
			comp2 = comp2.Mul(comp3).Mul(comp3)

			// 1 / (2n+1) * 2 * (z / (z+2))^(2n+1)
			comp4 = NewFromInt(int64(2*n + 1))
			comp4 = comp2.DivRound(comp4, calcPrecision)

			// comp1 = 2 sum [ 1 / (2n+1) * (z / (z+2))^(2n+1) ]
			comp1 = comp1.Add(comp4)

			if comp4.Abs().Cmp(epsilon) <= 0 {
				break
			}
		}
	} else {
		// Halley's Iteration.
		// Calculating n-th term of formula: a_(n+1) = a_n - 2 * (exp(a_n) - z) / (exp(a_n) + z),
		// until the difference between current and next term is smaller than epsilon
		var prevStep Decimal
		maxIters := calcPrecision*2 + 10

		for i := int32(0); i < maxIters; i++ {
			// exp(a_n)
			comp3, _ = comp1.ExpTaylor(calcPrecision)
			// exp(a_n) - z
			comp2 = comp3.Sub(z)
			// 2 * (exp(a_n) - z)
			comp2 = comp2.Add(comp2)
			// exp(a_n) + z
			comp4 = comp3.Add(z)
			// 2 * (exp(a_n) - z) / (exp(a_n) + z)
			comp3 = comp2.DivRound(comp4, calcPrecision)
			// comp1 = a_(n+1) = a_n - 2 * (exp(a_n) - z) / (exp(a_n) + z)
			comp1 = comp1.Sub(comp3)

			if prevStep.Add(comp3).IsZero() {
				// If iteration steps oscillate we should return early and prevent an infinity loop
				// NOTE(mwoss): This should be quite a rare case, returning error is not necessary
				break
			}

			if comp3.Abs().Cmp(epsilon) <= 0 {
				break
			}

			prevStep = comp3
		}
	}

	comp1 = comp1.Add(reduceAdjust)

	return comp1.Round(precision), nil
}

// Trig functions

var (
	// Pi/4 split into three parts
	pi4A = NewFromFloat(7.85398125648498535156e-1)                             // 0x3fe921fb40000000
	pi4B = NewFromFloat(3.77489470793079817668e-8)                             // 0x3e64442d00000000
	pi4C = NewFromFloat(2.69515142907905952645e-15)                            // 0x3ce8469898cc5170
	m4PI = NewFromFloat(1.273239544735162542821171882678754627704620361328125) // 4/pi

	atanP0 = NewFromFloat(-8.750608600031904122785e-01)
	atanP1 = NewFromFloat(-1.615753718733365076637e+01)
	atanP2 = NewFromFloat(-7.500855792314704667340e+01)
	atanP3 = NewFromFloat(-1.228866684490136173410e+02)
	atanP4 = NewFromFloat(-6.485021904942025371773e+01)
	atanQ0 = NewFromFloat(2.485846490142306297962e+01)
	atanQ1 = NewFromFloat(1.650270098316988542046e+02)
	atanQ2 = NewFromFloat(4.328810604912902668951e+02)
	atanQ3 = NewFromFloat(4.853903996359136964868e+02)
	atanQ4 = NewFromFloat(1.945506571482613964425e+02)

	atanMorebits = NewFromFloat(6.123233995736765886130e-17) // pi/2 = PIO2 + Morebits
	tan3pio8     = NewFromFloat(2.41421356237309504880)      // tan(3*pi/8)
	piFloat      = NewFromFloat(3.14159265358979323846264338327950288419716939937510582097494459)
)

// Atan returns the arctangent, in radians, of x.
func (d Decimal) Atan() Decimal {
	if d.IsZero() {
		return d
	}
	if d.IsPositive() {
		return d.satan()
	}
	return d.Neg().satan().Neg()
}

func (d Decimal) xatan() Decimal {
	P0, P1, P2, P3, P4 := atanP0, atanP1, atanP2, atanP3, atanP4
	Q0, Q1, Q2, Q3, Q4 := atanQ0, atanQ1, atanQ2, atanQ3, atanQ4
	z := d.Mul(d)
	b1 := P0.Mul(z).Add(P1).Mul(z).Add(P2).Mul(z).Add(P3).Mul(z).Add(P4).Mul(z)
	b2 := z.Add(Q0).Mul(z).Add(Q1).Mul(z).Add(Q2).Mul(z).Add(Q3).Mul(z).Add(Q4)
	z = b1.Div(b2)
	z = d.Mul(z).Add(d)
	return z
}

// satan reduces its argument (known to be positive)
// to the range [0, 0.66] and calls xatan.
func (d Decimal) satan() Decimal {
	Morebits, Tan3pio8, pi := atanMorebits, tan3pio8, piFloat

	if d.LessThanOrEqual(New(66, -2)) {
		return d.xatan()
	}
	if d.GreaterThan(Tan3pio8) {
		return pi.Div(New(2, 0)).Sub(New(1, 0).Div(d).xatan()).Add(Morebits)
	}
	return pi.Div(New(4, 0)).Add((d.Sub(New(1, 0)).Div(d.Add(New(1, 0)))).xatan()).Add(New(5, -1).Mul(Morebits))
}

// sin coefficients
var _sin = [...]Decimal{
	NewFromFloat(1.58962301576546568060e-10), // 0x3de5d8fd1fd19ccd
	NewFromFloat(-2.50507477628578072866e-8), // 0xbe5ae5e5a9291f5d
	NewFromFloat(2.75573136213857245213e-6),  // 0x3ec71de3567d48a1
	NewFromFloat(-1.98412698295895385996e-4), // 0xbf2a01a019bfdf03
	NewFromFloat(8.33333333332211858878e-3),  // 0x3f8111111110f7d0
	NewFromFloat(-1.66666666666666307295e-1), // 0xbfc5555555555548
}

// Sin returns the sine of the radian argument x.
func (d Decimal) Sin() Decimal {
	PI4A, PI4B, PI4C, M4PI := pi4A, pi4B, pi4C, m4PI

	if d.IsZero() {
		return d
	}
	// make argument positive but save the sign
	sign := false
	if d.IsNegative() {
		d = d.Neg()
		sign = true
	}

	j := d.Mul(M4PI).IntPart()    // integer part of x/(Pi/4), as integer for tests on the phase angle
	y := NewFromFloat(float64(j)) // integer part of x/(Pi/4), as float

	// map zeros to origin
	if j&1 == 1 {
		j++
		y = y.Add(New(1, 0))
	}
	j &= 7 // octant modulo 2Pi radians (360 degrees)
	// reflect in x axis
	if j > 3 {
		sign = !sign
		j -= 4
	}
	z := d.Sub(y.Mul(PI4A)).Sub(y.Mul(PI4B)).Sub(y.Mul(PI4C)) // Extended precision modular arithmetic
	zz := z.Mul(z)

	if j == 1 || j == 2 {
		w := zz.Mul(zz).Mul(_cos[0].Mul(zz).Add(_cos[1]).Mul(zz).Add(_cos[2]).Mul(zz).Add(_cos[3]).Mul(zz).Add(_cos[4]).Mul(zz).Add(_cos[5]))
		y = New(1, 0).Sub(New(5, -1).Mul(zz)).Add(w)
	} else {
		y = z.Add(z.Mul(zz).Mul(_sin[0].Mul(zz).Add(_sin[1]).Mul(zz).Add(_sin[2]).Mul(zz).Add(_sin[3]).Mul(zz).Add(_sin[4]).Mul(zz).Add(_sin[5])))
	}
	if sign {
		y = y.Neg()
	}
	return y
}

// cos coefficients
var _cos = [...]Decimal{
	NewFromFloat(-1.13585365213876817300e-11), // 0xbda8fa49a0861a9b
	NewFromFloat(2.08757008419747316778e-9),   // 0x3e21ee9d7b4e3f05
	NewFromFloat(-2.75573141792967388112e-7),  // 0xbe927e4f7eac4bc6
	NewFromFloat(2.48015872888517045348e-5),   // 0x3efa01a019c844f5
	NewFromFloat(-1.38888888888730564116e-3),  // 0xbf56c16c16c14f91
	NewFromFloat(4.16666666666665929218e-2),   // 0x3fa555555555554b
}

// Cos returns the cosine of the radian argument x.
func (d Decimal) Cos() Decimal {

	PI4A, PI4B, PI4C, M4PI := pi4A, pi4B, pi4C, m4PI

	// make argument positive
	sign := false
	if d.IsNegative() {
		d = d.Neg()
	}

	j := d.Mul(M4PI).IntPart()    // integer part of x/(Pi/4), as integer for tests on the phase angle
	y := NewFromFloat(float64(j)) // integer part of x/(Pi/4), as float

	// map zeros to origin
	if j&1 == 1 {
		j++
		y = y.Add(New(1, 0))
	}
	j &= 7 // octant modulo 2Pi radians (360 degrees)
	// reflect in x axis
	if j > 3 {
		sign = !sign
		j -= 4
	}
	if j > 1 {
		sign = !sign
	}

	z := d.Sub(y.Mul(PI4A)).Sub(y.Mul(PI4B)).Sub(y.Mul(PI4C)) // Extended precision modular arithmetic
	zz := z.Mul(z)

	if j == 1 || j == 2 {
		y = z.Add(z.Mul(zz).Mul(_sin[0].Mul(zz).Add(_sin[1]).Mul(zz).Add(_sin[2]).Mul(zz).Add(_sin[3]).Mul(zz).Add(_sin[4]).Mul(zz).Add(_sin[5])))
	} else {
		w := zz.Mul(zz).Mul(_cos[0].Mul(zz).Add(_cos[1]).Mul(zz).Add(_cos[2]).Mul(zz).Add(_cos[3]).Mul(zz).Add(_cos[4]).Mul(zz).Add(_cos[5]))
		y = New(1, 0).Sub(New(5, -1).Mul(zz)).Add(w)
	}
	if sign {
		y = y.Neg()
	}
	return y
}

var _tanP = [...]Decimal{
	NewFromFloat(-1.30936939181383777646e+4), // 0xc0c992d8d24f3f38
	NewFromFloat(1.15351664838587416140e+6),  // 0x413199eca5fc9ddd
	NewFromFloat(-1.79565251976484877988e+7), // 0xc1711fead3299176
}
var _tanQ = [...]Decimal{
	NewFromFloat(1.00000000000000000000e+0),
	NewFromFloat(1.36812963470692954678e+4),  // 0x40cab8a5eeb36572
	NewFromFloat(-1.32089234440210967447e+6), // 0xc13427bc582abc96
	NewFromFloat(2.50083801823357915839e+7),  // 0x4177d98fc2ead8ef
	NewFromFloat(-5.38695755929454629881e+7), // 0xc189afe03cbe5a31
}

// Tan returns the tangent of the radian argument x.
func (d Decimal) Tan() Decimal {

	PI4A, PI4B, PI4C, M4PI := pi4A, pi4B, pi4C, m4PI

	if d.IsZero() {
		return d
	}

	// make argument positive but save the sign
	sign := false
	if d.IsNegative() {
		d = d.Neg()
		sign = true
	}

	j := d.Mul(M4PI).IntPart()    // integer part of x/(Pi/4), as integer for tests on the phase angle
	y := NewFromFloat(float64(j)) // integer part of x/(Pi/4), as float

	// map zeros to origin
	if j&1 == 1 {
		j++
		y = y.Add(New(1, 0))
	}

	z := d.Sub(y.Mul(PI4A)).Sub(y.Mul(PI4B)).Sub(y.Mul(PI4C)) // Extended precision modular arithmetic
	zz := z.Mul(z)

	if zz.GreaterThan(New(1, -14)) {
		w := zz.Mul(_tanP[0].Mul(zz).Add(_tanP[1]).Mul(zz).Add(_tanP[2]))
		x := zz.Add(_tanQ[1]).Mul(zz).Add(_tanQ[2]).Mul(zz).Add(_tanQ[3]).Mul(zz).Add(_tanQ[4])
		y = z.Add(z.Mul(w.Div(x)))
	} else {
		y = z
	}
	if j&2 == 2 {
		y = New(-1, 0).Div(y)
	}
	if sign {
		y = y.Neg()
	}
	return y
}
