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

package obsreportconfig // import "go.opentelemetry.io/collector/internal/obsreportconfig"

import (
	"sync/atomic"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/internal/obsreportconfig/obsmetrics"
)

var (
	globalLevel = int32(configtelemetry.LevelBasic)
)

// ObsMetrics wraps OpenCensus View for Collector observability metrics
type ObsMetrics struct {
	Views []*view.View
}

// Configure is used to control the settings that will be used by the obsreport
// package.
func Configure(level configtelemetry.Level) *ObsMetrics {
	atomic.StoreInt32(&globalLevel, int32(level))

	var views []*view.View

	if Level() != configtelemetry.LevelNone {
		obsMetricViews := allViews()
		views = append(views, obsMetricViews.Views...)
	}

	return &ObsMetrics{
		Views: views,
	}
}

func Level() configtelemetry.Level {
	return configtelemetry.Level(atomic.LoadInt32(&globalLevel))
}

// allViews return the list of all views that needs to be configured.
func allViews() *ObsMetrics {
	var views []*view.View
	// Receiver views.
	measures := []*stats.Int64Measure{
		obsmetrics.ReceiverAcceptedSpans,
		obsmetrics.ReceiverRefusedSpans,
		obsmetrics.ReceiverAcceptedMetricPoints,
		obsmetrics.ReceiverRefusedMetricPoints,
		obsmetrics.ReceiverAcceptedLogRecords,
		obsmetrics.ReceiverRefusedLogRecords,
	}
	tagKeys := []tag.Key{
		obsmetrics.TagKeyReceiver, obsmetrics.TagKeyTransport,
	}
	views = append(views, genViews(measures, tagKeys, view.Sum())...)

	// Scraper views.
	measures = []*stats.Int64Measure{
		obsmetrics.ScraperScrapedMetricPoints,
		obsmetrics.ScraperErroredMetricPoints,
	}
	tagKeys = []tag.Key{obsmetrics.TagKeyReceiver, obsmetrics.TagKeyScraper}
	views = append(views, genViews(measures, tagKeys, view.Sum())...)

	// Exporter views.
	measures = []*stats.Int64Measure{
		obsmetrics.ExporterSentSpans,
		obsmetrics.ExporterFailedToSendSpans,
		obsmetrics.ExporterSentMetricPoints,
		obsmetrics.ExporterFailedToSendMetricPoints,
		obsmetrics.ExporterSentLogRecords,
		obsmetrics.ExporterFailedToSendLogRecords,
	}
	tagKeys = []tag.Key{obsmetrics.TagKeyExporter}
	views = append(views, genViews(measures, tagKeys, view.Sum())...)

	errorNumberView := &view.View{
		Name:        obsmetrics.ExporterPrefix + "send_failed_requests",
		Description: "number of times exporters failed to send requests to the destination",
		Measure:     obsmetrics.ExporterFailedToSendSpans,
		Aggregation: view.Count(),
	}
	views = append(views, errorNumberView)

	// Processor views.
	measures = []*stats.Int64Measure{
		obsmetrics.ProcessorAcceptedSpans,
		obsmetrics.ProcessorRefusedSpans,
		obsmetrics.ProcessorDroppedSpans,
		obsmetrics.ProcessorAcceptedMetricPoints,
		obsmetrics.ProcessorRefusedMetricPoints,
		obsmetrics.ProcessorDroppedMetricPoints,
		obsmetrics.ProcessorAcceptedLogRecords,
		obsmetrics.ProcessorRefusedLogRecords,
		obsmetrics.ProcessorDroppedLogRecords,
	}
	tagKeys = []tag.Key{obsmetrics.TagKeyProcessor}
	views = append(views, genViews(measures, tagKeys, view.Sum())...)

	return &ObsMetrics{
		Views: views,
	}
}

func genViews(
	measures []*stats.Int64Measure,
	tagKeys []tag.Key,
	aggregation *view.Aggregation,
) []*view.View {
	views := make([]*view.View, 0, len(measures))
	for _, measure := range measures {
		views = append(views, &view.View{
			Name:        measure.Name(),
			Description: measure.Description(),
			TagKeys:     tagKeys,
			Measure:     measure,
			Aggregation: aggregation,
		})
	}
	return views
}
