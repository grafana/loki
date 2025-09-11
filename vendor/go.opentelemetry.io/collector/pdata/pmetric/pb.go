// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

func (e *ProtoMarshaler) MarshalMetrics(md Metrics) ([]byte, error) {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.getOrig().Marshal()
	}
	size := internal.SizeProtoOrigExportMetricsServiceRequest(md.getOrig())
	buf := make([]byte, size)
	_ = internal.MarshalProtoOrigExportMetricsServiceRequest(md.getOrig(), buf)
	return buf, nil
}

func (e *ProtoMarshaler) MetricsSize(md Metrics) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.getOrig().Size()
	}
	return internal.SizeProtoOrigExportMetricsServiceRequest(md.getOrig())
}

func (e *ProtoMarshaler) ResourceMetricsSize(md ResourceMetrics) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigResourceMetrics(md.orig)
}

func (e *ProtoMarshaler) ScopeMetricsSize(md ScopeMetrics) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigScopeMetrics(md.orig)
}

func (e *ProtoMarshaler) MetricSize(md Metric) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigMetric(md.orig)
}

func (e *ProtoMarshaler) NumberDataPointSize(md NumberDataPoint) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigNumberDataPoint(md.orig)
}

func (e *ProtoMarshaler) SummaryDataPointSize(md SummaryDataPoint) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigSummaryDataPoint(md.orig)
}

func (e *ProtoMarshaler) HistogramDataPointSize(md HistogramDataPoint) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigHistogramDataPoint(md.orig)
}

func (e *ProtoMarshaler) ExponentialHistogramDataPointSize(md ExponentialHistogramDataPoint) int {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return md.orig.Size()
	}
	return internal.SizeProtoOrigExponentialHistogramDataPoint(md.orig)
}

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalMetrics(buf []byte) (Metrics, error) {
	md := NewMetrics()
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		err := md.getOrig().Unmarshal(buf)
		if err != nil {
			return Metrics{}, err
		}
		return md, nil
	}
	err := internal.UnmarshalProtoOrigExportMetricsServiceRequest(md.getOrig(), buf)
	if err != nil {
		return Metrics{}, err
	}
	return md, nil
}
