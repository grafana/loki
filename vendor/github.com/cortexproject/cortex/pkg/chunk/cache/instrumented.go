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
)

func init() {
	requestDuration.Register()
	prometheus.MustRegister(fetchedKeys)
	prometheus.MustRegister(hits)
}

// Instrument returns an instrumented cache.
func Instrument(name string, cache Cache) Cache {
	return &instrumentedCache{
		name:        name,
		fetchedKeys: fetchedKeys.WithLabelValues(name),
		hits:        hits.WithLabelValues(name),
		Cache:       cache,
	}
}

type instrumentedCache struct {
	name              string
	fetchedKeys, hits prometheus.Counter
	Cache
}

func (i *instrumentedCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
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
	return found, bufs, missing
}

func (i *instrumentedCache) Stop() error {
	return i.Cache.Stop()
}
