package vm_color

import (
	"encoding/json"
	"fmt"
	"math"
	"unsafe"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/runtime"
)

var (
	errUnsupportedFloat = encoder.ErrUnsupportedFloat
)

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

func errUnimplementedOp(op encoder.OpType) error {
	return fmt.Errorf("encoder: opcode %s has not been implemented", op)
}

// load / store are for the half of a slot for a pointer, and loadInt / storeInt are for the other half.
// A pointer is stored as uintptr, which needs no write barrier: see encoder.Slot.

func load(base unsafe.Pointer, idx uint32) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Add(base, idx))
}

func store(base unsafe.Pointer, idx uint32, p unsafe.Pointer) {
	*(*uintptr)(unsafe.Add(base, idx)) = uintptr(p)
}

func loadInt(base unsafe.Pointer, idx uint32) uintptr {
	return *(*uintptr)(unsafe.Add(base, idx))
}

func storeInt(base unsafe.Pointer, idx uint32, v uintptr) {
	*(*uintptr)(unsafe.Add(base, idx)) = v
}

func ptrToUint64(p unsafe.Pointer, bitSize uint8) uint64 {
	switch bitSize {
	case 8:
		return uint64(*(*uint8)(p))
	case 16:
		return uint64(*(*uint16)(p))
	case 32:
		return uint64(*(*uint32)(p))
	case 64:
		return *(*uint64)(p)
	}
	return 0
}
func ptrToFloat32(p unsafe.Pointer) float32 { return *(*float32)(p) }

// isInfOrNaN is whether the float is not finite, which encoding/json refuses: the exponent is all ones.
// math.IsInf is not inlined into Run, which is a big function to the inliner; this is.
func isInfOrNaN(v float64) bool {
	const exponent = 0x7ff << 52
	return math.Float64bits(v)&exponent == exponent
}
func ptrToFloat64(p unsafe.Pointer) float64            { return *(*float64)(p) }
func ptrToBool(p unsafe.Pointer) bool                  { return *(*bool)(p) }
func ptrToBytes(p unsafe.Pointer) []byte               { return *(*[]byte)(p) }
func ptrToNumber(p unsafe.Pointer) json.Number         { return *(*json.Number)(p) }
func ptrToString(p unsafe.Pointer) string              { return *(*string)(p) }
func ptrToSlice(p unsafe.Pointer) *runtime.SliceHeader { return (*runtime.SliceHeader)(p) }
func ptrToPtr(p unsafe.Pointer) unsafe.Pointer         { return *(*unsafe.Pointer)(p) }

// ptrToNPtr follows the pointer ptrNum times, or up to a nil one. It is written to be inlined into Run,
// which is a big function: only a function whose cost is 20 or less is inlined into it.
func ptrToNPtr(p unsafe.Pointer, ptrNum uint8) unsafe.Pointer {
	for ; ptrNum > 0 && p != nil; ptrNum-- {
		p = *(*unsafe.Pointer)(p)
	}
	return p
}

func ptrToInterface(code *encoder.Opcode, p unsafe.Pointer) any {
	return *(*any)(unsafe.Pointer(&emptyInterface{
		typ: code.Type,
		ptr: p,
	}))
}

func appendInt(ctx *encoder.RuntimeContext, b []byte, p unsafe.Pointer, code *encoder.Opcode) []byte {
	format := ctx.Option.ColorScheme.Int
	b = append(b, format.Header...)
	b = encoder.AppendInt(ctx, b, p, code)
	return append(b, format.Footer...)
}

func appendUint(ctx *encoder.RuntimeContext, b []byte, p unsafe.Pointer, code *encoder.Opcode) []byte {
	format := ctx.Option.ColorScheme.Uint
	b = append(b, format.Header...)
	b = encoder.AppendUint(ctx, b, p, code)
	return append(b, format.Footer...)
}

// appendFloat32 appends the float, or returns the error for one which is not finite as encoding/json does.
func appendFloat32(ctx *encoder.RuntimeContext, b []byte, v float32) ([]byte, error) {
	if isInfOrNaN(float64(v)) {
		return nil, errUnsupportedFloat(float64(v))
	}
	format := ctx.Option.ColorScheme.Float
	b = append(b, format.Header...)
	b = encoder.AppendFloat32(ctx, b, v)
	return append(b, format.Footer...), nil
}

func appendFloat64(ctx *encoder.RuntimeContext, b []byte, v float64) []byte {
	format := ctx.Option.ColorScheme.Float
	b = append(b, format.Header...)
	b = encoder.AppendFloat64(ctx, b, v)
	return append(b, format.Footer...)
}

func appendString(ctx *encoder.RuntimeContext, b []byte, v string) []byte {
	format := ctx.Option.ColorScheme.String
	b = append(b, format.Header...)
	b = encoder.AppendString(ctx, b, v)
	return append(b, format.Footer...)
}

