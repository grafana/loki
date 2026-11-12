package decoder

import (
	"encoding"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/runtime"
)

type unmarshalTextDecoder struct {
	typ reflect.Type
}

func newUnmarshalTextDecoder(typ reflect.Type) *unmarshalTextDecoder {
	return &unmarshalTextDecoder{typ: typ}
}

var (
	nullbytes = []byte(`null`)
)

func (d *unmarshalTextDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	end, methodErr, err := d.decodeText(ctx, cursor, depth, p)
	if methodErr != nil {
		ctx.methodError(methodErr)
	}
	return end, err
}

// decodeText decodes the value at cursor into p, and returns the position after it, and the error of UnmarshalText
// apart from the one of the decoding.
func (d *unmarshalTextDecoder) decodeText(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	end, err := skipValue(buf, cursor, depth)
	if err != nil {
		return 0, nil, err
	}
	src := buf[start:end]
	switch c := src[0]; {
	case c == 'n':
		// null leaves the value as it is, and is not given to UnmarshalText
		return end, nil, nil
	case c != '"':
		// a value of another kind than a string, which is a type error
		end, err := ctx.textUnmarshalerKindError(start, depth, d.typ)
		return end, nil, err
	}

	// a string, which skipValue found valid, which ends at its length: a method which appends to it doesn't write into
	// the input after it
	src = jsonstring.UnquoteValid(src)
	src = src[:len(src):len(src)]
	v := *(*any)(unsafe.Pointer(&emptyInterface{
		typ: runtime.TypePtr(d.typ),
		ptr: *(*unsafe.Pointer)(unsafe.Pointer(&p)),
	}))
	return end, v.(encoding.TextUnmarshaler).UnmarshalText(src), nil
}

// textKeyDecoder is the decoder of the keys of a map whose type implements encoding.TextUnmarshaler by its pointer
// type: the entry of a key which UnmarshalText fails for is dropped, as the one of a key of another type is ( see
// errMapKeyType ), and the error of the method is recorded as the one of a value.
type textKeyDecoder struct {
	unmarshalTextDecoder
}

func (d *textKeyDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	end, methodErr, err := d.decodeText(ctx, cursor, depth, p)
	if methodErr != nil {
		ctx.methodError(methodErr)
		return end, errMapKeyType
	}
	return end, err
}

func (d *unmarshalTextDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: unmarshal text decoder does not support decode path")
}
