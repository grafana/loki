// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"fmt"
)

// Equal checks equality with another Stack
func (ms Stack) Equal(val Stack) bool {
	if ms.LocationIndices().Len() != val.LocationIndices().Len() {
		return false
	}

	for i := range ms.LocationIndices().Len() {
		if ms.LocationIndices().At(i) != val.LocationIndices().At(i) {
			return false
		}
	}

	return true
}

// switchDictionary updates the Stack, switching its indices from one
// dictionary to another.
func (ms Stack) switchDictionary(src, dst ProfilesDictionary) error {
	for i, v := range ms.LocationIndices().All() {
		if src.LocationTable().Len() <= int(v) {
			return fmt.Errorf("invalid location index %d", v)
		}

		loc := src.LocationTable().At(int(v))
		idx, err := SetLocation(dst.LocationTable(), loc)
		if err != nil {
			return fmt.Errorf("couldn't set location %d: %w", i, err)
		}
		ms.LocationIndices().SetAt(i, idx)
	}

	return nil
}
