// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

// MarkReadOnly marks the Logs as shared so that no further modifications can be done on it.
func (ms Logs) MarkReadOnly() {
	ms.getState().MarkReadOnly()
}

// IsReadOnly returns true if this Logs instance is read-only.
func (ms Logs) IsReadOnly() bool {
	return ms.getState().IsReadOnly()
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
