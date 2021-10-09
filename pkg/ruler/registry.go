package ruler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/pkg/ruler/storage/instance"
	"github.com/grafana/loki/pkg/ruler/storage/wal"
)

type walRegistry struct {
	logger  log.Logger
	manager instance.Manager

	metrics *storageRegistryMetrics

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
	tenant, _ := user.ExtractOrgID(ctx)
	rwCfg, err := r.getTenantRemoteWriteConfig(tenant, r.config.RemoteWrite)
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
	if r.lastUpdateTime.Before(now.Add(-r.config.RemoteWrite.ConfigRefreshPeriod)) {
		r.lastUpdateTime = now

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
	conf, err := r.config.WAL.Clone()
	if err != nil {
		return instance.Config{}, err
	}

	conf.Name = tenant
	conf.Tenant = tenant

	// we don't need to send metadata - we have no scrape targets
	r.config.RemoteWrite.Client.MetadataConfig.Send = false

	// retrieve remote-write config for this tenant, using the global remote-write for defaults
	rwCfg, err := r.getTenantRemoteWriteConfig(tenant, r.config.RemoteWrite)
	if err != nil {
		return instance.Config{}, err
	}

	// TODO(dannyk): implement multiple RW configs
	if rwCfg.Enabled {
		if rwCfg.Client.Headers == nil {
			rwCfg.Client.Headers = make(map[string]string)
		}

		// ensure that no variation of the X-Scope-OrgId header can be added, which might trick authentication
		for k := range rwCfg.Client.Headers {
			if strings.ToLower(user.OrgIDHeaderName) == strings.ToLower(strings.TrimSpace(k)) {
				delete(rwCfg.Client.Headers, k)
			}
		}

		// always inject the X-Scope-OrgId header for multi-tenant metrics backends
		rwCfg.Client.Headers[user.OrgIDHeaderName] = tenant

		conf.RemoteWrite = []*config.RemoteWriteConfig{
			&rwCfg.Client,
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

	overrides.Client.Name = fmt.Sprintf("%s-rw", tenant)
	overrides.Client.SendExemplars = false
	// TODO(dannyk): configure HTTP client overrides
	// metadata is only used by prometheus scrape configs
	overrides.Client.MetadataConfig = config.MetadataConfig{Send: false}
	overrides.Client.SigV4Config = nil

	if r.overrides.RulerRemoteWriteDisabled(tenant) {
		overrides.Enabled = false
	}

	if v := r.overrides.RulerRemoteWriteURL(tenant); v != "" {
		u, err := url.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("error parsing given remote-write URL: %w", err)
		}
		overrides.Client.URL = &promConfig.URL{u}
	}

	if v := r.overrides.RulerRemoteWriteTimeout(tenant); v > 0 {
		overrides.Client.RemoteTimeout = model.Duration(v)
	}

	// overwrite, do not merge
	if v := r.overrides.RulerRemoteWriteHeaders(tenant); v != nil {
		overrides.Client.Headers = v
	}

	relabelConfigs, err := r.createRelabelConfigs(tenant)
	if err != nil {
		return nil, fmt.Errorf("failed to parse relabel configs: %w", err)
	}

	// if any relabel configs are defined for a tenant, override all base relabel configs,
	// even if an empty list is configured; however if this value is not overridden for a tenant,
	// it should retain the base value
	if relabelConfigs != nil {
		overrides.Client.WriteRelabelConfigs = relabelConfigs
	}

	if v := r.overrides.RulerRemoteWriteQueueCapacity(tenant); v > 0 {
		overrides.Client.QueueConfig.Capacity = v
	}

	if v := r.overrides.RulerRemoteWriteQueueMinShards(tenant); v > 0 {
		overrides.Client.QueueConfig.MinShards = v
	}

	if v := r.overrides.RulerRemoteWriteQueueMaxShards(tenant); v > 0 {
		overrides.Client.QueueConfig.MaxShards = v
	}

	if v := r.overrides.RulerRemoteWriteQueueMaxSamplesPerSend(tenant); v > 0 {
		overrides.Client.QueueConfig.MaxSamplesPerSend = v
	}

	if v := r.overrides.RulerRemoteWriteQueueMinBackoff(tenant); v > 0 {
		overrides.Client.QueueConfig.MinBackoff = model.Duration(v)
	}

	if v := r.overrides.RulerRemoteWriteQueueMaxBackoff(tenant); v > 0 {
		overrides.Client.QueueConfig.MaxBackoff = model.Duration(v)
	}

	if v := r.overrides.RulerRemoteWriteQueueBatchSendDeadline(tenant); v > 0 {
		overrides.Client.QueueConfig.BatchSendDeadline = model.Duration(v)
	}

	if v := r.overrides.RulerRemoteWriteQueueRetryOnRateLimit(tenant); v {
		overrides.Client.QueueConfig.RetryOnRateLimit = v
	}

	return overrides, nil
}

// createRelabelConfigs converts the util.RelabelConfig into relabel.Config to allow for
// more control over json/yaml unmarshaling
func (r *walRegistry) createRelabelConfigs(tenant string) ([]*relabel.Config, error) {
	configs := r.overrides.RulerRemoteWriteRelabelConfigs(tenant)

	// zero value is nil, which we want to treat as "no override"
	if configs == nil {
		return nil, nil
	}

	// we want to treat an empty slice as "no relabel configs"
	relabelConfigs := make([]*relabel.Config, len(configs))
	for i, config := range configs {
		out, err := yaml.Marshal(config)
		if err != nil {
			return nil, err
		}

		var rc relabel.Config
		if err = yaml.Unmarshal(out, &rc); err != nil {
			return nil, err
		}

		relabelConfigs[i] = &rc
	}

	return relabelConfigs, nil
}

var errNotReady = errors.New("appender not ready")

type notReadyAppender struct{}

func (n notReadyAppender) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, errNotReady
}
func (n notReadyAppender) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, errNotReady
}
func (n notReadyAppender) Commit() error   { return errNotReady }
func (n notReadyAppender) Rollback() error { return errNotReady }

type discardingAppender struct{}

func (n discardingAppender) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, nil
}
func (n discardingAppender) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, nil
}
func (n discardingAppender) Commit() error   { return nil }
func (n discardingAppender) Rollback() error { return nil }

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

	// create metrics here and pass down
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

func (n nullRegistry) Appender(ctx context.Context) storage.Appender { return discardingAppender{} }
func (n nullRegistry) isReady(tenant string) bool                    { return true }
func (n nullRegistry) stop()                                         {}
func (n nullRegistry) configureTenantStorage(tenant string)          {}
