package ingester

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	lokilog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	indexstore "github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	index_stats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	server_util "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/util/wal"
)

const (
	// RingKey is the key under which we store the ingesters ring in the KVStore.
	RingKey = "ring"

	shutdownMarkerFilename = "shutdown-requested.txt"

	// PartitionRingKey is the key under which we store the partitions ring used by the "ingest storage".
	PartitionRingKey  = "ingester-partitions-key"
	PartitionRingName = "ingester-partitions"
)

// ErrReadOnly is returned when the ingester is shutting down and a push was
// attempted.
var (
	ErrReadOnly = errors.New("Ingester is shutting down")

	compressionStats   = analytics.NewString("ingester_compression")
	targetSizeStats    = analytics.NewInt("ingester_target_size_bytes")
	walStats           = analytics.NewString("ingester_wal")
	activeTenantsStats = analytics.NewInt("ingester_active_tenants")
)

// Config for an ingester.
type Config struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`

	ConcurrentFlushes   int               `yaml:"concurrent_flushes"`
	FlushCheckPeriod    time.Duration     `yaml:"flush_check_period"`
	FlushOpBackoff      backoff.Config    `yaml:"flush_op_backoff"`
	FlushOpTimeout      time.Duration     `yaml:"flush_op_timeout"`
	RetainPeriod        time.Duration     `yaml:"chunk_retain_period"`
	MaxChunkIdle        time.Duration     `yaml:"chunk_idle_period"`
	BlockSize           int               `yaml:"chunk_block_size"`
	TargetChunkSize     int               `yaml:"chunk_target_size"`
	ChunkEncoding       string            `yaml:"chunk_encoding"`
	parsedEncoding      compression.Codec `yaml:"-"` // placeholder for validated encoding
	MaxChunkAge         time.Duration     `yaml:"max_chunk_age"`
	AutoForgetUnhealthy bool              `yaml:"autoforget_unhealthy"`

	// Synchronization settings. Used to make sure that ingesters cut their chunks at the same moments.
	SyncPeriod         time.Duration `yaml:"sync_period"`
	SyncMinUtilization float64       `yaml:"sync_min_utilization"`

	MaxReturnedErrors int `yaml:"max_returned_stream_errors"`

	// For testing, you can override the address and ID of this ingester.
	ingesterClientFactory func(cfg client.Config, addr string) (client.HealthAndIngesterClient, error)

	QueryStore                  bool          `yaml:"-"`
	QueryStoreMaxLookBackPeriod time.Duration `yaml:"query_store_max_look_back_period"`

	WAL WALConfig `yaml:"wal,omitempty" doc:"description=The ingester WAL (Write Ahead Log) records incoming logs and stores them on the local file systems in order to guarantee persistence of acknowledged data in the event of a process crash."`

	ChunkFilterer          chunk.RequestChunkFilterer     `yaml:"-"`
	PipelineWrapper        lokilog.PipelineWrapper        `yaml:"-"`
	SampleExtractorWrapper lokilog.SampleExtractorWrapper `yaml:"-"`

	// Optional wrapper that can be used to modify the behaviour of the ingester
	Wrapper Wrapper `yaml:"-"`

	IndexShards int `yaml:"index_shards"`

	MaxDroppedStreams int `yaml:"max_dropped_streams"`

	ShutdownMarkerPath string `yaml:"shutdown_marker_path"`

	OwnedStreamsCheckInterval time.Duration `yaml:"owned_streams_check_interval" doc:"description=Interval at which the ingester ownedStreamService checks for changes in the ring to recalculate owned streams."`

	KafkaIngestion KafkaIngestionConfig `yaml:"kafka_ingestion,omitempty"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f, util_log.Logger)
	cfg.WAL.RegisterFlags(f)
	cfg.KafkaIngestion.RegisterFlags(f)

	f.IntVar(&cfg.ConcurrentFlushes, "ingester.concurrent-flushes", 32, "How many flushes can happen concurrently from each stream.")
	f.DurationVar(&cfg.FlushCheckPeriod, "ingester.flush-check-period", 30*time.Second, "How often should the ingester see if there are any blocks to flush. The first flush check is delayed by a random time up to 0.8x the flush check period. Additionally, there is +/- 1% jitter added to the interval.")
	f.DurationVar(&cfg.FlushOpBackoff.MinBackoff, "ingester.flush-op-backoff-min-period", 10*time.Second, "Minimum backoff period when a flush fails. Each concurrent flush has its own backoff, see `ingester.concurrent-flushes`.")
	f.DurationVar(&cfg.FlushOpBackoff.MaxBackoff, "ingester.flush-op-backoff-max-period", time.Minute, "Maximum backoff period when a flush fails. Each concurrent flush has its own backoff, see `ingester.concurrent-flushes`.")
	f.IntVar(&cfg.FlushOpBackoff.MaxRetries, "ingester.flush-op-backoff-retries", 10, "Maximum retries for failed flushes.")
	f.DurationVar(&cfg.FlushOpTimeout, "ingester.flush-op-timeout", 10*time.Minute, "The timeout for an individual flush. Will be retried up to `flush-op-backoff-retries` times.")
	f.DurationVar(&cfg.RetainPeriod, "ingester.chunks-retain-period", 0, "How long chunks should be retained in-memory after they've been flushed.")
	f.DurationVar(&cfg.MaxChunkIdle, "ingester.chunks-idle-period", 30*time.Minute, "How long chunks should sit in-memory with no updates before being flushed if they don't hit the max block size. This means that half-empty chunks will still be flushed after a certain period as long as they receive no further activity.")
	f.IntVar(&cfg.BlockSize, "ingester.chunks-block-size", 256*1024, "The targeted _uncompressed_ size in bytes of a chunk block When this threshold is exceeded the head block will be cut and compressed inside the chunk.")
	f.IntVar(&cfg.TargetChunkSize, "ingester.chunk-target-size", 1572864, "A target _compressed_ size in bytes for chunks. This is a desired size not an exact size, chunks may be slightly bigger or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period). A value of 0 creates chunks with a fixed 10 blocks, a non zero value will create chunks with a variable number of blocks to meet the target size.") // 1.5 MB
	f.StringVar(&cfg.ChunkEncoding, "ingester.chunk-encoding", compression.GZIP.String(), fmt.Sprintf("The algorithm to use for compressing chunk. (%s)", compression.SupportedCodecs()))
	f.DurationVar(&cfg.SyncPeriod, "ingester.sync-period", 1*time.Hour, "Parameters used to synchronize ingesters to cut chunks at the same moment. Sync period is used to roll over incoming entry to a new chunk. If chunk's utilization isn't high enough (eg. less than 50% when sync_min_utilization is set to 0.5), then this chunk rollover doesn't happen.")
	f.Float64Var(&cfg.SyncMinUtilization, "ingester.sync-min-utilization", 0.1, "Minimum utilization of chunk when doing synchronization.")
	f.IntVar(&cfg.MaxReturnedErrors, "ingester.max-ignored-stream-errors", 10, "The maximum number of errors a stream will report to the user when a push fails. 0 to make unlimited.")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 2*time.Hour, "The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this, the current chunk will be flushed to the store and a new chunk created.")
	f.DurationVar(&cfg.QueryStoreMaxLookBackPeriod, "ingester.query-store-max-look-back-period", 0, "How far back should an ingester be allowed to query the store for data, for use only with boltdb-shipper/tsdb index and filesystem object store. -1 for infinite.")
	f.BoolVar(&cfg.AutoForgetUnhealthy, "ingester.autoforget-unhealthy", false, "Forget about ingesters having heartbeat timestamps older than `ring.kvstore.heartbeat_timeout`. This is equivalent to clicking on the `/ring` `forget` button in the UI: the ingester is removed from the ring. This is a useful setting when you are sure that an unhealthy node won't return. An example is when not using stateful sets or the equivalent. Use `memberlist.rejoin_interval` > 0 to handle network partition cases when using a memberlist.")
	f.IntVar(&cfg.IndexShards, "ingester.index-shards", index.DefaultIndexShards, "Shard factor used in the ingesters for the in process reverse index. This MUST be evenly divisible by ALL schema shard factors or Loki will not start.")
	f.IntVar(&cfg.MaxDroppedStreams, "ingester.tailer.max-dropped-streams", 10, "Maximum number of dropped streams to keep in memory during tailing.")
	f.StringVar(&cfg.ShutdownMarkerPath, "ingester.shutdown-marker-path", "", "Path where the shutdown marker file is stored. If not set and common.path_prefix is set then common.path_prefix will be used.")
	f.DurationVar(&cfg.OwnedStreamsCheckInterval, "ingester.owned-streams-check-interval", 30*time.Second, "Interval at which the ingester ownedStreamService checks for changes in the ring to recalculate owned streams.")
}

