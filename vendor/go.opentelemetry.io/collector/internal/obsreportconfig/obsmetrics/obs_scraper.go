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
	// ScraperKey used to identify scrapers in metrics and traces.
	ScraperKey = "scraper"

	// ScrapedMetricPointsKey used to identify metric points scraped by the
	// Collector.
	ScrapedMetricPointsKey = "scraped_metric_points"
	// ErroredMetricPointsKey used to identify metric points errored (i.e.
	// unable to be scraped) by the Collector.
	ErroredMetricPointsKey = "errored_metric_points"
)

const (
	ScraperPrefix                 = ScraperKey + NameSep
	ScraperMetricsOperationSuffix = NameSep + "MetricsScraped"
)

var (
	TagKeyScraper, _ = tag.NewKey(ScraperKey)

	ScraperScrapedMetricPoints = stats.Int64(
		ScraperPrefix+ScrapedMetricPointsKey,
		"Number of metric points successfully scraped.",
		stats.UnitDimensionless)
	ScraperErroredMetricPoints = stats.Int64(
		ScraperPrefix+ErroredMetricPointsKey,
		"Number of metric points that were unable to be scraped.",
		stats.UnitDimensionless)
)
