// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// TelemetrySettings provides components with APIs to report telemetry.
type TelemetrySettings struct {
	// Logger that the factory can use during creation and can pass to the created
	// component to be used later as well.
	Logger *zap.Logger

	// TracerProvider that the factory can pass to other instrumented third-party libraries.
	TracerProvider trace.TracerProvider

	// MeterProvider that the factory can pass to other instrumented third-party libraries.
	MeterProvider metric.MeterProvider

	// MetricsLevel represents the configuration value set when the collector
	// is configured. Components may use this level to decide whether it is
	// appropriate to avoid computationally expensive calculations.
	MetricsLevel configtelemetry.Level

	// Resource contains the resource attributes for the collector's telemetry.
	Resource pcommon.Resource
}
