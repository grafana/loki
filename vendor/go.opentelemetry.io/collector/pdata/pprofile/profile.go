// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"fmt"
)

// switchDictionary updates the Profile, switching its indices from one
// dictionary to another.
func (ms Profile) switchDictionary(src, dst ProfilesDictionary, mi *mergeIndex) error {
	for i, v := range ms.AttributeIndices().All() {
		if v < 0 || src.AttributeTable().Len() <= int(v) {
			return fmt.Errorf("invalid attribute index %d", v)
		}

		attr := src.AttributeTable().At(int(v))
		idx, err := mi.setAttribute(dst.AttributeTable(), attr)
		if err != nil {
			return fmt.Errorf("couldn't set attribute %d: %w", i, err)
		}
		ms.AttributeIndices().SetAt(i, idx)
	}

	for i, v := range ms.Samples().All() {
		if err := v.switchDictionary(src, dst, mi); err != nil {
			return fmt.Errorf("error switching dictionary for sample %d: %w", i, err)
		}
	}

	if err := ms.PeriodType().switchDictionary(src, dst, mi); err != nil {
		return fmt.Errorf("error switching dictionary for period type: %w", err)
	}
	if err := ms.SampleType().switchDictionary(src, dst, mi); err != nil {
		return fmt.Errorf("error switching dictionary for sample type: %w", err)
	}

	return nil
}
