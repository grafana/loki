package decoder

import (
	"fmt"
	"reflect"
	"unsafe"
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
		kind:       typ.Kind(),
		op:         op,
		structName: structName,
		fieldName:  fieldName,
	}
}

func (d *uintDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
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
		// 20 digits: the value of the first 19 is exact, and the last one fits if the whole is less than 1<<64.
		var hi uint64
		for i := int64(0); i < maxUint64Digits; i++ {
			hi = hi*10 + uint64(buf[start+i]-'0')
		}
		lo := uint64(buf[start+maxUint64Digits] - '0')
		if hi > (1<<64-1)/10 || (hi == (1<<64-1)/10 && lo > (1<<64-1)%10) {
			return d.decodeSlow(ctx, start, depth, p)
		}
		u = hi*10 + lo
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
		ctx.numberTypeError(cursor, end, d.typ)
		return end, nil
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
		ctx.numberTypeError(start, end, d.typ)
		return end, nil
	}
	if digits == maxUint64Digits+1 {
		// 20 digits: the value of the first 19 is exact, and the last one fits if the whole is less than 1<<64.
		var hi uint64
		for i := int64(0); i < maxUint64Digits; i++ {
			hi = hi*10 + uint64(buf[start+i]-'0')
		}
		lo := uint64(buf[start+maxUint64Digits] - '0')
		if hi > (1<<64-1)/10 || (hi == (1<<64-1)/10 && lo > (1<<64-1)%10) {
			ctx.numberTypeError(start, next, d.typ)
			return next, nil
		}
		u = hi*10 + lo
	}
	switch d.kind {
	case reflect.Uint8:
		if (1 << 8) <= u {
			ctx.numberTypeError(start, next, d.typ)
			return next, nil
		}
	case reflect.Uint16:
		if (1 << 16) <= u {
			ctx.numberTypeError(start, next, d.typ)
			return next, nil
		}
	case reflect.Uint32:
		if (1 << 32) <= u {
			ctx.numberTypeError(start, next, d.typ)
			return next, nil
		}
	}
	d.op(p, u)
	return next, nil
}

func (d *uintDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: uint decoder does not support decode path")
}
