package decoder

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type arrayDecoder struct {
	elemType reflect.Type
	// arrayPtrType is the type descriptor of the pointer to the array.
	arrayPtrType unsafe.Pointer
	size         uintptr
	valueDecoder Decoder
	alen         int
	structName   string
	fieldName    string
	// typ is the type of the array, which the type errors report.
	typ reflect.Type
}

func newArrayDecoder(dec Decoder, arrayType reflect.Type, structName, fieldName string) *arrayDecoder {
	elemType := arrayType.Elem()
	return &arrayDecoder{
		typ:          arrayType,
		valueDecoder: dec,
		elemType:     elemType,
		arrayPtrType: ptrTypeOf(arrayType),
		size:         elemType.Size(),
		alen:         arrayType.Len(),
		structName:   structName,
		fieldName:    fieldName,
	}
}

// zeroFrom sets the elements of the array at p from idx to the end to their zero value,
// as encoding/json does for the elements the JSON array has not.
func (d *arrayDecoder) zeroFrom(p unsafe.Pointer, idx int) {
	if idx < d.alen {
		valueAt(d.arrayPtrType, p).Slice(idx, d.alen).Clear()
	}
}

func (d *arrayDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}

	for {
		switch buf[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
			continue
		case 'n':
			if err := validateNull(buf, cursor); err != nil {
				return 0, err
			}
			cursor += 4
			return cursor, nil
		case '[':
			idx := 0
			cursor++
			cursor = skipWhiteSpace(buf, cursor)
			if buf[cursor] == ']' {
				d.zeroFrom(p, idx)
				cursor++
				return cursor, nil
			}
			for {
				if idx < d.alen {
					c, err := d.valueDecoder.Decode(ctx, cursor, depth, unsafe.Add(p, uintptr(idx)*d.size))
					if err != nil {
						return 0, err
					}
					cursor = c
				} else {
					c, err := skipValue(buf, cursor, depth)
					if err != nil {
						return 0, err
					}
					cursor = c
				}
				idx++
				cursor = skipWhiteSpace(buf, cursor)
				switch buf[cursor] {
				case ']':
					d.zeroFrom(p, idx)
					cursor++
					return cursor, nil
				case ',':
					cursor++
					continue
				default:
					return 0, errors.ErrInvalidCharacter(buf[cursor], "array", cursor)
				}
			}
		default:
			return d.decodeOther(ctx, cursor, depth-1)
		}
	}
}

// decodeOther skips the value at cursor, which is not an array: a value of another kind is a type error, and
// anything else a syntax error. It is a function of its own, so that Decode keeps the size it had.
//
//go:noinline
func (d *arrayDecoder) decodeOther(ctx *RuntimeContext, cursor, depth int64) (int64, error) {
	if isOtherValue(ctx.Buf[cursor], arrayValue) {
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	return 0, errors.ErrUnexpectedEndOfJSON("array", cursor)
}

func (d *arrayDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: array decoder does not support decode path")
}
