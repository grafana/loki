// Package prom implements a Prometheus-lite client for service discovery,
// scraping metrics into a WAL, and remote_write. Clients are broken into a
// set of instances, each of which contain their own set of configs.
package prom

import (
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/ruler/prom/instance"
	"github.com/grafana/loki/pkg/ruler/prom/wal"
	"github.com/grafana/loki/pkg/ruler/util"
)

// DefaultConfig is the default settings for the Prometheus-lite client.
var DefaultConfig = Config{
	Global:                 instance.DefaultGlobalConfig,
	InstanceRestartBackoff: instance.DefaultBasicManagerConfig.InstanceRestartBackoff,
	WALCleanupAge:          DefaultCleanupAge,
	WALCleanupPeriod:       DefaultCleanupPeriod,
	InstanceMode:           instance.DefaultMode,
}

// Config defines the configuration for the entire set of Prometheus client
// instances, along with a global configuration.
type Config struct {
	Global                 instance.GlobalConfig `yaml:"global,omitempty"`
	WALDir                 string                `yaml:"wal_directory,omitempty"`
	WALCleanupAge          time.Duration         `yaml:"wal_cleanup_age,omitempty"`
	WALCleanupPeriod       time.Duration         `yaml:"wal_cleanup_period,omitempty"`
	Configs                []instance.Config     `yaml:"configs,omitempty,omitempty"`
	InstanceRestartBackoff time.Duration         `yaml:"instance_restart_backoff,omitempty"`
	InstanceMode           instance.Mode         `yaml:"instance_mode,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	type plain Config
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}

	return nil
}

// ApplyDefaults applies default values to the Config and validates it.
func (c *Config) ApplyDefaults() error {
	needWAL := len(c.Configs) > 0
	if needWAL && c.WALDir == "" {
		return errors.New("no wal_directory configured")
	}

	usedNames := map[string]struct{}{}

	for i := range c.Configs {
		name := c.Configs[i].Name
		if err := c.Configs[i].ApplyDefaults(c.Global); err != nil {
			// Try to show a helpful name in the error
			if name == "" {
				name = fmt.Sprintf("at index %d", i)
			}

			return fmt.Errorf("error validating instance %s: %w", name, err)
		}

		if _, ok := usedNames[name]; ok {
			return fmt.Errorf(
				"prometheus instance names must be unique. found multiple instances with name %s",
				name,
			)
		}
		usedNames[name] = struct{}{}
	}

	return nil
}

// RegisterFlags defines flags corresponding to the Config.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.WALDir, "prometheus.wal-directory", "", "base directory to store the WAL in")
	f.DurationVar(&c.WALCleanupAge, "prometheus.wal-cleanup-age", DefaultConfig.WALCleanupAge, "remove abandoned (unused) WALs older than this")
	f.DurationVar(&c.WALCleanupPeriod, "prometheus.wal-cleanup-period", DefaultConfig.WALCleanupPeriod, "how often to check for abandoned WALs")
	f.DurationVar(&c.InstanceRestartBackoff, "prometheus.instance-restart-backoff", DefaultConfig.InstanceRestartBackoff, "how long to wait before restarting a failed Prometheus instance")
}

// Agent is an agent for collecting Prometheus metrics. It acts as a
// Prometheus-lite; only running the service discovery, remote_write, and WAL
// components of Prometheus. It is broken down into a series of Instances, each
// of which perform metric collection.
type Agent struct {
	mut    sync.RWMutex
	cfg    Config
	logger log.Logger
	reg    prometheus.Registerer

	// Store both the basic manager and the modal manager so we can update their
	// settings indepedently. Only the ModalManager should be used for mutating
	// configs.
	bm      *instance.BasicManager
	mm      *instance.ModalManager
	cleaner *WALCleaner

	instanceFactory instanceFactory

	stopped  bool
	stopOnce sync.Once
	actor    chan func()
}

// New creates and starts a new Agent.
func New(reg prometheus.Registerer, cfg Config, logger log.Logger) (*Agent, error) {
	return newAgent(reg, cfg, logger, defaultInstanceFactory)
}

func newAgent(reg prometheus.Registerer, cfg Config, logger log.Logger, fact instanceFactory) (*Agent, error) {
	a := &Agent{
		logger:          log.With(logger, "agent", "prometheus"),
		instanceFactory: fact,
		reg:             reg,
		actor:           make(chan func(), 1),
	}

	a.bm = instance.NewBasicManager(instance.BasicManagerConfig{
		InstanceRestartBackoff: cfg.InstanceRestartBackoff,
	}, a.logger, a.newInstance)

	var err error
	a.mm, err = instance.NewModalManager(a.reg, a.logger, a.bm, cfg.InstanceMode)
	if err != nil {
		return nil, fmt.Errorf("failed to create modal instance manager: %w", err)
	}

	if err := a.ApplyConfig(cfg); err != nil {
		return nil, err
	}
	go a.run()
	return a, nil
}

// newInstance creates a new Instance given a config.
func (a *Agent) newInstance(c instance.Config) (instance.ManagedInstance, error) {
	a.mut.RLock()
	defer a.mut.RUnlock()

	reg := prometheus.WrapRegistererWith(prometheus.Labels{
		"tenant": c.Tenant,
	}, a.reg)

	// create metrics here and pass down
	metrics := wal.NewMetrics(reg)

	return a.instanceFactory(reg, c, metrics, a.cfg.WALDir, a.logger)
}

// Validate will validate the incoming Config and mutate it to apply defaults.
func (a *Agent) Validate(c *instance.Config) error {
	a.mut.RLock()
	defer a.mut.RUnlock()

	if err := c.ApplyDefaults(a.cfg.Global); err != nil {
		return fmt.Errorf("failed to apply defaults to %q: %w", c.Name, err)
	}
	return nil
}

// ApplyConfig applies config changes to the Agent.
func (a *Agent) ApplyConfig(cfg Config) error {
	a.mut.Lock()

	if util.CompareYAML(a.cfg, cfg) {
		a.mut.Unlock()
		return nil
	}

	if a.stopped {
		return fmt.Errorf("agent stopped")
	}

	// The ordering here is done to minimze the number of instances that need to
	// be restarted. We update components from lowest to highest level:
	//
	// 1. WAL Cleaner
	// 2. Basic manager
	// 3. Modal Manager
	// 4. Cluster
	// 5. Local configs

	if a.cleaner != nil {
		a.cleaner.Stop()
	}
	a.cleaner = NewWALCleaner(
		a.logger,
		a.mm,
		cfg.WALDir,
		cfg.WALCleanupAge,
		cfg.WALCleanupPeriod,
	)

	// BasicManager and ModalManager share the same agent mutex... unlock first.
	a.mut.Unlock()

	a.bm.UpdateManagerConfig(instance.BasicManagerConfig{
		InstanceRestartBackoff: cfg.InstanceRestartBackoff,
	})

	if err := a.mm.SetMode(cfg.InstanceMode); err != nil {
		return err
	}

	// Queue an actor in the background to sync the instances. This is required
	// because creating both this function and newInstance grab the mutex.
	oldConfig := a.cfg

	a.actor <- func() {
		a.syncInstances(oldConfig, cfg)
	}

	a.cfg = cfg
	return nil
}

// syncInstances syncs the state of the instance manager to newConfig by
// applying all configs from newConfig and deleting any configs from oldConfig
// that are not in newConfig.
func (a *Agent) syncInstances(oldConfig, newConfig Config) {
	// Apply the new configs
	for _, c := range newConfig.Configs {
		if err := a.mm.ApplyConfig(c); err != nil {
			level.Error(a.logger).Log("msg", "failed to apply config", "name", c.Name, "err", err)
		}
	}

	// Remove any configs from oldConfig that aren't in newConfig.
	for _, oc := range oldConfig.Configs {
		foundConfig := false
		for _, nc := range newConfig.Configs {
			if nc.Name == oc.Name {
				foundConfig = true
				break
			}
		}
		if foundConfig {
			continue
		}

		if err := a.mm.DeleteConfig(oc.Name); err != nil {
			level.Error(a.logger).Log("msg", "failed to delete old config", "name", oc.Name, "err", err)
		}
	}
}

// run calls received actor functions in the background.
func (a *Agent) run() {
	for f := range a.actor {
		f()
	}
}

// WireGRPC wires gRPC services into the provided server.
func (a *Agent) WireGRPC(_ *grpc.Server) {
}

// Config returns the configuration of this Agent.
func (a *Agent) Config() Config { return a.cfg }

// InstanceManager returns the instance manager used by this Agent.
func (a *Agent) InstanceManager() instance.Manager { return a.mm }

// Stop stops the agent and all its instances.
func (a *Agent) Stop() {
	a.mut.Lock()
	defer a.mut.Unlock()

	// Close the actor channel to stop run.
	a.stopOnce.Do(func() {
		close(a.actor)
	})

	a.cleaner.Stop()

	// Only need to stop the ModalManager, which will passthrough everything to the
	// BasicManager.
	a.mm.Stop()

	a.stopped = true
}

type instanceFactory = func(reg prometheus.Registerer, cfg instance.Config, metrics *wal.Metrics, walDir string, logger log.Logger) (instance.ManagedInstance, error)

func defaultInstanceFactory(reg prometheus.Registerer, cfg instance.Config, metrics *wal.Metrics, walDir string, logger log.Logger) (instance.ManagedInstance, error) {
	return instance.New(reg, cfg, metrics, walDir, logger)
}
