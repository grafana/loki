// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
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
	*dest.orig = []otlpcommon.KeyValue{
		{Key: "str", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "value"}}},
		{Key: "bool", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BoolValue{BoolValue: true}}},
		{Key: "double", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_DoubleValue{DoubleValue: 3.14}}},
		{Key: "int", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_IntValue{IntValue: 123}}},
		{Key: "bytes", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BytesValue{BytesValue: []byte{1, 2, 3}}}},
		{Key: "array", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_ArrayValue{
			ArrayValue: &otlpcommon.ArrayValue{Values: []otlpcommon.AnyValue{{Value: &otlpcommon.AnyValue_IntValue{IntValue: 321}}}},
		}}},
		{Key: "map", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_KvlistValue{
			KvlistValue: &otlpcommon.KeyValueList{Values: []otlpcommon.KeyValue{{Key: "key", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "value"}}}}},
		}}},
	}
}

// MarshalJSONStreamMap marshals all properties from the current struct to the destination stream.
func MarshalJSONStreamMap(ms Map, dest *json.Stream) {
	dest.WriteArrayStart()
	if len(*ms.orig) > 0 {
		writeAttribute(&(*ms.orig)[0], ms.state, dest)
	}
	for i := 1; i < len(*ms.orig); i++ {
		dest.WriteMore()
		writeAttribute(&(*ms.orig)[i], ms.state, dest)
	}
	dest.WriteArrayEnd()
}

func writeAttribute(attr *otlpcommon.KeyValue, state *State, dest *json.Stream) {
	dest.WriteObjectStart()
	if attr.Key != "" {
		dest.WriteObjectField("key")
		dest.WriteString(attr.Key)
	}
	dest.WriteObjectField("value")
	MarshalJSONStreamValue(NewValue(&attr.Value, state), dest)
	dest.WriteObjectEnd()
}

func UnmarshalJSONIterMap(ms Map, iter *json.Iterator) {
	iter.ReadArrayCB(func(iter *json.Iterator) bool {
		*ms.orig = append(*ms.orig, otlpcommon.KeyValue{})
		iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
			switch f {
			case "key":
				(*ms.orig)[len(*ms.orig)-1].Key = iter.ReadString()
			case "value":
				UnmarshalJSONIterValue(NewValue(&(*ms.orig)[len(*ms.orig)-1].Value, nil), iter)
			default:
				iter.Skip()
			}
			return true
		})
		return true
	})
}
