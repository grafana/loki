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

package pdata

import (
	"go.opentelemetry.io/collector/model/internal"
	otlpcollectormetrics "go.opentelemetry.io/collector/model/internal/data/protogen/collector/metrics/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
)

// MetricsMarshaler marshals pdata.Metrics into bytes.
type MetricsMarshaler interface {
	// MarshalMetrics the given pdata.Metrics into bytes.
	// If the error is not nil, the returned bytes slice cannot be used.
	MarshalMetrics(td Metrics) ([]byte, error)
}

// MetricsUnmarshaler unmarshalls bytes into pdata.Metrics.
type MetricsUnmarshaler interface {
	// UnmarshalMetrics the given bytes into pdata.Metrics.
	// If the error is not nil, the returned pdata.Metrics cannot be used.
	UnmarshalMetrics(buf []byte) (Metrics, error)
}

// Metrics is an opaque interface that allows transition to the new internal Metrics data, but also facilitates the
// transition to the new components, especially for traces.
//
// Outside of the core repository, the metrics pipeline cannot be converted to the new model since data.MetricData is
// part of the internal package.
type Metrics struct {
	orig *otlpcollectormetrics.ExportMetricsServiceRequest
}

// NewMetrics creates a new Metrics.
func NewMetrics() Metrics {
	return Metrics{orig: &otlpcollectormetrics.ExportMetricsServiceRequest{}}
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

// OtlpProtoSize returns the size in bytes of this Metrics encoded as OTLP Collector
// ExportMetricsServiceRequest ProtoBuf bytes.
func (md Metrics) OtlpProtoSize() int {
	return md.orig.Size()
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
				case MetricDataTypeIntGauge:
					dataPointCount += m.IntGauge().DataPoints().Len()
				case MetricDataTypeGauge:
					dataPointCount += m.Gauge().DataPoints().Len()
				case MetricDataTypeIntSum:
					dataPointCount += m.IntSum().DataPoints().Len()
				case MetricDataTypeSum:
					dataPointCount += m.Sum().DataPoints().Len()
				case MetricDataTypeHistogram:
					dataPointCount += m.Histogram().DataPoints().Len()
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
	MetricDataTypeIntGauge
	MetricDataTypeGauge
	MetricDataTypeIntSum
	MetricDataTypeSum
	MetricDataTypeHistogram
	MetricDataTypeSummary
)

// String returns the string representation of the MetricDataType.
func (mdt MetricDataType) String() string {
	switch mdt {
	case MetricDataTypeNone:
		return "None"
	case MetricDataTypeIntGauge:
		return "IntGauge"
	case MetricDataTypeGauge:
		return "Gauge"
	case MetricDataTypeIntSum:
		return "IntSum"
	case MetricDataTypeSum:
		return "Sum"
	case MetricDataTypeHistogram:
		return "Histogram"
	case MetricDataTypeSummary:
		return "Summary"
	}
	return ""
}

// DataType returns the type of the data for this Metric.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) DataType() MetricDataType {
	switch ms.orig.Data.(type) {
	case *otlpmetrics.Metric_IntGauge:
		return MetricDataTypeIntGauge
	case *otlpmetrics.Metric_Gauge:
		return MetricDataTypeGauge
	case *otlpmetrics.Metric_IntSum:
		return MetricDataTypeIntSum
	case *otlpmetrics.Metric_Sum:
		return MetricDataTypeSum
	case *otlpmetrics.Metric_Histogram:
		return MetricDataTypeHistogram
	case *otlpmetrics.Metric_Summary:
		return MetricDataTypeSummary
	}
	return MetricDataTypeNone
}

// SetDataType clears any existing data and initialize it with an empty data of the given type.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) SetDataType(ty MetricDataType) {
	switch ty {
	case MetricDataTypeIntGauge:
		ms.orig.Data = &otlpmetrics.Metric_IntGauge{IntGauge: &otlpmetrics.IntGauge{}}
	case MetricDataTypeGauge:
		ms.orig.Data = &otlpmetrics.Metric_Gauge{Gauge: &otlpmetrics.Gauge{}}
	case MetricDataTypeIntSum:
		ms.orig.Data = &otlpmetrics.Metric_IntSum{IntSum: &otlpmetrics.IntSum{}}
	case MetricDataTypeSum:
		ms.orig.Data = &otlpmetrics.Metric_Sum{Sum: &otlpmetrics.Sum{}}
	case MetricDataTypeHistogram:
		ms.orig.Data = &otlpmetrics.Metric_Histogram{Histogram: &otlpmetrics.Histogram{}}
	case MetricDataTypeSummary:
		ms.orig.Data = &otlpmetrics.Metric_Summary{Summary: &otlpmetrics.Summary{}}
	}
}

// IntGauge returns the data as IntGauge.
// Calling this function when DataType() != MetricDataTypeIntGauge will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) IntGauge() IntGauge {
	return newIntGauge(ms.orig.Data.(*otlpmetrics.Metric_IntGauge).IntGauge)
}

// Gauge returns the data as Gauge.
// Calling this function when DataType() != MetricDataTypeGauge will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) Gauge() Gauge {
	return newGauge(ms.orig.Data.(*otlpmetrics.Metric_Gauge).Gauge)
}

// IntSum returns the data as IntSum.
// Calling this function when DataType() != MetricDataTypeIntSum  will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) IntSum() IntSum {
	return newIntSum(ms.orig.Data.(*otlpmetrics.Metric_IntSum).IntSum)
}

