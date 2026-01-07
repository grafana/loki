// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

type TraceStateWrapper struct {
	orig  *string
	state *State
}

func GetTraceStateOrig(ms TraceStateWrapper) *string {
	return ms.orig
}

func GetTraceStateState(ms TraceStateWrapper) *State {
	return ms.state
}

func NewTraceStateWrapper(orig *string, state *State) TraceStateWrapper {
	return TraceStateWrapper{orig: orig, state: state}
}

func GenTestTraceStateWrapper() TraceStateWrapper {
	return NewTraceStateWrapper(GenTestTraceState(), NewState())
}

func GenTestTraceState() *string {
	orig := new(string)
	*orig = "rojo=00f067aa0ba902b7"
	return orig
}
