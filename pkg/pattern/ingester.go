package pattern

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc/health/grpc_health_v1"

	ring_client "github.com/grafana/dskit/ring/client"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/clientpool"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const readBatchSize = 1024

type Config struct {
	Enabled               bool                  `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester is enabled."`
	LifecyclerConfig      ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the pattern ingester will operate and where it will register for discovery."`
	ClientConfig          clientpool.Config     `yaml:"client_config,omitempty" doc:"description=Configures how the pattern ingester will connect to the ingesters."`
	ConcurrentFlushes     int                   `yaml:"concurrent_flushes"`
	FlushCheckPeriod      time.Duration         `yaml:"flush_check_period"`
	MaxClusters           int                   `yaml:"max_clusters,omitempty" doc:"description=The maximum number of detected pattern clusters that can be created by streams."`
	MaxEvictionRatio      float64               `yaml:"max_eviction_ratio,omitempty" doc:"description=The maximum eviction ratio of patterns per stream. Once that ratio is reached, the stream will throttled pattern detection."`
	MetricAggregation     aggregation.Config    `yaml:"metric_aggregation,omitempty" doc:"description=Configures the metric aggregation and storage behavior of the pattern ingester."`
	PatternPersistence    PersistenceConfig     `yaml:"pattern_persistence,omitempty" doc:"description=Configures how detected patterns are pushed back to Loki for persistence."`
	TeeConfig             TeeConfig             `yaml:"tee_config,omitempty" doc:"description=Configures the pattern tee which forwards requests to the pattern ingester."`
	ConnectionTimeout     time.Duration         `yaml:"connection_timeout"`
	MaxAllowedLineLength  int                   `yaml:"max_allowed_line_length,omitempty" doc:"description=The maximum length of log lines that can be used for pattern detection."`
	RetainFor             time.Duration         `yaml:"retain_for,omitempty" doc:"description=How long to retain patterns in the pattern ingester after they are pushed."`
	MaxChunkAge           time.Duration         `yaml:"max_chunk_age,omitempty" doc:"description=The maximum time span for a single pattern chunk."`
	PatternSampleInterval time.Duration         `yaml:"pattern_sample_interval,omitempty" doc:"description=The time resolution for pattern samples within chunks."`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("pattern-ingester.", fs, util_log.Logger)
	cfg.ClientConfig.RegisterFlags(fs)
	cfg.MetricAggregation.RegisterFlagsWithPrefix(fs, "pattern-ingester.metric-aggregation.")
	cfg.PatternPersistence.RegisterFlagsWithPrefix(fs, "pattern-ingester.pattern-persistence.")
	cfg.TeeConfig.RegisterFlags(fs, "pattern-ingester.")

	fs.BoolVar(
		&cfg.Enabled,
		"pattern-ingester.enabled",
		false,
		"Flag to enable or disable the usage of the pattern-ingester component.",
	)
	fs.IntVar(
		&cfg.ConcurrentFlushes,
		"pattern-ingester.concurrent-flushes",
		32,
		"How many flushes can happen concurrently from each stream.",
	)
	fs.DurationVar(
		&cfg.FlushCheckPeriod,
		"pattern-ingester.flush-check-period",
		1*time.Minute,
		"How often should the ingester see if there are any blocks to flush. The first flush check is delayed by a random time up to 0.8x the flush check period. Additionally, there is +/- 1% jitter added to the interval.",
	)
	fs.IntVar(
		&cfg.MaxClusters,
		"pattern-ingester.max-clusters",
		drain.DefaultConfig().MaxClusters,
		"The maximum number of detected pattern clusters that can be created by the pattern ingester.",
	)
	fs.Float64Var(
		&cfg.MaxEvictionRatio,
		"pattern-ingester.max-eviction-ratio",
		drain.DefaultConfig().MaxEvictionRatio,
		"The maximum eviction ratio of patterns per stream. Once that ratio is reached, the stream will be throttled for pattern detection.",
	)
	fs.DurationVar(
		&cfg.ConnectionTimeout,
		"pattern-ingester.connection-timeout",
		2*time.Second,
		"Timeout for connections between the Loki and the pattern ingester.",
	)
	fs.IntVar(
		&cfg.MaxAllowedLineLength,
		"pattern-ingester.max-allowed-line-length",
		drain.DefaultConfig().MaxAllowedLineLength,
		"The maximum length of log lines that can be used for pattern detection.",
	)
	fs.DurationVar(
		&cfg.RetainFor,
		"pattern-ingester.retain-for",
		3*time.Hour,
		"How long to retain patterns in the pattern ingester after they are pushed.",
	)
	fs.DurationVar(
		&cfg.MaxChunkAge,
		"pattern-ingester.max-chunk-age",
		1*time.Hour,
		"The maximum time span for a single pattern chunk.",
	)
	fs.DurationVar(
		&cfg.PatternSampleInterval,
		"pattern-ingester.sample-interval",
		10*time.Second,
		"The time resolution for pattern samples within chunks.",
	)
}

