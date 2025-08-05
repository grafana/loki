// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals pdata.Logs to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalLogs to the OTLP/JSON format.
func (*JSONMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	ld.marshalJSONStream(dest)
	return slices.Clone(dest.Buffer()), dest.Error()
}

var _ Unmarshaler = (*JSONUnmarshaler)(nil)

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to pdata.Logs.
type JSONUnmarshaler struct{}

// UnmarshalLogs from OTLP/JSON format into pdata.Logs.
func (*JSONUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	ld := NewLogs()
	ld.unmarshalJSONIter(iter)
	if iter.Error() != nil {
		return Logs{}, iter.Error()
	}
	otlp.MigrateLogs(ld.getOrig().ResourceLogs)
	return ld, nil
}

func (ms ResourceLogs) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "resource":
			internal.UnmarshalJSONIterResource(internal.NewResource(&ms.orig.Resource, ms.state), iter)
		case "scope_logs", "scopeLogs":
			ms.ScopeLogs().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ScopeLogs) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "scope":
			internal.UnmarshalJSONIterInstrumentationScope(internal.NewInstrumentationScope(&ms.orig.Scope, ms.state), iter)
		case "log_records", "logRecords":
			ms.LogRecords().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms LogRecord) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "observed_time_unix_nano", "observedTimeUnixNano":
			ms.orig.ObservedTimeUnixNano = iter.ReadUint64()
		case "severity_number", "severityNumber":
			ms.orig.SeverityNumber = otlplogs.SeverityNumber(iter.ReadEnumValue(otlplogs.SeverityNumber_value))
		case "severity_text", "severityText":
			ms.orig.SeverityText = iter.ReadString()
		case "event_name", "eventName":
			ms.orig.EventName = iter.ReadString()
		case "body":
			internal.UnmarshalJSONIterValue(internal.NewValue(&ms.orig.Body, ms.state), iter)
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = iter.ReadUint32()
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		case "traceId", "trace_id":
			ms.orig.TraceId.UnmarshalJSONIter(iter)
		case "spanId", "span_id":
			ms.orig.SpanId.UnmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}
