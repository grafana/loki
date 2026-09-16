// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// switchDictionary updates the ResourceProfiles, switching its indices from one
// dictionary to another.
func (ms ResourceProfiles) switchDictionary(src, dst ProfilesDictionary) error {
	for i, v := range ms.ScopeProfiles().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("error switching dictionary for scope profile %d: %w", i, err)
		}
	}

	return nil
}
