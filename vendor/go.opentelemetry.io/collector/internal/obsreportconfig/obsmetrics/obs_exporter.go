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

package obsmetrics // import "go.opentelemetry.io/collector/internal/obsreportconfig/obsmetrics"

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
)

const (
	// ExporterKey used to identify exporters in metrics and traces.
	ExporterKey = "exporter"

	// SentSpansKey used to track spans sent by exporters.
	SentSpansKey = "sent_spans"
	// FailedToSendSpansKey used to track spans that failed to be sent by exporters.
	FailedToSendSpansKey = "send_failed_spans"

	// SentMetricPointsKey used to track metric points sent by exporters.
	SentMetricPointsKey = "sent_metric_points"
	// FailedToSendMetricPointsKey used to track metric points that failed to be sent by exporters.
	FailedToSendMetricPointsKey = "send_failed_metric_points"

	// SentLogRecordsKey used to track logs sent by exporters.
	SentLogRecordsKey = "sent_log_records"
	// FailedToSendLogRecordsKey used to track logs that failed to be sent by exporters.
	FailedToSendLogRecordsKey = "send_failed_log_records"
)

var (
	TagKeyExporter, _ = tag.NewKey(ExporterKey)

	ExporterPrefix                 = ExporterKey + NameSep
	ExportTraceDataOperationSuffix = NameSep + "traces"
	ExportMetricsOperationSuffix   = NameSep + "metrics"
	ExportLogsOperationSuffix      = NameSep + "logs"

	// Exporter metrics. Any count of data items below is in the final format
	// that they were sent, reasoning: reconciliation is easier if measurements
	// on backend and exporter are expected to be the same. Translation issues
	// that result in a different number of elements should be reported in a
	// separate way.
	ExporterSentSpans = stats.Int64(
		ExporterPrefix+SentSpansKey,
		"Number of spans successfully sent to destination.",
		stats.UnitDimensionless)
	ExporterFailedToSendSpans = stats.Int64(
		ExporterPrefix+FailedToSendSpansKey,
		"Number of spans in failed attempts to send to destination.",
		stats.UnitDimensionless)
	ExporterSentMetricPoints = stats.Int64(
		ExporterPrefix+SentMetricPointsKey,
		"Number of metric points successfully sent to destination.",
		stats.UnitDimensionless)
	ExporterFailedToSendMetricPoints = stats.Int64(
		ExporterPrefix+FailedToSendMetricPointsKey,
		"Number of metric points in failed attempts to send to destination.",
		stats.UnitDimensionless)
	ExporterSentLogRecords = stats.Int64(
		ExporterPrefix+SentLogRecordsKey,
		"Number of log record successfully sent to destination.",
		stats.UnitDimensionless)
	ExporterFailedToSendLogRecords = stats.Int64(
		ExporterPrefix+FailedToSendLogRecordsKey,
		"Number of log records in failed attempts to send to destination.",
		stats.UnitDimensionless)
)
