// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// Equal checks equality with another Mapping
func (ms Mapping) Equal(val Mapping) bool {
	return ms.MemoryStart() == val.MemoryStart() &&
		ms.MemoryLimit() == val.MemoryLimit() &&
		ms.FileOffset() == val.FileOffset() &&
		ms.FilenameStrindex() == val.FilenameStrindex() &&
		ms.AttributeIndices().Equal(val.AttributeIndices())
}

// switchDictionary updates the Mapping, switching its indices from one
// dictionary to another.
func (ms Mapping) switchDictionary(src, dst ProfilesDictionary) error {
	if ms.FilenameStrindex() > 0 {
		if src.StringTable().Len() <= int(ms.FilenameStrindex()) {
			return fmt.Errorf("invalid filename index %d", ms.FilenameStrindex())
		}

		idx, err := SetString(dst.StringTable(), src.StringTable().At(int(ms.FilenameStrindex())))
		if err != nil {
			return fmt.Errorf("couldn't set filename: %w", err)
		}
		ms.SetFilenameStrindex(idx)
	}

	for i, v := range ms.AttributeIndices().All() {
		if src.AttributeTable().Len() <= int(v) {
			return fmt.Errorf("invalid attribute index %d", v)
		}

		attr := src.AttributeTable().At(int(v))
		idx, err := SetAttribute(dst.AttributeTable(), attr)
		if err != nil {
			return fmt.Errorf("couldn't set attribute %d: %w", i, err)
		}
		ms.AttributeIndices().SetAt(i, idx)
	}

	return nil
}
