package jsontext

import (
	"bytes"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/goccy/go-json/internal/intfmt"
	"github.com/goccy/go-json/internal/jsonnum"
)

// Kind is the kind of a JSON token.
//
// It is a single byte, the first byte of the token in the grammar, except for numbers, whose kind is always
// '0'.
type Kind byte

const (
	KindInvalid     Kind = 0   // invalid kind
	KindNull        Kind = 'n' // null
	KindFalse       Kind = 'f' // false
	KindTrue        Kind = 't' // true
	KindString      Kind = '"' // string
	KindNumber      Kind = '0' // number
	KindBeginObject Kind = '{' // begin object
	KindEndObject   Kind = '}' // end object
	KindBeginArray  Kind = '[' // begin array
	KindEndArray    Kind = ']' // end array
)

// String returns a string representation of k.
func (k Kind) String() string {
	switch k {
	case KindInvalid:
		return "invalid"
	case KindNull:
		return "null"
	case KindFalse:
		return "false"
	case KindTrue:
		return "true"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBeginObject, KindEndObject, KindBeginArray, KindEndArray:
		return string(rune(k))
	}
	return "<invalid jsontext.Kind: " + quoteChar([]byte{byte(k)}) + ">"
}

// kindOf is the kind of a token or value which starts with c, or KindInvalid.
func kindOf(c byte) Kind {
	switch c {
	case 'n', 'f', 't', '"', '{', '}', '[', ']':
		return Kind(c)
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return KindNumber
	}
	return KindInvalid
}

// The forms in which a Token holds its value.
const (
	formNone    = iota // a literal or a delimiter, whose kind is all of it
	formRaw            // the text of the token: in the buffer of dec, or in str after Clone
	formString         // a string, in str
	formInt            // an int64, in num
	formUint           // a uint64, in num
	formFloat          // a float64, as its bits in num
	formFloat32        // a float32, as the bits of its float64 value in num
)

// Token is a lexical JSON token, one of:
//   - a JSON literal (i.e., null, true, or false)
//   - a JSON string (e.g., "hello, world!")
//   - a JSON number (e.g., 123.456)
//   - a begin or end delimiter of a JSON object (i.e., { or } )
//   - a begin or end delimiter of a JSON array (i.e., [ or ] )
//
// A Token can't hold a whole array or object, which a Value can. There is no Token for a comma or a colon,
// which the structure implies.
//
// A Token holds its value in one of two forms:
//
//   - As raw JSON text in the buffer of a Decoder, as only Decoder.ReadToken makes it. Such a token is only
//     valid until the next call of a method of the Decoder (e.g., Decoder.PeekKind, Decoder.ReadToken,
//     Decoder.ReadValue, or Decoder.SkipValue). Token.Clone copies the text to a token which stays valid.
//
//   - As a Go value, as the constructors (e.g., String, Int, Uint, Float) make it. Such a token is valid
//     forever.
type Token struct {
	_ [0]func() // not comparable

	// Of four fields at most, so that the compiler keeps a Token in registers.
	src *tokenSource // what the token is
	str string       // the value of a string, or the text of a clone of a raw token
	num uint64       // the value of a number, or, in the buffer of the decoder, the offset of the token in the input
}

// tokenSource is the kind and the form of a token, and the decoder of a raw token: a raw token points to one of
// its decoder, and the others to one of the variables here.
type tokenSource struct {
	dec  *decoder
	kind Kind
	form uint8
}

