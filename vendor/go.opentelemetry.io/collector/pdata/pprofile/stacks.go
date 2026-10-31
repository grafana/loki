// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"math"
)

var errTooManyStackTableEntries = errors.New("too many entries in StackTable")

// SetStack updates a StackSlice, adding or providing a stack and returns its
// index.
func SetStack(table StackSlice, st Stack) (int32, error) {
	for j, l := range table.All() {
		if l.Equal(st) {
			if j > math.MaxInt32 {
				return 0, errTooManyStackTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyStackTableEntries
	}

	st.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
