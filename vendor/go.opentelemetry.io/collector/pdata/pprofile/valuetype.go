// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// switchDictionary updates the ValueType, switching its indices from one
// dictionary to another.
func (ms ValueType) switchDictionary(src, dst ProfilesDictionary) error {
	if ms.TypeStrindex() > 0 {
		if src.StringTable().Len() <= int(ms.TypeStrindex()) {
			return fmt.Errorf("invalid type index %d", ms.TypeStrindex())
		}

		idx, err := SetString(dst.StringTable(), src.StringTable().At(int(ms.TypeStrindex())))
		if err != nil {
			return fmt.Errorf("couldn't set type: %w", err)
		}
		ms.SetTypeStrindex(idx)
	}

	if ms.UnitStrindex() > 0 {
		if src.StringTable().Len() <= int(ms.UnitStrindex()) {
			return fmt.Errorf("invalid unit index %d", ms.UnitStrindex())
		}

		idx, err := SetString(dst.StringTable(), src.StringTable().At(int(ms.UnitStrindex())))
		if err != nil {
			return fmt.Errorf("couldn't set unit: %w", err)
		}
		ms.SetUnitStrindex(idx)
	}

	return nil
}