var (
	invalidSource = tokenSource{}

	nullSource, falseSource, trueSource = tokenSource{kind: KindNull}, tokenSource{kind: KindFalse}, tokenSource{kind: KindTrue}

	beginObjectSource, endObjectSource = tokenSource{kind: KindBeginObject}, tokenSource{kind: KindEndObject}
	beginArraySource, endArraySource   = tokenSource{kind: KindBeginArray}, tokenSource{kind: KindEndArray}

	stringSource                     = tokenSource{kind: KindString, form: formString}
	intSource, uintSource            = tokenSource{kind: KindNumber, form: formInt}, tokenSource{kind: KindNumber, form: formUint}
	floatSource, float32Source       = tokenSource{kind: KindNumber, form: formFloat}, tokenSource{kind: KindNumber, form: formFloat32}
	nonFiniteSource                  = tokenSource{kind: KindString, form: formFloat} // NaN or an infinity
	nonFinite32Source                = tokenSource{kind: KindString, form: formFloat32}
	rawStringSource, rawNumberSource = tokenSource{kind: KindString, form: formRaw}, tokenSource{kind: KindNumber, form: formRaw}
)

var (
	Null  = Token{src: &nullSource}
	False = Token{src: &falseSource}
	True  = Token{src: &trueSource}

	BeginObject = Token{src: &beginObjectSource}
	EndObject   = Token{src: &endObjectSource}
	BeginArray  = Token{src: &beginArraySource}
	EndArray    = Token{src: &endArraySource}
)

// source is the source of t, which the zero Token has not.
func (t Token) source() *tokenSource {
	if t.src == nil {
		return &invalidSource
	}
	return t.src
}

// literalToken is the token of a literal or a delimiter of the kind k.
func literalToken(k Kind) Token {
	switch k {
	case KindNull:
		return Null
	case KindFalse:
		return False
	case KindTrue:
		return True
	case KindBeginObject:
		return BeginObject
	case KindEndObject:
		return EndObject
	case KindBeginArray:
		return BeginArray
	}
	return EndArray
}

// Bool constructs a Token of a JSON boolean.
func Bool(b bool) Token {
	if b {
		return True
	}
	return False
}

// String constructs a Token of a JSON string. The string should be valid UTF-8: invalid bytes may be mangled
// as the Unicode replacement character.
func String(s string) Token {
	return Token{src: &stringSource, str: s}
}

// Float constructs a Token of a JSON number of a 64-bit floating-point number, formatted as ECMA-262, 10th
// edition, section 7.1.12.1 and RFC 8785, section 3.2.2.3 do, except that -0 is formatted as -0. NaN, +Inf and
// -Inf are JSON strings of the values "NaN", "Infinity" and "-Infinity".
func Float(n float64) Token {
	src := &floatSource
	if math.IsNaN(n) || math.IsInf(n, 0) {
		src = &nonFiniteSource
	}
	return Token{src: src, num: math.Float64bits(n)}
}

// Float32 constructs a Token of a JSON number of a 32-bit floating-point number, formatted as ECMA-262, 10th
// edition, section 7.1.12.1 does, except that -0 is formatted as -0. NaN, +Inf and -Inf are JSON strings of the
// values "NaN", "Infinity" and "-Infinity".
//
// Most JSON libraries and standards take JSON numbers as 64-bit floating-point numbers: use 32-bit precision
// only if the decoder knows that the number has only 32-bit precision. In any other case, use Float.
func Float32(n float32) Token {
	f := float64(n)
	src := &float32Source
	if math.IsNaN(f) || math.IsInf(f, 0) {
		src = &nonFinite32Source
	}
	return Token{src: src, num: math.Float64bits(f)}
}

// Int constructs a Token of a JSON number of an int64.
func Int(n int64) Token {
	return Token{src: &intSource, num: uint64(n)}
}

// Uint constructs a Token of a JSON number of a uint64.
func Uint(n uint64) Token {
	return Token{src: &uintSource, num: n}
}

// Clone returns a copy of the token whose value is not in the buffer of a Decoder: it stays valid after the
// following calls of the Decoder. A token which a constructor made is returned as it is.
func (t Token) Clone() Token {
	if t.source().dec == nil {
		return t
	}
	if t.kind() == KindString {
		return Token{src: &rawStringSource, str: string(t.raw())}
	}
	return Token{src: &rawNumberSource, str: string(t.raw())}
}

