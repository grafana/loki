package bloomshipper

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type storeMetrics struct {
}

func newStoreMetrics(_ prometheus.Registerer, _, _ string) *storeMetrics {
	return &storeMetrics{}
}

type fetcherMetrics struct {
	metasFetched  prometheus.Histogram
	blocksFetched prometheus.Histogram
}

func newFetcherMetrics(registerer prometheus.Registerer, namespace, subsystem string) *fetcherMetrics {
	r := promauto.With(registerer)
	return &fetcherMetrics{
		metasFetched: r.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "metas_fetched",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // [1, 2, 4, 8, 16, 32, 64, 128, 256, 512]
			Help:      "Amount of metas fetched with a single operation",
		}),
		blocksFetched: r.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_fetched",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // [1, 2, 4, 8, 16, 32, 64, 128, 256, 512]
			Help:      "Amount of blocks fetched with a single operation",
		}),
	}
}
