// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package extprom

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/testutil"
)

// CurrentGaugeValuesFor returns gauge values for given metric names. Useful for testing based on registry,
// when you don't have access to metric variable.
func CurrentGaugeValuesFor(t *testing.T, reg prometheus.Gatherer, metricNames ...string) map[string]float64 {
	f, err := reg.Gather()
	testutil.Ok(t, err)

	res := make(map[string]float64, len(metricNames))
	for _, g := range f {
		for _, m := range metricNames {
			if g.GetName() != m {
				continue
			}

			testutil.Equals(t, 1, len(g.GetMetric()))
			if _, ok := res[m]; ok {
				t.Error("expected only one metric family for", m)
				t.FailNow()
			}
			res[m] = *g.GetMetric()[0].GetGauge().Value
		}
	}
	return res
}
