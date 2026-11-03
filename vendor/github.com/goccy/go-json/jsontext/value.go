package jsontext

import (
	"bytes"
	"errors"
	"io"
	"math"
	"slices"
	"strconv"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/floatfmt"
)

// Value is a raw JSON value, one of:
//   - a JSON literal (i.e., null, true, or false)
//   - a JSON string (e.g., "hello, world!")
//   - a JSON number (e.g., 123.456)
//   - a whole JSON object (e.g., {"fizz":"buzz"} )
//   - a whole JSON array (e.g., [1,2,3] )
//
// A Value can hold a whole object or array, which a Token can't. A Value may have leading and trailing white
// space.
type Value []byte //nolint:recvcheck // the methods of encoding/json/jsontext.Value

// Clone returns a copy of v.
func (v Value) Clone() Value {
	return bytes.Clone(v)
}

// String returns the text of v, or null for a nil v.
func (v Value) String() string {
	if v == nil {
		return "null"
	}
	return string(v)
}

// Kind returns the kind of the first token of v. A valid value never starts with KindEndObject or
// KindEndArray.
func (v Value) Kind() Kind {
	// a value without leading white space, as most are, is looked at without a call
	if len(v) > 0 && v[0] > ' ' {
		return kindOf(v[0])
	}
	if i := skipSpace(v, 0); i < len(v) {
		return kindOf(v[i])
	}
	return KindInvalid
}

// MarshalJSON returns v as its JSON encoding, without validating it. It returns null for a nil v.
func (v Value) MarshalJSON() ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return v, nil
}

// UnmarshalJSON sets v to a copy of b, without validating it.
func (v *Value) UnmarshalJSON(b []byte) error {
	if v == nil {
		return errors.New("jsontext.Value: UnmarshalJSON on nil pointer")
	}
	*v = append((*v)[:0], b...)
	return nil
}

// IsValid reports whether v is valid JSON under the options.
//
// Without options, it validates v as RFC 7493 specifies: v is valid UTF-8, the escape sequences of the strings
// are of valid Unicode code points, and the names of each object are unique. It doesn't check whether the
// numbers fit in any numeric type (e.g., float64, int64 or uint64).
//
// The options which matter are AllowDuplicateNames and AllowInvalidUTF8; the others are ignored.
func (v Value) IsValid(opts ...Options) bool {
	e := getEncoder(opts)
	defer putEncoder(e)
	e.vs.write, e.vs.spaced = false, false
	return e.writeValue(v) == nil
}

// Format formats v in place.
//
// Without options, it validates v as RFC 7493 specifies, and writes it in its minimal form: without white
// space, and the strings in their shortest encoding.
//
// The options which matter are AllowDuplicateNames, AllowInvalidUTF8, EscapeForHTML, EscapeForJS,
// PreserveRawStrings, CanonicalizeRawInts, CanonicalizeRawFloats, ReorderRawObjects, SpaceAfterColon,
// SpaceAfterComma, Multiline, WithIndent and WithIndentPrefix; the others are ignored.
//
// It succeeds if v is valid under the same options. If v is formatted already, it is not changed.
func (v *Value) Format(opts ...Options) error {
	return v.format(nil, opts)
}

// Compact removes the white space of v.
//
// It doesn't change the representation of the strings and numbers. So that it formats as many values as it
// can, it takes values of duplicate names and invalid UTF-8.
//
// It is Value.Format with the options AllowDuplicateNames(true), AllowInvalidUTF8(true),
// PreserveRawStrings(true), Multiline(false), SpaceAfterColon(false) and SpaceAfterComma(false), and an empty
// indentation, followed by the options given.
func (v *Value) Compact(opts ...Options) error {
	return v.format(compactOptions, opts)
}

var compactOptions = []Options{
	AllowDuplicateNames(true), AllowInvalidUTF8(true), PreserveRawStrings(true), noWhiteSpace,
}

// noWhiteSpace sets the options of white space to none, so that an option given after it changes only itself.
var noWhiteSpace = &config{Set: multiline | spaceAfterColon | spaceAfterComma | indentSet}

