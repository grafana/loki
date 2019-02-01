package promtail

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/targets"
)

// Promtail is the root struct for Promtail...
type Promtail struct {
	client         *client.Client
	positions      *positions.Positions
	targetManagers *targets.TargetManagers
	server         *server.Server
}

// New makes a new Promtail.
func New(cfg api.Config) (*Promtail, error) {
	positions, err := positions.New(util.Logger, cfg.PositionsConfig)
	if err != nil {
		return nil, err
	}

	client, err := client.New(cfg.ClientConfig, util.Logger)
	if err != nil {
		return nil, err
	}

	tms, err := targets.NewTargetManagers(util.Logger, positions, client, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return nil, err
	}

	server, err := server.New(cfg.ServerConfig)
	if err != nil {
		return nil, err
	}

	return &Promtail{
		client:         client,
		positions:      positions,
		targetManagers: tms,
		server:         server,
	}, nil
}

// Run the promtail; will block until a signal is received.
func (p *Promtail) Run() error {
	return p.server.Run()
}

// Shutdown the promtail.
func (p *Promtail) Shutdown() {
	p.server.Shutdown()
	p.targetManagers.Stop()
	p.positions.Stop()
	p.client.Stop()
}
