// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// Equal checks equality with another Location
func (ms Location) Equal(val Location) bool {
	return ms.MappingIndex() == val.MappingIndex() &&
		ms.Address() == val.Address() &&
		ms.AttributeIndices().Equal(val.AttributeIndices()) &&
		ms.Lines().Equal(val.Lines())
}

// switchDictionary updates the Location, switching its indices from one
// dictionary to another.
func (ms Location) switchDictionary(src, dst ProfilesDictionary) error {
	if ms.MappingIndex() > 0 {
		if src.MappingTable().Len() <= int(ms.MappingIndex()) {
			return fmt.Errorf("invalid mapping index %d", ms.MappingIndex())
		}

		mapping := src.MappingTable().At(int(ms.MappingIndex()))
		idx, err := SetMapping(dst.MappingTable(), mapping)
		if err != nil {
			return fmt.Errorf("couldn't set mapping: %w", err)
		}
		ms.SetMappingIndex(idx)
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

	for i, v := range ms.Lines().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch dictionary for line %d: %w", i, err)
		}
	}

	return nil
}
