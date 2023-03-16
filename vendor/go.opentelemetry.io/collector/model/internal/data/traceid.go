// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package data

import (
	"encoding/hex"
	"errors"
)

const traceIDSize = 16

var errInvalidTraceIDSize = errors.New("invalid length for SpanID")

// TraceID is a custom data type that is used for all trace_id fields in OTLP
// Protobuf messages.
type TraceID struct {
	id [traceIDSize]byte
}

// NewTraceID creates a TraceID from a byte slice.
func NewTraceID(bytes [16]byte) TraceID {
	return TraceID{
		id: bytes,
	}
}

// HexString returns hex representation of the ID.
func (tid TraceID) HexString() string {
	if tid.IsEmpty() {
		return ""
	}
	return hex.EncodeToString(tid.id[:])
}

// Size returns the size of the data to serialize.
func (tid *TraceID) Size() int {
	if tid.IsEmpty() {
		return 0
	}
	return traceIDSize
}

// Equal returns true if ids are equal.
func (tid TraceID) Equal(that TraceID) bool {
	return tid.id == that.id
}

// IsEmpty returns true if id contains at leas one non-zero byte.
func (tid TraceID) IsEmpty() bool {
	return tid.id == [16]byte{}
}

// Bytes returns the byte array representation of the TraceID.
func (tid TraceID) Bytes() [16]byte {
	return tid.id
}

// MarshalTo converts trace ID into a binary representation. Called by Protobuf serialization.
func (tid *TraceID) MarshalTo(data []byte) (n int, err error) {
	if tid.IsEmpty() {
		return 0, nil
	}
	return marshalBytes(data, tid.id[:])
}

// Unmarshal inflates this trace ID from binary representation. Called by Protobuf serialization.
func (tid *TraceID) Unmarshal(data []byte) error {
	if len(data) == 0 {
		tid.id = [16]byte{}
		return nil
	}

	if len(data) != traceIDSize {
		return errInvalidTraceIDSize
	}

	copy(tid.id[:], data)
	return nil
}

// MarshalJSON converts trace id into a hex string enclosed in quotes.
func (tid TraceID) MarshalJSON() ([]byte, error) {
	if tid.IsEmpty() {
		return []byte(`""`), nil
	}
	return marshalJSON(tid.id[:])
}

// UnmarshalJSON inflates trace id from hex string, possibly enclosed in quotes.
// Called by Protobuf JSON deserialization.
func (tid *TraceID) UnmarshalJSON(data []byte) error {
	tid.id = [16]byte{}
	return unmarshalJSON(tid.id[:], data)
}
