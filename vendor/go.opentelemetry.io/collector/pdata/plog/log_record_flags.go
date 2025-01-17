// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

const isSampledMask = uint32(1)

var DefaultLogRecordFlags = LogRecordFlags(0)

// LogRecordFlags defines flags for the LogRecord. The 8 least significant bits are the trace flags as
// defined in W3C Trace Context specification. 24 most significant bits are reserved and must be set to 0.
type LogRecordFlags uint32

// IsSampled returns true if the LogRecordFlags contains the IsSampled flag.
func (ms LogRecordFlags) IsSampled() bool {
	return uint32(ms)&isSampledMask != 0
}

// WithIsSampled returns a new LogRecordFlags, with the IsSampled flag set to the given value.
func (ms LogRecordFlags) WithIsSampled(b bool) LogRecordFlags {
	orig := uint32(ms)
	if b {
		orig |= isSampledMask
	} else {
		orig &^= isSampledMask
	}
	return LogRecordFlags(orig)
}
