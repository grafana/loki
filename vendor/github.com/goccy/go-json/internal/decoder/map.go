package decoder

import (
	"reflect"
	"sync"
	"sync/atomic"
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
	// keyUnsupported is set for a map whose keys encoding/json of the Go version doesn't decode ( see
	// mapKeySupported ): an object is a type error.
	keyUnsupported bool
	structName     string
	fieldName      string
	// temps holds the zero values which the key and the value of an entry are decoded into ( see mapTemps ).
	temps sync.Pool
	// lastLen is the number of the entries of the object which the decoder decoded last, up to maxMapSizeHint,
	// which a new map is made for: the objects of a kind often have a number of entries. It is only a hint, for
	// any goroutine.
	lastLen atomic.Int32
}

// maxMapSizeHint is the largest number of the entries which a new map is made for ( see lastLen ).
const maxMapSizeHint = 64

// mapTemps are the zero values of the key and of the value of a map, into which the key and the value of every
// entry are decoded, which reflect.Value.SetMapIndex copies into the map, and which are zeroed after it: a
// decoder keeps them in a pool, so that a map is decoded without allocating them.
type mapTemps struct {
	k, v   unsafe.Pointer
	kv, vv reflect.Value
}

func newMapDecoder(mapType reflect.Type, keyType reflect.Type, keyDec Decoder, valueType reflect.Type, valueDec Decoder, structName, fieldName string) *mapDecoder {
	_, isIfaceValue := valueDec.(*interfaceDecoder)
	d := &mapDecoder{
		mapType:        mapType,
		keyDecoder:     keyDec,
		keyType:        keyType,
		valueType:      valueType,
		mapTypePtr:     runtime.TypePtr(mapType),
		keyPtrType:     ptrTypeOf(keyType),
		valuePtrType:   ptrTypeOf(valueType),
		isStringAnyMap: mapType == interfaceMapType && isIfaceValue,
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
			drop = err != nil
			cursor = keyCursor
		}
		afterKey = false
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] != ':' {
			return 0, errors.ErrExpected("colon after object key", cursor)
		}
		valueCursor, err := d.valueDecoder.Decode(ctx, cursor+1, depth, v)
		if err != nil {
			return 0, err
		}
		if !drop {
			mv.SetMapIndex(kv, vv)
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
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}

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
	default:
		return d.decodeOther(ctx, cursor, depth-1)
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
		// made for the number of the entries of the last object, which an object of the kind is likely to have
		mapValue = reflect.MakeMapWithSize(d.mapType, int(d.lastLen.Load())).UnsafePointer()
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
	n := int32(0)
	for {
		keyCursor, err := d.keyDecoder.Decode(ctx, cursor, depth, k)
		if err != nil {
			if err == errMapKeyType {
				// a key of another type: the rest of the object is decoded by decodeEntries
				return d.decodeEntries(ctx, keyCursor, depth, p, mapValue, k, v, true)
			}
			return 0, err
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
		n++
		cursor = skipWhiteSpace(buf, valueCursor)
		if buf[cursor] == '}' {
			**(**unsafe.Pointer)(unsafe.Pointer(&p)) = mapValue
			d.temps.Put(t)
			d.lastLen.Store(min(n, maxMapSizeHint))
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
	depth++
	if depth > maxDecodeNestingDepth {
		return nil, 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}

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
		key, keyCursor, err := keyDecoder.decodeByte(buf, cursor)
		if err != nil {
			return nil, 0, err
		}
		cursor = skipWhiteSpace(buf, keyCursor)
		if buf[cursor] != ':' {
			return nil, 0, errors.ErrExpected("colon after object key", cursor)
		}
		cursor++
		child, found, err := ctx.Option.Path.Field(string(key))
		if err != nil {
			return nil, 0, err
		}
		if found {
			if child != nil {
				oldPath := ctx.Option.Path.node
				ctx.Option.Path.node = child
				paths, c, err := d.valueDecoder.DecodePath(ctx, cursor, depth)
				if err != nil {
					return nil, 0, err
				}
				ctx.Option.Path.node = oldPath
				ret = append(ret, paths...)
				cursor = c
			} else {
				start := cursor
				end, err := skipValue(buf, cursor, depth)
				if err != nil {
					return nil, 0, err
				}
				ret = append(ret, buf[start:end])
				cursor = end
			}
		} else {
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
