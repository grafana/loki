package decoder

import (
	"encoding/json"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type interfaceDecoder struct {
	typ reflect.Type
	// hasMethods is set for an interface type which has methods, whose value is not replaced by the decoded value
	// ( see decodeWithMethods ): the value of an interface{} is read without reflect ( see Decode ).
	hasMethods bool
	// values are how the values of the types which the interface value holds are decoded, by the type of an
	// interface{}, or by the itab of an interface type with methods ( see valueDecoderOf ).
	values        runtime.TypeCache[ifaceValueDecoder]
	structName    string
	fieldName     string
	sliceDecoder  *sliceDecoder
	mapDecoder    *mapDecoder
	floatDecoder  *floatDecoder
	numberDecoder *numberDecoder
	stringDecoder *stringDecoder
}

func newEmptyInterfaceDecoder(structName, fieldName string) *interfaceDecoder {
	ifaceDecoder := &interfaceDecoder{
		typ:        emptyInterfaceType,
		structName: structName,
		fieldName:  fieldName,
		floatDecoder: newFloatDecoder(structName, fieldName, func(p unsafe.Pointer, v float64) {
			*(*any)(p) = v
		}),
		numberDecoder: newNumberDecoder(structName, fieldName, func(p unsafe.Pointer, v json.Number) {
			*(*any)(p) = v
		}),
		stringDecoder: newStringDecoder(structName, fieldName),
	}
	ifaceDecoder.sliceDecoder = newSliceDecoder(
		ifaceDecoder,
		emptyInterfaceType,
		emptyInterfaceType.Size(),
		structName, fieldName,
	)
	ifaceDecoder.mapDecoder = newMapDecoder(
		interfaceMapType,
		stringType,
		ifaceDecoder.stringDecoder,
		interfaceMapType.Elem(),
		ifaceDecoder,
		structName,
		fieldName,
	)
	return ifaceDecoder
}

func newInterfaceDecoder(typ reflect.Type, structName, fieldName string) *interfaceDecoder {
	emptyIfaceDecoder := newEmptyInterfaceDecoder(structName, fieldName)
	stringDecoder := newStringDecoder(structName, fieldName)
	return &interfaceDecoder{
		typ:        typ,
		hasMethods: typ.NumMethod() > 0,
		structName: structName,
		fieldName:  fieldName,
		sliceDecoder: newSliceDecoder(
			emptyIfaceDecoder,
			emptyInterfaceType,
			emptyInterfaceType.Size(),
			structName, fieldName,
		),
		mapDecoder: newMapDecoder(
			interfaceMapType,
			stringType,
			stringDecoder,
			interfaceMapType.Elem(),
			emptyIfaceDecoder,
			structName,
			fieldName,
		),
		floatDecoder: newFloatDecoder(structName, fieldName, func(p unsafe.Pointer, v float64) {
			*(*any)(p) = v
		}),
		numberDecoder: newNumberDecoder(structName, fieldName, func(p unsafe.Pointer, v json.Number) {
			*(*any)(p) = v
		}),
		stringDecoder: stringDecoder,
	}
}

var (
	emptyInterfaceType = reflect.TypeOf((*any)(nil)).Elem()
	EmptyInterfaceType = emptyInterfaceType
	interfaceMapType   = reflect.TypeOf((*map[string]any)(nil)).Elem()
	stringType         = reflect.TypeOf("")
)

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

func (d *interfaceDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	if d.hasMethods {
		return d.decodeWithMethods(ctx, cursor, depth, p)
	}
	// p is an interface{}: a nil one, the most common, is decoded as it is, and the value of another one may be
	// decoded into what it points to
	h := (*emptyInterface)(p)
	if h.ptr == nil {
		return d.decodeEmptyInterface(ctx, cursor, depth, p)
	}
	v := d.values.Load(uintptr(h.typ))
	if v == nil {
		var err error
		if v, err = d.valueDecoderOf(ctx, h.typ, *(*any)(p)); err != nil {
			return 0, err
		}
	}
	return d.decodeValue(ctx, cursor, depth, p, v, h.ptr)
}

// ifaceValueKind is how the value which an interface value holds is decoded, as encoding/json does.
type ifaceValueKind uint8

const (
	// ifaceValueReplaced is a value of an interface{} which is replaced by the value decoded as the one of an
	// interface{}.
	ifaceValueReplaced ifaceValueKind = iota
	// ifaceValuePointee is a pointer to a value which is not an interface value: the value is decoded into what it
	// points to, as a value of the pointer type, whose unmarshalers are used.
	ifaceValuePointee
	// ifaceValueKept is a value of an interface type with methods which is not a pointer: it is kept, and can be
	// decoded from null only, which sets the interface value to nil.
	ifaceValueKept
)

// ifaceValueDecoder is how a value of a type which an interface value holds is decoded, which depends on the type
// only: it is found once for a type.
type ifaceValueDecoder struct {
	kind ifaceValueKind
	// dec is the decoder of a pointer of ifaceValuePointee.
	dec Decoder
	// textType is the pointer type of ifaceValuePointee which implements encoding.TextUnmarshaler: a value which
	// is not a string is a type error of the interface value ( see ifaceTextUnmarshalerKindError ).
	textType reflect.Type
}

// valueDecoderOf finds how the value iface, of the type typ, which the interface value holds is decoded, and
// keeps it for the type by key: the type of an interface{}, or the itab of an interface type with methods.
func (d *interfaceDecoder) valueDecoderOf(ctx *RuntimeContext, key unsafe.Pointer, iface any) (*ifaceValueDecoder, error) {
	v := &ifaceValueDecoder{kind: ifaceValueReplaced}
	if d.hasMethods {
		v.kind = ifaceValueKept
	}
	if typ := reflect.TypeOf(iface); typ.Kind() == reflect.Ptr && typ.Elem() != d.typ {
		dec, err := ctx.DecoderOf((*emptyInterface)(unsafe.Pointer(&iface)).typ)
		if err != nil {
			return nil, err
		}
		v.kind, v.dec = ifaceValuePointee, dec
		if text, ok := dec.(*unmarshalTextDecoder); ok {
			v.textType = text.typ
		}
	}
	return d.values.Store(uintptr(key), v), nil
}

// decodeValue decodes the value at cursor into the interface value at p, which holds a value decoded as v says,
// whose data word is ptr.
func (d *interfaceDecoder) decodeValue(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer, v *ifaceValueDecoder, ptr unsafe.Pointer) (int64, error) {
	switch v.kind {
	case ifaceValueReplaced:
		return d.decodeEmptyInterface(ctx, cursor, depth, p)
	case ifaceValuePointee:
		buf := ctx.Buf
		cursor = skipWhiteSpace(buf, cursor)
		if buf[cursor] == 'n' {
			// null sets the interface value to nil, not what it points to
			if err := validateNull(buf, cursor); err != nil {
				return 0, err
			}
			*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
			return cursor + 4, nil
		}
		if v.textType != nil && isOtherValue(buf[cursor], stringValue) {
			return ctx.ifaceTextUnmarshalerKindError(cursor, depth, d.typ, v.textType)
		}
		return v.dec.Decode(ctx, cursor, depth, ptr)
	}
	return d.decodeKept(ctx, cursor, depth, p)
}

// decodeKept decodes the value at cursor into the interface value at p of an interface type with methods, whose
// value is not decoded into: null sets it to nil, and any other value is a type error, which keeps it, as
// encoding/json does.
func (d *interfaceDecoder) decodeKept(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
		return cursor + 4, nil
	}
	return ctx.keptInterfaceTypeError(cursor, depth, d.typ, p)
}

