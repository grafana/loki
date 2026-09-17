package loki

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	loglinequeryfrontend "github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// LoglineTenantSettingsProvider returns extension-owned, tenant-specific
// settings when the Logline middleware module is initialized.
type LoglineTenantSettingsProvider func() loglinequeryfrontend.TenantSettings

func (t *Loki) initLoglineMiddleware() (services.Service, error) {
	logger := log.With(util_log.Logger, "component", "logline-query-frontend")
	_ = level.Debug(logger).Log("msg", "initializing logline tripperware")
	if !t.Cfg.Logline.Enabled {
		_ = level.Debug(logger).Log("msg", "logline tripperware disabled")
		return nil, nil
	}

	var tenantSettings loglinequeryfrontend.TenantSettings
	if t.GetLoglineTenantSettings != nil {
		tenantSettings = t.GetLoglineTenantSettings()
	}

	wrapped, storeService, cleanup, err := loglinequeryfrontend.WrapMiddleware(
		t.Cfg.Logline,
		loglinequeryfrontend.MiddlewareInputs{
			SchemaConfig:         t.Cfg.SchemaConfig,
			ObjectStoreConfig:    t.Cfg.StorageConfig.ObjectStore,
			QueryIngestersWithin: t.Cfg.Querier.QueryIngestersWithin,
			ResultsCacheConfig:   t.Cfg.QueryRange.ResultsCacheConfig.CacheConfig,
		},
		tenantSettings,
		t.QueryFrontEndMiddleware,
		logger,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize logline tripperware: %w", err)
	}

	t.QueryFrontEndMiddleware = wrapped
	if storeService == nil {
		cleanup()
	}
	return storeService, nil
}
