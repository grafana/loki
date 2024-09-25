package ingesterrf1

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime/pprof"
	"sync"
	"time"

	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/clientpool"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/objstore"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	// RingKey is the key under which we store the ingesters ring in the KVStore.
	RingKey = "ring"

	shutdownMarkerFilename = "shutdown-requested.txt"
)

// ErrReadOnly is returned when the ingester is shutting down and a push was
// attempted.
var (
	ErrReadOnly = errors.New("Ingester is shutting down")

	compressionStats   = analytics.NewString("ingester_compression")
	targetSizeStats    = analytics.NewInt("ingester_target_size_bytes")
	activeTenantsStats = analytics.NewInt("ingester_active_tenants")
)

// Config for an ingester.
type Config struct {
	Enabled bool `yaml:"enabled" doc:"description=Whether the ingester is enabled."`

	LifecyclerConfig    ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`
	MaxSegmentAge       time.Duration         `yaml:"max_segment_age"`
	MaxSegmentSize      int                   `yaml:"max_segment_size"`
	MaxSegments         int                   `yaml:"max_segments"`
	ConcurrentFlushes   int                   `yaml:"concurrent_flushes"`
	FlushCheckPeriod    time.Duration         `yaml:"flush_check_period"`
	FlushOpBackoff      backoff.Config        `yaml:"flush_op_backoff"`
	FlushOpTimeout      time.Duration         `yaml:"flush_op_timeout"`
	AutoForgetUnhealthy bool                  `yaml:"autoforget_unhealthy"`

	MaxReturnedErrors int `yaml:"max_returned_stream_errors"`

	// For testing, you can override the address and ID of this ingester.
	ingesterClientFactory func(cfg client.Config, addr string) (client.HealthAndIngesterClient, error)

	// Optional wrapper that can be used to modify the behaviour of the ingester
	Wrapper Wrapper `yaml:"-"`

	IndexShards int `yaml:"index_shards"`

	MaxDroppedStreams int `yaml:"max_dropped_streams"`

	ShutdownMarkerPath string `yaml:"shutdown_marker_path"`

	OwnedStreamsCheckInterval time.Duration `yaml:"owned_streams_check_interval" doc:"description=Interval at which the ingester ownedStreamService checks for changes in the ring to recalculate owned streams."`
	StreamRetainPeriod        time.Duration `yaml:"stream_retain_period" doc:"description=How long stream metadata is retained in memory after it was last seen."`

	// Tee configs
	ClientConfig clientpool.Config       `yaml:"client_config,omitempty" doc:"description=Configures how the pattern ingester will connect to the ingesters."`
	factory      ring_client.PoolFactory `yaml:"-"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingester-rf1.", f, util_log.Logger)
	cfg.ClientConfig.RegisterFlags(f)

	f.IntVar(&cfg.ConcurrentFlushes, "ingester-rf1.concurrent-flushes", 32, "How many flushes can happen concurrently from each stream.")
	f.DurationVar(&cfg.FlushCheckPeriod, "ingester-rf1.flush-check-period", 500*time.Millisecond, "How often should the ingester see if there are any blocks to flush. The first flush check is delayed by a random time up to 0.8x the flush check period. Additionally, there is +/- 1% jitter added to the interval.")
	f.DurationVar(&cfg.FlushOpBackoff.MinBackoff, "ingester-rf1.flush-op-backoff-min-period", 100*time.Millisecond, "Minimum backoff period when a flush fails. Each concurrent flush has its own backoff, see `ingester.concurrent-flushes`.")
	f.DurationVar(&cfg.FlushOpBackoff.MaxBackoff, "ingester-rf1.flush-op-backoff-max-period", time.Minute, "Maximum backoff period when a flush fails. Each concurrent flush has its own backoff, see `ingester.concurrent-flushes`.")
	f.IntVar(&cfg.FlushOpBackoff.MaxRetries, "ingester-rf1.flush-op-backoff-retries", 10, "Maximum retries for failed flushes.")
	f.DurationVar(&cfg.FlushOpTimeout, "ingester-rf1.flush-op-timeout", 10*time.Second, "The timeout for an individual flush. Will be retried up to `flush-op-backoff-retries` times.")
	f.DurationVar(&cfg.MaxSegmentAge, "ingester-rf1.max-segment-age", 500*time.Millisecond, "The maximum age of a segment before it should be flushed. Increasing this value allows more time for a segment to grow to max-segment-size, but may increase latency if the write volume is too small.")
	f.IntVar(&cfg.MaxSegmentSize, "ingester-rf1.max-segment-size", 8*1024*1024, "The maximum size of a segment before it should be flushed. It is not a strict limit, and segments can exceed the maximum size when individual appends are larger than the remaining capacity.")
	f.IntVar(&cfg.MaxSegments, "ingester-rf1.max-segments", 10, "The maximum number of segments to buffer in-memory. Increasing this value allows for large bursts of writes to be buffered in memory, but may increase latency if the write volume exceeds the rate at which segments can be flushed.")

	f.IntVar(&cfg.MaxReturnedErrors, "ingester-rf1.max-ignored-stream-errors", 10, "The maximum number of errors a stream will report to the user when a push fails. 0 to make unlimited.")
	f.BoolVar(&cfg.AutoForgetUnhealthy, "ingester-rf1.autoforget-unhealthy", false, "Forget about ingesters having heartbeat timestamps older than `ring.kvstore.heartbeat_timeout`. This is equivalent to clicking on the `/ring` `forget` button in the UI: the ingester is removed from the ring. This is a useful setting when you are sure that an unhealthy node won't return. An example is when not using stateful sets or the equivalent. Use `memberlist.rejoin_interval` > 0 to handle network partition cases when using a memberlist.")
	f.IntVar(&cfg.IndexShards, "ingester-rf1.index-shards", index.DefaultIndexShards, "Shard factor used in the ingesters for the in process reverse index. This MUST be evenly divisible by ALL schema shard factors or Loki will not start.")
	f.IntVar(&cfg.MaxDroppedStreams, "ingester-rf1.tailer.max-dropped-streams", 10, "Maximum number of dropped streams to keep in memory during tailing.")
	f.StringVar(&cfg.ShutdownMarkerPath, "ingester-rf1.shutdown-marker-path", "", "Path where the shutdown marker file is stored. If not set and common.path_prefix is set then common.path_prefix will be used.")
	f.DurationVar(&cfg.OwnedStreamsCheckInterval, "ingester-rf1.owned-streams-check-interval", 30*time.Second, "Interval at which the ingester ownedStreamService checks for changes in the ring to recalculate owned streams.")
	f.DurationVar(&cfg.StreamRetainPeriod, "ingester-rf1.stream-retain-period", 5*time.Minute, "How long stream metadata should be retained in-memory after the last log was seen.")
	f.BoolVar(&cfg.Enabled, "ingester-rf1.enabled", false, "Flag to enable or disable the usage of the ingester-rf1 component.")
}

