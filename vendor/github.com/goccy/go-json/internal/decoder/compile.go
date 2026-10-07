package decoder

import (
	"encoding/json"
	"fmt"
	"reflect"
	"unicode"
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
	dec, err := compileHead(runtime.TypeOfPtr(typ), map[uintptr]Decoder{})
	if err != nil {
		return nil, err
	}
	return *cachedDecoder.Store(typeptr, &dec), nil
}

var (
	jsonNumberType = reflect.TypeOf(json.Number(""))
	cachedDecoder  runtime.TypeCache[Decoder]
)

func compileHead(typ reflect.Type, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	// The kind is validated here, once per type, instead of for every call of the decoding functions:
	// the decoder of a non-pointer type is never created, so it is never cached either.
	if typ.Kind() != reflect.Ptr {
		return nil, &errors.InvalidUnmarshalError{Type: typ}
	}
	switch {
	case implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return newUnmarshalJSONDecoder(reflect.PointerTo(typ), "", ""), nil
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return newUnmarshalTextDecoder(reflect.PointerTo(typ), "", ""), nil
	}
	return compile(typ.Elem(), "", "", structTypeToDecoder)
}

func compile(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	switch {
	case implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return newUnmarshalJSONDecoder(reflect.PointerTo(typ), structName, fieldName), nil
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return newUnmarshalTextDecoder(reflect.PointerTo(typ), structName, fieldName), nil
	}

	switch typ.Kind() {
	case reflect.Ptr:
		return compilePtr(typ, structName, fieldName, structTypeToDecoder)
	case reflect.Struct:
		return compileStruct(typ, structName, fieldName, structTypeToDecoder)
	case reflect.Slice:
		elem := typ.Elem()
		if elem.Kind() == reflect.Uint8 {
			return compileBytes(typ, structName, fieldName)
		}
		return compileSlice(typ, structName, fieldName, structTypeToDecoder)
	case reflect.Array:
		return compileArray(typ, structName, fieldName, structTypeToDecoder)
	case reflect.Map:
		return compileMap(typ, structName, fieldName, structTypeToDecoder)
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
	switch {
	case implementsUnmarshalJSONType(reflect.PointerTo(typ)):
		return false
	case reflect.PointerTo(typ).Implements(unmarshalTextType):
		return false
	}
	switch typ.Kind() {
	case reflect.Map:
		return false
	case reflect.Slice:
		return false
	case reflect.Array:
		return false
	case reflect.Struct:
		return false
	case reflect.Interface:
		return false
	}
	return true
}

func compileMapKey(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	if reflect.PointerTo(typ).Implements(unmarshalTextType) {
		return newUnmarshalTextDecoder(reflect.PointerTo(typ), structName, fieldName), nil
	}
	if typ.Kind() == reflect.String {
		d := newStringDecoder(structName, fieldName)
		d.typ = typ
		return d, nil
	}
	dec, err := compile(typ, structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	for {
		switch t := dec.(type) {
		case *stringDecoder, *interfaceDecoder:
			return dec, nil
		case *boolDecoder, *intDecoder, *uintDecoder, *floatDecoder, *float32Decoder, *numberDecoder:
			d := newWrappedStringDecoder(typ, dec, structName, fieldName)
			d.isMapKey = true
			return d, nil
		case *ptrDecoder:
			dec = t.dec
		case *basicPtrDecoder:
			dec = t.dec
		default:
			return newInvalidDecoder(typ, structName, fieldName), nil
		}
	}
}

func compilePtr(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	dec, err := compile(typ.Elem(), structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	return newPtrDecoder(dec, typ.Elem(), structName, fieldName), nil
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

func compileSlice(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	elem := typ.Elem()
	decoder, err := compile(elem, structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	d := newSliceDecoder(decoder, elem, elem.Size(), structName, fieldName)
	d.typ = typ
	return d, nil
}

func compileArray(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	elem := typ.Elem()
	decoder, err := compile(elem, structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	return newArrayDecoder(decoder, typ, structName, fieldName), nil
}

func compileMap(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	keyDec, err := compileMapKey(typ.Key(), structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	valueDec, err := compile(typ.Elem(), structName, fieldName, structTypeToDecoder)
	if err != nil {
		return nil, err
	}
	return newMapDecoder(typ, typ.Key(), keyDec, typ.Elem(), valueDec, structName, fieldName), nil
}

func compileInterface(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	return newInterfaceDecoder(typ, structName, fieldName), nil
}

func compileFunc(typ reflect.Type, strutName, fieldName string) (Decoder, error) {
	return newFuncDecoder(typ, strutName, fieldName), nil
}

// typeToStructTags returns the tags of the fields of typ which have a key, which hides a field of the same key
// promoted from an embedded struct. An embedded struct without a key in its tag has no key of its own: its fields
// are promoted, and one of them may have the name of the embedded struct.
func typeToStructTags(typ reflect.Type) runtime.StructTags {
	tags := runtime.StructTags{}
	fieldNum := typ.NumField()
	for i := 0; i < fieldNum; i++ {
		field := typ.Field(i)
		if runtime.IsIgnoredStructField(field) {
			continue
		}
		tag := runtime.StructTagFromField(field)
		if field.Anonymous && !tag.IsTaggedKey && isStructOrPointerToStruct(field.Type) {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func isStructOrPointerToStruct(typ reflect.Type) bool {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.Struct
}

func compileStruct(typ reflect.Type, structName, fieldName string, structTypeToDecoder map[uintptr]Decoder) (Decoder, error) {
	fieldNum := typ.NumField()
	typeptr := uintptr(runtime.TypePtr(typ))
	if dec, exists := structTypeToDecoder[typeptr]; exists {
		return dec, nil
	}
	structDec := newStructDecoder(structName, fieldName)
	structDec.typ, structDec.typeName = typ, typ.Name()
	structTypeToDecoder[typeptr] = structDec
	structName = typ.Name()
	tags := typeToStructTags(typ)
	allFields := []*structFieldSet{}
	for i := 0; i < fieldNum; i++ {
		field := typ.Field(i)
		if runtime.IsIgnoredStructField(field) {
			continue
		}
		isUnexportedField := unicode.IsLower([]rune(field.Name)[0])
		tag := runtime.StructTagFromField(field)
		dec, err := compile(field.Type, structName, field.Name, structTypeToDecoder)
		if err != nil {
			return nil, err
		}
		if field.Anonymous && !tag.IsTaggedKey {
			if stDec, ok := dec.(*structDecoder); ok {
				if field.Type == typ {
					// recursive definition
					continue
				}
				for _, v := range stDec.fields {
					if tags.ExistsKey(v.key) {
						continue
					}
					fieldSet := &structFieldSet{
						dec:         v.dec,
						offset:      field.Offset + v.offset,
						isTaggedKey: v.isTaggedKey,
						key:         v.key,
						keyLen:      int64(len(v.key)),
					}
					structDec.setEmbedded(fieldSet, append([]string{field.Name}, stDec.embedded[v]...))
					allFields = append(allFields, fieldSet)
				}
			} else if pdec, ok := dec.(*ptrDecoder); ok {
				contentDec := pdec.contentDecoder()
				if pdec.typ == typ {
					// recursive definition
					continue
				}
				var fieldSetErr error
				if isUnexportedField {
					fieldSetErr = fmt.Errorf(
						"json: cannot set embedded pointer to unexported struct: %v",
						field.Type.Elem(),
					)
				}
				if dec, ok := contentDec.(*structDecoder); ok {
					for _, v := range dec.fields {
						if tags.ExistsKey(v.key) {
							continue
						}
						fieldSet := &structFieldSet{
							dec:         newAnonymousFieldDecoder(pdec.typ, v.offset, v.dec),
							offset:      field.Offset,
							isTaggedKey: v.isTaggedKey,
							key:         v.key,
							keyLen:      int64(len(v.key)),
							err:         fieldSetErr,
						}
						structDec.setEmbedded(fieldSet, append([]string{field.Name}, dec.embedded[v]...))
						allFields = append(allFields, fieldSet)
					}
				} else {
					fieldSet := &structFieldSet{
						dec:         pdec,
						offset:      field.Offset,
						isTaggedKey: tag.IsTaggedKey,
						key:         field.Name,
						keyLen:      int64(len(field.Name)),
					}
					allFields = append(allFields, fieldSet)
				}
			} else {
				fieldSet := &structFieldSet{
					dec:         dec,
					offset:      field.Offset,
					isTaggedKey: tag.IsTaggedKey,
					key:         field.Name,
					keyLen:      int64(len(field.Name)),
				}
				allFields = append(allFields, fieldSet)
			}
		} else {
			if tag.IsString && isStringTagSupportedType(field.Type) {
				dec = newWrappedStringDecoder(field.Type, dec, structName, field.Name)
			}
			var key string
			if tag.Key != "" {
				key = tag.Key
			} else {
				key = field.Name
			}
			fieldSet := &structFieldSet{
				dec:         dec,
				offset:      field.Offset,
				isTaggedKey: tag.IsTaggedKey,
				key:         key,
				keyLen:      int64(len(key)),
			}
			allFields = append(allFields, fieldSet)
		}
	}
	// A key which remains more than once is the one of the last field, as it was when the fields were kept
	// in a map, at the place of the first.
	fields := []*structFieldSet{}
	indexByKey := map[string]int{}
	for _, set := range filterDuplicatedFields(allFields) {
		if i, exists := indexByKey[set.key]; exists {
			fields[i] = set
			continue
		}
		indexByKey[set.key] = len(fields)
		fields = append(fields, set)
	}
	structDec.setFields(fields)
	delete(structTypeToDecoder, typeptr)
	return structDec, nil
}

func filterDuplicatedFields(allFields []*structFieldSet) []*structFieldSet {
	fieldMap := map[string][]*structFieldSet{}
	for _, field := range allFields {
		fieldMap[field.key] = append(fieldMap[field.key], field)
	}
	duplicatedFieldMap := map[string]struct{}{}
	for k, sets := range fieldMap {
		sets = filterFieldSets(sets)
		if len(sets) != 1 {
			duplicatedFieldMap[k] = struct{}{}
		}
	}

	filtered := make([]*structFieldSet, 0, len(allFields))
	for _, field := range allFields {
		if _, exists := duplicatedFieldMap[field.key]; exists {
			continue
		}
		filtered = append(filtered, field)
	}
	return filtered
}

func filterFieldSets(sets []*structFieldSet) []*structFieldSet {
	if len(sets) == 1 {
		return sets
	}
	filtered := make([]*structFieldSet, 0, len(sets))
	for _, set := range sets {
		if set.isTaggedKey {
			filtered = append(filtered, set)
		}
	}
	return filtered
}

func implementsUnmarshalJSONType(typ reflect.Type) bool {
	return typ.Implements(unmarshalJSONType) || typ.Implements(unmarshalJSONContextType)
}
