package jsonnum

// The numbers are parsed in one pass: the digits are accumulated as they are read. The parsers read a buffer
// which a byte that can't be in a number ends, as the nul byte which the buffer of the decoder of encoding/json
// ends with, without checks of the end of the buffer.

// MaxUint64Digits is the number of the digits which a uint64 holds whatever they are: 19 nines are less than
// 1<<64.
const MaxUint64Digits = 19

// ParseDigits reads the digits at cursor, which starts a number after its sign, in a terminated buffer, and
// returns their value, the position after them and their number. A number which starts with 0 has no other
// digit: the digit after it, if any, is not read. The value is exact for up to MaxUint64Digits digits; the value of
// more may wrap around, and be any value, 0 too, which only their number tells.
func ParseDigits(buf []byte, cursor int64) (uint64, int64, int) {
	if buf[cursor] == '0' {
		return 0, cursor + 1, 1
	}
	start := cursor
	var u uint64
	for {
		d := buf[cursor] - '0'
		if d > 9 {
			break
		}
		u = u*10 + uint64(d)
		cursor++
	}
	return u, cursor, int(cursor - start)
}

// TwentyDigits is the value of the 20 digits at start, which ParseDigits doesn't take exactly, and true if it
// fits a uint64: the value of the first 19 is exact, and the last one fits if the whole is less than 1<<64.
func TwentyDigits(buf []byte, start int64) (uint64, bool) {
	var hi uint64
	for i := int64(0); i < MaxUint64Digits; i++ {
		hi = hi*10 + uint64(buf[start+i]-'0')
	}
	lo := uint64(buf[start+MaxUint64Digits] - '0')
	if hi > (1<<64-1)/10 || (hi == (1<<64-1)/10 && lo > (1<<64-1)%10) {
		return 0, false
	}
	return hi*10 + lo, true
}

// ParseUint is the value of b, which is the digits of an integer without a sign and nothing else, and true; or
// false if b is not one, as an integer which starts with 0 and has more digits, or its value overflows a uint64.
func ParseUint(b []byte) (uint64, bool) {
	if len(b) == 0 || len(b) > MaxUint64Digits+1 {
		return 0, false
	}
	var buf [MaxUint64Digits + 2]byte // the byte after b is 0
	copy(buf[:], b)
	u, end, digits := ParseDigits(buf[:], 0)
	switch {
	case int(end) != len(b):
		return 0, false
	case digits == MaxUint64Digits+1:
		return TwentyDigits(buf[:], 0)
	}
	return u, true
}

// IsFloatContinuation reports whether c continues an integer as a number which is not an integer.
func IsFloatContinuation(c byte) bool {
	return c == '.' || c == 'e' || c == 'E'
}

// float64pow10 are the powers of ten which a float64 holds exactly.
var float64pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// ParseFloatTerminated parses the number at cursor of a terminated buffer, as the JSON grammar has it, when its
// mantissa has at most 19 digits: the float64 of a mantissa of at most 53 bits times or divided by a power of
// ten which a float64 holds exactly is correctly rounded as it is ( Clinger's fast path ), and any other is
// multiplied by the power of ten from a table ( mulPow10 ). The result is the one of strconv.ParseFloat, and the
// position after the number. It returns false for a number which is not decided so, or anything which is not a
// number by the grammar, which the caller parses as it did before.
func ParseFloatTerminated(buf []byte, cursor int64) (float64, int64, bool) {
	// the start of the number, which significantDigits reads again in the guard after the exponent; a call before
	// it, in the branch of the fraction, which can derive the start, made the parsing of numbers of a few digits
	// slower
	begin := cursor
	neg := buf[cursor] == '-'
	if neg {
		cursor++
	}
	if buf[cursor]-'0' > 9 {
		return 0, 0, false
	}
	// The digits are counted by their positions, not by the value of the mantissa, which may wrap around past
	// MaxUint64Digits digits and be 0 after any digit past them. Their number counts the 0 of an integer part 0 and
	// the zeros of its fraction before the first digit which is not zero, which are not digits of the mantissa: a
	// number of more than MaxUint64Digits digits counts its digits again without them ( significantDigits ).
	mantissa, cursor, digits := ParseDigits(buf, cursor)
	exp := 0
	if buf[cursor] == '.' {
		cursor++
		start := cursor
		for {
			d := buf[cursor] - '0'
			if d > 9 {
				break
			}
			mantissa = mantissa*10 + uint64(d)
			cursor++
		}
		fraction := int(cursor - start)
		if fraction == 0 {
			return 0, 0, false
		}
		exp = -fraction
		digits += fraction
	}
	if c := buf[cursor]; c == 'e' || c == 'E' {
		cursor++
		expNeg := false
		switch buf[cursor] {
		case '-':
			expNeg = true
			cursor++
		case '+':
			cursor++
		}
		start := cursor
		e := 0
		for {
			d := buf[cursor] - '0'
			if d > 9 {
				break
			}
			if e < 10000 {
				e = e*10 + int(d)
			}
			cursor++
		}
		if cursor == start {
			return 0, 0, false
		}
		if expNeg {
			e = -e
		}
		exp += e
	}
	if digits > MaxUint64Digits && significantDigits(buf, begin) > MaxUint64Digits {
		return 0, 0, false
	}
	var f float64
	if mantissa <= 1<<53 && exp >= -22 && exp <= 22 {
		f = float64(mantissa)
		if exp < 0 {
			f /= float64pow10[-exp]
		} else {
			f *= float64pow10[exp]
		}
	} else {
		var ok bool
		if f, ok = mulPow10(mantissa, exp); !ok {
			return 0, 0, false
		}
	}
	if neg {
		f = -f
	}
	return f, cursor, true
}

// significantDigits is the number of the digits of the mantissa of the number at start, which ParseFloatTerminated
// read: the digits of its integer part and its fraction from the first one which is not zero. It is a function of
// its own, which is not inlined, so that the numbers of a few digits, which don't need it, are parsed without a jump
// over it.
//
//go:noinline
func significantDigits(buf []byte, start int64) int {
	if buf[start] == '-' {
		start++
	}
	digits := 0
	if buf[start] == '0' {
		// the integer part starts with 0 only if it is 0, which is not a digit of the mantissa, nor are the zeros of
		// the fraction before its first digit which is not zero
		start++
		if buf[start] == '.' {
			start++
		}
		for buf[start] == '0' {
			start++
		}
	} else {
		for buf[start]-'0' <= 9 {
			start++
			digits++
		}
		if buf[start] == '.' {
			start++
		}
	}
	for buf[start]-'0' <= 9 {
		start++
		digits++
	}
	return digits
}

// maxParsedLen is the longest number which ParseFloat parses: a longer one has more digits than a mantissa of
// ParseFloatTerminated holds, unless it has many zeros, which strconv parses.
const maxParsedLen = 31

// ParseFloat is ParseFloatTerminated of b, which is a JSON number and nothing else: it is copied to a terminated
// buffer of its own. It returns false where strconv parses the number.
func ParseFloat(b []byte) (float64, bool) {
	if len(b) > maxParsedLen {
		return 0, false
	}
	var buf [maxParsedLen + 1]byte // the byte after b is 0
	copy(buf[:], b)
	f, end, ok := ParseFloatTerminated(buf[:], 0)
	return f, ok && int(end) == len(b)
}
