package promtail

import (
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/server"
	"github.com/grafana/loki/pkg/promtail/targets"
)

// Promtail is the root struct for Promtail...
type Promtail struct {
	Client         client.Client
	Positions      *positions.Positions
	TargetManagers *targets.TargetManagers
	Server         *server.Server

	configFile string
	Cfg        *config.Config
}

type Master struct {
	Promtail   *Promtail
	defaultCfg *config.Config
	Cancel chan struct{}
}

func InitMaster(configFile string, cfg config.Config) (*Master, error) {
	p, err := New(configFile, cfg)
	if err != nil {
		//level.Error(util.Logger).Log("msg", "error creating promtail", "error", err)
		return nil, err
	}

	cancel := make(chan struct{})

	m := &Master{
		Promtail:   p,
		defaultCfg: &cfg,
		Cancel:cancel,
	}

	p.Server.HTTP.Path("/reload").Handler(http.HandlerFunc(m.Reload))

	return m, nil
}

func (m *Master) Reload(rw http.ResponseWriter, _ *http.Request) {
	go m.DoReload()
	rw.WriteHeader(http.StatusNoContent)
}

func (m *Master) DoReload() {
	errChan := make(chan error, 1)

	level.Info(util.Logger).Log("msg", "=== received RELOAD ===\n*** reloading")
	// trigger server shutdown
	m.Promtail.Server.Stop()

	// wait old promtail shutdown
	<- m.Cancel

	p, err := New(m.Promtail.configFile, *m.defaultCfg)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error reloading new promtail", "error", err)
		errChan <- err
	}
	// Re-init the logger which will now honor a different log level set in ServerConfig.Config
	util.InitLogger(&m.Promtail.Cfg.ServerConfig.Config)

	p.Server.HTTP.Path("/reload").Handler(http.HandlerFunc(m.Reload))

	m.Promtail = p

	err = m.Promtail.Run()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error starting new promtail", "error", err)
		errChan <- err
	}

	m.WaitSignals(errChan)
}

func (m *Master) WaitSignals(errChan chan error) {
	// can not directly use m.promtail.Run() to handler signals since reload call Shutdown() too
	// avoid close of closed channel panic
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	buf := make([]byte, 1<<20)
	select {
	case <-errChan:
		os.Exit(1)
	case sig := <-sigs:
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			level.Info(util.Logger).Log("msg", "=== received SIGINT/SIGTERM ===\n*** exiting")
			m.Promtail.Shutdown()
			return
		case syscall.SIGQUIT:
			stacklen := runtime.Stack(buf, true)
			level.Info(util.Logger).Log("msg", "=== received SIGQUIT ===\n*** goroutine dump...\n%s\n*** end", buf[:stacklen])
		}
	}
}

// New promtail from config file
func New(configFile string, cfg config.Config) (*Promtail, error) {
	if err := helpers.LoadConfig(configFile, &cfg); err != nil {
		return nil, err
	}

	positions, err := positions.New(util.Logger, cfg.PositionsConfig)
	if err != nil {
		return nil, err
	}

	if cfg.ClientConfig.URL.URL != nil {
		// if a single client config is used we add it to the multiple client config for backward compatibility
		cfg.ClientConfigs = append(cfg.ClientConfigs, cfg.ClientConfig)
	}

	client, err := client.NewMulti(util.Logger, cfg.ClientConfigs...)
	if err != nil {
		return nil, err
	}

	tms, err := targets.NewTargetManagers(util.Logger, positions, client, cfg.ScrapeConfig, &cfg.TargetConfig)
	if err != nil {
		return nil, err
	}

	server, err := server.New(cfg.ServerConfig, tms)
	if err != nil {
		return nil, err
	}

	return &Promtail{
		Client:         client,
		Positions:      positions,
		TargetManagers: tms,
		Server:         server,
		configFile:     configFile,
		Cfg:            &cfg,
	}, nil
}

// Run the promtail; will block until a signal is received.
func (p *Promtail) Run() error {
	return p.Server.Run()
}

// Shutdown the promtail.
func (p *Promtail) Shutdown() {
	p.Server.Shutdown()
	p.TargetManagers.Stop()
	p.Positions.Stop()
	p.Client.Stop()
}