func (cfg *Config) Validate() error {
	if cfg.FlushOpBackoff.MinBackoff > cfg.FlushOpBackoff.MaxBackoff {
		return errors.New("invalid flush op min backoff: cannot be larger than max backoff")
	}
	if cfg.FlushOpBackoff.MaxRetries <= 0 {
		return fmt.Errorf("invalid flush op max retries: %d", cfg.FlushOpBackoff.MaxRetries)
	}
	if cfg.FlushOpTimeout <= 0 {
		return fmt.Errorf("invalid flush op timeout: %s", cfg.FlushOpTimeout)
	}

	return nil
}

type Wrapper interface {
	Wrap(wrapped Interface) Interface
}

// Storage is the store interface we need on the ingester.
type Storage interface {
	PutObject(ctx context.Context, objectKey string, object io.Reader) error
	Stop()
}

// Interface is an interface for the Ingester
type Interface interface {
	services.Service
	http.Handler

	logproto.PusherServer

	CheckReady(ctx context.Context) error
	FlushHandler(w http.ResponseWriter, _ *http.Request)
	GetOrCreateInstance(instanceID string) (*instance, error)
	ShutdownHandler(w http.ResponseWriter, r *http.Request)
	PrepareShutdown(w http.ResponseWriter, r *http.Request)
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	services.Service

	cfg    Config
	logger log.Logger

	clientConfig  client.Config
	tenantConfigs *runtime.TenantConfigs

	shutdownMtx  sync.Mutex // Allows processes to grab a lock and prevent a shutdown
	instancesMtx sync.RWMutex
	instances    map[string]*instance
	readonly     bool

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher

	store           Storage
	metastoreClient metastorepb.MetastoreServiceClient
	periodicConfigs []config.PeriodConfig

	loopQuit    chan struct{}
	tailersQuit chan struct{}

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushBuffers     []*bytes.Buffer
	flushWorkersDone sync.WaitGroup

	wal *wal.Manager

	limiter *Limiter

	// Flag for whether stopping the ingester service should also terminate the
	// loki process.
	// This is set when calling the shutdown handler.
	terminateOnShutdown bool

	// Only used by WAL & flusher to coordinate backpressure during replay.
	// replayController *replayController

	metrics *ingesterMetrics

	streamRateCalculator *StreamRateCalculator

	writeLogManager *writefailures.Manager

	customStreamsTracker push.UsageTracker

	readRing ring.ReadRing
}

