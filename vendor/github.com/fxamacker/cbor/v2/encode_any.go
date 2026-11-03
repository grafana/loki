// Copyright (c) Faye Amacker. All rights reserved.
// Licensed under the MIT License. See LICENSE in the project root for license information.

package cbor

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// enableEncodingFastPath is used in tests to ensure that
// fastEncode produces the same encoded data as the reflect path.
var enableEncodingFastPath = true

// fastPathKindsMask is a set of reflect.Kind values that fastEncode() is likely to handle.
// NOTE: fastPathKindsMask must be in sync with fastEncode.
const fastPathKindsMask = 1<<reflect.Int |
	1<<reflect.Int64 |
	1<<reflect.Uint |
	1<<reflect.Uint64 |
	1<<reflect.String |
	1<<reflect.Float64 |
	1<<reflect.Bool |
	1<<reflect.Invalid |
	1<<reflect.Slice |
	1<<reflect.Map

func tryFastPath(rv reflect.Value) bool {
	// NOTE: this function is written to be inlinable.
	return enableEncodingFastPath && fastPathKindsMask&(1<<rv.Kind()) != 0
}

// fastEncode uses reflect-less path to encode values of a few selected types.
// NOTE:
// - fastEncode doesn't handle any value that the reflect path doesn't.
// - fastEncode produces the same encoded data as the reflect path.
// - fastEncode must be in sync with fastPathKindsMask.
func fastEncode(dst []byte, em *encMode, v any) (_ []byte, handled bool, err error) {
	switch v := v.(type) {
	case int:
		if v >= 0 {
			dst = appendPositiveInt(dst, uint64(v))
			return dst, true, nil
		}
		dst = appendNegativeInt(dst, int64(v))
		return dst, true, nil

	case int64:
		if v >= 0 {
			dst = appendPositiveInt(dst, uint64(v))
			return dst, true, nil
		}
		dst = appendNegativeInt(dst, v)
		return dst, true, nil

	case uint:
		dst = appendPositiveInt(dst, uint64(v))
		return dst, true, nil

	case uint64:
		dst = appendPositiveInt(dst, v)
		return dst, true, nil

	case []byte:
		dst = appendByteSliceWithEM(dst, em, v)
		return dst, true, nil

	case string:
		dst = appendStringWithEM(dst, em, v)
		return dst, true, nil

	case float64:
		dst, err = appendFloat64WithEM(dst, em, v)
		return dst, true, err

	case bool:
		dst = appendBool(dst, v)
		return dst, true, nil

	case nil:
		dst = append(dst, cborNil)
		return dst, true, nil

	case []any:
		dst, err = appendSliceAnyWithEM(dst, em, v)
		return dst, true, err

	case map[any]any:
		dst, err = appendMapAnyAnyWithEM(dst, em, v)
		return dst, true, err

	case map[string]any:
		dst, err = appendMapStringAnyWithEM(dst, em, v)
		return dst, true, err

	case map[string]string:
		dst, err = appendMapStringStringWithEM(dst, em, v)
		return dst, true, err
	}

	return dst, false, nil
}

// NOTE: appendFloat64WithEM must be in sync with encodeFloat and produce the same encoded data.
func appendFloat64WithEM(dst []byte, em *encMode, f64 float64) ([]byte, error) {
	if math.IsNaN(f64) {
		return encodeNaN(dst, em, reflect.ValueOf(f64))
	}
	if math.IsInf(f64, 0) {
		return encodeInf(dst, em, reflect.ValueOf(f64))
	}
	if em.shortestFloat == ShortestFloatNone {
		return appendFloat64(dst, f64), nil
	}
	return appendShortestFloat64(dst, f64)
}

// NOTE: appendByteSliceWithEM must be in sync with encodeByteSlice and produce the same encoded data.
func appendByteSliceWithEM(dst []byte, em *encMode, bs []byte) []byte {
	if bs == nil && em.nilContainers == NilContainerAsNull {
		return append(dst, cborNil)
	}
	if em.byteSliceLaterEncodingTag != 0 {
		dst = appendHead(dst, byte(cborTypeTag), em.byteSliceLaterEncodingTag)
	}
	return appendByteString(dst, bs)
}

