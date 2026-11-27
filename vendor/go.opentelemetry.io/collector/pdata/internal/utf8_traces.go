// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// SanitizeUTF8ExportTraceServiceRequest replaces invalid UTF-8 byte sequences in every string field of req
// with the Unicode replacement character U+FFFD.
func SanitizeUTF8ExportTraceServiceRequest(req *ExportTraceServiceRequest) {
	for _, rs := range req.ResourceSpans {
		SanitizeUTF8Resource(&rs.Resource)
		rs.SchemaUrl = SanitizeUTF8String(rs.SchemaUrl)
		sanitizeUTF8ScopeSpansSlice(rs.ScopeSpans)
		// DeprecatedScopeSpans is normally emptied by otlp.MigrateTraces, but JSON unmarshalling can still
		// populate it, so it is walked as well.
		sanitizeUTF8ScopeSpansSlice(rs.DeprecatedScopeSpans)
	}
}

func sanitizeUTF8ScopeSpansSlice(sss []*ScopeSpans) {
	for _, ss := range sss {
		SanitizeUTF8InstrumentationScope(&ss.Scope)
		ss.SchemaUrl = SanitizeUTF8String(ss.SchemaUrl)
		for _, span := range ss.Spans {
			sanitizeUTF8Span(span)
		}
	}
}

func sanitizeUTF8Span(span *Span) {
	span.Name = SanitizeUTF8String(span.Name)
	span.TraceState = SanitizeUTF8String(span.TraceState)
	SanitizeUTF8KeyValueSlice(span.Attributes)
	for _, ev := range span.Events {
		ev.Name = SanitizeUTF8String(ev.Name)
		SanitizeUTF8KeyValueSlice(ev.Attributes)
	}
	for _, link := range span.Links {
		link.TraceState = SanitizeUTF8String(link.TraceState)
		SanitizeUTF8KeyValueSlice(link.Attributes)
	}
	span.Status.Message = SanitizeUTF8String(span.Status.Message)
}
