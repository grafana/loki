package bloomcompactor

import (
	"context"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

type Compactor struct {
	services.Service

	cfg    Config
	logger log.Logger
}

//TODO do we want to configure retention in the bloom compactors?
//var (
//	retentionEnabledStats = analytics.NewString("compactor_retention_enabled")
//	defaultRetentionStats = analytics.NewString("compactor_default_retention")
//)

func New(cfg Config, logger log.Logger, _ prometheus.Registerer) (*Compactor, error) {
	g := &Compactor{
		cfg:    cfg,
		logger: logger,
	}
	g.Service = services.NewIdleService(g.starting, g.stopping)

	return g, nil
}

func (g *Compactor) starting(_ context.Context) error {
	return nil
}

func (g *Compactor) stopping(_ error) error {
	return nil
}