// New makes a new Ingester.
func New(cfg Config, clientConfig client.Config,
	periodConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
	limits Limits, configs *runtime.TenantConfigs,
	metastoreClient metastorepb.MetastoreServiceClient,
	registerer prometheus.Registerer,
	writeFailuresCfg writefailures.Cfg,
	metricsNamespace string,
	logger log.Logger,
	customStreamsTracker push.UsageTracker, readRing ring.ReadRing,
) (*Ingester, error) {
	storage, err := objstore.New(periodConfigs, storageConfig, clientMetrics)
	if err != nil {
		return nil, err
	}
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = client.New
	}
	metrics := newIngesterMetrics(registerer)

	walManager, err := wal.NewManager(wal.Config{
		MaxAge:         cfg.MaxSegmentAge,
		MaxSegments:    int64(cfg.MaxSegments),
		MaxSegmentSize: int64(cfg.MaxSegmentSize),
	}, wal.NewManagerMetrics(registerer))
	if err != nil {
		return nil, err
	}

	i := &Ingester{
		cfg:                  cfg,
		logger:               logger,
		clientConfig:         clientConfig,
		tenantConfigs:        configs,
		instances:            map[string]*instance{},
		store:                storage,
		periodicConfigs:      periodConfigs,
		flushBuffers:         make([]*bytes.Buffer, cfg.ConcurrentFlushes),
		flushWorkersDone:     sync.WaitGroup{},
		loopQuit:             make(chan struct{}),
		tailersQuit:          make(chan struct{}),
		metrics:              metrics,
		metastoreClient:      metastoreClient,
		terminateOnShutdown:  false,
		streamRateCalculator: NewStreamRateCalculator(),
		writeLogManager:      writefailures.NewManager(logger, registerer, writeFailuresCfg, configs, "ingester_rf1"),
		customStreamsTracker: customStreamsTracker,
		readRing:             readRing,
		wal:                  walManager,
	}

	// TODO: change flush on shutdown
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester-rf1", "ingester-rf1-ring", true, logger, prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer))
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	// Now that the lifecycler has been created, we can create the limiter
	// which depends on it.
	i.limiter = NewLimiter(limits, metrics, i.lifecycler, cfg.LifecyclerConfig.RingConfig.ReplicationFactor)

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)

	i.setupAutoForget()

	// i.recalculateOwnedStreams = newRecalculateOwnedStreams(i.getInstances, i.lifecycler.ID, i.readRing, cfg.OwnedStreamsCheckInterval, util_log.Logger)

	return i, nil
}

