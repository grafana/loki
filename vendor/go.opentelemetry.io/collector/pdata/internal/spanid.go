// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"encoding/hex"
	"errors"

	"go.opentelemetry.io/collector/pdata/internal/json"
)

const spanIDSize = 8

var errUnmarshalSpanID = errors.New("unmarshal: invalid SpanID length")

// SpanID is a custom data type that is used for all span_id fields in OTLP
// Protobuf messages.
type SpanID [spanIDSize]byte

func DeleteSpanID(*SpanID, bool) {}

func CopySpanID(dest, src *SpanID) {
	*dest = *src
}

// IsEmpty returns true if id contains at least one non-zero byte.
func (sid SpanID) IsEmpty() bool {
	return sid == [spanIDSize]byte{}
}

// SizeProto returns the size of the data to serialize in proto format.
func (sid SpanID) SizeProto() int {
	if sid.IsEmpty() {
		return 0
	}
	return spanIDSize
}

// MarshalProto converts span ID into a binary representation. Called by Protobuf serialization.
func (sid SpanID) MarshalProto(buf []byte) int {
	if sid.IsEmpty() {
		return 0
	}

	return copy(buf[len(buf)-spanIDSize:], sid[:])
}

// UnmarshalProto inflates this span ID from binary representation. Called by Protobuf serialization.
func (sid *SpanID) UnmarshalProto(data []byte) error {
	if len(data) == 0 {
		*sid = [spanIDSize]byte{}
		return nil
	}

	if len(data) != spanIDSize {
		return errUnmarshalSpanID
	}

	copy(sid[:], data)
	return nil
}

// MarshalJSON converts SpanID into a hex string.
//
//nolint:govet
func (sid SpanID) MarshalJSON(dest *json.Stream) {
	dest.WriteString(hex.EncodeToString(sid[:]))
}

// UnmarshalJSON decodes SpanID from hex string.
//
//nolint:govet
func (sid *SpanID) UnmarshalJSON(iter *json.Iterator) {
	*sid = [spanIDSize]byte{}
	unmarshalJSON(sid[:], iter)
}

func GenTestSpanID() *SpanID {
	sid := SpanID([8]byte{8, 7, 6, 5, 4, 3, 2, 1})
	return &sid
}
