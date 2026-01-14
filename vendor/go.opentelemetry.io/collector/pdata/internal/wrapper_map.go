// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

type MapWrapper struct {
	orig  *[]KeyValue
	state *State
}

func GetMapOrig(ms MapWrapper) *[]KeyValue {
	return ms.orig
}

func GetMapState(ms MapWrapper) *State {
	return ms.state
}

func NewMapWrapper(orig *[]KeyValue, state *State) MapWrapper {
	return MapWrapper{orig: orig, state: state}
}

func GenTestMapWrapper() MapWrapper {
	orig := GenTestKeyValueSlice()
	return NewMapWrapper(&orig, NewState())
}
