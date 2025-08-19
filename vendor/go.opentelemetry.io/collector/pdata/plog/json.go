// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals Logs to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalLogs to the OTLP/JSON format.
func (*JSONMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	internal.MarshalJSONOrigExportLogsServiceRequest(ld.getOrig(), dest)
	if dest.Error() != nil {
		return nil, dest.Error()
	}
	return slices.Clone(dest.Buffer()), nil
}

var _ Unmarshaler = (*JSONUnmarshaler)(nil)

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to Logs.
type JSONUnmarshaler struct{}

// UnmarshalLogs from OTLP/JSON format into Logs.
func (*JSONUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	ld := NewLogs()
	internal.UnmarshalJSONOrigExportLogsServiceRequest(ld.getOrig(), iter)
	if iter.Error() != nil {
		return Logs{}, iter.Error()
	}
	otlp.MigrateLogs(ld.getOrig().ResourceLogs)
	return ld, nil
}
