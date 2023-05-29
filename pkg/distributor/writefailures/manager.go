package writefailures

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"
)

type Manager struct {
	limiter *limiter.RateLimiter
	logger  log.Logger
}

func NewManager(logger log.Logger, cfg Cfg) *Manager {
	logger = log.With(logger, "path", "write")

	if cfg.AddInsightsLabel {
		logger = log.With(logger, "insight", "true")
	}

	strat := newStrategy(cfg.LogRate, float64(cfg.LogRate))

	return &Manager{
		limiter: limiter.NewRateLimiter(strat, time.Minute),
		logger:  logger,
	}
}

func (m *Manager) Log(tenantID string, err error) {
	if m.limiter.AllowN(time.Now(), tenantID, 1) {
		level.Error(m.logger).Log("msg", "write operation failed", "err", err)
	}
}
