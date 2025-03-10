// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/ruler/storage/util"
	"github.com/grafana/loki/v3/pkg/ruler/storage/wal"
	"github.com/grafana/loki/v3/pkg/util/build"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func init() {
	remote.UserAgent = fmt.Sprintf("LokiRulerWAL/%s", build.Version)
}

var (
	remoteWriteMetricName = "queue_highest_sent_timestamp_seconds"
)

// Default configuration values
var (
	DefaultConfig = Config{
		Dir:                 "ruler-wal",
		TruncateFrequency:   60 * time.Minute,
		MinAge:              5 * time.Minute,
		MaxAge:              4 * time.Hour,
		RemoteFlushDeadline: 1 * time.Minute,
	}
)

// Config is a specific agent that runs within the overall Prometheus
// agent. It has its own set of scrape_configs and remote_write rules.
type Config struct {
	Tenant      string                      `doc:"hidden"`
	Name        string                      `doc:"hidden"`
	RemoteWrite []*config.RemoteWriteConfig `doc:"hidden"`

	Dir string `yaml:"dir"`

	// How frequently the WAL should be truncated.
	TruncateFrequency time.Duration `yaml:"truncate_frequency,omitempty"`

	// Minimum and maximum time series should exist in the WAL for.
	MinAge time.Duration `yaml:"min_age,omitempty"`
	MaxAge time.Duration `yaml:"max_age,omitempty"`

	RemoteFlushDeadline time.Duration `yaml:"remote_flush_deadline,omitempty" doc:"hidden"`
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
func (c *Config) ApplyDefaults() error {
	switch {
	case c.Name == "":
		return errors.New("missing instance name")
	case c.TruncateFrequency <= 0:
		return errors.New("wal_truncate_frequency must be greater than 0s")
	case c.RemoteFlushDeadline <= 0:
		return errors.New("remote_flush_deadline must be greater than 0s")
	case c.MinAge > c.MaxAge:
		return errors.New("min_wal_time must be less than max_wal_time")
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

	// Some tests will trip up on this; the marshal/unmarshal cycle might set
	// an empty slice to nil. Set it back to an empty slice if we detect this
	// happening.
	if cp.RemoteWrite == nil && c.RemoteWrite != nil {
		cp.RemoteWrite = []*config.RemoteWriteConfig{}
	}

	return *cp, nil
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.Dir, "ruler.wal.dir", DefaultConfig.Dir, "The directory in which to write tenant WAL files. Each tenant will have its own directory one level below this directory.")
	f.DurationVar(&c.TruncateFrequency, "ruler.wal.truncate-frequency", DefaultConfig.TruncateFrequency, "Frequency with which to run the WAL truncation process.")
	f.DurationVar(&c.MinAge, "ruler.wal.min-age", DefaultConfig.MinAge, "Minimum age that samples must exist in the WAL before being truncated.")
	f.DurationVar(&c.MaxAge, "ruler.wal.max-age", DefaultConfig.MaxAge, "Maximum age that samples must exist in the WAL before being truncated.")
}

type walStorageFactory func(reg prometheus.Registerer) (walStorage, error)

// Instance is an individual metrics collector and remote_writer.
type Instance struct {
	initialized bool

	// All fields in the following block may be accessed and modified by
	// concurrently running goroutines.
	//
	// Note that all Prometheus components listed here may be nil at any
	// given time; methods reading them should take care to do nil checks.
	mut         sync.Mutex
	cfg         Config
	wal         walStorage
	remoteStore *remote.Storage
	storage     storage.Storage

	logger log.Logger

	reg    prometheus.Registerer
	newWal walStorageFactory

	vc     *MetricValueCollector
	tenant string
}

// New creates a new Instance with a directory for storing the WAL. The instance
// will not start until Run is called on the instance.
func New(reg prometheus.Registerer, cfg Config, metrics *wal.Metrics, logger log.Logger) (*Instance, error) {
	logger = log.With(logger, "instance", cfg.Name)

	instWALDir := filepath.Join(cfg.Dir, cfg.Tenant)

	newWal := func(reg prometheus.Registerer) (walStorage, error) {
		return wal.NewStorage(logger, metrics, reg, instWALDir)
	}

	return newInstance(cfg, reg, logger, newWal, cfg.Tenant)
}

func newInstance(cfg Config, reg prometheus.Registerer, logger log.Logger, newWal walStorageFactory, tenant string) (*Instance, error) {
	vc := NewMetricValueCollector(prometheus.DefaultGatherer, remoteWriteMetricName)

	i := &Instance{
		cfg:    cfg,
		logger: logger,
		vc:     vc,

		reg:    reg,
		newWal: newWal,

		tenant: tenant,
	}

	return i, nil
}

func (i *Instance) Storage() storage.Storage {
	i.mut.Lock()
	defer i.mut.Unlock()

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
		// Truncation loop
		ctx, contextCancel := context.WithCancel(context.Background())
		defer contextCancel()
		rg.Add(
			func() error {
				i.truncateLoop(ctx, i.wal, &cfg)
				level.Info(i.logger).Log("msg", "truncation loop stopped")
				return nil
			},
			func(_ error) {
				level.Info(i.logger).Log("msg", "stopping truncation loop...")
				contextCancel()
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

type noopScrapeManager struct{}

func (n noopScrapeManager) Get() (*scrape.Manager, error) {
	return nil, errors.New("No-op Scrape manager not ready")
}

// initialize sets up the various Prometheus components with their initial
// settings. initialize will be called each time the Instance is run. Prometheus
// components cannot be reused after they are stopped so we need to recreate them
// each run.
func (i *Instance) initialize(_ context.Context, reg prometheus.Registerer, cfg *Config) error {
	i.mut.Lock()
	defer i.mut.Unlock()

	// explicitly set this in case this function is called multiple times
	i.initialized = false

	var err error

	i.wal, err = i.newWal(reg)
	if err != nil {
		return fmt.Errorf("error creating WAL: %w", err)
	}

	// Setup the remote storage
	remoteLogger := log.With(i.logger, "component", "remote")
	i.remoteStore = remote.NewStorage(util_log.SlogFromGoKit(remoteLogger), reg, i.wal.StartTime, i.wal.Directory(), cfg.RemoteFlushDeadline, noopScrapeManager{}, false)
	err = i.remoteStore.ApplyConfig(&config.Config{
		RemoteWriteConfigs: cfg.RemoteWrite,
	})
	if err != nil {
		return fmt.Errorf("failed applying config to remote storage: %w", err)
	}

	i.storage = storage.NewFanout(util_log.SlogFromGoKit(i.logger), i.wal, i.remoteStore)
	i.wal.SetWriteNotified(i.remoteStore)
	i.initialized = true

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
	case i.cfg.TruncateFrequency != c.TruncateFrequency:
		err = errImmutableField{Field: "wal_truncate_frequency"}
	case i.cfg.RemoteFlushDeadline != c.RemoteFlushDeadline:
		err = errImmutableField{Field: "remote_flush_deadline"}
	}
	if err != nil {
		return ErrInvalidUpdate{Inner: err}
	}

	// Check to see if the components exist yet.
	if i.remoteStore == nil {
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

	err = i.remoteStore.ApplyConfig(&config.Config{
		RemoteWriteConfigs: c.RemoteWrite,
	})
	if err != nil {
		return fmt.Errorf("error applying new remote_write configs: %w", err)
	}

	return nil
}

// Ready indicates if the instance is ready for processing.
func (i *Instance) Ready() bool {
	i.mut.Lock()
	defer i.mut.Unlock()

	return i.initialized
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

// Stop stops the WAL
func (i *Instance) Stop() error {
	level.Info(i.logger).Log("msg", "stopping WAL instance", "user", i.Tenant())

	// close WAL first to prevent any further appends
	if err := i.wal.Close(); err != nil {
		level.Error(i.logger).Log("msg", "error stopping WAL instance", "user", i.Tenant(), "err", err)
		return err
	}

	if err := i.remoteStore.Close(); err != nil {
		level.Error(i.logger).Log("msg", "error stopping remote storage instance", "user", i.Tenant(), "err", err)
		return err
	}

	return nil
}

// Tenant returns the tenant name of the instance
func (i *Instance) Tenant() string {
	return i.tenant
}

func (i *Instance) truncateLoop(ctx context.Context, wal walStorage, cfg *Config) {
	// Track the last timestamp we truncated for to prevent segments from getting
	// deleted until at least some new data has been sent.
	var lastTs int64 = math.MinInt64

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.TruncateFrequency):
			// The timestamp ts is used to determine which series are not receiving
			// samples and may be deleted from the WAL. Their most recent append
			// timestamp is compared to ts, and if that timestamp is older then ts,
			// they are considered inactive and may be deleted.
			//
			// Subtracting a duration from ts will delay when it will be considered
			// inactive and scheduled for deletion.
			ts := i.getRemoteWriteTimestamp() - i.cfg.MinAge.Milliseconds()
			if ts < 0 {
				ts = 0
			}

			// Network issues can prevent the result of getRemoteWriteTimestamp from
			// changing. We don't want data in the WAL to grow forever, so we set a cap
			// on the maximum age data can be. If our ts is older than this cutoff point,
			// we'll shift it forward to start deleting very stale data.
			if maxTS := timestamp.FromTime(time.Now().Add(-i.cfg.MaxAge)); ts < maxTS {
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
	SetWriteNotified(wlog.WriteNotified)
	Appender(context.Context) storage.Appender
	Truncate(mint int64) error

	Close() error
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
