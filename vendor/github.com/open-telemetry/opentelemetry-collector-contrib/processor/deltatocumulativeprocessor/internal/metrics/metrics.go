// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/metrics"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
)

type Ident = identity.Metric

type Metric struct {
	res   pcommon.Resource
	scope pcommon.InstrumentationScope
	pmetric.Metric
}

func (m *Metric) Ident() Ident {
	return identity.OfResourceMetric(m.res, m.scope, m.Metric)
}

func (m *Metric) Resource() pcommon.Resource {
	return m.res
}

func (m *Metric) Scope() pcommon.InstrumentationScope {
	return m.scope
}

func From(res pcommon.Resource, scope pcommon.InstrumentationScope, metric pmetric.Metric) Metric {
	return Metric{res: res, scope: scope, Metric: metric}
}

func (m Metric) AggregationTemporality() pmetric.AggregationTemporality {
	switch m.Type() {
	case pmetric.MetricTypeSum:
		return m.Sum().AggregationTemporality()
	case pmetric.MetricTypeHistogram:
		return m.Histogram().AggregationTemporality()
	case pmetric.MetricTypeExponentialHistogram:
		return m.ExponentialHistogram().AggregationTemporality()
	}

	return pmetric.AggregationTemporalityUnspecified
}

func (m Metric) Typed() Any {
	//exhaustive:enforce
	switch m.Type() {
	case pmetric.MetricTypeSum:
		return Sum(m)
	case pmetric.MetricTypeGauge:
		return Gauge(m)
	case pmetric.MetricTypeExponentialHistogram:
		return ExpHistogram(m)
	case pmetric.MetricTypeHistogram:
		return Histogram(m)
	case pmetric.MetricTypeSummary:
		return Summary(m)
	}
	panic("unreachable")
}

var (
	_ Any = Sum{}
	_ Any = Gauge{}
	_ Any = ExpHistogram{}
	_ Any = Histogram{}
	_ Any = Summary{}
)

type Any interface {
	Len() int
	Ident() identity.Metric
	SetAggregationTemporality(pmetric.AggregationTemporality)
}

func (m Metric) Filter(ok func(id identity.Stream, dp any) bool) {
	mid := m.Ident()
	switch m.Type() {
	case pmetric.MetricTypeSum:
		m.Sum().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			id := identity.OfStream(mid, dp)
			return !ok(id, dp)
		})
	case pmetric.MetricTypeGauge:
		m.Gauge().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			id := identity.OfStream(mid, dp)
			return !ok(id, dp)
		})
	case pmetric.MetricTypeHistogram:
		m.Histogram().DataPoints().RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
			id := identity.OfStream(mid, dp)
			return !ok(id, dp)
		})
	case pmetric.MetricTypeExponentialHistogram:
		m.ExponentialHistogram().DataPoints().RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
			id := identity.OfStream(mid, dp)
			return !ok(id, dp)
		})
	case pmetric.MetricTypeSummary:
		m.Summary().DataPoints().RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
			id := identity.OfStream(mid, dp)
			return !ok(id, dp)
		})
	}
}
