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
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/internal/obsreportconfig/obsmetrics"
)

// BuildProcessorCustomMetricName is used to be build a metric name following
// the standards used in the Collector. The configType should be the same
// value used to identify the type on the config.
func BuildProcessorCustomMetricName(configType, metric string) string {
	componentPrefix := obsmetrics.ProcessorPrefix
	if !strings.HasSuffix(componentPrefix, obsmetrics.NameSep) {
		componentPrefix += obsmetrics.NameSep
	}
	if configType == "" {
		return componentPrefix
	}
	return componentPrefix + configType + obsmetrics.NameSep + metric
}

// Processor is a helper to add observability to a component.Processor.
type Processor struct {
	level    configtelemetry.Level
	mutators []tag.Mutator
}

// ProcessorSettings are settings for creating a Processor.
type ProcessorSettings struct {
	Level                   configtelemetry.Level
	ProcessorID             config.ComponentID
	ProcessorCreateSettings component.ProcessorCreateSettings
}

// NewProcessor creates a new Processor.
func NewProcessor(cfg ProcessorSettings) *Processor {
	return &Processor{
		level:    cfg.Level,
		mutators: []tag.Mutator{tag.Upsert(obsmetrics.TagKeyProcessor, cfg.ProcessorID.String(), tag.WithTTL(tag.TTLNoPropagation))},
	}
}

// TracesAccepted reports that the trace data was accepted.
func (por *Processor) TracesAccepted(ctx context.Context, numSpans int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedSpans.M(int64(numSpans)),
			obsmetrics.ProcessorRefusedSpans.M(0),
			obsmetrics.ProcessorDroppedSpans.M(0),
		)
	}
}

// TracesRefused reports that the trace data was refused.
func (por *Processor) TracesRefused(ctx context.Context, numSpans int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedSpans.M(0),
			obsmetrics.ProcessorRefusedSpans.M(int64(numSpans)),
			obsmetrics.ProcessorDroppedSpans.M(0),
		)
	}
}

// TracesDropped reports that the trace data was dropped.
func (por *Processor) TracesDropped(ctx context.Context, numSpans int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedSpans.M(0),
			obsmetrics.ProcessorRefusedSpans.M(0),
			obsmetrics.ProcessorDroppedSpans.M(int64(numSpans)),
		)
	}
}

// MetricsAccepted reports that the metrics were accepted.
func (por *Processor) MetricsAccepted(ctx context.Context, numPoints int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedMetricPoints.M(int64(numPoints)),
			obsmetrics.ProcessorRefusedMetricPoints.M(0),
			obsmetrics.ProcessorDroppedMetricPoints.M(0),
		)
	}
}

// MetricsRefused reports that the metrics were refused.
func (por *Processor) MetricsRefused(ctx context.Context, numPoints int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedMetricPoints.M(0),
			obsmetrics.ProcessorRefusedMetricPoints.M(int64(numPoints)),
			obsmetrics.ProcessorDroppedMetricPoints.M(0),
		)
	}
}

// MetricsDropped reports that the metrics were dropped.
func (por *Processor) MetricsDropped(ctx context.Context, numPoints int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedMetricPoints.M(0),
			obsmetrics.ProcessorRefusedMetricPoints.M(0),
			obsmetrics.ProcessorDroppedMetricPoints.M(int64(numPoints)),
		)
	}
}

// LogsAccepted reports that the logs were accepted.
func (por *Processor) LogsAccepted(ctx context.Context, numRecords int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedLogRecords.M(int64(numRecords)),
			obsmetrics.ProcessorRefusedLogRecords.M(0),
			obsmetrics.ProcessorDroppedLogRecords.M(0),
		)
	}
}

// LogsRefused reports that the logs were refused.
func (por *Processor) LogsRefused(ctx context.Context, numRecords int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedLogRecords.M(0),
			obsmetrics.ProcessorRefusedLogRecords.M(int64(numRecords)),
			obsmetrics.ProcessorDroppedMetricPoints.M(0),
		)
	}
}

// LogsDropped reports that the logs were dropped.
func (por *Processor) LogsDropped(ctx context.Context, numRecords int) {
	if por.level != configtelemetry.LevelNone {
		// ignore the error for now; should not happen
		_ = stats.RecordWithTags(
			ctx,
			por.mutators,
			obsmetrics.ProcessorAcceptedLogRecords.M(0),
			obsmetrics.ProcessorRefusedLogRecords.M(0),
			obsmetrics.ProcessorDroppedLogRecords.M(int64(numRecords)),
		)
	}
}
