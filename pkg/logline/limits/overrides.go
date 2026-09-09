package limits

import (
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/validation"
)

// runtimeConfigValues mirrors Loki's runtime config YAML structure.
// Only the overrides key is relevant to logline.
type runtimeConfigValues struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`
}

func (r runtimeConfigValues) validate(logger log.Logger) error {
	for t, c := range r.TenantLimits {
		if c == nil {
			level.Warn(logger).Log("msg", "skipping empty tenant limit definition", "tenant", t)
		}
	}
	return nil
}

// tenantLimitsFromRuntimeConfig implements validation.TenantLimits by
// reading from a live runtimeconfig.Manager. Same pattern as Loki.
type tenantLimitsFromRuntimeConfig struct {
	manager *runtimeconfig.Manager
}

func (t *tenantLimitsFromRuntimeConfig) TenantLimits(userID string) *validation.Limits {
	all := t.AllByUserID()
	if all == nil {
		return nil
	}
	return all[userID]
}

func (t *tenantLimitsFromRuntimeConfig) AllByUserID() map[string]*validation.Limits {
	if t.manager == nil {
		return nil
	}
	cfg, ok := t.manager.GetConfig().(*runtimeConfigValues)
	if cfg != nil && ok {
		return cfg.TenantLimits
	}
	return nil
}

// maxLimits precomputes the maximum of each limit across all tenants.
// Values are recomputed on every runtime config reload via a listener
// goroutine and served from atomics, so callers pay no iteration cost.
type maxLimits struct {
	inner *validation.Overrides

	retention atomic.Int64 // time.Duration stored as int64

	retentionGauge prometheus.Gauge
}

func (m *maxLimits) RetentionPeriod() time.Duration {
	return time.Duration(m.retention.Load())
}

// recompute iterates all tenant limits and updates the cached max values.
// The default comes from the Loki config's limits_config; per-tenant
// overrides can only increase it (or set zero to disable).
func (m *maxLimits) recompute() {
	rp := m.computeMax(
		func(l *validation.Limits) time.Duration { return time.Duration(l.RetentionPeriod) },
		m.inner.RetentionPeriod(""),
	)
	m.retention.Store(int64(rp))
	m.retentionGauge.Set(rp.Seconds())
}

// computeMax returns the maximum of defaultVal and the field extracted by fn
// from every tenant's limits. Per-tenant zero values are skipped (treated as
// "use the default"). The result is always at least defaultVal.
func (m *maxLimits) computeMax(fn func(*validation.Limits) time.Duration, defaultVal time.Duration) time.Duration {
	maxVal := defaultVal
	for _, l := range m.inner.AllByUserID() {
		if l == nil {
			continue
		}
		if v := fn(l); v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

// listenForReloads blocks until the channel is closed (manager stopped),
// recomputing max limits on every runtime config reload.
func (m *maxLimits) listenForReloads(ch <-chan any) {
	for range ch {
		m.recompute()
	}
}

// NewOverrides creates a Limits implementation and the underlying
// *validation.Overrides from the given Loki config. If the Loki config
// specifies a runtime config file (runtime_config.file), a
// runtimeconfig.Manager service is returned that polls the file for changes.
// The caller must start this service. If no file is configured the returned
// service is nil.
//
// The returned Limits precomputes the maximum RetentionPeriod across all
// tenants because logline indexes are shared across tenants. Values are
// recomputed on each config reload.
func NewOverrides(
	lokiCfg loki.ConfigWrapper,
	logger log.Logger,
	reg prometheus.Registerer,
) (Limits, *validation.Overrides, services.Service, error) {
	runtimeCfg := lokiCfg.RuntimeConfig

	var (
		tl      validation.TenantLimits
		manager *runtimeconfig.Manager
		svc     services.Service
	)

	if len(runtimeCfg.LoadPath) > 0 {
		// Set defaults so that YAML unmarshalling fills in unset fields
		// with the Loki config values, not Go zero values.
		validation.SetDefaultLimitsForYAMLUnmarshalling(lokiCfg.LimitsConfig)

		runtimeCfg.Loader = loadRuntimeConfig(logger)

		var err error
		manager, err = runtimeconfig.New(runtimeCfg, "logline", reg, logger)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("create runtime config manager: %w", err)
		}
		svc = manager
		tl = &tenantLimitsFromRuntimeConfig{manager: manager}
	}

	ov, err := validation.NewOverrides(lokiCfg.LimitsConfig, tl)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create validation overrides: %w", err)
	}

	lim := &maxLimits{
		inner: ov,
		retentionGauge: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_limits_retention_period_seconds",
			Help: "Maximum retention_period across all tenants, in seconds. Zero means unlimited.",
		}),
	}
	lim.recompute()

	if manager != nil {
		ch := manager.CreateListenerChannel(1)
		go lim.listenForReloads(ch)
	}

	return lim, ov, svc, nil
}

// loadRuntimeConfig returns a Loader that parses the runtime config YAML.
func loadRuntimeConfig(logger log.Logger) runtimeconfig.Loader {
	return func(r io.Reader) (any, error) {
		var cfg runtimeConfigValues
		decoder := yaml.NewDecoder(r)
		if err := decoder.Decode(&cfg); err != nil {
			return nil, err
		}
		if err := cfg.validate(logger); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
}
