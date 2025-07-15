// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

type Map struct {
	orig  *[]otlpcommon.KeyValue
	state *State
}

func GetOrigMap(ms Map) *[]otlpcommon.KeyValue {
	return ms.orig
}

func GetMapState(ms Map) *State {
	return ms.state
}

func NewMap(orig *[]otlpcommon.KeyValue, state *State) Map {
	return Map{orig: orig, state: state}
}

func CopyOrigMap(dest, src []otlpcommon.KeyValue) []otlpcommon.KeyValue {
	if cap(dest) < len(src) {
		dest = make([]otlpcommon.KeyValue, len(src))
	}
	dest = dest[:len(src)]
	for i := 0; i < len(src); i++ {
		dest[i].Key = src[i].Key
		CopyOrigValue(&dest[i].Value, &src[i].Value)
	}
	return dest
}

func GenerateTestMap() Map {
	var orig []otlpcommon.KeyValue
	state := StateMutable
	ms := NewMap(&orig, &state)
	FillTestMap(ms)
	return ms
}

func FillTestMap(dest Map) {
	*dest.orig = nil
	*dest.orig = append(*dest.orig, otlpcommon.KeyValue{Key: "k", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "v"}}})
}
