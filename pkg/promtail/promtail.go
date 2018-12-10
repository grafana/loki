package promtail

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/server"
)

// Promtail is the root struct for Promtail...
type Promtail struct {
	client        *Client
	positions     *Positions
	targetManager *TargetManager
	server        *server.Server
}

// New makes a new Promtail.
func New(cfg Config) (*Promtail, error) {
	positions, err := NewPositions(util.Logger, cfg.PositionsConfig)
	if err != nil {
		return nil, err
	}

	client, err := NewClient(cfg.ClientConfig, util.Logger)
	if err != nil {
		return nil, err
	}

	tm, err := NewTargetManager(util.Logger, positions, client, cfg.ScrapeConfig)
	if err != nil {
		return nil, err
	}

	server, err := server.New(cfg.ServerConfig)
	if err != nil {
		return nil, err
	}

	return &Promtail{
		client:        client,
		positions:     positions,
		targetManager: tm,
		server:        server,
	}, nil
}

// Run the promtail; will block until a signal is received.
func (p *Promtail) Run() error {
	return p.server.Run()
}

// Shutdown the promtail.
func (p *Promtail) Shutdown() {
	p.server.Shutdown()
	p.targetManager.Stop()
	p.client.Stop()
}
