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

package pmetricjson // import "go.opentelemetry.io/collector/pdata/pmetric/internal/pmetricjson"

import (
	"fmt"

	"github.com/gogo/protobuf/jsonpb"
	jsoniter "github.com/json-iterator/go"

	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

var JSONMarshaler = &jsonpb.Marshaler{
	// https://github.com/open-telemetry/opentelemetry-specification/pull/2758
	EnumsAsInts: true,
	// https://github.com/open-telemetry/opentelemetry-specification/pull/2829
	OrigName: false,
}

func UnmarshalMetricsData(buf []byte, dest *otlpmetrics.MetricsData) error {
	iter := jsoniter.ConfigFastest.BorrowIterator(buf)
	defer jsoniter.ConfigFastest.ReturnIterator(iter)
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "resource_metrics", "resourceMetrics":
			iter.ReadArrayCB(func(iterator *jsoniter.Iterator) bool {
				dest.ResourceMetrics = append(dest.ResourceMetrics, readResourceMetrics(iter))
				return true
			})
		default:
			iter.Skip()
		}
		return true
	})
	otlp.MigrateMetrics(dest.ResourceMetrics)
	return iter.Error
}

func UnmarshalExportMetricsServiceRequest(buf []byte, dest *otlpcollectormetrics.ExportMetricsServiceRequest) error {
	iter := jsoniter.ConfigFastest.BorrowIterator(buf)
	defer jsoniter.ConfigFastest.ReturnIterator(iter)
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "resource_metrics", "resourceMetrics":
			iter.ReadArrayCB(func(iterator *jsoniter.Iterator) bool {
				dest.ResourceMetrics = append(dest.ResourceMetrics, readResourceMetrics(iter))
				return true
			})
		default:
			iter.Skip()
		}
		return true
	})
	otlp.MigrateMetrics(dest.ResourceMetrics)
	return iter.Error
}

func UnmarshalExportMetricsServiceResponse(buf []byte, dest *otlpcollectormetrics.ExportMetricsServiceResponse) error {
	iter := jsoniter.ConfigFastest.BorrowIterator(buf)
	defer jsoniter.ConfigFastest.ReturnIterator(iter)
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "partial_success", "partialSuccess":
			dest.PartialSuccess = readExportMetricsPartialSuccess(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return iter.Error
}

func readResourceMetrics(iter *jsoniter.Iterator) *otlpmetrics.ResourceMetrics {
	rs := &otlpmetrics.ResourceMetrics{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "resource":
			json.ReadResource(iter, &rs.Resource)
		case "scopeMetrics", "scope_metrics":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				rs.ScopeMetrics = append(rs.ScopeMetrics,
					readScopeMetrics(iter))
				return true
			})
		case "schemaUrl", "schema_url":
			rs.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
	return rs
}

func readScopeMetrics(iter *jsoniter.Iterator) *otlpmetrics.ScopeMetrics {
	ils := &otlpmetrics.ScopeMetrics{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "scope":
			json.ReadScope(iter, &ils.Scope)
		case "metrics":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				ils.Metrics = append(ils.Metrics, readMetric(iter))
				return true
			})
		case "schemaUrl", "schema_url":
			ils.SchemaUrl = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
	return ils
}

func readMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric {
	sp := &otlpmetrics.Metric{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "name":
			sp.Name = iter.ReadString()
		case "description":
			sp.Description = iter.ReadString()
		case "unit":
			sp.Unit = iter.ReadString()
		case "sum":
			sp.Data = readSumMetric(iter)
		case "gauge":
			sp.Data = readGaugeMetric(iter)
		case "histogram":
			sp.Data = readHistogramMetric(iter)
		case "exponential_histogram", "exponentialHistogram":
			sp.Data = readExponentialHistogramMetric(iter)
		case "summary":
			sp.Data = readSummaryMetric(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return sp
}

func readSumMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric_Sum {
	data := &otlpmetrics.Metric_Sum{
		Sum: &otlpmetrics.Sum{},
	}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "aggregation_temporality", "aggregationTemporality":
			data.Sum.AggregationTemporality = readAggregationTemporality(iter)
		case "is_monotonic", "isMonotonic":
			data.Sum.IsMonotonic = iter.ReadBool()
		case "data_points", "dataPoints":
			var dataPoints []*otlpmetrics.NumberDataPoint
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				dataPoints = append(dataPoints, readNumberDataPoint(iter))
				return true
			})
			data.Sum.DataPoints = dataPoints
		default:
			iter.Skip()
		}
		return true
	})
	return data
}

func readGaugeMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric_Gauge {
	data := &otlpmetrics.Metric_Gauge{
		Gauge: &otlpmetrics.Gauge{},
	}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			var dataPoints []*otlpmetrics.NumberDataPoint
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				dataPoints = append(dataPoints, readNumberDataPoint(iter))
				return true
			})
			data.Gauge.DataPoints = dataPoints
		default:
			iter.Skip()
		}
		return true
	})
	return data
}

func readHistogramMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric_Histogram {
	data := &otlpmetrics.Metric_Histogram{
		Histogram: &otlpmetrics.Histogram{},
	}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			var dataPoints []*otlpmetrics.HistogramDataPoint
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				dataPoints = append(dataPoints, readHistogramDataPoint(iter))
				return true
			})
			data.Histogram.DataPoints = dataPoints
		case "aggregation_temporality", "aggregationTemporality":
			data.Histogram.AggregationTemporality = readAggregationTemporality(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return data
}

func readExponentialHistogramMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric_ExponentialHistogram {
	data := &otlpmetrics.Metric_ExponentialHistogram{
		ExponentialHistogram: &otlpmetrics.ExponentialHistogram{},
	}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				data.ExponentialHistogram.DataPoints = append(data.ExponentialHistogram.DataPoints,
					readExponentialHistogramDataPoint(iter))
				return true
			})
		case "aggregation_temporality", "aggregationTemporality":
			data.ExponentialHistogram.AggregationTemporality = readAggregationTemporality(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return data
}

func readSummaryMetric(iter *jsoniter.Iterator) *otlpmetrics.Metric_Summary {
	data := &otlpmetrics.Metric_Summary{
		Summary: &otlpmetrics.Summary{},
	}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "data_points", "dataPoints":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				data.Summary.DataPoints = append(data.Summary.DataPoints,
					readSummaryDataPoint(iter))
				return true
			})
		default:
			iter.Skip()
		}
		return true
	})
	return data
}

func readExemplar(iter *jsoniter.Iterator) otlpmetrics.Exemplar {
	exemplar := otlpmetrics.Exemplar{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "filtered_attributes", "filteredAttributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				exemplar.FilteredAttributes = append(exemplar.FilteredAttributes, json.ReadAttribute(iter))
				return true
			})
		case "timeUnixNano", "time_unix_nano":
			exemplar.TimeUnixNano = json.ReadUint64(iter)
		case "as_int", "asInt":
			exemplar.Value = &otlpmetrics.Exemplar_AsInt{
				AsInt: json.ReadInt64(iter),
			}
		case "as_double", "asDouble":
			exemplar.Value = &otlpmetrics.Exemplar_AsDouble{
				AsDouble: json.ReadFloat64(iter),
			}
		case "traceId", "trace_id":
			if err := exemplar.TraceId.UnmarshalJSON([]byte(iter.ReadString())); err != nil {
				iter.ReportError("exemplar.traceId", fmt.Sprintf("parse trace_id:%v", err))
			}
		case "spanId", "span_id":
			if err := exemplar.SpanId.UnmarshalJSON([]byte(iter.ReadString())); err != nil {
				iter.ReportError("exemplar.spanId", fmt.Sprintf("parse span_id:%v", err))
			}
		default:
			iter.Skip()
		}
		return true
	})
	return exemplar
}

func readNumberDataPoint(iter *jsoniter.Iterator) *otlpmetrics.NumberDataPoint {
	point := &otlpmetrics.NumberDataPoint{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			point.TimeUnixNano = json.ReadUint64(iter)
		case "start_time_unix_nano", "startTimeUnixNano":
			point.StartTimeUnixNano = json.ReadUint64(iter)
		case "as_int", "asInt":
			point.Value = &otlpmetrics.NumberDataPoint_AsInt{
				AsInt: json.ReadInt64(iter),
			}
		case "as_double", "asDouble":
			point.Value = &otlpmetrics.NumberDataPoint_AsDouble{
				AsDouble: json.ReadFloat64(iter),
			}
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Attributes = append(point.Attributes, json.ReadAttribute(iter))
				return true
			})
		case "exemplars":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Exemplars = append(point.Exemplars, readExemplar(iter))
				return true
			})
		case "flags":
			point.Flags = json.ReadUint32(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return point
}

func readHistogramDataPoint(iter *jsoniter.Iterator) *otlpmetrics.HistogramDataPoint {
	point := &otlpmetrics.HistogramDataPoint{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			point.TimeUnixNano = json.ReadUint64(iter)
		case "start_time_unix_nano", "startTimeUnixNano":
			point.StartTimeUnixNano = json.ReadUint64(iter)
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Attributes = append(point.Attributes, json.ReadAttribute(iter))
				return true
			})
		case "count":
			point.Count = json.ReadUint64(iter)
		case "sum":
			point.Sum_ = &otlpmetrics.HistogramDataPoint_Sum{Sum: json.ReadFloat64(iter)}
		case "bucket_counts", "bucketCounts":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.BucketCounts = append(point.BucketCounts, json.ReadUint64(iter))
				return true
			})
		case "explicit_bounds", "explicitBounds":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.ExplicitBounds = append(point.ExplicitBounds, json.ReadFloat64(iter))
				return true
			})
		case "exemplars":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Exemplars = append(point.Exemplars, readExemplar(iter))
				return true
			})
		case "flags":
			point.Flags = json.ReadUint32(iter)
		case "max":
			point.Max_ = &otlpmetrics.HistogramDataPoint_Max{
				Max: json.ReadFloat64(iter),
			}
		case "min":
			point.Min_ = &otlpmetrics.HistogramDataPoint_Min{
				Min: json.ReadFloat64(iter),
			}
		default:
			iter.Skip()
		}
		return true
	})
	return point
}

