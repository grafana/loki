// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
)

type Metrics struct {
	orig *otlpcollectormetrics.ExportMetricsServiceRequest
}

func GetOrigMetrics(ms Metrics) *otlpcollectormetrics.ExportMetricsServiceRequest {
	return ms.orig
}

func NewMetrics(orig *otlpcollectormetrics.ExportMetricsServiceRequest) Metrics {
	return Metrics{orig: orig}
}

// MetricsToProto internal helper to convert Metrics to protobuf representation.
func MetricsToProto(l Metrics) otlpmetrics.MetricsData {
	return otlpmetrics.MetricsData{
		ResourceMetrics: l.orig.ResourceMetrics,
	}
}

// MetricsFromProto internal helper to convert protobuf representation to Metrics.
func MetricsFromProto(orig otlpmetrics.MetricsData) Metrics {
	return Metrics{orig: &otlpcollectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: orig.ResourceMetrics,
	}}
}