// nonEmptyInterface is the layout of a value of an interface type with methods: the itab of the type it holds,
// which is nil for a nil value, and the data word.
type nonEmptyInterface struct {
	itab unsafe.Pointer
	ptr  unsafe.Pointer
}

// decodeWithMethods decodes the value of an interface type which has methods: a value which is a pointer is
// decoded into what it points to, as encoding/json does. How a value is decoded is kept by its itab, which is of
// one type for the interface type of the decoder.
func (d *interfaceDecoder) decodeWithMethods(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	h := (*nonEmptyInterface)(p)
	if h.itab == nil || h.ptr == nil {
		// a nil value, or a nil pointer
		return d.decodeKept(ctx, cursor, depth, p)
	}
	v := d.values.Load(uintptr(h.itab))
	if v == nil {
		var err error
		if v, err = d.valueDecoderOf(ctx, h.itab, reflect.NewAt(d.typ, p).Elem().Interface()); err != nil {
			return 0, err
		}
	}
	return d.decodeValue(ctx, cursor, depth, p, v, h.ptr)
}

// decodeFloatSlow decodes the number at cursor into the interface{} at p, which the fast parse doesn't: a number
// which is not valid by the grammar is a syntax error, and a number out of the range of a float64 is a type error,
// after which the decoding goes on ( see floatRangeErrorOfInterface ). It is not inlined, so that the decoding of
// the values of interface{} keeps the size it had.
//
//go:noinline
func decodeFloatSlow(ctx *RuntimeContext, cursor int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	end, err := numberEnd(buf, cursor)
	if err != nil {
		return 0, err
	}
	b := buf[cursor:end]
	f, parseErr := strconv.ParseFloat(*(*string)(unsafe.Pointer(&b)), 64)
	// a number of the grammar which ParseFloat fails is out of the range of float64
	if inRange := parseErr == nil; !inRange {
		ctx.floatRangeErrorOfInterface(cursor, end, f, p)
		return end, nil
	}
	**(**any)(unsafe.Pointer(&p)) = ctx.boxFloat(f)
	return end, nil
}

