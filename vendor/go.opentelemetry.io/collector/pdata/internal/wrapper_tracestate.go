// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"go.opentelemetry.io/collector/pdata/internal/json"
)

type TraceState struct {
	orig  *string
	state *State
}

func GetOrigTraceState(ms TraceState) *string {
	return ms.orig
}

func GetTraceStateState(ms TraceState) *State {
	return ms.state
}

func NewTraceState(orig *string, state *State) TraceState {
	return TraceState{orig: orig, state: state}
}

func CopyOrigTraceState(dest, src *string) {
	*dest = *src
}

func GenerateTestTraceState() TraceState {
	var orig string
	state := StateMutable
	ms := NewTraceState(&orig, &state)
	FillTestTraceState(ms)
	return ms
}

func FillTestTraceState(dest TraceState) {
	*dest.orig = "rojo=00f067aa0ba902b7"
}

// MarshalJSONStreamTraceState marshals all properties from the current struct to the destination stream.
func MarshalJSONStreamTraceState(ms TraceState, dest *json.Stream) {
	dest.WriteString(*ms.orig)
}
