// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pdata // import "go.opentelemetry.io/collector/model/pdata"

import (
	"go.opentelemetry.io/collector/model/internal"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
)

// MetricsMarshaler marshals pdata.Metrics into bytes.
type MetricsMarshaler interface {
	// MarshalMetrics the given pdata.Metrics into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalMetrics(md Metrics) ([]byte, error)
}

// MetricsUnmarshaler unmarshalls bytes into pdata.Metrics.
type MetricsUnmarshaler interface {
	// UnmarshalMetrics the given bytes into pdata.Metrics.
	// If the error is not nil, the returned pdata.Metrics cannot be used.
	UnmarshalMetrics(buf []byte) (Metrics, error)
}

// MetricsSizer is an optional interface implemented by the MetricsMarshaler,
// that calculates the size of a marshaled Metrics.
type MetricsSizer interface {
	// MetricsSize returns the size in bytes of a marshaled Metrics.
	MetricsSize(md Metrics) int
}

// Metrics is an opaque interface that allows transition to the new internal Metrics data, but also facilitates the
// transition to the new components, especially for traces.
//
// Outside of the core repository, the metrics pipeline cannot be converted to the new model since data.MetricData is
// part of the internal package.
type Metrics struct {
	orig *otlpmetrics.MetricsData
}

// NewMetrics creates a new Metrics.
func NewMetrics() Metrics {
	return Metrics{orig: &otlpmetrics.MetricsData{}}
}

// MetricsFromInternalRep creates Metrics from the internal representation.
// Should not be used outside this module.
func MetricsFromInternalRep(wrapper internal.MetricsWrapper) Metrics {
	return Metrics{orig: internal.MetricsToOtlp(wrapper)}
}

// InternalRep returns internal representation of the Metrics.
// Should not be used outside this module.
func (md Metrics) InternalRep() internal.MetricsWrapper {
	return internal.MetricsFromOtlp(md.orig)
}

// Clone returns a copy of MetricData.
func (md Metrics) Clone() Metrics {
	cloneMd := NewMetrics()
	md.ResourceMetrics().CopyTo(cloneMd.ResourceMetrics())
	return cloneMd
}

// ResourceMetrics returns the ResourceMetricsSlice associated with this Metrics.
func (md Metrics) ResourceMetrics() ResourceMetricsSlice {
	return newResourceMetricsSlice(&md.orig.ResourceMetrics)
}

// MetricCount calculates the total number of metrics.
func (md Metrics) MetricCount() int {
	metricCount := 0
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		ilms := rm.InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			ilm := ilms.At(j)
			metricCount += ilm.Metrics().Len()
		}
	}
	return metricCount
}

// DataPointCount calculates the total number of data points.
func (md Metrics) DataPointCount() (dataPointCount int) {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		ilms := rm.InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			ilm := ilms.At(j)
			ms := ilm.Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				switch m.DataType() {
				case MetricDataTypeGauge:
					dataPointCount += m.Gauge().DataPoints().Len()
				case MetricDataTypeSum:
					dataPointCount += m.Sum().DataPoints().Len()
				case MetricDataTypeHistogram:
					dataPointCount += m.Histogram().DataPoints().Len()
				case MetricDataTypeExponentialHistogram:
					dataPointCount += m.ExponentialHistogram().DataPoints().Len()
				case MetricDataTypeSummary:
					dataPointCount += m.Summary().DataPoints().Len()
				}
			}
		}
	}
	return
}

// MetricDataType specifies the type of data in a Metric.
type MetricDataType int32

const (
	MetricDataTypeNone MetricDataType = iota
	MetricDataTypeGauge
	MetricDataTypeSum
	MetricDataTypeHistogram
	MetricDataTypeExponentialHistogram
	MetricDataTypeSummary
)

// String returns the string representation of the MetricDataType.
func (mdt MetricDataType) String() string {
	switch mdt {
	case MetricDataTypeNone:
		return "None"
	case MetricDataTypeGauge:
		return "Gauge"
	case MetricDataTypeSum:
		return "Sum"
	case MetricDataTypeHistogram:
		return "Histogram"
	case MetricDataTypeExponentialHistogram:
		return "ExponentialHistogram"
	case MetricDataTypeSummary:
		return "Summary"
	}
	return ""
}

// DataType returns the type of the data for this Metric.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) DataType() MetricDataType {
	switch ms.orig.Data.(type) {
	case *otlpmetrics.Metric_Gauge:
		return MetricDataTypeGauge
	case *otlpmetrics.Metric_Sum:
		return MetricDataTypeSum
	case *otlpmetrics.Metric_Histogram:
		return MetricDataTypeHistogram
	case *otlpmetrics.Metric_ExponentialHistogram:
		return MetricDataTypeExponentialHistogram
	case *otlpmetrics.Metric_Summary:
		return MetricDataTypeSummary
	}
	return MetricDataTypeNone
}

