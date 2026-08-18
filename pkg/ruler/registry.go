package ruler

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"dario.cat/mergo"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
	"github.com/grafana/loki/v3/pkg/ruler/storage/wal"
)

type walRegistry struct {
	logger  log.Logger
	manager instance.Manager

	metrics     *storageRegistryMetrics
	overridesMu sync.Mutex
	refreshMu   sync.Mutex

	config         Config
	overrides      RulesLimits
	lastUpdateTime time.Time
	cleaner        *cleaner.WALCleaner
}

type storageRegistry interface {
	storage.Appendable
	readyChecker

	stop()
	configureTenantStorage(tenant string)
}

func newWALRegistry(logger log.Logger, reg prometheus.Registerer, config Config, overrides RulesLimits) storageRegistry {
	if !config.RemoteWrite.Enabled {
		return nullRegistry{}
	}

	// Wipe the WAL directory before any per-tenant storage is opened. This runs
	// exactly once per ruler startup (newWALRegistry is called once), unlike the
	// per-tenant wal.NewStorage which is re-invoked whenever a tenant instance
	// restarts. Because the WAL is always discarded here, there is nothing to
	// replay when the per-tenant storage is later opened.
	level.Info(logger).Log("msg", "wiping ruler WAL directory on startup", "dir", config.WAL.Dir)
	if err := os.RemoveAll(config.WAL.Dir); err != nil {
		// Non-fatal: a failed wipe just leaves stale data on disk, so we log and
		// continue rather than blocking ruler startup.
		level.Error(logger).Log("msg", "failed to wipe ruler WAL directory on startup", "dir", config.WAL.Dir, "err", err)
	}

	manager := createInstanceManager(logger, reg)

	return &walRegistry{
		logger:    logger,
		metrics:   newStorageRegistryMetrics(reg),
		config:    config,
		overrides: overrides,
		manager:   manager,

		cleaner: cleaner.NewWALCleaner(
			logger,
			manager,
			cleaner.NewMetrics(reg),
			config.WAL.Dir,
			config.WALCleaner),
	}
}

func createInstanceManager(logger log.Logger, reg prometheus.Registerer) *instance.BasicManager {
	tenantManager := &tenantWALManager{
		reg:    reg,
		logger: log.With(logger, "manager", "tenant-wal"),
	}

	return instance.NewBasicManager(instance.BasicManagerConfig{
		InstanceRestartBackoff: time.Second,
	}, instance.NewMetrics(reg), log.With(logger, "manager", "tenant-wal"), tenantManager.newInstance)
}

func (r *walRegistry) isReady(tenant string) bool {
	return r.manager.InstanceReady(tenant)
}

func (r *walRegistry) get(tenant string) storage.Storage {
	logger := log.With(r.logger, "user", tenant)
	ready := r.metrics.appenderReady.WithLabelValues(tenant)

	if r.manager == nil {
		level.Warn(logger).Log("msg", "instance manager is not set")

		ready.Set(0)
		return nil
	}

	inst, err := r.manager.GetInstance(tenant)
	if err != nil {
		level.Error(logger).Log("msg", "could not retrieve instance", "err", err)

		ready.Set(0)
		return nil
	}

	i, ok := inst.(*instance.Instance)
	if !ok {
		level.Warn(logger).Log("msg", "instance is invalid")

		ready.Set(0)
		return nil
	}

	if !i.Ready() {
		level.Debug(logger).Log("msg", "instance is not yet ready")

		ready.Set(0)
		return nil
	}

	ready.Set(1)
	return i.Storage()
}

