// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"fmt"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

// switchDictionary updates the Profile, switching its indices from one
// dictionary to another.
func (ms Profile) switchDictionary(src, dst ProfilesDictionary) error {
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

	for i, v := range ms.Samples().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("error switching dictionary for sample %d: %w", i, err)
		}
	}

	err := ms.PeriodType().switchDictionary(src, dst)
	if err != nil {
		return fmt.Errorf("error switching dictionary for period type: %w", err)
	}
	err = ms.SampleType().switchDictionary(src, dst)
	if err != nil {
		return fmt.Errorf("error switching dictionary for sample type: %w", err)
	}

	return nil
}

// Duration returns the duration associated with this Profile.
//
// Deprecated: Use Profile.DurationNano instead.
func (ms Profile) Duration() pcommon.Timestamp {
	return pcommon.Timestamp(0)
}

// SetDuration replaces the duration associated with this Profile.
//
// Deprecated: Use Profile.SetDurationNano instead.
func (ms Profile) SetDuration(_ pcommon.Timestamp) {
}
