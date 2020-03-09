package runtimeconfig

import (
	"flag"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// Loader loads the configuration from file.
type Loader func(filename string) (interface{}, error)

// ManagerConfig holds the config for an Manager instance.
// It holds config related to loading per-tenant config.
type ManagerConfig struct {
	ReloadPeriod time.Duration `yaml:"period"`
	LoadPath     string        `yaml:"file"`
	Loader       Loader        `yaml:"-"`
}

// RegisterFlags registers flags.
func (mc *ManagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&mc.LoadPath, "runtime-config.file", "", "File with the configuration that can be updated in runtime.")
	f.DurationVar(&mc.ReloadPeriod, "runtime-config.reload-period", 10*time.Second, "How often to check runtime config file.")
}

// Manager periodically reloads the configuration from a file, and keeps this
// configuration available for clients.
type Manager struct {
	cfg  ManagerConfig
	quit chan struct{}

	listenersMtx sync.Mutex
	listeners    []chan interface{}

	configMtx sync.RWMutex
	config    interface{}

	configLoadSuccess prometheus.Gauge
}

// NewRuntimeConfigManager creates an instance of Manager and starts reload config loop based on config
func NewRuntimeConfigManager(cfg ManagerConfig, registerer prometheus.Registerer) (*Manager, error) {
	mgr := Manager{
		cfg:  cfg,
		quit: make(chan struct{}),
		configLoadSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cortex_overrides_last_reload_successful",
			Help: "Whether the last config reload attempt was successful.",
		}),
	}

	if registerer != nil {
		registerer.MustRegister(mgr.configLoadSuccess)
	}

	if cfg.LoadPath != "" {
		if err := mgr.loadConfig(); err != nil {
			// Log but don't stop on error - we don't want to halt all ingesters because of a typo
			level.Error(util.Logger).Log("msg", "failed to load config", "err", err)
		}
		go mgr.loop()
	} else {
		level.Info(util.Logger).Log("msg", "runtime config disabled: file not specified")
	}

	return &mgr, nil
}

// CreateListenerChannel creates new channel that can be used to receive new config values.
// If there is no receiver waiting for value when config manager tries to send the update,
// or channel buffer is full, update is discarded.
//
// When config manager is stopped, it closes all channels to notify receivers that they will
// not receive any more updates.
func (om *Manager) CreateListenerChannel(buffer int) <-chan interface{} {
	ch := make(chan interface{}, buffer)

	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	om.listeners = append(om.listeners, ch)
	return ch
}

// CloseListenerChannel removes given channel from list of channels to send notifications to and closes channel.
func (om *Manager) CloseListenerChannel(listener <-chan interface{}) {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for ix, ch := range om.listeners {
		if ch == listener {
			om.listeners = append(om.listeners[:ix], om.listeners[ix+1:]...)
			close(ch)
			break
		}
	}
}

func (om *Manager) loop() {
	ticker := time.NewTicker(om.cfg.ReloadPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := om.loadConfig()
			if err != nil {
				// Log but don't stop on error - we don't want to halt all ingesters because of a typo
				level.Error(util.Logger).Log("msg", "failed to load config", "err", err)
			}
		case <-om.quit:
			return
		}
	}
}

// loadConfig loads configuration using the loader function, and if successful,
// stores it as current configuration and notifies listeners.
func (om *Manager) loadConfig() error {
	cfg, err := om.cfg.Loader(om.cfg.LoadPath)
	if err != nil {
		om.configLoadSuccess.Set(0)
		return err
	}
	om.configLoadSuccess.Set(1)

	om.setConfig(cfg)
	om.callListeners(cfg)

	return nil
}

func (om *Manager) setConfig(config interface{}) {
	om.configMtx.Lock()
	defer om.configMtx.Unlock()
	om.config = config
}

func (om *Manager) callListeners(newValue interface{}) {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for _, ch := range om.listeners {
		select {
		case ch <- newValue:
			// ok
		default:
			// nobody is listening or buffer full.
		}
	}
}

// Stop stops the Manager
func (om *Manager) Stop() {
	close(om.quit)

	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for _, ch := range om.listeners {
		close(ch)
	}
	om.listeners = nil
}

// GetConfig returns last loaded config value, possibly nil.
func (om *Manager) GetConfig() interface{} {
	om.configMtx.RLock()
	defer om.configMtx.RUnlock()

	return om.config
}
