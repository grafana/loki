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
	logger = log.With(logger, "path", "write", "insight", "true")

	start := newStrategy(cfg.LogRate.Val(), float64(cfg.LogRate.Val()))

	return &Manager{
		limiter:    limiter.NewRateLimiter(start, time.Minute),
		logger:     logger,
		tenantCfgs: tenants,
	}
}

func (m *Manager) Log(tenantID string, err error) {
	if !m.tenantCfgs.LimitedLogPushErrors(tenantID) {
		return
	}

	errMsg := err.Error()
	if m.limiter.AllowN(time.Now(), tenantID, len(errMsg)) {
		level.Error(m.logger).Log("msg", "write operation failed", "err", errMsg)
	}
}
