// Copyright (c) Faye Amacker. All rights reserved.
// Licensed under the MIT License. See LICENSE in the project root for license information.

package cbor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type encodeFuncs struct {
	ef  encodeFunc
	ief isEmptyFunc
	izf isZeroFunc
}

var (
	decodingStructTypeCache sync.Map // map[reflect.Type]*decodingStructType
	encodingStructTypeCache sync.Map // map[reflect.Type]*encodingStructType
	encodeFuncCache         sync.Map // map[reflect.Type]encodeFuncs
	typeInfoCache           sync.Map // map[reflect.Type]*typeInfo
)

type specialType int

const (
	specialTypeNone specialType = iota
	specialTypeUnmarshalerIface
	specialTypeUnexportedUnmarshalerIface
	specialTypeEmptyIface
	specialTypeIface
	specialTypeTag
	specialTypeTime
	specialTypeJSONUnmarshalerIface
)

type typeInfo struct {
	elemTypeInfo               *typeInfo
	keyTypeInfo                *typeInfo
	typ                        reflect.Type
	kind                       reflect.Kind
	nonPtrType                 reflect.Type
	nonPtrKind                 reflect.Kind
	spclType                   specialType
	implBinaryUnmarshaler      bool
	implTextUnmarshaler        bool
	elemIsUint8                bool
	nonPtrTypeIsString         bool
	typeIsString               bool
	keyNeedsHashableValueCheck bool
}

func newTypeInfo(t reflect.Type, newTypeInfos map[reflect.Type]*typeInfo) *typeInfo {
	tInfo := typeInfo{
		typ:          t,
		kind:         t.Kind(),
		typeIsString: t == typeString,
	}

	newTypeInfos[t] = &tInfo

	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	k := t.Kind()

	tInfo.nonPtrType = t
	tInfo.nonPtrKind = k

	if k == reflect.Interface {
		if t.NumMethod() == 0 {
			tInfo.spclType = specialTypeEmptyIface
		} else {
			tInfo.spclType = specialTypeIface
		}
	} else if t == typeTag {
		tInfo.spclType = specialTypeTag
	} else if t == typeTime {
		tInfo.spclType = specialTypeTime
	} else if reflect.PointerTo(t).Implements(typeUnexportedUnmarshaler) {
		tInfo.spclType = specialTypeUnexportedUnmarshalerIface
	} else if reflect.PointerTo(t).Implements(typeUnmarshaler) {
		tInfo.spclType = specialTypeUnmarshalerIface
	} else if reflect.PointerTo(t).Implements(typeJSONUnmarshaler) {
		tInfo.spclType = specialTypeJSONUnmarshalerIface
	}

	tInfo.implBinaryUnmarshaler = reflect.PointerTo(t).Implements(typeBinaryUnmarshaler)
	tInfo.implTextUnmarshaler = reflect.PointerTo(t).Implements(typeTextUnmarshaler)
	tInfo.nonPtrTypeIsString = t == typeString

	switch k {
	case reflect.Array, reflect.Slice:
		tInfo.elemTypeInfo = getTypeInfoWithNewTypeInfos(t.Elem(), newTypeInfos)
		tInfo.elemIsUint8 = tInfo.elemTypeInfo.kind == reflect.Uint8
	case reflect.Map:
		tInfo.keyTypeInfo = getTypeInfoWithNewTypeInfos(t.Key(), newTypeInfos)
		tInfo.keyNeedsHashableValueCheck = needsHashableValueCheck(t.Key())
		tInfo.elemTypeInfo = getTypeInfoWithNewTypeInfos(t.Elem(), newTypeInfos)
		tInfo.elemIsUint8 = tInfo.elemTypeInfo.kind == reflect.Uint8
	}

	return &tInfo
}

