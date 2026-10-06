package decoder

import (
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type invalidDecoder struct {
	typ        reflect.Type
	kind       reflect.Kind
	structName string
	fieldName  string
}

func newInvalidDecoder(typ reflect.Type, structName, fieldName string) *invalidDecoder {
	return &invalidDecoder{
		typ:        typ,
		kind:       typ.Kind(),
		structName: structName,
		fieldName:  fieldName,
	}
}

// Decode skips the value, which no value of the type can be decoded from: it is a type error, as encoding/json
// reports it, but null, which is ignored.
func (d *invalidDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		return cursor + 4, nil
	}
	return ctx.skipTypeError(cursor, depth, d.typ)
}

func (d *invalidDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, &errors.UnmarshalTypeError{
		Value:  "object",
		Type:   d.typ,
		Offset: cursor,
		Struct: d.structName,
		Field:  d.fieldName,
	}
}
