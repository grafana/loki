package decoder

import (
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type mapDecoder struct {
	mapType   reflect.Type
	keyType   reflect.Type
	valueType reflect.Type
	// mapTypePtr, keyPtrType and valuePtrType are the type descriptors of the map, of *K and of *V.
	mapTypePtr   unsafe.Pointer
	keyPtrType   unsafe.Pointer
	valuePtrType unsafe.Pointer
	// isStringAnyMap is whether the map is a map[string]interface{}, which is decoded without reflect.
	isStringAnyMap bool
	keyDecoder     Decoder
	valueDecoder   Decoder
	// keyUnsupported is set for a map whose keys are not decoded ( see mapKeySupported ): an object is a type
	// error.
	keyUnsupported bool
	structName     string
	fieldName      string
	// temps holds the zero values which the key and the value of an entry are decoded into ( see mapTemps ).
	temps sync.Pool
}

// mapTemps are the zero values of the key and of the value of a map, into which the key and the value of every
// entry are decoded, which reflect.Value.SetMapIndex copies into the map, and which are zeroed after it: a
// decoder keeps them in a pool, so that a map is decoded without allocating them.
type mapTemps struct {
	k, v   unsafe.Pointer
	kv, vv reflect.Value
}

func newMapDecoder(mapType reflect.Type, keyType reflect.Type, keyDec Decoder, valueType reflect.Type, valueDec Decoder, structName, fieldName string) *mapDecoder {
	d := &mapDecoder{
		mapType:        mapType,
		keyDecoder:     keyDec,
		keyType:        keyType,
		valueType:      valueType,
		mapTypePtr:     runtime.TypePtr(mapType),
		keyPtrType:     ptrTypeOf(keyType),
		valuePtrType:   ptrTypeOf(valueType),
		isStringAnyMap: mapType == interfaceMapType,
		valueDecoder:   valueDec,
		keyUnsupported: !mapKeySupported(keyType, keyDec),
		structName:     structName,
		fieldName:      fieldName,
	}
	d.temps.New = func() any {
		t := &mapTemps{k: newValue(keyType), v: newValue(valueType)}
		t.kv, t.vv = valueAt(d.keyPtrType, t.k), valueAt(d.valuePtrType, t.v)
		return t
	}
	return d
}

// mapValue returns the reflect.Value of the map m.
func (d *mapDecoder) mapValue(m unsafe.Pointer) reflect.Value {
	// A map is a pointer in an interface value.
	return reflect.ValueOf(*(*any)(unsafe.Pointer(&emptyInterface{typ: d.mapTypePtr, ptr: m})))
}

// decodeEntries decodes the entries of the object from cursor into the map, whose keys and values are decoded into
// k and v, as Decode does, but the entries whose keys are of another type ( see errMapKeyType ), which are dropped.
// If afterKey is set, cursor is after the key of such an entry, whose value is decoded first. It is a function of
// its own, so that Decode keeps the size it had: such a key is rare.
//
//go:noinline
func (d *mapDecoder) decodeEntries(ctx *RuntimeContext, cursor, depth int64, p, mapValue, k, v unsafe.Pointer, afterKey bool) (int64, error) {
	buf := ctx.Buf
	mv := d.mapValue(mapValue)
	kv := valueAt(d.keyPtrType, k)
	vv := valueAt(d.valuePtrType, v)
	for {
		drop := afterKey
		if !afterKey {
			keyCursor, err := d.keyDecoder.Decode(ctx, cursor, depth, k)
			if err != nil && err != errMapKeyType {
				return 0, err
			}
			if buf[keyCursor-1] != '"' {
				return 0, errKeyNotString(buf, cursor)
			}
			drop = err != nil
			cursor = keyCursor
		}
		afterKey = false
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] != ':' {
			return 0, errors.ErrExpected("colon after object key", cursor)
		}
		var valueCursor int64
		var err error
		if drop {
			// the value of an entry which is dropped is skipped, as encoding/json does
			valueCursor, err = skipValue(buf, cursor+1, depth)
		} else if valueCursor, err = d.valueDecoder.Decode(ctx, cursor+1, depth, v); err == nil {
			mv.SetMapIndex(kv, vv)
		}
		if err != nil {
			return 0, err
		}
		kv.SetZero()
		vv.SetZero()
		cursor = skipWhiteSpace(buf, valueCursor)
		if buf[cursor] == '}' {
			**(**unsafe.Pointer)(unsafe.Pointer(&p)) = mapValue
			return cursor + 1, nil
		}
		if buf[cursor] != ',' {
			return 0, errors.ErrExpected("comma after object value", cursor)
		}
		cursor++
	}
}

