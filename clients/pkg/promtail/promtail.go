package promtail

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/config"
	"github.com/grafana/loki/v3/clients/pkg/promtail/server"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/v3/clients/pkg/promtail/utils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/wal"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	timeoutUntilFanoutHardStop = time.Second * 30
)

var reloadSuccessTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: "promtail",
	Name:      "config_reload_success_total",
	Help:      "Number of reload success times.",
})

var reloadFailTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: "promtail",
	Name:      "config_reload_fail_total",
	Help:      "Number of reload fail times.",
})

var errConfigNotChange = errors.New("config has not changed")

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
	walWriter      *wal.Writer
	entriesFanout  api.EntryHandler
	targetManagers *targets.TargetManagers
	server         server.Server
	logger         log.Logger
	reg            prometheus.Registerer

	stopped      bool
	mtx          sync.Mutex
	configLoaded string
	newConfig    func() (*config.Config, error)
	metrics      *client.Metrics
	dryRun       bool
}

// New makes a new Promtail.
func New(cfg config.Config, newConfig func() (*config.Config, error), metrics *client.Metrics, dryRun bool, opts ...Option) (*Promtail, error) {
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
	err := promtail.reg.Register(reloadSuccessTotal)
	if err != nil {
		return nil, fmt.Errorf("error register prometheus collector reloadSuccessTotal :%w", err)
	}
	err = promtail.reg.Register(reloadFailTotal)
	if err != nil {
		return nil, fmt.Errorf("error register prometheus collector reloadFailTotal :%w", err)
	}
	err = promtail.reloadConfig(&cfg)
	if err != nil {
		return nil, err
	}
	server, err := server.New(cfg.ServerConfig, promtail.logger, promtail.targetManagers, cfg.String())
	if err != nil {
		return nil, fmt.Errorf("error creating loki server: %w", err)
	}
	promtail.server = server
	promtail.newConfig = newConfig

	return promtail, nil
}

func (p *Promtail) reloadConfig(cfg *config.Config) error {
	level.Debug(p.logger).Log("msg", "Reloading configuration file")
	p.mtx.Lock()
	defer p.mtx.Unlock()
	newConfigFile := cfg.String()
	if newConfigFile == p.configLoaded {
		return errConfigNotChange
	}
	newConf := cfg.String()
	level.Info(p.logger).Log("msg", "Reloading configuration file", "md5sum", fmt.Sprintf("%x", md5.Sum([]byte(newConf))))
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
	// entryHandlers contains all sinks were scraped log entries should get to
	var entryHandlers = []api.EntryHandler{}

	// TODO: Refactor all client instantiation inside client.Manager
	cfg.PositionsConfig.ReadOnly = cfg.PositionsConfig.ReadOnly || p.dryRun
	if p.dryRun {
		p.client, err = client.NewLogger(p.metrics, p.logger, cfg.ClientConfigs...)
		if err != nil {
			return err
		}
		cfg.PositionsConfig.ReadOnly = true
	} else {
		var notifier client.WriterEventsNotifier = client.NilNotifier
		if cfg.WAL.Enabled {
			p.walWriter, err = wal.NewWriter(cfg.WAL, p.logger, p.reg)
			if err != nil {
				return fmt.Errorf("failed to create wal writer: %w", err)
			}

			// If WAL is enabled, the walWriter should notify the manager of new WAL writes, and it should as well
			// be an entry handler where the processing pipeline writes to
			notifier = p.walWriter
			entryHandlers = append(entryHandlers, p.walWriter)
		}
		p.client, err = client.NewManager(
			p.metrics,
			p.logger,
			cfg.LimitsConfig,
			p.reg,
			cfg.WAL,
			notifier,
			cfg.ClientConfigs...,
		)
		if err != nil {
			return fmt.Errorf("failed to create client manager: %w", err)
		}
	}

	entryHandlers = append(entryHandlers, p.client)
	p.entriesFanout = utils.NewFanoutEntryHandler(timeoutUntilFanoutHardStop, entryHandlers...)

	tms, err := targets.NewTargetManagers(p, p.reg, p.logger, cfg.PositionsConfig, p.entriesFanout, cfg.ScrapeConfig, &cfg.TargetConfig, cfg.Global.FileWatch, &cfg.LimitsConfig)
	if err != nil {
		return err
	}
	p.targetManagers = tms

	promServer := p.server
	if promServer != nil {
		promtailServer, ok := promServer.(*server.PromtailServer)
		if !ok {
			return errors.New("promtailServer cast fail")
		}
		promtailServer.ReloadServer(p.targetManagers, cfg.String())
	}
	p.configLoaded = newConf
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
	if p.entriesFanout != nil {
		p.entriesFanout.Stop()
	}
	if p.walWriter != nil {
		p.walWriter.Stop()
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
	if p.newConfig == nil {
		level.Warn(p.logger).Log("msg", "disable watchConfig", "reason", "Promtail newConfig func is Empty")
		return
	}
	switch srv := p.server.(type) {
	case *server.NoopServer:
		level.Warn(p.logger).Log("msg", "disable watchConfig", "reason", "Promtail server is disabled")
		return
	case *server.PromtailServer:
		level.Warn(p.logger).Log("msg", "enable watchConfig")
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		for {
			select {
			case <-hup:
				_ = p.reload()
			case rc := <-srv.Reload():
				if err := p.reload(); err != nil {
					rc <- err
				} else {
					rc <- nil
				}
			}
		}
	default:
		level.Warn(p.logger).Log("msg", "disable watchConfig", "reason", "Unknown Promtail server type")
		return
	}
}

func (p *Promtail) reload() error {
	cfg, err := p.newConfig()
	if err != nil {
		reloadFailTotal.Inc()
		return fmt.Errorf("Error new Config: %w", err)
	}
	err = p.reloadConfig(cfg)
	if err != nil {
		reloadFailTotal.Inc()
		level.Error(p.logger).Log("msg", "Error reloading config", "err", err)
		return err
	}
	reloadSuccessTotal.Inc()
	return nil
}
