package cache

import (
	"context"

	"github.com/grafana/dskit/tenant"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	instr "github.com/weaveworks/common/instrument"
)

// Instrument returns an instrumented cache.
func Instrument(name string, cache Cache, reg prometheus.Registerer, perTenant bool) Cache {
	valueSize := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "cache_value_size_bytes",
		Help:      "Size of values in the cache.",
		// Cached chunks are generally in the KBs, but cached index can
		// get big.  Histogram goes from 1KB to 4MB.
		// 1024 * 4^(7-1) = 4MB
		Buckets:     prometheus.ExponentialBuckets(1024, 4, 7),
		ConstLabels: prometheus.Labels{"name": name},
	}, []string{"method", "tenant"})

	return &instrumentedCache{
		Cache: cache,

		name:      name,
		perTenant: perTenant,

		requestDuration: instr.NewHistogramCollector(promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "cache_request_duration_seconds",
			Help:      "Total time spent in seconds doing cache requests.",
			// Cache requests are very quick: smallest bucket is 16us, biggest is 1s.
			Buckets:     prometheus.ExponentialBuckets(0.000016, 4, 8),
			ConstLabels: prometheus.Labels{"name": name},
			// NOTE: we don't need to track the per-tenant request duration, so this metric is global
		}, []string{"method", "status_code"})),

		fetchedKeys: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "cache_fetched_keys",
			Help:        "Total count of keys requested from cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}, []string{"tenant"}),

		hits: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "cache_hits",
			Help:        "Total count of keys found in cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}, []string{"tenant"}),

		storedValueSize:  valueSize,
		fetchedValueSize: valueSize,
	}
}

type instrumentedCache struct {
	Cache

	name      string
	perTenant bool

	fetchedKeys, hits                 *prometheus.CounterVec
	storedValueSize, fetchedValueSize prometheus.ObserverVec
	requestDuration                   *instr.HistogramCollector
}

func (i *instrumentedCache) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	for j := range bufs {
		i.storedValueSize.WithLabelValues("store", i.getTenantID(ctx)).Observe(float64(len(bufs[j])))
	}

	method := i.name + ".store"
	return instr.CollectedRequest(ctx, method, i.requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := ot.SpanFromContext(ctx)
		sp.LogFields(otlog.Int("keys", len(keys)))
		storeErr := i.Cache.Store(ctx, keys, bufs)
		if storeErr != nil {
			ext.Error.Set(sp, true)
			sp.LogFields(otlog.String("event", "error"), otlog.String("message", storeErr.Error()))
		}
		return storeErr
	})
}

func (i *instrumentedCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	var (
		found    []string
		bufs     [][]byte
		missing  []string
		fetchErr error
		method   = i.name + ".fetch"
	)

	err := instr.CollectedRequest(ctx, method, i.requestDuration, instr.ErrorCode, func(ctx context.Context) error {
		sp := ot.SpanFromContext(ctx)
		sp.LogFields(otlog.Int("keys requested", len(keys)))
		found, bufs, missing, fetchErr = i.Cache.Fetch(ctx, keys)
		if fetchErr != nil {
			ext.Error.Set(sp, true)
			sp.LogFields(otlog.String("event", "error"), otlog.String("message", fetchErr.Error()))
		}
		sp.LogFields(otlog.Int("keys found", len(found)), otlog.Int("keys missing", len(keys)-len(found)))
		return fetchErr
	})

	i.fetchedKeys.WithLabelValues(i.getTenantID(ctx)).Add(float64(len(keys)))
	i.hits.WithLabelValues(i.getTenantID(ctx)).Add(float64(len(found)))
	for j := range bufs {
		i.fetchedValueSize.WithLabelValues("fetch", i.getTenantID(ctx)).Observe(float64(len(bufs[j])))
	}

	return found, bufs, missing, err
}

func (i *instrumentedCache) Stop() {
	i.Cache.Stop()
}

func (i *instrumentedCache) getTenantID(ctx context.Context) string {
	// if per-tenant metrics are disabled, set an empty label which will be ignored
	if !i.perTenant {
		return ""
	}

	// best effort: if tenant ID cannot be retrieved, set an empty label which will be ignored
	userID, _ := tenant.TenantID(ctx)
	return userID
}
