package vm_indent

import (
	"encoding/json"
	"fmt"
	"math"
	"unsafe"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/runtime"
)

var (
	appendInt           = encoder.AppendInt
	appendUint          = encoder.AppendUint
	appendFloat64       = encoder.AppendFloat64
	appendString        = encoder.AppendString
	appendByteSlice     = encoder.AppendByteSlice
	appendNumber        = encoder.AppendNumber
	appendStructEnd     = encoder.AppendStructEndIndent
	appendIndent        = encoder.AppendIndent
	errUnsupportedFloat = encoder.ErrUnsupportedFloat
)

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

func errUnimplementedOp(op encoder.OpType) error {
	return fmt.Errorf("encoder (indent): opcode %s has not been implemented", op)
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

// appendFloat32 appends the float, or returns the error for one which is not finite as encoding/json does.
func appendFloat32(ctx *encoder.RuntimeContext, b []byte, v float32) ([]byte, error) {
	if isInfOrNaN(float64(v)) {
		return nil, errUnsupportedFloat(float64(v))
	}
	return encoder.AppendFloat32(ctx, b, v), nil
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

func appendBool(_ *encoder.RuntimeContext, b []byte, v bool) []byte {
	if v {
		return append(b, "true"...)
	}
	return append(b, "false"...)
}

func appendNull(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, "null"...)
}

func appendComma(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, ',', '\n')
}

func appendNullComma(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, "null,\n"...)
}

func appendColon(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b[:len(b)-2], ':', ' ')
}

func appendMapKeyValue(ctx *encoder.RuntimeContext, code *encoder.Opcode, b, key, value []byte) []byte {
	b = appendIndent(ctx, b, code.Indent+1)
	b = append(b, key...)
	b[len(b)-2] = ':'
	b[len(b)-1] = ' '
	return append(b, value...)
}

func appendMapEnd(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	b = b[:len(b)-2]
	b = append(b, '\n')
	b = appendIndent(ctx, b, code.Indent)
	return append(b, '}', ',', '\n')
}

func appendArrayHead(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	b = append(b, '[', '\n')
	return appendIndent(ctx, b, code.Indent+1)
}

func appendArrayEnd(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	b = b[:len(b)-2]
	b = append(b, '\n')
	b = appendIndent(ctx, b, code.Indent)
	return append(b, ']', ',', '\n')
}

func appendEmptyArray(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '[', ']', ',', '\n')
}

func appendEmptyObject(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '{', '}', ',', '\n')
}

func appendObjectEnd(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	// replace comma to newline
	b[last-1] = '\n'
	b = appendIndent(ctx, b[:last], code.Indent)
	return append(b, '}', ',', '\n')
}

func appendMarshalJSON(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	return encoder.AppendMarshalJSONIndent(ctx, code, b, p)
}

func appendMarshalText(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	return encoder.AppendMarshalTextIndent(ctx, code, b, p)
}

func appendStructHead(_ *encoder.RuntimeContext, b []byte) []byte {
	return append(b, '{', '\n')
}

func appendStructKey(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	b = appendIndent(ctx, b, code.Indent)
	b = append(b, code.Key...)
	return append(b, ' ')
}

// appendLongStructKey appends a key of any length: every key is written by appendStructKey here.
func appendLongStructKey(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	return appendStructKey(ctx, code, b)
}

func appendStructEndSkipLast(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	last := len(b) - 1
	if b[last-1] == '{' {
		b[last] = '}'
	} else {
		if b[last] == '\n' {
			// to remove ',' and '\n' characters
			b = b[:len(b)-2]
		}
		b = append(b, '\n')
		b = appendIndent(ctx, b, code.Indent-1)
		b = append(b, '}')
	}
	return appendComma(ctx, b)
}

func appendArrayElemIndent(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	return appendIndent(ctx, b, code.Indent+1)
}

// appendMapKeyIndent indents an entry of a map, whose opcode is the one of the map or of its key: one deeper
// than the braces of the map, which appendObjectEnd indents by the same opcode.
func appendMapKeyIndent(ctx *encoder.RuntimeContext, code *encoder.Opcode, b []byte) []byte {
	return appendIndent(ctx, b, code.Indent+1)
}

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
