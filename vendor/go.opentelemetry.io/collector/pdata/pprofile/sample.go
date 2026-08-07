// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// switchDictionary updates the Sample, switching its indices from one
// dictionary to another.
func (ms Sample) switchDictionary(src, dst ProfilesDictionary) error {
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

	if ms.LinkIndex() > 0 {
		if src.LinkTable().Len() <= int(ms.LinkIndex()) {
			return fmt.Errorf("invalid link index %d", ms.LinkIndex())
		}

		idx, err := SetLink(dst.LinkTable(), src.LinkTable().At(int(ms.LinkIndex())))
		if err != nil {
			return fmt.Errorf("couldn't set link: %w", err)
		}
		ms.SetLinkIndex(idx)
	}

	if ms.StackIndex() > 0 {
		if src.StackTable().Len() <= int(ms.StackIndex()) {
			return fmt.Errorf("invalid stack index %d", ms.StackIndex())
		}

		stack := src.StackTable().At(int(ms.StackIndex()))
		idx, err := SetStack(dst.StackTable(), stack)
		if err != nil {
			return fmt.Errorf("couldn't set stack: %w", err)
		}
		ms.SetStackIndex(idx)
	}

	return nil
}
