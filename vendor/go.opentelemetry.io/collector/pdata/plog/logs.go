// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectorlog "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/logs/v1"
)

// Logs is the top-level struct that is propagated through the logs pipeline.
// Use NewLogs to create new instance, zero-initialized instance is not valid for use.
type Logs internal.Logs

func newLogs(orig *otlpcollectorlog.ExportLogsServiceRequest) Logs {
	state := internal.StateMutable
	return Logs(internal.NewLogs(orig, &state))
}

func (ms Logs) getOrig() *otlpcollectorlog.ExportLogsServiceRequest {
	return internal.GetOrigLogs(internal.Logs(ms))
}

func (ms Logs) getState() *internal.State {
	return internal.GetLogsState(internal.Logs(ms))
}

// NewLogs creates a new Logs struct.
func NewLogs() Logs {
	return newLogs(&otlpcollectorlog.ExportLogsServiceRequest{})
}

// IsReadOnly returns true if this Logs instance is read-only.
func (ms Logs) IsReadOnly() bool {
	return *ms.getState() == internal.StateReadOnly
}

// CopyTo copies the Logs instance overriding the destination.
func (ms Logs) CopyTo(dest Logs) {
	ms.ResourceLogs().CopyTo(dest.ResourceLogs())
}

// LogRecordCount calculates the total number of log records.
func (ms Logs) LogRecordCount() int {
	logCount := 0
	rss := ms.ResourceLogs()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ill := rs.ScopeLogs()
		for i := 0; i < ill.Len(); i++ {
			logs := ill.At(i)
			logCount += logs.LogRecords().Len()
		}
	}
	return logCount
}

// ResourceLogs returns the ResourceLogsSlice associated with this Logs.
func (ms Logs) ResourceLogs() ResourceLogsSlice {
	return newResourceLogsSlice(&ms.getOrig().ResourceLogs, internal.GetLogsState(internal.Logs(ms)))
}

// MarkReadOnly marks the Logs as shared so that no further modifications can be done on it.
func (ms Logs) MarkReadOnly() {
	internal.SetLogsState(internal.Logs(ms), internal.StateReadOnly)
}
