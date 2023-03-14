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

import "go.opentelemetry.io/collector/pdata/internal"

// This file contains aliases for trace data structures.

// Traces is the top-level struct that is propagated through the traces pipeline.
// Use NewTraces to create new instance, zero-initialized instance is not valid for use.
type Traces = internal.Traces

// NewTraces creates a new Traces struct.
var NewTraces = internal.NewTraces

// TraceState is a string representing the tracestate in w3c-trace-context format: https://www.w3.org/TR/trace-context/#tracestate-header
type TraceState = internal.TraceState

const (
	// TraceStateEmpty represents the empty TraceState.
	TraceStateEmpty = internal.TraceStateEmpty
)

// SpanKind is the type of span. Can be used to specify additional relationships between spans
// in addition to a parent/child relationship.
type SpanKind = internal.SpanKind

const (
	// SpanKindUnspecified represents that the SpanKind is unspecified, it MUST NOT be used.
	SpanKindUnspecified = internal.SpanKindUnspecified

	// SpanKindInternal indicates that the span represents an internal operation within an application,
	// as opposed to an operation happening at the boundaries. Default value.
	SpanKindInternal = internal.SpanKindInternal

	// SpanKindServer indicates that the span covers server-side handling of an RPC or other
	// remote network request.
	SpanKindServer = internal.SpanKindServer

	// SpanKindProducer indicates that the span describes a producer sending a message to a broker.
	// Unlike CLIENT and SERVER, there is often no direct critical path latency relationship
	// between producer and consumer spans.
	// A PRODUCER span ends when the message was accepted by the broker while the logical processing of
	// the message might span a much longer time.
	SpanKindClient = internal.SpanKindClient

	// SpanKindProducer indicates that the span describes a producer sending a message to a broker.
	// Unlike CLIENT and SERVER, there is often no direct critical path latency relationship
	// between producer and consumer spans.
	// A PRODUCER span ends when the message was accepted by the broker while the logical processing of
	// the message might span a much longer time.
	SpanKindProducer = internal.SpanKindProducer

	// SpanKindConsumer indicates that the span describes consumer receiving a message from a broker.
	// Like the PRODUCER kind, there is often no direct critical path latency relationship between
	// producer and consumer spans.
	SpanKindConsumer = internal.SpanKindConsumer
)

// StatusCode mirrors the codes defined at
// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/api.md#set-status
type StatusCode = internal.StatusCode

const (
	StatusCodeUnset = internal.StatusCodeUnset
	StatusCodeOk    = internal.StatusCodeOk
	StatusCodeError = internal.StatusCodeError
)
