package decoder

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// CompileToGetDecoder returns the decoder of the type.
// The type is given by the pointer to its type descriptor, which the caller reads from an interface value,
// because reflect.Type is required only when the decoder is not cached yet.
func CompileToGetDecoder(typ unsafe.Pointer) (Decoder, error) {
	typeptr := uintptr(typ)
	if dec := cachedDecoder.Load(typeptr); dec != nil {
		return *dec, nil
	}
	dec, err := compileHead(runtime.TypeOfPtr(typ), newCompiler(false))
	if err != nil {
		return nil, err
	}
	return *cachedDecoder.Store(typeptr, &dec), nil
}

var (
	jsonNumberType = reflect.TypeOf(json.Number(""))
	cachedDecoder  runtime.TypeCache[Decoder]
)

// compiler compiles the decoder of a type and the ones of the types of its values.
type compiler struct {
	// decoders has the decoders of the struct types compiled by this compile, and of the array, slice and map types
	// which are being compiled ( see compileKind ).
	decoders map[uintptr]Decoder
	// v2 is set for the decoders of the semantics of encoding/json/v2 ( see compileV2 ).
	v2 bool
	// stringifyNumbers is set, with v2, for the decoders of its option StringifyNumbers, and of the names of the
	// objects decoded into maps; stringTag, with stringifyNumbers, for the decoders of the values of a field of the
	// `string` option ( see compiler.stringTagCompiler ).
	stringifyNumbers bool
	stringTag        bool
	// v1Options are the options of encoding/json which the decoders of v2 are compiled for ( see v1DecoderOptions ).
	v1Options V2Flags
	// mapKeys is set for the decoders of the keys of a map, which the methods of JSON don't decode with
	// CallMethodsWithLegacySemantics ( see compileV2 ): the type of a key doesn't tell it is one.
	mapKeys bool
	// funcs are the functions of unmarshaling which the decoders of v2 are compiled for, or nil.
	funcs *UnmarshalFuncs
}

func newCompiler(v2 bool) *compiler {
	return &compiler{decoders: map[uintptr]Decoder{}, v2: v2}
}

func compileHead(typ reflect.Type, c *compiler) (Decoder, error) {
	// The kind is validated here, once per type, instead of for every call of the decoding functions:
	// the decoder of a non-pointer type is never created, so it is never cached either.
	if typ.Kind() != reflect.Ptr {
		return nil, &errors.InvalidUnmarshalError{Type: typ}
	}
	if c.v2 {
		return compile(typ.Elem(), "", "", c)
	}
	switch {
	case implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return newUnmarshalJSONDecoder(reflect.PointerTo(typ)), nil
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return newUnmarshalTextDecoder(reflect.PointerTo(typ)), nil
	}
	return compile(typ.Elem(), "", "", c)
}

func compile(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	if c.v2 {
		return c.compileV2(typ, structName, fieldName)
	}
	if implementsUnmarshalFrom(reflect.PointerTo(typ)) {
		next, err := compileAfterFrom(typ, structName, fieldName, c)
		if err != nil {
			return nil, err
		}
		return newUnmarshalFromDecoder(reflect.PointerTo(typ), next), nil
	}
	return compileAfterFrom(typ, structName, fieldName, c)
}

// compileAfterFrom returns the decoder of a value of the type by its methods of unmarshaling after
// UnmarshalJSONFrom, or by its kind.
func compileAfterFrom(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	switch {
	case implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return newUnmarshalJSONDecoder(reflect.PointerTo(typ)), nil
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return newUnmarshalTextDecoder(reflect.PointerTo(typ)), nil
	}
	return compileKind(typ, structName, fieldName, c)
}

