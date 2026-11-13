package decoder

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/options"
	"github.com/goccy/go-json/internal/runtime"
	"github.com/goccy/go-json/internal/textcoder"
)

// The functions of unmarshaling of WithUnmarshalers of encoding/json/v2: a value of a type which a function takes is
// decoded by the function, before its method or its kind. The decoders of a type depend on the functions, so they
// are compiled for the functions and cached with them, apart from the tables shared by every goroutine.

// UnmarshalFunc is a function of unmarshaling, for the values of the types castable to Type ( see
// runtime.CastableTo ).
type UnmarshalFunc struct {
	Type reflect.Type
	// Decode decodes the value at cursor into the value at p, of the type from, and returns the cursor after it, or
	// ErrUseDefault if the function declined the value, which a function of UnmarshalFromFunc may.
	Decode func(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer, from reflect.Type) (int64, error)
	// FromDecoder is set for a function of UnmarshalFromFunc, which reads a jsontext.Decoder: a stream gives it a
	// value which it hasn't read whole ( see V2Call.Attach ), and any other function the value read whole.
	FromDecoder bool
}

// ErrUseDefault is the error of a method or a function of unmarshaling which declined a value without reading it:
// the next function, or the next method or the kind of the type, decodes it.
var ErrUseDefault = errors.New("json: the value is decoded by the next representation")

// UnmarshalFuncs are the functions of unmarshaling of a call, in the order they are tried, with the decoders
// compiled for them.
type UnmarshalFuncs struct {
	Funcs    []UnmarshalFunc
	decoders sync.Map // map[flagsKey]Decoder by the pointer type and the options which change the decoders
	castable sync.Map // map[reflect.Type][]*UnmarshalFunc
}

// decoderOf returns the decoder of the pointer type typ for the functions, compiling it if the type is new.
func (f *UnmarshalFuncs) decoderOf(typ unsafe.Pointer, flags V2Flags) (Decoder, error) {
	return decoderIn(&f.decoders, typ, flags, f)
}

// funcsOf returns the functions which take a value of the type, in their order.
func (f *UnmarshalFuncs) funcsOf(typ reflect.Type) []*UnmarshalFunc {
	if f == nil {
		return nil
	}
	if v, ok := f.castable.Load(typ); ok {
		return v.([]*UnmarshalFunc)
	}
	var funcs []*UnmarshalFunc
	for i := range f.Funcs {
		if runtime.CastableTo(typ, f.Funcs[i].Type) {
			funcs = append(funcs, &f.Funcs[i])
		}
	}
	v, _ := f.castable.LoadOrStore(typ, funcs)
	return v.([]*UnmarshalFunc)
}

