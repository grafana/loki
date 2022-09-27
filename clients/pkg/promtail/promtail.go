package promtail

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/config"
	"github.com/grafana/loki/clients/pkg/promtail/server"
	"github.com/grafana/loki/clients/pkg/promtail/targets"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

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

	stopped    bool
	mtx        sync.Mutex
	configFile string
	newConfig  func() *config.Config
	metrics    *client.Metrics
	dryRun     bool
}

// New makes a new Promtail.
func New(cfg config.Config, newConfig func() *config.Config, metrics *client.Metrics, dryRun bool, opts ...Option) (*Promtail, error) {
	// Initialize promtail with some defaults and allow the options to override
	// them.

	promtail := &Promtail{
		logger:  util_log.Logger,
		reg:     prometheus.DefaultRegisterer,
		metrics: metrics,
		dryRun:  dryRun,
	}
	for _, o := range opts {
		// todo (callum) I don't understand why I needed to add this check
		if o == nil {
			continue
		}
		o(promtail)
	}
	err := promtail.reloadConfig(&cfg)
	if err != nil {
		return nil, err
	}
	server, err := server.New(cfg.ServerConfig, promtail.logger, promtail.targetManagers, cfg.String())
	if err != nil {
		return nil, errors.Wrap(err, "error creating loki server")
	}
	promtail.server = server
	promtail.newConfig = newConfig

	return promtail, nil
}

func (p *Promtail) reloadConfig(cfg *config.Config) error {
	level.Info(p.logger).Log("msg", "Reloading configuration file")
	p.mtx.Lock()
	defer p.mtx.Unlock()
	if p.targetManagers != nil {
		p.targetManagers.Stop()
	}
	if p.client != nil {
		p.client.Stop()
	}

	cfg.Setup(p.logger)
	if cfg.LimitsConfig.ReadlineRateEnabled {
		stages.SetReadLineRateLimiter(cfg.LimitsConfig.ReadlineRate, cfg.LimitsConfig.ReadlineBurst, cfg.LimitsConfig.ReadlineRateDrop)
	}
	var err error
	if p.dryRun {
		p.client, err = client.NewLogger(p.metrics, cfg.Options.StreamLagLabels, p.logger, cfg.ClientConfigs...)
		if err != nil {
			return err
		}
		cfg.PositionsConfig.ReadOnly = true
	} else {
		p.client, err = client.NewMulti(p.metrics, cfg.Options.StreamLagLabels, p.logger, cfg.LimitsConfig.MaxStreams, cfg.ClientConfigs...)
		if err != nil {
			return err
		}
	}

	tms, err := targets.NewTargetManagers(p, p.reg, p.logger, cfg.PositionsConfig, p.client, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return err
	}
	p.targetManagers = tms

	promServer := p.server
	if promServer != nil {
		promtailServer := promServer.(*server.PromtailServer)
		promtailServer.ReloadServer(p.targetManagers, cfg.String())
	}

	return nil
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
	go p.watchConfig()
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
	if p.stopped {
		return
	}
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

// ActiveTargets returns active targets per jobs from the target manager
func (p *Promtail) ActiveTargets() map[string][]target.Target {
	return p.targetManagers.ActiveTargets()
}

func (p *Promtail) watchConfig() {
	// Reload handler.
	// Make sure that sighup handler is registered with a redirect to the channel before the potentially
	if p.newConfig == nil {
		level.Warn(p.logger).Log("msg", "disable watchConfig")
		return
	}
	level.Warn(p.logger).Log("msg", "enable watchConfig")
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	promtailServer := p.server.(*server.PromtailServer)
	for {
		select {
		case <-hup:
			cfg := p.newConfig()
			if err := p.reloadConfig(cfg); err != nil {
				level.Error(p.logger).Log("msg", "Error reloading config", "err", err)
			}
		case rc := <-promtailServer.Reload():
			cfg := p.newConfig()
			if err := p.reloadConfig(cfg); err != nil {
				level.Error(p.logger).Log("msg", "Error reloading config", "err", err)
				rc <- err
			} else {
				rc <- nil
			}
		}
	}
}
