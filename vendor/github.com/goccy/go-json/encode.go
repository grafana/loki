package json

import (
	"context"
	"io"
	"os"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/encoder/run"
)

// An Encoder writes JSON values to an output stream.
type Encoder struct {
	w                 io.Writer
	enabledIndent     bool
	enabledHTMLEscape bool
	prefix            string
	indentStr         string
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, enabledHTMLEscape: true}
}

// Encode writes the JSON encoding of v to the stream, followed by a newline character.
//
// See the documentation for Marshal for details about the conversion of Go values to JSON.
func (e *Encoder) Encode(v any) error {
	return e.EncodeWithOption(v)
}

// EncodeWithOption call Encode with EncodeOption.
func (e *Encoder) EncodeWithOption(v any, optFuncs ...EncodeOptionFunc) error {
	ctx := encoder.TakeRuntimeContext()
	ctx.Option.Flag = 0

	err := e.encodeWithOption(ctx, v, optFuncs...)

	encoder.ReleaseRuntimeContext(ctx)
	return err
}

// EncodeContext call Encode with context.Context and EncodeOption.
func (e *Encoder) EncodeContext(ctx context.Context, v any, optFuncs ...EncodeOptionFunc) error {
	rctx := encoder.TakeRuntimeContext()
	rctx.Option.Flag = 0
	rctx.Option.Flag |= encoder.ContextOption
	rctx.Option.Context = ctx

	err := e.encodeWithOption(rctx, v, optFuncs...)

	encoder.ReleaseRuntimeContext(rctx)
	return err
}

func (e *Encoder) encodeWithOption(ctx *encoder.RuntimeContext, v any, optFuncs ...EncodeOptionFunc) error {
	if e.enabledHTMLEscape {
		ctx.Option.Flag |= encoder.HTMLEscapeOption
	}
	ctx.Option.Flag |= encoder.NormalizeUTF8Option
	ctx.Option.DebugOut = os.Stdout
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}
	var (
		buf []byte
		err error
	)
	if e.enabledIndent {
		buf, err = run.EncodeIndent(ctx, v, e.prefix, e.indentStr)
	} else {
		buf, err = run.Encode(ctx, v)
	}
	if err != nil {
		return err
	}
	if e.enabledIndent {
		buf = buf[:len(buf)-2]
	} else {
		buf = buf[:len(buf)-1]
	}
	buf = append(buf, '\n')
	if _, err := e.w.Write(buf); err != nil {
		return err
	}
	return nil
}

// SetEscapeHTML specifies whether problematic HTML characters should be escaped inside JSON quoted strings.
// The default behavior is to escape &, <, and > to \u0026, \u003c, and \u003e to avoid certain safety problems that can arise when embedding JSON in HTML.
//
// In non-HTML settings where the escaping interferes with the readability of the output, SetEscapeHTML(false) disables this behavior.
func (e *Encoder) SetEscapeHTML(on bool) {
	e.enabledHTMLEscape = on
}

// SetIndent instructs the encoder to format each subsequent encoded value as if indented by the package-level function Indent(dst, src, prefix, indent).
// Calling SetIndent("", "") disables indentation.
func (e *Encoder) SetIndent(prefix, indent string) {
	if prefix == "" && indent == "" {
		e.enabledIndent = false
		return
	}
	e.prefix = prefix
	e.indentStr = indent
	e.enabledIndent = true
}

func marshalContext(ctx context.Context, v any, optFuncs ...EncodeOptionFunc) ([]byte, error) {
	rctx := encoder.TakeRuntimeContext()
	rctx.Option.Flag = 0
	rctx.Option.Flag = encoder.HTMLEscapeOption | encoder.NormalizeUTF8Option | encoder.ContextOption
	rctx.Option.Context = ctx
	for _, optFunc := range optFuncs {
		optFunc(rctx.Option)
	}

	buf, err := run.Encode(rctx, v)
	if err != nil {
		encoder.ReleaseRuntimeContext(rctx)
		return nil, err
	}

	// this line exists to escape call of `runtime.makeslicecopy` .
	// if use `make([]byte, len(buf)-1)` and `copy(copied, buf)`,
	// dst buffer size and src buffer size are differrent.
	// in this case, compiler uses `runtime.makeslicecopy`, but it is slow.
	buf = buf[:len(buf)-1]
	copied := make([]byte, len(buf))
	copy(copied, buf)

	encoder.ReleaseRuntimeContext(rctx)
	return copied, nil
}

func marshal(v any, optFuncs ...EncodeOptionFunc) ([]byte, error) {
	ctx := encoder.TakeRuntimeContext()

	ctx.Option.Flag = 0
	ctx.Option.Flag |= (encoder.HTMLEscapeOption | encoder.NormalizeUTF8Option)
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}

	buf, err := run.Encode(ctx, v)
	if err != nil {
		encoder.ReleaseRuntimeContext(ctx)
		return nil, err
	}

	// this line exists to escape call of `runtime.makeslicecopy` .
	// if use `make([]byte, len(buf)-1)` and `copy(copied, buf)`,
	// dst buffer size and src buffer size are differrent.
	// in this case, compiler uses `runtime.makeslicecopy`, but it is slow.
	buf = buf[:len(buf)-1]
	copied := make([]byte, len(buf))
	copy(copied, buf)

	encoder.ReleaseRuntimeContext(ctx)
	return copied, nil
}

func marshalIndent(v any, prefix, indent string, optFuncs ...EncodeOptionFunc) ([]byte, error) {
	ctx := encoder.TakeRuntimeContext()

	ctx.Option.Flag = 0
	ctx.Option.Flag |= (encoder.HTMLEscapeOption | encoder.NormalizeUTF8Option | encoder.IndentOption)
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}

	buf, err := run.EncodeIndent(ctx, v, prefix, indent)
	if err != nil {
		encoder.ReleaseRuntimeContext(ctx)
		return nil, err
	}

	buf = buf[:len(buf)-2]
	copied := make([]byte, len(buf))
	copy(copied, buf)

	encoder.ReleaseRuntimeContext(ctx)
	return copied, nil
}