func (cfg *Config) Validate() error {
	enc, err := compression.ParseCodec(cfg.ChunkEncoding)
	if err != nil {
		return err
	}
	cfg.parsedEncoding = enc

	if err = cfg.WAL.Validate(); err != nil {
		return err
	}

	if cfg.FlushOpBackoff.MinBackoff > cfg.FlushOpBackoff.MaxBackoff {
		return errors.New("invalid flush op min backoff: cannot be larger than max backoff")
	}
	if cfg.FlushOpBackoff.MaxRetries <= 0 {
		return fmt.Errorf("invalid flush op max retries: %d", cfg.FlushOpBackoff.MaxRetries)
	}
	if cfg.FlushOpTimeout <= 0 {
		return fmt.Errorf("invalid flush op timeout: %s", cfg.FlushOpTimeout)
	}
	if cfg.IndexShards <= 0 {
		return fmt.Errorf("invalid ingester index shard factor: %d", cfg.IndexShards)
	}

	return nil
}

type KafkaIngestionConfig struct {
	Enabled             bool                 `yaml:"enabled" doc:"description=Whether the kafka ingester is enabled."`
	PartitionRingConfig partitionring.Config `yaml:"partition_ring" category:"experimental"`
	KafkaConfig         kafka.Config         `yaml:"-"`
}

func (cfg *KafkaIngestionConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.PartitionRingConfig.RegisterFlagsWithPrefix("ingester.", f)
	f.BoolVar(&cfg.Enabled, "ingester.kafka-ingestion-enabled", false, "Whether the ingester will consume data from kafka.")
}

type Wrapper interface {
	Wrap(wrapped Interface) Interface
}

