// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/low"
)

// buildSlice converts a slice of low.ValueReference[L] to a slice of H using a conversion function.
func buildSlice[L any, H any](refs []low.ValueReference[L], convert func(L) H) []H {
	if len(refs) == 0 {
		return nil
	}
	out := make([]H, 0, len(refs))
	for _, ref := range refs {
		out = append(out, convert(ref.Value))
	}
	return out
}

// buildValueSlice extracts the Value from each low.ValueReference into a plain slice.
func buildValueSlice[T any](refs []low.ValueReference[T]) []T {
	if len(refs) == 0 {
		return nil
	}
	out := make([]T, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Value)
	}
	return out
}
