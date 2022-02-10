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
	"go.opentelemetry.io/collector/receiver/scrapererror"
)

// Scraper is a helper to add observability to a component.Scraper.
type Scraper struct {
	receiverID config.ComponentID
	scraper    config.ComponentID
	mutators   []tag.Mutator
	tracer     trace.Tracer
}

// ScraperSettings are settings for creating a Scraper.
type ScraperSettings struct {
	ReceiverID             config.ComponentID
	Scraper                config.ComponentID
	ReceiverCreateSettings component.ReceiverCreateSettings
}

// NewScraper creates a new Scraper.
func NewScraper(cfg ScraperSettings) *Scraper {
	return &Scraper{
		receiverID: cfg.ReceiverID,
		scraper:    cfg.Scraper,
		mutators: []tag.Mutator{
			tag.Upsert(obsmetrics.TagKeyReceiver, cfg.ReceiverID.String(), tag.WithTTL(tag.TTLNoPropagation)),
			tag.Upsert(obsmetrics.TagKeyScraper, cfg.Scraper.String(), tag.WithTTL(tag.TTLNoPropagation))},
		tracer: cfg.ReceiverCreateSettings.TracerProvider.Tracer(cfg.Scraper.String()),
	}
}

// StartMetricsOp is called when a scrape operation is started. The
// returned context should be used in other calls to the obsreport functions
// dealing with the same scrape operation.
func (s *Scraper) StartMetricsOp(ctx context.Context) context.Context {
	ctx, _ = tag.New(ctx, s.mutators...)

	spanName := obsmetrics.ScraperPrefix + s.receiverID.String() + obsmetrics.NameSep + s.scraper.String() + obsmetrics.ScraperMetricsOperationSuffix
	ctx, _ = s.tracer.Start(ctx, spanName)
	return ctx
}

// EndMetricsOp completes the scrape operation that was started with
// StartMetricsOp.
func (s *Scraper) EndMetricsOp(
	scraperCtx context.Context,
	numScrapedMetrics int,
	err error,
) {
	numErroredMetrics := 0
	if err != nil {
		if partialErr, isPartial := err.(scrapererror.PartialScrapeError); isPartial {
			numErroredMetrics = partialErr.Failed
		} else {
			numErroredMetrics = numScrapedMetrics
			numScrapedMetrics = 0
		}
	}

	span := trace.SpanFromContext(scraperCtx)

	if obsreportconfig.Level() != configtelemetry.LevelNone {
		stats.Record(
			scraperCtx,
			obsmetrics.ScraperScrapedMetricPoints.M(int64(numScrapedMetrics)),
			obsmetrics.ScraperErroredMetricPoints.M(int64(numErroredMetrics)))
	}

	// end span according to errors
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String(obsmetrics.FormatKey, string(config.MetricsDataType)),
			attribute.Int64(obsmetrics.ScrapedMetricPointsKey, int64(numScrapedMetrics)),
			attribute.Int64(obsmetrics.ErroredMetricPointsKey, int64(numErroredMetrics)),
		)
		recordError(span, err)
	}

	span.End()
}