// Store is the store interface we need on the ingester.
type Store interface {
	stores.ChunkWriter
	stores.ChunkFetcher
	storage.SelectStore
	storage.SchemaConfigProvider
	indexstore.StatsReader
}

// Interface is an interface for the Ingester
type Interface interface {
	services.Service

	logproto.PusherServer
	logproto.QuerierServer
	logproto.StreamDataServer

	CheckReady(ctx context.Context) error
	FlushHandler(w http.ResponseWriter, _ *http.Request)
	GetOrCreateInstance(instanceID string) (*instance, error)
	ShutdownHandler(w http.ResponseWriter, r *http.Request)
	PrepareShutdown(w http.ResponseWriter, r *http.Request)
	PreparePartitionDownscaleHandler(w http.ResponseWriter, r *http.Request)
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

	store           Store
	periodicConfigs []config.PeriodConfig

	loopDone    sync.WaitGroup
	loopQuit    chan struct{}
	tailersQuit chan struct{}

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup

	// Spread out calls to the chunk store over the flush period
	flushRateLimiter *rate.Limiter

	limiter *Limiter

	// Denotes whether the ingester should flush on shutdown.
	// Currently only used by the WAL to signal when the disk is full.
	flushOnShutdownSwitch *OnceSwitch
	// Flag for whether stopping the ingester service should also terminate the
	// loki process.
	// This is set when calling the shutdown handler.
	terminateOnShutdown bool

	// Only used by WAL & flusher to coordinate backpressure during replay.
	replayController *replayController

	metrics *ingesterMetrics

	wal WAL

	chunkFilter      chunk.RequestChunkFilterer
	extractorWrapper lokilog.SampleExtractorWrapper
	pipelineWrapper  lokilog.PipelineWrapper

	streamRateCalculator *StreamRateCalculator

	writeLogManager *writefailures.Manager

	customStreamsTracker push.UsageTracker

	// recalculateOwnedStreams periodically checks the ring for changes and recalculates owned streams for each instance.
	readRing                ring.ReadRing
	recalculateOwnedStreams *recalculateOwnedStreamsSvc

	ingestPartitionID       int32
	partitionRingLifecycler *ring.PartitionInstanceLifecycler
	partitionReader         *partition.ReaderService
}

