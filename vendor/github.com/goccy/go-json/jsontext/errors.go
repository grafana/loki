package jsontext

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	ierrors "github.com/goccy/go-json/internal/errors"
)

// ErrDuplicateName reports that a JSON token could not be encoded or decoded because its name is already the
// name of another member of the same JSON object. It is wrapped directly by a SyntacticError, whose
// JSONPointer points to the duplicate name:
//
//	var serr *jsontext.SyntacticError
//	if errors.As(err, &serr) && serr.Err == jsontext.ErrDuplicateName {
//		ptr := serr.JSONPointer // JSON pointer to the duplicate member
//		name := ptr.LastToken() // the name itself
//		...
//	}
//
// It is only reported if AllowDuplicateNames is false.
var ErrDuplicateName = ierrors.ErrDuplicateName

// ErrNonStringName reports that a JSON token could not be encoded or decoded because it is not a string where
// a JSON object name is, which RFC 8259, section 4, requires to be a string. It is wrapped directly by a
// SyntacticError.
var ErrNonStringName = ierrors.ErrNonStringName

// errInvalidUTF8 reports invalid UTF-8 in a JSON string.
var errInvalidUTF8 = ierrors.ErrInvalidUTF8

// The errors of the grammar, which an encoder reports where a token doesn't fit the structure.
var (
	errMismatchDelim = errors.New("mismatching structural token for object or array")
	errMissingValue  = errors.New("missing value after object name")
	errMaxDepth      = errors.New("exceeded max depth")
	errInvalidToken  = errors.New("invalid jsontext.Token")
	// errInvalidNamespace reports a write to an encoder after MarshalEncode of the v2 json package failed within
	// an object or an array it opened.
	errInvalidNamespace = errors.New("object namespace is in an invalid state")
	errEndOfPlace       = errors.New("cannot end the object or array which the value being written is in")
	// errEndOfReadPlace is the error of a decoder attached at the place of a value, which a method or a function of
	// unmarshaling reads, which reads the end of the object or the array the value is in.
	errEndOfReadPlace = errors.New("cannot read the end of the object or array which the value being read is in")
)

// SyntacticError describes an error of the JSON grammar found while encoding or decoding.
//
// The contents of this error, as this package makes it, may change over time.
type SyntacticError struct {
	_ [0]func() // not comparable

	// ByteOffset is the offset in the input or output at or after which the error occurred.
	ByteOffset int64
	// JSONPointer points to the JSON value within which the error occurred (see RFC 6901).
	JSONPointer Pointer

	// Err is the underlying error.
	Err error
}

func (e *SyntacticError) Error() string {
	if e.Err == ErrDuplicateName {
		s := "jsontext: " + e.Err.Error() + " " + strconv.Quote(e.JSONPointer.LastToken())
		if parent := e.JSONPointer.Parent(); parent != "" {
			s += " within " + strconv.Quote(ierrors.ShortPointer(string(parent)))
		}
		return s
	}
	b := []byte("jsontext: syntactic error")
	if e.Err != nil {
		b = append(b[:len("jsontext: ")], e.Err.Error()...)
	}
	if e.JSONPointer != "" {
		b = append(b, " within "...)
		b = strconv.AppendQuote(b, ierrors.ShortPointer(string(e.JSONPointer)))
	}
	if e.ByteOffset > 0 {
		b = append(b, " after offset "...)
		b = strconv.AppendInt(b, e.ByteOffset, 10)
	}
	return string(b)
}

func (e *SyntacticError) Unwrap() error { return e.Err }

// ioError is an error of the reader of a Decoder or the writer of an Encoder.
type ioError struct {
	write bool
	err   error
}

func (e *ioError) Error() string {
	if e.write {
		return "jsontext: write error: " + e.err.Error()
	}
	return "jsontext: read error: " + e.err.Error()
}

func (e *ioError) Unwrap() error { return e.err }

// errBufferWriteAfterNext reports a write to the bytes.Buffer which a Decoder reads in place.
var errBufferWriteAfterNext = errors.New("invalid bytes.Buffer.Write call after calling bytes.Buffer.Next")

