package congestion

func NewController(cfg Config, metrics *Metrics) Controller {
	ctrl := newController(cfg)
	retry := newRetrier(cfg)
	hedge := newHedger(cfg)

	// TODO(dannyk): fluid interface?
	ctrl.WithRetrier(retry)
	ctrl.WithHedger(hedge)
	ctrl.WithMetrics(metrics)

	return ctrl
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
