package decoder

import (
	"fmt"

	"github.com/goccy/go-json/internal/errors"
)

// The numbers of the input are parsed in one pass: the digits are accumulated as they are read.

// maxUint64Digits is the number of the digits which a uint64 holds whatever they are: 19 nines are less than 1<<64.
const maxUint64Digits = 19

// parseDigits reads the digits at cursor, which starts a number after its sign, and returns their value, the
// position after them and their number. A number which starts with 0 has no other digit: the digit after it,
// if any, is not read. The value is exact for up to maxUint64Digits digits.
func parseDigits(buf []byte, cursor int64) (uint64, int64, int) {
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

// isFloatContinuation reports whether c continues an integer as a number which is not an integer.
func isFloatContinuation(c byte) bool {
	return c == '.' || c == 'e' || c == 'E'
}

// float64pow10 are the powers of ten which a float64 holds exactly.
var float64pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// parseFloatFast parses the number at cursor, as the JSON grammar has it, when its mantissa has at most 19
// digits: the float64 of a mantissa of at most 53 bits times or divided by a power of ten which a float64 holds
// exactly is correctly rounded as it is ( Clinger's fast path ), and any other is multiplied by the power of ten
// from a table ( mulPow10 ). The result is the one of strconv.ParseFloat. It returns false for a number which
// is not decided so, or anything which is not a number by the grammar, which the caller parses as it did before.
func parseFloatFast(buf []byte, cursor int64) (float64, int64, bool) {
	neg := buf[cursor] == '-'
	if neg {
		cursor++
	}
	if buf[cursor]-'0' > 9 {
		return 0, 0, false
	}
	mantissa, cursor, digits := parseDigits(buf, cursor)
	exp := 0
	if buf[cursor] == '.' {
		cursor++
		start := cursor
		if mantissa == 0 {
			// the zeros before the first digit which is not zero are not digits of the mantissa
			digits = 0
		}
		for {
			d := buf[cursor] - '0'
			if d > 9 {
				break
			}
			mantissa = mantissa*10 + uint64(d)
			if mantissa != 0 {
				digits++
			}
			cursor++
		}
		fraction := int(cursor - start)
		if fraction == 0 {
			return 0, 0, false
		}
		exp = -fraction
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
	if digits > maxUint64Digits {
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

// numberEnd returns the position after the number which starts at start, validated by the grammar of the JSON
// numbers, which is followed by a byte which may end a value.
func numberEnd(buf []byte, start int64) (int64, error) {
	c := start
	if buf[c] == '-' {
		c++
	}
	switch {
	case buf[c] == '0':
		c++
	case '1' <= buf[c] && buf[c] <= '9':
		c = skipDigits(buf, c+1)
	default:
		return 0, errors.ErrSyntax(fmt.Sprintf("invalid character %s in numeric literal", quoteChar(buf[c])), c+1)
	}
	if buf[c] == '.' {
		c++
		if buf[c]-'0' > 9 {
			return 0, errors.ErrSyntax(fmt.Sprintf("invalid character %s after decimal point in numeric literal", quoteChar(buf[c])), c+1)
		}
		c = skipDigits(buf, c)
	}
	if buf[c] == 'e' || buf[c] == 'E' {
		c++
		if buf[c] == '+' || buf[c] == '-' {
			c++
		}
		if buf[c]-'0' > 9 {
			return 0, errors.ErrSyntax(fmt.Sprintf("invalid character %s in exponent of numeric literal", quoteChar(buf[c])), c+1)
		}
		c = skipDigits(buf, c)
	}
	if !validEndNumberChar[buf[c]] {
		return 0, errors.ErrSyntax(fmt.Sprintf("invalid character %s after top-level value", quoteChar(buf[c])), c+1)
	}
	return c, nil
}

// skipDigits returns the position of the first byte from c which is not a digit.
func skipDigits(buf []byte, c int64) int64 {
	for buf[c]-'0' <= 9 {
		c++
	}
	return c
}