// raw is the text of a raw token. It panics if the decoder has moved past the token.
func (t Token) raw() []byte {
	dec := t.source().dec
	if dec == nil {
		return []byte(t.str)
	}
	t.panicIfVoided()
	return dec.prevBuffer()
}

// panicIfVoided panics if t is a raw token which the decoder has moved past.
func (t Token) panicIfVoided() {
	if dec := t.source().dec; dec != nil && dec.prevOffset() != int64(t.num) {
		panic("invalid jsontext.Token; it has been voided by a subsequent json.Decoder call")
	}
}

// Kind returns the kind of the token. It panics for a raw token which the decoder has moved past.
func (t Token) Kind() Kind {
	return t.kind()
}

// kind is the kind of t. The kind of a raw token is the one of the text of the decoder at its offset, which a
// Reset of the decoder may change, as encoding/json/jsontext reads it.
func (t Token) kind() Kind {
	if t.src != nil && t.src.dec == nil {
		return t.src.kind // inlined, for the tokens which the encoder writes
	}
	return t.otherKind()
}

// otherKind is the kind of the zero Token and of a raw token.
func (t Token) otherKind() Kind {
	if t.src == nil {
		return KindInvalid
	}
	t.panicIfVoided()
	dec := t.src.dec
	return kindOf(dec.buf[dec.prevStart])
}

// Bool returns the value of a JSON boolean. It panics if the token is not a JSON boolean.
func (t Token) Bool() bool {
	switch t.kind() {
	case KindTrue:
		return true
	case KindFalse:
		return false
	}
	panic("invalid JSON token kind: " + t.kind().String())
}

// String returns the unescaped value of a JSON string. For a token of another kind, it returns its raw JSON
// text.
func (t Token) String() string {
	// It is small enough to be inlined: the text of a raw token, which is in the buffer of the decoder, is
	// converted by the caller, which doesn't allocate it where the string doesn't escape.
	s, b := t.text()
	if b != nil {
		return string(b)
	}
	return s
}

// text is the value of String, as a string or as the bytes of the buffer of a decoder.
func (t Token) text() (string, []byte) {
	switch t.source().form {
	case formNone:
		if t.kind() == KindInvalid {
			return "<invalid jsontext.Token>", nil
		}
		return t.kind().String(), nil
	case formRaw:
		k := t.kind()
		b := t.raw()
		if k != KindString {
			return "", b
		}
		if v := b[1 : len(b)-1]; bytes.IndexByte(v, '\\') < 0 && utf8.Valid(v) {
			return "", v // the value is the text between the quotes
		}
		s, _ := AppendUnquote(nil, b)
		return string(s), nil
	case formString:
		return t.str, nil
	}
	return string(t.appendNumber(nil)), nil
}

// appendNumber appends the text of a number of a Go value: for a float which is not finite, the text of its
// string, without the quotes.
func (t Token) appendNumber(b []byte) []byte {
	switch t.source().form {
	case formInt:
		return intfmt.AppendInt(b, int64(t.num))
	case formUint:
		return intfmt.AppendUint(b, t.num)
	case formFloat:
		return appendFloatText(b, math.Float64frombits(t.num), 64)
	default:
		return appendFloatText(b, math.Float64frombits(t.num), 32)
	}
}

// numError is the error of a method of Token which reads a number.
type numError struct {
	method string
	text   string
	err    error
}

func (e *numError) Error() string {
	return "jsontext.Token(" + e.text + ")." + e.method + " error: " + e.err.Error()
}

func (e *numError) Unwrap() error { return e.err }

// panicUnlessNumber panics unless the token is a JSON number.
func (t Token) panicUnlessNumber() {
	if t.kind() != KindNumber {
		panic("invalid JSON token kind: " + t.kind().String())
	}
}

