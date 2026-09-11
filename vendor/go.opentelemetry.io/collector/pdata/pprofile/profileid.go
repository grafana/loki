// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"encoding/hex"

	"go.opentelemetry.io/collector/pdata/internal"
)

var emptyProfileID = ProfileID([16]byte{})

// ProfileID is a profile identifier.
type ProfileID [16]byte

// NewProfileIDEmpty returns a new empty (all zero bytes) ProfileID.
func NewProfileIDEmpty() ProfileID {
	return emptyProfileID
}

// String returns string representation of the ProfileID.
//
// Important: Don't rely on this method to get a string identifier of ProfileID.
// Use hex.EncodeToString explicitly instead.
// This method is meant to implement Stringer interface for display purposes only.
func (ms ProfileID) String() string {
	if ms.IsEmpty() {
		return ""
	}
	return hex.EncodeToString(ms[:])
}

// IsEmpty returns true if id doesn't contain at least one non-zero byte.
func (ms ProfileID) IsEmpty() bool {
	return internal.ProfileID(ms).IsEmpty()
}
