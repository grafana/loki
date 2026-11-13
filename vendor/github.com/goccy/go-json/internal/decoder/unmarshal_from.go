package decoder

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/options"
	"github.com/goccy/go-json/internal/runtime"
	"github.com/goccy/go-json/jsontext"
)

// The UnmarshalJSONFrom methods which the decoder of v1 calls, before UnmarshalJSON: the one which takes the Decoder
// of github.com/goccy/go-json/jsontext, which go-json replaces encoding/json/jsontext by, first, and the one which
// takes the Decoder of encoding/json/jsontext, which encoding/json of Go 1.27 calls ( see stdUnmarshalerFromType ).
// The value is read whole, as for UnmarshalJSON, and the method reads it from a decoder of its own. A method which
// declines, by errors.ErrUnsupported, leaves the value to the decoder after it, as encoding/json/v2 documents it.
// A key of a map is not decoded by the method, as encoding/json doesn't ( see compileMapKey ).

// unmarshalerFromType is the interface of UnmarshalJSONFrom with the Decoder of github.com/goccy/go-json/jsontext.
var unmarshalerFromType = reflect.TypeFor[unmarshalerFrom]()

type unmarshalerFrom interface {
	UnmarshalJSONFrom(*jsontext.Decoder) error
}

// implementsUnmarshalFrom reports whether the decoder of v1 calls UnmarshalJSONFrom of a value of the type: not of a
// json.Number, which has the one of encoding/json/jsontext from Go 1.27, and which the decoder decodes by itself, with
// the options and at the places which the method looks at.
func implementsUnmarshalFrom(typ reflect.Type) bool {
	return typ != reflect.PointerTo(jsonNumberType) &&
		(typ.Implements(unmarshalerFromType) || stdUnmarshalerFromType != nil && typ.Implements(stdUnmarshalerFromType))
}

// fromDecoder is a decoder of jsontext which an UnmarshalJSONFrom method reads from, and the reader of its input.
type fromDecoder struct {
	r   bytes.Reader
	dec *jsontext.Decoder
}

var fromDecoders = sync.Pool{New: func() any {
	d := new(fromDecoder)
	d.dec = jsontext.NewDecoder(&d.r)
	return d
}}

// fromOptions are the options of the decoder which UnmarshalJSONFrom of github.com/goccy/go-json/jsontext reads from,
// made of the options of the decoding: the ones of encoding/json, which DefaultOptionsV1 of the root package sets,
// and RejectUnknownMembers and UnmarshalAnyWithRawNumber for DisallowUnknownFieldsOption and UseNumberOption, which a
// Decoder of encoding/json adds for DisallowUnknownFields and UseNumber, as encoding/json gives its methods, by which
// UnmarshalDecode of v2 within the method decodes as the decoder of v1 does.
type fromOptions OptionFlags

func (o fromOptions) ApplyTo(c *options.Config) {
	f := options.DefaultV1Flags
	if OptionFlags(o)&DisallowUnknownFieldsOption != 0 {
		f |= options.RejectUnknownMembers
	}
	if OptionFlags(o)&UseNumberOption != 0 {
		f |= options.UnmarshalAnyWithRawNumber
	}
	c.Set |= f
	c.Value |= f
}

// unmarshalFrom calls UnmarshalJSONFrom of m with a decoder of the value, by the options of the decoding flags ( see
// fromOptions ), and returns its error, and whether it declined.
func unmarshalFrom(m unmarshalerFrom, value []byte, flags OptionFlags) (bool, error) {
	d := fromDecoders.Get().(*fromDecoder)
	defer fromDecoders.Put(d)
	d.r.Reset(value)
	d.dec.Reset(&d.r, fromOptions(flags))
	err := m.UnmarshalJSONFrom(d.dec)
	_, n := d.dec.StackIndex(0)
	declined, err := errors.MethodCallResult(err, d.dec.StackDepth(), n)
	// the pool keeps no reference to the input, only the copy which the decoder read into its buffer
	d.r.Reset(nil)
	return declined, err
}

// unmarshalFromDecoder decodes a value by UnmarshalJSONFrom of the pointer type typ, and by next where the method
// declines.
type unmarshalFromDecoder struct {
	typ  reflect.Type
	from func(v any, value []byte, flags OptionFlags) (bool, error)
	next Decoder
}

func newUnmarshalFromDecoder(typ reflect.Type, next Decoder) *unmarshalFromDecoder {
	// the method of encoding/json/jsontext, which implementsUnmarshalFrom found, if the type has not the one of
	// github.com/goccy/go-json/jsontext
	from := stdUnmarshalFrom
	if typ.Implements(unmarshalerFromType) {
		from = func(v any, value []byte, flags OptionFlags) (bool, error) {
			return unmarshalFrom(v.(unmarshalerFrom), value, flags)
		}
	}
	return &unmarshalFromDecoder{typ: typ, from: from, next: next}
}

func (d *unmarshalFromDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	start := skipWhiteSpace(buf, cursor)
	end, err := skipValue(buf, start, depth)
	if err != nil {
		return 0, err
	}
	v := *(*any)(unsafe.Pointer(&emptyInterface{
		typ: runtime.TypePtr(d.typ),
		ptr: p,
	}))
	declined, err := d.from(v, buf[start:end], ctx.Option.Flags)
	if declined {
		return d.next.Decode(ctx, start, depth, p)
	}
	if err != nil {
		ctx.methodError(err)
	}
	return end, nil
}

func (d *unmarshalFromDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: unmarshal json from decoder does not support decode path")
}
