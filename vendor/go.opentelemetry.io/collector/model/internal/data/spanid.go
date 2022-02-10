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

package data // import "go.opentelemetry.io/collector/model/internal/data"

import (
	"encoding/hex"
	"errors"
)

const spanIDSize = 8

var errInvalidSpanIDSize = errors.New("invalid length for SpanID")

// SpanID is a custom data type that is used for all span_id fields in OTLP
// Protobuf messages.
type SpanID struct {
	id [spanIDSize]byte
}

// NewSpanID creates a SpanID from a byte slice.
func NewSpanID(bytes [8]byte) SpanID {
	return SpanID{id: bytes}
}

// HexString returns hex representation of the ID.
func (sid SpanID) HexString() string {
	if sid.IsEmpty() {
		return ""
	}
	return hex.EncodeToString(sid.id[:])
}

// Size returns the size of the data to serialize.
func (sid *SpanID) Size() int {
	if sid.IsEmpty() {
		return 0
	}
	return spanIDSize
}

// Equal returns true if ids are equal.
func (sid SpanID) Equal(that SpanID) bool {
	return sid.id == that.id
}

// IsEmpty returns true if id contains at least one non-zero byte.
func (sid SpanID) IsEmpty() bool {
	return sid.id == [8]byte{}
}

// Bytes returns the byte array representation of the SpanID.
func (sid SpanID) Bytes() [8]byte {
	return sid.id
}

// MarshalTo converts trace ID into a binary representation. Called by Protobuf serialization.
func (sid *SpanID) MarshalTo(data []byte) (n int, err error) {
	if sid.IsEmpty() {
		return 0, nil
	}
	return marshalBytes(data, sid.id[:])
}

// Unmarshal inflates this trace ID from binary representation. Called by Protobuf serialization.
func (sid *SpanID) Unmarshal(data []byte) error {
	if len(data) == 0 {
		sid.id = [8]byte{}
		return nil
	}

	if len(data) != spanIDSize {
		return errInvalidSpanIDSize
	}

	copy(sid.id[:], data)
	return nil
}

// MarshalJSON converts SpanID into a hex string enclosed in quotes.
func (sid SpanID) MarshalJSON() ([]byte, error) {
	if sid.IsEmpty() {
		return []byte(`""`), nil
	}
	return marshalJSON(sid.id[:])
}

// UnmarshalJSON decodes SpanID from hex string, possibly enclosed in quotes.
// Called by Protobuf JSON deserialization.
func (sid *SpanID) UnmarshalJSON(data []byte) error {
	sid.id = [8]byte{}
	return unmarshalJSON(sid.id[:], data)
}
