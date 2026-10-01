//go:build !go1.27 || !goexperiment.jsonv2

package decoder

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

// newTypeError returns the type error as encoding/json before Go 1.27, and with GOEXPERIMENT=nojsonv2, reports
// it. Offset is after a literal and after the bracket of an array or an object, and at the first byte of a key.
// Struct is the struct type of the last field of the path, and Field the names of its fields, through their
// embedded fields since Go 1.24 ( see embeddedFieldNames ); the elements and the entries of the path have no name. An error in no field has neither.
func newTypeError(p *pendingTypeError, path []typeErrorStep, _ reflect.Type) *errors.UnmarshalTypeError {
	e := &errors.UnmarshalTypeError{Value: p.value, Type: p.typ, Offset: p.end, Err: p.err}
	if p.noContext {
		return e
	}
	switch {
	case p.atKey:
		e.Offset = p.start + 1
	case !p.literal && (p.kind == arrayValue || p.kind == objectValue):
		e.Offset = p.start + 1
	}
	var names []string
	for _, step := range path {
		if !step.isField {
			continue
		}
		e.Struct = step.structName
		names = append(append(names, step.embedded...), step.name)
	}
	e.Field = strings.Join(names, ".")
	return e
}

// base64Error records the error of the decoding of the string between start and end into typ, which is not
// base64: encoding/json before Go 1.27 returns the error of the decoding as it is.
func (ctx *RuntimeContext) base64Error(_, _ int64, _ reflect.Type, err error) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{plain: err}
	}
}

// numberKindError records the type error of the value at cursor, of another kind than a number or a string,
// which is decoded into a json.Number, and skips it.
func (ctx *RuntimeContext) numberKindError(cursor, depth int64, typ reflect.Type) (int64, error) {
	return ctx.skipTypeError(cursor, depth, typ)
}

// numberStringError returns the error of the string between start and end, which is not a number, decoded into
// a json.Number: encoding/json before Go 1.27 stops the decoding with it.
func (ctx *RuntimeContext) numberStringError(start, end int64, _ reflect.Type) (int64, error) {
	return 0, fmt.Errorf("json: invalid number literal, trying to unmarshal %q into Number", ctx.Buf[start:end])
}

// stringOptionUnquoted records the error of the value at cursor of a field of the string option, which is not in
// a string, and skips it: encoding/json before Go 1.27 reports it as an invalid use of the option.
func (ctx *RuntimeContext) stringOptionUnquoted(cursor, depth int64, typ reflect.Type) (int64, error) {
	end, err := skipValue(ctx.Buf, skipWhiteSpace(ctx.Buf, cursor), depth)
	if err != nil {
		return 0, err
	}
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{
			plain: fmt.Errorf("json: invalid use of ,string struct tag, trying to unmarshal unquoted value into %v", typ),
		}
	}
	return end, nil
}

// stringOptionError records or returns the error of the string between start and end of a field of the string
// option, whose bytes are not a value of typ, as encoding/json before Go 1.27 tells it from the first byte of the
// value and the kind of typ: a number out of the range of typ is a type error, a value which starts as no value
// of its kind stops the decoding, and any other one is an invalid use of the option.
func (ctx *RuntimeContext) stringOptionError(start, end int64, value []byte, typ reflect.Type) (int64, error) {
	invalidUse := func() error {
		return fmt.Errorf("json: invalid use of ,string struct tag, trying to unmarshal %q into %v", value, typ)
	}
	save := func(err error) (int64, error) {
		if ctx.typeError == nil {
			ctx.typeError = &pendingTypeError{plain: err}
		}
		return end, nil
	}
	if len(value) == 0 {
		return save(invalidUse())
	}
	elem := typ
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	invalidUse = func() error {
		return fmt.Errorf("json: invalid use of ,string struct tag, trying to unmarshal %q into %v", value, elem)
	}
	switch c := value[0]; {
	case c == 'n', c == 't', c == 'f':
		return save(invalidUse())
	case c == '"':
		if elem == jsonNumberType {
			// a JSON string, whose bytes must be a number
			if s, ok := unquoteBytes(value); ok && !isValidNumber(s) {
				return 0, fmt.Errorf("json: invalid number literal, trying to unmarshal %q into Number", value)
			}
		}
		if elem.Kind() == reflect.String {
			return 0, invalidUse()
		}
		if ctx.typeError == nil {
			ctx.typeError = &pendingTypeError{typ: elem, start: start, end: end, value: "string", kind: stringValue, literal: true}
		}
		return end, nil
	case c != '-' && c-'0' > 9:
		return 0, invalidUse()
	}
	switch elem.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if ctx.typeError == nil {
			ctx.typeError = &pendingTypeError{
				typ: elem, start: start, end: end, value: "number " + string(value), kind: numberValue, literal: true,
			}
		}
		return end, nil
	}
	return 0, invalidUse()
}

