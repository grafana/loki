// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

type ValueWrapper struct {
	orig  *AnyValue
	state *State
}

func GetValueOrig(ms ValueWrapper) *AnyValue {
	return ms.orig
}

func GetValueState(ms ValueWrapper) *State {
	return ms.state
}

func NewValueWrapper(orig *AnyValue, state *State) ValueWrapper {
	return ValueWrapper{orig: orig, state: state}
}

func GenTestValueWrapper() ValueWrapper {
	orig := GenTestAnyValue()
	return NewValueWrapper(orig, NewState())
}
