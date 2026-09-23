package loki

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func (t *Loki) initLoglineStore() (services.Service, error) {
	if !t.Cfg.Logline.Enabled {
		return nil, nil
	}

	logger := log.With(util_log.Logger, "component", "logline-store")
	storeCfg := t.Cfg.Logline.Store
	qiw := t.Cfg.Querier.QueryIngestersWithin
	if qiw == 0 {
		qiw = store.DefaultQueryIngestersWithin
	}
	storeCfg.QueryIngestersWithin = qiw

	indexStore, err := store.New(
		context.Background(),
		t.Cfg.SchemaConfig,
		t.Cfg.StorageConfig.ObjectStore,
		storeCfg,
		logger,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return nil, err
	}
	t.loglineStore = indexStore

	return services.NewBasicService(
		func(ctx context.Context) error {
			return indexStore.StartPolling(ctx)
		},
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		nil,
	), nil
}

func (t *Loki) initLoglineMiddleware() (services.Service, error) {
	if !t.Cfg.Logline.Enabled {
		return nil, nil
	}

	level.Info(util_log.Logger).Log("msg", "initializing logline query-frontend middleware")
	logger := log.With(util_log.Logger, "component", "logline-query-frontend")

	wrapped, hintCache, err := queryfrontend.WrapMiddlewareWithStore(
		queryfrontend.HostConfig{
			SchemaConfig:         t.Cfg.SchemaConfig,
			ObjectStore:          t.Cfg.StorageConfig.ObjectStore,
			QueryIngestersWithin: t.Cfg.Querier.QueryIngestersWithin,
			ResultsCache:         t.Cfg.QueryRange.ResultsCacheConfig.CacheConfig,
			QuerySplitDuration:   time.Duration(t.Cfg.LimitsConfig.QuerySplitDuration),
		},
		t.Cfg.LoglineQueryFrontend,
		nil,
		t.loglineStore,
		t.QueryFrontEndMiddleware,
		logger,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return nil, err
	}
	t.QueryFrontEndMiddleware = wrapped
	return newHintCacheService(hintCache), nil
}

func newHintCacheService(hintCache cache.Cache) services.Service {
	if hintCache == nil {
		return nil
	}
	return services.NewIdleService(nil, func(_ error) error {
		hintCache.Stop()
		return nil
	})
}
