package decoder

import (
	"reflect"
	"strconv"
)

// The type errors are the ones of encoding/json: a value which is not of the type of its destination is skipped,
// the decoding goes on, and the first such error is returned at the end, unless the input has a syntax error,
// which is returned instead. The error is recorded where it happens by its position only; the path to its value,
// which encoding/json reports as the Struct and the Field of the error, is computed at the end by a walk of the
// input from the root ( see typeErrorPath ), so that the decoding of a value which has no error keeps no path.

// float64Type is the type which the type error of a number out of the range of a float64 decoded into an
// interface{} reports.
var float64Type = reflect.TypeOf(float64(0))

// pendingTypeError is the first type error of a decoding, as it is recorded.
type pendingTypeError struct {
	typ reflect.Type
	// start and end are the positions of the value in the buffer.
	start, end int64
	// value is the description of the value which the error reports, as "string" or "number 1.5".
	value string
	// kind is the kind of the value.
	kind jsonKind
	// atKey is set for the error of the key of a map entry, whose value starts at start too.
	atKey bool
	// literal is set for an error which reports the literal of the value in its value, as "number 1.5".
	literal bool
	// plain is an error which is not a type error, which encoding/json returns as it is: an invalid use of the string
	// option before Go 1.27, or the error of an unmarshal method by Go 1.27.
	plain error
	// err is the cause of the error, which encoding/json of Go 1.27 reports.
	err error
	// noContext is set for an error which is reported with no path and no offset, as encoding/json of Go 1.27
	// reports the ones of a json.Number.
	noContext bool
	// atStart is set for an error whose offset is the start of the value, as encoding/json of Go 1.27 reports the
	// ones of an interface value which is kept.
	atStart bool
}

// jsonKind is the kind of a JSON value, which a decoder decodes and a type error reports.
type jsonKind uint8

const (
	noValue jsonKind = iota
	stringValue
	numberValue
	boolValue
	nullValue
	arrayValue
	objectValue
)

// kindOf returns the kind of the value which starts with c, or noValue if c starts no value.
func kindOf(c byte) jsonKind {
	switch c {
	case '"':
		return stringValue
	case 't', 'f':
		return boolValue
	case 'n':
		return nullValue
	case '[':
		return arrayValue
	case '{':
		return objectValue
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return numberValue
	}
	return noValue
}

// String returns the description of the kind which the type errors report.
func (k jsonKind) String() string {
	switch k {
	case stringValue:
		return "string"
	case boolValue:
		return "bool"
	case nullValue:
		return "null"
	case arrayValue:
		return "array"
	case objectValue:
		return "object"
	case numberValue:
		return "number"
	}
	return ""
}

// isOtherValue reports whether c starts a JSON value of another kind than kind, which is the kind of the values
// of a decoder: such a value is of another type, and is a type error, as a byte which starts no value is a
// syntax error.
func isOtherValue(c byte, kind jsonKind) bool {
	k := kindOf(c)
	return k != noValue && k != kind
}

// skipTypeError records the type error of the value at cursor, which is not of typ, if it is the first one, and
// returns the position after the value, which is skipped. A syntax error of the value is returned instead.
func (ctx *RuntimeContext) skipTypeError(cursor, depth int64, typ reflect.Type) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	end, err := skipValue(buf, cursor, depth)
	if err != nil {
		return 0, err
	}
	if ctx.typeError == nil {
		kind := kindOf(buf[cursor])
		ctx.typeError = &pendingTypeError{typ: typ, start: cursor, end: end, value: kind.String(), kind: kind}
	}
	return end, nil
}

// setEmbedded sets the names of the embedded fields which the field is promoted through.
func (d *structDecoder) setEmbedded(field *structFieldSet, names []string) {
	if d.embedded == nil {
		d.embedded = map[*structFieldSet][]string{}
	}
	d.embedded[field] = names
}

// numberTypeError records the type error of the number between start and end, which is not of typ because it
// is out of its range or not an integer: the error reports the literal of the number. It is not inlined, so that
// the decoders of the numbers keep the size they had.
//
//go:noinline
func (ctx *RuntimeContext) numberTypeError(start, end int64, typ reflect.Type) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{
			typ: typ, start: start, end: end, value: "number " + string(ctx.Buf[start:end]), kind: numberValue,
			literal: true,
		}
	}
}

