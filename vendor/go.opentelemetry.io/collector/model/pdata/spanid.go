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

package pdata

import (
	"go.opentelemetry.io/collector/model/internal/data"
)

// SpanID is an alias of OTLP SpanID data type.
type SpanID struct {
	orig data.SpanID
}

// InvalidSpanID returns an empty (all zero bytes) SpanID.
func InvalidSpanID() SpanID {
	return SpanID{orig: data.NewSpanID([8]byte{})}
}

// NewSpanID returns a new SpanID from the given byte array.
func NewSpanID(bytes [8]byte) SpanID {
	return SpanID{orig: data.NewSpanID(bytes)}
}

// Bytes returns the byte array representation of the SpanID.
func (t SpanID) Bytes() [8]byte {
	return t.orig.Bytes()
}

// HexString returns hex representation of the SpanID.
func (t SpanID) HexString() string {
	return t.orig.HexString()
}

// IsEmpty returns true if id doesn't contain at least one non-zero byte.
func (t SpanID) IsEmpty() bool {
	return t.orig.IsEmpty()
}
