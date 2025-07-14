// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

type Value struct {
	orig  *otlpcommon.AnyValue
	state *State
}

func GetOrigValue(ms Value) *otlpcommon.AnyValue {
	return ms.orig
}

func GetValueState(ms Value) *State {
	return ms.state
}

func NewValue(orig *otlpcommon.AnyValue, state *State) Value {
	return Value{orig: orig, state: state}
}

func CopyOrigValue(dest, src *otlpcommon.AnyValue) {
	switch sv := src.Value.(type) {
	case *otlpcommon.AnyValue_KvlistValue:
		dv, ok := dest.Value.(*otlpcommon.AnyValue_KvlistValue)
		if !ok {
			dv = &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}
			dest.Value = dv
		}
		if sv.KvlistValue == nil {
			dv.KvlistValue = nil
			return
		}
		dv.KvlistValue.Values = CopyOrigMap(dv.KvlistValue.Values, sv.KvlistValue.Values)
	case *otlpcommon.AnyValue_ArrayValue:
		dv, ok := dest.Value.(*otlpcommon.AnyValue_ArrayValue)
		if !ok {
			dv = &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}
			dest.Value = dv
		}
		if sv.ArrayValue == nil {
			dv.ArrayValue = nil
			return
		}
		dv.ArrayValue.Values = CopyOrigSlice(dv.ArrayValue.Values, sv.ArrayValue.Values)
	case *otlpcommon.AnyValue_BytesValue:
		bv, ok := dest.Value.(*otlpcommon.AnyValue_BytesValue)
		if !ok {
			bv = &otlpcommon.AnyValue_BytesValue{}
			dest.Value = bv
		}
		bv.BytesValue = make([]byte, len(sv.BytesValue))
		copy(bv.BytesValue, sv.BytesValue)
	default:
		// Primitive immutable type, no need for deep copy.
		dest.Value = sv
	}
}

func FillTestValue(dest Value) {
	dest.orig.Value = &otlpcommon.AnyValue_StringValue{StringValue: "v"}
}

func GenerateTestValue() Value {
	var orig otlpcommon.AnyValue
	state := StateMutable
	ms := NewValue(&orig, &state)
	FillTestValue(ms)
	return ms
}
