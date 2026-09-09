// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"math"
)

var errTooManyMappingTableEntries = errors.New("too many entries in MappingTable")

// SetMapping updates a MappingTable, adding or providing a value and returns
// its index.
func SetMapping(table MappingSlice, ma Mapping) (int32, error) {
	for j, m := range table.All() {
		if m.Equal(ma) {
			if j > math.MaxInt32 {
				return 0, errTooManyMappingTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyMappingTableEntries
	}

	ma.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