// New makes a new Ingester.
func New(cfg Config, clientConfig client.Config, store Store, limits Limits, configs *runtime.TenantConfigs, registerer prometheus.Registerer, writeFailuresCfg writefailures.Cfg, metricsNamespace string, logger log.Logger, customStreamsTracker push.UsageTracker, readRing ring.ReadRing, partitionRingWatcher ring.PartitionRingReader) (*Ingester, error) {
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = client.New
	}
	compressionStats.Set(cfg.ChunkEncoding)
	targetSizeStats.Set(int64(cfg.TargetChunkSize))
	walStats.Set("disabled")
	if cfg.WAL.Enabled {
		walStats.Set("enabled")
	}
	metrics := newIngesterMetrics(registerer, metricsNamespace)

	i := &Ingester{
		cfg:                   cfg,
		logger:                logger,
		clientConfig:          clientConfig,
		tenantConfigs:         configs,
		instances:             map[string]*instance{},
		store:                 store,
		periodicConfigs:       store.GetSchemaConfigs(),
		loopQuit:              make(chan struct{}),
		flushQueues:           make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		flushRateLimiter:      rate.NewLimiter(rate.Inf, 1),
		tailersQuit:           make(chan struct{}),
		metrics:               metrics,
		flushOnShutdownSwitch: &OnceSwitch{},
		terminateOnShutdown:   false,
		streamRateCalculator:  NewStreamRateCalculator(),
		writeLogManager:       writefailures.NewManager(logger, registerer, writeFailuresCfg, configs, "ingester"),
		customStreamsTracker:  customStreamsTracker,
		readRing:              readRing,
	}
	i.replayController = newReplayController(metrics, cfg.WAL, &replayFlusher{i})

	if cfg.WAL.Enabled {
		if err := os.MkdirAll(cfg.WAL.Dir, 0o750); err != nil {
			// Best effort try to make path absolute for easier debugging.
			path, _ := filepath.Abs(cfg.WAL.Dir)
			if path == "" {
				path = cfg.WAL.Dir
			}

			return nil, fmt.Errorf("creating WAL folder at %q: %w", path, err)
		}
	}

	wal, err := newWAL(cfg.WAL, registerer, metrics, newIngesterSeriesIter(i))
	if err != nil {
		return nil, err
	}
	i.wal = wal

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", RingKey, !cfg.WAL.Enabled || cfg.WAL.FlushOnShutdown, logger, prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer))
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	if i.cfg.KafkaIngestion.Enabled {
		i.ingestPartitionID, err = partitionring.ExtractIngesterPartitionID(cfg.LifecyclerConfig.ID)
		if err != nil {
			return nil, fmt.Errorf("calculating ingester partition ID: %w", err)
		}
		partitionRingKV := cfg.KafkaIngestion.PartitionRingConfig.KVStore.Mock
		if partitionRingKV == nil {
			partitionRingKV, err = kv.NewClient(cfg.KafkaIngestion.PartitionRingConfig.KVStore, ring.GetPartitionRingCodec(), kv.RegistererWithKVName(registerer, PartitionRingName+"-lifecycler"), logger)
			if err != nil {
				return nil, fmt.Errorf("creating KV store for ingester partition ring: %w", err)
			}
		}
		i.partitionRingLifecycler = ring.NewPartitionInstanceLifecycler(
			i.cfg.KafkaIngestion.PartitionRingConfig.ToLifecyclerConfig(i.ingestPartitionID, cfg.LifecyclerConfig.ID),
			PartitionRingName,
			PartitionRingKey,
			partitionRingKV,
			logger,
			prometheus.WrapRegistererWithPrefix("loki_", registerer))

		i.partitionReader, err = partition.NewReaderService(
			cfg.KafkaIngestion.KafkaConfig,
			i.ingestPartitionID,
			cfg.LifecyclerConfig.ID,
			NewKafkaConsumerFactory(i, registerer),
			logger,
			registerer,
		)
		if err != nil {
			return nil, err
		}
		i.lifecyclerWatcher.WatchService(i.partitionRingLifecycler)
		i.lifecyclerWatcher.WatchService(i.partitionReader)
	}

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)

	i.setupAutoForget()

	if i.cfg.ChunkFilterer != nil {
		i.SetChunkFilterer(i.cfg.ChunkFilterer)
	}

	if i.cfg.PipelineWrapper != nil {
		i.SetPipelineWrapper(i.cfg.PipelineWrapper)
	}

	if i.cfg.SampleExtractorWrapper != nil {
		i.SetExtractorWrapper(i.cfg.SampleExtractorWrapper)
	}

	var streamCountLimiter limiterRingStrategy
	var ownedStreamsStrategy ownershipStrategy
	var streamRateLimiter RateLimiterStrategy
	if i.cfg.KafkaIngestion.Enabled {
		streamCountLimiter = newPartitionRingLimiterStrategy(partitionRingWatcher, limits.IngestionPartitionsTenantShardSize)
		ownedStreamsStrategy = newOwnedStreamsPartitionStrategy(i.ingestPartitionID, partitionRingWatcher, limits.IngestionPartitionsTenantShardSize, util_log.Logger)
		streamRateLimiter = &NoLimitsStrategy{} // Kafka ingestion does not have per-stream rate limits, because we control the consumption speed.
	} else {
		streamCountLimiter = newIngesterRingLimiterStrategy(i.lifecycler, cfg.LifecyclerConfig.RingConfig.ReplicationFactor)
		ownedStreamsStrategy = newOwnedStreamsIngesterStrategy(i.lifecycler.ID, i.readRing, util_log.Logger)
		streamRateLimiter = &TenantBasedStrategy{limits: limits}
	}
	// Now that the lifecycler has been created, we can create the limiter
	// which depends on it.
	i.limiter = NewLimiter(limits, metrics, streamCountLimiter, streamRateLimiter)
	i.recalculateOwnedStreams = newRecalculateOwnedStreamsSvc(i.getInstances, ownedStreamsStrategy, cfg.OwnedStreamsCheckInterval, util_log.Logger)

	return i, nil
}

func (i *Ingester) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

func (i *Ingester) SetExtractorWrapper(wrapper lokilog.SampleExtractorWrapper) {
	i.extractorWrapper = wrapper
}