// Sum returns the data as Sum.
// Calling this function when DataType() != MetricDataTypeSum will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) Sum() Sum {
	return newSum(ms.orig.Data.(*otlpmetrics.Metric_Sum).Sum)
}

// Histogram returns the data as Histogram.
// Calling this function when DataType() != MetricDataTypeHistogram will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) Histogram() Histogram {
	return newHistogram(ms.orig.Data.(*otlpmetrics.Metric_Histogram).Histogram)
}

// Summary returns the data as Summary.
// Calling this function when DataType() != MetricDataTypeSummary will cause a panic.
// Calling this function on zero-initialized Metric will cause a panic.
func (ms Metric) Summary() Summary {
	return newSummary(ms.orig.Data.(*otlpmetrics.Metric_Summary).Summary)
}

func copyData(src, dest *otlpmetrics.Metric) {
	switch srcData := (src).Data.(type) {
	case *otlpmetrics.Metric_IntGauge:
		data := &otlpmetrics.Metric_IntGauge{IntGauge: &otlpmetrics.IntGauge{}}
		newIntGauge(srcData.IntGauge).CopyTo(newIntGauge(data.IntGauge))
		dest.Data = data
	case *otlpmetrics.Metric_Gauge:
		data := &otlpmetrics.Metric_Gauge{Gauge: &otlpmetrics.Gauge{}}
		newGauge(srcData.Gauge).CopyTo(newGauge(data.Gauge))
		dest.Data = data
	case *otlpmetrics.Metric_IntSum:
		data := &otlpmetrics.Metric_IntSum{IntSum: &otlpmetrics.IntSum{}}
		newIntSum(srcData.IntSum).CopyTo(newIntSum(data.IntSum))
		dest.Data = data
	case *otlpmetrics.Metric_Sum:
		data := &otlpmetrics.Metric_Sum{Sum: &otlpmetrics.Sum{}}
		newSum(srcData.Sum).CopyTo(newSum(data.Sum))
		dest.Data = data
	case *otlpmetrics.Metric_Histogram:
		data := &otlpmetrics.Metric_Histogram{Histogram: &otlpmetrics.Histogram{}}
		newHistogram(srcData.Histogram).CopyTo(newHistogram(data.Histogram))
		dest.Data = data
	case *otlpmetrics.Metric_Summary:
		data := &otlpmetrics.Metric_Summary{Summary: &otlpmetrics.Summary{}}
		newSummary(srcData.Summary).CopyTo(newSummary(data.Summary))
		dest.Data = data
	}
}

// AggregationTemporality defines how a metric aggregator reports aggregated values.
// It describes how those values relate to the time interval over which they are aggregated.
type AggregationTemporality int32

const (
	// AggregationTemporalityUnspecified is the default AggregationTemporality, it MUST NOT be used.
	AggregationTemporalityUnspecified = AggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED)
	// AggregationTemporalityDelta is an AggregationTemporality for a metric aggregator which reports changes since last report time.
	AggregationTemporalityDelta = AggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA)
	// AggregationTemporalityCumulative is an AggregationTemporality for a metric aggregator which reports changes since a fixed start time.
	AggregationTemporalityCumulative = AggregationTemporality(otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE)
)

// String returns the string representation of the AggregationTemporality.
func (at AggregationTemporality) String() string {
	return otlpmetrics.AggregationTemporality(at).String()
}

// MetricValueType specifies the type of NumberDataPoint.
type MetricValueType int32

const (
	MetricValueTypeNone MetricValueType = iota
	MetricValueTypeInt
	MetricValueTypeDouble
)

// Type returns the type of the value for this NumberDataPoint.
// Calling this function on zero-initialized NumberDataPoint will cause a panic.
func (ms NumberDataPoint) Type() MetricValueType {
	if ms.orig.Value == nil {
		return MetricValueTypeNone
	}
	switch ms.orig.Value.(type) {
	case *otlpmetrics.NumberDataPoint_AsDouble:
		return MetricValueTypeDouble
	case *otlpmetrics.NumberDataPoint_AsInt:
		return MetricValueTypeInt
	}
	return MetricValueTypeNone
}

// Value returns the value associated with this NumberDataPoint.
// Deprecated: Use DoubleVal instead.
func (ms NumberDataPoint) Value() float64 {
	return ms.DoubleVal()
}

// SetValue replaces the value associated with this NumberDataPoint.
// Deprecated: Use SetDoubleVal instead.
func (ms NumberDataPoint) SetValue(v float64) {
	ms.SetDoubleVal(v)
}

// Type returns the type of the value for this Exemplar.
// Calling this function on zero-initialized Exemplar will cause a panic.
func (ms Exemplar) Type() MetricValueType {
	if ms.orig.Value == nil {
		return MetricValueTypeNone
	}
	switch ms.orig.Value.(type) {
	case *otlpmetrics.Exemplar_AsDouble:
		return MetricValueTypeDouble
	case *otlpmetrics.Exemplar_AsInt:
		return MetricValueTypeInt
	}
	return MetricValueTypeNone
}

// Value returns the value associated with this Exemplar.
// Deprecated: Use DoubleVal instead.
func (ms Exemplar) Value() float64 {
	return ms.DoubleVal()
}

// SetValue replaces the value associated with this Exemplar.
// Deprecated: Use SetDoubleVal instead.
func (ms Exemplar) SetValue(v float64) {
	ms.SetDoubleVal(v)
}
