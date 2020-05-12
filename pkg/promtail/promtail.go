package promtail

import (
	"sync"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/server"
	"github.com/grafana/loki/pkg/promtail/targets"
)

// Promtail is the root struct for Promtail...
type Promtail struct {
	client         client.Client
	targetManagers *targets.TargetManagers
	server         server.Server

	stopped bool
	mtx     sync.Mutex
}

// New makes a new Promtail.
func New(cfg config.Config, dryRun bool) (*Promtail, error) {

	if cfg.ClientConfig.URL.URL != nil {
		// if a single client config is used we add it to the multiple client config for backward compatibility
		cfg.ClientConfigs = append(cfg.ClientConfigs, cfg.ClientConfig)
	}

	var err error
	var cl client.Client
	if dryRun {
		cl, err = client.NewLogger(cfg.ClientConfigs...)
		if err != nil {
			return nil, err
		}
		cfg.PositionsConfig.ReadOnly = true
	} else {
		cl, err = client.NewMulti(util.Logger, cfg.ClientConfigs...)
		if err != nil {
			return nil, err
		}
	}

	promtail := &Promtail{
		client: cl,
	}

	tms, err := targets.NewTargetManagers(promtail, util.Logger, cfg.PositionsConfig, cl, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return nil, err
	}
	promtail.targetManagers = tms
	server, err := server.New(cfg.ServerConfig, tms)
	if err != nil {
		return nil, err
	}
	promtail.server = server
	return promtail, nil
}

// Run the promtail; will block until a signal is received.
func (p *Promtail) Run() error {
	p.mtx.Lock()
	// if we stopped promtail before the server even started we can return without starting.
	if p.stopped {
		p.mtx.Unlock()
		return nil
	}
	p.mtx.Unlock() // unlock before blocking
	return p.server.Run()
}

// Shutdown the promtail.
func (p *Promtail) Shutdown() {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.stopped = true
	if p.server != nil {
		p.server.Shutdown()
	}
	if p.targetManagers != nil {
		p.targetManagers.Stop()
	}
	p.client.Stop()
}
