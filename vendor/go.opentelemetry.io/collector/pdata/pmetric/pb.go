// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

var _ MarshalSizer = (*ProtoMarshaler)(nil)

type ProtoMarshaler struct{}

func (e *ProtoMarshaler) MarshalMetrics(md Metrics) ([]byte, error) {
	size := md.getOrig().SizeProto()
	buf := make([]byte, size)
	_ = md.getOrig().MarshalProto(buf)
	return buf, nil
}

func (e *ProtoMarshaler) MetricsSize(md Metrics) int {
	return md.getOrig().SizeProto()
}

func (e *ProtoMarshaler) ResourceMetricsSize(md ResourceMetrics) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) ScopeMetricsSize(md ScopeMetrics) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) MetricSize(md Metric) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) NumberDataPointSize(md NumberDataPoint) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) SummaryDataPointSize(md SummaryDataPoint) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) HistogramDataPointSize(md HistogramDataPoint) int {
	return md.orig.SizeProto()
}

func (e *ProtoMarshaler) ExponentialHistogramDataPointSize(md ExponentialHistogramDataPoint) int {
	return md.orig.SizeProto()
}

type ProtoUnmarshaler struct{}

func (d *ProtoUnmarshaler) UnmarshalMetrics(buf []byte) (Metrics, error) {
	md := NewMetrics()
	err := md.getOrig().UnmarshalProto(buf)
	if err != nil {
		return Metrics{}, err
	}
	return md, nil
}
