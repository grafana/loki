//go:build go1.27 && goexperiment.jsonv2

package decoder

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

// newTypeError returns the type error as encoding/json of Go 1.27, which is made of encoding/json/v2, reports it.
// Offset is after the value, or after the key of a map entry. Struct is the name of the root type, and Field the
// path from it to the value: the names of the fields, the indices of the elements and the keys of the entries.
// An error at the root has neither.
// jsonPointerEscaper escapes a token of a JSON pointer ( RFC 6901 ).
var jsonPointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func newTypeError(p *pendingTypeError, path []typeErrorStep, root reflect.Type) *errors.UnmarshalTypeError {
	e := &errors.UnmarshalTypeError{Value: p.value, Type: p.typ, Offset: p.end, Err: p.err}
	if !p.literal && (p.kind == arrayValue || p.kind == objectValue) {
		e.Offset = p.start + 1
	}
	if p.noContext {
		e.Offset = 0
		return e
	}
	if len(path) > 0 {
		if root.Kind() == reflect.Pointer {
			root = root.Elem()
		}
		e.Struct = root.Name()
		names := make([]string, len(path))
		for i, step := range path {
			names[i] = step.name
			if step.isField {
				// the key of a field as the input has it
				names[i] = step.inputName
			}
			// encoding/json of Go 1.27 makes the path of the tokens of the JSON pointer of the value, which are
			// escaped as a JSON pointer escapes them
			names[i] = jsonPointerEscaper.Replace(names[i])
		}
		e.Field = strings.Join(names, ".")
	}
	return e
}

// base64Error records the error of the decoding of the string between start and end into typ, which is not
// base64: encoding/json of Go 1.27 reports it as a type error of the string, whose cause is the error.
func (ctx *RuntimeContext) base64Error(start, end int64, typ reflect.Type, err error) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{typ: typ, start: start, end: end, value: "string", kind: stringValue, err: err}
	}
}

// numberKindError records the type error of the value at cursor, of another kind than a number or a string,
// which is decoded into a json.Number, and skips it: encoding/json of Go 1.27 reports it with no path and no
// offset.
func (ctx *RuntimeContext) numberKindError(cursor, depth int64, typ reflect.Type) (int64, error) {
	first := ctx.typeError == nil
	next, err := ctx.skipTypeError(cursor, depth, typ)
	if err == nil && first {
		ctx.typeError.noContext = true
	}
	return next, err
}

// numberStringError records the type error of the string between start and end, which is not a number, decoded
// into a json.Number: encoding/json of Go 1.27 reports the string, with no path and no offset.
func (ctx *RuntimeContext) numberStringError(start, end int64, typ reflect.Type) (int64, error) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{
			typ: typ, start: start, end: end, value: "string " + string(ctx.Buf[start:end]), kind: stringValue,
			literal: true, err: strconv.ErrSyntax, noContext: true,
		}
	}
	return end, nil
}

// stringOptionUnquoted records the type error of the value at cursor of a field of the string option, which is not
// in a string, and skips it: encoding/json of Go 1.27 reports the value as a value of the type of the field.
func (ctx *RuntimeContext) stringOptionUnquoted(cursor, depth int64, typ reflect.Type) (int64, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == jsonNumberType {
		return ctx.numberKindError(cursor, depth, typ)
	}
	return ctx.skipTypeError(cursor, depth, typ)
}

// methodError records err, which an unmarshal method of the value between cursor and end returned, if it is the
// first error, and returns end: encoding/json of Go 1.27 goes on after it, as after a type error, and returns it as
// it is.
func (ctx *RuntimeContext) methodError(_, end int64, err error, _, _ string) (int64, error) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{plain: err}
	}
	return end, nil
}

// timeKindTypeErrors is whether a value of a time.Time which is not a string or null is a type error: encoding/json
// of Go 1.27 reports it as a type error, after which the decoding goes on.
const timeKindTypeErrors = true

// stringOptionNumber reports whether the bytes of the string of a json.Number of the string option are stored as
// they are: encoding/json of Go 1.27 stores a number by the grammar of the numbers, and nothing else.
func stringOptionNumber(value []byte) bool {
	return isValidNumber(value)
}

// stringOptionNumberDecoded is whether the bytes of the string of a json.Number of the string option, when they are
// not a number, are decoded as a JSON value: encoding/json of Go 1.27 reports them as a type error.
const stringOptionNumberDecoded = false