func (i *Ingester) SetPipelineWrapper(wrapper lokilog.PipelineWrapper) {
	i.pipelineWrapper = wrapper
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

func (i *Ingester) starting(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			// if starting() fails for any reason (e.g., context canceled),
			// the lifecycler must be stopped.
			_ = services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
		}
	}()

	if i.cfg.WAL.Enabled {
		start := time.Now()

		// Ignore retain period during wal replay.
		oldRetain := i.cfg.RetainPeriod
		i.cfg.RetainPeriod = 0

		// Disable the in process stream limit checks while replaying the WAL.
		// It is re-enabled in the recover's Close() method.
		i.limiter.DisableForWALReplay()

		recoverer := newIngesterRecoverer(i)

		i.metrics.walReplayActive.Set(1)

		endReplay := func() func() {
			var once sync.Once
			return func() {
				once.Do(func() {
					level.Info(i.logger).Log("msg", "closing recoverer")
					recoverer.Close()

					elapsed := time.Since(start)

					i.metrics.walReplayActive.Set(0)
					i.metrics.walReplayDuration.Set(elapsed.Seconds())
					i.cfg.RetainPeriod = oldRetain
					level.Info(i.logger).Log("msg", "WAL recovery finished", "time", elapsed.String())
				})
			}
		}()
		defer endReplay()

		level.Info(i.logger).Log("msg", "recovering from checkpoint")
		checkpointReader, checkpointCloser, err := newCheckpointReader(i.cfg.WAL.Dir, i.logger)
		if err != nil {
			return err
		}
		defer checkpointCloser.Close()

		checkpointRecoveryErr := RecoverCheckpoint(checkpointReader, recoverer)
		if checkpointRecoveryErr != nil {
			i.metrics.walCorruptionsTotal.WithLabelValues(walTypeCheckpoint).Inc()
			level.Error(i.logger).Log(
				"msg",
				`Recovered from checkpoint with errors. Some streams were likely not recovered due to WAL checkpoint file corruptions (or WAL file deletions while Loki is running). No administrator action is needed and data loss is only a possibility if more than (replication factor / 2 + 1) ingesters suffer from this.`,
				"elapsed", time.Since(start).String(),
			)
		}
		level.Info(i.logger).Log(
			"msg", "recovered WAL checkpoint recovery finished",
			"elapsed", time.Since(start).String(),
			"errors", checkpointRecoveryErr != nil,
		)

		level.Info(i.logger).Log("msg", "recovering from WAL")
		segmentReader, segmentCloser, err := wal.NewWalReader(i.cfg.WAL.Dir, -1)
		if err != nil {
			return err
		}
		defer segmentCloser.Close()

		segmentRecoveryErr := RecoverWAL(ctx, segmentReader, recoverer)
		if segmentRecoveryErr != nil {
			i.metrics.walCorruptionsTotal.WithLabelValues(walTypeSegment).Inc()
			level.Error(i.logger).Log(
				"msg",
				"Recovered from WAL segments with errors. Some streams and/or entries were likely not recovered due to WAL segment file corruptions (or WAL file deletions while Loki is running). No administrator action is needed and data loss is only a possibility if more than (replication factor / 2 + 1) ingesters suffer from this.",
				"elapsed", time.Since(start).String(),
			)
		}
		level.Info(i.logger).Log(
			"msg", "WAL segment recovery finished",
			"elapsed", time.Since(start).String(),
			"errors", segmentRecoveryErr != nil,
		)

		endReplay()

		i.wal.Start()
	}

	i.InitFlushQueues()

	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)
	shutdownMarker, err := shutdownMarkerExists(shutdownMarkerPath)
	if err != nil {
		return fmt.Errorf("failed to check ingester shutdown marker: %w", err)
	}

	if shutdownMarker {
		level.Info(i.logger).Log("msg", "detected existing shutdown marker, setting unregister and flush on shutdown", "path", shutdownMarkerPath)
		i.setPrepareShutdown()
	}

	// start our flush loop: this needs to start before the partition-reader in order for chunks to be shipped in the case of Kafka catching up.
	i.loopDone.Add(1)
	go i.loop()

	// When kafka ingestion is enabled, we have to make sure that reader catches up replaying the partition
	// BEFORE the ingester ring lifecycler is started, because once the ingester ring lifecycler will start
	// it will switch the ingester state in the ring to ACTIVE.
	if i.partitionReader != nil {
		if err := services.StartAndAwaitRunning(ctx, i.partitionReader); err != nil {
			return fmt.Errorf("failed to start partition reader: %w", err)
		}
	}

	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err = i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	err = i.recalculateOwnedStreams.StartAsync(ctx)
	if err != nil {
		return fmt.Errorf("can not start recalculate owned streams service: %w", err)
	}

	err = i.recalculateOwnedStreams.AwaitRunning(ctx)
	if err != nil {
		return fmt.Errorf("can not ensure recalculate owned streams service is running: %w", err)
	}

	if i.partitionRingLifecycler != nil {
		if err := services.StartAndAwaitRunning(ctx, i.partitionRingLifecycler); err != nil {
			return fmt.Errorf("failed to start partition ring lifecycler: %w", err)
		}
	}

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
	close(i.tailersQuit)
	for _, instance := range i.getInstances() {
		instance.closeTailers()
	}

	close(i.loopQuit)
	i.loopDone.Wait()
	return serviceError
}

// stopping is called when Ingester transitions to Stopping state.
//
// At this point, loop no longer runs, but flushers are still running.
func (i *Ingester) stopping(_ error) error {
	i.stopIncomingRequests()

	if i.partitionReader != nil {
		if err := services.StopAndAwaitTerminated(context.Background(), i.partitionReader); err != nil {
			level.Warn(i.logger).Log("msg", "failed to stop partition reader", "err", err)
		}
	}

	if i.partitionRingLifecycler != nil {
		if err := services.StopAndAwaitTerminated(context.Background(), i.partitionRingLifecycler); err != nil {
			level.Warn(i.logger).Log("msg", "failed to stop partition ring lifecycler", "err", err)
		}
	}

	var errs util.MultiError
	errs.Add(i.wal.Stop())

	if i.flushOnShutdownSwitch.Get() {
		i.lifecycler.SetFlushOnShutdown(true)
	}
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.lifecycler))

	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}
	i.flushQueuesDone.Wait()

	i.streamRateCalculator.Stop()

	// In case the flag to terminate on shutdown is set or this instance is marked to release its resources,
	// we need to mark the ingester service as "failed", so Loki will shut down entirely.
	// The module manager logs the failure `modules.ErrStopProcess` in a special way.
	if i.terminateOnShutdown && errs.Err() == nil {
		i.removeShutdownMarkerFile()
		return modules.ErrStopProcess
	}
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