func (r *walRegistry) Appender(ctx context.Context) storage.Appender {
	// concurrency-safe retrieval of remote-write config for this tenant, using the global remote-write for defaults
	r.overridesMu.Lock()
	tenant, _ := user.ExtractOrgID(ctx)
	rwCfg, err := r.getTenantRemoteWriteConfig(tenant, r.config.RemoteWrite)
	r.overridesMu.Unlock()

	if err != nil {
		level.Error(r.logger).Log("msg", "error retrieving remote-write config; discarding samples", "user", tenant, "err", err)
		return discardingAppender{}
	}

	if !rwCfg.Enabled {
		return discardingAppender{}
	}

	inst := r.get(tenant)
	if inst == nil {
		level.Warn(r.logger).Log("user", tenant, "msg", "WAL instance not yet ready")
		return notReadyAppender{}
	}

	// we should reconfigure the storage whenever this appender is requested, but since
	// this can request an appender very often, we hide this behind a gate
	now := time.Now()
	r.refreshMu.Lock()
	shouldRefresh := r.lastUpdateTime.Before(now.Add(-r.config.RemoteWrite.ConfigRefreshPeriod))
	if shouldRefresh {
		r.lastUpdateTime = now
	}
	r.refreshMu.Unlock()

	if shouldRefresh {
		level.Debug(r.logger).Log("user", tenant, "msg", "refreshing remote-write configuration")
		r.configureTenantStorage(tenant)
	}

	return inst.Appender(ctx)
}

func (r *walRegistry) configureTenantStorage(tenant string) {
	conf, err := r.getTenantConfig(tenant)
	if err != nil {
		level.Error(r.logger).Log("msg", "error configuring tenant storage", "user", tenant, "err", err)
		return
	}

	if err := r.manager.ApplyConfig(conf); err != nil {
		level.Error(r.logger).Log("user", tenant, "msg", "could not apply given config", "err", err)
	}
}

func (r *walRegistry) stop() {
	if r.cleaner != nil {
		r.cleaner.Stop()
	}

	r.manager.Stop()
}

func (r *walRegistry) getTenantConfig(tenant string) (instance.Config, error) {
	r.overridesMu.Lock()
	defer r.overridesMu.Unlock()

	conf, err := r.config.WAL.Clone()
	if err != nil {
		return instance.Config{}, err
	}

	conf.Name = tenant
	conf.Tenant = tenant

	// retrieve remote-write config for this tenant, using the global remote-write for defaults
	rwCfg, err := r.getTenantRemoteWriteConfig(tenant, r.config.RemoteWrite)
	if err != nil {
		return instance.Config{}, err
	}

	// TODO(dannyk): implement multiple RW configs
	conf.RemoteWrite = []*config.RemoteWriteConfig{}
	if rwCfg.Enabled {
		for id := range r.config.RemoteWrite.Clients {
			clt := rwCfg.Clients[id]
			if clt.Headers == nil {
				clt.Headers = make(map[string]string)
			} else {
				clt.Headers = maps.Clone(clt.Headers)
			}

			// ensure that no variation of the X-Scope-OrgId header can be added, which might trick authentication
			for k := range clt.Headers {
				if strings.EqualFold(user.OrgIDHeaderName, strings.TrimSpace(k)) {
					delete(clt.Headers, k)
				}
			}

			if rwCfg.AddOrgIDHeader {
				// inject the X-Scope-OrgId header for multi-tenant metrics backends
				clt.Headers[user.OrgIDHeaderName] = tenant
			}

			rwCfg.Clients[id] = clt

			conf.RemoteWrite = append(conf.RemoteWrite, &clt)
		}
	} else {
		// reset if remote-write is disabled at runtime
		conf.RemoteWrite = []*config.RemoteWriteConfig{}
	}

	return conf, nil
}

