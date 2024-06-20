package writefailures

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/runtime"
)

type Manager struct {
	limiter    *limiter.RateLimiter
	logger     log.Logger
	tenantCfgs *runtime.TenantConfigs
	metrics    *metrics
}

func NewManager(logger log.Logger, reg prometheus.Registerer, cfg Cfg, tenants *runtime.TenantConfigs, subsystem string) *Manager {
	logger = log.With(logger, "path", "write")
	if cfg.AddInsightsLabel {
		logger = log.With(logger, "insight", "true")
	}

	strategy := newStrategy(cfg.LogRate.Val(), float64(cfg.LogRate.Val()))

	return &Manager{
		limiter:    limiter.NewRateLimiter(strategy, time.Minute),
		logger:     logger,
		tenantCfgs: tenants,
		metrics:    newMetrics(reg, subsystem),
	}
}

func (m *Manager) Log(tenantID string, err error) {
	if m == nil {
		return
	}

	if !(m.tenantCfgs.LimitedLogPushErrors(tenantID) ||
		m.tenantCfgs.LogDuplicateStreamInfo(tenantID)) {
		return
	}

	errMsg := err.Error()
	if m.limiter.AllowN(time.Now(), tenantID, len(errMsg)) {
		m.metrics.loggedCount.WithLabelValues(tenantID).Inc()
		level.Error(m.logger).Log("msg", "write operation failed", "details", errMsg, "org_id", tenantID)
		return
	}

	m.metrics.discardedCount.WithLabelValues(tenantID).Inc()
}