// errKeyNotString returns the syntax error of the key of an object at cursor, which is not a string.
//
//go:noinline
func errKeyNotString(buf []byte, cursor int64) error {
	cursor = skipWhiteSpace(buf, cursor)
	return errors.ErrInvalidBeginningOfValue(buf[cursor], cursor)
}

// decodeOther skips the value at cursor, which is not an object: a value of another kind is a type error, and
// anything else a syntax error. It is a function of its own, so that Decode keeps the size it had.
//
//go:noinline
func (d *mapDecoder) decodeOther(ctx *RuntimeContext, cursor, depth int64) (int64, error) {
	if isOtherValue(ctx.Buf[cursor], objectValue) {
		return ctx.skipTypeError(cursor, depth, d.mapType)
	}
	return 0, errors.ErrExpected("{ character for map value", cursor)
}

func (d *mapDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	buflen := int64(len(buf))
	if buflen < 2 {
		return 0, errors.ErrExpected("{} for map", cursor)
	}
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		cursor += 4
		**(**unsafe.Pointer)(unsafe.Pointer(&p)) = nil
		return cursor, nil
	case '{':
		// only an object nests its values one level deeper: null or another value at the limit is not too deep
		depth++
		if depth > maxDecodeNestingDepth {
			return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
		}
	default:
		return d.decodeOther(ctx, cursor, depth)
	}
	if d.keyUnsupported {
		return ctx.unsupportedMapKeys(d, cursor, depth-1, p)
	}
	if d.isStringAnyMap {
		m := *(*map[string]any)(p)
		if m == nil {
			m, c, err := decodeNewStringAnyMap(ctx, d.valueDecoder.(*interfaceDecoder), cursor, depth)
			if err != nil {
				return 0, err
			}
			*(*map[string]any)(p) = m
			return c, nil
		}
		c, err := decodeStringAnyMap(ctx, d.valueDecoder.(*interfaceDecoder), m, cursor, depth)
		if err != nil {
			return 0, err
		}
		return c, nil
	}
	cursor++
	cursor = skipWhiteSpace(buf, cursor)
	mapValue := *(*unsafe.Pointer)(p)
	if mapValue == nil {
		mapValue = reflect.MakeMap(d.mapType).UnsafePointer()
	}
	if buf[cursor] == '}' {
		**(**unsafe.Pointer)(unsafe.Pointer(&p)) = mapValue
		cursor++
		return cursor, nil
	}
	// The key and the value of every entry are decoded into the same zero values ( see mapTemps ), which go back
	// to the pool when the object ends: after an error, they may have values, and are left to the collector.
	t := d.temps.Get().(*mapTemps)
	k, v, kv, vv := t.k, t.v, t.kv, t.vv
	mv := d.mapValue(mapValue)
	for {
		keyCursor, err := d.keyDecoder.Decode(ctx, cursor, depth, k)
		if err != nil {
			if err == errMapKeyType {
				// a key of another type: the rest of the object is decoded by decodeEntries
				return d.decodeEntries(ctx, keyCursor, depth, p, mapValue, k, v, true)
			}
			return 0, err
		}
		if buf[keyCursor-1] != '"' {
			// a key is a string, which ends with a quote, but its decoder may decode a value of any kind
			return 0, errKeyNotString(buf, cursor)
		}
		cursor = skipWhiteSpace(buf, keyCursor)
		if buf[cursor] != ':' {
			return 0, errors.ErrExpected("colon after object key", cursor)
		}
		cursor++
		valueCursor, err := d.valueDecoder.Decode(ctx, cursor, depth, v)
		if err != nil {
			return 0, err
		}
		mv.SetMapIndex(kv, vv)
		kv.SetZero()
		vv.SetZero()
		cursor = skipWhiteSpace(buf, valueCursor)
		if buf[cursor] == '}' {
			**(**unsafe.Pointer)(unsafe.Pointer(&p)) = mapValue
			d.temps.Put(t)
			cursor++
			return cursor, nil
		}
		if buf[cursor] != ',' {
			return 0, errors.ErrExpected("comma after object value", cursor)
		}
		cursor++
	}
}

