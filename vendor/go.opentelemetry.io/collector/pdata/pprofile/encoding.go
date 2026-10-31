// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

// MarshalSizer is the interface that groups the basic Marshal and Size methods
type MarshalSizer interface {
	Marshaler
	Sizer
}

// Marshaler marshals pprofile.Profiles into bytes.
type Marshaler interface {
	// MarshalProfiles the given pprofile.Profiles into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalProfiles(td Profiles) ([]byte, error)
}

// Unmarshaler unmarshalls bytes into pprofile.Profiles.
type Unmarshaler interface {
	// UnmarshalProfiles the given bytes into pprofile.Profiles.
	// If the error is not nil, the returned pprofile.Profiles cannot be used.
	UnmarshalProfiles(buf []byte) (Profiles, error)
}

// Sizer is an optional interface implemented by the Marshaler,
// that calculates the size of a marshaled Profiles.
type Sizer interface {
	// ProfilesSize returns the size in bytes of a marshaled Profiles.
	ProfilesSize(td Profiles) int
}
