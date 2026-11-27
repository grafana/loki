package decoder

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/jsonnum"
)

type uintDecoder struct {
	typ        reflect.Type
	kind       reflect.Kind
	op         func(unsafe.Pointer, uint64)
	structName string
	fieldName  string
}

func newUintDecoder(typ reflect.Type, structName, fieldName string, op func(unsafe.Pointer, uint64)) *uintDecoder {
	return &uintDecoder{
		typ:        typ,
		kind:       uintKindOf(typ),
		op:         op,
		structName: structName,
		fieldName:  fieldName,
	}
}

// uintKindOf returns the kind of the unsigned integers of the size of typ, whose range the decoder checks: a uint
// and a uintptr are a uint32 on a 32-bit platform.
func uintKindOf(typ reflect.Type) reflect.Kind {
	switch typ.Size() {
	case 1:
		return reflect.Uint8
	case 2:
		return reflect.Uint16
	case 4:
		return reflect.Uint32
	}
	return reflect.Uint64
}

func (d *uintDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if ctx.Option.V2&(V2Semantics|V2MergeWithLegacySemantics) == V2Semantics {
			// the semantics of encoding/json/v2 decode null as the zero value, but with MergeWithLegacySemantics
			d.op(p, 0)
		}
		return cursor + 4, nil
	}
	if buf[cursor]-'0' > 9 {
		return d.decodeSlow(ctx, cursor, depth, p)
	}
	start := cursor
	u, next, digits := parseDigits(buf, cursor)
	if isFloatContinuation(buf[next]) || digits > maxUint64Digits+1 {
		return d.decodeSlow(ctx, start, depth, p)
	}
	if digits == maxUint64Digits+1 {
		var ok bool
		if u, ok = jsonnum.TwentyDigits(buf, start); !ok {
			return d.decodeSlow(ctx, start, depth, p)
		}
	}
	switch d.kind {
	case reflect.Uint8:
		if (1 << 8) <= u {
			return d.decodeSlow(ctx, start, depth, p)
		}
	case reflect.Uint16:
		if (1 << 16) <= u {
			return d.decodeSlow(ctx, start, depth, p)
		}
	case reflect.Uint32:
		if (1 << 32) <= u {
			return d.decodeSlow(ctx, start, depth, p)
		}
	}
	d.op(p, u)
	return next, nil
}

// decodeSlow decodes the value at cursor, which Decode doesn't: a value of another kind, a negative number or
// a number which is not an integer of the type, which are type errors, or a syntax error. It is a function of its
// own, so that Decode keeps the size it had.
//
//go:noinline
func (d *uintDecoder) decodeSlow(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch c := buf[cursor]; {
	case c == 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		return cursor + 4, nil
	case c == '-':
		// a negative number, which the error reports
		end, err := numberEnd(buf, cursor)
		if err != nil {
			return 0, err
		}
		return ctx.numberError(cursor, end, d.typ)
	case c-'0' > 9:
		// a value of another kind
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	start := cursor
	u, next, digits := parseDigits(buf, cursor)
	if isFloatContinuation(buf[next]) || digits > maxUint64Digits+1 {
		// a number which is not an integer, or too large for any integer
		end, err := numberEnd(buf, start)
		if err != nil {
			return 0, err
		}
		return ctx.numberError(start, end, d.typ)
	}
	if digits == maxUint64Digits+1 {
		var ok bool
		if u, ok = jsonnum.TwentyDigits(buf, start); !ok {
			return ctx.numberError(start, next, d.typ)
		}
	}
	switch d.kind {
	case reflect.Uint8:
		if (1 << 8) <= u {
			return ctx.numberError(start, next, d.typ)
		}
	case reflect.Uint16:
		if (1 << 16) <= u {
			return ctx.numberError(start, next, d.typ)
		}
	case reflect.Uint32:
		if (1 << 32) <= u {
			return ctx.numberError(start, next, d.typ)
		}
	}
	d.op(p, u)
	return next, nil
}

func (d *uintDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: uint decoder does not support decode path")
}
