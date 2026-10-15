package cache

import (
	"context"

	instr "github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	attribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Instrument returns an instrumented cache.
func Instrument(name string, cache Cache, reg prometheus.Registerer) Cache {
	return InstrumentTyped[string, []byte](name, cache, reg, byteSliceSize)
}

// InstrumentTyped returns an instrumented typed cache that exposes the same
// metrics as Instrument. valueSize reports the size of a cached value in bytes.
func InstrumentTyped[K comparable, V any](name string, cache TypedCache[K, V], reg prometheus.Registerer, valueSize func(V) int) TypedCache[K, V] {
	valueSizeHistogram := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "cache_value_size_bytes",
		Help:      "Size of values in the cache.",
		// Cached chunks are generally in the KBs, but cached index can
		// get big.  Histogram goes from 1KB to 4MB.
		// 1024 * 4^(7-1) = 4MB
		Buckets:     prometheus.ExponentialBuckets(1024, 4, 7),
		ConstLabels: prometheus.Labels{"name": name},
	}, []string{"method"})

	return &instrumentedCache[K, V]{
		name:       name,
		TypedCache: cache,
		valueSize:  valueSize,

		requestDuration: instr.NewHistogramCollector(promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "cache_request_duration_seconds",
			Help:      "Total time spent in seconds doing cache requests.",
			// Cache requests are very quick: smallest bucket is 16us, biggest is 1s.
			Buckets:     prometheus.ExponentialBuckets(0.000016, 4, 8),
			ConstLabels: prometheus.Labels{"name": name},
		}, []string{"method", "status_code"})),

		fetchedKeys: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "cache_fetched_keys",
			Help:        "Total count of keys requested from cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),

		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "cache_hits",
			Help:        "Total count of keys found in cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),

		storedValueSize:  valueSizeHistogram.WithLabelValues("store"),
		fetchedValueSize: valueSizeHistogram.WithLabelValues("fetch"),
	}
}

func byteSliceSize(buf []byte) int {
	return len(buf)
}

type instrumentedCache[K comparable, V any] struct {
	name string
	TypedCache[K, V]
	valueSize func(V) int

	fetchedKeys, hits                 prometheus.Counter
	storedValueSize, fetchedValueSize prometheus.Observer
	requestDuration                   *instr.HistogramCollector
}

func (i *instrumentedCache[K, V]) Store(ctx context.Context, keys []K, values []V) error {
	for j := range values {
		i.storedValueSize.Observe(float64(i.valueSize(values[j])))
	}

	method := i.name + ".store"
	return instr.CollectedRequest(ctx, method, i.requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := trace.SpanFromContext(ctx)
		sp.SetAttributes(attribute.Int("keys", len(keys)))
		storeErr := i.TypedCache.Store(ctx, keys, values)
		if storeErr != nil {
			sp.SetStatus(codes.Error, storeErr.Error())
			sp.RecordError(storeErr)
		}
		return storeErr
	})
}

func (i *instrumentedCache[K, V]) Fetch(ctx context.Context, keys []K) ([]K, []V, []K, error) {
	var (
		found    []K
		values   []V
		missing  []K
		fetchErr error
		method   = i.name + ".fetch"
	)

	err := instr.CollectedRequest(ctx, method, i.requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := trace.SpanFromContext(ctx)
		sp.SetAttributes(attribute.Int("keys requested", len(keys)))
		found, values, missing, fetchErr = i.TypedCache.Fetch(ctx, keys)
		if fetchErr != nil {
			sp.SetStatus(codes.Error, fetchErr.Error())
			sp.RecordError(fetchErr)
			return fetchErr
		}

		sp.SetAttributes(
			attribute.Int("keys found", len(found)),
			attribute.Int("keys missing", len(keys)-len(found)),
		)
		return nil
	})

	i.fetchedKeys.Add(float64(len(keys)))
	i.hits.Add(float64(len(found)))
	for j := range values {
		i.fetchedValueSize.Observe(float64(i.valueSize(values[j])))
	}

	return found, values, missing, err
}

func (i *instrumentedCache[K, V]) Stop() {
	i.TypedCache.Stop()
}
