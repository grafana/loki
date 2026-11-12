// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// switchDictionary updates the ScopeProfiles, switching its indices from one
// dictionary to another.
func (ms ScopeProfiles) switchDictionary(src, dst ProfilesDictionary, mi *mergeIndex) error {
	for i, v := range ms.Profiles().All() {
		if err := v.switchDictionary(src, dst, mi); err != nil {
			return fmt.Errorf("error switching dictionary for profile %d: %w", i, err)
		}
	}

	return nil
}