func (d *interfaceDecoder) decodeEmptyInterface(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch buf[cursor] {
	case '{':
		depth++
		if depth > maxDecodeNestingDepth {
			return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
		}
		m, cursor, err := decodeNewStringAnyMap(ctx, d, cursor, depth)
		if err != nil {
			return 0, err
		}
		**(**any)(unsafe.Pointer(&p)) = m
		return cursor, nil
	case '[':
		return d.decodeAnySlice(ctx, cursor, depth, p)
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if (ctx.Option.Flags & UseNumberOption) != 0 {
			return d.numberDecoder.Decode(ctx, cursor, depth, p)
		}
		if f, next, ok := parseFloatFast(buf, cursor); ok && validEndNumberChar[buf[next]] {
			**(**any)(unsafe.Pointer(&p)) = ctx.boxFloat(f)
			return next, nil
		}
		return decodeFloatSlow(ctx, cursor, p)
	case '"':
		if end := plainStringEnd(buf, cursor+1); end >= 0 {
			**(**any)(unsafe.Pointer(&p)) = ctx.boxString(ctx.makeString(buf[cursor+1 : end]))
			return end + 1, nil
		}
		s, c, _, err := d.stringDecoder.decodeString(ctx, cursor)
		if err != nil {
			return 0, err
		}
		**(**any)(unsafe.Pointer(&p)) = ctx.boxString(s)
		return c, nil
	case 't':
		if err := validateTrue(buf, cursor); err != nil {
			return 0, err
		}
		cursor += 4
		**(**any)(unsafe.Pointer(&p)) = true
		return cursor, nil
	case 'f':
		if err := validateFalse(buf, cursor); err != nil {
			return 0, err
		}
		cursor += 5
		**(**any)(unsafe.Pointer(&p)) = false
		return cursor, nil
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		cursor += 4
		**(**any)(unsafe.Pointer(&p)) = nil
		return cursor, nil
	}
	return cursor, errors.ErrInvalidBeginningOfValue(buf[cursor], cursor)
}

// decodeStringAnyMap decodes the object at cursor into m, a map[string]interface{}, by the assignment of Go:
// the values are decoded as interface{} by d. depth counts the object already.
func decodeStringAnyMap(ctx *RuntimeContext, d *interfaceDecoder, m map[string]any, cursor, depth int64) (int64, error) {
	buf := ctx.Buf
	cursor++ // '{'
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == '}' {
		return cursor + 1, nil
	}
	for {
		key, c, ok, err := d.stringDecoder.decodeString(ctx, cursor)
		if err != nil {
			return 0, err
		}
		if !ok {
			// null is not a key
			return 0, errors.ErrSyntax("invalid character 'n' looking for beginning of object key string", skipWhiteSpace(buf, cursor)+1)
		}
		cursor = skipWhiteSpace(buf, c)
		if buf[cursor] != ':' {
			return 0, errors.ErrExpected("colon after object key", cursor)
		}
		cursor++
		c, err = d.decodeEmptyInterface(ctx, cursor, depth, unsafe.Pointer(&ctx.slot))
		if err != nil {
			ctx.slot = nil
			return 0, err
		}
		m[key] = ctx.slot
		ctx.slot = nil
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case '}':
			return cursor + 1, nil
		case ',':
			cursor++
		default:
			return 0, errors.ErrExpected("comma after object value", cursor)
		}
	}
}

