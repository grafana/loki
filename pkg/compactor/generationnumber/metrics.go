package generationnumber

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Make this package level because we want several instances of a loader to be able to report metrics
var metrics *genLoaderMetrics

type genLoaderMetrics struct {
	cacheGenLoadFailures *prometheus.CounterVec
}

func newGenLoaderMetrics(r prometheus.Registerer) *genLoaderMetrics {
	if metrics != nil {
		return metrics
	}

	if r == nil {
		return nil
	}

	cacheGenLoadFailures := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "delete_cache_gen_load_failures_total",
		Help:      "Total number of failures while loading cache generation number using gen number loader",
	}, []string{"source"})

	r.MustRegister(cacheGenLoadFailures)

	metrics = &genLoaderMetrics{
		cacheGenLoadFailures: cacheGenLoadFailures,
	}

	return metrics
}
