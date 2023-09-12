package congestion

import (
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func NewController(cfg Config, logger log.Logger, metrics *Metrics) Controller {
	logger = log.With(logger, "component", "congestion_control")

	return newController(cfg, logger).
		withRetrier(newRetrier(cfg, logger)).
		withHedger(newHedger(cfg, logger)).
		withMetrics(metrics)
}

func newController(cfg Config, logger log.Logger) Controller {
	strat := strings.ToLower(cfg.Controller.Strategy)
	switch strat {
	case "aimd":
		return NewAIMDController(cfg).withLogger(logger)
	default:
		level.Warn(logger).Log("msg", "unrecognized congestion control strategy in config, using noop", "strategy", strat)
		return NewNoopController(cfg).withLogger(logger)
	}
}

func newRetrier(cfg Config, logger log.Logger) Retrier {
	strat := strings.ToLower(cfg.Retry.Strategy)
	switch strat {
	case "limited":
		return NewLimitedRetrier(cfg).withLogger(logger)
	default:
		level.Warn(logger).Log("msg", "unrecognized retried strategy in config, using noop", "strategy", strat)
		return NewNoopRetrier(cfg).withLogger(logger)
	}
}

func newHedger(cfg Config, logger log.Logger) Hedger {
	strat := strings.ToLower(cfg.Hedge.Strategy)
	switch strat {
	default:
		level.Warn(logger).Log("msg", "unrecognized hedging strategy in config, using noop", "strategy", strat)
		return NewNoopHedger(cfg).withLogger(logger)
	}
}
