// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// SpanKind is the type of span. Can be used to specify additional relationships between spans
// in addition to a parent/child relationship.
type SpanKind int32

const (
	// SpanKindUnspecified represents that the SpanKind is unspecified, it MUST NOT be used.
	SpanKindUnspecified = SpanKind(internal.SpanKind_SPAN_KIND_UNSPECIFIED)
	// SpanKindInternal indicates that the span represents an internal operation within an application,
	// as opposed to an operation happening at the boundaries. Default value.
	SpanKindInternal = SpanKind(internal.SpanKind_SPAN_KIND_INTERNAL)
	// SpanKindServer indicates that the span covers server-side handling of an RPC or other
	// remote network request.
	SpanKindServer = SpanKind(internal.SpanKind_SPAN_KIND_SERVER)
	// SpanKindClient indicates that the span describes a request to some remote service.
	SpanKindClient = SpanKind(internal.SpanKind_SPAN_KIND_CLIENT)
	// SpanKindProducer indicates that the span describes a producer sending a message to a broker.
	// Unlike CLIENT and SERVER, there is often no direct critical path latency relationship
	// between producer and consumer spans.
	// A PRODUCER span ends when the message was accepted by the broker while the logical processing of
	// the message might span a much longer time.
	SpanKindProducer = SpanKind(internal.SpanKind_SPAN_KIND_PRODUCER)
	// SpanKindConsumer indicates that the span describes consumer receiving a message from a broker.
	// Like the PRODUCER kind, there is often no direct critical path latency relationship between
	// producer and consumer spans.
	SpanKindConsumer = SpanKind(internal.SpanKind_SPAN_KIND_CONSUMER)
)

// String returns the string representation of the SpanKind.
func (sk SpanKind) String() string {
	switch sk {
	case SpanKindUnspecified:
		return "Unspecified"
	case SpanKindInternal:
		return "Internal"
	case SpanKindServer:
		return "Server"
	case SpanKindClient:
		return "Client"
	case SpanKindProducer:
		return "Producer"
	case SpanKindConsumer:
		return "Consumer"
	}
	return ""
}
