// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals Logs to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalLogs to the OTLP/JSON format.
func (*JSONMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	ld.getOrig().MarshalJSON(dest)
	if dest.Error() != nil {
		return nil, dest.Error()
	}
	return slices.Clone(dest.Buffer()), nil
}

var _ Unmarshaler = (*JSONUnmarshaler)(nil)

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to Logs.
type JSONUnmarshaler struct {
	// prevent unkeyed literal initialization
	_ struct{}
	// DisallowUnknownFields causes UnmarshalLogs to return an error when the
	// input contains JSON object fields that are not defined by the OTLP
	// schema. When false (the default), unknown fields are silently ignored.
	//
	// Warning: enabling this option breaks forwards compatibility with future
	// evolutions of the OTLP format, as fields added to the format in newer
	// versions will be rejected as unknown.
	DisallowUnknownFields bool
}

// UnmarshalLogs from OTLP/JSON format into Logs.
func (u *JSONUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	iter.SetDisallowUnknownFields(u.DisallowUnknownFields)
	ld := NewLogs()
	ld.getOrig().UnmarshalJSON(iter)
	if iter.Error() != nil {
		return Logs{}, iter.Error()
	}
	otlp.MigrateLogs(ld.getOrig().ResourceLogs)
	return ld, nil
}
