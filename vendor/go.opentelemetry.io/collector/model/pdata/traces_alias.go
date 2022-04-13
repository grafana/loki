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

import (
	"go.opentelemetry.io/collector/model/internal/pdata"
)

// TracesMarshaler is an alias for pdata.TracesMarshaler interface.
type TracesMarshaler = pdata.TracesMarshaler

// TracesUnmarshaler is an alias for pdata.TracesUnmarshaler interface.
type TracesUnmarshaler = pdata.TracesUnmarshaler

// TracesSizer is an alias for pdata.TracesSizer interface.
type TracesSizer = pdata.TracesSizer

// Traces is an alias for pdata.Traces struct.
type Traces = pdata.Traces

// NewTraces is an alias for a function to create new Traces.
var NewTraces = pdata.NewTraces

// TraceState is an alias for pdata.TraceState type.
type TraceState = pdata.TraceState

const (
	TraceStateEmpty = pdata.TraceStateEmpty
)

// SpanKind is an alias for pdata.SpanKind type.
type SpanKind = pdata.SpanKind

const (
	SpanKindUnspecified = pdata.SpanKindUnspecified
	SpanKindInternal    = pdata.SpanKindInternal
	SpanKindServer      = pdata.SpanKindServer
	SpanKindClient      = pdata.SpanKindClient
	SpanKindProducer    = pdata.SpanKindProducer
	SpanKindConsumer    = pdata.SpanKindConsumer
)

// StatusCode is an alias for pdata.StatusCode type.
type StatusCode = pdata.StatusCode

const (
	StatusCodeUnset = pdata.StatusCodeUnset
	StatusCodeOk    = pdata.StatusCodeOk
	StatusCodeError = pdata.StatusCodeError
)

// Deprecated: [v0.48.0] Use ScopeSpansSlice instead.
type InstrumentationLibrarySpansSlice = pdata.ScopeSpansSlice

// Deprecated: [v0.48.0] Use NewScopeSpansSlice instead.
var NewInstrumentationLibrarySpansSlice = pdata.NewScopeSpansSlice

// Deprecated: [v0.48.0] Use ScopeSpans instead.
type InstrumentationLibrarySpans = pdata.ScopeSpans

// Deprecated: [v0.48.0] Use NewScopeSpans instead.
var NewInstrumentationLibrarySpans = pdata.NewScopeSpans
