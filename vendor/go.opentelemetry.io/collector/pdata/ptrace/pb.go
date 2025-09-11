// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

func (e *ProtoMarshaler) MarshalTraces(td Traces) ([]byte, error) {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return td.getOrig().Marshal()
	}
	size := internal.SizeProtoOrigExportTraceServiceRequest(td.getOrig())
	buf := make([]byte, size)
	_ = internal.MarshalProtoOrigExportTraceServiceRequest(td.getOrig(), buf)
	return buf, nil
}

func (e *ProtoMarshaler) TracesSize(td Traces) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return td.getOrig().Size()
	}
	return internal.SizeProtoOrigExportTraceServiceRequest(td.getOrig())
}

func (e *ProtoMarshaler) ResourceSpansSize(td ResourceSpans) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return td.orig.Size()
	}
	return internal.SizeProtoOrigResourceSpans(td.orig)
}

func (e *ProtoMarshaler) ScopeSpansSize(td ScopeSpans) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return td.orig.Size()
	}
	return internal.SizeProtoOrigScopeSpans(td.orig)
}

func (e *ProtoMarshaler) SpanSize(td Span) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return td.orig.Size()
	}
	return internal.SizeProtoOrigSpan(td.orig)
}

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalTraces(buf []byte) (Traces, error) {
	td := NewTraces()
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		err := td.getOrig().Unmarshal(buf)
		if err != nil {
			return Traces{}, err
		}
		return td, nil
	}
	err := internal.UnmarshalProtoOrigExportTraceServiceRequest(td.getOrig(), buf)
	if err != nil {
		return Traces{}, err
	}
	return td, nil
}
