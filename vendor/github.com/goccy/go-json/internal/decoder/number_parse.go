package decoder

import (
	"github.com/goccy/go-json/internal/jsonnum"
)

// The numbers of the input are parsed by jsonnum, in the buffer which a nul byte ends. The functions here are
// inlined, so that they are direct calls, which jsonnum.ParseDigits is inlined into.

// maxUint64Digits is the number of the digits which a uint64 holds whatever they are.
const maxUint64Digits = jsonnum.MaxUint64Digits

func parseDigits(buf []byte, cursor int64) (uint64, int64, int) {
	return jsonnum.ParseDigits(buf, cursor)
}

func isFloatContinuation(c byte) bool {
	return jsonnum.IsFloatContinuation(c)
}

func parseFloatFast(buf []byte, cursor int64) (float64, int64, bool) {
	return jsonnum.ParseFloatTerminated(buf, cursor)
}

// numberEnd returns the position after the number which starts at start, checked by the grammar of the JSON
// numbers, which is followed by a byte which may end a value.
func numberEnd(buf []byte, start int64) (int64, error) {
	n, place := jsonnum.Scan(buf[start:])
	end := start + int64(n)
	if place != jsonnum.Valid {
		return 0, numberPlaceError(buf, end, start, place)
	}
	if !validEndNumberChar[buf[end]] {
		return 0, syntaxErrorAt(buf, end, whereAfterTop)
	}
	return end, nil
}

// skipDigits returns the position of the first byte from c which is not a digit.
func skipDigits(buf []byte, c int64) int64 {
	for buf[c]-'0' <= 9 {
		c++
	}
	return c
}

// numberPlaceError returns the syntax error of the byte at cursor in the number which starts at start, which
// jsonnum.Scan found invalid at place.
func numberPlaceError(buf []byte, cursor, start int64, place jsonnum.Place) error {
	where := whereNumber
	switch place {
	case jsonnum.Fraction:
		where = whereFraction
	case jsonnum.Exponent:
		where = whereExponent
	}
	return numberSyntaxError(buf, cursor, start, where)
}