// annotateMethodError sets the field of a type error, or the offset of a syntax error, which an unmarshal method
// of the value at cursor returned, as encoding/json before Go 1.27 does.
func annotateMethodError(cursor int64, err error, structName, fieldName string) {
	switch e := err.(type) {
	case *errors.UnmarshalTypeError:
		e.Struct = structName
		e.Field = fieldName
	case *json.UnmarshalTypeError:
		// the type error of encoding/json, which it sets as its own
		e.Struct = structName
		e.Field = fieldName
	case *errors.SyntaxError:
		e.Offset = cursor
	}
}

// methodError returns err, which an unmarshal method of the value between cursor and end returned: encoding/json
// before Go 1.27 stops the decoding with it, with the field of a type error and the offset of a syntax error.
func (ctx *RuntimeContext) methodError(cursor, _ int64, err error, structName, fieldName string) (int64, error) {
	annotateMethodError(cursor, err, structName, fieldName)
	return 0, err
}

// timeKindTypeErrors is whether a value of a time.Time which is not a string or null is a type error: encoding/json
// before Go 1.27 gives it to the UnmarshalJSON of time.Time, whose error stops the decoding.
const timeKindTypeErrors = false

// stringOptionNumber reports whether the bytes of the string of a json.Number of the string option are stored as
// they are: encoding/json before Go 1.27 stores any bytes which start as a number does, without the grammar of the
// numbers, and decodes any other ones as a JSON value ( see stringOptionNumberDecoded ).
func stringOptionNumber(value []byte) bool {
	return len(value) != 0 && (value[0] == '-' || value[0]-'0' <= 9)
}

// stringOptionNumberDecoded is whether the bytes of the string of a json.Number of the string option, when
// stringOptionNumber doesn't store them, are decoded as a JSON value, a string of a number or null, as encoding/json
// before Go 1.27 decodes them.
const stringOptionNumberDecoded = true

// stringOptionUnquotedAllocates is whether a nil pointer of a field of the string option is set to a zero value
// when the value of the field is not in a string: encoding/json before Go 1.27 leaves it.
const stringOptionUnquotedAllocates = false

// StreamOffsetBase returns the position which the offsets of the type errors of a value of a stream are relative to,
// of the end of the previous value and of the start of the value: encoding/json before Go 1.27 reports them from
// the end of the previous value.
func StreamOffsetBase(prevEnd, _ int64) int64 {
	return prevEnd
}

// stringOptionAllocates reports whether a nil pointer of a field of the string option is set to a zero value when
// the value in the string of the field is not of its type: encoding/json before Go 1.27 leaves it for an empty
// string.
func stringOptionAllocates(value []byte) bool {
	return len(value) > 0
}

// textUnmarshalerKindError records the type error of the value at cursor, which is not a string, of a type whose
// pointer type typ implements encoding.TextUnmarshaler, and skips it: encoding/json before Go 1.27 reports the
// pointer type.
func (ctx *RuntimeContext) textUnmarshalerKindError(cursor, depth int64, typ reflect.Type) (int64, error) {
	return ctx.skipTypeError(cursor, depth, typ)
}

