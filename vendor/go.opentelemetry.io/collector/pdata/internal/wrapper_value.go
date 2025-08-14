// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"fmt"

	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
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

func CopyOrigAnyValue(dest, src *otlpcommon.AnyValue) {
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
		dv.KvlistValue.Values = CopyOrigKeyValueSlice(dv.KvlistValue.Values, sv.KvlistValue.Values)
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
		dv.ArrayValue.Values = CopyOrigAnyValueSlice(dv.ArrayValue.Values, sv.ArrayValue.Values)
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

func GenerateTestValue() Value {
	var orig otlpcommon.AnyValue
	FillOrigTestAnyValue(&orig)
	state := StateMutable
	return NewValue(&orig, &state)
}

// MarshalJSONOrigAnyValue marshals all properties from the current struct to the destination stream.
func MarshalJSONOrigAnyValue(orig *otlpcommon.AnyValue, dest *json.Stream) {
	dest.WriteObjectStart()
	switch v := orig.Value.(type) {
	case nil:
		// Do nothing, return an empty object.
	case *otlpcommon.AnyValue_StringValue:
		dest.WriteObjectField("stringValue")
		dest.WriteString(v.StringValue)
	case *otlpcommon.AnyValue_BoolValue:
		dest.WriteObjectField("boolValue")
		dest.WriteBool(v.BoolValue)
	case *otlpcommon.AnyValue_IntValue:
		dest.WriteObjectField("intValue")
		dest.WriteInt64(v.IntValue)
	case *otlpcommon.AnyValue_DoubleValue:
		dest.WriteObjectField("doubleValue")
		dest.WriteFloat64(v.DoubleValue)
	case *otlpcommon.AnyValue_BytesValue:
		dest.WriteObjectField("bytesValue")
		dest.WriteBytes(v.BytesValue)
	case *otlpcommon.AnyValue_ArrayValue:
		dest.WriteObjectField("arrayValue")
		dest.WriteObjectStart()
		values := v.ArrayValue.Values
		if len(values) > 0 {
			dest.WriteObjectField("values")
			dest.WriteArrayStart()
			MarshalJSONOrigAnyValue(&values[0], dest)
			for i := 1; i < len(values); i++ {
				dest.WriteMore()
				MarshalJSONOrigAnyValue(&values[i], dest)
			}
			dest.WriteArrayEnd()
		}
		dest.WriteObjectEnd()
	case *otlpcommon.AnyValue_KvlistValue:
		dest.WriteObjectField("kvlistValue")
		dest.WriteObjectStart()
		values := v.KvlistValue.Values
		if len(values) > 0 {
			dest.WriteObjectField("values")
			dest.WriteArrayStart()
			MarshalJSONOrigKeyValue(&values[0], dest)
			for i := 1; i < len(values); i++ {
				dest.WriteMore()
				MarshalJSONOrigKeyValue(&values[i], dest)
			}
			dest.WriteArrayEnd()
		}
		dest.WriteObjectEnd()
	default:
		dest.ReportError(fmt.Errorf("invalid value type in the passed attribute value: %T", orig.Value))
	}
	dest.WriteObjectEnd()
}

// UnmarshalJSONOrigAnyValue Unmarshal JSON data and return otlpcommon.AnyValue
func UnmarshalJSONOrigAnyValue(orig *otlpcommon.AnyValue, iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "stringValue", "string_value":
			orig.Value = &otlpcommon.AnyValue_StringValue{
				StringValue: iter.ReadString(),
			}
		case "boolValue", "bool_value":
			orig.Value = &otlpcommon.AnyValue_BoolValue{
				BoolValue: iter.ReadBool(),
			}
		case "intValue", "int_value":
			orig.Value = &otlpcommon.AnyValue_IntValue{
				IntValue: iter.ReadInt64(),
			}
		case "doubleValue", "double_value":
			orig.Value = &otlpcommon.AnyValue_DoubleValue{
				DoubleValue: iter.ReadFloat64(),
			}
		case "bytesValue", "bytes_value":
			orig.Value = &otlpcommon.AnyValue_BytesValue{
				BytesValue: UnmarshalJSONOrigByteSlice(iter),
			}
		case "arrayValue", "array_value":
			orig.Value = &otlpcommon.AnyValue_ArrayValue{
				ArrayValue: readArray(iter),
			}
		case "kvlistValue", "kvlist_value":
			orig.Value = &otlpcommon.AnyValue_KvlistValue{
				KvlistValue: readKvlistValue(iter),
			}
		default:
			iter.Skip()
		}
		return true
	})
}

func readArray(iter *json.Iterator) *otlpcommon.ArrayValue {
	v := &otlpcommon.ArrayValue{}
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "values":
			v.Values = UnmarshalJSONOrigAnyValueSlice(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return v
}

func readKvlistValue(iter *json.Iterator) *otlpcommon.KeyValueList {
	v := &otlpcommon.KeyValueList{}
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "values":
			v.Values = UnmarshalJSONOrigKeyValueSlice(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return v
}

func FillOrigTestAnyValue(orig *otlpcommon.AnyValue) {
	orig.Value = &otlpcommon.AnyValue_StringValue{StringValue: "v"}
}

func SizeProtoOrigAnyValue(orig *otlpcommon.AnyValue) int {
	return orig.Size()
}

func MarshalProtoOrigAnyValue(orig *otlpcommon.AnyValue, buf []byte) int {
	size, _ := orig.MarshalToSizedBuffer(buf)
	return size
}