// NOTE: appendSliceAnyWithEM must be in sync with arrayEncodeFunc.encode and produce the same encoded data.
func appendSliceAnyWithEM(dst []byte, em *encMode, v []any) ([]byte, error) {
	var done bool
	dst, done = appendNilOrEmptyContainer(dst, em, cborTypeArray, v)
	if done {
		return dst, nil
	}

	var err error
	dst = appendHead(dst, byte(cborTypeArray), uint64(len(v)))
	for _, elem := range v {
		dst, err = appendAnyWithEM(dst, em, elem)
		if err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// mapStringKeysPool caches []string for sorting map string keys.
var mapStringKeysPool = sync.Pool{
	New: func() any {
		return new([]string)
	},
}

func getMapKeyStrings(length int) *[]string {
	p := mapStringKeysPool.Get().(*[]string)
	if cap(*p) < length {
		*p = make([]string, 0, length)
	} else {
		*p = (*p)[:0]
	}
	return p
}

func putMapKeyStrings(p *[]string) {
	if cap(*p) > maxPooledKeyValues {
		*p = nil
	} else {
		clear(*p)
		*p = (*p)[:0]
	}
	mapStringKeysPool.Put(p)
}

// NOTE: appendMapAnyAnyWithEM must be in sync with mapEncodeFunc.encode and produce the same encoded data.
func appendMapAnyAnyWithEM(dst []byte, em *encMode, m map[any]any) ([]byte, error) {
	var done bool
	dst, done = appendNilOrEmptyContainer(dst, em, cborTypeMap, m)
	if done {
		return dst, nil
	}

	var err error
	mlen := len(m)
	dst = appendHead(dst, byte(cborTypeMap), uint64(mlen))
	if em.sort == SortNone || em.sort == SortFastShuffle || mlen <= 1 {
		for k, v := range m {
			// Encode map key
			dst, err = appendAnyWithEM(dst, em, k)
			if err != nil {
				return dst, err
			}

			// Encode map value
			dst, err = appendAnyWithEM(dst, em, v)
			if err != nil {
				return dst, err
			}
		}
		return dst, nil
	}

	kvsp := getKeyValues(len(m)) // for sorting keys
	defer putKeyValues(kvsp)
	kvs := *kvsp

	kvBeginOffset := len(dst)
	i := 0
	for k, v := range m {
		// Encode map key
		offset := len(dst)
		dst, err = appendAnyWithEM(dst, em, k)
		if err != nil {
			return dst, err
		}

		// Encode map value
		valueOffset := len(dst)
		dst, err = appendAnyWithEM(dst, em, v)
		if err != nil {
			return dst, err
		}

		kvs[i] = keyValue{
			offset:      offset - kvBeginOffset,
			valueOffset: valueOffset - kvBeginOffset,
			nextOffset:  len(dst) - kvBeginOffset,
		}
		i++
	}

	return sortEncodedKeyValues(dst, em, kvs, kvBeginOffset), nil
}

// NOTE: appendMapStringAnyWithEM must be in sync with mapEncodeFunc.encode and produce the same encoded data.
func appendMapStringAnyWithEM(dst []byte, em *encMode, m map[string]any) ([]byte, error) {
	var done bool
	dst, done = appendNilOrEmptyContainer(dst, em, cborTypeMap, m)
	if done {
		return dst, nil
	}

	var err error
	mlen := len(m)
	dst = appendHead(dst, byte(cborTypeMap), uint64(mlen))
	if em.sort == SortNone || em.sort == SortFastShuffle || mlen <= 1 {
		for k, v := range m {
			// Encode map key
			dst = appendStringWithEM(dst, em, k)

			// Encode map value
			dst, err = appendAnyWithEM(dst, em, v)
			if err != nil {
				return dst, err
			}
		}
		return dst, nil
	}

	return withSortedKeys(dst, m, func(dst []byte, m map[string]any, keys []string) ([]byte, error) {
		var err error
		for _, k := range keys {
			// Encode map key
			dst = appendStringWithEM(dst, em, k)

			// Encode map value
			dst, err = appendAnyWithEM(dst, em, m[k])
			if err != nil {
				return dst, err
			}
		}
		return dst, nil
	})
}

// NOTE: appendMapStringStringWithEM must be in sync with mapEncodeFunc.encode and produce the same encoded data.
func appendMapStringStringWithEM(dst []byte, em *encMode, m map[string]string) ([]byte, error) {
	var done bool
	dst, done = appendNilOrEmptyContainer(dst, em, cborTypeMap, m)
	if done {
		return dst, nil
	}

	mlen := len(m)
	dst = appendHead(dst, byte(cborTypeMap), uint64(mlen))
	if em.sort == SortNone || em.sort == SortFastShuffle || mlen <= 1 {
		for k, v := range m {
			dst = appendStringWithEM(dst, em, k)
			dst = appendStringWithEM(dst, em, v)
		}
		return dst, nil
	}

	return withSortedKeys(dst, m, func(dst []byte, m map[string]string, keys []string) ([]byte, error) {
		for _, k := range keys {
			dst = appendStringWithEM(dst, em, k)
			dst = appendStringWithEM(dst, em, m[k])
		}
		return dst, nil
	})
}

type fastPathContainer interface {
	[]any | map[any]any | map[string]any | map[string]string
}

func appendNilOrEmptyContainer[C fastPathContainer](dst []byte, em *encMode, t cborType, c C) (_ []byte, done bool) {
	// NOTE: this function is written to be inlinable.
	if c == nil && em.nilContainers == NilContainerAsNull {
		return append(dst, cborNil), true
	}
	if len(c) == 0 {
		return append(dst, byte(t)), true
	}
	return dst, false
}

func withSortedKeys[V any](
	dst []byte,
	m map[string]V,
	encodeMapByKeys func(dst []byte, m map[string]V, keys []string) ([]byte, error),
) ([]byte, error) {
	pkeys := getMapKeyStrings(len(m))
	defer putMapKeyStrings(pkeys)
	keys := *pkeys

	for k := range m {
		keys = append(keys, k)
	}
	*pkeys = keys

	// Sorting strings in length-first order and bytewise lexicographic order
	// render the same result.
	slices.SortFunc(keys, func(a, b string) int {
		if len(a) == len(b) {
			return strings.Compare(a, b)
		}
		return len(a) - len(b)
	})

	return encodeMapByKeys(dst, m, keys)
}

func appendAnyWithEM(dst []byte, em *encMode, v any) ([]byte, error) {
	dst, handled, err := fastEncode(dst, em, v)
	if !handled {
		dst, err = encode(dst, em, reflect.ValueOf(v))
	}
	return dst, err
}
