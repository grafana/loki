// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// ProfilesToProto internal helper to convert Profiles to protobuf representation.
func ProfilesToProto(l ProfilesWrapper) ProfilesData {
	return ProfilesData{
		ResourceProfiles: l.orig.ResourceProfiles,
		Dictionary:       l.orig.Dictionary,
	}
}

// ProfilesFromProto internal helper to convert protobuf representation to Profiles.
// This function set exclusive state assuming that it's called only once per Profiles.
func ProfilesFromProto(orig ProfilesData) ProfilesWrapper {
	return NewProfilesWrapper(&ExportProfilesServiceRequest{
		ResourceProfiles: orig.ResourceProfiles,
		Dictionary:       orig.Dictionary,
	}, NewState())
}