func (i *Ingester) loop() {
	defer i.loopDone.Done()

	// Delay the first flush operation by up to 0.8x the flush time period.
	// This will ensure that multiple ingesters started at the same time do not
	// flush at the same time. Flushing at the same time can cause concurrently
	// writing the same chunk to object storage, which in AWS S3 leads to being
	// rate limited.
	jitter := time.Duration(rand.Int63n(int64(float64(i.cfg.FlushCheckPeriod.Nanoseconds()) * 0.8))) //#nosec G404 -- Jitter does not require a CSPRNG.
	initialDelay := time.NewTimer(jitter)
	defer initialDelay.Stop()

	level.Info(i.logger).Log("msg", "sleeping for initial delay before starting periodic flushing", "delay", jitter)

	select {
	case <-initialDelay.C:
		// do nothing and continue with flush loop
	case <-i.loopQuit:
		// ingester stopped while waiting for initial delay
		return
	}

	// Add +/- 20% of flush interval as jitter.
	// The default flush check period is 30s so max jitter will be 6s.
	j := i.cfg.FlushCheckPeriod / 5
	flushTicker := util.NewTickerWithJitter(i.cfg.FlushCheckPeriod, j)
	defer flushTicker.Stop()

	for {
		select {
		case <-flushTicker.C:
			i.sweepUsers(false, true)

		case <-i.loopQuit:
			return
		}
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

	if i.partitionRingLifecycler != nil {
		// When the prepare shutdown endpoint is called there are two changes in the partitions ring behavior:
		//
		// 1. If setPrepareShutdown() is called at startup, because of the shutdown marker found on disk,
		//    the ingester shouldn't create the partition if doesn't exist, because we expect the ingester will
		//    be scaled down shortly after.
		// 2. When the ingester will shutdown we'll have to remove the ingester from the partition owners,
		//    because we expect the ingester to be scaled down.
		i.partitionRingLifecycler.SetCreatePartitionOnStartup(false)
		i.partitionRingLifecycler.SetRemoveOwnerOnShutdown(true)
	}
}

func (i *Ingester) unsetPrepareShutdown() {
	level.Info(i.logger).Log("msg", "undoing preparation for full ingester shutdown")
	i.lifecycler.SetFlushOnShutdown(!i.cfg.WAL.Enabled || i.cfg.WAL.FlushOnShutdown)
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

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "write"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}
	return &logproto.PushResponse{}, instance.Push(ctx, req)
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
	return &logproto.StreamRatesResponse{StreamRates: rates}, nil
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
		inst, err = newInstance(&i.cfg, i.periodicConfigs, instanceID, i.limiter, i.tenantConfigs, i.wal, i.metrics, i.flushOnShutdownSwitch, i.chunkFilter, i.pipelineWrapper, i.extractorWrapper, i.streamRateCalculator, i.writeLogManager, i.customStreamsTracker)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
		activeTenantsStats.Set(int64(len(i.instances)))
	}
	return inst, nil
}

// Query the ingests for log streams matching a set of matchers.
func (i *Ingester) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	// initialize stats collection for ingester queries.
	_, ctx := stats.NewContext(queryServer.Context())
	_, ctx = metadata.NewContext(ctx)

	if req.Plan == nil {
		parsed, err := syntax.ParseLogSelector(req.Selector, true)
		if err != nil {
			return err
		}
		req.Plan = &plan.QueryPlan{
			AST: parsed,
		}
	}

	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "log"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}
	it, err := instance.Query(ctx, logql.SelectLogParams{QueryRequest: req})
	if err != nil {
		return err
	}

	if start, end, ok := buildStoreRequest(i.cfg, req.Start, req.End, time.Now()); ok {
		storeReq := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Selector:  req.Selector,
			Direction: req.Direction,
			Start:     start,
			End:       end,
			Limit:     req.Limit,
			Shards:    req.Shards,
			Deletes:   req.Deletes,
			Plan:      req.Plan,
		}}
		storeItr, err := i.store.SelectLogs(ctx, storeReq)
		if err != nil {
			util.LogErrorWithContext(ctx, "closing iterator", it.Close)
			return err
		}
		it = iter.NewMergeEntryIterator(ctx, []iter.EntryIterator{it, storeItr}, req.Direction)
	}

	defer util.LogErrorWithContext(ctx, "closing iterator", it.Close)

	// sendBatches uses -1 to specify no limit.
	batchLimit := int32(req.Limit)
	if batchLimit == 0 {
		batchLimit = -1
	}

	return sendBatches(ctx, it, queryServer, batchLimit)
}

// QuerySample the ingesters for series from logs matching a set of matchers.
func (i *Ingester) QuerySample(req *logproto.SampleQueryRequest, queryServer logproto.Querier_QuerySampleServer) error {
	// initialize stats collection for ingester queries.
	_, ctx := stats.NewContext(queryServer.Context())
	_, ctx = metadata.NewContext(ctx)
	sp := opentracing.SpanFromContext(ctx)

	// If the plan is empty we want all series to be returned.
	if req.Plan == nil {
		parsed, err := syntax.ParseSampleExpr(req.Selector)
		if err != nil {
			return err
		}
		req.Plan = &plan.QueryPlan{
			AST: parsed,
		}
	}

	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "metric"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}

	it, err := instance.QuerySample(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
	if err != nil {
		return err
	}
	if sp != nil {
		sp.LogKV("event", "finished instance query sample", "selector", req.Selector, "start", req.Start, "end", req.End)
	}

	if start, end, ok := buildStoreRequest(i.cfg, req.Start, req.End, time.Now()); ok {
		storeReq := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    start,
			End:      end,
			Selector: req.Selector,
			Shards:   req.Shards,
			Deletes:  req.Deletes,
			Plan:     req.Plan,
		}}
		storeItr, err := i.store.SelectSamples(ctx, storeReq)
		if err != nil {
			util.LogErrorWithContext(ctx, "closing iterator", it.Close)
			return err
		}

		it = iter.NewMergeSampleIterator(ctx, []iter.SampleIterator{it, storeItr})
	}

	defer util.LogErrorWithContext(ctx, "closing iterator", it.Close)

	return sendSampleBatches(ctx, it, queryServer)
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