// Indent formats the white space of v so that each element of an object or array starts a line which is
// indented by its depth.
//
// It doesn't change the representation of the strings and numbers. So that it formats as many values as it
// can, it takes values of duplicate names and invalid UTF-8.
//
// It is Value.Format with the options AllowDuplicateNames(true), AllowInvalidUTF8(true),
// PreserveRawStrings(true), Multiline(true), SpaceAfterColon(true) and WithIndent("\t"), followed by the options
// given.
func (v *Value) Indent(opts ...Options) error {
	return v.format(indentOptions, opts)
}

var indentOptions = []Options{
	AllowDuplicateNames(true), AllowInvalidUTF8(true), PreserveRawStrings(true),
	&config{Set: multiline | spaceAfterColon | indentSet, Value: multiline | spaceAfterColon, Indent: "\t"},
}

// Canonicalize formats v in the canonical form of the JSON Canonicalization Scheme (JCS) of RFC 8785: a value
// of the same meaning, which Canonicalize doesn't change.
//
// The strings are in their minimal form, the numbers are formatted as double precision numbers, the members of
// the objects are sorted by their names, and the white space is removed.
//
// It is Value.Format with the options CanonicalizeRawInts(true), CanonicalizeRawFloats(true),
// ReorderRawObjects(true), Multiline(false), SpaceAfterColon(false) and SpaceAfterComma(false), and an empty
// indentation, followed by the options given.
//
// JCS takes all JSON numbers as IEEE 754 double precision numbers: a number which needs more precision loses
// it. For example, integers beyond ±2⁵³ lose their precision. To keep the representation of the integers, set
// CanonicalizeRawInts to false:
//
//	v.Canonicalize(jsontext.CanonicalizeRawInts(false))
func (v *Value) Canonicalize(opts ...Options) error {
	return v.format(canonicalOptions, opts)
}

var canonicalOptions = []Options{CanonicalizeRawInts(true), CanonicalizeRawFloats(true), ReorderRawObjects(true), noWhiteSpace}

func (v *Value) format(first, opts []Options) error {
	e := getEncoder(opts, first...)
	e.buf = slices.Grow(e.buf, len(*v))
	defer putEncoder(e)
	e.vs.keep = true
	if err := e.writeValue(*v); err != nil {
		return err
	}
	if e.vs.kept {
		return nil // v is formatted already
	}
	if !bytes.Equal(*v, e.buf) {
		*v = append((*v)[:0], e.buf...)
	}
	return nil
}

// AppendFormat appends the JSON value src to dst, formatted as the options say. See Value.Format for how it
// formats values.
//
// dst and src may overlap. At an error, all of src is appended to dst.
func AppendFormat[Bytes ~[]byte | ~string](dst []byte, src Bytes, opts ...Options) ([]byte, error) {
	e := getEncoder(opts)
	defer putEncoder(e)
	e.buf = slices.Grow(e.buf, len(src))
	e.vs.keep = true
	// the value is only read, which a string may be without a copy.
	if err := e.writeValue(readOnlyBytes(src)); err != nil {
		return append(dst, src...), err
	}
	if e.vs.kept {
		return append(dst, src...), nil // src is formatted already
	}
	return append(dst, e.buf...), nil
}

// encoders are the encoders of the functions and methods which format a single value.
var encoders sync.Pool

// getEncoder returns an encoder of the options first and opts, which writes to its buffer only, without a line
// feed after the value.
func getEncoder(opts []Options, first ...Options) *encoder {
	e, _ := encoders.Get().(*encoder)
	if e == nil {
		e = new(encoder)
		e.vs.open = new([64]openObject)
	}
	e.cfg = config{}
	e.cfg.Apply(first)
	e.cfg.Apply(opts)
	e.cfg.Set |= omitTopLevelNewline
	e.cfg.Value |= omitTopLevelNewline
	e.init(nil)
	return e
}

func putEncoder(e *encoder) {
	// a large buffer is kept only if it was used well, so that one large value doesn't keep it for small ones.
	if cap(e.buf) > 64<<10 && len(e.buf) < cap(e.buf)/4 {
		e.buf = nil
	}
	// neither are the arrays of the names and of the reordered members of a large value.
	const large = 64 << 10
	if cap(e.vs.ro.buf) > large || cap(e.vs.ro.names) > large || cap(e.vs.ro.members) > large/32 {
		e.vs.ro = reorderer{}
	}
	if cap(e.st.names.buf) > large || cap(e.st.names.ends) > large/8 {
		e.st.names = names{}
	}
	e.vs.out = nil
	encoders.Put(e)
}

