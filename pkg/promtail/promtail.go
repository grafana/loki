package promtail

import (
	"net/http"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/targets"

	fsnotify "gopkg.in/fsnotify/fsnotify.v1"
)

// Promtail is the root struct for Promtail...
type Promtail struct {
	client         *client.Client
	positions      *positions.Positions
	targetManagers *targets.TargetManagers
	server         *server.Server

	watcher    *fsnotify.Watcher
	configFile string
}

// New makes a new Promtail.
func New(configFile string) (*Promtail, error) {
	var cfg config.Config

	if err := helpers.LoadConfig(configFile, &cfg); err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	p := &Promtail{configFile: configFile, watcher: watcher}

	if err := p.watchConfig(); err != nil {
		return nil, err
	}

	if err := p.refresh(); err != nil {
		return nil, err
	}

	p.server.HTTP.Path("/ready").Handler(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if p.targetManagers.Ready() {
			rw.WriteHeader(http.StatusNoContent)
		} else {
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}))

	return p, nil
}

// Run the promtail; will block until a signal is received or watcher failed.
func (p *Promtail) Run() error {
	defer p.Shutdown()

	p.server.Run()

	for {
		select {
		case event := <-p.watcher.Events:
			if event.Op^fsnotify.Write == 0 {
				if err := p.refresh(); err != nil {
					return err
				}
			}
		case err := <-p.watcher.Errors:
			return err
		}
	}
}

// Shutdown the promtail.
func (p *Promtail) Shutdown() {
	p.server.Shutdown()
	p.targetManagers.Stop()
	p.positions.Stop()
	p.client.Stop()
}

// watchFiles sets watches on config file
func (p *Promtail) watchConfig() error {
	if p.watcher == nil {
		panic("no watcher configured")
	}
	if err := p.watcher.Add(p.configFile); err != nil {
		return err
	}
	return nil
}

// Refresh config
func (p *Promtail) refresh() error {
	var cfg config.Config

	if err := helpers.LoadConfig(p.configFile, &cfg); err != nil {
		return err
	}

	positions, err := positions.New(util.Logger, cfg.PositionsConfig)
	if err != nil {
		return err
	}

	client, err := client.New(cfg.ClientConfig, util.Logger)
	if err != nil {
		return err
	}

	tms, err := targets.NewTargetManagers(util.Logger, positions, client, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return err
	}

	server, err := server.New(cfg.ServerConfig)
	if err != nil {
		return err
	}

	p.client = client
	p.positions = positions
	p.targetManagers = tms
	p.server = server

	return nil
}
