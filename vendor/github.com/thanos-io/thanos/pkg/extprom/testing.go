// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package extprom

import (
	"fmt"
	"strings"
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

			for _, metric := range g.GetMetric() {
				var lbls []string
				for _, l := range metric.GetLabel() {
					lbls = append(lbls, *l.Name+"="+*l.Value)
				}

				key := fmt.Sprintf("%s{%s}", m, strings.Join(lbls, ","))
				if _, ok := res[key]; ok {
					t.Fatal("duplicate metrics, should never happen with Prometheus Registry; key =", key)
				}
				if metric.GetGauge() == nil {
					t.Fatal("metric is not a gauge; key =", key)
				}
				res[key] = *(metric.GetGauge().Value)
			}
		}
	}
	return res
}
