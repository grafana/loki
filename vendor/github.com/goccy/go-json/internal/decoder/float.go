package decoder

import (
	"math"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type floatDecoder struct {
	op         func(unsafe.Pointer, float64)
	structName string
	fieldName  string
	// is32 is set for a float32, whose range is checked: a number out of it is a type error.
	is32 bool
	// typ is the type of the value, which the type errors report.
	typ reflect.Type
}

func newFloatDecoder(structName, fieldName string, op func(unsafe.Pointer, float64)) *floatDecoder {
	return &floatDecoder{op: op, structName: structName, fieldName: fieldName, typ: reflect.TypeOf(float64(0))}
}

// overflowsFloat32 reports whether f is out of the range of a float32, as reflect.Value.OverflowFloat does.
func overflowsFloat32(f float64) bool {
	if f < 0 {
		f = -f
	}
	return math.MaxFloat32 < f && f <= math.MaxFloat64
}

var (
	floatTable = [256]bool{
		'0': true,
		'1': true,
		'2': true,
		'3': true,
		'4': true,
		'5': true,
		'6': true,
		'7': true,
		'8': true,
		'9': true,
		'.': true,
		'e': true,
		'E': true,
		'+': true,
		'-': true,
	}

	validEndNumberChar = [256]bool{
		nul:  true,
		' ':  true,
		'\t': true,
		'\r': true,
		'\n': true,
		',':  true,
		':':  true,
		'}':  true,
		']':  true,
	}
)

func (d *floatDecoder) decodeByte(buf []byte, cursor int64) ([]byte, int64, error) {
	for {
		switch buf[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
			continue
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			start := cursor
			cursor++
			for floatTable[buf[cursor]] {
				cursor++
			}
			num := buf[start:cursor]
			return num, cursor, nil
		case 'n':
			if err := validateNull(buf, cursor); err != nil {
				return nil, 0, err
			}
			cursor += 4
			return nil, cursor, nil
		default:
			return nil, 0, errors.ErrUnexpectedEndOfJSON("float", cursor)
		}
	}
}

func (d *floatDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if f, next, ok := parseFloatFast(buf, cursor); ok && validEndNumberChar[buf[next]] {
		d.op(p, f)
		return next, nil
	}
	return d.decodeSlow(ctx, cursor, depth, p)
}

// float32Decoder is the decoder of a float32, whose range is checked: a number out of it is a type error. It is a
// type of its own, so that the decoder of a float64 checks nothing more.
type float32Decoder struct {
	floatDecoder
}

func (d *float32Decoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if f, next, ok := parseFloatFast(buf, cursor); ok && validEndNumberChar[buf[next]] && !overflowsFloat32(f) {
		d.op(p, f)
		return next, nil
	}
	return d.decodeSlow(ctx, cursor, depth, p)
}

// decodeSlow decodes the value at cursor which the fast parse doesn't: null, a number which it can't parse, a
// number out of the range of the type, which is a type error, or a value of another kind, which is a type error
// too. It is a function of its own, so that Decode keeps the size it had.
//
//go:noinline
func (d *floatDecoder) decodeSlow(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	switch c := buf[cursor]; {
	case c == 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		return cursor + 4, nil
	case c == '-' || c-'0' <= 9:
		end, err := numberEnd(buf, cursor)
		if err != nil {
			return 0, err
		}
		b := buf[cursor:end]
		f, parseErr := strconv.ParseFloat(*(*string)(unsafe.Pointer(&b)), 64)
		// a number of the grammar which ParseFloat fails is out of the range of float64
		if inRange := parseErr == nil && !(d.is32 && overflowsFloat32(f)); !inRange {
			ctx.numberTypeError(cursor, end, d.typ)
			if storesFloatsOutOfRange {
				// ±Inf, which ParseFloat returns, or which the conversion to a float32 makes
				d.op(p, f)
			}
			return end, nil
		}
		d.op(p, f)
		return end, nil
	case isOtherValue(c, numberValue):
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	return 0, errors.ErrUnexpectedEndOfJSON("float", cursor)
}

func (d *floatDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	buf := ctx.Buf
	bytes, c, err := d.decodeByte(buf, cursor)
	if err != nil {
		return nil, 0, err
	}
	if bytes == nil {
		return [][]byte{nullbytes}, c, nil
	}
	return [][]byte{bytes}, c, nil
}
