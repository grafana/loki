package limits

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

type IngestLimiter struct {
	cfg Config
	logger log.Logger

	services.Service
}

func New(cfg Config, logger log.Logger, r prometheus.Registerer) (*IngestLimiter, error) {
	l := &IngestLimiter{
		cfg: cfg,
		logger: logger,
	}
	l.Service = services.NewBasicService(l.starting, l.running, l.stopping)
	return l, nil
}

func (l *IngestLimiter) starting(ctx context.Context) error {
	return nil
}

func (l *IngestLimiter) running(ctx context.Context) error {
	return nil
}

func (l *IngestLimiter) stopping(_ error) error {
	return nil
}
