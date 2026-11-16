// Package jsonnum reads JSON numbers by the grammar of RFC 8259, section 6, for the encoder and the decoder of
// encoding/json and for jsontext. The callers write their own errors from where a number is invalid.
package jsonnum

import (
	"encoding/binary"
	"math/bits"
)

// Place is where Scan found a number invalid.
type Place uint8

const (
	Valid    Place = iota // the number is valid
	Start                 // the first digit, after the sign if any
	Fraction              // the first digit after the decimal point
	Exponent              // the first digit of the exponent, after its sign if any
)

// Scan returns the length of the longest JSON number which b starts with, and Valid; or, if the number is
// invalid, the position of the byte where it is, which is len(b) if b ends there, and the place of that byte.
// The byte after a valid number is not looked at: it may be anything, which the caller checks.
func Scan(b []byte) (int, Place) {
	i := 0
	if i < len(b) && b[i] == '-' {
		i++
	}
	switch {
	case i == len(b):
		return i, Start
	case b[i] == '0':
		i++
	case '1' <= b[i] && b[i] <= '9':
		i = ScanDigits(b, i+1)
	default:
		return i, Start
	}
	if i < len(b) && b[i] == '.' {
		if i++; i == len(b) || b[i] < '0' || '9' < b[i] {
			return i, Fraction
		}
		i = ScanDigits(b, i+1)
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		if i++; i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		if i == len(b) || b[i] < '0' || '9' < b[i] {
			return i, Exponent
		}
		i = ScanDigits(b, i+1)
	}
	return i, Valid
}

// IsValid reports whether b is one JSON number, without anything before or after it. It takes a byte at a time
// by one lookup in a table of the states of the grammar, nearly as cheap as a check of the bytes alone, for the
// short numbers which a json.Number has: the state is the index of its row, which the byte is added to.
func IsValid(b []byte) bool {
	st := uint16(stateStart * 256)
	for _, c := range b {
		st = transitions[uint(st)+uint(c)]
	}
	return accepting[st>>8]
}

// NonDigitTops has the top bit of each byte of w which is not a digit. It is small enough to be inlined, as
// GatherTops is.
func NonDigitTops(w uint64) uint64 {
	const lsb, top = 0x0101010101010101, 0x8080808080808080
	// the top bit of each byte which is 0x3A or more, or less than 0x30, of its low 7 bits, or which has its own:
	// adding to the low 7 bits, and subtracting from the bytes with the top bit, carries between no bytes.
	return (((w&^top)+lsb*(0x80-0x3a))|w)&top | top&^((w|top)-lsb*0x30)
}

// GatherTops gathers the top bits of the bytes of m, which has no other bits, into a byte: of the byte i in the
// bit i.
func GatherTops(m uint64) uint8 {
	return uint8((m >> 7) * 0x0102040810204080 >> 56)
}

// The states of IsValid: where a number is, after the bytes before.
const (
	stateStart   = iota // before the sign
	stateSign           // after the sign
	stateZero           // after the integer 0
	stateInt            // in the digits of an integer which starts with 1 to 9
	stateDot            // after the decimal point
	stateFrac           // in the digits of the fraction
	stateE              // after e or E
	stateExpSign        // after the sign of the exponent
	stateExp            // in the digits of the exponent
	stateInvalid        // not a number, whatever follows
	numStates
)

// accepting are the states of IsValid at the end of a number.
var accepting = [numStates]bool{stateZero: true, stateInt: true, stateFrac: true, stateExp: true}

// transitions are the states of IsValid after each byte from each state, times 256, at the state times 256 plus
// the byte; the bytes which are not in the grammar from a state lead to stateInvalid.
var transitions = func() [numStates * 256]uint16 {
	var flat [numStates * 256]uint16
	var t [numStates][256]uint8
	for s := range t {
		for c := range t[s] {
			t[s][c] = stateInvalid
		}
	}
	digits := func(s, next uint8) {
		for c := '0'; c <= '9'; c++ {
			t[s][c] = next
		}
	}
	t[stateStart]['-'] = stateSign
	for _, s := range []uint8{stateStart, stateSign} {
		t[s]['0'] = stateZero
		for c := '1'; c <= '9'; c++ {
			t[s][c] = stateInt
		}
	}
	digits(stateInt, stateInt)
	for _, s := range []uint8{stateZero, stateInt} {
		t[s]['.'] = stateDot
		t[s]['e'], t[s]['E'] = stateE, stateE
	}
	digits(stateDot, stateFrac)
	digits(stateFrac, stateFrac)
	t[stateFrac]['e'], t[stateFrac]['E'] = stateE, stateE
	t[stateE]['+'], t[stateE]['-'] = stateExpSign, stateExpSign
	digits(stateE, stateExp)
	digits(stateExpSign, stateExp)
	digits(stateExp, stateExp)
	for s := range t {
		for c := range t[s] {
			flat[s*256+c] = uint16(t[s][c]) * 256
		}
	}
	return flat
}()

// End returns the end of the valid JSON number which starts b, if a byte which ends it follows it in b, or -1:
// at an error, and at a number which may continue after b. It is the fast check of a scanner which reads its
// input by parts.
func End(b []byte) int {
	i := 0
	if i < len(b) && b[i] == '-' {
		i++
	}
	switch {
	case i == len(b):
		return -1
	case b[i] == '0':
		i++
	case '1' <= b[i] && b[i] <= '9':
		i = ScanDigits(b, i+1)
	default:
		return -1
	}
	if i < len(b) && b[i] == '.' {
		if i++; i == len(b) || b[i] < '0' || '9' < b[i] {
			return -1
		}
		i = ScanDigits(b, i+1)
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		if i++; i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		if i == len(b) || b[i] < '0' || '9' < b[i] {
			return -1
		}
		i = ScanDigits(b, i+1)
	}
	if i == len(b) {
		return -1
	}
	return i
}

// ScanDigits returns the position of the first byte of b from i which is not a digit, or len(b). Long runs of
// digits, as the fractions of floats have, are looked at 8 bytes at a time.
func ScanDigits(b []byte, i int) int {
	for i+8 <= len(b) {
		// a digit XOR '0' is below 10: adding 0x76 to each byte of the low 7 bits carries into the top bit from
		// 10 on, and any byte of 0x80 or more has its top bit already.
		w := binary.LittleEndian.Uint64(b[i:]) ^ 0x3030303030303030
		if m := ((w &^ 0x8080808080808080) + 0x7676767676767676 | w) & 0x8080808080808080; m != 0 {
			return i + bits.TrailingZeros64(m)/8
		}
		i += 8
	}
	for i < len(b) && '0' <= b[i] && b[i] <= '9' {
		i++
	}
	return i
}
