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

package data // import "go.opentelemetry.io/collector/pdata/internal/data"

import (
	"errors"

	"github.com/gogo/protobuf/proto"
)

const spanIDSize = 8

var (
	errMarshalSpanID   = errors.New("marshal: invalid buffer length for SpanID")
	errUnmarshalSpanID = errors.New("unmarshal: invalid SpanID length")
)

// SpanID is a custom data type that is used for all span_id fields in OTLP
// Protobuf messages.
type SpanID [spanIDSize]byte

var _ proto.Sizer = (*SpanID)(nil)

// Size returns the size of the data to serialize.
func (sid SpanID) Size() int {
	if sid.IsEmpty() {
		return 0
	}
	return spanIDSize
}

// IsEmpty returns true if id contains at least one non-zero byte.
func (sid SpanID) IsEmpty() bool {
	return sid == [spanIDSize]byte{}
}

// MarshalTo converts trace ID into a binary representation. Called by Protobuf serialization.
func (sid SpanID) MarshalTo(data []byte) (n int, err error) {
	if sid.IsEmpty() {
		return 0, nil
	}

	if len(data) < spanIDSize {
		return 0, errMarshalSpanID
	}

	return copy(data, sid[:]), nil
}

// Unmarshal inflates this trace ID from binary representation. Called by Protobuf serialization.
func (sid *SpanID) Unmarshal(data []byte) error {
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

// MarshalJSON converts SpanID into a hex string enclosed in quotes.
func (sid SpanID) MarshalJSON() ([]byte, error) {
	if sid.IsEmpty() {
		return []byte(`""`), nil
	}
	return marshalJSON(sid[:])
}

// UnmarshalJSON decodes SpanID from hex string, possibly enclosed in quotes.
// Called by Protobuf JSON deserialization.
func (sid *SpanID) UnmarshalJSON(data []byte) error {
	*sid = [spanIDSize]byte{}
	return unmarshalJSON(sid[:], data)
}
