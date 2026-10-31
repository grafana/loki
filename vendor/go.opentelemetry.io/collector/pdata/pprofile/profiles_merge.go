// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

// MergeTo merges the current Profiles into dest, updating the destination
// dictionary as needed and appending the resource profiles.
// The source Profiles is consumed and marked read-only after this operation.
func (ms Profiles) MergeTo(dest Profiles) error {
	ms.getState().AssertMutable()
	dest.getState().AssertMutable()
	if ms.getOrig() == dest.getOrig() {
		return nil
	}

	if err := ms.switchDictionary(ms.Dictionary(), dest.Dictionary()); err != nil {
		return err
	}

	ms.ResourceProfiles().MoveAndAppendTo(dest.ResourceProfiles())
	ms.MarkReadOnly()

	return nil
}
