// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

type Value struct {
	orig *otlpcommon.AnyValue
}

func GetOrigValue(ms Value) *otlpcommon.AnyValue {
	return ms.orig
}

func NewValue(orig *otlpcommon.AnyValue) Value {
	return Value{orig: orig}
}

func FillTestValue(dest Value) {
	dest.orig.Value = &otlpcommon.AnyValue_StringValue{StringValue: "v"}
}

func GenerateTestValue() Value {
	var orig otlpcommon.AnyValue
	ms := NewValue(&orig)
	FillTestValue(ms)
	return ms
}
