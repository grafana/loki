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

package internal

import (
	otlpcollectorlog "go.opentelemetry.io/collector/model/internal/data/protogen/collector/logs/v1"
	otlpcollectormetrics "go.opentelemetry.io/collector/model/internal/data/protogen/collector/metrics/v1"
	otlpcollectortrace "go.opentelemetry.io/collector/model/internal/data/protogen/collector/trace/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
	otlptrace "go.opentelemetry.io/collector/model/internal/data/protogen/trace/v1"
)

// MetricsWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Metrics data type to be callable by
// any code outside of this module.
type MetricsWrapper struct {
	req *otlpcollectormetrics.ExportMetricsServiceRequest
}

// MetricsToOtlp internal helper to convert MetricsWrapper to protobuf representation.
func MetricsToOtlp(mw MetricsWrapper) *otlpcollectormetrics.ExportMetricsServiceRequest {
	return mw.req
}

// MetricsFromOtlp internal helper to convert protobuf representation to MetricsWrapper.
func MetricsFromOtlp(req *otlpcollectormetrics.ExportMetricsServiceRequest) MetricsWrapper {
	MetricsCompatibilityChanges(req)
	return MetricsWrapper{req: req}
}

// MetricsCompatibilityChanges performs backward compatibility conversion on Metrics:
// - Convert IntHistogram to Histogram. See https://github.com/open-telemetry/opentelemetry-proto/blob/f3b0ee0861d304f8f3126686ba9b01c106069cb0/opentelemetry/proto/metrics/v1/metrics.proto#L170
// - Convert IntGauge to Gauge. See https://github.com/open-telemetry/opentelemetry-proto/blob/f3b0ee0861d304f8f3126686ba9b01c106069cb0/opentelemetry/proto/metrics/v1/metrics.proto#L156
// - Convert IntSum to Sum. See https://github.com/open-telemetry/opentelemetry-proto/blob/f3b0ee0861d304f8f3126686ba9b01c106069cb0/opentelemetry/proto/metrics/v1/metrics.proto#L156
func MetricsCompatibilityChanges(req *otlpcollectormetrics.ExportMetricsServiceRequest) {
	for _, rsm := range req.ResourceMetrics {
		for _, ilm := range rsm.InstrumentationLibraryMetrics {
			for _, metric := range ilm.Metrics {
				switch m := metric.Data.(type) {
				case *otlpmetrics.Metric_IntHistogram:
					metric.Data = intHistogramToHistogram(m)
				case *otlpmetrics.Metric_IntGauge:
					metric.Data = intGaugeToGauge(m)
				case *otlpmetrics.Metric_IntSum:
					metric.Data = intSumToSum(m)
				default:
				}
			}
		}
	}
}

// TracesWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Traces data type to be callable by
// any code outside of this module.
type TracesWrapper struct {
	req *otlpcollectortrace.ExportTraceServiceRequest
}

// TracesToOtlp internal helper to convert TracesWrapper to protobuf representation.
func TracesToOtlp(mw TracesWrapper) *otlpcollectortrace.ExportTraceServiceRequest {
	return mw.req
}

// TracesFromOtlp internal helper to convert protobuf representation to TracesWrapper.
func TracesFromOtlp(req *otlpcollectortrace.ExportTraceServiceRequest) TracesWrapper {
	TracesCompatibilityChanges(req)
	return TracesWrapper{req: req}
}

// TracesCompatibilityChanges performs backward compatibility conversion of Span Status code according to
// OTLP specification as we are a new receiver and sender (we are pushing data to the pipelines):
// See https://github.com/open-telemetry/opentelemetry-proto/blob/59c488bfb8fb6d0458ad6425758b70259ff4a2bd/opentelemetry/proto/trace/v1/trace.proto#L239
// See https://github.com/open-telemetry/opentelemetry-proto/blob/59c488bfb8fb6d0458ad6425758b70259ff4a2bd/opentelemetry/proto/trace/v1/trace.proto#L253
func TracesCompatibilityChanges(req *otlpcollectortrace.ExportTraceServiceRequest) {
	for _, rss := range req.ResourceSpans {
		for _, ils := range rss.InstrumentationLibrarySpans {
			for _, span := range ils.Spans {
				switch span.Status.Code {
				case otlptrace.Status_STATUS_CODE_UNSET:
					if span.Status.DeprecatedCode != otlptrace.Status_DEPRECATED_STATUS_CODE_OK {
						span.Status.Code = otlptrace.Status_STATUS_CODE_ERROR
					}
				case otlptrace.Status_STATUS_CODE_OK:
					// If status code is set then overwrites deprecated.
					span.Status.DeprecatedCode = otlptrace.Status_DEPRECATED_STATUS_CODE_OK
				case otlptrace.Status_STATUS_CODE_ERROR:
					span.Status.DeprecatedCode = otlptrace.Status_DEPRECATED_STATUS_CODE_UNKNOWN_ERROR
				}
			}
		}
	}
}

// LogsWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Logs data type to be callable by
// any code outside of this module.
type LogsWrapper struct {
	req *otlpcollectorlog.ExportLogsServiceRequest
}

// LogsToOtlp internal helper to convert LogsWrapper to protobuf representation.
func LogsToOtlp(l LogsWrapper) *otlpcollectorlog.ExportLogsServiceRequest {
	return l.req
}

// LogsFromOtlp internal helper to convert protobuf representation to LogsWrapper.
func LogsFromOtlp(req *otlpcollectorlog.ExportLogsServiceRequest) LogsWrapper {
	return LogsWrapper{req: req}
}

func intHistogramToHistogram(src *otlpmetrics.Metric_IntHistogram) *otlpmetrics.Metric_Histogram {
	datapoints := []*otlpmetrics.HistogramDataPoint{}
	for _, datapoint := range src.IntHistogram.DataPoints {
		datapoints = append(datapoints, &otlpmetrics.HistogramDataPoint{
			Labels:            datapoint.Labels,
			TimeUnixNano:      datapoint.TimeUnixNano,
			Count:             datapoint.Count,
			StartTimeUnixNano: datapoint.StartTimeUnixNano,
			Sum:               float64(datapoint.Sum),
			BucketCounts:      datapoint.BucketCounts,
			ExplicitBounds:    datapoint.ExplicitBounds,
			Exemplars:         intExemplarToExemplar(datapoint.Exemplars),
		})
	}
	return &otlpmetrics.Metric_Histogram{
		Histogram: &otlpmetrics.Histogram{
			AggregationTemporality: src.IntHistogram.GetAggregationTemporality(),
			DataPoints:             datapoints,
		},
	}
}

func intGaugeToGauge(src *otlpmetrics.Metric_IntGauge) *otlpmetrics.Metric_Gauge {
	datapoints := make([]*otlpmetrics.NumberDataPoint, len(src.IntGauge.DataPoints))
	for i, datapoint := range src.IntGauge.DataPoints {
		datapoints[i] = &otlpmetrics.NumberDataPoint{
			Labels:            datapoint.Labels,
			TimeUnixNano:      datapoint.TimeUnixNano,
			StartTimeUnixNano: datapoint.StartTimeUnixNano,
			Exemplars:         intExemplarToExemplar(datapoint.Exemplars),
			Value:             &otlpmetrics.NumberDataPoint_AsInt{AsInt: datapoint.Value},
		}
	}
	return &otlpmetrics.Metric_Gauge{
		Gauge: &otlpmetrics.Gauge{
			DataPoints: datapoints,
		},
	}
}

func intSumToSum(src *otlpmetrics.Metric_IntSum) *otlpmetrics.Metric_Sum {
	datapoints := make([]*otlpmetrics.NumberDataPoint, len(src.IntSum.DataPoints))
	for i, datapoint := range src.IntSum.DataPoints {
		datapoints[i] = &otlpmetrics.NumberDataPoint{
			Labels:            datapoint.Labels,
			TimeUnixNano:      datapoint.TimeUnixNano,
			StartTimeUnixNano: datapoint.StartTimeUnixNano,
			Exemplars:         intExemplarToExemplar(datapoint.Exemplars),
			Value:             &otlpmetrics.NumberDataPoint_AsInt{AsInt: datapoint.Value},
		}
	}
	return &otlpmetrics.Metric_Sum{
		Sum: &otlpmetrics.Sum{
			AggregationTemporality: src.IntSum.AggregationTemporality,
			DataPoints:             datapoints,
			IsMonotonic:            src.IntSum.IsMonotonic,
		},
	}
}

func intExemplarToExemplar(src []otlpmetrics.IntExemplar) []otlpmetrics.Exemplar { //nolint:staticcheck // SA1019 ignore this!
	exemplars := []otlpmetrics.Exemplar{}
	for _, exemplar := range src {
		exemplars = append(exemplars, otlpmetrics.Exemplar{
			FilteredLabels: exemplar.FilteredLabels,
			TimeUnixNano:   exemplar.TimeUnixNano,
			Value: &otlpmetrics.Exemplar_AsInt{
				AsInt: exemplar.Value,
			},
			SpanId:  exemplar.SpanId,
			TraceId: exemplar.TraceId,
		})
	}
	return exemplars
}
