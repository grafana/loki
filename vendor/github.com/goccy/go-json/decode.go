package json

import (
	"context"
	"io"
	"unsafe"

	"github.com/goccy/go-json/internal/decoder"
	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type Decoder struct {
	s *decoder.Stream
}

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

func unmarshal(data []byte, v any, optFuncs ...DecodeOptionFunc) error {
	header := (*emptyInterface)(unsafe.Pointer(&v))

	if err := validateType(header.typ, uintptr(header.ptr)); err != nil {
		return err
	}
	ctx := decoder.TakeRuntimeContext()
	dec, err := ctx.DecoderOf(header.typ)
	if err != nil {
		decoder.ReleaseRuntimeContext(ctx)
		return err
	}
	src := ctx.SetInput(data)
	ctx.Option.Flags = 0
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}
	cursor, err := dec.Decode(ctx, 0, 0, header.ptr)
	if err == nil {
		// src is the buffer of the context: it is validated before the context is released
		err = validateEndBuf(src, cursor)
	}
	if err != nil || ctx.HasTypeError() {
		return endWithError(ctx, header.typ, err)
	}
	decoder.ReleaseRuntimeContext(ctx)
	return nil
}

func unmarshalContext(ctx context.Context, data []byte, v any, optFuncs ...DecodeOptionFunc) error {
	header := (*emptyInterface)(unsafe.Pointer(&v))

	if err := validateType(header.typ, uintptr(header.ptr)); err != nil {
		return err
	}
	rctx := decoder.TakeRuntimeContext()
	dec, err := rctx.DecoderOf(header.typ)
	if err != nil {
		decoder.ReleaseRuntimeContext(rctx)
		return err
	}
	src := rctx.SetInput(data)
	rctx.Option.Flags = 0
	rctx.Option.Flags |= decoder.ContextOption
	rctx.Option.Context = ctx
	for _, optFunc := range optFuncs {
		optFunc(rctx.Option)
	}
	cursor, err := dec.Decode(rctx, 0, 0, header.ptr)
	if err == nil {
		// src is the buffer of the context: it is validated before the context is released
		err = validateEndBuf(src, cursor)
	}
	if err != nil || rctx.HasTypeError() {
		return endWithError(rctx, header.typ, err)
	}
	decoder.ReleaseRuntimeContext(rctx)
	return nil
}

var (
	pathDecoder = decoder.NewPathDecoder()
)

func extractFromPath(path *Path, data []byte, optFuncs ...DecodeOptionFunc) ([][]byte, error) {
	if path.path.RootSelectorOnly {
		return [][]byte{data}, nil
	}
	src := decoder.NewInput(data)

	ctx := decoder.TakeRuntimeContext()
	ctx.Buf = src
	ctx.Option.Flags = 0
	ctx.Option.Flags |= decoder.PathOption
	ctx.Option.Path = path.path
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}
	paths, cursor, err := pathDecoder.DecodePath(ctx, 0, 0)
	if err != nil {
		decoder.ReleaseRuntimeContext(ctx)
		return nil, err
	}
	decoder.ReleaseRuntimeContext(ctx)
	if err := validateEndBuf(src, cursor); err != nil {
		return nil, err
	}
	return paths, nil
}

// endWithError releases the context of a decoding into a value of the pointer type typ, which failed with err or
// else has a type error, and returns the error: the type error is returned only when the whole input is valid, as
// encoding/json does. It takes the decoder of typ again, so that the functions which decode keep nothing more
// across their call of it: an error is rare.
//
//go:noinline
func endWithError(ctx *decoder.RuntimeContext, typ unsafe.Pointer, err error) error {
	err = inputSyntaxError(ctx, err)
	if err == nil {
		var dec decoder.Decoder
		if dec, err = ctx.DecoderOf(typ); err == nil {
			err = ctx.TypeError(dec, runtime.TypeOfPtr(typ), 0, 0)
		}
	}
	ctx.DiscardTypeError()
	decoder.ReleaseRuntimeContext(ctx)
	return err
}

// inputSyntaxError returns err, the error of the decoding of the input of ctx, or, for a syntax error, the first
// syntax error of the input as encoding/json reports it: it checks the whole input before it decodes it.
func inputSyntaxError(ctx *decoder.RuntimeContext, err error) error {
	if _, ok := err.(*errors.SyntaxError); ok {
		if serr := ctx.InputSyntaxError(); serr != nil {
			return serr
		}
	}
	return err
}

// validateEndBuf returns the syntax error of what follows the value, which ends at cursor, in src, the input
// followed by the nul byte: only white spaces may follow it. The value is most often at the end of the input, which
// is checked without a call.
func validateEndBuf(src []byte, cursor int64) error {
	if cursor == int64(len(src))-1 {
		return nil
	}
	return decoder.ValidateEnd(src, cursor)
}

// validateType validates that the value is not nil.
// Whether the type is a pointer is validated when its decoder is compiled, once per type.
func validateType(typ unsafe.Pointer, p uintptr) error {
	if typ == nil || p == 0 {
		return &InvalidUnmarshalError{Type: runtime.TypeOfPtr(typ)}
	}
	return nil
}

// NewDecoder returns a new decoder that reads from r.
//
// The decoder introduces its own buffering and may
// read data from r beyond the JSON values requested.
func NewDecoder(r io.Reader) *Decoder {
	s := decoder.NewStream(r)
	return &Decoder{
		s: s,
	}
}

// Buffered returns a reader of the data remaining in the Decoder's
// buffer. The reader is valid until the next call to Decode.
func (d *Decoder) Buffered() io.Reader {
	return d.s.Buffered()
}

// Decode reads the next JSON-encoded value from its
// input and stores it in the value pointed to by v.
//
// See the documentation for Unmarshal for details about
// the conversion of JSON into a Go value.
func (d *Decoder) Decode(v any) error {
	return d.DecodeWithOption(v)
}

// DecodeContext reads the next JSON-encoded value from its
// input and stores it in the value pointed to by v with context.Context.
func (d *Decoder) DecodeContext(ctx context.Context, v any) error {
	d.s.Option.Flags |= decoder.ContextOption
	d.s.Option.Context = ctx
	return d.DecodeWithOption(v)
}

func (d *Decoder) DecodeWithOption(v any, optFuncs ...DecodeOptionFunc) error {
	header := (*emptyInterface)(unsafe.Pointer(&v))
	if err := validateType(header.typ, uintptr(header.ptr)); err != nil {
		return err
	}

	dec, err := d.s.DecoderOf(header.typ)
	if err != nil {
		return err
	}
	s := d.s
	for _, optFunc := range optFuncs {
		optFunc(s.Option)
	}
	return s.Decode(dec, header.typ, header.ptr)
}

func (d *Decoder) More() bool {
	return d.s.More()
}

func (d *Decoder) Token() (Token, error) {
	return d.s.Token()
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (d *Decoder) DisallowUnknownFields() {
	d.s.Option.Flags |= decoder.DisallowUnknownFieldsOption
}

func (d *Decoder) InputOffset() int64 {
	return d.s.InputOffset()
}

// UseNumber causes the Decoder to unmarshal a number into an interface{} as a
// Number instead of as a float64.
func (d *Decoder) UseNumber() {
	d.s.Option.Flags |= decoder.UseNumberOption
}
