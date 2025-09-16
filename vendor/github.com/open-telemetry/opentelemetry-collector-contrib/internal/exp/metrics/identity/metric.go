// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"

import (
	"fmt"
	"hash"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type Metric struct {
	scope

	name string
	unit string
	ty   pmetric.MetricType

	monotonic   bool
	temporality pmetric.AggregationTemporality
}

func (m Metric) Hash() hash.Hash64 {
	sum := m.scope.Hash()
	sum.Write([]byte(m.name))
	sum.Write([]byte(m.unit))

	var mono byte
	if m.monotonic {
		mono = 1
	}
	sum.Write([]byte{byte(m.ty), mono, byte(m.temporality)})
	return sum
}

func (m Metric) Scope() Scope {
	return m.scope
}

func OfMetric(scope Scope, m pmetric.Metric) Metric {
	id := Metric{
		scope: scope,
		name:  m.Name(),
		unit:  m.Unit(),
		ty:    m.Type(),
	}

	switch m.Type() {
	case pmetric.MetricTypeSum:
		sum := m.Sum()
		id.monotonic = sum.IsMonotonic()
		id.temporality = sum.AggregationTemporality()
	case pmetric.MetricTypeExponentialHistogram:
		exp := m.ExponentialHistogram()
		id.monotonic = true
		id.temporality = exp.AggregationTemporality()
	case pmetric.MetricTypeHistogram:
		hist := m.Histogram()
		id.monotonic = true
		id.temporality = hist.AggregationTemporality()
	}

	return id
}

func (m Metric) String() string {
	return fmt.Sprintf("metric/%x", m.Hash().Sum64())
}

func OfResourceMetric(res pcommon.Resource, scope pcommon.InstrumentationScope, metric pmetric.Metric) Metric {
	return OfMetric(OfScope(OfResource(res), scope), metric)
}