// keyTypeError records the type error of the key of a map entry between start and end, the quotes of which are
// included, which is not a number of typ: the error reports the key.
func (ctx *RuntimeContext) keyTypeError(start, end int64, key []byte, typ reflect.Type) {
	if ctx.typeError == nil {
		ctx.typeError = &pendingTypeError{
			typ: typ, start: start, end: end, value: "number " + string(key), kind: stringValue, atKey: true,
			literal: true,
		}
	}
}

// HasTypeError reports whether the decoding has a type error, which TypeError returns: the type of the value
// is made for it only.
func (ctx *RuntimeContext) HasTypeError() bool {
	return ctx.typeError != nil
}

// DiscardTypeError drops the type error of a decoding which failed with another error.
func (ctx *RuntimeContext) DiscardTypeError() {
	ctx.typeError = nil
}

// TypeError returns the first type error of the decoding of the value which dec decoded from base into a value
// of typ, as encoding/json of the Go version reports it, or nil if the decoding had none. Its offset is relative
// to offsetBase.
func (ctx *RuntimeContext) TypeError(dec Decoder, typ reflect.Type, base, offsetBase int64) error {
	p := ctx.typeError
	if p == nil {
		return nil
	}
	ctx.typeError = nil
	if p.plain != nil {
		return p.plain
	}
	e := newTypeError(p, typeErrorPath(dec, ctx.Buf, base, p), typ)
	if p.atStart {
		e.Offset = p.start
	}
	if e.Offset != 0 {
		e.Offset -= offsetBase
	}
	return e
}

// typeErrorStep is a step of the path from the root to the value of a type error: a field of a struct, an
// element of an array or a slice, or an entry of a map.
type typeErrorStep struct {
	// structName is the name of the struct type of a field.
	structName string
	// embedded are the names of the embedded fields which a field is promoted through.
	embedded []string
	// name is the name of a field, which is its key, the key of a map entry as it is decoded, or the index of an
	// element.
	name string
	// inputName is the key of a field as the input has it, decoded, which encoding/json of Go 1.27 reports.
	inputName string
	// isField is set for a field of a struct.
	isField bool
}

// typeErrorPath returns the path from the value which dec decodes at cursor to the value of the error, by the
// decoders of the values it goes through.
func typeErrorPath(dec Decoder, buf []byte, cursor int64, p *pendingTypeError) []typeErrorStep {
	var path []typeErrorStep
	for {
		cursor = skipWhiteSpace(buf, cursor)
		if cursor >= p.start {
			return path
		}
		switch d := dec.(type) {
		case *ptrDecoder:
			dec = d.dec
			continue
		case *basicPtrDecoder:
			dec = d.dec
			continue
		case *anonymousFieldDecoder:
			dec = d.dec
			continue
		case *structDecoder:
			step, child, next, ok := d.typeErrorChild(buf, cursor, p)
			if !ok {
				return path
			}
			path = append(path, step)
			if next < 0 {
				return path
			}
			dec, cursor = child, next
		case *sliceDecoder:
			i, next, ok := typeErrorElement(buf, cursor, p)
			if !ok {
				return path
			}
			path = append(path, typeErrorStep{name: strconv.Itoa(i)})
			dec, cursor = d.valueDecoder, next
		case *arrayDecoder:
			i, next, ok := typeErrorElement(buf, cursor, p)
			if !ok {
				return path
			}
			path = append(path, typeErrorStep{name: strconv.Itoa(i)})
			dec, cursor = d.valueDecoder, next
		case *interfaceDecoder:
			// the objects and the arrays of an interface{}, whose values are decoded by the same decoder
			var next int64
			var ok bool
			switch buf[cursor] {
			case '{':
				var key string
				if key, next, ok = typeErrorEntry(buf, cursor, p); ok {
					path = append(path, typeErrorStep{name: key})
				}
			case '[':
				var i int
				if i, next, ok = typeErrorElement(buf, cursor, p); ok {
					path = append(path, typeErrorStep{name: strconv.Itoa(i)})
				}
			}
			if !ok || next < 0 || d.hasMethods {
				return path
			}
			cursor = next
		case *mapDecoder:
			key, next, ok := typeErrorEntry(buf, cursor, p)
			if !ok {
				return path
			}
			path = append(path, typeErrorStep{name: key})
			if next < 0 {
				return path
			}
			dec, cursor = d.valueDecoder, next
		default:
			return path
		}
	}
}

