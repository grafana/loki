// GoAWK interpreter value type (not exported).

package interp

import (
	"fmt"
	"math"
	"strconv"

	"github.com/benhoyt/goawk/internal/strutil"
)

type valueType uint8

const (
	typeNull valueType = iota
	typeStr
	typeNum
	typeNumStr
)

// An AWK value (these are passed around by value)
type value struct {
	typ valueType // Type of value
	s   string    // String value (for typeStr)
	n   float64   // Numeric value (for typeNum and typeNumStr)
}

// Create a new null value
func null() value {
	return value{}
}

// Create a new number value
func num(n float64) value {
	return value{typ: typeNum, n: n}
}

// Create a new string value
func str(s string) value {
	return value{typ: typeStr, s: s}
}

// Create a new value for a "numeric string" context, converting the
// string to a number if possible.
func numStr(s string) value {
	f, err := strconv.ParseFloat(strutil.TrimSpace(s), 64)
	if err != nil {
		// Doesn't parse as number, make it a "true string"
		return value{typ: typeStr, s: s}
	}
	return value{typ: typeNumStr, s: s, n: f}
}

// Create a numeric value from a Go bool
func boolean(b bool) value {
	if b {
		return num(1)
	}
	return num(0)
}

// Return true if value is a "true string" (string but not a "numeric
// string")
func (v value) isTrueStr() bool {
	return v.typ == typeStr
}

// Return Go bool value of AWK value. For numbers or numeric strings,
// zero is false and everything else is true. For strings, empty
// string is false and everything else is true.
func (v value) boolean() bool {
	if v.isTrueStr() {
		return v.s != ""
	} else {
		return v.n != 0
	}
}

// Return value's string value, or convert to a string using given
// format if a number value. Integers are a special case and don't
// use floatFormat.
func (v value) str(floatFormat string) string {
	if v.typ == typeNum {
		switch {
		case math.IsNaN(v.n):
			return "nan"
		case math.IsInf(v.n, 0):
			if v.n < 0 {
				return "-inf"
			} else {
				return "inf"
			}
		case v.n == float64(int(v.n)):
			return strconv.Itoa(int(v.n))
		default:
			return fmt.Sprintf(floatFormat, v.n)
		}
	}
	// For typeStr and typeStrNum we already have the string, for
	// typeNull v.s == "".
	return v.s
}

// Return value's number value, converting from string if necessary
func (v value) num() float64 {
	if v.typ == typeStr {
		// Ensure string starts with a float and convert it
		return parseFloatPrefix(v.s)
	}
	// Handle case for typeNum and typeStrNum. If it's a numeric
	// string, we already have the float value from the numStr()
	// call. For typeNull v.n == 0.
	return v.n
}

// Like strconv.ParseFloat, but parses at the start of string and
// allows things like "1.5foo"
func parseFloatPrefix(s string) float64 {
	// Skip whitespace at start
	i := 0
	for i < len(s) && strutil.IsASCIISpace(s[i]) {
		i++
	}
	start := i

	// Parse mantissa: optional sign, initial digit(s), optional '.',
	// then more digits
	gotDigit := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		gotDigit = true
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		gotDigit = true
		i++
	}
	if !gotDigit {
		return 0
	}

	// Parse exponent ("1e" and similar are allowed, but ParseFloat
	// rejects them)
	end := i
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			end = i
		}
	}

	floatStr := s[start:end]
	f, _ := strconv.ParseFloat(floatStr, 64)
	return f // Returns infinity in case of "value out of range" error
}