// compileKind returns the decoder of a value of the type by its kind, without its methods of unmarshaling.
// c.decoders has the decoders of the struct types compiled by this compile, and of the array, slice and map types
// which are being compiled: a value of such a type inside itself, or a struct met again, is decoded by its decoder.
func compileKind(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	if dec, exists := c.decoders[uintptr(runtime.TypePtr(typ))]; exists {
		return dec, nil
	}
	switch typ.Kind() {
	case reflect.Ptr:
		return compilePtr(typ, structName, fieldName, c)
	case reflect.Struct:
		return compileStruct(typ, structName, fieldName, c)
	case reflect.Slice:
		elem := typ.Elem()
		if elem.Kind() == reflect.Uint8 {
			return compileBytes(typ, structName, fieldName)
		}
		return compileSlice(typ, structName, fieldName, c)
	case reflect.Array:
		return compileArray(typ, structName, fieldName, c)
	case reflect.Map:
		return compileMap(typ, structName, fieldName, c)
	case reflect.Interface:
		return compileInterface(typ, structName, fieldName)
	case reflect.Uintptr:
		return compileUint(typ, structName, fieldName)
	case reflect.Int:
		return compileInt(typ, structName, fieldName)
	case reflect.Int8:
		return compileInt8(typ, structName, fieldName)
	case reflect.Int16:
		return compileInt16(typ, structName, fieldName)
	case reflect.Int32:
		return compileInt32(typ, structName, fieldName)
	case reflect.Int64:
		return compileInt64(typ, structName, fieldName)
	case reflect.Uint:
		return compileUint(typ, structName, fieldName)
	case reflect.Uint8:
		return compileUint8(typ, structName, fieldName)
	case reflect.Uint16:
		return compileUint16(typ, structName, fieldName)
	case reflect.Uint32:
		return compileUint32(typ, structName, fieldName)
	case reflect.Uint64:
		return compileUint64(typ, structName, fieldName)
	case reflect.String:
		return compileString(typ, structName, fieldName)
	case reflect.Bool:
		return compileBool(typ, structName, fieldName)
	case reflect.Float32:
		return compileFloat32(typ, structName, fieldName)
	case reflect.Float64:
		return compileFloat64(typ, structName, fieldName)
	case reflect.Func:
		return compileFunc(typ, structName, fieldName)
	}
	return newInvalidDecoder(typ, structName, fieldName), nil
}

func isStringTagSupportedType(typ reflect.Type) bool {
	// The option applies to a field of a string, a number or a bool, as encoding/json documents it. As encoding/json
	// of Go 1.27 does, it follows one pointer, named or not, and does not apply to the value of a pointer to a pointer,
	// nor to a value which has a method of unmarshaling.
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch {
	case implementsUnmarshalFrom(reflect.PointerTo(typ)), implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return false
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return false
	}
	switch typ.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	}
	return false
}

