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
	c := &Compactor{
		cfg:    cfg,
		logger: logger,
	}
	c.Service = services.NewIdleService(c.starting, c.stopping)

	return c, nil
}

func (c *Compactor) starting(_ context.Context) error {
	return nil
}

func (c *Compactor) stopping(_ error) error {
	return nil
}
