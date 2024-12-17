// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

// MarshalSizer is the interface that groups the basic Marshal and Size methods
type MarshalSizer interface {
	Marshaler
	Sizer
}

// Marshaler marshals pdata.Logs into bytes.
type Marshaler interface {
	// MarshalLogs the given pdata.Logs into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalLogs(ld Logs) ([]byte, error)
}

// Unmarshaler unmarshalls bytes into pdata.Logs.
type Unmarshaler interface {
	// UnmarshalLogs the given bytes into pdata.Logs.
	// If the error is not nil, the returned pdata.Logs cannot be used.
	UnmarshalLogs(buf []byte) (Logs, error)
}

// Sizer is an optional interface implemented by the Marshaler,
// that calculates the size of a marshaled Logs.
type Sizer interface {
	// LogsSize returns the size in bytes of a marshaled Logs.
	LogsSize(ld Logs) int
}