// setupAutoForget looks for ring status if `AutoForgetUnhealthy` is enabled
// when enabled, unhealthy ingesters that reach `ring.kvstore.heartbeat_timeout` are removed from the ring every `HeartbeatPeriod`
func (i *Ingester) setupAutoForget() {
	if !i.cfg.AutoForgetUnhealthy {
		return
	}

	go func() {
		ctx := context.Background()
		err := i.Service.AwaitRunning(ctx)
		if err != nil {
			level.Error(i.logger).Log("msg", fmt.Sprintf("autoforget received error %s, autoforget is disabled", err.Error()))
			return
		}

		level.Info(i.logger).Log("msg", fmt.Sprintf("autoforget is enabled and will remove unhealthy instances from the ring after %v with no heartbeat", i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout))

		ticker := time.NewTicker(i.cfg.LifecyclerConfig.HeartbeatPeriod)
		defer ticker.Stop()

		var forgetList []string
		for range ticker.C {
			err := i.lifecycler.KVStore.CAS(ctx, RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
				forgetList = forgetList[:0]
				if in == nil {
					return nil, false, nil
				}

				ringDesc, ok := in.(*ring.Desc)
				if !ok {
					level.Warn(i.logger).Log("msg", fmt.Sprintf("autoforget saw a KV store value that was not `ring.Desc`, got `%T`", in))
					return nil, false, nil
				}

				for id, ingester := range ringDesc.Ingesters {
					if !ingester.IsHealthy(ring.Reporting, i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout, time.Now()) {
						if i.lifecycler.ID == id {
							level.Warn(i.logger).Log("msg", fmt.Sprintf("autoforget has seen our ID `%s` as unhealthy in the ring, network may be partitioned, skip forgeting ingesters this round", id))
							return nil, false, nil
						}
						forgetList = append(forgetList, id)
					}
				}

				if len(forgetList) == len(ringDesc.Ingesters)-1 {
					level.Warn(i.logger).Log("msg", fmt.Sprintf("autoforget have seen %d unhealthy ingesters out of %d, network may be partioned, skip forgeting ingesters this round", len(forgetList), len(ringDesc.Ingesters)))
					forgetList = forgetList[:0]
					return nil, false, nil
				}

				if len(forgetList) > 0 {
					for _, id := range forgetList {
						ringDesc.RemoveIngester(id)
					}
					return ringDesc, true, nil
				}
				return nil, false, nil
			})
			if err != nil {
				level.Warn(i.logger).Log("msg", err)
				continue
			}

			for _, id := range forgetList {
				level.Info(i.logger).Log("msg", fmt.Sprintf("autoforget removed ingester %v from the ring because it was not healthy after %v", id, i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout))
			}
			i.metrics.autoForgetUnhealthyIngestersTotal.Add(float64(len(forgetList)))
		}
	}()
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *Ingester) starting(ctx context.Context) error {
	i.InitFlushWorkers()

	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)
	shutdownMarker, err := shutdownMarkerExists(shutdownMarkerPath)
	if err != nil {
		return fmt.Errorf("failed to check ingester shutdown marker: %w", err)
	}

	if shutdownMarker {
		level.Info(i.logger).Log("msg", "detected existing shutdown marker, setting unregister and flush on shutdown", "path", shutdownMarkerPath)
		i.setPrepareShutdown()
	}

	//err = i.recalculateOwnedStreams.StartAsync(ctx)
	//if err != nil {
	//	return fmt.Errorf("can not start recalculate owned streams service: %w", err)
	//}

	go i.periodicStreamMaintenance()
	return nil
}

func (i *Ingester) running(ctx context.Context) error {
	var serviceError error
	select {
	// wait until service is asked to stop
	case <-ctx.Done():
	// stop
	case err := <-i.lifecyclerWatcher.Chan():
		serviceError = fmt.Errorf("lifecycler failed: %w", err)
	}

	// close tailers before stopping our loop
	//close(i.tailersQuit)
	//for _, instance := range i.getInstances() {
	//	instance.closeTailers()
	//}

	return serviceError
}

// stopping is called when Ingester transitions to Stopping state.
//
// At this point, loop no longer runs, but flushers are still running.
func (i *Ingester) stopping(_ error) error {
	i.stopIncomingRequests()
	var errs util.MultiError

	//if i.flushOnShutdownSwitch.Get() {
	//	i.lifecycler.SetFlushOnShutdown(true)
	//}
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.lifecycler))

	i.flushWorkersDone.Wait()

	// i.streamRateCalculator.Stop()

	// In case the flag to terminate on shutdown is set or this instance is marked to release its resources,
	// we need to mark the ingester service as "failed", so Loki will shut down entirely.
	// The module manager logs the failure `modules.ErrStopProcess` in a special way.
	if i.terminateOnShutdown && errs.Err() == nil {
		i.removeShutdownMarkerFile()
		return modules.ErrStopProcess
	}
	i.store.Stop()
	return errs.Err()
}

// stopIncomingRequests is called when ingester is stopping
func (i *Ingester) stopIncomingRequests() {
	i.shutdownMtx.Lock()
	defer i.shutdownMtx.Unlock()

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()

	i.readonly = true
}

