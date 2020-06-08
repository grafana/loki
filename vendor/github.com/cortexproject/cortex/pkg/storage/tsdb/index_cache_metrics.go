package tsdb

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

func MustNewIndexCacheMetrics(backend string, reg *prometheus.Registry) prometheus.Collector {
	switch backend {
	case IndexCacheBackendInMemory:
		return NewInMemoryIndexCacheMetrics(reg)
	case IndexCacheBackendMemcached:
		return NewMemcachedIndexCacheMetrics(reg)
	default:
		panic(errUnsupportedIndexCacheBackend.Error())
	}
}

// InMemoryIndexCacheMetrics aggregates metrics exported by Thanos in-memory index cache
// and re-exports them as Cortex metrics.
type InMemoryIndexCacheMetrics struct {
	reg *prometheus.Registry

	// Metrics gathered from Thanos InMemoryIndexCache
	cacheItemsEvicted          *prometheus.Desc
	cacheItemsAdded            *prometheus.Desc
	cacheRequests              *prometheus.Desc
	cacheItemsOverflow         *prometheus.Desc
	cacheHits                  *prometheus.Desc
	cacheItemsCurrentCount     *prometheus.Desc
	cacheItemsCurrentSize      *prometheus.Desc
	cacheItemsTotalCurrentSize *prometheus.Desc

	// Ignored:
	// thanos_store_index_cache_max_size_bytes
	// thanos_store_index_cache_max_item_size_bytes
}

// NewInMemoryIndexCacheMetrics makes InMemoryIndexCacheMetrics.
func NewInMemoryIndexCacheMetrics(reg *prometheus.Registry) *InMemoryIndexCacheMetrics {
	return &InMemoryIndexCacheMetrics{
		reg: reg,

		// Cache
		cacheItemsEvicted: prometheus.NewDesc(
			"blocks_index_cache_items_evicted_total",
			"Total number of items that were evicted from the index cache.",
			[]string{"item_type"}, nil),
		cacheItemsAdded: prometheus.NewDesc(
			"blocks_index_cache_items_added_total",
			"Total number of items that were added to the index cache.",
			[]string{"item_type"}, nil),
		cacheRequests: prometheus.NewDesc(
			"blocks_index_cache_requests_total",
			"Total number of requests to the cache.",
			[]string{"item_type"}, nil),
		cacheItemsOverflow: prometheus.NewDesc(
			"blocks_index_cache_items_overflowed_total",
			"Total number of items that could not be added to the cache due to being too big.",
			[]string{"item_type"}, nil),
		cacheHits: prometheus.NewDesc(
			"blocks_index_cache_hits_total",
			"Total number of requests to the cache that were a hit.",
			[]string{"item_type"}, nil),
		cacheItemsCurrentCount: prometheus.NewDesc(
			"blocks_index_cache_items",
			"Current number of items in the index cache.",
			[]string{"item_type"}, nil),
		cacheItemsCurrentSize: prometheus.NewDesc(
			"blocks_index_cache_items_size_bytes",
			"Current byte size of items in the index cache.",
			[]string{"item_type"}, nil),
		cacheItemsTotalCurrentSize: prometheus.NewDesc(
			"blocks_index_cache_total_size_bytes",
			"Current byte size of items (both value and key) in the index cache.",
			[]string{"item_type"}, nil),
	}
}

func (m *InMemoryIndexCacheMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- m.cacheItemsEvicted
	out <- m.cacheItemsAdded
	out <- m.cacheRequests
	out <- m.cacheItemsOverflow
	out <- m.cacheHits
	out <- m.cacheItemsCurrentCount
	out <- m.cacheItemsCurrentSize
	out <- m.cacheItemsTotalCurrentSize
}