func (d *mapDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	buflen := int64(len(buf))
	if buflen < 2 {
		return nil, 0, errors.ErrExpected("{} for map", cursor)
	}
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return nil, 0, err
		}
		cursor += 4
		return [][]byte{nullbytes}, cursor, nil
	case '{':
		depth++
		if depth > maxDecodeNestingDepth {
			return nil, 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
		}
	default:
		return nil, 0, errors.ErrExpected("{ character for map value", cursor)
	}
	cursor++
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == '}' {
		cursor++
		return nil, cursor, nil
	}
	keyDecoder, ok := d.keyDecoder.(*stringDecoder)
	if !ok {
		return nil, 0, &errors.UnmarshalTypeError{
			Value:  "string",
			Type:   reflect.TypeOf(""),
			Offset: cursor,
			Struct: d.structName,
			Field:  d.fieldName,
		}
	}
	ret := [][]byte{}
	for {
		if buf[skipWhiteSpace(buf, cursor)] != '"' {
			return nil, 0, errKeyNotString(buf, cursor)
		}
		key, keyCursor, err := keyDecoder.decodeByte(buf, cursor)
		if err != nil {
			return nil, 0, err
		}
		cursor = skipWhiteSpace(buf, keyCursor)
		if buf[cursor] != ':' {
			return nil, 0, errors.ErrExpected("colon after object key", cursor)
		}
		cursor++
		node := ctx.Option.Path.node
		child, found, err := ctx.Option.Path.Field(string(key))
		if err != nil {
			return nil, 0, err
		}
		// .. selects its name in the value too, at any depth: the value is searched with it
		_, recursive := node.(*PathRecursiveNode)
		start := cursor
		if found && child != nil {
			ctx.Option.Path.node = child
			paths, c, err := d.valueDecoder.DecodePath(ctx, cursor, depth)
			ctx.Option.Path.node = node
			if _, mismatch := err.(*errors.PathError); mismatch && recursive {
				// the rest of the path after .. does not fit the value, so it selects nothing in it
				paths = nil
				c, err = skipValue(buf, cursor, depth)
			}
			if err != nil {
				return nil, 0, err
			}
			ret = append(ret, paths...)
			cursor = c
		}
		switch {
		case recursive:
			paths, c, err := d.valueDecoder.DecodePath(ctx, start, depth)
			if err != nil {
				return nil, 0, err
			}
			if found && child == nil {
				// the value is selected too, which the same pass ends
				ret = append(ret, buf[start:c])
			}
			ret = append(ret, paths...)
			cursor = c
		case found && child == nil:
			end, err := skipValue(buf, cursor, depth)
			if err != nil {
				return nil, 0, err
			}
			ret = append(ret, buf[start:end])
			cursor = end
		case !found:
			c, err := skipValue(buf, cursor, depth)
			if err != nil {
				return nil, 0, err
			}
			cursor = c
		}
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] == '}' {
			cursor++
			return ret, cursor, nil
		}
		if buf[cursor] != ',' {
			return nil, 0, errors.ErrExpected("comma after object value", cursor)
		}
		cursor++
	}
}