// textError is an error of the text of a token: an invalid character or escape sequence. It keeps what the
// message is made of, so that encoding/json, which reports the syntax errors in its own words, takes them from
// it ( see legacySyntaxError ).
type textError struct {
	label string // "character", "escape sequence" or "surrogate pair"
	what  string // the invalid text
	place charPlace
	// literal and expect are the literal and the character expected in it, for placeInLiteral.
	literal string
	expect  byte
}

// charPlace is where an invalid character is in the grammar, as an error says it.
type charPlace uint8

const (
	placeInNumber charPlace = iota
	placeInString
	placeStartOfValue
	placeStartOfString
	placeAfterTopLevel
	placeAfterString
	placeAfterObjectValue
	placeAfterObjectName
	placeAfterArrayValue
	placeAfterArrayElement
	placeInLiteral
	placeInEscape // in the string of an escape sequence
)

// placeTexts are the words of the places, as encoding/json/jsontext and encoding/json say them.
var placeTexts = [...]struct{ text, legacy string }{
	placeInNumber:          {"in number (expecting digit)", "in numeric literal"},
	placeInString:          {"in string (expecting non-control character)", "in string"},
	placeStartOfValue:      {"at start of value", "looking for beginning of value"},
	placeStartOfString:     {"at start of string (expecting '\"')", "looking for beginning of object key string"},
	placeAfterTopLevel:     {"after top-level value", "after top-level value"},
	placeAfterString:       {"after string value", "after string value"},
	placeAfterObjectValue:  {"after object value (expecting ',' or '}')", "after object key:value pair"},
	placeAfterObjectName:   {"after object name (expecting ':')", "after object key"},
	placeAfterArrayValue:   {"after array value (expecting ',' or ']')", "after array value"},
	placeAfterArrayElement: {"after array element (expecting ',' or ']')", "after array element"},
	placeInEscape:          {"in string", "in string"},
}

func (e *textError) Error() string { return e.message(false) }

// message is the message of the error, in the words of encoding/json/jsontext, or of encoding/json if legacy is
// set: without what the grammar expects, but in a literal, and with its names for the places.
func (e *textError) message(legacy bool) string {
	var where string
	switch {
	case e.place == placeInLiteral:
		where = "in literal " + e.literal + " (expecting " + strconv.QuoteRune(rune(e.expect)) + ")"
	case legacy:
		where = placeTexts[e.place].legacy
	default:
		where = placeTexts[e.place].text
	}
	if e.label == "character" {
		return "invalid character " + quoteChar([]byte(e.what)) + " " + where
	}
	return "invalid " + e.label + " " + quoteEscape(e.what) + " " + where
}

// invalidChar is the error of the character at the start of b, which is not expected at the place.
func invalidChar(b []byte, place charPlace) error {
	_, n := utf8.DecodeRune(b)
	return &textError{label: "character", what: string(b[:n]), place: place}
}

// invalidLiteralChar is the error of the character at the start of b, which is not the character i of lit.
func invalidLiteralChar(b []byte, lit string, i int) error {
	_, n := utf8.DecodeRune(b)
	return &textError{label: "character", what: string(b[:n]), place: placeInLiteral, literal: lit, expect: lit[i]}
}

// quoteChar quotes the character at the start of b as a Go rune literal, or its first byte if it isn't valid
// UTF-8.
func quoteChar(b []byte) string {
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n <= 1 {
		if len(b) == 0 {
			return "''"
		}
		return `'\x` + strconv.FormatUint(uint64(b[0])>>4, 16) + strconv.FormatUint(uint64(b[0])&0xf, 16) + `'`
	}
	return strconv.QuoteRune(r)
}

// invalidEscape is the error of the escape sequence at the start of b, the label says of which kind.
func invalidEscape(b []byte, label string) error {
	return &textError{label: label, what: string(b), place: placeInEscape}
}

// quoteEscape quotes an escape sequence by backquotes if it can be read so, and else as a Go string: as
// encoding/json/jsontext quotes it, U+FFFD, whether it is in the text or stands for invalid UTF-8, is quoted so.
func quoteEscape(s string) string {
	if strings.ContainsFunc(s, func(r rune) bool {
		return r == '`' || r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) {
		return strconv.Quote(s)
	}
	return "`" + s + "`"
}
