// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"encoding/hex"
	"errors"

	"go.opentelemetry.io/collector/pdata/internal/json"
)

const traceIDSize = 16

var errUnmarshalTraceID = errors.New("unmarshal: invalid TraceID length")

// TraceID is a custom data type that is used for all trace_id fields in OTLP
// Protobuf messages.
type TraceID [traceIDSize]byte

func DeleteTraceID(*TraceID, bool) {}

func CopyTraceID(dest, src *TraceID) {
	*dest = *src
}

// IsEmpty returns true if id contains at leas one non-zero byte.
func (tid TraceID) IsEmpty() bool {
	return tid == [traceIDSize]byte{}
}

// SizeProto returns the size of the data to serialize in proto format.
func (tid TraceID) SizeProto() int {
	if tid.IsEmpty() {
		return 0
	}

	return traceIDSize
}

// MarshalProto converts trace ID into a binary representation. Called by Protobuf serialization.
func (tid TraceID) MarshalProto(buf []byte) int {
	if tid.IsEmpty() {
		return 0
	}

	return copy(buf[len(buf)-traceIDSize:], tid[:])
}

// UnmarshalProto inflates this trace ID from binary representation. Called by Protobuf serialization.
func (tid *TraceID) UnmarshalProto(buf []byte) error {
	if len(buf) == 0 {
		*tid = [traceIDSize]byte{}
		return nil
	}

	if len(buf) != traceIDSize {
		return errUnmarshalTraceID
	}

	copy(tid[:], buf)
	return nil
}

// MarshalJSON converts TraceID into a hex string.
//
//nolint:govet
func (tid TraceID) MarshalJSON(dest *json.Stream) {
	dest.WriteString(hex.EncodeToString(tid[:]))
}

// UnmarshalJSON decodes TraceID from hex string.
//
//nolint:govet
func (tid *TraceID) UnmarshalJSON(iter *json.Iterator) {
	*tid = [profileIDSize]byte{}
	unmarshalJSON(tid[:], iter)
}

func GenTestTraceID() *TraceID {
	tid := TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1})
	return &tid
}