func compileMapKey(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	if reflect.PointerTo(typ).Implements(unmarshalTextType) {
		return &textKeyDecoder{unmarshalTextDecoder{typ: reflect.PointerTo(typ)}}, nil
	}
	if typ.Kind() == reflect.String {
		d := newStringDecoder(structName, fieldName)
		d.typ = typ
		return d, nil
	}
	if typ.Kind() == reflect.Pointer {
		if isPointerCycle(typ) {
			return newInvalidDecoder(typ, structName, fieldName), nil
		}
		// a key of a pointer type is the key the pointer points to, which is allocated
		dec, err := compileMapKey(typ.Elem(), structName, fieldName, c)
		if err != nil {
			return nil, err
		}
		if _, ok := dec.(*invalidDecoder); ok {
			return newInvalidDecoder(typ, structName, fieldName), nil
		}
		return newPtrDecoder(dec, typ.Elem(), structName, fieldName), nil
	}
	switch typ.Kind() {
	case reflect.Array, reflect.Struct, reflect.Slice, reflect.Map:
		// No key is decoded into a value of these kinds: their decoders would end as an invalidDecoder by the switch
		// below. They are not compiled, so that a type which holds the map, as in type M map[*M]int, ends here.
		return newInvalidDecoder(typ, structName, fieldName), nil
	}
	// The other keys are decoded by their kind, whatever other methods they have, as encoding/json decodes them: an
	// integer, a float, a bool, and an empty interface, which a key of a string is stored in.
	dec, err := compileKind(typ, structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	switch dec.(type) {
	case *interfaceDecoder:
		if typ.NumMethod() == 0 {
			return dec, nil
		}
	case *boolDecoder, *intDecoder, *uintDecoder, *floatDecoder, *float32Decoder, *numberDecoder:
		d := newWrappedStringDecoder(typ, dec, structName, fieldName)
		d.isMapKey = true
		return d, nil
	}
	return newInvalidDecoder(typ, structName, fieldName), nil
}

func compilePtr(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	if isPointerCycle(typ) {
		// No value but null can be decoded into it, which sets the pointer to nil.
		return newPtrDecoder(newInvalidDecoder(typ.Elem(), structName, fieldName), typ.Elem(), structName, fieldName), nil
	}
	dec, err := compile(typ.Elem(), structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	return newPtrDecoder(dec, typ.Elem(), structName, fieldName), nil
}

// isPointerCycle reports whether the pointer type points to pointers only, without an end, as type R *R does:
// the pointers are followed two at a time and one at a time, which meet in a cycle.
func isPointerCycle(typ reflect.Type) bool {
	slow, fast := typ, typ
	for fast.Kind() == reflect.Pointer && fast.Elem().Kind() == reflect.Pointer {
		slow, fast = slow.Elem(), fast.Elem().Elem()
		if slow == fast {
			return true
		}
	}
	return false
}

func compileInt(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newIntDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v int64) {
		*(*int)(p) = int(v)
	}), nil
}

func compileInt8(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newIntDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v int64) {
		*(*int8)(p) = int8(v)
	}), nil
}

func compileInt16(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newIntDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v int64) {
		*(*int16)(p) = int16(v)
	}), nil
}

func compileInt32(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newIntDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v int64) {
		*(*int32)(p) = int32(v)
	}), nil
}

func compileInt64(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newIntDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v int64) {
		*(*int64)(p) = v
	}), nil
}

func compileUint(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newUintDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v uint64) {
		*(*uint)(p) = uint(v)
	}), nil
}

func compileUint8(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newUintDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v uint64) {
		*(*uint8)(p) = uint8(v)
	}), nil
}

func compileUint16(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newUintDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v uint64) {
		*(*uint16)(p) = uint16(v)
	}), nil
}

func compileUint32(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newUintDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v uint64) {
		*(*uint32)(p) = uint32(v)
	}), nil
}

func compileUint64(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newUintDecoder(typ, structName, fieldName, func(p unsafe.Pointer, v uint64) {
		*(*uint64)(p) = v
	}), nil
}

func compileFloat32(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := newFloatDecoder(structName, fieldName, func(p unsafe.Pointer, v float64) {
		*(*float32)(p) = float32(v)
	})
	d.typ, d.is32 = typ, true
	return &float32Decoder{floatDecoder: *d}, nil
}

func compileFloat64(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := newFloatDecoder(structName, fieldName, func(p unsafe.Pointer, v float64) {
		*(*float64)(p) = v
	})
	d.typ = typ
	return d, nil
}

func compileString(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	if typ == jsonNumberType {
		return newNumberDecoder(structName, fieldName, func(p unsafe.Pointer, v json.Number) {
			*(*json.Number)(p) = v
		}), nil
	}
	d := newStringDecoder(structName, fieldName)
	d.typ = typ
	return d, nil
}

func compileBool(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := newBoolDecoder(structName, fieldName)
	d.typ = typ
	return d, nil
}

func compileBytes(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := newBytesDecoder(typ.Elem(), structName, fieldName)
	d.sliceType = typ
	return d, nil
}

