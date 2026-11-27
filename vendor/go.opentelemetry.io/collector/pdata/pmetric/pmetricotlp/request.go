// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetricotlp // import "go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// ExportRequest represents the request for gRPC/HTTP client/server.
// It's a wrapper for pmetric.Metrics data.
type ExportRequest struct {
	orig  *internal.ExportMetricsServiceRequest
	state *internal.State
}

// NewExportRequest returns an empty ExportRequest.
func NewExportRequest() ExportRequest {
	return ExportRequest{
		orig:  &internal.ExportMetricsServiceRequest{},
		state: internal.NewState(),
	}
}

// NewExportRequestFromMetrics returns a ExportRequest from pmetric.Metrics.
// Because ExportRequest is a wrapper for pmetric.Metrics,
// any changes to the provided Metrics struct will be reflected in the ExportRequest and vice versa.
func NewExportRequestFromMetrics(md pmetric.Metrics) ExportRequest {
	return ExportRequest{
		orig:  internal.GetMetricsOrig(internal.MetricsWrapper(md)),
		state: internal.GetMetricsState(internal.MetricsWrapper(md)),
	}
}

// MarshalProto marshals ExportRequest into proto bytes.
func (ms ExportRequest) MarshalProto() ([]byte, error) {
	size := ms.orig.SizeProto()
	buf := make([]byte, size)
	_ = ms.orig.MarshalProto(buf)
	return buf, nil
}

// UnmarshalProto unmarshalls ExportRequest from proto bytes.
func (ms ExportRequest) UnmarshalProto(data []byte) error {
	err := ms.orig.UnmarshalProto(data)
	if err != nil {
		return err
	}
	otlp.MigrateMetrics(ms.orig.ResourceMetrics)
	return nil
}

// MarshalJSON marshals ExportRequest into JSON bytes.
func (ms ExportRequest) MarshalJSON() ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	ms.orig.MarshalJSON(dest)
	if dest.Error() != nil {
		return nil, dest.Error()
	}
	return slices.Clone(dest.Buffer()), nil
}

// UnmarshalJSON unmarshalls ExportRequest from JSON bytes.
func (ms ExportRequest) UnmarshalJSON(data []byte) error {
	iter := json.BorrowIterator(data)
	defer json.ReturnIterator(iter)
	ms.orig.UnmarshalJSON(iter)
	return iter.Error()
}

// ValidateUTF8 returns false when any string in the request contains invalid UTF-8.
func (ms ExportRequest) ValidateUTF8() bool {
	return internal.ValidateUTF8(ms.orig)
}

// RejectInvalidUTF8 removes data points containing invalid UTF-8 and returns the number removed.
func (ms ExportRequest) RejectInvalidUTF8() int {
	md := ms.Metrics()
	rejected := 0
	md.ResourceMetrics().RemoveIf(func(rm pmetric.ResourceMetrics) bool {
		if !internal.ValidateUTF8(rm.Resource()) || !internal.ValidateUTF8(rm.SchemaUrl()) {
			rejected += countResourceDataPoints(rm)
			return true
		}
		if rm.ScopeMetrics().Len() == 0 {
			return false
		}
		rm.ScopeMetrics().RemoveIf(func(sm pmetric.ScopeMetrics) bool {
			if !internal.ValidateUTF8(sm.Scope()) || !internal.ValidateUTF8(sm.SchemaUrl()) {
				rejected += countScopeDataPoints(sm)
				return true
			}
			if sm.Metrics().Len() == 0 {
				return false
			}
			sm.Metrics().RemoveIf(func(metric pmetric.Metric) bool {
				if !internal.ValidateUTF8(metric.Name()) || !internal.ValidateUTF8(metric.Description()) ||
					!internal.ValidateUTF8(metric.Unit()) || !internal.ValidateUTF8(metric.Metadata()) {
					rejected += countMetricDataPoints(metric)
					return true
				}
				if countMetricDataPoints(metric) == 0 {
					return false
				}
				rejected += rejectMetricDataPoints(metric)
				return countMetricDataPoints(metric) == 0
			})
			return sm.Metrics().Len() == 0
		})
		return rm.ScopeMetrics().Len() == 0
	})
	return rejected
}

func countResourceDataPoints(rm pmetric.ResourceMetrics) int {
	count := 0
	for i := 0; i < rm.ScopeMetrics().Len(); i++ {
		count += countScopeDataPoints(rm.ScopeMetrics().At(i))
	}
	return count
}

func countScopeDataPoints(sm pmetric.ScopeMetrics) int {
	count := 0
	for i := 0; i < sm.Metrics().Len(); i++ {
		count += countMetricDataPoints(sm.Metrics().At(i))
	}
	return count
}

func countMetricDataPoints(metric pmetric.Metric) int {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		return metric.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		return metric.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		return metric.Histogram().DataPoints().Len()
	case pmetric.MetricTypeExponentialHistogram:
		return metric.ExponentialHistogram().DataPoints().Len()
	case pmetric.MetricTypeSummary:
		return metric.Summary().DataPoints().Len()
	default:
		return 0
	}
}

func rejectMetricDataPoints(metric pmetric.Metric) int {
	rejected := 0
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		metric.Gauge().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			return rejectDataPoint(dp, &rejected)
		})
	case pmetric.MetricTypeSum:
		metric.Sum().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			return rejectDataPoint(dp, &rejected)
		})
	case pmetric.MetricTypeHistogram:
		metric.Histogram().DataPoints().RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
			return rejectDataPoint(dp, &rejected)
		})
	case pmetric.MetricTypeExponentialHistogram:
		metric.ExponentialHistogram().DataPoints().RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
			return rejectDataPoint(dp, &rejected)
		})
	case pmetric.MetricTypeSummary:
		metric.Summary().DataPoints().RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
			return rejectDataPoint(dp, &rejected)
		})
	}
	return rejected
}

func rejectDataPoint(dp any, rejected *int) bool {
	invalid := !internal.ValidateUTF8(dp)
	if invalid {
		*rejected++
	}
	return invalid
}

func (ms ExportRequest) Metrics() pmetric.Metrics {
	return pmetric.Metrics(internal.NewMetricsWrapper(ms.orig, ms.state))
}

// SanitizeUTF8 replaces every run of invalid UTF-8 bytes in the string fields of the request with the
// Unicode replacement character U+FFFD, so the request only contains valid UTF-8 strings afterwards.
// Attribute keys and values, nested map and slice values, names, descriptions and other string fields
// are all covered. Bytes values and binary identifiers (exemplar trace and span IDs) are not modified.
func (ms ExportRequest) SanitizeUTF8() {
	ms.state.AssertMutable()
	internal.SanitizeUTF8ExportMetricsServiceRequest(ms.orig)
}
