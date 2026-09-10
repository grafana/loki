package loki

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	loglinequeryfrontend "github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// setupLogline registers the logline query frontend module and its
// dependencies.
//
// Modelled on setupLBAC. The module is user-invisible while the feature is
// experimental, and is pulled in by the query frontend rather than being a
// target of its own.
func (l *Loki) setupLogline() error {
	l.ModuleManager.RegisterModule(LoglineQueryFrontendMW, l.initLoglineQueryFrontendMiddleware, modules.UserInvisibleModule)

	loglineDeps := map[string][]string{
		LoglineQueryFrontendMW: {QueryFrontendTripperware, Overrides},

		// The middleware has to wrap QueryFrontEndMiddleware before the query
		// frontend consumes it, so the frontend depends on this module rather
		// than the other way round.
		QueryFrontend: {LoglineQueryFrontendMW},
	}

	for mod, targets := range loglineDeps {
		if err := l.ModuleManager.AddDependency(mod, targets...); err != nil {
			return fmt.Errorf("could not add logline module dependency %s -> %v: %w", mod, targets, err)
		}
	}
	return nil
}

// initLoglineQueryFrontendMiddleware prepends the logline middleware to the
// query frontend chain.
//
// It returns the index store's service so the module manager owns its
// lifecycle, or no service when logline is not configured for this deployment.
func (l *Loki) initLoglineQueryFrontendMiddleware() (services.Service, error) {
	if !l.Cfg.Logline.QueryFrontend.Enabled {
		_ = level.Debug(util_log.Logger).Log("msg", "logline query frontend middleware disabled")
		return nil, nil
	}

	_ = level.Debug(util_log.Logger).Log("msg", "initializing logline query frontend middleware")

	wrapped, storeService, cleanup, err := loglinequeryfrontend.WrapMiddleware(
		loglinequeryfrontend.Deps{
			SchemaConfig:         l.Cfg.SchemaConfig,
			ObjectStoreConfig:    l.Cfg.StorageConfig.ObjectStore,
			QueryIngestersWithin: l.Cfg.Querier.QueryIngestersWithin,
			HintCacheConfig:      l.Cfg.QueryRange.ResultsCacheConfig.CacheConfig,
		},
		l.Cfg.Logline.QueryFrontend,
		l.Cfg.Logline.Store,
		loglinequeryfrontend.NewTenantSettings(l.Overrides),
		l.QueryFrontEndMiddleware,
		util_log.Logger,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize logline query frontend middleware: %w", err)
	}

	l.QueryFrontEndMiddleware = wrapped

	if storeService == nil {
		cleanup()
		return nil, nil
	}
	return storeService, nil
}
