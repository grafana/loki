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
