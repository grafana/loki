// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentattribute // import "go.opentelemetry.io/collector/internal/telemetry/componentattribute"

import (
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	ComponentKindKey = "otelcol.component.kind"
	ComponentIDKey   = "otelcol.component.id"
	PipelineIDKey    = "otelcol.pipeline.id"
	SignalKey        = "otelcol.signal"
	SignalOutputKey  = "otelcol.signal.output"
)

func RemoveAttributes(attrs attribute.Set, fields ...string) attribute.Set {
	attrs, _ = attribute.NewSetWithFiltered(attrs.ToSlice(), func(kv attribute.KeyValue) bool {
		return !slices.Contains(fields, string(kv.Key))
	})
	return attrs
}

// ToZapFields converts an OTel Go attribute set to a slice of zap fields.
func ToZapFields(attrs attribute.Set) []zap.Field {
	zapFields := make([]zap.Field, 0, attrs.Len())
	for _, attr := range attrs.ToSlice() {
		var zapField zap.Field
		key := string(attr.Key)
		switch attr.Value.Type() {
		case attribute.BOOL:
			zapField = zap.Bool(key, attr.Value.AsBool())
		case attribute.INT64:
			zapField = zap.Int64(key, attr.Value.AsInt64())
		case attribute.FLOAT64:
			zapField = zap.Float64(key, attr.Value.AsFloat64())
		case attribute.STRING:
			zapField = zap.String(key, attr.Value.AsString())
		case attribute.BOOLSLICE:
			zapField = zap.Bools(key, attr.Value.AsBoolSlice())
		case attribute.INT64SLICE:
			zapField = zap.Int64s(key, attr.Value.AsInt64Slice())
		case attribute.FLOAT64SLICE:
			zapField = zap.Float64s(key, attr.Value.AsFloat64Slice())
		case attribute.STRINGSLICE:
			zapField = zap.Strings(key, attr.Value.AsStringSlice())
		default:
			zapField = zap.Any(key, attr.Value.AsInterface())
		}
		zapFields = append(zapFields, zapField)
	}
	return zapFields
}
