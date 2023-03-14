// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcollectorlog "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/logs/v1"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
)

// LogsToOtlp internal helper to convert Logs to otlp request representation.
func LogsToOtlp(mw Logs) *otlpcollectorlog.ExportLogsServiceRequest {
	return mw.orig
}

// LogsFromOtlp internal helper to convert otlp request representation to Logs.
func LogsFromOtlp(orig *otlpcollectorlog.ExportLogsServiceRequest) Logs {
	return Logs{orig: orig}
}

// LogsToProto internal helper to convert Logs to protobuf representation.
func LogsToProto(l Logs) otlplogs.LogsData {
	return otlplogs.LogsData{
		ResourceLogs: l.orig.ResourceLogs,
	}
}

// LogsFromProto internal helper to convert protobuf representation to Logs.
func LogsFromProto(orig otlplogs.LogsData) Logs {
	return Logs{orig: &otlpcollectorlog.ExportLogsServiceRequest{
		ResourceLogs: orig.ResourceLogs,
	}}
}

// Logs is the top-level struct that is propagated through the logs pipeline.
// Use NewLogs to create new instance, zero-initialized instance is not valid for use.
type Logs struct {
	orig *otlpcollectorlog.ExportLogsServiceRequest
}

// NewLogs creates a new Logs struct.
func NewLogs() Logs {
	return Logs{orig: &otlpcollectorlog.ExportLogsServiceRequest{}}
}

// MoveTo moves all properties from the current struct to dest
// resetting the current instance to its zero value.
func (ld Logs) MoveTo(dest Logs) {
	*dest.orig = *ld.orig
	*ld.orig = otlpcollectorlog.ExportLogsServiceRequest{}
}

// Clone returns a copy of Logs.
func (ld Logs) Clone() Logs {
	cloneLd := NewLogs()
	ld.ResourceLogs().CopyTo(cloneLd.ResourceLogs())
	return cloneLd
}

// LogRecordCount calculates the total number of log records.
func (ld Logs) LogRecordCount() int {
	logCount := 0
	rss := ld.ResourceLogs()
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
func (ld Logs) ResourceLogs() ResourceLogsSlice {
	return newResourceLogsSlice(&ld.orig.ResourceLogs)
}

// SeverityNumber represents severity number of a log record.
type SeverityNumber int32

const (
	SeverityNumberUNDEFINED = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED)
	SeverityNumberTRACE     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_TRACE)
	SeverityNumberTRACE2    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_TRACE2)
	SeverityNumberTRACE3    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_TRACE3)
	SeverityNumberTRACE4    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_TRACE4)
	SeverityNumberDEBUG     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_DEBUG)
	SeverityNumberDEBUG2    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_DEBUG2)
	SeverityNumberDEBUG3    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_DEBUG3)
	SeverityNumberDEBUG4    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_DEBUG4)
	SeverityNumberINFO      = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_INFO)
	SeverityNumberINFO2     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_INFO2)
	SeverityNumberINFO3     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_INFO3)
	SeverityNumberINFO4     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_INFO4)
	SeverityNumberWARN      = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN)
	SeverityNumberWARN2     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN2)
	SeverityNumberWARN3     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN3)
	SeverityNumberWARN4     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN4)
	SeverityNumberERROR     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_ERROR)
	SeverityNumberERROR2    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_ERROR2)
	SeverityNumberERROR3    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_ERROR3)
	SeverityNumberERROR4    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_ERROR4)
	SeverityNumberFATAL     = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_FATAL)
	SeverityNumberFATAL2    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_FATAL2)
	SeverityNumberFATAL3    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_FATAL3)
	SeverityNumberFATAL4    = SeverityNumber(otlplogs.SeverityNumber_SEVERITY_NUMBER_FATAL4)
)

// String returns the string representation of the SeverityNumber.
func (sn SeverityNumber) String() string { return otlplogs.SeverityNumber(sn).String() }
