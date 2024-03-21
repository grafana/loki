// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"bytes"
	"fmt"

	jsoniter "github.com/json-iterator/go"

	"go.opentelemetry.io/collector/pdata/internal"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals pdata.Logs to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalLogs to the OTLP/JSON format.
func (*JSONMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	buf := bytes.Buffer{}
	pb := internal.LogsToProto(internal.Logs(ld))
	err := json.Marshal(&buf, &pb)
	return buf.Bytes(), err
}

var _ Unmarshaler = (*JSONUnmarshaler)(nil)

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to pdata.Logs.
type JSONUnmarshaler struct{}

// UnmarshalLogs from OTLP/JSON format into pdata.Logs.
func (*JSONUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	iter := jsoniter.ConfigFastest.BorrowIterator(buf)
	defer jsoniter.ConfigFastest.ReturnIterator(iter)
	ld := NewLogs()
	ld.unmarshalJsoniter(iter)
	if iter.Error != nil {
		return Logs{}, iter.Error
	}
	otlp.MigrateLogs(ld.getOrig().ResourceLogs)
	return ld, nil
}

func (ms Logs) unmarshalJsoniter(iter *jsoniter.Iterator) {
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "resource_logs", "resourceLogs":
			iter.ReadArrayCB(func(*jsoniter.Iterator) bool {
				ms.ResourceLogs().AppendEmpty().unmarshalJsoniter(iter)
				return true
			})
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ResourceLogs) unmarshalJsoniter(iter *jsoniter.Iterator) {
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "resource":
			json.ReadResource(iter, &ms.orig.Resource)
		case "scope_logs", "scopeLogs":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				ms.ScopeLogs().AppendEmpty().unmarshalJsoniter(iter)
				return true
			})
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ScopeLogs) unmarshalJsoniter(iter *jsoniter.Iterator) {
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "scope":
			json.ReadScope(iter, &ms.orig.Scope)
		case "log_records", "logRecords":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				ms.LogRecords().AppendEmpty().unmarshalJsoniter(iter)
				return true
			})
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms LogRecord) unmarshalJsoniter(iter *jsoniter.Iterator) {
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = json.ReadUint64(iter)
		case "observed_time_unix_nano", "observedTimeUnixNano":
			ms.orig.ObservedTimeUnixNano = json.ReadUint64(iter)
		case "severity_number", "severityNumber":
			ms.orig.SeverityNumber = otlplogs.SeverityNumber(json.ReadEnumValue(iter, otlplogs.SeverityNumber_value))
		case "severity_text", "severityText":
			ms.orig.SeverityText = iter.ReadString()
		case "body":
			json.ReadValue(iter, &ms.orig.Body)
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				ms.orig.Attributes = append(ms.orig.Attributes, json.ReadAttribute(iter))
				return true
			})
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = json.ReadUint32(iter)
		case "flags":
			ms.orig.Flags = json.ReadUint32(iter)
		case "traceId", "trace_id":
			if err := ms.orig.TraceId.UnmarshalJSON([]byte(iter.ReadString())); err != nil {
				iter.ReportError("readLog.traceId", fmt.Sprintf("parse trace_id:%v", err))
			}
		case "spanId", "span_id":
			if err := ms.orig.SpanId.UnmarshalJSON([]byte(iter.ReadString())); err != nil {
				iter.ReportError("readLog.spanId", fmt.Sprintf("parse span_id:%v", err))
			}
		default:
			iter.Skip()
		}
		return true
	})
}
