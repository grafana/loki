// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

func (e *ProtoMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	size := ld.getOrig().SizeProto()
	buf := make([]byte, size)
	_ = ld.getOrig().MarshalProto(buf)
	return buf, nil
}

func (e *ProtoMarshaler) LogsSize(ld Logs) int {
	return ld.getOrig().SizeProto()
}

func (e *ProtoMarshaler) ResourceLogsSize(ld ResourceLogs) int {
	return ld.orig.SizeProto()
}

func (e *ProtoMarshaler) ScopeLogsSize(ld ScopeLogs) int {
	return ld.orig.SizeProto()
}

func (e *ProtoMarshaler) LogRecordSize(ld LogRecord) int {
	return ld.orig.SizeProto()
}

var _ Unmarshaler = (*ProtoUnmarshaler)(nil)

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	ld := NewLogs()
	err := ld.getOrig().UnmarshalProto(buf)
	if err != nil {
		return Logs{}, err
	}
	return ld, nil
}
