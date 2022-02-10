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

package obsreport // import "go.opentelemetry.io/collector/obsreport"

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/internal/obsreportconfig"
	"go.opentelemetry.io/collector/internal/obsreportconfig/obsmetrics"
)

// Exporter is a helper to add observability to a component.Exporter.
type Exporter struct {
	level          configtelemetry.Level
	spanNamePrefix string
	mutators       []tag.Mutator
	tracer         trace.Tracer
}

// ExporterSettings are settings for creating an Exporter.
type ExporterSettings struct {
	Level                  configtelemetry.Level
	ExporterID             config.ComponentID
	ExporterCreateSettings component.ExporterCreateSettings
}

// NewExporter creates a new Exporter.
func NewExporter(cfg ExporterSettings) *Exporter {
	return &Exporter{
		level:          cfg.Level,
		spanNamePrefix: obsmetrics.ExporterPrefix + cfg.ExporterID.String(),
		mutators:       []tag.Mutator{tag.Upsert(obsmetrics.TagKeyExporter, cfg.ExporterID.String(), tag.WithTTL(tag.TTLNoPropagation))},
		tracer:         cfg.ExporterCreateSettings.TracerProvider.Tracer(cfg.ExporterID.String()),
	}
}

// StartTracesOp is called at the start of an Export operation.
// The returned context should be used in other calls to the Exporter functions
// dealing with the same export operation.
func (exp *Exporter) StartTracesOp(ctx context.Context) context.Context {
	return exp.startOp(ctx, obsmetrics.ExportTraceDataOperationSuffix)
}

// EndTracesOp completes the export operation that was started with StartTracesOp.
func (exp *Exporter) EndTracesOp(ctx context.Context, numSpans int, err error) {
	numSent, numFailedToSend := toNumItems(numSpans, err)
	exp.recordMetrics(ctx, numSent, numFailedToSend, obsmetrics.ExporterSentSpans, obsmetrics.ExporterFailedToSendSpans)
	endSpan(ctx, err, numSent, numFailedToSend, obsmetrics.SentSpansKey, obsmetrics.FailedToSendSpansKey)
}

// StartMetricsOp is called at the start of an Export operation.
// The returned context should be used in other calls to the Exporter functions
// dealing with the same export operation.
func (exp *Exporter) StartMetricsOp(ctx context.Context) context.Context {
	return exp.startOp(ctx, obsmetrics.ExportMetricsOperationSuffix)
}

// EndMetricsOp completes the export operation that was started with
// StartMetricsOp.
func (exp *Exporter) EndMetricsOp(ctx context.Context, numMetricPoints int, err error) {
	numSent, numFailedToSend := toNumItems(numMetricPoints, err)
	exp.recordMetrics(ctx, numSent, numFailedToSend, obsmetrics.ExporterSentMetricPoints, obsmetrics.ExporterFailedToSendMetricPoints)
	endSpan(ctx, err, numSent, numFailedToSend, obsmetrics.SentMetricPointsKey, obsmetrics.FailedToSendMetricPointsKey)
}

// StartLogsOp is called at the start of an Export operation.
// The returned context should be used in other calls to the Exporter functions
// dealing with the same export operation.
func (exp *Exporter) StartLogsOp(ctx context.Context) context.Context {
	return exp.startOp(ctx, obsmetrics.ExportLogsOperationSuffix)
}

// EndLogsOp completes the export operation that was started with StartLogsOp.
func (exp *Exporter) EndLogsOp(ctx context.Context, numLogRecords int, err error) {
	numSent, numFailedToSend := toNumItems(numLogRecords, err)
	exp.recordMetrics(ctx, numSent, numFailedToSend, obsmetrics.ExporterSentLogRecords, obsmetrics.ExporterFailedToSendLogRecords)
	endSpan(ctx, err, numSent, numFailedToSend, obsmetrics.SentLogRecordsKey, obsmetrics.FailedToSendLogRecordsKey)
}

// startOp creates the span used to trace the operation. Returning
// the updated context and the created span.
func (exp *Exporter) startOp(ctx context.Context, operationSuffix string) context.Context {
	spanName := exp.spanNamePrefix + operationSuffix
	ctx, _ = exp.tracer.Start(ctx, spanName)
	return ctx
}

func (exp *Exporter) recordMetrics(ctx context.Context, numSent, numFailedToSend int64, sentMeasure, failedToSendMeasure *stats.Int64Measure) {
	if obsreportconfig.Level() == configtelemetry.LevelNone {
		return
	}
	// Ignore the error for now. This should not happen.
	if numFailedToSend > 0 {
		_ = stats.RecordWithTags(ctx, exp.mutators, sentMeasure.M(numSent), failedToSendMeasure.M(numFailedToSend))
	} else {
		_ = stats.RecordWithTags(ctx, exp.mutators, sentMeasure.M(numSent))
	}
}

func endSpan(ctx context.Context, err error, numSent, numFailedToSend int64, sentItemsKey, failedToSendItemsKey string) {
	span := trace.SpanFromContext(ctx)
	// End the span according to errors.
	if span.IsRecording() {
		span.SetAttributes(
			attribute.Int64(sentItemsKey, numSent),
			attribute.Int64(failedToSendItemsKey, numFailedToSend),
		)
		recordError(span, err)
	}
	span.End()
}

func toNumItems(numExportedItems int, err error) (int64, int64) {
	if err != nil {
		return 0, int64(numExportedItems)
	}
	return int64(numExportedItems), 0
}