// needsHashableValueCheck returns true if the hashability of a value of
// the given type can't be determined by the static type.
func needsHashableValueCheck(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Interface:
		return true
	case reflect.Array:
		return needsHashableValueCheck(typ.Elem())
	case reflect.Struct:
		for i := range typ.NumField() {
			if needsHashableValueCheck(typ.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

type decodingStructType struct {
	fields               decodingFields
	fieldIndicesByName   map[string]int // Only populated if toArray is false
	fieldIndicesByIntKey map[int64]int  // Only populated if toArray is false
	err                  error
	toArray              bool
}

func getDecodingStructType(t reflect.Type) (*decodingStructType, error) {
	if v, _ := decodingStructTypeCache.Load(t); v != nil {
		structType := v.(*decodingStructType)
		if structType.err != nil {
			return nil, structType.err
		}
		return structType, nil
	}

	flds, structOptions := collectFields(t)

	hasToArray, hasToIndefArray, err := parseArrayStructOptions(t, structOptions)
	if err != nil {
		structType := &decodingStructType{err: err}
		decodingStructTypeCache.Store(t, structType)
		return nil, err
	}
	// Both options describe a struct laid out as a CBOR array. The decoder
	// accepts definite- and indefinite-length arrays interchangeably, so
	// the same code path serves both options.
	if hasToArray || hasToIndefArray {
		return getDecodingStructToArrayType(t, flds)
	}

	fieldIndicesByName := make(map[string]int, len(flds))
	var fieldIndicesByIntKey map[int64]int

	decFlds := make(decodingFields, len(flds))
	for i, f := range flds {
		// nameAsInt is set in collectFields() except for fields with an unparsable tagged name.
		// Atoi() is called here to catch and save parsing errors.
		if f.keyAsInt && f.nameAsInt == 0 {
			if _, numErr := strconv.Atoi(f.name); numErr != nil {
				structType := &decodingStructType{
					err: errors.New("cbor: failed to parse field name \"" + f.name + "\" to int (" + numErr.Error() + ")"),
				}
				decodingStructTypeCache.Store(t, structType)
				return nil, structType.err
			}
		}

		if f.keyAsInt {
			if fieldIndicesByIntKey == nil {
				fieldIndicesByIntKey = make(map[int64]int, len(flds))
			}
			// The duplication check is only a safeguard, since collectFields() already deduplicates fields.
			if _, ok := fieldIndicesByIntKey[f.nameAsInt]; ok {
				structType := &decodingStructType{
					err: fmt.Errorf("cbor: two or more fields of %v have the same keyasint value %d", t, f.nameAsInt),
				}
				decodingStructTypeCache.Store(t, structType)
				return nil, structType.err
			}
			fieldIndicesByIntKey[f.nameAsInt] = i
		} else {
			// The duplication check is only a safeguard, since collectFields() already deduplicates fields.
			if _, ok := fieldIndicesByName[f.name]; ok {
				structType := &decodingStructType{
					err: fmt.Errorf("cbor: two or more fields of %v have the same name %q", t, f.name),
				}
				decodingStructTypeCache.Store(t, structType)
				return nil, structType.err
			}
			fieldIndicesByName[f.name] = i
		}

		decFlds[i] = &decodingField{
			field:   *f,
			typInfo: getTypeInfo(f.typ),
		}
	}

	structType := &decodingStructType{
		fields:               decFlds,
		fieldIndicesByName:   fieldIndicesByName,
		fieldIndicesByIntKey: fieldIndicesByIntKey,
	}
	decodingStructTypeCache.Store(t, structType)
	return structType, nil
}

func getDecodingStructToArrayType(t reflect.Type, flds fields) (*decodingStructType, error) {
	decFlds := make(decodingFields, len(flds))
	for i, f := range flds {
		// nameAsInt is set in collectFields() except for fields with an unparsable tagged name.
		// Atoi() is called here to catch and save parsing errors.
		if f.keyAsInt && f.nameAsInt == 0 {
			if _, numErr := strconv.Atoi(f.name); numErr != nil {
				structType := &decodingStructType{
					err: errors.New("cbor: failed to parse field name \"" + f.name + "\" to int (" + numErr.Error() + ")"),
				}
				decodingStructTypeCache.Store(t, structType)
				return nil, structType.err
			}
		}

		decFlds[i] = &decodingField{
			field:   *f,
			typInfo: getTypeInfo(f.typ),
		}
	}

	structType := &decodingStructType{
		fields:  decFlds,
		toArray: true,
	}
	decodingStructTypeCache.Store(t, structType)
	return structType, nil
}

type encodingStructType struct {
	fields             encodingFields
	bytewiseFields     encodingFields // Only populated if struct is not array-shaped
	lengthFirstFields  encodingFields // Only populated if struct is not array-shaped
	omitEmptyFieldsIdx []int          // Only populated if struct is not array-shaped
	err                error
	toArray            bool // True iff the struct declares the `toarray` option
	toIndefArray       bool // True iff the struct declares the `toindefarray` option
}

func (st *encodingStructType) getFields(em *encMode) encodingFields {
	switch em.sort {
	case SortNone, SortFastShuffle:
		return st.fields
	case SortLengthFirst:
		return st.lengthFirstFields
	default:
		return st.bytewiseFields
	}
}

func bytewiseFieldCmp(fi, fj *encodingField) int {
	return bytes.Compare(fi.cborName, fj.cborName)
}

func lengthFirstFieldCmp(fi, fj *encodingField) int {
	if len(fi.cborName) != len(fj.cborName) {
		return cmp.Compare(len(fi.cborName), len(fj.cborName))
	}
	return bytes.Compare(fi.cborName, fj.cborName)
}

func getEncodingStructType(t reflect.Type) (*encodingStructType, error) {
	if v, _ := encodingStructTypeCache.Load(t); v != nil {
		structType := v.(*encodingStructType)
		if structType.err != nil {
			return nil, structType.err
		}
		return structType, nil
	}

	flds, structOptions := collectFields(t)

	hasToArray, hasToIndefArray, err := parseArrayStructOptions(t, structOptions)
	if err != nil {
		structType := &encodingStructType{err: err}
		encodingStructTypeCache.Store(t, structType)
		return nil, err
	}
	if hasToArray || hasToIndefArray {
		return getEncodingStructToArrayType(t, flds, hasToArray, hasToIndefArray)
	}

	var hasKeyAsInt bool
	var hasKeyAsStr bool
	var omitEmptyIdx []int

	encFlds := make(encodingFields, len(flds))

	for i, f := range flds {
		encFlds[i] = &encodingField{field: *f}
		ef := encFlds[i]

		// Get field's encodeFunc
		ef.ef, ef.ief, ef.izf = getEncodeFunc(f.typ)
		if ef.ef == nil {
			structType := &encodingStructType{err: &UnsupportedTypeError{t}}
			encodingStructTypeCache.Store(t, structType)
			return nil, structType.err
		}

		// Encode field name
		if f.keyAsInt {
			if f.nameAsInt == 0 {
				// nameAsInt is set in collectFields() except for fields with an unparsable tagged name.
				// Atoi() is called here to catch and save parsing errors.
				if _, numErr := strconv.Atoi(f.name); numErr != nil {
					structType := &encodingStructType{
						err: errors.New("cbor: failed to parse field name \"" + f.name + "\" to int (" + numErr.Error() + ")"),
					}
					encodingStructTypeCache.Store(t, structType)
					return nil, structType.err
				}
			}
			nameAsInt := f.nameAsInt
			if nameAsInt >= 0 {
				ef.cborName = make([]byte, 0, encodedHeadLength(uint64(nameAsInt)))
				ef.cborName = appendHead(ef.cborName, byte(cborTypePositiveInt), uint64(nameAsInt))
			} else {
				n := nameAsInt*(-1) - 1
				ef.cborName = make([]byte, 0, encodedHeadLength(uint64(n)))
				ef.cborName = appendHead(ef.cborName, byte(cborTypeNegativeInt), uint64(n))
			}

			hasKeyAsInt = true
		} else {
			ef.cborName = make([]byte, 0, encodedHeadLength(uint64(len(f.name)))+len(f.name))
			ef.cborName = appendHead(ef.cborName, byte(cborTypeTextString), uint64(len(f.name)))
			ef.cborName = append(ef.cborName, f.name...)

			// If cborName contains a text string, then cborNameByteString contains a
			// string that has the byte string major type but is otherwise identical to
			// cborName.
			ef.cborNameByteString = make([]byte, len(ef.cborName))
			copy(ef.cborNameByteString, ef.cborName)
			// Reset encoded CBOR type to byte string, preserving the "additional
			// information" bits:
			ef.cborNameByteString[0] = byte(cborTypeByteString) |
				getAdditionalInformation(ef.cborNameByteString[0])

			hasKeyAsStr = true
		}

		// Check if field can be omitted when empty
		if f.omitEmpty {
			omitEmptyIdx = append(omitEmptyIdx, i)
		}
	}

	// Sort fields by canonical order
	bytewiseFields := make(encodingFields, len(encFlds))
	copy(bytewiseFields, encFlds)
	slices.SortFunc(bytewiseFields, bytewiseFieldCmp)

	lengthFirstFields := bytewiseFields
	if hasKeyAsInt && hasKeyAsStr {
		lengthFirstFields = make(encodingFields, len(encFlds))
		copy(lengthFirstFields, encFlds)
		slices.SortFunc(lengthFirstFields, lengthFirstFieldCmp)
	}

	structType := &encodingStructType{
		fields:             encFlds,
		bytewiseFields:     bytewiseFields,
		lengthFirstFields:  lengthFirstFields,
		omitEmptyFieldsIdx: omitEmptyIdx,
	}

	encodingStructTypeCache.Store(t, structType)
	return structType, nil
}

func getEncodingStructToArrayType(t reflect.Type, flds fields, toArray, toIndefArray bool) (*encodingStructType, error) {
	encFlds := make(encodingFields, len(flds))
	for i, f := range flds {
		encFlds[i] = &encodingField{field: *f}
		encFlds[i].ef, encFlds[i].ief, encFlds[i].izf = getEncodeFunc(f.typ)
		if encFlds[i].ef == nil {
			structType := &encodingStructType{err: &UnsupportedTypeError{t}}
			encodingStructTypeCache.Store(t, structType)
			return nil, structType.err
		}
	}

	structType := &encodingStructType{
		fields:       encFlds,
		toArray:      toArray,
		toIndefArray: toIndefArray,
	}
	encodingStructTypeCache.Store(t, structType)
	return structType, nil
}

type inProgressEncodeFuncs struct {
	encodeFuncs
	complete     bool
	indirectUsed bool
}

func getEncodeFunc(t reflect.Type) (encodeFunc, isEmptyFunc, isZeroFunc) {
	if v, _ := encodeFuncCache.Load(t); v != nil {
		fs := v.(encodeFuncs)
		return fs.ef, fs.ief, fs.izf
	}
	var rebuild bool
	newEncodeFuncs := make(map[reflect.Type]*inProgressEncodeFuncs)
	for unsupportedTypeCount := 0; ; {
		newEncodeFunc(t, newEncodeFuncs)
		newEncodeFuncs, rebuild = needRebuild(newEncodeFuncs)
		if !rebuild {
			break
		}
		if _, unsupported := newEncodeFuncs[t]; unsupported {
			break
		}
		// Every rebuild must find at least one new unsupported type.
		// This check is to ensure termination.
		if len(newEncodeFuncs) <= unsupportedTypeCount {
			newEncodeFuncs[t] = &inProgressEncodeFuncs{complete: true}
			break
		}
		unsupportedTypeCount = len(newEncodeFuncs)
	}
	for typ, fs := range newEncodeFuncs {
		encodeFuncCache.Store(typ, fs.encodeFuncs)
	}
	return newEncodeFuncs[t].ef, newEncodeFuncs[t].ief, newEncodeFuncs[t].izf
}

// needRebuild returns true if a type in the inProgressEncodeFuncs has a nil encodeFunc
// and was also indirectly used.
// In other words, rebuild is needed if a type:
// - was used in a closure encodeFunc during type building and
// - was resolved to be unsupported when type building was completed.
// If rebuild is needed, resolved unsupported types are returned as seed for the next rebuilding;
// otherwise, the unmodified newEncodeFuncs is returned.
// NOTE: rebuilding is not needed if all types are supported.
func needRebuild(newEncodeFuncs map[reflect.Type]*inProgressEncodeFuncs) (map[reflect.Type]*inProgressEncodeFuncs, bool) {
	rebuild := false
	unsupported := make(map[reflect.Type]*inProgressEncodeFuncs)
	for typ, fs := range newEncodeFuncs {
		if fs.ef == nil {
			if fs.indirectUsed {
				rebuild = true
			}
			unsupported[typ] = &inProgressEncodeFuncs{encodeFuncs: fs.encodeFuncs, complete: true}
		}
	}
	if !rebuild {
		return newEncodeFuncs, false
	}
	return unsupported, true
}

func getEncodeFuncWithNewEncodeFuncs(t reflect.Type, newEncodeFuncs map[reflect.Type]*inProgressEncodeFuncs) encodeFunc {
	if fs, found := newEncodeFuncs[t]; found {
		if fs.complete {
			return fs.ef
		}
		fs.indirectUsed = true
		return func(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
			if fs.ef == nil {
				return dst, &UnsupportedTypeError{t}
			}
			return fs.ef(dst, em, v)
		}
	}

	if v, _ := encodeFuncCache.Load(t); v != nil {
		fs := v.(encodeFuncs)
		return fs.ef
	}

	ef, _, _ := newEncodeFunc(t, newEncodeFuncs)
	return ef
}

func getTypeInfo(t reflect.Type) *typeInfo {
	if v, _ := typeInfoCache.Load(t); v != nil {
		return v.(*typeInfo)
	}
	newTypeInfos := make(map[reflect.Type]*typeInfo)
	tInfo := newTypeInfo(t, newTypeInfos)
	for typ, ti := range newTypeInfos {
		typeInfoCache.Store(typ, ti)
	}
	return tInfo
}

func getTypeInfoWithNewTypeInfos(t reflect.Type, newTypeInfos map[reflect.Type]*typeInfo) *typeInfo {
	if tInfo, found := newTypeInfos[t]; found {
		return tInfo
	}
	if v, _ := typeInfoCache.Load(t); v != nil {
		return v.(*typeInfo)
	}
	return newTypeInfo(t, newTypeInfos)
}

func hasToArrayOption(tag string) bool {
	s := ",toarray"
	idx := strings.Index(tag, s)
	return idx >= 0 && (len(tag) == idx+len(s) || tag[idx+len(s)] == ',')
}

func hasToIndefArrayOption(tag string) bool {
	s := ",toindefarray"
	idx := strings.Index(tag, s)
	return idx >= 0 && (len(tag) == idx+len(s) || tag[idx+len(s)] == ',')
}

// parseArrayStructOptions reports whether the struct options request encoding as
// a CBOR array (definite- or indefinite-length), and returns an error if the
// two options are specified together (they are mutually exclusive).
func parseArrayStructOptions(t reflect.Type, structOptions string) (hasToArray, hasToIndefArray bool, err error) {
	hasToArray = hasToArrayOption(structOptions)
	hasToIndefArray = hasToIndefArrayOption(structOptions)
	if hasToArray && hasToIndefArray {
		return false, false, fmt.Errorf("cbor: struct %v cannot have both \"toarray\" and \"toindefarray\" options", t)
	}
	return hasToArray, hasToIndefArray, nil
}
