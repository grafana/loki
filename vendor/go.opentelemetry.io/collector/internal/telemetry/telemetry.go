// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "go.opentelemetry.io/collector/internal/telemetry"

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/internal/telemetry/componentattribute"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

var NewPipelineTelemetryGate = featuregate.GlobalRegistry().MustRegister(
	"telemetry.newPipelineTelemetry",
	featuregate.StageAlpha,
	featuregate.WithRegisterFromVersion("v0.123.0"),
	featuregate.WithRegisterReferenceURL("https://github.com/open-telemetry/opentelemetry-collector/blob/main/docs/rfcs/component-universal-telemetry.md"),
	featuregate.WithRegisterDescription("Injects component-identifying scope attributes in internal Collector metrics"),
)

// IMPORTANT: This struct is reexported as part of the public API of
// go.opentelemetry.io/collector/component, a stable module.
// DO NOT MAKE BREAKING CHANGES TO EXPORTED FIELDS.
type TelemetrySettings struct {
	// Logger that the factory can use during creation and can pass to the created
	// component to be used later as well.
	Logger *zap.Logger

	// TracerProvider that the factory can pass to other instrumented third-party libraries.
	TracerProvider trace.TracerProvider

	// MeterProvider that the factory can pass to other instrumented third-party libraries.
	MeterProvider metric.MeterProvider

	// Resource contains the resource attributes for the collector's telemetry.
	Resource pcommon.Resource

	// Extra attributes added to instrumentation scopes
	extraAttributes attribute.Set
}

// The publicization of this API is tracked in https://github.com/open-telemetry/opentelemetry-collector/issues/12405

func WithoutAttributes(ts TelemetrySettings, fields ...string) TelemetrySettings {
	return WithAttributeSet(ts, componentattribute.RemoveAttributes(ts.extraAttributes, fields...))
}

func WithAttributeSet(ts TelemetrySettings, attrs attribute.Set) TelemetrySettings {
	ts.extraAttributes = attrs
	ts.Logger = componentattribute.ZapLoggerWithAttributes(ts.Logger, ts.extraAttributes)
	ts.TracerProvider = componentattribute.TracerProviderWithAttributes(ts.TracerProvider, ts.extraAttributes)
	if NewPipelineTelemetryGate.IsEnabled() {
		ts.MeterProvider = componentattribute.MeterProviderWithAttributes(ts.MeterProvider, ts.extraAttributes)
	}
	return ts
}
