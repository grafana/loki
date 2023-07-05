package writefailures

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"

	"github.com/grafana/loki/pkg/runtime"
)

type Manager struct {
	limiter    *limiter.RateLimiter
	logger     log.Logger
	tenantCfgs *runtime.TenantConfigs
}

func NewManager(logger log.Logger, cfg Cfg, tenants *runtime.TenantConfigs) *Manager {
	logger = log.With(logger, "path", "write")
	if cfg.AddInsightsLabel {
		logger = log.With(logger, "insight", "true")
	}

	strategy := newStrategy(cfg.LogRate.Val(), float64(cfg.LogRate.Val()))

	return &Manager{
		limiter:    limiter.NewRateLimiter(strategy, time.Minute),
		logger:     logger,
		tenantCfgs: tenants,
	}
}

func (m *Manager) Log(tenantID string, err error) {
	if m == nil {
		return
	}

	if !m.tenantCfgs.LimitedLogPushErrors(tenantID) {
		return
	}

	errMsg := err.Error()
	if m.limiter.AllowN(time.Now(), tenantID, len(errMsg)) {
		level.Error(m.logger).Log("msg", "write operation failed", "details", errMsg, "tenant", tenantID)
	}
}
