// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"math"
)

var errTooManyFunctionTableEntries = errors.New("too many entries in FunctionTable")

// SetFunction updates a FunctionTable, adding or providing a value and returns
// its index.
func SetFunction(table FunctionSlice, fn Function) (int32, error) {
	for j, m := range table.All() {
		if m.Equal(fn) {
			if j > math.MaxInt32 {
				return 0, errTooManyFunctionTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyFunctionTableEntries
	}

	fn.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
