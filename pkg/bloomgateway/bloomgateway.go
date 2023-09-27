package bloomgateway

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

type Gateway struct {
	services.Service

	cfg    Config
	logger log.Logger
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, logger log.Logger, _ prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:    cfg,
		logger: logger,
	}
	g.Service = services.NewIdleService(g.starting, g.stopping)

	return g, nil
}

func (g *Gateway) starting(_ context.Context) error {
	return nil
}

func (g *Gateway) stopping(_ error) error {
	return nil
}
