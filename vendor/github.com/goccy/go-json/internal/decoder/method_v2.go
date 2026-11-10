package decoder

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

// The methods of unmarshaling of the semantics of encoding/json/v2, and the time types. A value is always
// addressable for them: the method of the pointer to a type is called for a value of the type, at its address.
// The first of them which a type has decodes its value: UnmarshalJSONFrom, UnmarshalJSON, UnmarshalText. A
// time.Time and a time.Duration are decoded as their own, before their methods, and a function of WithUnmarshalers
// which takes the type comes before all of them.

// The methods of unmarshaling, in the order of their precedence.
const (
	noMethod = iota
	methodUnmarshalJSONFrom
	methodUnmarshalJSON
	methodUnmarshalText
)

// V2Hooks are what the decoder needs of the v2 json package, which it can't import: the decoding by the methods
// which read a jsontext.Decoder, which the package sets.
var V2Hooks struct {
	// UnmarshalerFromType is the type of the interface json.UnmarshalerFrom.
	UnmarshalerFromType reflect.Type
	// UnmarshalFrom decodes the value at cursor into the value at p, of typ, by its UnmarshalJSONFrom: it returns
	// the cursor after the value, or ErrUseDefault if the method declined the value.
	UnmarshalFrom func(ctx *RuntimeContext, cursor, depth int64, typ reflect.Type, p unsafe.Pointer) (int64, error)
	// SkipStreamValue skips the value of the stream, nested in depth objects and arrays, which the buffer doesn't
	// hold whole, by a jsontext.Decoder attached at its place ( see V2Call.Value ), which checks it as
	// encoding/json/v2 checks a value it skips: it returns the error at its place.
	SkipStreamValue func(s *Stream, depth int64) error
	// Lend sets up the stream of the call of UnmarshalDecode of ctx, which decodes its value in the buffer of its
	// decoder without it ( see V2Call.Lent ), as the stream is while it decodes the value whole ( see
	// Stream.decodeWholeV2 ): for a method or a function of the value which reads a jsontext.Decoder, attached at
	// its place, or for its error.
	Lend func(ctx *RuntimeContext)
	// AppendUnquote is jsontext.AppendUnquote, which reads a JSON string in a JSON string of the `string` option with
	// StringifyWithLegacySemantics, and whose error is the one of the value.
	AppendUnquote func(dst []byte, src string) ([]byte, error)
}

// v2MethodOf returns the first method of unmarshaling from first on which decodes a value of the type, or noMethod.
// A pointer and an interface value have none: the value they point to or hold has, which is addressable.
func v2MethodOf(typ reflect.Type, first int) int {
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Interface {
		return noMethod
	}
	ptr := reflect.PointerTo(typ)
	switch {
	case first <= methodUnmarshalJSONFrom && V2Hooks.UnmarshalerFromType != nil && ptr.Implements(V2Hooks.UnmarshalerFromType):
		return methodUnmarshalJSONFrom
	case first <= methodUnmarshalJSON && ptr.Implements(unmarshalJSONType):
		return methodUnmarshalJSON
	case ptr.Implements(unmarshalTextType):
		return methodUnmarshalText
	}
	return noMethod
}

// v2UnmarshalFromDecoder is the decoder of a value whose UnmarshalJSONFrom decodes it, from a jsontext.Decoder at
// its place in the input ( see V2Hooks.UnmarshalFrom ), and next of a value the method declines: the decoder of its
// next method, or of its kind.
type v2UnmarshalFromDecoder struct {
	typ  reflect.Type
	next Decoder
}

func (d *v2UnmarshalFromDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	copyStreamNames(ctx)
	end, err := V2Hooks.UnmarshalFrom(ctx, cursor, depth, d.typ, p)
	if err == ErrUseDefault {
		return d.next.Decode(ctx, cursor, depth, p)
	}
	return end, err
}

func (d *v2UnmarshalFromDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// ReadValue reads the value at cursor whole, as the method of unmarshaling, or the function, of a value of typ is
// given it: it must have the text which v2 allows ( see skipValueV2 ). It returns the start and the end of the
// value. An error of the text is one of the value, at its start, which the v2 json package reads whole to find it
// again, as encoding/json/v2 reads it.
func (ctx *RuntimeContext) ReadValue(cursor, depth int64, typ reflect.Type, method errors.UnmarshalMethod) (int64, int64, error) {
	start := skipWhiteSpace(ctx.Buf, cursor)
	end, err := ctx.skipValueV2(start, depth)
	if err != nil {
		return 0, 0, &errors.UnmarshalError{GoType: typ, Start: start, Err: err, Method: method}
	}
	return start, end, nil
}

// readTextV2 reads the value at cursor of a value of typ which method, or the decoder of a time, is given the text of:
// a string of ASCII without an escape, which most are, is read once, and is its text, as it has nothing which v2
// rejects; any other value is read whole by ReadValue, and a string is decoded. It returns the text of a string, or
// nil, and the start and the end of the value. The text is a part of the input, which the method must copy to keep
// it, as encoding.TextUnmarshaler says, and ends at its length: a method which appends to it doesn't write into the
// input after it.
func readTextV2(ctx *RuntimeContext, cursor, depth int64, typ reflect.Type, method errors.UnmarshalMethod) ([]byte, int64, int64, error) {
	buf := ctx.Buf
	if start := skipWhiteSpace(buf, cursor); buf[start] == '"' {
		if q := plainStringEnd(buf, start+1); q >= 0 {
			return buf[start+1 : q : q], start, q + 1, nil
		}
	}
	start, end, err := ctx.ReadValue(cursor, depth, typ, method)
	if err != nil || buf[start] != '"' {
		return nil, start, end, err
	}
	text := unquotedName(buf[start:end])
	return text[:len(text):len(text)], start, end, nil
}

// v2UnmarshalJSONDecoder is the decoder of a value whose UnmarshalJSON decodes it, which is given the value as the
// input has it, null included. The bytes are a part of the input, which the method must copy to keep them, as the
// interface of encoding/json/v2 says.
type v2UnmarshalJSONDecoder struct {
	typ     reflect.Type
	ptrType unsafe.Pointer
}

func (d *v2UnmarshalJSONDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	start, end, err := ctx.ReadValue(cursor, depth, d.typ, errors.UnmarshalJSONMethod)
	if err != nil {
		return 0, err
	}
	v := *(*any)(unsafe.Pointer(&emptyInterface{typ: d.ptrType, ptr: p}))
	if err := v.(json.Unmarshaler).UnmarshalJSON(ctx.Buf[start:end:end]); err != nil {
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: start, Err: err, Method: errors.UnmarshalJSONMethod}
	}
	return end, nil
}

