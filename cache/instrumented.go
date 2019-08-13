package cache

import (
	"context"

	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	instr "github.com/weaveworks/common/instrument"
)

var (
	requestDuration = instr.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "cache_request_duration_seconds",
		Help:      "Total time spent in seconds doing cache requests.",
		// Cache requests are very quick: smallest bucket is 16us, biggest is 1s.
		Buckets: prometheus.ExponentialBuckets(0.000016, 4, 8),
	}, []string{"method", "status_code"}))

	fetchedKeys = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "cache_fetched_keys",
		Help:      "Total count of keys requested from cache.",
	}, []string{"name"})

	hits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "cache_hits",
		Help:      "Total count of keys found in cache.",
	}, []string{"name"})

	valueSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "cache_value_size_bytes",
		Help:      "Size of values in the cache.",
		// Cached chunks are generally in the KBs, but cached index can
		// get big.  Histogram goes from 1KB to 4MB.
		// 1024 * 4^(7-1) = 4MB
		Buckets: prometheus.ExponentialBuckets(1024, 4, 7),
	}, []string{"name", "method"})
)

func init() {
	requestDuration.Register()
	prometheus.MustRegister(fetchedKeys)
	prometheus.MustRegister(hits)
	prometheus.MustRegister(valueSize)
}

// Instrument returns an instrumented cache.
func Instrument(name string, cache Cache) Cache {
	return &instrumentedCache{
		name:  name,
		Cache: cache,

		fetchedKeys:      fetchedKeys.WithLabelValues(name),
		hits:             hits.WithLabelValues(name),
		storedValueSize:  valueSize.WithLabelValues(name, "store"),
		fetchedValueSize: valueSize.WithLabelValues(name, "fetch"),
	}
}

type instrumentedCache struct {
	name string
	Cache

	fetchedKeys, hits                 prometheus.Counter
	storedValueSize, fetchedValueSize prometheus.Observer
}

func (i *instrumentedCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	for j := range bufs {
		i.storedValueSize.Observe(float64(len(bufs[j])))
	}

	method := i.name + ".store"
	instr.CollectedRequest(ctx, method, requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := ot.SpanFromContext(ctx)
		sp.LogFields(otlog.Int("keys", len(keys)))
		i.Cache.Store(ctx, keys, bufs)
		return nil
	})
}

func (i *instrumentedCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string) {
	var (
		found   []string
		bufs    [][]byte
		missing []string
		method  = i.name + ".fetch"
	)

	instr.CollectedRequest(ctx, method, requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := ot.SpanFromContext(ctx)
		sp.LogFields(otlog.Int("keys requested", len(keys)))

		found, bufs, missing = i.Cache.Fetch(ctx, keys)
		sp.LogFields(otlog.Int("keys found", len(found)), otlog.Int("keys missing", len(keys)-len(found)))
		return nil
	})

	i.fetchedKeys.Add(float64(len(keys)))
	i.hits.Add(float64(len(found)))
	for j := range bufs {
		i.fetchedValueSize.Observe(float64(len(bufs[j])))
	}

	return found, bufs, missing
}

func (i *instrumentedCache) Stop() error {
	return i.Cache.Stop()
}