// AppendQuote appends src to dst as a JSON string, in its minimal form of RFC 8785, section 3.2.2.2, and returns
// the extended buffer. Invalid UTF-8 is appended as the Unicode replacement character, and reported by an error
// at the end. dst must not overlap src.
func AppendQuote[Bytes ~[]byte | ~string](dst []byte, src Bytes) ([]byte, error) {
	// the room of the quoted src is made first, as encoding/json/jsontext makes it: a dst which has less isn't
	// written to.
	dst = slices.Grow(dst, len(src)+2)
	b := readOnlyBytes(src)
	if overlaps(b, dst[len(dst):]) {
		// src in the room of dst, which the caller must not give: the output would be written over src, which
		// is read from a copy.
		b = bytes.Clone(b)
	}
	dst, valid := appendQuoted(dst, unsafe.String(unsafe.SliceData(b), len(b)), 0)
	if !valid {
		return dst, &SyntacticError{Err: errInvalidUTF8}
	}
	return dst, nil
}

// AppendUnquote appends the value of the JSON string src to dst and returns the extended buffer. src must be a
// JSON string, without white space around it. Invalid UTF-8 is appended as the Unicode replacement character,
// and reported by an error at the end. Bytes after the string are an error. dst must not overlap src.
func AppendUnquote[Bytes ~[]byte | ~string](dst []byte, src Bytes) ([]byte, error) {
	dst = slices.Grow(dst, len(src)) // not nil, as encoding/json/jsontext returns it, when src isn't empty
	b := []byte(src)
	if overlaps(b, dst[len(dst):]) {
		// src in the room of dst, which the caller must not give: the output would be written over src, which
		// is read from a copy, so that it doesn't panic.
		b = bytes.Clone(b)
	}
	if len(b) == 0 || b[0] != '"' {
		var err error = &SyntacticError{Err: invalidChar(b, placeStartOfString)}
		if len(b) == 0 {
			err = &SyntacticError{Err: io.ErrUnexpectedEOF}
		}
		return dst, err
	}
	n, f, err := scanString(b, 0)
	if err != nil {
		// the characters before the error
		return appendUnquotedPrefix(dst, b[:n], f), &SyntacticError{Err: err}
	}
	dst = appendUnquoted(dst, b[:n], f)
	if n < len(b) {
		return dst, &SyntacticError{Err: invalidChar(b[n:], placeAfterString)}
	}
	if f&strInvalidUTF8 != 0 {
		// the last invalid UTF-8 or escaped surrogate which is not in a pair, which were mangled
		var last error
		for i := 1; ; {
			i, _, err = scanStringFrom(b, i, 0, strValid)
			if err == nil {
				break
			}
			last = err
			if b[i] == '\\' {
				i += len(`\uXXXX`)
			} else {
				i++
			}
		}
		return dst, &SyntacticError{Err: last}
	}
	return dst, nil
}

// appendUnquotedPrefix appends the value of the start of a JSON string, which has no quote at its end, and which
// scanString took with the flags f up to an error: its escape sequences are complete.
func appendUnquotedPrefix(dst, b []byte, f strFlags) []byte {
	return appendUnquoted(dst, append(b[:len(b):len(b)], '"'), f)
}

// AppendFloat appends src to dst as a JSON number of RFC 8259, section 6, with bits of precision.
//
// Except for -0, which it writes as -0, it writes a number as ECMA-262, 10th edition, section 7.1.12.1 does, and,
// with 64 bits, as RFC 8785, section 3.2.2.3 does. NaN, +Inf and -Inf are written as NaN, +Inf and -Inf.
//
// Most JSON libraries and standards take JSON numbers as 64-bit floating-point numbers: use 64 bits of
// precision unless the reader of the number knows that it has 32 bits of precision.
func AppendFloat(dst []byte, src float64, bits int) []byte {
	if bits != 32 && bits != 64 {
		panic("illegal AppendFloat bit size")
	}
	if bits == 32 {
		src = float64(float32(src))
	}
	if math.IsNaN(src) || math.IsInf(src, 0) {
		return strconv.AppendFloat(dst, src, 'g', -1, bits)
	}
	return floatfmt.AppendFloat(dst, src, bits)
}
