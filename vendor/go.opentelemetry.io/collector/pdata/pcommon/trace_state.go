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

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// TraceState represents the trace state from the w3c-trace-context.
type TraceState internal.TraceState

func NewTraceState() TraceState {
	return TraceState(internal.NewTraceState(new(string)))
}

func (ms TraceState) getOrig() *string {
	return internal.GetOrigTraceState(internal.TraceState(ms))
}

// AsRaw returns the string representation of the tracestate in w3c-trace-context format: https://www.w3.org/TR/trace-context/#tracestate-header
func (ms TraceState) AsRaw() string {
	return *ms.getOrig()
}

// FromRaw copies the string representation in w3c-trace-context format of the tracestate into this TraceState.
func (ms TraceState) FromRaw(v string) {
	*ms.getOrig() = v
}

// MoveTo moves TraceState to another instance.
func (ms TraceState) MoveTo(dest TraceState) {
	*dest.getOrig() = *ms.getOrig()
	*ms.getOrig() = ""
}

// CopyTo copies TraceState to another instance.
func (ms TraceState) CopyTo(dest TraceState) {
	*dest.getOrig() = *ms.getOrig()
}
