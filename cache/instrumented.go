package cache

import (
	"context"
	"time"

	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	instr "github.com/weaveworks/common/instrument"
)

var (
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "cache_request_duration_seconds",
		Help:      "Total time spent in seconds doing cache requests.",
		// Cache requests are very quick: smallest bucket is 16us, biggest is 1s.
		Buckets: prometheus.ExponentialBuckets(0.000016, 4, 8),
	}, []string{"method", "status_code"})

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
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(fetchedKeys)
	prometheus.MustRegister(hits)
}

// Instrument returns an instrumented cache.
func Instrument(name string, cache Cache) Cache {
	return &instrumentedCache{
		name:        name,
		fetchedKeys: fetchedKeys.WithLabelValues(name),
		hits:        hits.WithLabelValues(name),
		trace:       true,
		Cache:       cache,
	}
}

// MetricsInstrument returns an instrumented cache that only tracks metrics and not traces.
func MetricsInstrument(name string, cache Cache) Cache {
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
	trace             bool
	Cache
}

func (i *instrumentedCache) Store(ctx context.Context, key string, buf []byte) error {
	method := i.name + ".store"
	if i.trace {
		return instr.TimeRequestHistogram(ctx, method, requestDuration, func(ctx context.Context) error {
			sp := ot.SpanFromContext(ctx)
			sp.LogFields(otlog.String("key", key))

			return i.Cache.Store(ctx, key, buf)
		})
	}

	return UntracedCollectedRequest(ctx, method, instr.NewHistogramCollector(requestDuration), instr.ErrorCode, func(ctx context.Context) error {
		return i.Cache.Store(ctx, key, buf)
	})
}

func (i *instrumentedCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	var (
		found   []string
		bufs    [][]byte
		missing []string
		err     error
		method  = i.name + ".fetch"
	)

	if i.trace {
		err = instr.TimeRequestHistogram(ctx, method, requestDuration, func(ctx context.Context) error {
			sp := ot.SpanFromContext(ctx)
			sp.LogFields(otlog.Int("keys requested", len(keys)))

			var err error
			found, bufs, missing, err = i.Cache.Fetch(ctx, keys)

			if err == nil {
				sp.LogFields(otlog.Int("keys found", len(found)), otlog.Int("keys missing", len(keys)-len(found)))
			}

			return err
		})
	} else {
		err = UntracedCollectedRequest(ctx, method, instr.NewHistogramCollector(requestDuration), instr.ErrorCode, func(ctx context.Context) error {
			var err error
			found, bufs, missing, err = i.Cache.Fetch(ctx, keys)

			return err
		})
	}

	i.fetchedKeys.Add(float64(len(keys)))
	i.hits.Add(float64(len(found)))
	return found, bufs, missing, err
}

func (i *instrumentedCache) Stop() error {
	return i.Cache.Stop()
}

// UntracedCollectedRequest is the same as instr.CollectedRequest but without any tracing.
func UntracedCollectedRequest(ctx context.Context, method string, col instr.Collector, toStatusCode func(error) string, f func(context.Context) error) error {
	start := time.Now()
	col.Before(method, start)
	err := f(ctx)
	col.After(method, toStatusCode(err), start)

	return err
}