func (r *walRegistry) getTenantRemoteWriteConfig(tenant string, base RemoteWriteConfig) (*RemoteWriteConfig, error) {
	overrides, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("error generating tenant remote-write config: %w", err)
	}

	if r.overrides.RulerRemoteWriteDisabled(tenant) {
		overrides.Enabled = false
	}

	for id, clt := range overrides.Clients {
		clt.Name = fmt.Sprintf("%s-rw-%s", tenant, id)
		clt.SendExemplars = false
		// TODO(dannyk): configure HTTP client overrides
		// metadata is only used by prometheus scrape configs
		clt.MetadataConfig = config.MetadataConfig{Send: false}

		if v := r.overrides.RulerRemoteWriteConfig(tenant, id); v != nil {
			// overwrite, do not merge
			if v.Headers != nil {
				clt.Headers = maps.Clone(v.Headers)
			}

			// if any relabel configs are defined for a tenant, override all base relabel configs,
			// even if an empty list is configured; however if this value is not overridden for a tenant,
			// it should retain the base value
			if v.WriteRelabelConfigs != nil {
				clt.WriteRelabelConfigs = v.WriteRelabelConfigs
			}

			// Cast [rulerconfig.RemoteWriteConfig] to [config.RemoteWriteConfig] so it can be used to merge with [clt].
			// This can be done safely without copying, because the structs are identical
			// and mergo only reads [casted].
			casted := (*config.RemoteWriteConfig)(v)
			// merge with override
			if err := mergo.Merge(&clt, casted, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("failed to apply remote write clients configs: %w", err)
			}
		}

		if err := clt.Validate(model.UTF8Validation); err != nil {
			return nil, fmt.Errorf("invalid remote write config for tenant %q: %w", clt.Name, err)
		}

		overrides.Clients[id] = clt
	}

	return overrides, nil
}

// createRelabelConfigs converts the util.RelabelConfig into relabel.Config to allow for
// more control over json/yaml unmarshaling
func (r *walRegistry) createRelabelConfigs(tenant string) ([]*relabel.Config, error) {
	return nil, nil
}

var errNotReady = errors.New("appender not ready")

type notReadyAppender struct{}

func (n notReadyAppender) Append(_ storage.SeriesRef, _ labels.Labels, _ int64, _ float64) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendExemplar(_ storage.SeriesRef, _ labels.Labels, _ exemplar.Exemplar) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) UpdateMetadata(_ storage.SeriesRef, _ labels.Labels, _ metadata.Metadata) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendHistogram(_ storage.SeriesRef, _ labels.Labels, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendHistogramCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendHistogramSTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendSTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	return 0, errNotReady
}
func (n notReadyAppender) SetOptions(_ *storage.AppendOptions) {}
func (n notReadyAppender) Commit() error                       { return errNotReady }
func (n notReadyAppender) Rollback() error                     { return errNotReady }

type discardingAppender struct{}

func (n discardingAppender) Append(_ storage.SeriesRef, _ labels.Labels, _ int64, _ float64) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendExemplar(_ storage.SeriesRef, _ labels.Labels, _ exemplar.Exemplar) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) UpdateMetadata(_ storage.SeriesRef, _ labels.Labels, _ metadata.Metadata) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendHistogram(_ storage.SeriesRef, _ labels.Labels, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendHistogramCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendHistogramSTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) AppendSTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	return 0, nil
}
func (n discardingAppender) SetOptions(_ *storage.AppendOptions) {}
func (n discardingAppender) Commit() error                       { return nil }
func (n discardingAppender) Rollback() error                     { return nil }

type readyChecker interface {
	isReady(tenant string) bool
}

type tenantWALManager struct {
	logger log.Logger
	reg    prometheus.Registerer
}

func (t *tenantWALManager) newInstance(c instance.Config) (instance.ManagedInstance, error) {
	reg := prometheus.WrapRegistererWith(prometheus.Labels{
		"tenant": c.Tenant,
	}, t.reg)

	// create the instance with our custom walFactory
	return instance.New(reg, c, wal.NewMetrics(reg), t.logger)
}

type storageRegistryMetrics struct {
	reg prometheus.Registerer

	appenderReady *prometheus.GaugeVec
}

func newStorageRegistryMetrics(reg prometheus.Registerer) *storageRegistryMetrics {
	m := &storageRegistryMetrics{
		reg: reg,
		appenderReady: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "appender_ready",
			Help: "Whether a WAL appender is ready to accept samples (1) or not (0)",
		}, []string{"tenant"}),
	}

	if reg != nil {
		reg.MustRegister(
			m.appenderReady,
		)
	}

	return m
}

type nullRegistry struct{}

func (n nullRegistry) Appender(_ context.Context) storage.Appender { return discardingAppender{} }
func (n nullRegistry) isReady(_ string) bool                       { return true }
func (n nullRegistry) stop()                                       {}
func (n nullRegistry) configureTenantStorage(_ string)             {}