func appendByteSlice(ctx *encoder.RuntimeContext, b []byte, src []byte) []byte {
	format := ctx.Option.ColorScheme.Binary
	b = append(b, format.Header...)
	b = encoder.AppendByteSlice(ctx, b, src)
	return append(b, format.Footer...)
}

func appendNumber(ctx *encoder.RuntimeContext, b []byte, n json.Number) ([]byte, error) {
	format := ctx.Option.ColorScheme.Int
	b = append(b, format.Header...)
	bb, err := encoder.AppendNumber(ctx, b, n)
	if err != nil {
		return nil, err
	}
	return append(bb, format.Footer...), nil
}

func appendBool(ctx *encoder.RuntimeContext, b []byte, v bool) []byte {
	format := ctx.Option.ColorScheme.Bool
	b = append(b, format.Header...)
	if v {
		b = append(b, "true"...)
	} else {
		b = append(b, "false"...)
	}
	return append(b, format.Footer...)
}

func appendNull(ctx *encoder.RuntimeContext, b []byte) []byte {
	format := ctx.Option.ColorScheme.Null
	b = append(b, format.Header...)
	b = append(b, "null"...)
	return append(b, format.Footer...)
}

func appendComma(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, ',')
}

func appendNullComma(ctx *encoder.RuntimeContext, b []byte) []byte {
	format := ctx.Option.ColorScheme.Null
	b = append(b, format.Header...)
	b = append(b, "null"...)
	return append(append(b, format.Footer...), ',')
}

func appendColon(_ *encoder.RuntimeContext, b []byte) []byte {
	last := len(b) - 1
	b[last] = ':'
	return b
}

func appendMapKeyValue(_ *encoder.RuntimeContext, _ *encoder.Opcode, b, key, value []byte) []byte {
	b = append(b, key[:len(key)-1]...)
	b = append(b, ':')
	return append(b, value...)
}

func appendMapEnd(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	b[last] = '}'
	b = append(b, ',')
	return b
}

func appendMarshalJSON(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	return encoder.AppendMarshalJSON(ctx, code, b, p)
}

func appendMarshalText(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	format := ctx.Option.ColorScheme.String
	b = append(b, format.Header...)
	bb, err := encoder.AppendMarshalText(ctx, code, b, p)
	if err != nil {
		return nil, err
	}
	return append(bb, format.Footer...), nil
}

func appendArrayHead(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte {
	return append(b, '[')
}

func appendArrayEnd(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	b[last] = ']'
	return append(b, ',')
}

func appendEmptyArray(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '[', ']', ',')
}

func appendEmptyObject(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '{', '}', ',')
}

func appendObjectEnd(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	b[last] = '}'
	return append(b, ',')
}

func appendStructHead(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '{')
}

func appendStructKey(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	format := ctx.Option.ColorScheme.ObjectKey
	b = append(b, format.Header...)
	b = append(b, code.Key[:len(code.Key)-1]...)
	b = append(b, format.Footer...)

	return append(b, ':')
}

func appendStructEnd(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte {
	return append(b, '}', ',')
}

// appendLongStructKey appends a key of any length: every key is written by appendStructKey here.
func appendLongStructKey(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	return appendStructKey(ctx, code, b)
}

func appendStructEndSkipLast(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	if b[last] == ',' {
		b[last] = '}'
		return appendComma(ctx, b)
	}
	return appendStructEnd(ctx, code, b)
}

func appendMapKeyIndent(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte    { return b }
func appendArrayElemIndent(_ *encoder.RuntimeContext, _ *encoder.Opcode, b []byte) []byte { return b }

// appendScalar appends the value at p by the opcode of a scalar, with the comma: what the VM does for the opcode.
// It is for a scalar held by an interface value, which is encoded without a frame.
//
//go:noinline
func appendScalar(ctx *encoder.RuntimeContext, b []byte, code *encoder.Opcode, p unsafe.Pointer) ([]byte, error) {
	switch code.Op {
	case encoder.OpInt:
		b = appendInt(ctx, b, p, code)
	case encoder.OpUint:
		b = appendUint(ctx, b, p, code)
	case encoder.OpFloat32:
		bb, err := appendFloat32(ctx, b, ptrToFloat32(p))
		if err != nil {
			return nil, err
		}
		b = bb
	case encoder.OpFloat64:
		v := ptrToFloat64(p)
		if isInfOrNaN(v) {
			return nil, errUnsupportedFloat(v)
		}
		b = appendFloat64(ctx, b, v)
	case encoder.OpString:
		b = appendString(ctx, b, ptrToString(p))
	case encoder.OpBool:
		b = appendBool(ctx, b, ptrToBool(p))
	case encoder.OpBytes:
		b = appendByteSlice(ctx, b, ptrToBytes(p))
	case encoder.OpNumber:
		bb, err := appendNumber(ctx, b, ptrToNumber(p))
		if err != nil {
			return nil, err
		}
		b = bb
	default:
		return nil, errUnimplementedOp(code.Op)
	}
	return appendComma(ctx, b), nil
}
