// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"math"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

var errTooManyStringTableEntries = errors.New("too many entries in StringTable")

// SetString updates a StringTable, adding or providing a value and returns its index.
func SetString(table pcommon.StringSlice, val string) (int32, error) {
	for j, v := range table.All() {
		if v == val {
			if j > math.MaxInt32 {
				return 0, errTooManyStringTableEntries
			}
			// Return the index of the existing value.
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyStringTableEntries
	}

	table.Append(val)
	return int32(table.Len() - 1), nil
}
