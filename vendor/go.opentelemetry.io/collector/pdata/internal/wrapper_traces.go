// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// TracesToProto internal helper to convert Traces to protobuf representation.
func TracesToProto(l TracesWrapper) TracesData {
	return TracesData{
		ResourceSpans: l.orig.ResourceSpans,
	}
}

// TracesFromProto internal helper to convert protobuf representation to Traces.
// This function set exclusive state assuming that it's called only once per Traces.
func TracesFromProto(orig TracesData) TracesWrapper {
	return NewTracesWrapper(&ExportTraceServiceRequest{
		ResourceSpans: orig.ResourceSpans,
	}, NewState())
}