// SetDataType clears any existing data and initialize it with an empty data of the given type.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) SetDataType(ty MetricDataType) {
	switch ty {
	case MetricDataTypeGauge:
		ms.orig.Data = &otlpmetrics.Metric_Gauge{Gauge: &otlpmetrics.Gauge{}}
	case MetricDataTypeSum:
		ms.orig.Data = &otlpmetrics.Metric_Sum{Sum: &otlpmetrics.Sum{}}
	case MetricDataTypeHistogram:
		ms.orig.Data = &otlpmetrics.Metric_Histogram{Histogram: &otlpmetrics.Histogram{}}
	case MetricDataTypeExponentialHistogram:
		ms.orig.Data = &otlpmetrics.Metric_ExponentialHistogram{ExponentialHistogram: &otlpmetrics.ExponentialHistogram{}}
	case MetricDataTypeSummary:
		ms.orig.Data = &otlpmetrics.Metric_Summary{Summary: &otlpmetrics.Summary{}}
	}
}

// MetricAggregationTemporality defines how a metric aggregator reports aggregated values.
// It describes how those values relate to the time interval over which they are aggregated.
type MetricAggregationTemporality int32

const (
	// MetricAggregationTemporalityUnspecified is the default MetricAggregationTemporality, it MUST NOT be used.
	MetricAggregationTemporalityUnspecified = MetricAggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED)
	// MetricAggregationTemporalityDelta is a MetricAggregationTemporality for a metric aggregator which reports changes since last report time.
	MetricAggregationTemporalityDelta = MetricAggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA)
	// MetricAggregationTemporalityCumulative is a MetricAggregationTemporality for a metric aggregator which reports changes since a fixed start time.
	MetricAggregationTemporalityCumulative = MetricAggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE)
)

// String returns the string representation of the MetricAggregationTemporality.
func (at MetricAggregationTemporality) String() string {
	return otlpmetrics.AggregationTemporality(at).String()
}

// MetricDataPointFlags defines how a metric aggregator reports aggregated values.
// It describes how those values relate to the time interval over which they are aggregated.
type MetricDataPointFlags uint32

const (
	// MetricDataPointFlagsNone is the default MetricDataPointFlags.
	MetricDataPointFlagsNone = MetricDataPointFlags(otlpmetrics.DataPointFlags_FLAG_NONE)
)

// NewMetricDataPointFlags returns a new MetricDataPointFlags combining the flags passed
// in as parameters.
func NewMetricDataPointFlags(flags ...MetricDataPointFlag) MetricDataPointFlags {
	var flag MetricDataPointFlags
	for _, f := range flags {
		flag |= MetricDataPointFlags(f)
	}
	return flag
}

// HasFlag returns true if the MetricDataPointFlags contains the specified flag
func (d MetricDataPointFlags) HasFlag(flag MetricDataPointFlag) bool {
	return d&MetricDataPointFlags(flag) != 0
}

// String returns the string representation of the MetricDataPointFlags.
func (d MetricDataPointFlags) String() string {
	return otlpmetrics.DataPointFlags(d).String()
}

// MetricDataPointFlag allow users to configure DataPointFlags. This is achieved via NewMetricDataPointFlags.
// The separation between MetricDataPointFlags and MetricDataPointFlag exists to prevent users accidentally
// comparing the value of individual flags with MetricDataPointFlags. Instead, users must use the HasFlag method.
type MetricDataPointFlag uint32

const (
	// MetricDataPointFlagNoRecordedValue is flag for a metric aggregator which reports changes since last report time.
	MetricDataPointFlagNoRecordedValue = MetricDataPointFlag(otlpmetrics.DataPointFlags_FLAG_NO_RECORDED_VALUE)
)

// MetricValueType specifies the type of NumberDataPoint.
type MetricValueType int32

const (
	MetricValueTypeNone MetricValueType = iota
	MetricValueTypeInt
	MetricValueTypeDouble
)

// String returns the string representation of the MetricValueType.
func (mdt MetricValueType) String() string {
	switch mdt {
	case MetricValueTypeNone:
		return "None"
	case MetricValueTypeInt:
		return "Int"
	case MetricValueTypeDouble:
		return "Double"
	}
	return ""
}

// Type returns the type of the value for this NumberDataPoint.
// Calling this function on zero-initialized NumberDataPoint will cause a panic.
func (ms NumberDataPoint) Type() MetricValueType {
	switch ms.orig.Value.(type) {
	case *otlpmetrics.NumberDataPoint_AsDouble:
		return MetricValueTypeDouble
	case *otlpmetrics.NumberDataPoint_AsInt:
		return MetricValueTypeInt
	}
	return MetricValueTypeNone
}

// Type returns the type of the value for this Exemplar.
// Calling this function on zero-initialized Exemplar will cause a panic.
func (ms Exemplar) Type() MetricValueType {
	switch ms.orig.Value.(type) {
	case *otlpmetrics.Exemplar_AsDouble:
		return MetricValueTypeDouble
	case *otlpmetrics.Exemplar_AsInt:
		return MetricValueTypeInt
	}
	return MetricValueTypeNone
}