func readExponentialHistogramDataPoint(iter *jsoniter.Iterator) *otlpmetrics.ExponentialHistogramDataPoint {
	point := &otlpmetrics.ExponentialHistogramDataPoint{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			point.TimeUnixNano = json.ReadUint64(iter)
		case "start_time_unix_nano", "startTimeUnixNano":
			point.StartTimeUnixNano = json.ReadUint64(iter)
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Attributes = append(point.Attributes, json.ReadAttribute(iter))
				return true
			})
		case "count":
			point.Count = json.ReadUint64(iter)
		case "sum":
			point.Sum_ = &otlpmetrics.ExponentialHistogramDataPoint_Sum{
				Sum: json.ReadFloat64(iter),
			}
		case "scale":
			point.Scale = iter.ReadInt32()
		case "zero_count", "zeroCount":
			point.ZeroCount = json.ReadUint64(iter)
		case "positive":
			point.Positive = readExponentialHistogramBuckets(iter)
		case "negative":
			point.Negative = readExponentialHistogramBuckets(iter)
		case "exemplars":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Exemplars = append(point.Exemplars, readExemplar(iter))
				return true
			})
		case "flags":
			point.Flags = json.ReadUint32(iter)
		case "max":
			point.Max_ = &otlpmetrics.ExponentialHistogramDataPoint_Max{
				Max: json.ReadFloat64(iter),
			}
		case "min":
			point.Min_ = &otlpmetrics.ExponentialHistogramDataPoint_Min{
				Min: json.ReadFloat64(iter),
			}
		default:
			iter.Skip()
		}
		return true
	})
	return point
}

func readSummaryDataPoint(iter *jsoniter.Iterator) *otlpmetrics.SummaryDataPoint {
	point := &otlpmetrics.SummaryDataPoint{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "timeUnixNano", "time_unix_nano":
			point.TimeUnixNano = json.ReadUint64(iter)
		case "start_time_unix_nano", "startTimeUnixNano":
			point.StartTimeUnixNano = json.ReadUint64(iter)
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.Attributes = append(point.Attributes, json.ReadAttribute(iter))
				return true
			})
		case "count":
			point.Count = json.ReadUint64(iter)
		case "sum":
			point.Sum = json.ReadFloat64(iter)
		case "quantile_values", "quantileValues":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				point.QuantileValues = append(point.QuantileValues, readQuantileValue(iter))
				return true
			})
		case "flags":
			point.Flags = json.ReadUint32(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return point
}

func readExponentialHistogramBuckets(iter *jsoniter.Iterator) otlpmetrics.ExponentialHistogramDataPoint_Buckets {
	buckets := otlpmetrics.ExponentialHistogramDataPoint_Buckets{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "bucket_counts", "bucketCounts":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				buckets.BucketCounts = append(buckets.BucketCounts, json.ReadUint64(iter))
				return true
			})
		case "offset":
			buckets.Offset = iter.ReadInt32()
		default:
			iter.Skip()
		}
		return true
	})
	return buckets
}

func readQuantileValue(iter *jsoniter.Iterator) *otlpmetrics.SummaryDataPoint_ValueAtQuantile {
	point := &otlpmetrics.SummaryDataPoint_ValueAtQuantile{}
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "quantile":
			point.Quantile = json.ReadFloat64(iter)
		case "value":
			point.Value = json.ReadFloat64(iter)
		default:
			iter.Skip()
		}
		return true
	})
	return point
}

func readAggregationTemporality(iter *jsoniter.Iterator) otlpmetrics.AggregationTemporality {
	return otlpmetrics.AggregationTemporality(json.ReadEnumValue(iter, otlpmetrics.AggregationTemporality_value))
}

func readExportMetricsPartialSuccess(iter *jsoniter.Iterator) otlpcollectormetrics.ExportMetricsPartialSuccess {
	lpr := otlpcollectormetrics.ExportMetricsPartialSuccess{}
	iter.ReadObjectCB(func(iterator *jsoniter.Iterator, f string) bool {
		switch f {
		case "rejected_data_points", "rejectedDataPoints":
			lpr.RejectedDataPoints = json.ReadInt64(iter)
		case "error_message", "errorMessage":
			lpr.ErrorMessage = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
	return lpr
}
