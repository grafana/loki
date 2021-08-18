// Package instance provides a mini Prometheus scraper and remote_writer.
package instance

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/ruler/prom/wal"
	"github.com/grafana/loki/pkg/ruler/util"
	"github.com/grafana/loki/pkg/util/build"
)

func init() {
	remote.UserAgent = fmt.Sprintf("GrafanaAgent/%s", build.Version)
}

var (
	remoteWriteMetricName = "queue_highest_sent_timestamp_seconds"
	managerMtx            sync.Mutex
)

// Default configuration values
var (
	DefaultConfig = Config{
		HostFilter:           false,
		WALTruncateFrequency: 60 * time.Minute,
		MinWALTime:           5 * time.Minute,
		MaxWALTime:           4 * time.Hour,
		RemoteFlushDeadline:  1 * time.Minute,
		WriteStaleOnShutdown: false,
		global:               DefaultGlobalConfig,
	}
)

// Config is a specific agent that runs within the overall Prometheus
// agent. It has its own set of scrape_configs and remote_write rules.
type Config struct {
	Tenant                   string
	Name                     string                      `yaml:"name,omitempty"`
	HostFilter               bool                        `yaml:"host_filter,omitempty"`
	HostFilterRelabelConfigs []*relabel.Config           `yaml:"host_filter_relabel_configs,omitempty"`
	ScrapeConfigs            []*config.ScrapeConfig      `yaml:"scrape_configs,omitempty"`
	RemoteWrite              []*config.RemoteWriteConfig `yaml:"remote_write,omitempty"`

	// How frequently the WAL should be truncated.
	WALTruncateFrequency time.Duration `yaml:"wal_truncate_frequency,omitempty"`

	// Minimum and maximum time series should exist in the WAL for.
	MinWALTime time.Duration `yaml:"min_wal_time,omitempty"`
	MaxWALTime time.Duration `yaml:"max_wal_time,omitempty"`

	RemoteFlushDeadline  time.Duration `yaml:"remote_flush_deadline,omitempty"`
	WriteStaleOnShutdown bool          `yaml:"write_stale_on_shutdown,omitempty"`

	global GlobalConfig `yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	type plain Config
	return unmarshal((*plain)(c))
}

// MarshalYAML implements yaml.Marshaler.
func (c Config) MarshalYAML() (interface{}, error) {
	// We want users to be able to marshal instance.Configs directly without
	// *needing* to call instance.MarshalConfig, so we call it internally
	// here and return a map.
	bb, err := MarshalConfig(&c, false)
	if err != nil {
		return nil, err
	}

	// Use a yaml.MapSlice rather than a map[string]interface{} so
	// order of keys is retained compared to just calling MarshalConfig.
	var m yaml.MapSlice
	if err := yaml.Unmarshal(bb, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ApplyDefaults applies default configurations to the configuration to all
// values that have not been changed to their non-zero value. ApplyDefaults
// also validates the config.
//
// The value for global will saved.
func (c *Config) ApplyDefaults(global GlobalConfig) error {
	c.global = global

	switch {
	case c.Name == "":
		return errors.New("missing instance name")
	case c.WALTruncateFrequency <= 0:
		return errors.New("wal_truncate_frequency must be greater than 0s")
	case c.RemoteFlushDeadline <= 0:
		return errors.New("remote_flush_deadline must be greater than 0s")
	case c.MinWALTime > c.MaxWALTime:
		return errors.New("min_wal_time must be less than max_wal_time")
	}

	jobNames := map[string]struct{}{}
	for _, sc := range c.ScrapeConfigs {
		if sc == nil {
			return fmt.Errorf("empty or null scrape config section")
		}

		// First set the correct scrape interval, then check that the timeout
		// (inferred or explicit) is not greater than that.
		if sc.ScrapeInterval == 0 {
			sc.ScrapeInterval = c.global.Prometheus.ScrapeInterval
		}
		if sc.ScrapeTimeout > sc.ScrapeInterval {
			return fmt.Errorf("scrape timeout greater than scrape interval for scrape config with job name %q", sc.JobName)
		}
		if time.Duration(sc.ScrapeInterval) > c.WALTruncateFrequency {
			return fmt.Errorf("scrape interval greater than wal_truncate_frequency for scrape config with job name %q", sc.JobName)
		}
		if sc.ScrapeTimeout == 0 {
			if c.global.Prometheus.ScrapeTimeout > sc.ScrapeInterval {
				sc.ScrapeTimeout = sc.ScrapeInterval
			} else {
				sc.ScrapeTimeout = c.global.Prometheus.ScrapeTimeout
			}
		}

		if _, exists := jobNames[sc.JobName]; exists {
			return fmt.Errorf("found multiple scrape configs with job name %q", sc.JobName)
		}
		jobNames[sc.JobName] = struct{}{}
	}

	// If the instance remote write is not filled in, then apply the prometheus write config
	if len(c.RemoteWrite) == 0 {
		c.RemoteWrite = c.global.RemoteWrite
	}
	for _, cfg := range c.RemoteWrite {
		if cfg == nil {
			return fmt.Errorf("empty or null remote write config section")
		}
	}
	return nil
}

// Clone makes a deep copy of the config along with global settings.
func (c *Config) Clone() (Config, error) {
	bb, err := MarshalConfig(c, false)
	if err != nil {
		return Config{}, err
	}
	cp, err := UnmarshalConfig(bytes.NewReader(bb))
	if err != nil {
		return Config{}, err
	}
	cp.global = c.global

	// Some tests will trip up on this; the marshal/unmarshal cycle might set
	// an empty slice to nil. Set it back to an empty slice if we detect this
	// happening.
	if cp.ScrapeConfigs == nil && c.ScrapeConfigs != nil {
		cp.ScrapeConfigs = []*config.ScrapeConfig{}
	}
	if cp.RemoteWrite == nil && c.RemoteWrite != nil {
		cp.RemoteWrite = []*config.RemoteWriteConfig{}
	}

	return *cp, nil
}

type walStorageFactory func(reg prometheus.Registerer) (walStorage, error)

// Instance is an individual metrics collector and remote_writer.
type Instance struct {
	// All fields in the following block may be accessed and modified by
	// concurrently running goroutines.
	//
	// Note that all Prometheus components listed here may be nil at any
	// given time; methods reading them should take care to do nil checks.
	mut                sync.Mutex
	cfg                Config
	wal                walStorage
	discovery          *discoveryService
	readyScrapeManager *readyScrapeManager
	remoteStore        *remote.Storage
	storage            storage.Storage

	hostFilter *HostFilter

	logger log.Logger

	reg    prometheus.Registerer
	newWal walStorageFactory

	vc *MetricValueCollector
}

// New creates a new Instance with a directory for storing the WAL. The instance
// will not start until Run is called on the instance.
func New(reg prometheus.Registerer, cfg Config, metrics *wal.Metrics, walDir string, logger log.Logger) (*Instance, error) {
	logger = log.With(logger, "instance", cfg.Name)

	instWALDir := filepath.Join(walDir, cfg.Tenant)

	newWal := func(reg prometheus.Registerer) (walStorage, error) {
		return wal.NewStorage(logger, metrics, reg, instWALDir)
	}

	return newInstance(cfg, reg, logger, newWal)
}

func newInstance(cfg Config, reg prometheus.Registerer, logger log.Logger, newWal walStorageFactory) (*Instance, error) {
	vc := NewMetricValueCollector(prometheus.DefaultGatherer, remoteWriteMetricName)

	hostname, err := Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	i := &Instance{
		cfg:        cfg,
		logger:     logger,
		vc:         vc,
		hostFilter: NewHostFilter(hostname, cfg.HostFilterRelabelConfigs),

		reg:    reg,
		newWal: newWal,

		readyScrapeManager: &readyScrapeManager{},
	}

	return i, nil
}

func (i *Instance) Storage() storage.Storage {
	return i.storage
}

// Run starts the instance, initializing Prometheus components, and will
// continue to run until an error happens during execution or the provided
// context is cancelled.
//
// Run may be re-called after exiting, as components will be reinitialized each
// time Run is called.
func (i *Instance) Run(ctx context.Context) error {
	// i.cfg may change at any point in the middle of this method but not in a way
	// that affects any of the code below; rather than grabbing a mutex every time
	// we want to read the config, we'll simplify the access and just grab a copy
	// now.
	i.mut.Lock()
	cfg := i.cfg
	i.mut.Unlock()

	level.Debug(i.logger).Log("msg", "initializing instance", "name", cfg.Name)

	// trackingReg wraps the register for the instance to make sure that if Run
	// exits, any metrics Prometheus registers are removed and can be
	// re-registered if Run is called again.
	trackingReg := util.WrapWithUnregisterer(i.reg)
	defer trackingReg.UnregisterAll()

	if err := i.initialize(ctx, trackingReg, &cfg); err != nil {
		level.Error(i.logger).Log("msg", "failed to initialize instance", "err", err)
		return fmt.Errorf("failed to initialize instance: %w", err)
	}

	// The actors defined here are defined in the order we want them to shut down.
	// Primarily, we want to ensure that the following shutdown order is
	// maintained:
	//		1. The scrape manager stops
	//    2. WAL storage is closed
	//    3. Remote write storage is closed
	// This is done to allow the instance to write stale markers for all active
	// series.
	rg := runGroupWithContext(ctx)

	{
		// Target Discovery
		rg.Add(i.discovery.Run, i.discovery.Stop)
	}
	{
		// Truncation loop
		ctx, contextCancel := context.WithCancel(context.Background())
		defer contextCancel()
		rg.Add(
			func() error {
				i.truncateLoop(ctx, i.wal, &cfg)
				level.Info(i.logger).Log("msg", "truncation loop stopped")
				return nil
			},
			func(err error) {
				level.Info(i.logger).Log("msg", "stopping truncation loop...")
				contextCancel()
			},
		)
	}
	{
		sm, err := i.readyScrapeManager.Get()
		if err != nil {
			level.Error(i.logger).Log("msg", "failed to get scrape manager")
			return err
		}

		// Scrape manager
		rg.Add(
			func() error {
				err := sm.Run(i.discovery.SyncCh())
				level.Info(i.logger).Log("msg", "scrape manager stopped")
				return err
			},
			func(err error) {
				// The scrape manager is closed first to allow us to write staleness
				// markers without receiving new samples from scraping in the meantime.
				level.Info(i.logger).Log("msg", "stopping scrape manager...")
				sm.Stop()

				// On a graceful shutdown, write staleness markers. If something went
				// wrong, then the instance will be relaunched.
				if err == nil && cfg.WriteStaleOnShutdown {
					level.Info(i.logger).Log("msg", "writing staleness markers...")
					err := i.wal.WriteStalenessMarkers(i.getRemoteWriteTimestamp)
					if err != nil {
						level.Error(i.logger).Log("msg", "error writing staleness markers", "err", err)
					}
				}

				// Closing the storage closes both the WAL storage and remote wrte
				// storage.
				level.Info(i.logger).Log("msg", "closing storage...")
				if err := i.storage.Close(); err != nil {
					level.Error(i.logger).Log("msg", "error stopping storage", "err", err)
				}
			},
		)
	}

	level.Debug(i.logger).Log("msg", "running instance", "name", cfg.Name)
	err := rg.Run()
	if err != nil {
		level.Error(i.logger).Log("msg", "agent instance stopped with error", "err", err)
	}
	return err
}

// initialize sets up the various Prometheus components with their initial
// settings. initialize will be called each time the Instance is run. Prometheus
// components cannot be reused after they are stopped so we need to recreate them
// each run.
func (i *Instance) initialize(ctx context.Context, reg prometheus.Registerer, cfg *Config) error {
	i.mut.Lock()
	defer i.mut.Unlock()

	if cfg.HostFilter {
		i.hostFilter.PatchSD(cfg.ScrapeConfigs)
	}

	var err error

	i.wal, err = i.newWal(reg)
	if err != nil {
		return fmt.Errorf("error creating WAL: %w", err)
	}

	i.discovery, err = i.newDiscoveryManager(ctx, cfg)
	if err != nil {
		return fmt.Errorf("error creating discovery manager: %w", err)
	}

	i.readyScrapeManager = &readyScrapeManager{}

	// Setup the remote storage
	remoteLogger := log.With(i.logger, "component", "remote")
	i.remoteStore = remote.NewStorage(remoteLogger, reg, i.wal.StartTime, i.wal.Directory(), cfg.RemoteFlushDeadline, i.readyScrapeManager)
	err = i.remoteStore.ApplyConfig(&config.Config{
		GlobalConfig:       cfg.global.Prometheus,
		RemoteWriteConfigs: cfg.RemoteWrite,
	})
	if err != nil {
		return fmt.Errorf("failed applying config to remote storage: %w", err)
	}

	i.storage = storage.NewFanout(i.logger, i.wal, i.remoteStore)

	scrapeManager := newScrapeManager(log.With(i.logger, "component", "scrape manager"), i.storage)
	err = scrapeManager.ApplyConfig(&config.Config{
		GlobalConfig:  cfg.global.Prometheus,
		ScrapeConfigs: cfg.ScrapeConfigs,
	})
	if err != nil {
		return fmt.Errorf("failed applying config to scrape manager: %w", err)
	}

	i.readyScrapeManager.Set(scrapeManager)

	return nil
}

// Update accepts a new Config for the Instance and will dynamically update any
// running Prometheus components with the new values from Config. Update will
// return an ErrInvalidUpdate if the Update could not be applied.
func (i *Instance) Update(c Config) (err error) {
	i.mut.Lock()
	defer i.mut.Unlock()

	// It's only (currently) valid to update scrape_configs and remote_write, so
	// if any other field has changed here, return the error.
	switch {
	// This first case should never happen in practice but it's included here for
	// completions sake.
	case i.cfg.Name != c.Name:
		err = errImmutableField{Field: "name"}
	case i.cfg.HostFilter != c.HostFilter:
		err = errImmutableField{Field: "host_filter"}
	case i.cfg.WALTruncateFrequency != c.WALTruncateFrequency:
		err = errImmutableField{Field: "wal_truncate_frequency"}
	case i.cfg.RemoteFlushDeadline != c.RemoteFlushDeadline:
		err = errImmutableField{Field: "remote_flush_deadline"}
	case i.cfg.WriteStaleOnShutdown != c.WriteStaleOnShutdown:
		err = errImmutableField{Field: "write_stale_on_shutdown"}
	}
	if err != nil {
		return ErrInvalidUpdate{Inner: err}
	}

	// Check to see if the components exist yet.
	if i.discovery == nil || i.remoteStore == nil || i.readyScrapeManager == nil {
		return ErrInvalidUpdate{
			Inner: fmt.Errorf("cannot dynamically update because instance is not running"),
		}
	}

	// NOTE(rfratto): Prometheus applies configs in a specific order to ensure
	// flow from service discovery down to the WAL continues working properly.
	//
	// Keep the following order below:
	//
	// 1. Local config
	// 2. Remote Store
	// 3. Scrape Manager
	// 4. Discovery Manager

	originalConfig := i.cfg
	defer func() {
		if err != nil {
			i.cfg = originalConfig
		}
	}()
	i.cfg = c

	i.hostFilter.SetRelabels(c.HostFilterRelabelConfigs)
	if c.HostFilter {
		// N.B.: only call PatchSD if HostFilter is enabled since it
		// mutates what targets will be discovered.
		i.hostFilter.PatchSD(c.ScrapeConfigs)
	}

	err = i.remoteStore.ApplyConfig(&config.Config{
		GlobalConfig:       c.global.Prometheus,
		RemoteWriteConfigs: c.RemoteWrite,
	})
	if err != nil {
		return fmt.Errorf("error applying new remote_write configs: %w", err)
	}

	sm, err := i.readyScrapeManager.Get()
	if err != nil {
		return fmt.Errorf("couldn't get scrape manager to apply new scrape configs: %w", err)
	}
	err = sm.ApplyConfig(&config.Config{
		GlobalConfig:  c.global.Prometheus,
		ScrapeConfigs: c.ScrapeConfigs,
	})
	if err != nil {
		return fmt.Errorf("error applying updated configs to scrape manager: %w", err)
	}

	sdConfigs := map[string]discovery.Configs{}
	for _, v := range c.ScrapeConfigs {
		sdConfigs[v.JobName] = v.ServiceDiscoveryConfigs
	}
	err = i.discovery.Manager.ApplyConfig(sdConfigs)
	if err != nil {
		return fmt.Errorf("failed applying configs to discovery manager: %w", err)
	}

	return nil
}

// TargetsActive returns the set of active targets from the scrape manager. Returns nil
// if the scrape manager is not ready yet.
func (i *Instance) TargetsActive() map[string][]*scrape.Target {
	i.mut.Lock()
	defer i.mut.Unlock()

	if i.readyScrapeManager == nil {
		return nil
	}

	mgr, err := i.readyScrapeManager.Get()
	if err == ErrNotReady {
		return nil
	} else if err != nil {
		level.Error(i.logger).Log("msg", "failed to get scrape manager when collecting active targets", "err", err)
		return nil
	}
	return mgr.TargetsActive()
}

// StorageDirectory returns the directory where this Instance is writing series
// and samples to for the WAL.
func (i *Instance) StorageDirectory() string {
	return i.wal.Directory()
}

// Appender returns a storage.Appender from the instance's WAL
func (i *Instance) Appender(ctx context.Context) storage.Appender {
	return i.wal.Appender(ctx)
}

type discoveryService struct {
	Manager *discovery.Manager

	RunFunc    func() error
	StopFunc   func(err error)
	SyncChFunc func() GroupChannel
}

func (s *discoveryService) Run() error           { return s.RunFunc() }
func (s *discoveryService) Stop(err error)       { s.StopFunc(err) }
func (s *discoveryService) SyncCh() GroupChannel { return s.SyncChFunc() }

// newDiscoveryManager returns an implementation of a runnable service
// that outputs discovered targets to a channel. The implementation
// uses the Prometheus Discovery Manager. Targets will be filtered
// if the instance is configured to perform host filtering.
func (i *Instance) newDiscoveryManager(ctx context.Context, cfg *Config) (*discoveryService, error) {
	ctx, cancel := context.WithCancel(ctx)

	logger := log.With(i.logger, "component", "discovery manager")
	manager := discovery.NewManager(ctx, logger, discovery.Name("scrape"))

	// TODO(rfratto): refactor this to a function?
	// TODO(rfratto): ensure job name name is unique
	c := map[string]discovery.Configs{}
	for _, v := range cfg.ScrapeConfigs {
		c[v.JobName] = v.ServiceDiscoveryConfigs
	}
	err := manager.ApplyConfig(c)
	if err != nil {
		cancel()
		level.Error(i.logger).Log("msg", "failed applying config to discovery manager", "err", err)
		return nil, fmt.Errorf("failed applying config to discovery manager: %w", err)
	}

	rg := runGroupWithContext(ctx)

	// Run the manager
	rg.Add(func() error {
		err := manager.Run()
		level.Info(i.logger).Log("msg", "discovery manager stopped")
		return err
	}, func(err error) {
		level.Info(i.logger).Log("msg", "stopping discovery manager...")
		cancel()
	})

	syncChFunc := manager.SyncCh

	// If host filtering is enabled, run it and use its channel for discovered
	// targets.
	if cfg.HostFilter {
		rg.Add(func() error {
			i.hostFilter.Run(manager.SyncCh())
			level.Info(i.logger).Log("msg", "host filterer stopped")
			return nil
		}, func(_ error) {
			level.Info(i.logger).Log("msg", "stopping host filterer...")
			i.hostFilter.Stop()
		})

		syncChFunc = i.hostFilter.SyncCh
	}

	return &discoveryService{
		Manager: manager,

		RunFunc:    rg.Run,
		StopFunc:   rg.Stop,
		SyncChFunc: syncChFunc,
	}, nil
}

func (i *Instance) truncateLoop(ctx context.Context, wal walStorage, cfg *Config) {
	// Track the last timestamp we truncated for to prevent segments from getting
	// deleted until at least some new data has been sent.
	var lastTs int64 = math.MinInt64

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.WALTruncateFrequency):
			// The timestamp ts is used to determine which series are not receiving
			// samples and may be deleted from the WAL. Their most recent append
			// timestamp is compared to ts, and if that timestamp is older then ts,
			// they are considered inactive and may be deleted.
			//
			// Subtracting a duration from ts will delay when it will be considered
			// inactive and scheduled for deletion.
			ts := i.getRemoteWriteTimestamp() - i.cfg.MinWALTime.Milliseconds()
			if ts < 0 {
				ts = 0
			}

			// Network issues can prevent the result of getRemoteWriteTimestamp from
			// changing. We don't want data in the WAL to grow forever, so we set a cap
			// on the maximum age data can be. If our ts is older than this cutoff point,
			// we'll shift it forward to start deleting very stale data.
			if maxTS := timestamp.FromTime(time.Now().Add(-i.cfg.MaxWALTime)); ts < maxTS {
				ts = maxTS
			}

			if ts == lastTs {
				level.Debug(i.logger).Log("msg", "not truncating the WAL, remote_write timestamp is unchanged", "ts", ts)
				continue
			}
			lastTs = ts

			level.Debug(i.logger).Log("msg", "truncating the WAL", "ts", ts)
			err := wal.Truncate(ts)
			if err != nil {
				// The only issue here is larger disk usage and a greater replay time,
				// so we'll only log this as a warning.
				level.Warn(i.logger).Log("msg", "could not truncate WAL", "err", err)
			}
		}
	}
}

// getRemoteWriteTimestamp looks up the last successful remote write timestamp.
// This is passed to wal.Storage for its truncation. If no remote write sections
// are configured, getRemoteWriteTimestamp returns the current time.
func (i *Instance) getRemoteWriteTimestamp() int64 {
	i.mut.Lock()
	defer i.mut.Unlock()

	if len(i.cfg.RemoteWrite) == 0 {
		return timestamp.FromTime(time.Now())
	}

	lbls := make([]string, len(i.cfg.RemoteWrite))
	for idx := 0; idx < len(lbls); idx++ {
		lbls[idx] = i.cfg.RemoteWrite[idx].Name
	}

	vals, err := i.vc.GetValues("remote_name", lbls...)
	if err != nil {
		level.Error(i.logger).Log("msg", "could not get remote write timestamps", "err", err)
		return 0
	}
	if len(vals) == 0 {
		return 0
	}

	// We use the lowest value from the metric since we don't want to delete any
	// segments from the WAL until they've been written by all of the remote_write
	// configurations.
	ts := int64(math.MaxInt64)
	for _, val := range vals {
		ival := int64(val)
		if ival < ts {
			ts = ival
		}
	}

	// Convert to the millisecond precision which is used by the WAL
	return ts * 1000
}

// walStorage is an interface satisfied by wal.Storage, and created for testing.
type walStorage interface {
	// walStorage implements Queryable/ChunkQueryable for compatibility, but is unused.
	storage.Queryable
	storage.ChunkQueryable

	Directory() string

	StartTime() (int64, error)
	WriteStalenessMarkers(remoteTsFunc func() int64) error
	Appender(context.Context) storage.Appender
	Truncate(mint int64) error

	Close() error
}

// Hostname retrieves the hostname identifying the machine the process is
// running on. It will return the value of $HOSTNAME, if defined, and fall
// back to Go's os.Hostname.
func Hostname() (string, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}
	return hostname, nil
}

func getHash(data interface{}) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	hash := md5.Sum(bytes)
	return hex.EncodeToString(hash[:]), nil
}

// MetricValueCollector wraps around a Gatherer and provides utilities for
// pulling metric values from a given metric name and label matchers.
//
// This is used by the agent instances to find the most recent timestamp
// successfully remote_written to for purposes of safely truncating the WAL.
//
// MetricValueCollector is only intended for use with Gauges and Counters.
type MetricValueCollector struct {
	g     prometheus.Gatherer
	match string
}

// NewMetricValueCollector creates a new MetricValueCollector.
func NewMetricValueCollector(g prometheus.Gatherer, match string) *MetricValueCollector {
	return &MetricValueCollector{
		g:     g,
		match: match,
	}
}

// GetValues looks through all the tracked metrics and returns all values
// for metrics that match some key value pair.
func (vc *MetricValueCollector) GetValues(label string, labelValues ...string) ([]float64, error) {
	vals := []float64{}

	families, err := vc.g.Gather()
	if err != nil {
		return nil, err
	}

	for _, family := range families {
		if !strings.Contains(family.GetName(), vc.match) {
			continue
		}

		for _, m := range family.GetMetric() {
			matches := false
			for _, l := range m.GetLabel() {
				if l.GetName() != label {
					continue
				}

				v := l.GetValue()
				for _, match := range labelValues {
					if match == v {
						matches = true
						break
					}
				}
				break
			}
			if !matches {
				continue
			}

			var value float64
			if m.Gauge != nil {
				value = m.Gauge.GetValue()
			} else if m.Counter != nil {
				value = m.Counter.GetValue()
			} else if m.Untyped != nil {
				value = m.Untyped.GetValue()
			} else {
				return nil, errors.New("tracking unexpected metric type")
			}

			vals = append(vals, value)
		}
	}

	return vals, nil
}

func newScrapeManager(logger log.Logger, app storage.Appendable) *scrape.Manager {
	// scrape.NewManager modifies a global variable in Prometheus. To avoid a
	// data race of modifying that global, we lock a mutex here briefly.
	managerMtx.Lock()
	defer managerMtx.Unlock()
	return scrape.NewManager(logger, app)
}

type runGroupContext struct {
	cancel context.CancelFunc

	g *run.Group
}

// runGroupWithContext creates a new run.Group that will be stopped if the
// context gets canceled in addition to the normal behavior of stopping
// when any of the actors stop.
func runGroupWithContext(ctx context.Context) *runGroupContext {
	ctx, cancel := context.WithCancel(ctx)

	var g run.Group
	g.Add(func() error {
		<-ctx.Done()
		return nil
	}, func(_ error) {
		cancel()
	})

	return &runGroupContext{cancel: cancel, g: &g}
}

func (rg *runGroupContext) Add(execute func() error, interrupt func(error)) {
	rg.g.Add(execute, interrupt)
}

func (rg *runGroupContext) Run() error   { return rg.g.Run() }
func (rg *runGroupContext) Stop(_ error) { rg.cancel() }

// ErrNotReady is returned when the scrape manager is used but has not been
// initialized yet.
var ErrNotReady = errors.New("Scrape manager not ready")

// readyScrapeManager allows a scrape manager to be retrieved. Even if it's set at a later point in time.
type readyScrapeManager struct {
	mtx sync.RWMutex
	m   *scrape.Manager
}

// Set the scrape manager.
func (rm *readyScrapeManager) Set(m *scrape.Manager) {
	rm.mtx.Lock()
	defer rm.mtx.Unlock()

	rm.m = m
}

// Get the scrape manager. If is not ready, return an error.
func (rm *readyScrapeManager) Get() (*scrape.Manager, error) {
	rm.mtx.RLock()
	defer rm.mtx.RUnlock()

	if rm.m != nil {
		return rm.m, nil
	}

	return nil, ErrNotReady
}
