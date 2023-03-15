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

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

// Marshaler marshals pdata.Traces into bytes.
type Marshaler interface {
	// MarshalTraces the given pdata.Traces into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalTraces(td Traces) ([]byte, error)
}

// Unmarshaler unmarshalls bytes into pdata.Traces.
type Unmarshaler interface {
	// UnmarshalTraces the given bytes into pdata.Traces.
	// If the error is not nil, the returned pdata.Traces cannot be used.
	UnmarshalTraces(buf []byte) (Traces, error)
}

// Sizer is an optional interface implemented by the Marshaler,
// that calculates the size of a marshaled Traces.
type Sizer interface {
	// TracesSize returns the size in bytes of a marshaled Traces.
	TracesSize(td Traces) int
}
