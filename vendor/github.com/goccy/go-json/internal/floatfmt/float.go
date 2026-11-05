// Package floatfmt writes floats as JSON numbers, by their shortest decimal, as ECMA-262 formats numbers: the
// format of encoding/json and of encoding/json/jsontext.
package floatfmt

import (
	"encoding/binary"
	"math"
	"math/bits"
	"strconv"
)

// The floats are written by their shortest decimal, which is found as Raffaello Giulietti finds it in "The
// Schubfach way to render doubles" (2020): of the decimals which round to the float, the one of the fewest
// digits, and of those, the one closest to the float, or the one of an even last digit where two are as close.

//go:generate go run ../cmd/float_table float_table.go

// AppendFloat appends the finite float f, of the precision bits, 64 or 32, as ECMA-262, 10th edition, section
// 7.1.12.1 formats a number, except that -0 is -0: as strconv.AppendFloat with the format 'f', or 'e' below 1e-6
// and from 1e21, whose exponent has no leading zero, writes it.
func AppendFloat(dst []byte, f float64, bits int) []byte {
	if bits == 32 {
		f = float64(float32(f))
	}
	// an integer below 2⁵³, or 2²⁴ for 32 bits, is the only integer which rounds to its float: its digits are the
	// shortest.
	limit := int64(1) << 53
	if bits == 32 {
		limit = 1 << 24
	}
	if i := int64(f); float64(i) == f && i != 0 && -limit < i && i < limit {
		return strconv.AppendInt(dst, i, 10)
	}
	return appendShortestFloat(dst, f, bits)
}

// The range of the exponents k of the powers of ten of floatPow10.
const (
	floatKMin = -324
	floatKMax = 292
)

// floatFormat is the form of the floats of a size: the bits of the significand without its hidden bit, the
// bits of the exponent, and the least exponent of the significand as an integer.
type floatFormat struct {
	fracBits uint
	expMask  uint64
	qMin     int
}

var (
	float64Format = floatFormat{fracBits: 52, expMask: 0x7ff, qMin: -1074}
	float32Format = floatFormat{fracBits: 23, expMask: 0xff, qMin: -149}
)

// shortestDecimal returns the shortest decimal d·10ᵉ of the finite float of the size fm, which is not zero,
// whose sign and exponent and significand are given as the bits of IEEE 754. d may end with zeros.
func shortestDecimal(b uint64, fm *floatFormat) (uint64, int) {
	t := b & (1<<fm.fracBits - 1)
	bq := int(b>>fm.fracBits) & int(fm.expMask)
	if bq == 0 {
		// a subnormal float
		return schubfach(fm.qMin, t, fm)
	}
	return schubfach(bq-1+fm.qMin, 1<<fm.fracBits|t, fm)
}

// schubfach returns the shortest decimal of c·2^q. Unlike the paper, which writes at least two digits, it looks
// for a decimal of one digit less for any s of two digits or more, and scales no significand.
func schubfach(q int, c uint64, fm *floatFormat) (uint64, int) {
	out := c & 1 // a significand which is even rounds its halfway points to itself: they are in its interval
	cb := c << 2
	cbr := cb + 2
	var cbl uint64
	var k int
	if c != 1<<fm.fracBits || q == fm.qMin {
		cbl = cb - 2
		k = floorLog10Pow2(q)
	} else {
		// the float below a power of two is nearer than the one above it
		cbl = cb - 1
		k = floorLog10ThreeQuartersPow2(q)
	}
	h := q + floorLog2Pow10(-k) + 2
	g := &floatPow10[k-floatKMin]
	vb := roundOdd(g, cb<<uint(h))
	vbl := roundOdd(g, cbl<<uint(h))
	vbr := roundOdd(g, cbr<<uint(h))

	// The choices are made by arithmetic, not by branches, which the digits of the floats would mispredict.
	// Of s and s+1, one or both are in the interval: the decimal is s+1 where s is not, or where both are and
	// s+1 is closer to the float, or as close and even.
	s := vb >> 2
	uin := bit(vbl+out <= s<<2)
	win := bit((s+1)<<2+out <= vbr)
	mid := s<<2 + 2
	d := s + (1 - uin | win&(bit(vb > mid)|bit(vb == mid)&s))
	if s >= 10 {
		// a decimal of one digit less, which is the decimal if only one of its neighbors is in the interval: the
		// interval is narrower than ten of the last digit of s, and has one of them at most. It is returned
		// without its last digit, which is 0.
		sp := s / 10
		upin := bit(vbl+out <= sp*10<<2)
		wpin := bit((sp+1)*10<<2+out <= vbr)
		m := -(upin ^ wpin)
		d = d&^m | (sp+wpin)&m
		k += int(m & 1)
	}
	return d, k
}

