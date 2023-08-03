package congestion

func NewController(cfg Config, metrics *Metrics) Controller {
	return newController(cfg).
		WithRetrier(newRetrier(cfg)).
		WithHedger(newHedger(cfg)).
		WithMetrics(metrics)
}

func newController(cfg Config) Controller {
	switch cfg.Controller.Strategy {
	case "aimd", "AIMD":
		return NewAIMDController(cfg)
	default:
		return NewNoopController(cfg)
	}
}

func newRetrier(cfg Config) Retrier {
	switch cfg.Retry.Strategy {
	case "limited":
		return NewLimitedRetrier(cfg)
	default:
		return NewNoopRetrier(cfg)
	}
}

func newHedger(cfg Config) Hedger {
	switch cfg.Hedge.Strategy {
	default:
		return NewNoopHedger(cfg)
	}
}
