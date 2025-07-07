// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"reflect"
)

func mergeAppend(src, dest map[string]any) error {
	// mergeAppend recursively merges the src map into the dest map (left to right),
	// modifying and expanding the dest map in the process.
	// This function does not overwrite lists, and ensures that the final value is a name-aware
	// copy of lists from src and dest.

	for sKey, sVal := range src {
		dVal, dOk := dest[sKey]
		if !dOk {
			// key is not present in destination config. Hence, add it to destination map
			dest[sKey] = sVal
			continue
		}

		srcVal := reflect.ValueOf(sVal)
		destVal := reflect.ValueOf(dVal)

		if destVal.Kind() != srcVal.Kind() {
			// different kinds. Override the destination map
			dest[sKey] = sVal
			continue
		}

		switch srcVal.Kind() {
		case reflect.Array, reflect.Slice:
			// both of them are array. Merge them
			dest[sKey] = mergeSlice(srcVal, destVal)
		case reflect.Map:
			// both of them are maps. Recursively call the mergeAppend
			_ = mergeAppend(sVal.(map[string]any), dVal.(map[string]any))
		default:
			// any other datatype. Override the destination map
			dest[sKey] = sVal
		}
	}

	return nil
}

func mergeSlice(src, dest reflect.Value) any {
	slice := reflect.MakeSlice(src.Type(), 0, src.Cap()+dest.Cap())
	for i := 0; i < dest.Len(); i++ {
		slice = reflect.Append(slice, dest.Index(i))
	}

	for i := 0; i < src.Len(); i++ {
		if isPresent(slice, src.Index(i)) {
			continue
		}
		slice = reflect.Append(slice, src.Index(i))
	}
	return slice.Interface()
}

func isPresent(slice reflect.Value, val reflect.Value) bool {
	for i := 0; i < slice.Len(); i++ {
		if slice.Index(i).Equal(val) {
			return true
		}
	}
	return false
}
