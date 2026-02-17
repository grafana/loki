// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"encoding/hex"
	"errors"

	"go.opentelemetry.io/collector/pdata/internal/json"
)

const profileIDSize = 16

var errUnmarshalProfileID = errors.New("unmarshal: invalid ProfileID length")

// ProfileID is a custom data type that is used for all profile_id fields in OTLP
// Protobuf messages.
type ProfileID [profileIDSize]byte

func DeleteProfileID(*ProfileID, bool) {}

func CopyProfileID(dest, src *ProfileID) {
	*dest = *src
}

// IsEmpty returns true if id contains at leas one non-zero byte.
func (pid ProfileID) IsEmpty() bool {
	return pid == [profileIDSize]byte{}
}

// SizeProto returns the size of the data to serialize in proto format.
func (pid ProfileID) SizeProto() int {
	if pid.IsEmpty() {
		return 0
	}

	return profileIDSize
}

// MarshalProto converts profile ID into a binary representation. Called by Protobuf serialization.
func (pid ProfileID) MarshalProto(buf []byte) int {
	if pid.IsEmpty() {
		return 0
	}

	return copy(buf[len(buf)-profileIDSize:], pid[:])
}

// UnmarshalProto inflates this profile ID from binary representation. Called by Protobuf serialization.
func (pid *ProfileID) UnmarshalProto(buf []byte) error {
	if len(buf) == 0 {
		*pid = [profileIDSize]byte{}
		return nil
	}

	if len(buf) != profileIDSize {
		return errUnmarshalProfileID
	}

	copy(pid[:], buf)
	return nil
}

// MarshalJSON converts ProfileID into a hex string.
//
//nolint:govet
func (pid ProfileID) MarshalJSON(dest *json.Stream) {
	dest.WriteString(hex.EncodeToString(pid[:]))
}

// UnmarshalJSON decodes ProfileID from hex string.
//
//nolint:govet
func (pid *ProfileID) UnmarshalJSON(iter *json.Iterator) {
	*pid = [profileIDSize]byte{}
	unmarshalJSON(pid[:], iter)
}

func GenTestProfileID() *ProfileID {
	pid := ProfileID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1})
	return &pid
}
