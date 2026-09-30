package decoder

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

type unmarshalJSONDecoder struct {
	typ        reflect.Type
	structName string
	fieldName  string
	// retainsNothing is set for a type whose UnmarshalJSON keeps nothing of the bytes it is given, which are then
	// the ones of the buffer. The bytes of any other type are copied: its UnmarshalJSON may keep them, and the
	// buffer is written again by the next call.
	retainsNothing bool
}

// timePtrType is the type of *time.Time, whose UnmarshalJSON parses the bytes it is given and keeps nothing of
// them. The decoder is made for the pointer type, whose method set has UnmarshalJSON.
var timePtrType = reflect.TypeOf(&time.Time{})

// timeType is the type of time.Time.
var timeType = timePtrType.Elem()

func newUnmarshalJSONDecoder(typ reflect.Type, structName, fieldName string) *unmarshalJSONDecoder {
	return &unmarshalJSONDecoder{
		typ:            typ,
		structName:     structName,
		fieldName:      fieldName,
		retainsNothing: typ == timePtrType,
	}
}

func (d *unmarshalJSONDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if timeKindTypeErrors && d.typ == timePtrType {
		if c := buf[cursor]; c != '"' && c != 'n' {
			// a time.Time is decoded from a string only: any other value is a type error
			return ctx.skipTypeError(cursor, depth, timeType)
		}
	}
	start := cursor
	end, err := skipValue(buf, cursor, depth)
	if err != nil {
		return 0, err
	}
	dst := buf[start:end:end]
	if !d.retainsNothing {
		// a copy of the exact length: append would round its capacity up, which costs more
		copied := make([]byte, len(dst))
		copy(copied, dst)
		dst = copied
	}

	v := *(*any)(unsafe.Pointer(&emptyInterface{
		typ: runtime.TypePtr(d.typ),
		ptr: p,
	}))
	// The method is chosen by what the type implements, not by the entry point:
	// json.Unmarshal may meet a type with the context method and UnmarshalContext one without it.
	switch v := v.(type) {
	case unmarshalerContext:
		c := ctx.Option.Context
		if (ctx.Option.Flags&ContextOption) == 0 || c == nil {
			c = context.Background()
		}
		if err := v.UnmarshalJSON(c, dst); err != nil {
			return ctx.methodError(cursor, end, err, d.structName, d.fieldName)
		}
	case json.Unmarshaler:
		if err := v.UnmarshalJSON(dst); err != nil {
			return ctx.methodError(cursor, end, err, d.structName, d.fieldName)
		}
	}
	return end, nil
}

func (d *unmarshalJSONDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: unmarshal json decoder does not support decode path")
}
