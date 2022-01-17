package storegateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/store"
)

type gapBasedPartitioner struct {
	upstream store.Partitioner

	// Metrics.
	requestedBytes  prometheus.Counter
	requestedRanges prometheus.Counter
	expandedBytes   prometheus.Counter
	expandedRanges  prometheus.Counter
}

func newGapBasedPartitioner(maxGapBytes uint64, reg prometheus.Registerer) *gapBasedPartitioner {
	return &gapBasedPartitioner{
		upstream: store.NewGapBasedPartitioner(maxGapBytes),
		requestedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_bucket_store_partitioner_requested_bytes_total",
			Help: "Total size of byte ranges required to fetch from the storage before they are passed to the partitioner.",
		}),
		expandedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_bucket_store_partitioner_expanded_bytes_total",
			Help: "Total size of byte ranges returned by the partitioner after they've been combined together to reduce the number of bucket API calls.",
		}),
		requestedRanges: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_bucket_store_partitioner_requested_ranges_total",
			Help: "Total number of byte ranges required to fetch from the storage before they are passed to the partitioner.",
		}),
		expandedRanges: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_bucket_store_partitioner_expanded_ranges_total",
			Help: "Total number of byte ranges returned by the partitioner after they've been combined together to reduce the number of bucket API calls.",
		}),
	}
}

func (p *gapBasedPartitioner) Partition(length int, rng func(int) (uint64, uint64)) []store.Part {
	// Calculate the size of requested ranges.
	requestedBytes := uint64(0)
	for i := 0; i < length; i++ {
		start, end := rng(i)
		requestedBytes += end - start
	}

	// Run the upstream partitioner to compute the actual ranges that will be fetched.
	parts := p.upstream.Partition(length, rng)

	// Calculate the size of ranges that will be fetched.
	expandedBytes := uint64(0)
	for _, p := range parts {
		expandedBytes += p.End - p.Start
	}

	p.requestedBytes.Add(float64(requestedBytes))
	p.expandedBytes.Add(float64(expandedBytes))
	p.requestedRanges.Add(float64(length))
	p.expandedRanges.Add(float64(len(parts)))

	return parts
}
