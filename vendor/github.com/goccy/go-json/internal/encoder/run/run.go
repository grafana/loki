// Package run drives the encoder: it compiles the opcodes of a value, sets up the runtime context and runs the
// VM which the options choose. The v1 and the v2 json packages encode through it.
package run

import (
	"runtime"
	"unsafe"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/encoder/vm"
	"github.com/goccy/go-json/internal/encoder/vm_color"
	"github.com/goccy/go-json/internal/encoder/vm_color_indent"
	"github.com/goccy/go-json/internal/encoder/vm_indent"
	"github.com/goccy/go-json/internal/jsonstring"
)

func init() {
	encoder.RunHook = Code
}

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

// Encode appends v to the buffer of ctx as JSON, with a comma after it, by the options of ctx. On an error, it
// returns the output written before the error with it, which the v2 json package finds the place of the error by.
func Encode(ctx *encoder.RuntimeContext, v any) ([]byte, error) {
	b := ctx.Buf[:0]
	if v == nil {
		b = encoder.AppendNull(ctx, b)
		b = encoder.AppendComma(ctx, b)
		return b, nil
	}
	header := (*emptyInterface)(unsafe.Pointer(&v))
	typ := header.typ

	typeptr := uintptr(typ)
	codeSet, err := encoder.CompileToGetCodeSet(ctx, typeptr)
	if err != nil {
		return b, err
	}
	if ctx.Option.Flag&encoder.MarshalFuncsOption != 0 && header.ptr != nil {
		// a pointer at the top level, which the functions of marshaling take as the value it points to.
		if pointee := encoder.PointeeOf(ctx, codeSet); pointee != nil {
			codeSet = pointee
		}
	}

	p := ctx.ValueAddr(codeSet, header.ptr)
	if p == nil {
		// only a nil pointer has no address of the value.
		b = encoder.AppendNull(ctx, b)
		b = encoder.AppendComma(ctx, b)
		return b, nil
	}
	ctx.Init(p, codeSet.CodeLength)
	if ctx.Outer != nil {
		// the value is where the encoder of the method which called MarshalEncode is ( see ValueDepth ).
		ctx.BaseIndent = ctx.ValueDepth
		if err := encoder.EnterNested(ctx, ctx.Outer, codeSet, p); err != nil {
			return b, err
		}
	}

	buf, err := Code(ctx, b, codeSet)
	// the VM refers to the value by uintptr.
	runtime.KeepAlive(v)
	if err != nil {
		return buf, err
	}
	ctx.Buf = buf
	return buf, nil
}

// EncodeIndent is Encode with the indentation and the prefix of each line.
func EncodeIndent(ctx *encoder.RuntimeContext, v any, prefix, indent string) ([]byte, error) {
	b := ctx.Buf[:0]
	if v == nil {
		b = encoder.AppendNull(ctx, b)
		b = encoder.AppendCommaIndent(ctx, b)
		return b, nil
	}
	header := (*emptyInterface)(unsafe.Pointer(&v))
	typ := header.typ

	typeptr := uintptr(typ)
	codeSet, err := encoder.CompileToGetCodeSet(ctx, typeptr)
	if err != nil {
		return b, err
	}

	p := ctx.ValueAddr(codeSet, header.ptr)
	if p == nil {
		// only a nil pointer has no address of the value.
		b = encoder.AppendNull(ctx, b)
		b = encoder.AppendCommaIndent(ctx, b)
		return b, nil
	}
	ctx.Init(p, codeSet.CodeLength)
	// a nested run is indented as this one ( see Code )
	ctx.Option.Flag |= encoder.IndentOption
	buf, err := IndentCode(ctx, b, codeSet, prefix, indent)
	// the VM refers to the value by uintptr.
	runtime.KeepAlive(v)

	if err != nil {
		return buf, err
	}

	ctx.Buf = buf
	return buf, nil
}

// Code runs the opcodes of a value, which ctx is set up for, by the VM of the options of ctx.
func Code(ctx *encoder.RuntimeContext, b []byte, codeSet *encoder.OpcodeSet) ([]byte, error) {
	if ctx.Option.Flag&encoder.IndentOption != 0 {
		// a run nested in an indented one, with its prefix and its indentation, as of a value which its
		// MarshalJSONTo declined
		return indentRun(ctx, b, codeSet)
	}
	// the escaper of the strings ( see encoder.RuntimeContext.SetEscaper ), without a call for most options.
	// It is set again only for other options than the ones it was set for: the one of RejectInvalidUTF8Option,
	// whose record of invalid UTF-8 the caller reads and clears after each run ( see InvalidUTF8Output ), as well.
	if flags := ctx.Option.Flag; flags != ctx.Option.EscaperFlags {
		if flags&encoder.RejectInvalidUTF8Option != 0 {
			ctx.SetStrictEscaper()
		} else {
			ctx.Option.Escaper, ctx.Option.EscaperFlags = jsonstring.EscaperOf(uint(flags)), flags
		}
	}
	if (ctx.Option.Flag & encoder.DebugOption) != 0 {
		if (ctx.Option.Flag & encoder.ColorizeOption) != 0 {
			return vm_color.DebugRun(ctx, b, codeSet)
		}
		return vm.DebugRun(ctx, b, codeSet)
	}
	if (ctx.Option.Flag & encoder.ColorizeOption) != 0 {
		return vm_color.Run(ctx, b, codeSet)
	}
	return vm.Run(ctx, b, codeSet)
}

// IndentCode is Code with the indentation and the prefix of each line.
func IndentCode(ctx *encoder.RuntimeContext, b []byte, codeSet *encoder.OpcodeSet, prefix, indent string) ([]byte, error) {
	ctx.Prefix = []byte(prefix)
	ctx.IndentStr = []byte(indent)
	return indentRun(ctx, b, codeSet)
}

// indentRun is IndentCode with the prefix and the indentation which ctx has.
func indentRun(ctx *encoder.RuntimeContext, b []byte, codeSet *encoder.OpcodeSet) ([]byte, error) {
	ctx.SetEscaper()
	if (ctx.Option.Flag & encoder.DebugOption) != 0 {
		if (ctx.Option.Flag & encoder.ColorizeOption) != 0 {
			return vm_color_indent.DebugRun(ctx, b, codeSet)
		}
		return vm_indent.DebugRun(ctx, b, codeSet)
	}
	if (ctx.Option.Flag & encoder.ColorizeOption) != 0 {
		return vm_color_indent.Run(ctx, b, codeSet)
	}
	return vm_indent.Run(ctx, b, codeSet)
}
