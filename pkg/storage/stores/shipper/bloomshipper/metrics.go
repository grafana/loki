package bloomshipper

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	sourceCache      = "cache"
	sourceStorage    = "storage"
	sourceFilesystem = "filesystem"
)

type storeMetrics struct {
}

func newStoreMetrics(_ prometheus.Registerer, _, _ string) *storeMetrics {
	return &storeMetrics{}
}

type fetcherMetrics struct {
	metasFetched      prometheus.Histogram
	blocksFetched     prometheus.Histogram
	metasFetchedSize  *prometheus.HistogramVec
	blocksFetchedSize *prometheus.HistogramVec
}

func newFetcherMetrics(registerer prometheus.Registerer, namespace, subsystem string) *fetcherMetrics {
	r := promauto.With(registerer)
	return &fetcherMetrics{
		metasFetched: r.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "metas_fetched",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // [1, 2, 4, ... 512]
			Help:      "Amount of metas fetched with a single operation",
		}),
		metasFetchedSize: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "metas_fetched_size_bytes",
			Buckets:   prometheus.ExponentialBuckets(128, 1.25, 10), // [128, 160, 200, ... 955]
			Help:      "Decompressed size of metas fetched from storage/cache",
		}, []string{"source"}),
		blocksFetched: r.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_fetched",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // [1, 2, 4, ... 512]
			Help:      "Amount of blocks fetched with a single operation",
		}),
		blocksFetchedSize: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_fetched_size_bytes",
			Buckets:   prometheus.ExponentialBuckets((5 << 20), 1.75, 10), // [5M, 8.75M, 15.3M, ... 769.7M]
			Help:      "Decompressed size of blocks fetched from storage/filesystem/cache",
		}, []string{"source"}),
	}
}