// GetChunkIDs is meant to be used only when using an async store like boltdb-shipper or tsdb.
func (i *Ingester) GetChunkIDs(ctx context.Context, req *logproto.GetChunkIDsRequest) (*logproto.GetChunkIDsResponse, error) {
	gcr, err := i.getChunkIDs(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return gcr, err
}

// GetChunkIDs is meant to be used only when using an async store like boltdb-shipper or tsdb.
func (i *Ingester) getChunkIDs(ctx context.Context, req *logproto.GetChunkIDsRequest) (*logproto.GetChunkIDsResponse, error) {
	orgID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "chunkIDs"))
	pprof.SetGoroutineLabels(ctx)

	asyncStoreMaxLookBack := i.asyncStoreMaxLookBack()
	if asyncStoreMaxLookBack == 0 {
		return &logproto.GetChunkIDsResponse{}, nil
	}

	reqStart := req.Start
	reqStart = adjustQueryStartTime(asyncStoreMaxLookBack, reqStart, time.Now())

	// parse the request
	start, end := util.RoundToMilliseconds(reqStart, req.End)
	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil {
		return nil, err
	}

	// get chunk references
	chunksGroups, _, err := i.store.GetChunks(ctx, orgID, start, end, chunk.NewPredicate(matchers, nil), nil)
	if err != nil {
		return nil, err
	}

	// todo (Callum) ingester should maybe store the whole schema config?
	s := config.SchemaConfig{
		Configs: i.periodicConfigs,
	}

	// build the response
	resp := logproto.GetChunkIDsResponse{ChunkIDs: []string{}}
	for _, chunks := range chunksGroups {
		for _, chk := range chunks {
			resp.ChunkIDs = append(resp.ChunkIDs, s.ExternalKey(chk.ChunkRef))
		}
	}

	return &resp, nil
}

// Label returns the set of labels for the stream this ingester knows about.
func (i *Ingester) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "labels"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(userID)
	if err != nil {
		return nil, err
	}

	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}

	resp, err := instance.Label(ctx, req, matchers...)
	if err != nil {
		return nil, err
	}

	if req.Start == nil {
		return resp, nil
	}

	// Only continue if the active index type is one of async index store types or QueryStore flag is true.
	asyncStoreMaxLookBack := i.asyncStoreMaxLookBack()
	if asyncStoreMaxLookBack == 0 && !i.cfg.QueryStore {
		return resp, nil
	}

	var cs storage.Store
	var ok bool
	if cs, ok = i.store.(storage.Store); !ok {
		return resp, nil
	}

	maxLookBackPeriod := i.cfg.QueryStoreMaxLookBackPeriod
	if asyncStoreMaxLookBack != 0 {
		maxLookBackPeriod = asyncStoreMaxLookBack
	}
	// Adjust the start time based on QueryStoreMaxLookBackPeriod.
	start := adjustQueryStartTime(maxLookBackPeriod, *req.Start, time.Now())
	if start.After(*req.End) {
		// The request is older than we are allowed to query the store, just return what we have.
		return resp, nil
	}
	from, through := model.TimeFromUnixNano(start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	var storeValues []string
	if req.Values {
		storeValues, err = cs.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name, matchers...)
		if err != nil {
			return nil, err
		}
	} else {
		storeValues, err = cs.LabelNamesForMetricName(ctx, userID, from, through, "logs", matchers...)
		if err != nil {
			return nil, err
		}
	}

	return &logproto.LabelResponse{
		Values: util.MergeStringLists(resp.Values, storeValues),
	}, nil
}

// Series queries the ingester for log stream identifiers (label sets) matching a set of matchers
func (i *Ingester) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	sr, err := i.series(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return sr, err
}

func (i *Ingester) series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "series"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return nil, err
	}
	return instance.Series(ctx, req)
}

func (i *Ingester) GetStats(ctx context.Context, req *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	sp := opentracing.SpanFromContext(ctx)

	user, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "stats"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(user)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil {
		return nil, err
	}

	type f func() (*logproto.IndexStatsResponse, error)
	jobs := []f{
		f(func() (*logproto.IndexStatsResponse, error) {
			return instance.GetStats(ctx, req)
		}),
		f(func() (*logproto.IndexStatsResponse, error) {
			return i.store.Stats(ctx, user, req.From, req.Through, matchers...)
		}),
	}
	resps := make([]*logproto.IndexStatsResponse, len(jobs))

	if err := concurrency.ForEachJob(
		ctx,
		len(jobs),
		2,
		func(_ context.Context, idx int) error {
			res, err := jobs[idx]()
			resps[idx] = res
			return err
		},
	); err != nil {
		return nil, err
	}

	merged := index_stats.MergeStats(resps...)
	if sp != nil {
		sp.LogKV(
			"user", user,
			"from", req.From.Time(),
			"through", req.Through.Time(),
			"matchers", syntax.MatchersString(matchers),
			"streams", merged.Streams,
			"chunks", merged.Chunks,
			"bytes", merged.Bytes,
			"entries", merged.Entries,
		)
	}

	return &merged, nil
}

