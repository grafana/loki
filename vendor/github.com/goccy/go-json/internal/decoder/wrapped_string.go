package decoder

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"unsafe"
)

var (
	// errStringOptionRest tells that the value of a string of the string option is followed by more bytes.
	errStringOptionRest = errors.New("json: value followed by more bytes in a string of the string option")
	// errMapKeyType is returned by the decoder of a key of a map which is not of the type of the keys, whose type
	// error is recorded: the map decodes the value of the entry, and drops the entry, as encoding/json does.
	errMapKeyType = errors.New("json: map key of another type")
)

type wrappedStringDecoder struct {
	typ           reflect.Type
	dec           Decoder
	stringDecoder *stringDecoder
	structName    string
	fieldName     string
	isPtrType     bool
	// isMapKey is set for the decoder of the keys of a map, whose errors are the ones of the keys.
	isMapKey bool
	// numberKind is the kind of a number which the string has, read by strconv as encoding/json reads it ( see
	// decodeNumber ), or reflect.Invalid for a value of any other type.
	numberKind reflect.Kind
	// numberType is the type of the number, which typ is or points to.
	numberType reflect.Type
	// isJSONNumber is set for a json.Number, or a pointer to one, whose bytes are checked as encoding/json of the Go
	// version checks them ( see stringOptionNumber ).
	isJSONNumber bool
}

func newWrappedStringDecoder(typ reflect.Type, dec Decoder, structName, fieldName string) *wrappedStringDecoder {
	d := &wrappedStringDecoder{
		typ:           typ,
		dec:           dec,
		stringDecoder: newStringDecoder(structName, fieldName),
		structName:    structName,
		fieldName:     fieldName,
		isPtrType:     typ.Kind() == reflect.Ptr,
	}
	numberType := typ
	if numberType.Kind() == reflect.Pointer {
		numberType = numberType.Elem()
	}
	d.isJSONNumber = numberType == jsonNumberType
	switch numberType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if numberType.Implements(unmarshalJSONType) || reflect.PointerTo(numberType).Implements(unmarshalJSONType) ||
			reflect.PointerTo(numberType).Implements(unmarshalJSONContextType) ||
			reflect.PointerTo(numberType).Implements(unmarshalTextType) {
			// a number decoded by its own unmarshaler
			break
		}
		d.numberKind, d.numberType = numberType.Kind(), numberType
	}
	return d
}

func (d *wrappedStringDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	if c := buf[cursor]; c != 'n' && isOtherValue(c, stringValue) {
		// a value which is not in a string
		if d.isPtrType && stringOptionUnquotedAllocates {
			d.allocate(p)
		}
		return ctx.stringOptionUnquoted(cursor, depth, d.typ)
	}
	bytes, c, err := d.stringDecoder.decodeByte(buf, cursor)
	if err != nil {
		return 0, err
	}
	if bytes == nil {
		if d.isPtrType {
			*(*unsafe.Pointer)(p) = nil
		}
		return c, nil
	}
	if d.isJSONNumber && !d.isMapKey {
		if stringOptionNumber(bytes) {
			*(*json.Number)(d.target(p)) = json.Number(ctx.makeString(bytes))
			return c, nil
		}
		if !stringOptionNumberDecoded {
			return d.stringError(ctx, start, c, bytes, p)
		}
	}
	if d.numberKind != reflect.Invalid && len(bytes) > 0 {
		if d.decodeNumber(bytes, p) {
			return c, nil
		}
		return d.stringError(ctx, start, c, bytes, p)
	}
	if len(bytes) != 0 && (isWhiteSpace[bytes[0]] || isWhiteSpace[bytes[len(bytes)-1]]) {
		// encoding/json reads the bytes of the string as a value with nothing around it
		return d.stringError(ctx, start, c, bytes, p)
	}
	// The value is decoded from a copy of its bytes, which nothing else uses: its strings may refer to it. A type
	// error of it is the one of the string, which is made after it ( see stringOptionError ).
	saved := ctx.typeError
	ctx.typeError = nil
	oldBuf, oldOrigin := ctx.Buf, ctx.origin
	ctx.Buf, ctx.origin = NewInput(bytes), nil
	next, err := d.dec.Decode(ctx, 0, depth, p)
	if err == nil && skipWhiteSpace(ctx.Buf, next) != int64(len(bytes)) {
		// the value is followed by more bytes in the string
		err = errStringOptionRest
	}
	ctx.Buf, ctx.origin = oldBuf, oldOrigin
	failed := err != nil || ctx.typeError != nil
	ctx.typeError = saved
	if failed || len(bytes) == 0 {
		return d.stringError(ctx, start, c, bytes, p)
	}
	return c, nil
}

// stringError records or returns the error of the string between start and end, whose bytes are not a value of
// the type.
func (d *wrappedStringDecoder) stringError(ctx *RuntimeContext, start, end int64, bytes []byte, p unsafe.Pointer) (int64, error) {
	if d.isMapKey {
		ctx.keyTypeError(start, end, bytes, d.elemType())
		return end, errMapKeyType
	}
	if d.isPtrType && stringOptionAllocates(bytes) {
		d.allocate(p)
	}
	return ctx.stringOptionError(start, end, bytes, d.typ)
}

// decodeNumber reads the number which the bytes of the string are, as encoding/json reads the number of a string of
// the string option or of a key of a map, by strconv: a number which it fails, or which is out of the range of the
// type, is not stored. It reports whether the number is stored.
func (d *wrappedStringDecoder) decodeNumber(bytes []byte, p unsafe.Pointer) bool {
	if !d.isMapKey && !stringOptionNumberStart(bytes[0]) {
		return false
	}
	s := *(*string)(unsafe.Pointer(&bytes))
	bits := int(d.numberType.Size() * 8)
	switch d.numberKind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return false
		}
		storeInt(d.target(p), bits, uint64(n))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(s, 10, bits)
		if err != nil {
			return false
		}
		storeInt(d.target(p), bits, n)
	default:
		n, err := strconv.ParseFloat(s, bits)
		if err != nil {
			return false
		}
		if bits == 32 {
			*(*float32)(d.target(p)) = float32(n)
		} else {
			*(*float64)(d.target(p)) = n
		}
	}
	return true
}

// storeInt stores the lowest bits of n, an integer of the size of bits, at p.
func storeInt(p unsafe.Pointer, bits int, n uint64) {
	switch bits {
	case 8:
		*(*uint8)(p) = uint8(n)
	case 16:
		*(*uint16)(p) = uint16(n)
	case 32:
		*(*uint32)(p) = uint32(n)
	default:
		*(*uint64)(p) = n
	}
}

// target returns where the number is stored: at p, or where the pointer at p points to, which is allocated if it
// is nil.
func (d *wrappedStringDecoder) target(p unsafe.Pointer) unsafe.Pointer {
	if !d.isPtrType {
		return p
	}
	d.allocate(p)
	return *(*unsafe.Pointer)(p)
}

// elemType returns the type of the value, which the pointers of the type point to.
func (d *wrappedStringDecoder) elemType() reflect.Type {
	typ := d.typ
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// allocate sets the pointer at p, if it is nil, to a zero value, as encoding/json does for a value it fails to
// decode.
func (d *wrappedStringDecoder) allocate(p unsafe.Pointer) {
	if *(*unsafe.Pointer)(p) == nil {
		*(*unsafe.Pointer)(p) = newValue(d.typ.Elem())
	}
}

func (d *wrappedStringDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: wrapped string decoder does not support decode path")
}
