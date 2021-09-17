package ruler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/imdario/mergo"
	"github.com/prometheus/client_golang/prometheus"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/pkg/ruler/storage/instance"
	"github.com/grafana/loki/pkg/ruler/storage/wal"
)

type walRegistry struct {
	sync.RWMutex

	logger  log.Logger
	manager instance.Manager

	metrics *storageRegistryMetrics

	config         Config
	overrides      RulesLimits
	lastUpdateTime time.Time
	cleaner        *cleaner.WALCleaner
}

func newStorageRegistry(logger log.Logger, reg prometheus.Registerer, config Config, overrides RulesLimits) *walRegistry {
	manager := createInstanceManager(logger, reg)

	return &walRegistry{
		logger:    logger,
		metrics:   newStorageRegistryMetrics(reg),
		config:    config,
		overrides: overrides,
		manager:   manager,

		// TODO(dannyk): once we have a way to know when a rulegroup has been unregistered,
		// 				 we can enable the WAL cleaner - which cleans up WALs that are no longer managed
		//cleaner: cleaner.NewWALCleaner(
		//	logger,
		//	manager,
		//	cleaner.NewMetrics(reg),
		//	config.WAL.Dir,
		//	config.WALCleaner),
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

func (r *walRegistry) IsReady(tenant string) bool {
	return r.manager.InstanceReady(tenant)
}

func (r *walRegistry) Get(tenant string) storage.Storage {
	r.RLock()
	defer r.RUnlock()

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

	inst := r.Get(tenant)
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
	conf, err := r.config.WAL.Clone()
	if err != nil {
		level.Error(r.logger).Log("msg", "error configuring tenant storage", "user", tenant, "err", err)
		return
	}

	conf.Name = tenant
	conf.Tenant = tenant

	// we don't need to send metadata - we have no scrape targets
	r.config.RemoteWrite.Client.MetadataConfig.Send = false

	// retrieve remote-write config for this tenant, using the global remote-write for defaults
	rwCfg, err := r.getTenantRemoteWriteConfig(tenant, r.config.RemoteWrite)
	if err != nil {
		level.Error(r.logger).Log("msg", "error retrieving remote-write config", "user", tenant, "err", err)
		return
	}

	// TODO(dannyk): implement multiple RW configs
	if rwCfg.Enabled {
		if rwCfg.Client.Headers == nil {
			rwCfg.Client.Headers = make(map[string]string)
		}

		// always inject the X-Org-ScopeId header for multi-tenant metrics backends
		rwCfg.Client.Headers[user.OrgIDHeaderName] = tenant

		conf.RemoteWrite = []*config.RemoteWriteConfig{
			&rwCfg.Client,
		}
	} else {
		// reset if remote-write is disabled at runtime
		conf.RemoteWrite = []*config.RemoteWriteConfig{}
	}

	if err := r.manager.ApplyConfig(conf); err != nil {
		level.Error(r.logger).Log("user", tenant, "msg", "could not apply given config", "err", err)
	}
}

func (r *walRegistry) Stop() {
	r.Lock()
	defer r.Unlock()

	if r.cleaner != nil {
		r.cleaner.Stop()
	}

	r.manager.Stop()
}

func (r *walRegistry) getTenantRemoteWriteConfig(tenant string, base RemoteWriteConfig) (*RemoteWriteConfig, error) {
	copy, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("error generating tenant remote-write config: %w", err)
	}

	u, err := url.Parse(r.overrides.RulerRemoteWriteURL(tenant))
	if err != nil {
		return nil, fmt.Errorf("error parsing given remote-write URL: %w", err)
	}

	overrides := RemoteWriteConfig{
		Client:  config.RemoteWriteConfig{
			URL:                 &promConfig.URL{u},
			RemoteTimeout:       model.Duration(r.overrides.RulerRemoteWriteTimeout(tenant)),
			Headers:             r.overrides.RulerRemoteWriteHeaders(tenant),
			WriteRelabelConfigs: r.overrides.RulerRemoteWriteRelabelConfigs(tenant),
			Name:                fmt.Sprintf("%s-rw", tenant),
			SendExemplars:       false,
			HTTPClientConfig:    promConfig.HTTPClientConfig{}, // TODO: configure
			QueueConfig:         config.QueueConfig{
				Capacity:          r.overrides.RulerRemoteWriteQueueCapacity(tenant),
				MaxShards:         r.overrides.RulerRemoteWriteQueueMaxShards(tenant),
				MinShards:         r.overrides.RulerRemoteWriteQueueMinShards(tenant),
				MaxSamplesPerSend: r.overrides.RulerRemoteWriteQueueMaxSamplesPerSend(tenant),
				BatchSendDeadline: model.Duration(r.overrides.RulerRemoteWriteQueueBatchSendDeadline(tenant)),
				MinBackoff:        model.Duration(r.overrides.RulerRemoteWriteQueueMinBackoff(tenant)),
				MaxBackoff:        model.Duration(r.overrides.RulerRemoteWriteQueueMaxBackoff(tenant)),
				RetryOnRateLimit:  r.overrides.RulerRemoteWriteQueueRetryOnRateLimit(tenant),
			},
			MetadataConfig:      config.MetadataConfig{
				Send: false,
			},
			SigV4Config:         nil,
		},
		Enabled: true,
	}

	err = mergo.Merge(copy, overrides, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	// we can't use mergo.WithOverwriteWithEmptyValue since that will set all the default values, so here we
	// explicitly apply some config options that might be set to their type's zero value
	if r.overrides.RulerRemoteWriteDisabled(tenant) {
		copy.Enabled = false
	}

	return copy, nil
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
	IsReady(tenant string) bool
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
