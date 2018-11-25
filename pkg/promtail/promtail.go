package promtail

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/server"
)

type Promtail struct {
	client        *Client
	positions     *Positions
	targetManager *TargetManager
	server        *server.Server
}

func New(cfg Config) (*Promtail, error) {
	client, err := NewClient(cfg.ClientConfig, util.Logger)
	if err != nil {
		return nil, err
	}

	positions, err := NewPositions(cfg.PositionsConfig, util.Logger)
	if err != nil {
		return nil, err
	}

	newTargetFunc := func(path string, labels model.LabelSet) (*Target, error) {
		return NewTarget(util.Logger, client, positions, path, labels)
	}
	tm, err := NewTargetManager(util.Logger, cfg.ScrapeConfig, newTargetFunc)
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

func (p *Promtail) Run() error {
	return p.server.Run()
}

func (p *Promtail) Shutdown() {
	p.server.Shutdown()
	p.targetManager.Stop()
	p.client.Stop()
}
