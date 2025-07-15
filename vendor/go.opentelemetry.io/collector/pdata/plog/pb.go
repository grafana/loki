// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"go.opentelemetry.io/collector/pdata/internal"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
)

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

func (e *ProtoMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	pb := internal.LogsToProto(internal.Logs(ld))
	return pb.Marshal()
}

func (e *ProtoMarshaler) LogsSize(ld Logs) int {
	pb := internal.LogsToProto(internal.Logs(ld))
	return pb.Size()
}

func (e *ProtoMarshaler) ResourceLogsSize(rl ResourceLogs) int {
	return rl.orig.Size()
}

func (e *ProtoMarshaler) ScopeLogsSize(sl ScopeLogs) int {
	return sl.orig.Size()
}

func (e *ProtoMarshaler) LogRecordSize(lr LogRecord) int {
	return lr.orig.Size()
}

var _ Unmarshaler = (*ProtoUnmarshaler)(nil)

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	pb := otlplogs.LogsData{}
	err := pb.Unmarshal(buf)
	return Logs(internal.LogsFromProto(pb)), err
}
