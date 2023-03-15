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

package pdata // import "go.opentelemetry.io/collector/model/pdata"

// This file contains aliases for trace data structures.

import "go.opentelemetry.io/collector/pdata/ptrace"

// TracesMarshaler is an alias for ptrace.Marshaler interface.
// Deprecated: [v0.49.0] Use ptrace.Marshaler instead.
type TracesMarshaler = ptrace.Marshaler

// TracesUnmarshaler is an alias for ptrace.Unmarshaler interface.
// Deprecated: [v0.49.0] Use ptrace.Unmarshaler instead.
type TracesUnmarshaler = ptrace.Unmarshaler

// TracesSizer is an alias for ptrace.Sizer interface.
// Deprecated: [v0.49.0] Use ptrace.Sizer instead.
type TracesSizer = ptrace.Sizer

// Traces is an alias for ptrace.Traces struct.
// Deprecated: [v0.49.0] Use ptrace.Traces instead.
type Traces = ptrace.Traces

// NewTraces is an alias for a function to create new Traces.
// Deprecated: [v0.49.0] Use ptrace.NewTraces instead.
var NewTraces = ptrace.NewTraces

// TraceState is an alias for ptrace.TraceState type.
// Deprecated: [v0.49.0] Use ptrace.TraceState instead.
type TraceState = ptrace.TraceState

const (
	TraceStateEmpty = ptrace.TraceStateEmpty
)

// SpanKind is an alias for ptrace.SpanKind type.
// Deprecated: [v0.49.0] Use ptrace.SpanKind instead.
type SpanKind = ptrace.SpanKind

const (

	// Deprecated: [v0.49.0] Use ptrace.SpanKindUnspecified instead.
	SpanKindUnspecified = ptrace.SpanKindUnspecified

	// Deprecated: [v0.49.0] Use ptrace.SpanKindInternal instead.
	SpanKindInternal = ptrace.SpanKindInternal

	// Deprecated: [v0.49.0] Use ptrace.SpanKindServer instead.
	SpanKindServer = ptrace.SpanKindServer

	// Deprecated: [v0.49.0] Use ptrace.SpanKindClient instead.
	SpanKindClient = ptrace.SpanKindClient

	// Deprecated: [v0.49.0] Use ptrace.SpanKindProducer instead.
	SpanKindProducer = ptrace.SpanKindProducer

	// Deprecated: [v0.49.0] Use ptrace.SpanKindConsumer instead.
	SpanKindConsumer = ptrace.SpanKindConsumer
)

// StatusCode is an alias for ptrace.StatusCode type.
// Deprecated: [v0.49.0] Use ptrace.StatusCode instead.
type StatusCode = ptrace.StatusCode

const (

	// Deprecated: [v0.49.0] Use ptrace.StatusCodeUnset instead.
	StatusCodeUnset = ptrace.StatusCodeUnset

	// Deprecated: [v0.49.0] Use ptrace.StatusCodeOk instead.
	StatusCodeOk = ptrace.StatusCodeOk

	// Deprecated: [v0.49.0] Use ptrace.StatusCodeError instead.
	StatusCodeError = ptrace.StatusCodeError
)
