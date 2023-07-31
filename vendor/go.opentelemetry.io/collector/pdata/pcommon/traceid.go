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

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

import (
	"encoding/hex"

	"go.opentelemetry.io/collector/pdata/internal/data"
)

var emptyTraceID = TraceID([16]byte{})

// TraceID is a trace identifier.
type TraceID [16]byte

// NewTraceIDEmpty returns a new empty (all zero bytes) TraceID.
func NewTraceIDEmpty() TraceID {
	return emptyTraceID
}

// String returns string representation of the TraceID.
//
// Important: Don't rely on this method to get a string identifier of TraceID.
// Use hex.EncodeToString explicitly instead.
// This method meant to implement Stringer interface for display purposes only.
func (ms TraceID) String() string {
	if ms.IsEmpty() {
		return ""
	}
	return hex.EncodeToString(ms[:])
}

// Deprecated: [0.65.0] Call hex.EncodeToString explicitly instead.
func (ms TraceID) HexString() string {
	if ms.IsEmpty() {
		return ""
	}
	return hex.EncodeToString(ms[:])
}

// IsEmpty returns true if id doesn't contain at least one non-zero byte.
func (ms TraceID) IsEmpty() bool {
	return data.TraceID(ms).IsEmpty()
}
