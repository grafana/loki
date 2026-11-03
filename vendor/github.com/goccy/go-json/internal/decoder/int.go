package decoder

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type intDecoder struct {
	typ        reflect.Type
	kind       reflect.Kind
	op         func(unsafe.Pointer, int64)
	structName string
	fieldName  string
}

func newIntDecoder(typ reflect.Type, structName, fieldName string, op func(unsafe.Pointer, int64)) *intDecoder {
	return &intDecoder{
		typ:        typ,
		kind:       intKindOf(typ),
		op:         op,
		structName: structName,
		fieldName:  fieldName,
	}
}

// intKindOf returns the kind of the integers of the size of typ, whose range the decoder checks: an int is an
// int32 on a 32-bit platform.
func intKindOf(typ reflect.Type) reflect.Kind {
	switch typ.Size() {
	case 1:
		return reflect.Int8
	case 2:
		return reflect.Int16
	case 4:
		return reflect.Int32
	}
	return reflect.Int64
}

func (d *intDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if ctx.Option.V2&(V2Semantics|V2MergeWithLegacySemantics) == V2Semantics {
			// the semantics of encoding/json/v2 decode null as the zero value, but with MergeWithLegacySemantics
			d.op(p, 0)
		}
		return cursor + 4, nil
	case '-':
		cursor++
	}
	if buf[cursor]-'0' > 9 {
		return d.decodeSlow(ctx, start, depth, p)
	}
	u, next, digits := parseDigits(buf, cursor)
	if isFloatContinuation(buf[next]) || digits > maxUint64Digits {
		return d.decodeSlow(ctx, start, depth, p)
	}
	neg := cursor > start
	if (neg && u > 1<<63) || (!neg && u > 1<<63-1) {
		return d.decodeSlow(ctx, start, depth, p)
	}
	i64 := int64(u)
	if neg {
		i64 = -i64
	}
	switch d.kind {
	case reflect.Int8:
		if i64 < -1*(1<<7) || (1<<7) <= i64 {
			return d.decodeSlow(ctx, start, depth, p)
		}
	case reflect.Int16:
		if i64 < -1*(1<<15) || (1<<15) <= i64 {
			return d.decodeSlow(ctx, start, depth, p)
		}
	case reflect.Int32:
		if i64 < -1*(1<<31) || (1<<31) <= i64 {
			return d.decodeSlow(ctx, start, depth, p)
		}
	}
	d.op(p, i64)
	return next, nil
}

// decodeSlow decodes the value at cursor, which Decode doesn't: a value of another kind, or a number which is not
// an integer of the type, which are type errors, or a syntax error. It is a function of its own, so that Decode
// keeps the size it had.
//
//go:noinline
func (d *intDecoder) decodeSlow(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		return cursor + 4, nil
	case '-':
		cursor++
	}
	if buf[cursor]-'0' > 9 {
		if cursor > start {
			return 0, errors.ErrSyntax(fmt.Sprintf("invalid character %s in numeric literal", quoteChar(buf[cursor])), cursor+1)
		}
		// a value of another kind
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	u, next, digits := parseDigits(buf, cursor)
	if isFloatContinuation(buf[next]) || digits > maxUint64Digits {
		// a number which is not an integer, or too large for any integer
		end, err := numberEnd(buf, start)
		if err != nil {
			return 0, err
		}
		return ctx.numberError(start, end, d.typ)
	}
	neg := cursor > start
	if (neg && u > 1<<63) || (!neg && u > 1<<63-1) {
		return ctx.numberError(start, next, d.typ)
	}
	i64 := int64(u)
	if neg {
		i64 = -i64
	}
	switch d.kind {
	case reflect.Int8:
		if i64 < -1*(1<<7) || (1<<7) <= i64 {
			return ctx.numberError(start, next, d.typ)
		}
	case reflect.Int16:
		if i64 < -1*(1<<15) || (1<<15) <= i64 {
			return ctx.numberError(start, next, d.typ)
		}
	case reflect.Int32:
		if i64 < -1*(1<<31) || (1<<31) <= i64 {
			return ctx.numberError(start, next, d.typ)
		}
	}
	d.op(p, i64)
	return next, nil
}

func (d *intDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: int decoder does not support decode path")
}
