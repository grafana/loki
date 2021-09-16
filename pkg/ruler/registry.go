package ruler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
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
		cleaner: cleaner.NewWALCleaner(
			logger,
			manager,
			cleaner.NewMetrics(reg),
			config.WAL.Path,
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

	inst := r.Get(tenant)
	if inst == nil {
		level.Warn(r.logger).Log("user", tenant, "msg", "not ready")
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
	// make a copy
	conf := r.config.WAL

	conf.Name = tenant
	conf.Tenant = tenant

	// we don't need to send metadata - we have no scrape targets
	r.config.RemoteWrite.Client.MetadataConfig.Send = false

	// retrieve remote-write config for this tenant, using the global remote-write for defaults
	rwCfg := r.overrides.RulerRemoteWrite(tenant, r.config.RemoteWrite)

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