// Int returns the signed integer value of a JSON number.
//
// It reports an error which matches strconv.ErrSyntax if the number is not in the restricted grammar of a
// signed integer, and one which matches strconv.ErrRange if it is a signed integer out of the range of an
// int64. It returns a reasonable value even with an error: the fraction is truncated toward zero, and a number
// out of the range is saturated at the closest end.
//
// It panics if the token is not a JSON number.
func (t Token) Int() (int64, error) {
	t.panicUnlessNumber()
	var n int64
	var err error
	switch t.source().form {
	case formInt:
		return int64(t.num), nil
	case formUint:
		if t.num > math.MaxInt64 {
			n, err = math.MaxInt64, strconv.ErrRange
		} else {
			n = int64(t.num)
		}
	case formFloat, formFloat32:
		n, err = floatToInt(math.Float64frombits(t.num))
	default:
		n, err = parseInt(t.raw())
	}
	if err != nil {
		return n, &numError{"Int", t.String(), err}
	}
	return n, nil
}

// Uint returns the unsigned integer value of a JSON number.
//
// It reports an error which matches strconv.ErrSyntax if the number is not in the restricted grammar of an
// unsigned integer, and one which matches strconv.ErrRange if it is an unsigned integer out of the range of a
// uint64. It returns a reasonable value even with an error: the fraction is truncated toward zero, and a number
// out of the range is saturated at the closest end.
//
// It panics if the token is not a JSON number.
func (t Token) Uint() (uint64, error) {
	t.panicUnlessNumber()
	var n uint64
	var err error
	switch t.source().form {
	case formUint:
		return t.num, nil
	case formInt:
		if int64(t.num) < 0 {
			err = strconv.ErrSyntax
		} else {
			n = t.num
		}
	case formFloat, formFloat32:
		n, err = floatToUint(math.Float64frombits(t.num))
	default:
		n, err = parseUint(t.raw())
	}
	if err != nil {
		return n, &numError{"Uint", t.String(), err}
	}
	return n, nil
}

// Float returns the floating-point value of a JSON number, parsed with 64 bits of precision.
//
// If the number is out of the range of a float64, it returns +Inf or -Inf with an error which matches
// strconv.ErrRange.
//
// It returns NaN, +Inf or -Inf for a JSON string of the value "NaN", "Infinity" or "-Infinity".
//
// It panics if the token is not a JSON number or one of those JSON strings.
func (t Token) Float() (float64, error) {
	return t.float(64)
}

// Float32 returns the floating-point value of a JSON number, parsed with 32 bits of precision.
//
// If the number is out of the range of a float32, it returns +Inf or -Inf with an error which matches
// strconv.ErrRange.
//
// It returns NaN, +Inf or -Inf for a JSON string of the value "NaN", "Infinity" or "-Infinity".
//
// It panics if the token is not a JSON number or one of those JSON strings.
//
// Most JSON libraries and standards take JSON numbers as 64-bit floating-point numbers: use this method only if
// the number is known to have only 32 bits of precision (as Float32 writes it). In any other case, use
// Token.Float.
func (t Token) Float32() (float32, error) {
	f, err := t.float(32)
	return float32(f), err
}

func (t Token) float(bits int) (float64, error) {
	if t.kind() == KindString {
		if f, ok := t.nonFinite(); ok {
			return f, nil
		}
	}
	t.panicUnlessNumber()
	switch t.source().form {
	case formInt:
		return float64(int64(t.num)), nil
	case formUint:
		return float64(t.num), nil
	case formFloat, formFloat32:
		f := math.Float64frombits(t.num)
		if bits == 32 && t.source().form == formFloat {
			if f32 := float64(float32(f)); math.IsInf(f32, 0) {
				return f32, &numError{"Float", t.String(), strconv.ErrRange}
			}
			f = float64(float32(f))
		}
		return f, nil
	}
	raw := t.raw()
	if bits == 64 {
		// a float32 is parsed by strconv: a float64 rounded to a float32 may differ from it by the rounding.
		if f, ok := jsonnum.ParseFloat(raw); ok {
			return f, nil
		}
	}
	f, err := strconv.ParseFloat(string(raw), bits)
	if err != nil {
		// the grammar of a JSON number is in the one of ParseFloat: the only error is the range.
		return f, &numError{"Float", string(raw), strconv.ErrRange}
	}
	return f, nil
}