// ifaceTextUnmarshalerKindError records the type error of the value at cursor, which is not a string, of an
// interface value of ifaceType which holds a pointer of typ, which implements encoding.TextUnmarshaler, and skips
// it: encoding/json before Go 1.27 reports the interface type.
func (ctx *RuntimeContext) ifaceTextUnmarshalerKindError(cursor, depth int64, ifaceType, _ reflect.Type) (int64, error) {
	return ctx.skipTypeError(cursor, depth, ifaceType)
}

// keptInterfaceTypeError records the type error of the value at cursor, which is not null, decoded into the
// value at p of the interface type typ, which has methods and holds no pointer, and skips it: encoding/json before
// Go 1.27 keeps the interface value, and reports the error as the other type errors.
func (ctx *RuntimeContext) keptInterfaceTypeError(cursor, depth int64, typ reflect.Type, _ unsafe.Pointer) (int64, error) {
	return ctx.skipTypeError(cursor, depth, typ)
}

// textUnmarshalerNullSetsZero reports whether null sets a value of the kind, whose pointer type implements
// encoding.TextUnmarshaler, to its zero value: encoding/json before Go 1.27 does it for a slice, a map, a pointer
// and an interface value, as it does for any value of these kinds.
func textUnmarshalerNullSetsZero(kind reflect.Kind) bool {
	switch kind {
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
		return true
	}
	return false
}

// unsettableFieldError records err, the error of a field of the struct typ which can't be set, as an embedded
// pointer to an unexported struct, for the value at cursor, and skips the value: encoding/json before Go 1.27
// returns the error as it is.
func (ctx *RuntimeContext) unsettableFieldError(cursor, depth int64, _ reflect.Type, err error) (int64, error) {
	buf := ctx.Buf
	end, skipErr := skipValue(buf, skipWhiteSpace(buf, cursor), depth)
	if skipErr != nil {
		return 0, skipErr
	}
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{plain: err}
	}
	return end, nil
}

// storesFloatsOutOfRange reports whether a number out of the range of a float is stored as ±Inf with its type
// error: encoding/json before Go 1.27 leaves the value as it is.
const storesFloatsOutOfRange = false

// floatRangeErrorOfInterface records the type error of the number between start and end, which is out of the
// range of a float64, decoded into the interface{} at p, of value f: encoding/json before Go 1.27 leaves the
// value as it is, and reports the error after the byte which follows the number.
func (ctx *RuntimeContext) floatRangeErrorOfInterface(start, end int64, _ float64, _ unsafe.Pointer) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{
			typ: float64Type, start: start, end: end + 1, value: "number " + string(ctx.Buf[start:end]),
			kind: numberValue, literal: true,
		}
	}
}

// stringOptionNumberStart reports whether c starts a number of a string of the string option which encoding/json
// before Go 1.27 reads: a minus sign or a digit. It reads the numbers of the keys of maps whatever they start with.
func stringOptionNumberStart(c byte) bool {
	return c == '-' || c-'0' <= 9
}

// mapKeySupported reports whether encoding/json before Go 1.27 decodes the keys of a map of keyType, which dec
// decodes: strings, integers and the types which implement encoding.TextUnmarshaler.
func mapKeySupported(keyType reflect.Type, dec Decoder) bool {
	if _, ok := dec.(*unmarshalTextDecoder); ok {
		return true
	}
	switch keyType.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

// unsupportedMapKeys records the type error of the object at cursor of a map whose keys are not decoded ( see
// mapKeySupported ), and skips it: encoding/json before Go 1.27 reports the map, which is left.
func (ctx *RuntimeContext) unsupportedMapKeys(d *mapDecoder, cursor, depth int64, _ unsafe.Pointer) (int64, error) {
	return ctx.skipTypeError(cursor, depth, d.mapType)
}
