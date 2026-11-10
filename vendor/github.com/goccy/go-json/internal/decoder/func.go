package decoder

import (
	"fmt"
	"reflect"
	"unsafe"
)

type funcDecoder struct {
	typ        reflect.Type
	structName string
	fieldName  string
}

func newFuncDecoder(typ reflect.Type, structName, fieldName string) *funcDecoder {
	fnDecoder := &funcDecoder{typ, structName, fieldName}
	return fnDecoder
}

func (d *funcDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if buf[cursor+1] != 'u' || buf[cursor+2] != 'l' || buf[cursor+3] != 'l' {
			return 0, literalSyntaxError(buf, cursor, "null")
		}
		*(*unsafe.Pointer)(p) = nil
		return cursor + 4, nil
	}
	// a func can't be decoded from any other value: it is a type error, after which the decoding goes on
	return ctx.skipTypeError(cursor, depth, d.typ)
}

func (d *funcDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: func decoder does not support decode path")
}
