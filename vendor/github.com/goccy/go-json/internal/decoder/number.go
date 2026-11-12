package decoder

import (
	"encoding/json"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonnum"
)

type numberDecoder struct {
	stringDecoder *stringDecoder
	op            func(unsafe.Pointer, json.Number)
	structName    string
	fieldName     string
}

func newNumberDecoder(structName, fieldName string, op func(unsafe.Pointer, json.Number)) *numberDecoder {
	return &numberDecoder{
		stringDecoder: newStringDecoder(structName, fieldName),
		op:            op,
		structName:    structName,
		fieldName:     fieldName,
	}
}

func (d *numberDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	switch c := buf[cursor]; {
	case c == '-' || c-'0' <= 9:
		end, err := numberEnd(buf, cursor)
		if err != nil {
			return 0, err
		}
		d.op(p, json.Number(ctx.makeString(buf[cursor:end])))
		return end, nil
	case c == '"', c == 'n':
	case isOtherValue(c, numberValue):
		return ctx.numberKindError(cursor, depth, jsonNumberType)
	}
	bytes, c, err := d.decodeByte(buf, cursor)
	if err != nil {
		return 0, err
	}
	if bytes == nil {
		// null, which is ignored
		return c, nil
	}
	if !jsonnum.IsValid(bytes) {
		return ctx.numberStringError(start, c, jsonNumberType)
	}
	// a number in a string: its bytes end before the quote
	d.op(p, json.Number(ctx.makeString(bytes)))
	return c, nil
}

func (d *numberDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	bytes, c, err := d.decodeByte(ctx.Buf, cursor)
	if err != nil {
		return nil, 0, err
	}
	if bytes == nil {
		return [][]byte{nullbytes}, c, nil
	}
	return [][]byte{bytes}, c, nil
}

func (d *numberDecoder) decodeByte(buf []byte, cursor int64) ([]byte, int64, error) {
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
		case '"':
			return d.stringDecoder.decodeByte(buf, cursor)
		case nul:
			return nil, 0, errors.ErrUnexpectedEndOfJSON("json.Number", cursor)
		default:
			return nil, 0, errors.ErrInvalidBeginningOfValue(buf[cursor], cursor+1)
		}
	}
}
