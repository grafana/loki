// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/metrics"

import (
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type Sum Metric

func (s Sum) Len() int {
	return Metric(s).Sum().DataPoints().Len()
}

func (s Sum) Ident() Ident {
	return (*Metric)(&s).Ident()
}

func (s Sum) SetAggregationTemporality(at pmetric.AggregationTemporality) {
	s.Sum().SetAggregationTemporality(at)
}

type Histogram Metric

func (s Histogram) Len() int {
	return Metric(s).Histogram().DataPoints().Len()
}

func (s Histogram) Ident() Ident {
	return (*Metric)(&s).Ident()
}

func (s Histogram) SetAggregationTemporality(at pmetric.AggregationTemporality) {
	s.Histogram().SetAggregationTemporality(at)
}

type ExpHistogram Metric

func (s ExpHistogram) Len() int {
	return Metric(s).ExponentialHistogram().DataPoints().Len()
}

func (s ExpHistogram) Ident() Ident {
	return (*Metric)(&s).Ident()
}

func (s ExpHistogram) SetAggregationTemporality(at pmetric.AggregationTemporality) {
	s.ExponentialHistogram().SetAggregationTemporality(at)
}

type Gauge Metric

func (s Gauge) Len() int {
	return Metric(s).Gauge().DataPoints().Len()
}

func (s Gauge) Ident() Ident {
	return (*Metric)(&s).Ident()
}

func (Gauge) SetAggregationTemporality(pmetric.AggregationTemporality) {}

type Summary Metric

func (s Summary) Len() int {
	return Metric(s).Summary().DataPoints().Len()
}

func (s Summary) Ident() Ident {
	return (*Metric)(&s).Ident()
}

func (Summary) SetAggregationTemporality(pmetric.AggregationTemporality) {}
