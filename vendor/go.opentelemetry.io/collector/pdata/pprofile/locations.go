// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"fmt"
	"math"
)

// FromLocationIndices builds a slice containing all the locations of a Stack.
// Updates made to the returned map will not be applied back to the Stack.
func FromLocationIndices(table LocationSlice, record Stack) (LocationSlice, error) {
	m := NewLocationSlice()
	m.EnsureCapacity(record.LocationIndices().Len())

	for _, idx := range record.LocationIndices().All() {
		if idx < 0 || int(idx) >= table.Len() {
			return m, fmt.Errorf("location index %d out of bounds [0, %d)", idx, table.Len())
		}
		l := table.At(int(idx))
		l.CopyTo(m.AppendEmpty())
	}

	return m, nil
}

var errTooManyLocationTableEntries = errors.New("too many entries in LocationTable")

// SetLocation updates a LocationTable, adding or providing a value and returns
// its index.
func SetLocation(table LocationSlice, loc Location) (int32, error) {
	for j, a := range table.All() {
		if a.Equal(loc) {
			if j > math.MaxInt32 {
				return 0, errTooManyLocationTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyLocationTableEntries
	}

	loc.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