// stringOptionError records the type error of the string between start and end of a field of the string option,
// whose bytes are not a value of typ: encoding/json of Go 1.27 reports the bytes as a number for a number type,
// and else the string, whose cause is a syntax error.
func (ctx *RuntimeContext) stringOptionError(start, end int64, value []byte, typ reflect.Type) (int64, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == jsonNumberType {
		return ctx.numberStringError(start, end, typ)
	}
	if ctx.typeError != nil {
		return end, nil
	}
	p := &pendingTypeError{typ: typ, start: start, end: end, kind: stringValue, literal: true}
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		p.value = "number " + string(value)
	case reflect.String:
		// the bytes are decoded as a JSON string, whose syntax error is the cause
		p.value = "string"
		p.err = stringOptionStringError(value)
	default:
		p.value = "string " + string(ctx.Buf[start:end])
		p.err = strconv.ErrSyntax
	}
	ctx.typeError = p
	return end, nil
}

// stringOptionUnquotedAllocates is whether a nil pointer of a field of the string option is set to a zero value
// when the value of the field is not in a string: encoding/json of Go 1.27 sets it.
const stringOptionUnquotedAllocates = true

// StreamOffsetBase returns the position which the offsets of the type errors of a value of a stream are relative to,
// of the end of the previous value and of the start of the value: encoding/json of Go 1.27 reports them from the
// start of the value.
func StreamOffsetBase(_, valueStart int64) int64 {
	return valueStart
}

// stringOptionAllocates reports whether a nil pointer of a field of the string option is set to a zero value when
// the value in the string of the field is not of its type: encoding/json of Go 1.27 sets it.
func stringOptionAllocates([]byte) bool {
	return true
}

// errTextUnmarshalerKind is the cause of the type error of a value which is not a string, of a type which
// implements encoding.TextUnmarshaler, as encoding/json of Go 1.27 reports it.
var errTextUnmarshalerKind = stderrors.New("JSON value must be string type")

// textUnmarshalerKindError records the type error of the value at cursor, which is not a string, of a type whose
// pointer type typ implements encoding.TextUnmarshaler, and skips it: encoding/json of Go 1.27 reports the type
// of the value, and the cause of the error.
func (ctx *RuntimeContext) textUnmarshalerKindError(cursor, depth int64, typ reflect.Type) (int64, error) {
	first := ctx.typeError == nil
	next, err := ctx.skipTypeError(cursor, depth, typ.Elem())
	if err == nil && first {
		ctx.typeError.err = errTextUnmarshalerKind
	}
	return next, err
}

// ifaceTextUnmarshalerKindError records the type error of the value at cursor, which is not a string, of an
// interface value of ifaceType which holds a pointer of typ, which implements encoding.TextUnmarshaler, and skips
// it: encoding/json of Go 1.27 reports it as the one of the pointer ( see textUnmarshalerKindError ).
func (ctx *RuntimeContext) ifaceTextUnmarshalerKindError(cursor, depth int64, _, typ reflect.Type) (int64, error) {
	return ctx.textUnmarshalerKindError(cursor, depth, typ)
}

// keptInterfaceTypeError records the type error of the value at cursor, which is not null, decoded into the
// value at p of the interface type typ, which has methods and holds no pointer, and skips it: encoding/json of Go
// 1.27 sets the interface value to nil, and reports the error at the start of the value.
func (ctx *RuntimeContext) keptInterfaceTypeError(cursor, depth int64, typ reflect.Type, p unsafe.Pointer) (int64, error) {
	*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
	first := ctx.typeError == nil
	next, err := ctx.skipTypeError(cursor, depth, typ)
	if err == nil && first {
		ctx.typeError.atStart = true
	}
	return next, err
}

// textUnmarshalerNullSetsZero reports whether null sets a value of the kind, whose pointer type implements
// encoding.TextUnmarshaler, to its zero value: encoding/json of Go 1.27 leaves every such value as it is.
func textUnmarshalerNullSetsZero(reflect.Kind) bool {
	return false
}

// errUnsettableEmbeddedPointer is the cause which encoding/json of Go 1.27 reports for a field promoted from an
// embedded pointer to an unexported struct, which can't be set.
var errUnsettableEmbeddedPointer = stderrors.New("cannot set embedded pointer to unexported struct type")

// unsettableFieldError records the type error of the value at cursor of a field of the struct typ which can't be
// set, as an embedded pointer to an unexported struct, and skips the value: encoding/json of Go 1.27 reports it as
// a type error of the struct at the start of the value.
func (ctx *RuntimeContext) unsettableFieldError(cursor, depth int64, typ reflect.Type, _ error) (int64, error) {
	first := ctx.typeError == nil
	next, err := ctx.skipTypeError(cursor, depth, typ)
	if err == nil && first {
		ctx.typeError.err = errUnsettableEmbeddedPointer
		ctx.typeError.atStart = true
	}
	return next, err
}

