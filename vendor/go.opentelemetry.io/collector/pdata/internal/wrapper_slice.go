// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

type Slice struct {
	orig *[]otlpcommon.AnyValue
}

func GetOrigSlice(ms Slice) *[]otlpcommon.AnyValue {
	return ms.orig
}

func NewSlice(orig *[]otlpcommon.AnyValue) Slice {
	return Slice{orig: orig}
}

func GenerateTestSlice() Slice {
	orig := []otlpcommon.AnyValue{}
	tv := NewSlice(&orig)
	FillTestSlice(tv)
	return tv
}

func FillTestSlice(tv Slice) {
	*tv.orig = make([]otlpcommon.AnyValue, 7)
	for i := 0; i < 7; i++ {
		FillTestValue(NewValue(&(*tv.orig)[i]))
	}
}
