// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

// MarshalProfiles marshals Profiles to gRPC format bytes.
// If the input data is read-only, it will be copied to a mutable
// instance before mutation.
func (e *ProtoMarshaler) MarshalProfiles(pd Profiles) ([]byte, error) {
	// Only copy if data is shared/read-only to avoid unnecessary allocation
	pdToUse := pd
	if pd.IsReadOnly() {
		pdCopy := NewProfiles()
		pd.CopyTo(pdCopy)
		pdToUse = pdCopy
	}

	// Convert strings to references for efficient transmission
	convertProfilesToReferences(pdToUse)

	size := pdToUse.getOrig().SizeProto()
	buf := make([]byte, size)
	_ = pdToUse.getOrig().MarshalProto(buf)
	return buf, nil
}

func (e *ProtoMarshaler) ProfilesSize(pd Profiles) int {
	return pd.getOrig().SizeProto()
}

func (e *ProtoMarshaler) ResourceProfilesSize(pd ResourceProfiles) int {
	return pd.orig.SizeProto()
}

func (e *ProtoMarshaler) ScopeProfilesSize(pd ScopeProfiles) int {
	return pd.orig.SizeProto()
}

func (e *ProtoMarshaler) ProfileSize(pd Profile) int {
	return pd.orig.SizeProto()
}

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalProfiles(buf []byte) (Profiles, error) {
	pd := NewProfiles()
	err := pd.getOrig().UnmarshalProto(buf)
	if err != nil {
		return Profiles{}, err
	}

	// Resolve all string_value_ref and key_ref to their actual strings
	// so the pdata API works transparently
	resolveProfilesReferences(pd)

	return pd, nil
}