// storesFloatsOutOfRange reports whether a number out of the range of a float is stored as ±Inf with its type
// error, as encoding/json of Go 1.27 does.
const storesFloatsOutOfRange = true

// floatRangeErrorOfInterface records the type error of the number between start and end, which is out of the
// range of a float64, decoded into the interface{} at p, and stores f, which is ±Inf, as encoding/json of Go 1.27
// does.
func (ctx *RuntimeContext) floatRangeErrorOfInterface(start, end int64, f float64, p unsafe.Pointer) {
	**(**any)(unsafe.Pointer(&p)) = ctx.boxFloat(f)
	ctx.numberTypeError(start, end, float64Type)
}

// stringOptionNumberStart reports whether c starts a number of a string of the string option which encoding/json
// reads: encoding/json of Go 1.27 reads whatever strconv reads.
func stringOptionNumberStart(byte) bool {
	return true
}

// mapKeySupported reports whether encoding/json of Go 1.27 decodes the keys of a map of keyType, which dec
// decodes: any key but a bool and a key of a type which no key is decoded into.
func mapKeySupported(keyType reflect.Type, dec Decoder) bool {
	if _, ok := dec.(*invalidDecoder); ok {
		return false
	}
	return keyType.Kind() != reflect.Bool
}

// unsupportedMapKeys records the type error of the first key of the object at cursor of a map whose keys are not
// decoded ( see mapKeySupported ), and skips the object: encoding/json of Go 1.27 reports the key as a string of
// the type of the keys, and makes the map.
func (ctx *RuntimeContext) unsupportedMapKeys(d *mapDecoder, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	if *(*unsafe.Pointer)(p) == nil {
		*(*unsafe.Pointer)(p) = reflect.MakeMapWithSize(d.mapType, 0).UnsafePointer()
	}
	cursor = skipWhiteSpace(buf, cursor) + 1
	for {
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] == '}' {
			return cursor + 1, nil
		}
		keyStart := cursor
		keyEnd, err := skipValue(buf, cursor, depth)
		if err != nil {
			return 0, err
		}
		if ctx.typeError == nil {
			ctx.typeError = &pendingTypeError{
				typ: d.keyType, start: keyStart, end: keyEnd, value: "string", kind: stringValue, atKey: true,
				literal: true,
			}
		}
		cursor = skipWhiteSpace(buf, keyEnd)
		if buf[cursor] != ':' {
			return 0, errors.ErrExpected("colon after object key", cursor)
		}
		end, err := skipValue(buf, skipWhiteSpace(buf, cursor+1), depth)
		if err != nil {
			return 0, err
		}
		cursor = skipWhiteSpace(buf, end)
		switch buf[cursor] {
		case ',':
			cursor++
		case '}':
			return cursor + 1, nil
		default:
			return 0, errors.ErrExpected("comma after object value", cursor)
		}
	}
}

// stringOptionStringError returns the syntax error of the bytes of a string of the string option of a string,
// which are not a JSON string and nothing else, as encoding/json of Go 1.27 reports it.
func stringOptionStringError(value []byte) error {
	if len(value) == 0 {
		return errors.ErrSyntax("unexpected end of JSON input", 0)
	}
	if value[0] != '"' {
		return errors.ErrSyntax(fmt.Sprintf("invalid character %s looking for beginning of object key string", quoteChar(value[0])), 0)
	}
	i := 1
	for {
		if i >= len(value) {
			return errors.ErrSyntax("unexpected end of JSON input", 0)
		}
		c := value[i]
		if c == '"' {
			i++
			break
		}
		if c != '\\' {
			i++
			continue
		}
		if i+1 >= len(value) {
			return errors.ErrSyntax("unexpected end of JSON input", 0)
		}
		switch value[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			i += 2
			continue
		case 'u':
			if i+6 <= len(value) && isHex(value[i+2:i+6]) {
				i += 6
				continue
			}
			return errors.ErrSyntax(fmt.Sprintf("invalid escape sequence `%s` in string", value[i:min(i+6, len(value))]), 0)
		}
		return errors.ErrSyntax(fmt.Sprintf("invalid escape sequence `%s` in string", value[i:i+2]), 0)
	}
	if i < len(value) {
		return errors.ErrSyntax(fmt.Sprintf("invalid character %s after string value", quoteChar(value[i])), 0)
	}
	return nil
}

// isHex reports whether every byte of b is a hexadecimal digit.
func isHex(b []byte) bool {
	for _, c := range b {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}