func (d *v2UnmarshalJSONDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// errNonStringValue is the error of a value of a type whose UnmarshalText decodes it, which is not a string.
var errNonStringValue = fmt.Errorf("JSON value must be string type")

// v2UnmarshalTextDecoder is the decoder of a value whose UnmarshalText decodes it, from a string: null is the zero
// value, which the method is not called for, and any other value is an error.
type v2UnmarshalTextDecoder struct {
	typ     reflect.Type
	ptrType unsafe.Pointer
	zero    zeroer
}

func (d *v2UnmarshalTextDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	text, start, end, err := readTextV2(ctx, cursor, depth, d.typ, errors.UnmarshalTextMethod)
	if err != nil {
		return 0, err
	}
	switch ctx.Buf[start] {
	case 'n':
		if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			d.zero.zero(p)
		}
		return end, nil
	case '"':
	default:
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: start, Err: errNonStringValue, Method: errors.UnmarshalTextMethod}
	}
	v := *(*any)(unsafe.Pointer(&emptyInterface{typ: d.ptrType, ptr: p}))
	if err := v.(encoding.TextUnmarshaler).UnmarshalText(text); err != nil {
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: start, Err: err, Method: errors.UnmarshalTextMethod}
	}
	return end, nil
}

func (d *v2UnmarshalTextDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// v2TimeDecoder is the decoder of a time.Time, from a string of RFC 3339, which time.Time.UnmarshalText reads, but
// what RFC 3339 doesn't allow and the time package does: an hour of one digit, a comma before the fraction of a
// second, and a time zone out of range. null is the zero time.
type v2TimeDecoder struct{}

func (d v2TimeDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	text, start, end, err := readTextV2(ctx, cursor, depth, timeType, errors.UnmarshalTextMethod)
	if err != nil {
		return 0, err
	}
	switch ctx.Buf[start] {
	case 'n':
		if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			*(*time.Time)(p) = time.Time{}
		}
		return end, nil
	case '"':
	default:
		return 0, &errors.UnmarshalError{GoType: timeType, Start: start, Method: errors.UnmarshalTextMethod}
	}
	var t time.Time
	if err := t.UnmarshalText(text); err != nil {
		return 0, &errors.UnmarshalError{GoType: timeType, Start: start, Err: err, Method: errors.UnmarshalTextMethod}
	}
	if ctx.Option.V2&V2ParseTimeWithLooseRFC3339 == 0 {
		// what RFC 3339 doesn't allow, unless ParseTimeWithLooseRFC3339
		if err := strictRFC3339Error(text); err != nil {
			return 0, &errors.UnmarshalError{GoType: timeType, Start: start, Err: err, Method: errors.UnmarshalTextMethod}
		}
	}
	*(*time.Time)(p) = t
	return end, nil
}

func (d v2TimeDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// strictRFC3339Error returns the error of b, a time which time.Time.UnmarshalText read, which RFC 3339 doesn't allow,
// as encoding/json/v2 reports it, or nil.
func strictRFC3339Error(b []byte) error {
	parseError := func(valueElem, layoutElem, message string) error {
		return &time.ParseError{Layout: time.RFC3339, Value: string(b), LayoutElem: layoutElem, ValueElem: valueElem, Message: message}
	}
	switch {
	case b[len("2006-01-02T")+1] == ':':
		// the hour must have two digits
		return parseError(string(b[len("2006-01-02T"):][:1]), "15", "")
	case b[len("2006-01-02T15:04:05")] == ',':
		// the fraction of a second must follow a period
		return parseError(",", ".", "")
	case b[len(b)-1] != 'Z':
		// the zone is a string of the error only, as a time of a zone has no error but these
		zone := b[len(b)-len("Z07:00"):]
		switch {
		case dec2(zone[len("Z"):]) >= 24:
			return parseError(string(zone), "Z07:00", ": timezone hour out of range")
		case dec2(zone[len("Z07:"):]) >= 60:
			return parseError(string(zone), "Z07:00", ": timezone minute out of range")
		}
	}
	return nil
}

// dec2 returns the number of the two decimal digits at the start of b, or a number over 99 if they are not digits.
func dec2(b []byte) int {
	if len(b) < 2 || b[0]-'0' > 9 || b[1]-'0' > 9 {
		return 100
	}
	return int(b[0]-'0')*10 + int(b[1]-'0')
}
