// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"math"
)

var errTooManyLinkTableEntries = errors.New("too many entries in LinkTable")

// SetLink updates a LinkTable, adding or providing a value and returns its
// index.
func SetLink(table LinkSlice, li Link) (int32, error) {
	for j, l := range table.All() {
		if l.Equal(li) {
			if j > math.MaxInt32 {
				return 0, errTooManyLinkTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyLinkTableEntries
	}

	li.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
