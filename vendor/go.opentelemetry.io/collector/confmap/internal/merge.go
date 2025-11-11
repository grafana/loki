// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

import (
	"reflect"

	"github.com/gobwas/glob"
	"github.com/knadh/koanf/maps"
)

func mergeAppend(src, dest map[string]any) error {
	// mergeAppend recursively merges the src map into the dest map (left to right),
	// modifying and expanding the dest map in the process.
	// This function does not overwrite component lists, and ensures that the
	// final value is a name-aware copy of lists from src and dest.

	// Compile the globs once
	patterns := []string{
		"service::extensions",
		"service::**::receivers",
		"service::**::exporters",
	}
	var globs []glob.Glob
	for _, p := range patterns {
		if g, err := glob.Compile(p); err == nil {
			globs = append(globs, g)
		}
	}

	// Flatten both source and destination maps
	srcFlat, _ := maps.Flatten(src, []string{}, KeyDelimiter)
	destFlat, _ := maps.Flatten(dest, []string{}, KeyDelimiter)

	for sKey, sVal := range srcFlat {
		if !isMatch(sKey, globs) {
			continue
		}

		dVal, dOk := destFlat[sKey]
		if !dOk {
			continue // Let maps.Merge handle missing keys
		}

		srcVal := reflect.ValueOf(sVal)
		destVal := reflect.ValueOf(dVal)

		// Only merge if the value is a slice or array; let maps.Merge handle other types
		if srcVal.Kind() == reflect.Slice || srcVal.Kind() == reflect.Array {
			srcFlat[sKey] = mergeSlice(srcVal, destVal)
		}
	}

	// Unflatten and merge
	mergedSrc := maps.Unflatten(srcFlat, KeyDelimiter)
	maps.Merge(mergedSrc, dest)

	return nil
}

// isMatch checks if a key matches any glob in the list
func isMatch(key string, globs []glob.Glob) bool {
	for _, g := range globs {
		if g.Match(key) {
			return true
		}
	}
	return false
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

func isPresent(slice, val reflect.Value) bool {
	for i := 0; i < slice.Len(); i++ {
		if reflect.DeepEqual(val.Interface(), slice.Index(i).Interface()) {
			return true
		}
	}
	return false
}
