package runtimeconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gopkg.in/yaml.v3"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
)

// Loader loads the configuration from files.
type Loader func(r io.Reader) (interface{}, error)

// Config holds the config for an Manager instance.
// It holds config related to loading per-tenant config.
type Config struct {
	ReloadPeriod time.Duration `yaml:"period" category:"advanced"`
	// LoadPath contains the path to the runtime config files.
	// Requires a non-empty value
	LoadPath flagext.StringSliceCSV `yaml:"file"`
	Loader   Loader                 `yaml:"-"`
}

// RegisterFlags registers flags.
func (mc *Config) RegisterFlags(f *flag.FlagSet) {
	f.Var(&mc.LoadPath, "runtime-config.file", "Comma separated list of yaml files with the configuration that can be updated at runtime. Runtime config files will be merged from left to right.")
	f.DurationVar(&mc.ReloadPeriod, "runtime-config.reload-period", 10*time.Second, "How often to check runtime config files.")
}

// Manager periodically reloads the configuration from specified files, and keeps this
// configuration available for clients.
type Manager struct {
	services.Service

	cfg    Config
	logger log.Logger

	listenersMtx sync.Mutex
	listeners    []chan interface{}

	configMtx sync.RWMutex
	config    interface{}

	configLoadSuccess prometheus.Gauge
	configHash        *prometheus.GaugeVec

	// Maps path to hash. Only used by loadConfig in Starting and Running states, so it doesn't need synchronization.
	fileHashes map[string]string
}

// New creates an instance of Manager. Manager is a services.Service, and must be explicitly started to perform any work.
func New(cfg Config, registerer prometheus.Registerer, logger log.Logger) (*Manager, error) {
	if len(cfg.LoadPath) == 0 {
		return nil, errors.New("LoadPath is empty")
	}

	mgr := Manager{
		cfg: cfg,
		configLoadSuccess: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "runtime_config_last_reload_successful",
			Help: "Whether the last runtime-config reload attempt was successful.",
		}),
		configHash: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Name: "runtime_config_hash",
			Help: "Hash of the currently active runtime configuration, merged from all configured files.",
		}, []string{"sha256"}),
		logger: logger,
	}

	mgr.Service = services.NewBasicService(mgr.starting, mgr.loop, mgr.stopping)
	return &mgr, nil
}

func (om *Manager) starting(_ context.Context) error {
	if len(om.cfg.LoadPath) == 0 {
		return nil
	}

	return errors.Wrap(om.loadConfig(), "failed to load runtime config")
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

func (om *Manager) loop(ctx context.Context) error {
	if len(om.cfg.LoadPath) == 0 {
		level.Info(om.logger).Log("msg", "runtime config disabled: file not specified")
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(om.cfg.ReloadPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := om.loadConfig()
			if err != nil {
				// Log but don't stop on error - we don't want to halt all ingesters because of a typo
				level.Error(om.logger).Log("msg", "failed to load config", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// loadConfig loads all configuration files using the loader function then merges the yaml configuration files into one yaml document.
// and notifies listeners if successful.
func (om *Manager) loadConfig() error {
	rawData := map[string][]byte{}
	hashes := map[string]string{}

	for _, f := range om.cfg.LoadPath {
		buf, err := os.ReadFile(f)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "read file %q", f)
		}

		rawData[f] = buf
		hashes[f] = fmt.Sprintf("%x", sha256.Sum256(buf))
	}

	// check if new hashes are the same as before
	sameHashes := true
	for f, h := range hashes {
		if om.fileHashes[f] != h {
			sameHashes = false
			break
		}
	}

	if sameHashes {
		// No need to rebuild runtime config.
		om.configLoadSuccess.Set(1)
		return nil
	}

	mergedConfig := map[string]interface{}{}
	for _, f := range om.cfg.LoadPath {
		yamlFile := map[string]interface{}{}
		err := yaml.Unmarshal(rawData[f], &yamlFile)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "unmarshal file %q", f)
		}
		mergedConfig = mergeConfigMaps(mergedConfig, yamlFile)
	}

	buf, err := yaml.Marshal(mergedConfig)
	if err != nil {
		om.configLoadSuccess.Set(0)
		return errors.Wrap(err, "marshal file")
	}

	hash := sha256.Sum256(buf)
	cfg, err := om.cfg.Loader(bytes.NewReader(buf))
	if err != nil {
		om.configLoadSuccess.Set(0)
		return errors.Wrap(err, "load file")
	}
	om.configLoadSuccess.Set(1)

	om.setConfig(cfg)
	om.callListeners(cfg)

	// expose hash of runtime config
	om.configHash.Reset()
	om.configHash.WithLabelValues(fmt.Sprintf("%x", hash)).Set(1)

	// preserve hashes for next loop
	om.fileHashes = hashes
	return nil
}

func mergeConfigMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeConfigMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
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
func (om *Manager) stopping(_ error) error {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for _, ch := range om.listeners {
		close(ch)
	}
	om.listeners = nil
	return nil
}

// GetConfig returns last loaded config value, possibly nil.
func (om *Manager) GetConfig() interface{} {
	om.configMtx.RLock()
	defer om.configMtx.RUnlock()

	return om.config
}