// V2Call is the state of a call of the v2 json package which its decoders read, which a context keeps from a call to
// the next once a call made it ( see RuntimeContext.V2Call ).
type V2Call struct {
	// Funcs are the functions of unmarshaling of the call, which the decoders are compiled for ( see
	// DecoderOfV2 ), or nil.
	Funcs *UnmarshalFuncs
	// Config are the options of the call, which a method or a function of unmarshaling which reads a
	// jsontext.Decoder is given.
	Config *options.Config
	// Options are the options of a call of Unmarshal or of UnmarshalRead which has options, which Config points to:
	// the context, which a pool keeps, holds them for its calls.
	Options options.Config
	// From is the state of the v2 json package for the methods and the functions which read a jsontext.Decoder,
	// which the decoder doesn't know.
	From unsafe.Pointer
	// Stream is the stream which the call decodes, or nil for an input of its own, with the state of its decoding
	// element by element ( see Stream.DecodeV2 ): Path is the place of the value which is decoded, a level for
	// each object and array which it is in, the top level first ( see textcoder.Level ), and Value the value of
	// the innermost level which a decoder of v2 decodes.
	Stream *Stream
	Path   []textcoder.Level
	Value  StreamValue
	// spareName is the array of a name of the path which no level has, which the next name is copied into ( see
	// Stream.setName ).
	spareName []byte
	// pathNames are the places in the buffer of the stream of the last names of the levels of Path which the levels
	// have not copied yet, one for each level ( see Stream.setNameAt ): copied before the buffer drops them or the
	// path is read ( see Stream.copyNames ).
	pathNames []namePlace
	// namesFrom is the first level of Path which may have a name by its place: the levels before it have none, so
	// that copyNames looks at the levels whose names were set since it copied them, not at the whole path.
	namesFrom int
	// skip is what skipLongV2 follows of the levels of the value of the stream which it skips.
	skip skipPath
	// Place is where the error which the decoding of the stream returned is found again ( see StreamPlace ).
	Place StreamPlace
	// names are the names of no field, and the other names which are kept, of the objects of the call which are
	// being decoded ( see nameSet ), which their arrays are used again for.
	names callNames
	// skipNames are the sets of the names of the objects which skipFastV2 skips, by their levels in the value it
	// skips, which it opens skipLevels of at most ( see skipMaxLevel ).
	skipNames [skipLevels]nameSet
	// skippers are the scanners of jsontext which read the values that the call skips ( see textcoder.TakeSkipper ),
	// by the options of the names of an object twice and of invalid UTF-8 which leave them something to read ( see
	// skipperOf ), taken by the first value and put back when the context is released.
	skippers [3]unsafe.Pointer
	// Lent is the call of UnmarshalDecode which decodes the value of the call in the buffer of its decoder, without
	// the stream, which it sets up for a method or a function which reads a jsontext.Decoder ( see V2Hooks.Lend ),
	// or nil.
	Lent unsafe.Pointer
}

// StreamValue is the value of a stream which a decoder of v2 decodes, in the innermost level of the path, for a
// method or a function which reads a jsontext.Decoder, which is attached at its place: From and Start are the
// offsets in the buffer of the end of the token before the value and of the value. ReadsOn is set for a value which
// the buffer doesn't hold whole, which the decoder reads on from the reader of the stream ( see Stream.ReadOn ).
type StreamValue struct {
	From, Start int64
	ReadsOn     bool
}

// StreamPlace is the part of the buffer of a stream where an error of its decoding of v2 is found again: from
// From, the end of the token before the member, the element or the value which failed, in the innermost level of
// the path, up to End ( see V2Call.Path ).
type StreamPlace struct {
	From, End int64
	Set       bool
}

// CurrentV2Call returns the state of the call of the v2 json package which decodes by ctx, which the call made by
// V2Call, so it is never nil.
func (ctx *RuntimeContext) CurrentV2Call() *V2Call { return ctx.v2 }

// V2Call returns the state of the call of the v2 json package which decodes by ctx, which it makes the first time.
func (ctx *RuntimeContext) V2Call() *V2Call {
	if ctx.v2 == nil {
		ctx.v2 = new(V2Call)
	}
	return ctx.v2
}

// v2Funcs returns the functions of unmarshaling of the call which decodes by ctx, or nil.
func (ctx *RuntimeContext) v2Funcs() *UnmarshalFuncs {
	if v2 := ctx.v2; v2 != nil {
		return v2.Funcs
	}
	return nil
}

// v2FuncDecoder is the decoder of a value of a type which the functions of unmarshaling take, which decode it in
// their order: a function which declines it leaves it to the next one, and the last one to next, the decoder of
// the type without them.
type v2FuncDecoder struct {
	typ   reflect.Type
	funcs []*UnmarshalFunc
	next  Decoder
}

func (d *v2FuncDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	copyStreamNames(ctx)
	for _, fn := range d.funcs {
		next, err := fn.Decode(ctx, cursor, depth, p, d.typ)
		if err != ErrUseDefault {
			return next, err
		}
	}
	return d.next.Decode(ctx, cursor, depth, p)
}

func (d *v2FuncDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}
