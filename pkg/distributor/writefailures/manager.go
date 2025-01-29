package writefailures

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"
	"github.com/prometheus/client_golang/prometheus"
)

type Limits interface {
	LogStreamCreation(userID string) bool
	LogPushRequest(userID string) bool
	LogPushRequestStreams(userID string) bool
	LogDuplicateMetrics(userID string) bool
	LogDuplicateStreamInfo(userID string) bool
	LimitedLogPushErrors(userID string) bool
}

type Manager struct {
	Limits
	limiter *limiter.RateLimiter
	logger  log.Logger
	metrics *metrics
}

func NewManager(logger log.Logger, reg prometheus.Registerer, cfg Cfg, overrides Limits, subsystem string) *Manager {
	logger = log.With(logger, "path", "write")
	if cfg.AddInsightsLabel {
		logger = log.With(logger, "insight", "true")
	}

	strategy := newStrategy(cfg.LogRate.Val(), float64(cfg.LogRate.Val()))

	return &Manager{
		Limits:  overrides,
		limiter: limiter.NewRateLimiter(strategy, time.Minute),
		logger:  logger,
		metrics: newMetrics(reg, subsystem),
	}
}

func (m *Manager) Log(tenantID string, err error) {
	if m == nil {
		return
	}

	if !(m.LimitedLogPushErrors(tenantID) ||
		m.LogDuplicateStreamInfo(tenantID)) {
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
