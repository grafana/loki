// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	otlptrace "go.opentelemetry.io/collector/pdata/internal/data/protogen/trace/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals pdata.Traces to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalTraces to the OTLP/JSON format.
func (*JSONMarshaler) MarshalTraces(td Traces) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	td.marshalJSONStream(dest)
	return slices.Clone(dest.Buffer()), dest.Error()
}

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to pdata.Traces.
type JSONUnmarshaler struct{}

// UnmarshalTraces from OTLP/JSON format into pdata.Traces.
func (*JSONUnmarshaler) UnmarshalTraces(buf []byte) (Traces, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	td := NewTraces()
	td.unmarshalJSONIter(iter)
	if iter.Error() != nil {
		return Traces{}, iter.Error()
	}
	otlp.MigrateTraces(td.getOrig().ResourceSpans)
	return td, nil
}

func (ms ResourceSpans) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "resource":
			internal.UnmarshalJSONIterResource(internal.NewResource(&ms.orig.Resource, ms.state), iter)
		case "scopeSpans", "scope_spans":
			ms.ScopeSpans().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ScopeSpans) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "scope":
			internal.UnmarshalJSONIterInstrumentationScope(internal.NewInstrumentationScope(&ms.orig.Scope, ms.state), iter)
		case "spans":
			ms.Spans().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Span) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "traceId", "trace_id":
			ms.orig.TraceId.UnmarshalJSONIter(iter)
		case "spanId", "span_id":
			ms.orig.SpanId.UnmarshalJSONIter(iter)
		case "traceState", "trace_state":
			ms.TraceState().FromRaw(iter.ReadString())
		case "parentSpanId", "parent_span_id":
			ms.orig.ParentSpanId.UnmarshalJSONIter(iter)
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		case "name":
			ms.orig.Name = iter.ReadString()
		case "kind":
			ms.orig.Kind = otlptrace.Span_SpanKind(iter.ReadEnumValue(otlptrace.Span_SpanKind_value))
		case "startTimeUnixNano", "start_time_unix_nano":
			ms.orig.StartTimeUnixNano = iter.ReadUint64()
		case "endTimeUnixNano", "end_time_unix_nano":
			ms.orig.EndTimeUnixNano = iter.ReadUint64()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = iter.ReadUint32()
		case "events":
			ms.Events().unmarshalJSONIter(iter)
		case "droppedEventsCount", "dropped_events_count":
			ms.orig.DroppedEventsCount = iter.ReadUint32()
		case "links":
			ms.Links().unmarshalJSONIter(iter)
		case "droppedLinksCount", "dropped_links_count":
			ms.orig.DroppedLinksCount = iter.ReadUint32()
		case "status":
			ms.Status().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Status) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "message":
			ms.orig.Message = iter.ReadString()
		case "code":
			ms.orig.Code = otlptrace.Status_StatusCode(iter.ReadEnumValue(otlptrace.Status_StatusCode_value))
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms SpanLink) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "traceId", "trace_id":
			ms.orig.TraceId.UnmarshalJSONIter(iter)
		case "spanId", "span_id":
			ms.orig.SpanId.UnmarshalJSONIter(iter)
		case "traceState", "trace_state":
			ms.orig.TraceState = iter.ReadString()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = iter.ReadUint32()
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms SpanEvent) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "name":
			ms.orig.Name = iter.ReadString()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = iter.ReadUint32()
		default:
			iter.Skip()
		}
		return true
	})
}
