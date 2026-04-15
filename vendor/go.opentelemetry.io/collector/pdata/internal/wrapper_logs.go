// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// LogsToProto internal helper to convert Logs to protobuf representation.
func LogsToProto(l LogsWrapper) LogsData {
	return LogsData{
		ResourceLogs: l.orig.ResourceLogs,
	}
}

// LogsFromProto internal helper to convert protobuf representation to Logs.
// This function set exclusive state assuming that it's called only once per Logs.
func LogsFromProto(orig LogsData) LogsWrapper {
	return NewLogsWrapper(&ExportLogsServiceRequest{
		ResourceLogs: orig.ResourceLogs,
	}, NewState())
}