func (m *InMemoryIndexCacheMetrics) Collect(out chan<- prometheus.Metric) {
	data := util.BuildMetricFamiliesPerUserFromUserRegistries(map[string]*prometheus.Registry{
		"": m.reg,
	})

	data.SendSumOfCountersWithLabels(out, m.cacheItemsEvicted, "thanos_store_index_cache_items_evicted_total", "item_type")
	data.SendSumOfCountersWithLabels(out, m.cacheItemsAdded, "thanos_store_index_cache_items_added_total", "item_type")
	data.SendSumOfCountersWithLabels(out, m.cacheRequests, "thanos_store_index_cache_requests_total", "item_type")
	data.SendSumOfCountersWithLabels(out, m.cacheItemsOverflow, "thanos_store_index_cache_items_overflowed_total", "item_type")
	data.SendSumOfCountersWithLabels(out, m.cacheHits, "thanos_store_index_cache_hits_total", "item_type")

	data.SendSumOfGaugesWithLabels(out, m.cacheItemsCurrentCount, "thanos_store_index_cache_items", "item_type")
	data.SendSumOfGaugesWithLabels(out, m.cacheItemsCurrentSize, "thanos_store_index_cache_items_size_bytes", "item_type")
	data.SendSumOfGaugesWithLabels(out, m.cacheItemsTotalCurrentSize, "thanos_store_index_cache_total_size_bytes", "item_type")
}

// MemcachedIndexCacheMetrics aggregates metrics exported by Thanos memcached index cache
// and re-exports them as Cortex metrics.
type MemcachedIndexCacheMetrics struct {
	reg *prometheus.Registry

	// Metrics gathered from Thanos MemcachedIndexCache (and client).
	cacheRequests       *prometheus.Desc
	cacheHits           *prometheus.Desc
	memcachedOperations *prometheus.Desc
	memcachedFailures   *prometheus.Desc
	memcachedDuration   *prometheus.Desc
	memcachedSkipped    *prometheus.Desc
}

// NewMemcachedIndexCacheMetrics makes MemcachedIndexCacheMetrics.
func NewMemcachedIndexCacheMetrics(reg *prometheus.Registry) *MemcachedIndexCacheMetrics {
	return &MemcachedIndexCacheMetrics{
		reg: reg,

		cacheRequests: prometheus.NewDesc(
			"blocks_index_cache_requests_total",
			"Total number of requests to the cache.",
			[]string{"item_type"}, nil),
		cacheHits: prometheus.NewDesc(
			"blocks_index_cache_hits_total",
			"Total number of requests to the cache that were a hit.",
			[]string{"item_type"}, nil),
		memcachedOperations: prometheus.NewDesc(
			"blocks_index_cache_memcached_operations_total",
			"Total number of operations against memcached.",
			[]string{"operation"}, nil),
		memcachedFailures: prometheus.NewDesc(
			"blocks_index_cache_memcached_operation_failures_total",
			"Total number of operations against memcached that failed.",
			[]string{"operation"}, nil),
		memcachedDuration: prometheus.NewDesc(
			"blocks_index_cache_memcached_operation_duration_seconds",
			"Duration of operations against memcached.",
			[]string{"operation"}, nil),
		memcachedSkipped: prometheus.NewDesc(
			"blocks_index_cache_memcached_operation_skipped_total",
			"Total number of operations against memcached that have been skipped.",
			[]string{"operation", "reason"}, nil),
	}
}

func (m *MemcachedIndexCacheMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- m.cacheRequests
	out <- m.cacheHits
	out <- m.memcachedOperations
	out <- m.memcachedFailures
	out <- m.memcachedDuration
	out <- m.memcachedSkipped
}

func (m *MemcachedIndexCacheMetrics) Collect(out chan<- prometheus.Metric) {
	data := util.BuildMetricFamiliesPerUserFromUserRegistries(map[string]*prometheus.Registry{
		"": m.reg,
	})

	data.SendSumOfCountersWithLabels(out, m.cacheRequests, "thanos_store_index_cache_requests_total", "item_type")
	data.SendSumOfCountersWithLabels(out, m.cacheHits, "thanos_store_index_cache_hits_total", "item_type")

	data.SendSumOfCountersWithLabels(out, m.memcachedOperations, "thanos_memcached_operations_total", "operation")
	data.SendSumOfCountersWithLabels(out, m.memcachedFailures, "thanos_memcached_operation_failures_total", "operation")
	data.SendSumOfHistogramsWithLabels(out, m.memcachedDuration, "thanos_memcached_operation_duration_seconds", "operation")
	data.SendSumOfCountersWithLabels(out, m.memcachedSkipped, "thanos_memcached_operation_skipped_total", "operation", "reason")
}
