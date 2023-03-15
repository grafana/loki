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

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

// Marshaler marshals pmetric.Metrics into bytes.
type Marshaler interface {
	// MarshalMetrics the given pmetric.Metrics into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalMetrics(md Metrics) ([]byte, error)
}

// Unmarshaler unmarshalls bytes into pmetric.Metrics.
type Unmarshaler interface {
	// UnmarshalMetrics the given bytes into pmetric.Metrics.
	// If the error is not nil, the returned pmetric.Metrics cannot be used.
	UnmarshalMetrics(buf []byte) (Metrics, error)
}

// Sizer is an optional interface implemented by the Marshaler, that calculates the size of a marshaled Metrics.
type Sizer interface {
	// MetricsSize returns the size in bytes of a marshaled Metrics.
	MetricsSize(md Metrics) int
}