type TeeConfig struct {
	BatchSize          int           `yaml:"batch_size"`
	BatchFlushInterval time.Duration `yaml:"batch_flush_interval"`
	FlushQueueSize     int           `yaml:"flush_queue_size"`
	FlushWorkerCount   int           `yaml:"flush_worker_count"`
	StopFlushTimeout   time.Duration `yaml:"stop_flush_timeout"`
}

func (cfg *TeeConfig) RegisterFlags(f *flag.FlagSet, prefix string) {
	f.IntVar(
		&cfg.BatchSize,
		prefix+"tee.batch-size",
		5000,
		"The size of the batch of raw logs to send for template mining",
	)
	f.DurationVar(
		&cfg.BatchFlushInterval,
		prefix+"tee.batch-flush-interval",
		time.Second,
		"The max time between batches of raw logs to send for template mining",
	)
	f.IntVar(
		&cfg.FlushQueueSize,
		prefix+"tee.flush-queue-size",
		1000,
		"The number of log flushes to queue before dropping",
	)
	f.IntVar(
		&cfg.FlushWorkerCount,
		prefix+"tee.flush-worker-count",
		100,
		"the number of concurrent workers sending logs to the template service",
	)
	f.DurationVar(
		&cfg.StopFlushTimeout,
		prefix+"tee.stop-flush-timeout",
		30*time.Second,
		"The max time we will try to flush any remaining logs to be mined when the service is stopped",
	)
}

func (cfg *Config) Validate() error {
	if cfg.LifecyclerConfig.RingConfig.ReplicationFactor != 1 {
		return errors.New("pattern ingester replication factor must be 1")
	}

	// Validate retain-for >= chunk-duration
	if cfg.RetainFor < cfg.MaxChunkAge {
		return fmt.Errorf("retain-for (%v) must be greater than or equal to chunk-duration (%v)", cfg.RetainFor, cfg.MaxChunkAge)
	}

	// Validate chunk-duration >= sample-interval
	if cfg.MaxChunkAge < cfg.PatternSampleInterval {
		return fmt.Errorf("chunk-duration (%v) must be greater than or equal to sample-interval (%v)", cfg.MaxChunkAge, cfg.PatternSampleInterval)
	}

	return cfg.LifecyclerConfig.Validate()
}

type Limits interface {
	drain.Limits
	MetricAggregationEnabled(userID string) bool
	PatternPersistenceEnabled(userID string) bool
	PersistenceGranularity(userID string) time.Duration
	PatternRateThreshold(userID string) float64
}

type Ingester struct {
	services.Service
	lifecycler *ring.Lifecycler
	ringClient RingClient

	lifecyclerWatcher *services.FailureWatcher

	cfg        Config
	limits     Limits
	registerer prometheus.Registerer
	logger     log.Logger

	instancesMtx sync.RWMutex
	instances    map[string]*instance

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup
	loopDone        sync.WaitGroup
	loopQuit        chan struct{}

	metrics  *ingesterMetrics
	drainCfg *drain.Config
}

func New(
	cfg Config,
	limits Limits,
	ringClient RingClient,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Ingester, error) {
	metrics := newIngesterMetrics(registerer, metricsNamespace)
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	drainCfg := drain.DefaultConfig()
	drainCfg.MaxClusters = cfg.MaxClusters
	drainCfg.MaxEvictionRatio = cfg.MaxEvictionRatio
	drainCfg.MaxChunkAge = cfg.MaxChunkAge
	drainCfg.SampleInterval = cfg.PatternSampleInterval

	i := &Ingester{
		cfg:         cfg,
		limits:      limits,
		ringClient:  ringClient,
		logger:      log.With(logger, "component", "pattern-ingester"),
		registerer:  registerer,
		metrics:     metrics,
		instances:   make(map[string]*instance),
		flushQueues: make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		loopQuit:    make(chan struct{}),
		drainCfg:    drainCfg,
	}
	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	return i, nil
}

func (i *Ingester) getEffectivePersistenceGranularity(userID string) time.Duration {
	tenantGranularity := i.limits.PersistenceGranularity(userID)
	if tenantGranularity > 0 && tenantGranularity <= i.cfg.MaxChunkAge {
		return tenantGranularity
	}
	return i.cfg.MaxChunkAge
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *Ingester) starting(ctx context.Context) error {
	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}
	i.initFlushQueues()
	// start our loop
	i.loopDone.Add(1)
	go i.loop()
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

	close(i.loopQuit)
	i.loopDone.Wait()
	return serviceError
}

func (i *Ingester) stopping(_ error) error {
	err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}
	i.flushQueuesDone.Wait()

	// Flush all patterns before stopping writers to ensure patterns are persisted
	i.flushPatterns()

	i.stopWriters()
	return err
}