func (i *Ingester) GetVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	user, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "volume"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(user)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil && req.Matchers != seriesvolume.MatchAny {
		return nil, err
	}

	type f func() (*logproto.VolumeResponse, error)
	jobs := []f{
		f(func() (*logproto.VolumeResponse, error) {
			return instance.GetVolume(ctx, req)
		}),
		f(func() (*logproto.VolumeResponse, error) {
			return i.store.Volume(ctx, user, req.From, req.Through, req.Limit, req.TargetLabels, req.AggregateBy, matchers...)
		}),
	}
	resps := make([]*logproto.VolumeResponse, len(jobs))

	if err := concurrency.ForEachJob(
		ctx,
		len(jobs),
		2,
		func(_ context.Context, idx int) error {
			res, err := jobs[idx]()
			resps[idx] = res
			return err
		},
	); err != nil {
		return nil, err
	}

	merged := seriesvolume.Merge(resps, req.Limit)
	return merged, nil
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

// Tail logs matching given query
func (i *Ingester) Tail(req *logproto.TailRequest, queryServer logproto.Querier_TailServer) error {
	err := i.tail(req, queryServer)
	err = server_util.ClientGrpcStatusAndError(err)
	return err
}

func (i *Ingester) tail(req *logproto.TailRequest, queryServer logproto.Querier_TailServer) error {
	select {
	case <-i.tailersQuit:
		return errors.New("Ingester is stopping")
	default:
	}

	if req.Plan == nil {
		parsed, err := syntax.ParseLogSelector(req.Query, true)
		if err != nil {
			return err
		}
		req.Plan = &plan.QueryPlan{
			AST: parsed,
		}
	}

	instanceID, err := tenant.TenantID(queryServer.Context())
	if err != nil {
		return err
	}

	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}

	expr, ok := req.Plan.AST.(syntax.LogSelectorExpr)
	if !ok {
		return fmt.Errorf("unsupported query expression: want (LogSelectorExpr), got (%T)", req.Plan.AST)
	}

	tailer, err := newTailer(instanceID, expr, queryServer, i.cfg.MaxDroppedStreams)
	if err != nil {
		return err
	}

	if err := instance.addNewTailer(queryServer.Context(), tailer); err != nil {
		return err
	}
	tailer.loop()
	return nil
}

// TailersCount returns count of active tail requests from a user
func (i *Ingester) TailersCount(ctx context.Context, _ *logproto.TailersCountRequest) (*logproto.TailersCountResponse, error) {
	tcr, err := i.tailersCount(ctx)
	err = server_util.ClientGrpcStatusAndError(err)
	return tcr, err
}

func (i *Ingester) tailersCount(ctx context.Context) (*logproto.TailersCountResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	resp := logproto.TailersCountResponse{}

	instance, ok := i.getInstanceByID(instanceID)
	if ok {
		resp.Count = instance.openTailersCount()
	}

	return &resp, nil
}

// buildStoreRequest returns a store request from an ingester request, returns nil if QueryStore is set to false in configuration.
// The request may be truncated due to QueryStoreMaxLookBackPeriod which limits the range of request to make sure
// we only query enough to not miss any data and not add too many duplicates by covering the whole time range in query.
func buildStoreRequest(cfg Config, start, end, now time.Time) (time.Time, time.Time, bool) {
	if !cfg.QueryStore {
		return time.Time{}, time.Time{}, false
	}
	start = adjustQueryStartTime(cfg.QueryStoreMaxLookBackPeriod, start, now)

	if start.After(end) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

func adjustQueryStartTime(maxLookBackPeriod time.Duration, start, now time.Time) time.Time {
	if maxLookBackPeriod > 0 {
		oldestStartTime := now.Add(-maxLookBackPeriod)
		if oldestStartTime.After(start) {
			return oldestStartTime
		}
	}
	return start
}

func (i *Ingester) GetDetectedFields(_ context.Context, r *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	return &logproto.DetectedFieldsResponse{
		Fields: []*logproto.DetectedField{
			{
				Label:       "foo",
				Type:        logproto.DetectedFieldString,
				Cardinality: 1,
			},
		},
		Limit: r.GetLimit(),
	}, nil
}

// GetDetectedLabels returns map of detected labels and unique values from this ingester
func (i *Ingester) GetDetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	lvr, err := i.getDetectedLabels(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return lvr, err
}

func (i *Ingester) getDetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Set profiling tags
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("path", "read", "type", "detectedLabels"))
	pprof.SetGoroutineLabels(ctx)

	instance, err := i.GetOrCreateInstance(userID)
	if err != nil {
		return nil, err
	}
	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}

	labelMap, err := instance.LabelsWithValues(ctx, req.Start, matchers...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*logproto.UniqueLabelValues)
	for label, values := range labelMap {
		var uniqueValues []string
		for v := range values {
			uniqueValues = append(uniqueValues, v)
		}

		result[label] = &logproto.UniqueLabelValues{Values: uniqueValues}
	}
	return &logproto.LabelToValuesResponse{Labels: result}, nil
}
