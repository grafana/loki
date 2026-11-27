// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plogotlp // import "go.opentelemetry.io/collector/pdata/plog/plogotlp"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
	"go.opentelemetry.io/collector/pdata/plog"
)

// ExportRequest represents the request for gRPC/HTTP client/server.
// It's a wrapper for plog.Logs data.
type ExportRequest struct {
	orig  *internal.ExportLogsServiceRequest
	state *internal.State
}

// NewExportRequest returns an empty ExportRequest.
func NewExportRequest() ExportRequest {
	return ExportRequest{
		orig:  &internal.ExportLogsServiceRequest{},
		state: internal.NewState(),
	}
}

// NewExportRequestFromLogs returns a ExportRequest from plog.Logs.
// Because ExportRequest is a wrapper for plog.Logs,
// any changes to the provided Logs struct will be reflected in the ExportRequest and vice versa.
func NewExportRequestFromLogs(ld plog.Logs) ExportRequest {
	return ExportRequest{
		orig:  internal.GetLogsOrig(internal.LogsWrapper(ld)),
		state: internal.GetLogsState(internal.LogsWrapper(ld)),
	}
}

// MarshalProto marshals ExportRequest into proto bytes.
func (ms ExportRequest) MarshalProto() ([]byte, error) {
	size := ms.orig.SizeProto()
	buf := make([]byte, size)
	_ = ms.orig.MarshalProto(buf)
	return buf, nil
}

// UnmarshalProto unmarshalls ExportRequest from proto bytes.
func (ms ExportRequest) UnmarshalProto(data []byte) error {
	err := ms.orig.UnmarshalProto(data)
	if err != nil {
		return err
	}
	otlp.MigrateLogs(ms.orig.ResourceLogs)
	return nil
}

// MarshalJSON marshals ExportRequest into JSON bytes.
func (ms ExportRequest) MarshalJSON() ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	ms.orig.MarshalJSON(dest)
	if dest.Error() != nil {
		return nil, dest.Error()
	}
	return slices.Clone(dest.Buffer()), nil
}

// UnmarshalJSON unmarshalls ExportRequest from JSON bytes.
func (ms ExportRequest) UnmarshalJSON(data []byte) error {
	iter := json.BorrowIterator(data)
	defer json.ReturnIterator(iter)
	ms.orig.UnmarshalJSON(iter)
	return iter.Error()
}

// ValidateUTF8 returns false when any string in the request contains invalid UTF-8.
func (ms ExportRequest) ValidateUTF8() bool {
	return internal.ValidateUTF8(ms.orig)
}

// RejectInvalidUTF8 removes log records containing invalid UTF-8 and returns the number removed.
func (ms ExportRequest) RejectInvalidUTF8() int {
	ld := ms.Logs()
	rejected := 0
	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		if !internal.ValidateUTF8(rl.Resource()) || !internal.ValidateUTF8(rl.SchemaUrl()) {
			rejected += countResourceLogs(rl)
			return true
		}
		if rl.ScopeLogs().Len() == 0 {
			return false
		}
		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			if !internal.ValidateUTF8(sl.Scope()) || !internal.ValidateUTF8(sl.SchemaUrl()) {
				rejected += sl.LogRecords().Len()
				return true
			}
			if sl.LogRecords().Len() == 0 {
				return false
			}
			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				invalid := !internal.ValidateUTF8(lr)
				if invalid {
					rejected++
				}
				return invalid
			})
			return sl.LogRecords().Len() == 0
		})
		return rl.ScopeLogs().Len() == 0
	})
	return rejected
}

func countResourceLogs(rl plog.ResourceLogs) int {
	count := 0
	for i := 0; i < rl.ScopeLogs().Len(); i++ {
		count += rl.ScopeLogs().At(i).LogRecords().Len()
	}
	return count
}

func (ms ExportRequest) Logs() plog.Logs {
	return plog.Logs(internal.NewLogsWrapper(ms.orig, ms.state))
}

// SanitizeUTF8 replaces every run of invalid UTF-8 bytes in the string fields of the request with the
// Unicode replacement character U+FFFD, so the request only contains valid UTF-8 strings afterwards.
// Attribute keys and values, nested map and slice values, names, descriptions and other string fields
// are all covered. Bytes values and binary identifiers (trace and span IDs) are not modified.
func (ms ExportRequest) SanitizeUTF8() {
	ms.state.AssertMutable()
	internal.SanitizeUTF8ExportLogsServiceRequest(ms.orig)
}
