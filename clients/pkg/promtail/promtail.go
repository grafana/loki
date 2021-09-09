package promtail

import (
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/config"
	"github.com/grafana/loki/clients/pkg/promtail/server"
	"github.com/grafana/loki/clients/pkg/promtail/targets"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// Option is a function that can be passed to the New method of Promtail and
// customize the Promtail that is created.
type Option func(p *Promtail)

// WithLogger overrides the default logger for Promtail.
func WithLogger(log log.Logger) Option {
	return func(p *Promtail) {
		p.logger = log
	}
}

// WithRegisterer overrides the default registerer for Promtail.
func WithRegisterer(reg prometheus.Registerer) Option {
	return func(p *Promtail) {
		p.reg = reg
	}
}

// Promtail is the root struct for Promtail.
type Promtail struct {
	client         client.Client
	targetManagers *targets.TargetManagers
	server         server.Server
	logger         log.Logger
	reg            prometheus.Registerer

	stopped bool
	mtx     sync.Mutex
}

// New makes a new Promtail.
func New(cfg config.Config, dryRun bool, reg prometheus.Registerer, opts ...Option) (*Promtail, error) {
	// Initialize promtail with some defaults and allow the options to override
	// them.
	promtail := &Promtail{
		logger: util_log.Logger,
		reg:    prometheus.DefaultRegisterer,
	}
	for _, o := range opts {
		o(promtail)
	}

	if cfg.ClientConfig.URL.URL != nil {
		// if a single client config is used we add it to the multiple client config for backward compatibility
		cfg.ClientConfigs = append(cfg.ClientConfigs, cfg.ClientConfig)
	}

	// This is a bit crude but if the Loki Push API target is specified,
	// force the log level to match the promtail log level
	for i := range cfg.ScrapeConfig {
		if cfg.ScrapeConfig[i].PushConfig != nil {
			cfg.ScrapeConfig[i].PushConfig.Server.LogLevel = cfg.ServerConfig.LogLevel
			cfg.ScrapeConfig[i].PushConfig.Server.LogFormat = cfg.ServerConfig.LogFormat
		}
	}

	var err error
	if dryRun {
		promtail.client, err = client.NewLogger(prometheus.DefaultRegisterer, promtail.logger, cfg.ClientConfig.ExternalLabels, cfg.ClientConfigs...)
		if err != nil {
			return nil, err
		}
		cfg.PositionsConfig.ReadOnly = true
	} else {
		promtail.client, err = client.NewMulti(prometheus.DefaultRegisterer, promtail.logger, cfg.ClientConfig.ExternalLabels, cfg.ClientConfigs...)
		if err != nil {
			return nil, err
		}
	}

	tms, err := targets.NewTargetManagers(promtail, promtail.reg, promtail.logger, cfg.PositionsConfig, promtail.client, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return nil, err
	}
	promtail.targetManagers = tms
	server, err := server.New(cfg.ServerConfig, promtail.logger, tms, cfg.String())
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

// Client returns the underlying client Promtail uses to write to Loki.
func (p *Promtail) Client() client.Client {
	return p.client
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
	// todo work out the stop.
	p.client.Stop()
}