// typeErrorChild returns the step to the field of the object at cursor whose value has the error, the decoder of
// the field and the position of its value, which is -1 if the error is of the key. ok is false if the error is in
// no field of the object.
func (d *structDecoder) typeErrorChild(buf []byte, cursor int64, p *pendingTypeError) (typeErrorStep, Decoder, int64, bool) {
	if buf[cursor] != '{' {
		return typeErrorStep{}, nil, 0, false
	}
	cursor++
	for {
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] != '"' {
			return typeErrorStep{}, nil, 0, false
		}
		key, info, next, ok := typeErrorKey(buf, cursor)
		if !ok {
			return typeErrorStep{}, nil, 0, false
		}
		field, _ := d.keys.lookup(key, info)
		cursor = skipWhiteSpace(buf, next)
		if buf[cursor] != ':' {
			return typeErrorStep{}, nil, 0, false
		}
		cursor = skipWhiteSpace(buf, cursor+1)
		end, err := skipValue(buf, cursor, 0)
		if err != nil {
			return typeErrorStep{}, nil, 0, false
		}
		if p.start >= cursor && p.start < end {
			if field == nil {
				return typeErrorStep{}, nil, 0, false
			}
			step := typeErrorStep{
				structName: d.typeName, name: field.key, inputName: string(decodeLiteral(key, info)), isField: true,
			}
			if embeddedFieldNames {
				step.embedded = d.embedded[field]
			}
			return step, field.dec, cursor, true
		}
		cursor = skipWhiteSpace(buf, end)
		if buf[cursor] != ',' {
			return typeErrorStep{}, nil, 0, false
		}
		cursor++
	}
}

// typeErrorElement returns the index of the element of the array at cursor which has the error, and its position.
func typeErrorElement(buf []byte, cursor int64, p *pendingTypeError) (int, int64, bool) {
	if buf[cursor] != '[' {
		return 0, 0, false
	}
	cursor++
	for i := 0; ; i++ {
		cursor = skipWhiteSpace(buf, cursor)
		end, err := skipValue(buf, cursor, 0)
		if err != nil {
			return 0, 0, false
		}
		if p.start >= cursor && p.start < end {
			return i, cursor, true
		}
		cursor = skipWhiteSpace(buf, end)
		if buf[cursor] != ',' {
			return 0, 0, false
		}
		cursor++
	}
}

// typeErrorEntry returns the key of the entry of the object at cursor which has the error, and the position of
// its value, which is -1 if the error is of the key.
func typeErrorEntry(buf []byte, cursor int64, p *pendingTypeError) (string, int64, bool) {
	if buf[cursor] != '{' {
		return "", 0, false
	}
	cursor++
	for {
		cursor = skipWhiteSpace(buf, cursor)
		keyStart := cursor
		key, info, next, ok := typeErrorKey(buf, cursor)
		if !ok {
			return "", 0, false
		}
		key = decodeLiteral(key, info)
		if p.atKey && keyStart == p.start {
			return string(key), -1, true
		}
		cursor = skipWhiteSpace(buf, next)
		if buf[cursor] != ':' {
			return "", 0, false
		}
		cursor = skipWhiteSpace(buf, cursor+1)
		end, err := skipValue(buf, cursor, 0)
		if err != nil {
			return "", 0, false
		}
		if p.start >= cursor && p.start < end {
			return string(key), cursor, true
		}
		cursor = skipWhiteSpace(buf, end)
		if buf[cursor] != ',' {
			return "", 0, false
		}
		cursor++
	}
}

// typeErrorKey returns a copy of the bytes of the key at cursor as they are in buf, what they have, and the position
// after the key: the buffer is not written, as the decoding of a key does to its escapes.
func typeErrorKey(buf []byte, cursor int64) ([]byte, stringInfo, int64, bool) {
	raw, next, info, err := skipStringDecoder.scanString(buf, cursor)
	if next < 0 {
		raw, next, info, err = skipStringDecoder.scanStringRest(buf, raw, -next-1, info)
	}
	if err != nil || raw == nil {
		return nil, stringInfo{}, 0, false
	}
	return append([]byte(nil), raw...), info, next, true
}