// removeShutdownMarkerFile removes the shutdown marker if it exists. Any errors are logged.
func (i *Ingester) removeShutdownMarkerFile() {
	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)
	exists, err := shutdownMarkerExists(shutdownMarkerPath)
	if err != nil {
		level.Error(i.logger).Log("msg", "error checking shutdown marker file exists", "err", err)
	}
	if exists {
		err = removeShutdownMarker(shutdownMarkerPath)
		if err != nil {
			level.Error(i.logger).Log("msg", "error removing shutdown marker file", "err", err)
		}
	}
}

func (i *Ingester) periodicStreamMaintenance() {
	streamRetentionTicker := time.NewTicker(i.cfg.StreamRetainPeriod)
	defer streamRetentionTicker.Stop()

	for {
		select {
		case <-streamRetentionTicker.C:
			i.cleanIdleStreams()
		case <-i.loopQuit:
			return
		}
	}
}

func (i *Ingester) cleanIdleStreams() {
	for _, instance := range i.getInstances() {
		_ = instance.streams.ForEach(func(s *stream) (bool, error) {
			if time.Since(s.highestTs) > i.cfg.StreamRetainPeriod {
				instance.streams.Delete(s)
			}
			return true, nil
		})
	}
}

// PrepareShutdown will handle the /ingester/prepare_shutdown endpoint.
//
// Internally, when triggered, this handler will configure the ingester service to release their resources whenever a SIGTERM is received.
// Releasing resources meaning flushing data, deleting tokens, and removing itself from the ring.
//
// It also creates a file on disk which is used to re-apply the configuration if the
// ingester crashes and restarts before being permanently shutdown.
//
// * `GET` shows the status of this configuration
// * `POST` enables this configuration
// * `DELETE` disables this configuration
func (i *Ingester) PrepareShutdown(w http.ResponseWriter, r *http.Request) {
	if i.cfg.ShutdownMarkerPath == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)

	switch r.Method {
	case http.MethodGet:
		exists, err := shutdownMarkerExists(shutdownMarkerPath)
		if err != nil {
			level.Error(i.logger).Log("msg", "unable to check for prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if exists {
			util.WriteTextResponse(w, "set")
		} else {
			util.WriteTextResponse(w, "unset")
		}
	case http.MethodPost:
		if err := createShutdownMarker(shutdownMarkerPath); err != nil {
			level.Error(i.logger).Log("msg", "unable to create prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		i.setPrepareShutdown()
		level.Info(i.logger).Log("msg", "created prepare-shutdown marker file", "path", shutdownMarkerPath)

		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := removeShutdownMarker(shutdownMarkerPath); err != nil {
			level.Error(i.logger).Log("msg", "unable to remove prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		i.unsetPrepareShutdown()
		level.Info(i.logger).Log("msg", "removed prepare-shutdown marker file", "path", shutdownMarkerPath)

		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// setPrepareShutdown toggles ingester lifecycler config to prepare for shutdown
func (i *Ingester) setPrepareShutdown() {
	level.Info(i.logger).Log("msg", "preparing full ingester shutdown, resources will be released on SIGTERM")
	i.lifecycler.SetFlushOnShutdown(true)
	i.lifecycler.SetUnregisterOnShutdown(true)
	i.terminateOnShutdown = true
	i.metrics.shutdownMarker.Set(1)
}

func (i *Ingester) unsetPrepareShutdown() {
	level.Info(i.logger).Log("msg", "undoing preparation for full ingester shutdown")
	i.lifecycler.SetFlushOnShutdown(true)
	i.lifecycler.SetUnregisterOnShutdown(i.cfg.LifecyclerConfig.UnregisterOnShutdown)
	i.terminateOnShutdown = false
	i.metrics.shutdownMarker.Set(0)
}

// createShutdownMarker writes a marker file to disk to indicate that an ingester is
// going to be scaled down in the future. The presence of this file means that an ingester
// should flush and upload all data when stopping.
func createShutdownMarker(p string) error {
	// Write the file, fsync it, then fsync the containing directory in order to guarantee
	// it is persisted to disk. From https://man7.org/linux/man-pages/man2/fsync.2.html
	//
	// > Calling fsync() does not necessarily ensure that the entry in the
	// > directory containing the file has also reached disk.  For that an
	// > explicit fsync() on a file descriptor for the directory is also
	// > needed.
	file, err := os.Create(p)
	if err != nil {
		return err
	}

	merr := multierror.New()
	_, err = file.WriteString(time.Now().UTC().Format(time.RFC3339))
	merr.Add(err)
	merr.Add(file.Sync())
	merr.Add(file.Close())

	if err := merr.Err(); err != nil {
		return err
	}

	dir, err := os.OpenFile(path.Dir(p), os.O_RDONLY, 0o777)
	if err != nil {
		return err
	}

	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// removeShutdownMarker removes the shutdown marker file if it exists.
func removeShutdownMarker(p string) error {
	err := os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	dir, err := os.OpenFile(path.Dir(p), os.O_RDONLY, 0o777)
	if err != nil {
		return err
	}

	merr := multierror.New()
	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// shutdownMarkerExists returns true if the shutdown marker file exists, false otherwise
func shutdownMarkerExists(p string) (bool, error) {
	s, err := os.Stat(p)
	if err != nil && os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return s.Mode().IsRegular(), nil
}

// ShutdownHandler handles a graceful shutdown of the ingester service and
// termination of the Loki process.
func (i *Ingester) ShutdownHandler(w http.ResponseWriter, r *http.Request) {
	// Don't allow calling the shutdown handler multiple times
	if i.State() != services.Running {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Ingester is stopping or already stopped."))
		return
	}
	params := r.URL.Query()
	doFlush := util.FlagFromValues(params, "flush", true)
	doDeleteRingTokens := util.FlagFromValues(params, "delete_ring_tokens", false)
	doTerminate := util.FlagFromValues(params, "terminate", true)
	err := i.handleShutdown(doTerminate, doFlush, doDeleteRingTokens)

	// Stopping the module will return the modules.ErrStopProcess error. This is
	// needed so the Loki process is shut down completely.
	if err == nil || err == modules.ErrStopProcess {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
	}
}

// handleShutdown triggers the following operations:
//   - Change the state of ring to stop accepting writes.
//   - optional: Flush all the chunks.
//   - optional: Delete ring tokens file
//   - Unregister from KV store
//   - optional: Terminate process (handled by service manager in loki.go)
func (i *Ingester) handleShutdown(terminate, flush, del bool) error {
	i.lifecycler.SetFlushOnShutdown(flush)
	i.lifecycler.SetClearTokensOnShutdown(del)
	i.lifecycler.SetUnregisterOnShutdown(true)
	i.terminateOnShutdown = terminate
	return services.StopAndAwaitTerminated(context.Background(), i)
}

// Push implements logproto.Pusher.
func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	} else if i.readonly {
		return nil, ErrReadOnly
	}

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}

	if err = instance.Push(ctx, i.wal, req); err != nil {
		return nil, err
	}

	return &logproto.PushResponse{}, nil
}

// GetStreamRates returns a response containing all streams and their current rate
// TODO: It might be nice for this to be human readable, eventually: Sort output and return labels, too?
func (i *Ingester) GetStreamRates(ctx context.Context, _ *logproto.StreamRatesRequest) (*logproto.StreamRatesResponse, error) {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("event", "ingester started to handle GetStreamRates")
		defer sp.LogKV("event", "ingester finished handling GetStreamRates")
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "write"))
	pprof.SetGoroutineLabels(ctx)

	allRates := i.streamRateCalculator.Rates()
	rates := make([]*logproto.StreamRate, len(allRates))
	for idx := range allRates {
		rates[idx] = &allRates[idx]
	}
	return &logproto.StreamRatesResponse{StreamRates: nil}, nil
}

func (i *Ingester) GetOrCreateInstance(instanceID string) (*instance, error) { //nolint:revive
	inst, ok := i.getInstanceByID(instanceID)
	if ok {
		return inst, nil
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		var err error
		inst, err = newInstance(&i.cfg, i.periodicConfigs, instanceID, i.limiter, i.tenantConfigs, i.metrics, i.streamRateCalculator, i.writeLogManager, i.customStreamsTracker, i.logger)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
		activeTenantsStats.Set(int64(len(i.instances)))
	}
	return inst, nil
}

// asyncStoreMaxLookBack returns a max look back period only if active index type is one of async index stores like `boltdb-shipper` and `tsdb`.
// max look back is limited to from time of async store config.
// It considers previous periodic config's from time if that also has async index type.
// This is to limit the lookback to only async stores where relevant.
func (i *Ingester) asyncStoreMaxLookBack() time.Duration {
	activePeriodicConfigIndex := config.ActivePeriodConfig(i.periodicConfigs)
	activePeriodicConfig := i.periodicConfigs[activePeriodicConfigIndex]
	if activePeriodicConfig.IndexType != types.BoltDBShipperType && activePeriodicConfig.IndexType != types.TSDBType {
		return 0
	}

	startTime := activePeriodicConfig.From
	if activePeriodicConfigIndex != 0 && (i.periodicConfigs[activePeriodicConfigIndex-1].IndexType == types.BoltDBShipperType ||
		i.periodicConfigs[activePeriodicConfigIndex-1].IndexType == types.TSDBType) {
		startTime = i.periodicConfigs[activePeriodicConfigIndex-1].From
	}

	maxLookBack := time.Since(startTime.Time.Time())
	return maxLookBack
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) CheckReady(ctx context.Context) error {
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

func (i *Ingester) getInstanceByID(id string) (*instance, bool) {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	inst, ok := i.instances[id]
	return inst, ok
}

func (i *Ingester) getInstances() []*instance {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	instances := make([]*instance, 0, len(i.instances))
	for _, instance := range i.instances {
		instances = append(instances, instance)
	}
	return instances
}

//// Tail logs matching given query
//func (i *Ingester) Tail(req *logproto.TailRequest, queryServer logproto.Querier_TailServer) error {
//	err := i.tail(req, queryServer)
//	err = server_util.ClientGrpcStatusAndError(err)
//	return err
//}
//func (i *Ingester) tail(req *logproto.TailRequest, queryServer logproto.Querier_TailServer) error {
//	select {
//	case <-i.tailersQuit:
//		return errors.New("Ingester is stopping")
//	default:
//	}
//
//	if req.Plan == nil {
//		parsed, err := syntax.ParseLogSelector(req.Query, true)
//		if err != nil {
//			return err
//		}
//		req.Plan = &plan.QueryPlan{
//			AST: parsed,
//		}
//	}
//
//	instanceID, err := tenant.TenantID(queryServer.Context())
//	if err != nil {
//		return err
//	}
//
//	instance, err := i.GetOrCreateInstance(instanceID)
//	if err != nil {
//		return err
//	}
//
//	expr, ok := req.Plan.AST.(syntax.LogSelectorExpr)
//	if !ok {
//		return fmt.Errorf("unsupported query expression: want (LogSelectorExpr), got (%T)", req.Plan.AST)
//	}
//
//	tailer, err := newTailer(instanceID, expr, queryServer, i.cfg.MaxDroppedStreams)
//	if err != nil {
//		return err
//	}
//
//	if err := instance.addNewTailer(queryServer.Context(), tailer); err != nil {
//		return err
//	}
//	tailer.loop()
//	return nil
//}
//
//// TailersCount returns count of active tail requests from a user
//func (i *Ingester) TailersCount(ctx context.Context, _ *logproto.TailersCountRequest) (*logproto.TailersCountResponse, error) {
//	tcr, err := i.tailersCount(ctx)
//	err = server_util.ClientGrpcStatusAndError(err)
//	return tcr, err
//}
//
//func (i *Ingester) tailersCount(ctx context.Context) (*logproto.TailersCountResponse, error) {
//	instanceID, err := tenant.TenantID(ctx)
//	if err != nil {
//		return nil, err
//	}
//
//	resp := logproto.TailersCountResponse{}
//
//	instance, ok := i.getInstanceByID(instanceID)
//	if ok {
//		resp.Count = instance.openTailersCount()
//	}
//
//	return &resp, nil
//}

func (i *Ingester) GetDetectedFields(_ context.Context, r *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	return &logproto.DetectedFieldsResponse{
		Fields: []*logproto.DetectedField{
			{
				Label:       "foo",
				Type:        logproto.DetectedFieldString,
				Cardinality: 1,
			},
		},
		FieldLimit: r.GetFieldLimit(),
	}, nil
}
