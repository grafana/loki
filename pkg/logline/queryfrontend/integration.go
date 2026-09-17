package queryfrontend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
)

// lenientRegisterer wraps a prometheus.Registerer to swallow duplicate
// registration errors instead of panicking. Loki's middleware stack creates
// multiple caches that each initialise a DNS provider registering the same
// metrics; in the full Loki binary these are de-duped, but this integration
// can initialize additional cache clients.
type lenientRegisterer struct {
	prometheus.Registerer
}

func (r lenientRegisterer) Register(c prometheus.Collector) error {
	err := r.Registerer.Register(c)
	if err == nil {
		return nil
	}
	var are prometheus.AlreadyRegisteredError
	if errors.As(err, &are) {
		return nil
	}
	return err
}

func (r lenientRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := r.Register(c); err != nil {
			panic(err)
		}
	}
}

type identityMiddleware struct{}

func (identityMiddleware) Wrap(next queryrangebase.Handler) queryrangebase.Handler { return next }

// MiddlewareInputs contains the Loki configuration sections used to assemble
// the logline query-frontend middleware without depending on pkg/loki.
type MiddlewareInputs struct {
	SchemaConfig         storageconfig.SchemaConfig
	ObjectStoreConfig    bucket.ConfigWithNamedStores
	QueryIngestersWithin time.Duration
	ResultsCacheConfig   cache.Config
}

// WrapMiddleware builds and injects logline query middlewares around an
// existing Loki queryrange middleware stack. It creates and owns a store
// service lifecycle for index polling.
//
// Returns:
//   - wrapped middleware
//   - store polling service (start/stop managed by caller)
//   - cleanup function (idempotent; stops hint cache)
func WrapMiddleware(
	cfg Config,
	inputs MiddlewareInputs,
	tenantSettings TenantSettings,
	existing queryrangebase.Middleware,
	logger log.Logger,
	reg prometheus.Registerer,
) (queryrangebase.Middleware, services.Service, func(), error) {
	if !cfg.Enabled {
		if existing == nil {
			existing = identityMiddleware{}
		}
		return existing, nil, func() {}, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid logline config: %w", err)
	}

	queryIngestersWithin := inputs.QueryIngestersWithin
	if queryIngestersWithin == 0 {
		queryIngestersWithin = store.DefaultQueryIngestersWithin
	}
	cfg.Store.QueryIngestersWithin = queryIngestersWithin
	cfg.QueryFrontend.QueryIngestersWithin = queryIngestersWithin
	indexStore, err := store.New(
		context.Background(),
		inputs.SchemaConfig,
		inputs.ObjectStoreConfig,
		cfg.Store,
		logger,
		reg,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create logline index store: %w", err)
	}

	wrapped, hintCache, err := WrapMiddlewareWithStore(
		cfg.QueryFrontend,
		inputs.ResultsCacheConfig,
		tenantSettings,
		indexStore,
		existing,
		logger,
		reg,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			if hintCache != nil {
				hintCache.Stop()
			}
		})
	}

	storeSvc := services.NewBasicService(
		func(ctx context.Context) error {
			return indexStore.StartPolling(ctx)
		},
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		func(_ error) error {
			cleanup()
			return nil
		},
	)

	return wrapped, storeSvc, cleanup, nil
}

// WrapMiddlewareWithStore injects logline middlewares around an existing
// middleware stack using a caller-provided index store.
func WrapMiddlewareWithStore(
	cfg MiddlewareConfig,
	resultsCacheConfig cache.Config,
	tenantSettings TenantSettings,
	indexStore *store.Store,
	existing queryrangebase.Middleware,
	logger log.Logger,
	reg prometheus.Registerer,
) (queryrangebase.Middleware, cache.Cache, error) {
	if indexStore == nil {
		return nil, nil, fmt.Errorf("index store cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid query frontend config: %w", err)
	}

	if logger == nil {
		logger = log.NewNopLogger()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	reg = lenientRegisterer{reg}

	metrics := NewMetrics(reg)
	baseHintProvider, err := hintprovider.NewLoglineHintProvider(
		indexStore,
		cfg.NgramLength,
		cfg.MaxHintParallel,
		metrics.ObserveQueryMultipleTermBatches,
		logger,
		reg,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create hint provider: %w", err)
	}

	var hintCache cache.Cache
	if cfg.HintCacheTTL > 0 {
		hintCacheCfg := resultsCacheConfig
		hintCacheCfg.Prefix = "logline-hint-cache."
		hintCacheCfg.DefaultValidity = cfg.HintCacheTTL
		hintCacheCfg.Memcache.Expiration = cfg.HintCacheTTL
		hintCacheCfg.Redis.Expiration = cfg.HintCacheTTL
		hintCacheCfg.EmbeddedCache.TTL = cfg.HintCacheTTL

		// Fall back to an in-process embedded cache when the Loki config
		// doesn't provide an external cache backend (memcached/redis).
		if !cache.IsMemcacheSet(hintCacheCfg) && !cache.IsRedisSet(hintCacheCfg) {
			hintCacheCfg.EmbeddedCache.Enabled = true
			hintCacheCfg.EmbeddedCache.MaxSizeMB = cfg.HintCacheMaxSizeMB
		}

		if cache.IsCacheConfigured(hintCacheCfg) {
			hintCache, err = cache.New(hintCacheCfg, reg, logger, stats.CacheType("logline-hints"), "logline")
			if err != nil {
				return nil, nil, fmt.Errorf("create hint cache: %w", err)
			}
		}
	}

	hp := hintprovider.NewCachingHintProvider(baseHintProvider, hintCache, reg)

	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, tenantSettings, metrics, logger)
	filterMW := NewLoglineFilterMiddleware(cfg.HintTimeout, metrics, logger)

	if existing == nil {
		existing = identityMiddleware{}
	}

	wrapped := queryrangebase.MergeMiddlewares(prefetchMW, existing, filterMW)
	return wrapped, hintCache, nil
}
