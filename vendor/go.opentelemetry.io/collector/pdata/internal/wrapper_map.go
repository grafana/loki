// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

type Map struct {
	orig *[]otlpcommon.KeyValue
}

func GetOrigMap(ms Map) *[]otlpcommon.KeyValue {
	return ms.orig
}

func NewMap(orig *[]otlpcommon.KeyValue) Map {
	return Map{orig: orig}
}

func GenerateTestMap() Map {
	var orig []otlpcommon.KeyValue
	ms := NewMap(&orig)
	FillTestMap(ms)
	return ms
}

func FillTestMap(dest Map) {
	*dest.orig = nil
	*dest.orig = append(*dest.orig, otlpcommon.KeyValue{Key: "k", Value: otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "v"}}})
}
