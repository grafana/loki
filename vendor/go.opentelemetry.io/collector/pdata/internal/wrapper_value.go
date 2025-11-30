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

func NewAnyValueStringValue() *AnyValue_StringValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_StringValue{}
	}
	return ProtoPoolAnyValue_StringValue.Get().(*AnyValue_StringValue)
}

func NewAnyValueIntValue() *AnyValue_IntValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_IntValue{}
	}
	return ProtoPoolAnyValue_IntValue.Get().(*AnyValue_IntValue)
}

func NewAnyValueBoolValue() *AnyValue_BoolValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_BoolValue{}
	}
	return ProtoPoolAnyValue_BoolValue.Get().(*AnyValue_BoolValue)
}

func NewAnyValueDoubleValue() *AnyValue_DoubleValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_DoubleValue{}
	}
	return ProtoPoolAnyValue_DoubleValue.Get().(*AnyValue_DoubleValue)
}

func NewAnyValueBytesValue() *AnyValue_BytesValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_BytesValue{}
	}
	return ProtoPoolAnyValue_BytesValue.Get().(*AnyValue_BytesValue)
}

func NewAnyValueArrayValue() *AnyValue_ArrayValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_ArrayValue{}
	}
	return ProtoPoolAnyValue_ArrayValue.Get().(*AnyValue_ArrayValue)
}

func NewAnyValueKvlistValue() *AnyValue_KvlistValue {
	if !UseProtoPooling.IsEnabled() {
		return &AnyValue_KvlistValue{}
	}
	return ProtoPoolAnyValue_KvlistValue.Get().(*AnyValue_KvlistValue)
}
