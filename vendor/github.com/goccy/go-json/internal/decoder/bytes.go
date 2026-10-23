package decoder

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type bytesDecoder struct {
	typ           reflect.Type
	sliceDecoder  Decoder
	stringDecoder *stringDecoder
	structName    string
	fieldName     string
	// sliceType is the type of the slice, which the type errors report.
	sliceType reflect.Type
}

func byteUnmarshalerSliceDecoder(typ reflect.Type, structName string, fieldName string) Decoder {
	var unmarshalDecoder Decoder
	switch {
	case reflect.PointerTo(typ).Implements(unmarshalJSONType):
		unmarshalDecoder = newUnmarshalJSONDecoder(reflect.PointerTo(typ), structName, fieldName)
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		unmarshalDecoder = newUnmarshalTextDecoder(reflect.PointerTo(typ), structName, fieldName)
	default:
		unmarshalDecoder, _ = compileUint8(typ, structName, fieldName)
	}
	return newSliceDecoder(unmarshalDecoder, typ, 1, structName, fieldName)
}

func newBytesDecoder(typ reflect.Type, structName string, fieldName string) *bytesDecoder {
	return &bytesDecoder{
		typ:           typ,
		sliceType:     reflect.SliceOf(typ),
		sliceDecoder:  byteUnmarshalerSliceDecoder(typ, structName, fieldName),
		stringDecoder: newStringDecoder(structName, fieldName),
		structName:    structName,
		fieldName:     fieldName,
	}
}

func (d *bytesDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	cursor = skipWhiteSpace(ctx.Buf, cursor)
	start := cursor
	bytes, c, err := d.decodeBinary(ctx, cursor, depth, p)
	if err != nil {
		if c := ctx.Buf[cursor]; c != '[' && isOtherValue(c, stringValue) {
			return ctx.skipTypeError(cursor, depth, d.sliceType)
		}
		return 0, err
	}
	if bytes == nil {
		return c, nil
	}
	cursor = c
	decodedLen := base64.StdEncoding.DecodedLen(len(bytes))
	b := make([]byte, decodedLen)
	n, err := base64.StdEncoding.Decode(b, bytes)
	if err != nil {
		ctx.base64Error(start, cursor, d.sliceType, err)
		return cursor, nil
	}
	*(*[]byte)(p) = b[:n]
	return cursor, nil
}

func (d *bytesDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: []byte decoder does not support decode path")
}

func (d *bytesDecoder) decodeBinary(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) ([]byte, int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == '[' {
		if d.sliceDecoder == nil {
			return nil, 0, &errors.UnmarshalTypeError{
				Type:   d.typ,
				Offset: cursor,
			}
		}
		c, err := d.sliceDecoder.Decode(ctx, cursor, depth, p)
		if err != nil {
			return nil, 0, err
		}
		return nil, c, nil
	}
	return d.stringDecoder.decodeByte(buf, cursor)
}