func compileSlice(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	elem := typ.Elem()
	d := newSliceDecoder(nil, elem, elem.Size(), structName, fieldName)
	d.typ = typ
	var dec Decoder = d
	if c.v2 {
		dec = newV2SliceDecoder(d)
	}
	decoder, err := compileElem(typ, dec, structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	d.valueDecoder = c.goOnDecoder(decoder)
	return dec, nil
}

// compileElem returns the decoder of the elements of typ, an array, a slice or a map type whose decoder is dec, which
// decodes the values of typ inside them, as compileStruct does for the fields of a struct.
func compileElem(typ reflect.Type, dec Decoder, structName, fieldName string, c *compiler) (Decoder, error) {
	typeptr := uintptr(runtime.TypePtr(typ))
	if _, exists := c.decoders[typeptr]; exists {
		// a decoder of v2 which declines values to dec, which the values of typ in the element are decoded by ( see
		// compileNextV2 )
		return compile(typ.Elem(), structName, fieldName, c)
	}
	c.decoders[typeptr] = dec
	elemDec, err := compile(typ.Elem(), structName, fieldName, c)
	delete(c.decoders, typeptr)
	return elemDec, err
}

func compileArray(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	d := newArrayDecoder(nil, typ, structName, fieldName)
	decoder, err := compileElem(typ, d, structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	d.valueDecoder = decoder
	return d, nil
}

func compileMap(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	keyDec, err := compileMapKey(typ.Key(), structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	d := newMapDecoder(typ, typ.Key(), keyDec, typ.Elem(), nil, structName, fieldName)
	valueDec, err := compileElem(typ, d, structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	d.valueDecoder = valueDec
	return d, nil
}

func compileInterface(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newInterfaceDecoder(typ, structName, fieldName), nil
}

func compileFunc(typ reflect.Type, strutName, fieldName string) (Decoder, error) {
	return newFuncDecoder(typ, strutName, fieldName), nil
}

// embeddedStruct is a struct type whose fields are found for the struct being compiled: the struct itself, at no
// indexes, or a struct embedded in it, through the embedded fields at the indexes of index.
type embeddedStruct struct {
	typ   reflect.Type
	index []int
}

// fieldCandidate is a field of the struct being compiled or of a struct embedded in it, at the indexes of index.
type fieldCandidate struct {
	set   *structFieldSet
	index []int
}

func (c fieldCandidate) Depth() int        { return len(c.index) - 1 }
func (c fieldCandidate) IsTaggedKey() bool { return c.set.isTaggedKey }

// compileStruct returns the decoder of the struct type typ. It stays in c.decoders after the compile, so that a
// struct type met again in the same compile, by another field or by the fields of the structs it is embedded in, which
// are compiled as fields of each struct it is embedded in, is not compiled again.
func compileStruct(typ reflect.Type, structName, fieldName string, c *compiler) (Decoder, error) {
	structDec := newStructDecoder(structName, fieldName)
	structDec.typ = typ
	c.decoders[uintptr(runtime.TypePtr(typ))] = structDec
	fields, err := structFields(typ, c)
	if err != nil {
		return nil, err
	}
	structDec.setFields(fields)
	return structDec, nil
}

// structFields returns the fields of the struct type typ in their order, as encoding/json finds them: the fields of
// the structs embedded without a key are promoted, the least nested first, and a struct already met is not met again.
// Of the fields of a key, the one runtime.DominantField chooses is taken.
func structFields(typ reflect.Type, c *compiler) ([]*structFieldSet, error) {
	var candidates []fieldCandidate
	visited := map[reflect.Type]bool{}
	// the structs of a level and of the next one, and how many times each is embedded at its level
	var current []embeddedStruct
	next := []embeddedStruct{{typ: typ}}
	count, nextCount := map[reflect.Type]int{}, map[reflect.Type]int{}
	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, count
		clear(nextCount)
		for _, e := range current {
			if visited[e.typ] {
				continue
			}
			visited[e.typ] = true
			for i := 0; i < e.typ.NumField(); i++ {
				field := e.typ.Field(i)
				if runtime.IsIgnoredStructField(field) {
					continue
				}
				tag := runtime.StructTagFromField(field)
				index := append(slices.Clip(e.index), i)
				if st := promotingStruct(field, tag); st != nil {
					nextCount[st]++
					if nextCount[st] == 1 {
						next = append(next, embeddedStruct{typ: st, index: index})
					}
					continue
				}
				dec, err := compile(field.Type, e.typ.Name(), field.Name, c)
				if err != nil {
					return nil, err
				}
				if tag.IsString && isStringTagSupportedType(field.Type) {
					dec = newWrappedStringDecoder(field.Type, dec, e.typ.Name(), field.Name)
				}
				dec, offset, setErr := promotedField(typ, index, dec)
				set := &structFieldSet{
					dec:         dec,
					offset:      offset,
					isTaggedKey: tag.IsTaggedKey,
					key:         tag.Key,
					keyLen:      int64(len(tag.Key)),
					err:         setErr,
				}
				candidates = append(candidates, fieldCandidate{set: set, index: index})
				if count[e.typ] > 1 {
					// a struct embedded more than once at this level promotes each field more than once: none is taken
					candidates = append(candidates, fieldCandidate{set: set, index: index})
				}
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b fieldCandidate) int { return strings.Compare(a.set.key, b.set.key) })
	taken := candidates[:0]
	for i := 0; i < len(candidates); {
		j := i + 1
		for j < len(candidates) && candidates[j].set.key == candidates[i].set.key {
			j++
		}
		if dominant := runtime.DominantField(candidates[i:j]); dominant >= 0 {
			taken = append(taken, candidates[i+dominant])
		}
		i = j
	}
	slices.SortFunc(taken, func(a, b fieldCandidate) int { return slices.Compare(a.index, b.index) })
	fields := make([]*structFieldSet, 0, len(taken))
	for _, c := range taken {
		fields = append(fields, c.set)
	}
	return fields, nil
}

// promotingStruct returns the struct type of an embedded field without a key, or a pointer to it, whose fields are
// promoted, or nil. Its methods of unmarshaling don't matter, as in encoding/json: a struct to which they are promoted
// is decoded by them instead of by its fields.
func promotingStruct(field reflect.StructField, tag *runtime.StructTag) reflect.Type {
	if !field.Anonymous || tag.IsTaggedKey {
		return nil
	}
	typ := field.Type
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	return typ
}

// promotedField returns the decoder and the offset in the struct type typ of the field at index, a field of a struct
// embedded in typ when index has more than one, whose decoder in the struct which has it is dec, and the error of the
// field when the embedded pointer at that offset can't be allocated ( see structFieldSet ). An embedded pointer is
// allocated by the decoder when it is nil. Only the error of the pointer at the offset is returned: the one of an
// unexported pointer embedded in the struct it points to is not.
func promotedField(typ reflect.Type, index []int, dec Decoder) (Decoder, uintptr, error) {
	field := typ.Field(index[0])
	if len(index) == 1 {
		return dec, field.Offset, nil
	}
	if field.Type.Kind() != reflect.Pointer {
		dec, offset, err := promotedField(field.Type, index[1:], dec)
		return dec, field.Offset + offset, err
	}
	embedded := field.Type.Elem()
	dec, offset, _ := promotedField(embedded, index[1:], dec)
	dec = newAnonymousFieldDecoder(embedded, offset, dec)
	if !field.IsExported() {
		return dec, field.Offset, errUnsettableEmbeddedPointer
	}
	return dec, field.Offset, nil
}

func implementsUnmarshalJSONType(typ reflect.Type) bool {
	return typ.Implements(unmarshalJSONType) || typ.Implements(unmarshalJSONContextType)
}
