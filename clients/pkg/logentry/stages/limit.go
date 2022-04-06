package stages

import (
	"context"

	"github.com/go-kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

const (
	ErrLimitStageInvalidRateOrBurst = "limit stage failed to parse rate or burst"
)

var ratelimitDropReason = "ratelimit_drop_stage"

type LimitConfig struct {
	Rate  float64 `mapstructure:"rate"`
	Burst int     `mapstructure:"burst"`
	Drop  bool    `mapstructure:"drop"`
}

func newLimitStage(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &LimitConfig{}

	err := mapstructure.WeakDecode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateLimitConfig(cfg)
	if err != nil {
		return nil, err
	}

	r := &limitStage{
		logger:      log.With(logger, "component", "stage", "type", "limit"),
		cfg:         cfg,
		dropCount:   getDropCountMetric(registerer),
		rateLimiter: rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
	}
	return r, nil
}

func validateLimitConfig(cfg *LimitConfig) error {
	if cfg.Rate <= 0 || cfg.Burst <= 0 {
		return errors.Errorf(ErrLimitStageInvalidRateOrBurst)
	}
	return nil
}

// limitStage applies Label matchers to determine if the include stages should be run
type limitStage struct {
	logger      log.Logger
	cfg         *LimitConfig
	rateLimiter *rate.Limiter
	dropCount   *prometheus.CounterVec
}

func (m *limitStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			if !m.shouldThrottle() {
				out <- e
				continue
			}
		}
	}()
	return out
}

func (m *limitStage) shouldThrottle() bool {
	if m.cfg.Drop {
		if m.rateLimiter.Allow() {
			return false
		}
		m.dropCount.WithLabelValues(ratelimitDropReason).Inc()
		return true
	}
	_ = m.rateLimiter.Wait(context.Background())
	return false
}

// Name implements Stage
func (m *limitStage) Name() string {
	return StageTypeLimit
}
