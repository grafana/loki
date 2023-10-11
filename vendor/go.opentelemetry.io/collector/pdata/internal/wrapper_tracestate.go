// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

type TraceState struct {
	orig *string
}

func GetOrigTraceState(ms TraceState) *string {
	return ms.orig
}

func NewTraceState(orig *string) TraceState {
	return TraceState{orig: orig}
}

func GenerateTestTraceState() TraceState {
	var orig string
	ms := NewTraceState(&orig)
	FillTestTraceState(ms)
	return ms
}

func FillTestTraceState(dest TraceState) {
	*dest.orig = "rojo=00f067aa0ba902b7"
}
