// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// MarkReadOnly marks the ResourceProfiles as shared so that no further modifications can be done on it.
func (ms Profiles) MarkReadOnly() {
	ms.getState().MarkReadOnly()
}

// IsReadOnly returns true if this ResourceProfiles instance is read-only.
func (ms Profiles) IsReadOnly() bool {
	return ms.getState().IsReadOnly()
}

// SampleCount calculates the total number of samples.
func (ms Profiles) SampleCount() int {
	sampleCount := 0
	rps := ms.ResourceProfiles()
	for i := 0; i < rps.Len(); i++ {
		rp := rps.At(i)
		sps := rp.ScopeProfiles()
		for j := 0; j < sps.Len(); j++ {
			pcs := sps.At(j).Profiles()
			for k := 0; k < pcs.Len(); k++ {
				sampleCount += pcs.At(k).Samples().Len()
			}
		}
	}
	return sampleCount
}

// switchDictionary updates the Profiles, switching its indices from one
// dictionary to another.
func (ms Profiles) switchDictionary(src, dst ProfilesDictionary) error {
	for i, v := range ms.Dictionary().AttributeTable().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch attribute %d: %w", i, err)
		}
	}
	for i, v := range ms.Dictionary().FunctionTable().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch function %d: %w", i, err)
		}
	}
	for i, v := range ms.Dictionary().MappingTable().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch mapping %d: %w", i, err)
		}
	}
	for i, v := range ms.Dictionary().LocationTable().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch location %d: %w", i, err)
		}
	}
	for i, v := range ms.Dictionary().StackTable().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("couldn't switch stack %d: %w", i, err)
		}
	}

	for i, v := range ms.ResourceProfiles().All() {
		err := v.switchDictionary(src, dst)
		if err != nil {
			return fmt.Errorf("error switching dictionary for resource profile %d: %w", i, err)
		}
	}

	return nil
}

// ProfileCount calculates the total number of profile records.
func (ms Profiles) ProfileCount() int {
	profileCount := 0
	rps := ms.ResourceProfiles()
	for i := 0; i < rps.Len(); i++ {
		rp := rps.At(i)
		sps := rp.ScopeProfiles()
		for j := 0; j < sps.Len(); j++ {
			profileCount += sps.At(j).Profiles().Len()
		}
	}
	return profileCount
}