// decodeNewStringAnyMap decodes the object at cursor into a new map[string]interface{}. The entries are pushed
// to the stack of entries of the context, each key with its value, and put into a map of their number at the end
// of the object: a map filled entry by entry grows and moves its entries several times on the way, which took
// more time than the entries themselves. A key which is repeated is put later, so that the last one wins as in a
// map filled in order.
func decodeNewStringAnyMap(ctx *RuntimeContext, d *interfaceDecoder, cursor, depth int64) (map[string]any, int64, error) {
	buf := ctx.Buf
	cursor++ // '{'
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == '}' {
		return map[string]any{}, cursor + 1, nil
	}
	base := len(ctx.entryStack)
	fail := func(err error) (map[string]any, int64, error) {
		ctx.slot = nil
		ctx.popEntries(base)
		return nil, 0, err
	}
	for {
		if cursor = skipWhiteSpace(buf, cursor); buf[cursor] != '"' {
			return fail(syntaxErrorAt(buf, cursor, whereKey))
		}
		var key string
		var c int64
		if end := plainStringEnd(buf, cursor+1); end >= 0 {
			key, c = ctx.makeString(buf[cursor+1:end]), end+1
		} else {
			var err error
			if key, c, _, err = d.stringDecoder.decodeString(ctx, cursor); err != nil {
				return fail(err)
			}
		}
		cursor = skipWhiteSpace(buf, c)
		if buf[cursor] != ':' {
			return fail(syntaxErrorAt(buf, cursor, whereAfterKey))
		}
		cursor++
		c, err := d.decodeEmptyInterface(ctx, cursor, depth, unsafe.Pointer(&ctx.slot))
		if err != nil {
			return fail(err)
		}
		// the key and the value are pushed together, after the value, whose own entries are popped already.
		ctx.entryStack = append(ctx.entryStack, anyEntry{key: key, value: ctx.slot})
		ctx.slot = nil
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case '}':
			entries := ctx.entryStack[base:]
			m := make(map[string]any, len(entries))
			for i := range entries {
				m[entries[i].key] = entries[i].value
			}
			ctx.popEntries(base)
			return m, cursor + 1, nil
		case ',':
			cursor++
		default:
			return fail(errors.ErrExpected("comma after object value", cursor))
		}
	}
}

// emptyAnySlice is the value of an empty array as interface{}: an empty []interface{}, which all of them share,
// so that it is not allocated for each. It has no room for an element, so that nothing is written through it.
var emptyAnySlice any = []any{}

// decodeAnySlice decodes the array at cursor as a []interface{}: the elements are pushed to the stack
// of the context, and copied into a slice of their number at the end of the array.
func (d *interfaceDecoder) decodeAnySlice(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}
	cursor++ // '['
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == ']' {
		**(**any)(unsafe.Pointer(&p)) = emptyAnySlice
		return cursor + 1, nil
	}
	base := len(ctx.anyStack)
	for {
		c, err := d.decodeEmptyInterface(ctx, cursor, depth, unsafe.Pointer(&ctx.slot))
		if err != nil {
			ctx.slot = nil
			ctx.popAny(base)
			return 0, err
		}
		ctx.anyStack = append(ctx.anyStack, ctx.slot)
		ctx.slot = nil
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case ']':
			elems := make([]any, len(ctx.anyStack)-base)
			copy(elems, ctx.anyStack[base:])
			ctx.popAny(base)
			**(**any)(unsafe.Pointer(&p)) = elems
			return cursor + 1, nil
		case ',':
			cursor++
		default:
			ctx.popAny(base)
			return 0, errors.ErrInvalidCharacter(buf[cursor], "slice", cursor)
		}
	}
}

func NewPathDecoder() Decoder {
	ifaceDecoder := &interfaceDecoder{
		typ:        emptyInterfaceType,
		structName: "",
		fieldName:  "",
		floatDecoder: newFloatDecoder("", "", func(p unsafe.Pointer, v float64) {
			*(*any)(p) = v
		}),
		numberDecoder: newNumberDecoder("", "", func(p unsafe.Pointer, v json.Number) {
			*(*any)(p) = v
		}),
		stringDecoder: newStringDecoder("", ""),
	}
	ifaceDecoder.sliceDecoder = newSliceDecoder(
		ifaceDecoder,
		emptyInterfaceType,
		emptyInterfaceType.Size(),
		"", "",
	)
	ifaceDecoder.mapDecoder = newMapDecoder(
		interfaceMapType,
		stringType,
		ifaceDecoder.stringDecoder,
		interfaceMapType.Elem(),
		ifaceDecoder,
		"", "",
	)
	return ifaceDecoder
}

var (
	truebytes  = []byte("true")
	falsebytes = []byte("false")
)

func (d *interfaceDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch buf[cursor] {
	case '{':
		return d.mapDecoder.DecodePath(ctx, cursor, depth)
	case '[':
		return d.sliceDecoder.DecodePath(ctx, cursor, depth)
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.floatDecoder.DecodePath(ctx, cursor, depth)
	case '"':
		return d.stringDecoder.DecodePath(ctx, cursor, depth)
	case 't':
		if err := validateTrue(buf, cursor); err != nil {
			return nil, 0, err
		}
		cursor += 4
		return [][]byte{truebytes}, cursor, nil
	case 'f':
		if err := validateFalse(buf, cursor); err != nil {
			return nil, 0, err
		}
		cursor += 5
		return [][]byte{falsebytes}, cursor, nil
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return nil, 0, err
		}
		cursor += 4
		return [][]byte{nullbytes}, cursor, nil
	}
	return nil, cursor, errors.ErrInvalidBeginningOfValue(buf[cursor], cursor)
}