// bit is 1 for true and 0 for false.
func bit(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// roundOdd returns g·cp / 2¹²⁷, where g is the 126 bits of its high and low 63 bits, rounded to odd: its
// lowest bit is set if it is not exact.
func roundOdd(g *[2]uint64, cp uint64) uint64 {
	x1, _ := bits.Mul64(g[1], cp)
	y1, y0 := bits.Mul64(g[0], cp)
	z := y0>>1 + x1
	vbp := y1 + z>>63
	return vbp | (z&(1<<63-1)+(1<<63-1))>>63
}

// ⌊e·log₁₀2⌋, ⌊log₁₀(¾·2ᵉ)⌋ and ⌊e·log₂10⌋, exact for the exponents of floats.
// They take 64 bits, which an int of 32 bits doesn't have.
func floorLog10Pow2(e int) int { return int(int64(e) * 661971961083 >> 41) }
func floorLog10ThreeQuartersPow2(e int) int {
	return int((int64(e)*661971961083 - 274743187321) >> 41)
}
func floorLog2Pow10(e int) int { return int(int64(e) * 913124641741 >> 38) }

// appendShortestFloat appends the finite float f of the size, 64 or 32, as ECMA-262, 10th edition, section
// 7.1.12.1 formats a number, except that -0 is -0.
func appendShortestFloat(dst []byte, f float64, size int) []byte {
	var b uint64
	fm := &float64Format
	if size == 32 {
		b, fm = uint64(math.Float32bits(float32(f))), &float32Format
	} else {
		b = math.Float64bits(f)
	}
	if math.Signbit(f) {
		dst = append(dst, '-')
	}
	if f == 0 {
		return append(dst, '0')
	}
	d, e := shortestDecimal(b, fm)
	for d%10 == 0 {
		d /= 10
		e++
	}
	// the digits, the lowest ones by blocks of eight, whose divisions don't wait for each other
	var buf [24]byte
	i := len(buf)
	for d >= 1e8 {
		i -= 8
		put8Digits(buf[i:i+8], uint32(d%1e8))
		d /= 1e8
	}
	for ; d >= 100; d /= 100 {
		i -= 2
		copy(buf[i:i+2], twoDigits[d%100*2:])
	}
	if d >= 10 {
		i -= 2
		copy(buf[i:i+2], twoDigits[d*2:])
	} else {
		i--
		buf[i] = byte('0' + d)
	}
	digits := buf[i:]
	// the decimal is 0.digits·10ⁿ
	n := e + len(digits)
	switch {
	case len(digits) <= n && n <= 21:
		dst = append(dst, digits...)
		for range n - len(digits) {
			dst = append(dst, '0')
		}
	case 0 < n && n <= 21:
		dst = append(dst, digits[:n]...)
		dst = append(dst, '.')
		dst = append(dst, digits[n:]...)
	case -6 < n && n <= 0:
		dst = append(dst, "0."...)
		for range -n {
			dst = append(dst, '0')
		}
		dst = append(dst, digits...)
	default:
		dst = append(dst, digits[0])
		if len(digits) > 1 {
			dst = append(dst, '.')
			dst = append(dst, digits[1:]...)
		}
		dst = append(dst, 'e')
		x := n - 1
		if x < 0 {
			dst = append(dst, '-')
			x = -x
		} else {
			dst = append(dst, '+')
		}
		if x >= 100 {
			dst = append(dst, byte('0'+x/100))
			dst = append(dst, twoDigits[x%100*2:x%100*2+2]...)
		} else if x >= 10 {
			dst = append(dst, twoDigits[x*2:x*2+2]...)
		} else {
			dst = append(dst, byte('0'+x))
		}
	}
	return dst
}

// put8Digits writes the eight digits of v, which is below 10⁸, to b, by dividing the parts of v in the lanes of
// a word at once: its halves of four digits, then their halves of two digits, then their digits, the first one
// in the lowest lane, which is the first byte that the word is stored as.
func put8Digits(b []byte, v uint32) {
	x := uint64(v/1e4) | uint64(v%1e4)<<32
	// n/100 is n·10486 >> 20 for n below 10⁴, and n/10 is n·103 >> 10 for n below 100: the products don't
	// reach the bits of the lanes which are kept.
	q := x * 10486 >> 20 & 0x0000007f_0000007f
	x = q | (x-q*100)<<16
	q = x * 103 >> 10 & 0x000f000f_000f000f
	x = q | (x-q*10)<<8
	binary.LittleEndian.PutUint64(b, x|0x30303030_30303030)
}

// twoDigits are the numbers from 00 to 99.
const twoDigits = "00010203040506070809101112131415161718192021222324252627282930313233343536373839" +
	"40414243444546474849505152535455565758596061626364656667686970717273747576777879" +
	"8081828384858687888990919293949596979899"
