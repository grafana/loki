package queryfrontend

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/store"
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

// WrapMiddleware builds and injects logline query middlewares around an
// existing Loki queryrange middleware stack. It creates and owns a store
// service lifecycle for index polling.
//
// Returns:
//   - wrapped middleware
//   - store polling service (start/stop managed by caller)
//   - cleanup function (idempotent; stops hint cache)
func WrapMiddleware(
	lokiCfg loki.ConfigWrapper,
	cfg Config,
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

	lokiQIW := lokiCfg.Querier.QueryIngestersWithin
	if lokiQIW == 0 {
		lokiQIW = store.DefaultQueryIngestersWithin
	}
	cfg.Store.QueryIngestersWithin = lokiQIW
	indexStore, err := store.New(
		context.Background(),
		lokiCfg.SchemaConfig,
		lokiCfg.StorageConfig.ObjectStore,
		cfg.Store,
		logger,
		reg,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create logline index store: %w", err)
	}

	wrapped, hintCache, err := WrapMiddlewareWithStore(
		lokiCfg,
		cfg.QueryFrontend,
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
	lokiCfg loki.ConfigWrapper,
	cfg MiddlewareConfig,
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
		hintCacheCfg := lokiCfg.QueryRange.ResultsCacheConfig.CacheConfig
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
	if cfg.QueryIngestersWithin == 0 {
		cfg.QueryIngestersWithin = lokiCfg.Querier.QueryIngestersWithin
	}
	if cfg.MaxQueryBytesRead == 0 {
		cfg.MaxQueryBytesRead = int64(lokiCfg.LimitsConfig.MaxQueryBytesRead.Val())
	}

	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, tenantSettings, metrics, logger)
	filterMW := NewLoglineFilterMiddleware(cfg.HintTimeout, metrics, logger)

	if existing == nil {
		existing = identityMiddleware{}
	}

	wrapped := queryrangebase.MergeMiddlewares(prefetchMW, existing, filterMW)
	return wrapped, hintCache, nil
}