func (i *Ingester) loop() {
	defer i.loopDone.Done()

	// Delay the first flush operation by up to 0.8x the flush time period.
	// This will ensure that multiple ingesters started at the same time do not
	// flush at the same time. Flushing at the same time can cause concurrently
	// writing the same chunk to object storage, which in AWS S3 leads to being
	// rate limited.
	jitter := time.Duration(rand.Int63n(int64(float64(i.cfg.FlushCheckPeriod.Nanoseconds()) * 0.8))) //#nosec G404 -- Jitter does not require a CSPRNG -- nosemgrep: math-random-used
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

	downsampleTicker := time.NewTimer(i.cfg.MetricAggregation.SamplePeriod)
	defer downsampleTicker.Stop()
	for {
		select {
		case <-flushTicker.C:
			i.sweepUsers(false, true)
		case t := <-downsampleTicker.C:
			downsampleTicker.Reset(i.cfg.MetricAggregation.SamplePeriod)
			now := model.TimeFromUnixNano(t.UnixNano())
			i.downsampleMetrics(now)
		case <-i.loopQuit:
			return
		}
	}
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

func (i *Ingester) TransferOut(_ context.Context) error {
	// todo may be.
	return ring.ErrTransferDisabled
}

func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}
	return &logproto.PushResponse{}, instance.Push(ctx, req)
}

func (i *Ingester) Query(req *logproto.QueryPatternsRequest, stream logproto.Pattern_QueryServer) error {
	ctx := stream.Context()
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}
	iterator, err := instance.Iterator(ctx, req)
	if err != nil {
		return err
	}
	defer util.LogErrorWithContext(ctx, "closing iterator", iterator.Close)
	return sendPatternSample(ctx, iterator, stream)
}

func sendPatternSample(ctx context.Context, it iter.Iterator, stream logproto.Pattern_QueryServer) error {
	for ctx.Err() == nil {
		batch, err := iter.ReadBatch(it, readBatchSize)
		if err != nil {
			return err
		}
		if err := stream.Send(batch); err != nil && err != context.Canceled {
			return err
		}
		if len(batch.Series) == 0 {
			return nil
		}
	}
	return nil
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
		aggregationMetrics := aggregation.NewMetrics(i.registerer)

		var metricWriter aggregation.EntryWriter
		aggCfg := i.cfg.MetricAggregation
		if i.limits.MetricAggregationEnabled(instanceID) {
			metricWriter, err = aggregation.NewPush(
				aggCfg.LokiAddr,
				instanceID,
				aggCfg.WriteTimeout,
				aggCfg.PushPeriod,
				aggCfg.HTTPClientConfig,
				aggCfg.BasicAuth.Username,
				string(aggCfg.BasicAuth.Password),
				aggCfg.UseTLS,
				&aggCfg.BackoffConfig,
				log.With(i.logger, "writer", "metric-aggregation"),
				aggregationMetrics,
			)
			if err != nil {
				return nil, err
			}
		}

		var patternWriter aggregation.EntryWriter
		patternCfg := i.cfg.PatternPersistence
		if i.limits.PatternPersistenceEnabled(instanceID) {
			patternWriter, err = aggregation.NewPush(
				patternCfg.LokiAddr,
				instanceID,
				patternCfg.WriteTimeout,
				patternCfg.PushPeriod,
				patternCfg.HTTPClientConfig,
				patternCfg.BasicAuth.Username,
				string(patternCfg.BasicAuth.Password),
				patternCfg.UseTLS,
				&patternCfg.BackoffConfig,
				log.With(i.logger, "writer", "pattern"),
				aggregationMetrics,
			)
			if err != nil {
				return nil, err
			}
		}

		inst, err = newInstance(
			instanceID,
			i.logger,
			i.metrics,
			i.drainCfg,
			i.limits,
			i.ringClient,
			i.lifecycler.ID,
			metricWriter,
			patternWriter,
			aggregationMetrics,
		)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
	}
	return inst, nil
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

func (i *Ingester) stopWriters() {
	instances := i.getInstances()

	for _, instance := range instances {
		if instance.metricWriter != nil {
			instance.metricWriter.Stop()
		}
		if instance.patternWriter != nil {
			instance.patternWriter.Stop()
		}
	}
}

// flushPatterns flushes all patterns from all instances on shutdown.
func (i *Ingester) flushPatterns() {
	level.Info(i.logger).Log("msg", "flushing patterns on shutdown")
	instances := i.getInstances()

	for _, instance := range instances {
		if i.limits.PatternPersistenceEnabled(instance.instanceID) {
			instance.flushPatterns()
		}
	}
}

func (i *Ingester) downsampleMetrics(ts model.Time) {
	instances := i.getInstances()

	for _, instance := range instances {
		if i.limits.MetricAggregationEnabled(instance.instanceID) {
			instance.Downsample(ts)
		}
	}
}
