package planner

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

type Planner struct {
	services.Service

	cfg     Config
	metrics *Metrics
	logger  log.Logger
}

func New(
	cfg Config,
	logger log.Logger,
	r prometheus.Registerer,
) (*Planner, error) {
	utillog.WarnExperimentalUse("Bloom Planner", logger)

	p := &Planner{
		cfg:     cfg,
		metrics: NewMetrics(r),
		logger:  logger,
	}

	p.Service = services.NewBasicService(p.starting, p.running, p.stopping)
	return p, nil
}

func (p *Planner) starting(_ context.Context) (err error) {
	p.metrics.running.Set(1)
	return err
}

func (p *Planner) stopping(_ error) error {
	p.metrics.running.Set(0)
	return nil
}

func (p *Planner) running(ctx context.Context) error {
	// run once at beginning
	if err := p.runOne(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(p.cfg.PlanningInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			level.Debug(p.logger).Log("msg", "planner context done", "err", err)
			return err

		case <-ticker.C:
			if err := p.runOne(ctx); err != nil {
				return err
			}
		}
	}
}

func (p *Planner) runOne(_ context.Context) error {
	return nil
}
