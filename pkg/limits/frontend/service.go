package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

type IngestLimits struct {
	cfg     Config
	logger  log.Logger
	metrics *Metrics
	services.Service
}

func New(cfg Config, logger log.Logger, r prometheus.Registerer) (*IngestLimits, error) {
	l := &IngestLimits{
		cfg:     cfg,
		logger:  logger,
		metrics: NewMetrics(r),
	}
	l.Service = services.NewBasicService(l.starting, l.running, l.stopping)
	return l, nil
}

func (l *IngestLimits) starting(_ context.Context) error {
	return nil
}

func (l *IngestLimits) running(_ context.Context) error {
	return nil
}

func (l *IngestLimits) stopping(_ context.Context) error {
	return nil
}