// nonFinite is the float of a JSON string of the value "NaN", "Infinity" or "-Infinity".
func (t Token) nonFinite() (float64, bool) {
	var s string
	switch t.source().form {
	case formFloat, formFloat32:
		if f := math.Float64frombits(t.num); !math.IsNaN(f) {
			return f, true
		}
		return math.NaN(), true
	case formString:
		s = t.str
	case formRaw:
		s = t.String()
	}
	switch s {
	case "NaN":
		return math.NaN(), true
	case "Infinity":
		return math.Inf(+1), true
	case "-Infinity":
		return math.Inf(-1), true
	}
	return 0, false
}

// floatToInt is f truncated toward zero and saturated in the range of an int64, with an error of the syntax
// if f is not an integer, or else of the range if it is out of the range.
func floatToInt(f float64) (int64, error) {
	var err error
	if f != math.Trunc(f) {
		err = strconv.ErrSyntax
	}
	switch {
	case f >= 1<<63:
		if f > 1<<63 { // 2⁶³ is math.MaxInt64 rounded to a float64, as encoding/json/jsontext takes it
			err = orErr(err, strconv.ErrRange)
		}
		return math.MaxInt64, err
	case f < -1<<63:
		return math.MinInt64, orErr(err, strconv.ErrRange)
	}
	return int64(f), err
}

// floatToUint is floatToInt for a uint64: a negative number is out of the syntax of an unsigned integer.
func floatToUint(f float64) (uint64, error) {
	var err error
	if f != math.Trunc(f) || math.Signbit(f) {
		err = strconv.ErrSyntax
	}
	switch {
	case f >= 1<<64:
		if f > 1<<64 { // 2⁶⁴ is math.MaxUint64 rounded to a float64
			err = orErr(err, strconv.ErrRange)
		}
		return math.MaxUint64, err
	case f <= 0:
		return 0, err
	}
	return uint64(f), err
}

func orErr(err, other error) error {
	if err != nil {
		return err
	}
	return other
}

// parseInt is Int of the text of a JSON number.
func parseInt(b []byte) (int64, error) {
	neg := len(b) > 0 && b[0] == '-'
	digits := b
	if neg {
		digits = b[1:]
	}
	if n, ok := jsonnum.ParseUint(digits); ok {
		if neg {
			if n > 1<<63 {
				return math.MinInt64, strconv.ErrRange
			}
			return -int64(n), nil
		}
		if n > math.MaxInt64 {
			return math.MaxInt64, strconv.ErrRange
		}
		return int64(n), nil
	} else if isDigits(digits) {
		// an integer out of the range of a uint64
		if neg {
			return math.MinInt64, strconv.ErrRange
		}
		return math.MaxInt64, strconv.ErrRange
	}
	f := parseFloat64(b)
	n, _ := floatToInt(f)
	return n, strconv.ErrSyntax
}

// parseFloat64 is the float64 of the text of a JSON number, infinite if it is out of the range.
func parseFloat64(b []byte) float64 {
	if f, ok := jsonnum.ParseFloat(b); ok {
		return f
	}
	f, _ := strconv.ParseFloat(string(b), 64)
	return f
}

// parseUint is Uint of the text of a JSON number.
func parseUint(b []byte) (uint64, error) {
	if n, ok := jsonnum.ParseUint(b); ok {
		return n, nil
	} else if isDigits(b) {
		return math.MaxUint64, strconv.ErrRange
	}
	f := parseFloat64(b)
	n, _ := floatToUint(f)
	return n, strconv.ErrSyntax
}

// isDigits reports whether b is a JSON integer without a sign: 0, or digits which don't start with 0.
func isDigits(b []byte) bool {
	if len(b) == 0 || b[0] == '0' && len(b) > 1 {
		return false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
