package writefailures

import (
	"context"
	"errors"
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

	if m.skipError(err) {
		return
	}

	errMsg := err.Error()
	if m.limiter.AllowN(time.Now(), tenantID, len(errMsg)) {
		level.Error(m.logger).Log("msg", "write operation failed", "details", errMsg)
	}
}

// skipError dictates if a given error should be ignored or not.
//
// We should ignore errors that weren't caused by an inappropriate write.
// An inappropriate write would cause other errors instead.
//
// Common scenarios where we shouldn't report errors:
// - Overloaded ingesters causing write timeouts
// - Distributor OOMing causing cancellations
// - Ingester OOMing causing cancellations
func (m *Manager) skipError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	return false
}
