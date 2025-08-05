// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

var _ Marshaler = (*JSONMarshaler)(nil)

// JSONMarshaler marshals pdata.Metrics to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalMetrics to the OTLP/JSON format.
func (*JSONMarshaler) MarshalMetrics(md Metrics) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	md.marshalJSONStream(dest)
	return slices.Clone(dest.Buffer()), dest.Error()
}

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to pdata.Metrics.
type JSONUnmarshaler struct{}

// UnmarshalMetrics from OTLP/JSON format into pdata.Metrics.
func (*JSONUnmarshaler) UnmarshalMetrics(buf []byte) (Metrics, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	md := NewMetrics()
	md.unmarshalJSONIter(iter)
	if iter.Error() != nil {
		return Metrics{}, iter.Error()
	}
	otlp.MigrateMetrics(md.getOrig().ResourceMetrics)
	return md, nil
}

func (ms ResourceMetrics) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "resource":
			internal.UnmarshalJSONIterResource(internal.NewResource(&ms.orig.Resource, ms.state), iter)
		case "scopeMetrics", "scope_metrics":
			ms.ScopeMetrics().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ScopeMetrics) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "scope":
			internal.UnmarshalJSONIterInstrumentationScope(internal.NewInstrumentationScope(&ms.orig.Scope, ms.state), iter)
		case "metrics":
			ms.Metrics().unmarshalJSONIter(iter)
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Metric) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "name":
			ms.orig.Name = iter.ReadString()
		case "description":
			ms.orig.Description = iter.ReadString()
		case "unit":
			ms.orig.Unit = iter.ReadString()
		case "metadata":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Metadata, ms.state), iter)
		case "sum":
			ms.SetEmptySum().unmarshalJSONIter(iter)
		case "gauge":
			ms.SetEmptyGauge().unmarshalJSONIter(iter)
		case "histogram":
			ms.SetEmptyHistogram().unmarshalJSONIter(iter)
		case "exponential_histogram", "exponentialHistogram":
			ms.SetEmptyExponentialHistogram().unmarshalJSONIter(iter)
		case "summary":
			ms.SetEmptySummary().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Sum) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "aggregation_temporality", "aggregationTemporality":
			ms.orig.AggregationTemporality = readAggregationTemporality(iter)
		case "is_monotonic", "isMonotonic":
			ms.orig.IsMonotonic = iter.ReadBool()
		case "data_points", "dataPoints":
			ms.DataPoints().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Gauge) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			ms.DataPoints().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Histogram) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			ms.DataPoints().unmarshalJSONIter(iter)
		case "aggregation_temporality", "aggregationTemporality":
			ms.orig.AggregationTemporality = readAggregationTemporality(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ExponentialHistogram) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			ms.DataPoints().unmarshalJSONIter(iter)
		case "aggregation_temporality", "aggregationTemporality":
			ms.orig.AggregationTemporality = readAggregationTemporality(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Summary) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			ms.DataPoints().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms NumberDataPoint) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "start_time_unix_nano", "startTimeUnixNano":
			ms.orig.StartTimeUnixNano = iter.ReadUint64()
		case "as_int", "asInt":
			ms.orig.Value = &otlpmetrics.NumberDataPoint_AsInt{
				AsInt: iter.ReadInt64(),
			}
		case "as_double", "asDouble":
			ms.orig.Value = &otlpmetrics.NumberDataPoint_AsDouble{
				AsDouble: iter.ReadFloat64(),
			}
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "exemplars":
			ms.Exemplars().unmarshalJSONIter(iter)
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms HistogramDataPoint) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "start_time_unix_nano", "startTimeUnixNano":
			ms.orig.StartTimeUnixNano = iter.ReadUint64()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "count":
			ms.orig.Count = iter.ReadUint64()
		case "sum":
			ms.orig.Sum_ = &otlpmetrics.HistogramDataPoint_Sum{Sum: iter.ReadFloat64()}
		case "bucket_counts", "bucketCounts":
			internal.UnmarshalJSONIterUInt64Slice(internal.NewUInt64Slice(&ms.orig.BucketCounts, ms.state), iter)
		case "explicit_bounds", "explicitBounds":
			internal.UnmarshalJSONIterFloat64Slice(internal.NewFloat64Slice(&ms.orig.ExplicitBounds, ms.state), iter)
		case "exemplars":
			ms.Exemplars().unmarshalJSONIter(iter)
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		case "max":
			ms.orig.Max_ = &otlpmetrics.HistogramDataPoint_Max{
				Max: iter.ReadFloat64(),
			}
		case "min":
			ms.orig.Min_ = &otlpmetrics.HistogramDataPoint_Min{
				Min: iter.ReadFloat64(),
			}
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ExponentialHistogramDataPoint) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "start_time_unix_nano", "startTimeUnixNano":
			ms.orig.StartTimeUnixNano = iter.ReadUint64()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "count":
			ms.orig.Count = iter.ReadUint64()
		case "sum":
			ms.orig.Sum_ = &otlpmetrics.ExponentialHistogramDataPoint_Sum{
				Sum: iter.ReadFloat64(),
			}
		case "scale":
			ms.orig.Scale = iter.ReadInt32()
		case "zero_count", "zeroCount":
			ms.orig.ZeroCount = iter.ReadUint64()
		case "positive":
			ms.Positive().unmarshalJSONIter(iter)
		case "negative":
			ms.Negative().unmarshalJSONIter(iter)
		case "exemplars":
			ms.Exemplars().unmarshalJSONIter(iter)
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		case "max":
			ms.orig.Max_ = &otlpmetrics.ExponentialHistogramDataPoint_Max{
				Max: iter.ReadFloat64(),
			}
		case "min":
			ms.orig.Min_ = &otlpmetrics.ExponentialHistogramDataPoint_Min{
				Min: iter.ReadFloat64(),
			}
		case "zeroThreshold", "zero_threshold":
			ms.orig.ZeroThreshold = iter.ReadFloat64()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms SummaryDataPoint) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "start_time_unix_nano", "startTimeUnixNano":
			ms.orig.StartTimeUnixNano = iter.ReadUint64()
		case "attributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.Attributes, ms.state), iter)
		case "count":
			ms.orig.Count = iter.ReadUint64()
		case "sum":
			ms.orig.Sum = iter.ReadFloat64()
		case "quantile_values", "quantileValues":
			ms.QuantileValues().unmarshalJSONIter(iter)
		case "flags":
			ms.orig.Flags = iter.ReadUint32()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ExponentialHistogramDataPointBuckets) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "bucket_counts", "bucketCounts":
			internal.UnmarshalJSONIterUInt64Slice(internal.NewUInt64Slice(&ms.orig.BucketCounts, ms.state), iter)
		case "offset":
			ms.orig.Offset = iter.ReadInt32()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms SummaryDataPointValueAtQuantile) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "quantile":
			ms.orig.Quantile = iter.ReadFloat64()
		case "value":
			ms.orig.Value = iter.ReadFloat64()
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms Exemplar) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "filtered_attributes", "filteredAttributes":
			internal.UnmarshalJSONIterMap(internal.NewMap(&ms.orig.FilteredAttributes, ms.state), iter)
		case "timeUnixNano", "time_unix_nano":
			ms.orig.TimeUnixNano = iter.ReadUint64()
		case "as_int", "asInt":
			ms.orig.Value = &otlpmetrics.Exemplar_AsInt{
				AsInt: iter.ReadInt64(),
			}
		case "as_double", "asDouble":
			ms.orig.Value = &otlpmetrics.Exemplar_AsDouble{
				AsDouble: iter.ReadFloat64(),
			}
		case "traceId", "trace_id":
			ms.orig.TraceId.UnmarshalJSONIter(iter)
		case "spanId", "span_id":
			ms.orig.SpanId.UnmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func readAggregationTemporality(iter *json.Iterator) otlpmetrics.AggregationTemporality {
	return otlpmetrics.AggregationTemporality(iter.ReadEnumValue(otlpmetrics.AggregationTemporality_value))
}
