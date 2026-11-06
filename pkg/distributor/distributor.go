package distributor

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/trace"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc/codes"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	kafka_client "github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits"
	limits_frontend "github.com/grafana/loki/v3/pkg/limits/frontend"
	limits_frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/loghttp/push/otlpattrs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	ringKey = "distributor"

	ringAutoForgetUnhealthyPeriods = 2
)

var (
	maxLabelCacheSize = 100000
	rfStats           = analytics.NewInt("distributor_replication_factor")

	// the rune error replacement is rejected by Prometheus hence replacing them with space.
	removeInvalidUtf = func(r rune) rune {
		if r == utf8.RuneError {
			return 32 // rune value for space
		}
		return r
	}
)

// Config for a Distributor.
type Config struct {
	// Distributors ring
	DistributorRing RingConfig `yaml:"ring,omitempty"`
	PushWorkerCount int        `yaml:"push_worker_count"`

	// Request parser
	MaxInflightBytes int `yaml:"max_inflight_bytes"`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`

	// RateStore customizes the rate storing used by stream sharding.
	RateStore RateStoreConfig `yaml:"rate_store"`

	// WriteFailuresLoggingCfg customizes write failures logging behavior.
	WriteFailuresLogging writefailures.Cfg `yaml:"write_failures_logging" doc:"description=Customize the logging of write failures."`

	// OTLPAttributeLogging configures the sampled logging of OTLP attribute expansion
	// which is enabled per tenant with the log_otlp_attribute_expansion runtime config.
	OTLPAttributeLogging otlpattrs.Cfg `yaml:"otlp_attribute_logging" doc:"description=Customize the logging of OTLP attribute expansion."`

	OTLPConfig push.GlobalOTLPConfig `yaml:"otlp_config"`

	// DefaultPolicyStreamMappings contains the default policy stream mappings that are merged with per-tenant mappings.
	DefaultPolicyStreamMappings validation.PolicyStreamMapping `yaml:"default_policy_stream_mappings" doc:"description=Default policy stream mappings that are merged with per-tenant mappings."`

	KafkaEnabled              bool `yaml:"kafka_writes_enabled"`
	IngesterEnabled           bool `yaml:"ingester_writes_enabled"`
	IngestLimitsEnabled       bool `yaml:"ingest_limits_enabled"`
	IngestLimitsDryRunEnabled bool `yaml:"ingest_limits_dry_run_enabled"`

	KafkaConfig kafka.Config `yaml:"-"`

	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.OTLPConfig.RegisterFlags(fs)
	cfg.DistributorRing.RegisterFlags(fs)
	cfg.CircuitBreaker.RegisterFlags(fs)
	cfg.RateStore.RegisterFlagsWithPrefix("distributor.rate-store", fs)
	cfg.WriteFailuresLogging.RegisterFlagsWithPrefix("distributor.write-failures-logging", fs)
	cfg.OTLPAttributeLogging.RegisterFlagsWithPrefix("distributor.otlp-attribute-logging", fs)
	fs.IntVar(&cfg.MaxInflightBytes, "distributor.max-inflight-bytes", 0, "The maximum number of inflight bytes at a time. 0 means disabled.")
	fs.IntVar(&cfg.PushWorkerCount, "distributor.push-worker-count", 256, "Number of workers to push batches to ingesters.")
	fs.BoolVar(&cfg.KafkaEnabled, "distributor.kafka-writes-enabled", false, "Enable writes to Kafka during Push requests.")
	fs.BoolVar(&cfg.IngesterEnabled, "distributor.ingester-writes-enabled", true, "Enable writes to Ingesters during Push requests. Defaults to true.")
	fs.BoolVar(&cfg.IngestLimitsEnabled, "distributor.ingest-limits-enabled", false, "Enable checking limits against the ingest-limits service. Defaults to false.")
	fs.BoolVar(&cfg.IngestLimitsDryRunEnabled, "distributor.ingest-limits-dry-run-enabled", false, "Enable dry-run mode where limits are checked the ingest-limits service, but not enforced. Defaults to false.")
}

func (cfg *Config) Validate() error {
	if !cfg.KafkaEnabled && !cfg.IngesterEnabled {
		return errors.New("at least one of kafka and ingestor writes must be enabled")
	}
	if err := cfg.CircuitBreaker.Validate(); err != nil {
		return err
	}
	if cfg.MaxInflightBytes < 0 {
		return errors.New("max inflight bytes cannot be less than zero")
	}
	return nil
}

type CircuitBreakerConfig struct {
	Enabled         bool          `yaml:"enabled"`
	OpenPeriod      time.Duration `yaml:"open_period"`
	MinFailures     int           `yaml:"min_failures"`
	PermittedTrials int           `yaml:"permitted_trials"`
}

func (cfg *CircuitBreakerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "distributor.circuit-breaker.enabled", false, "Enable circuit breakers.")
	f.DurationVar(&cfg.OpenPeriod, "distributor.circuit-breaker.open-period", time.Second, "The open period.")
	f.IntVar(&cfg.MinFailures, "distributor.circuit-breaker.min-failures", 1, "The minimum number of successive failures required to open the circuit breaker.")
	f.IntVar(&cfg.PermittedTrials, "distributor.circuit-breaker.permitted-trials", 1, "The number of permitted trial requests in the half-open state. All requests must succeed to close the circuit breaker, any failure re-opens it.")
}

func (cfg *CircuitBreakerConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.OpenPeriod <= 0 {
		return errors.New("the open period must be a positive duration")
	}
	if cfg.MinFailures < 1 {
		return errors.New("the minimum number of failures must be at least 1")
	}
	if cfg.PermittedTrials < 1 {
		return errors.New("the permitted number of trials must be at least 1")
	}
	return nil
}

// RateStore manages the ingestion rate of streams, populated by data fetched from ingesters.
type RateStore interface {
	RateFor(tenantID string, streamHash uint64) (int64, float64)
}

type KafkaProducer interface {
	ProduceSync(ctx context.Context, records []*kgo.Record) kgo.ProduceResults
	Close()
}

// Distributor coordinates replicates and distribution of log streams.
type Distributor struct {
	services.Service

	cfg              Config
	ingesterCfg      ingester.Config
	logger           log.Logger
	m                *metrics
	clientCfg        ingester_client.Config
	tenantConfigs    *runtime.TenantConfigs
	tenantsRetention *retention.TenantsRetention
	ingestersRing    ring.ReadRing
	validator        *Validator
	ingesterClients  *ring_client.Pool
	tee              Tee

	rateStore    RateStore
	shardTracker *ShardTracker

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances.
	distributorsLifecycler *ring.BasicLifecycler
	distributorsRing       *ring.Ring
	healthyInstancesCount  *atomic.Uint32

	rateLimitStrat string

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	// Per-user rate limiter.
	ingestionRateLimiter *limiter.RateLimiter
	labelCache           *lru.Cache[string, labelData]

	// Push failures rate limiter.
	writeFailuresManager *writefailures.Manager

	// Rate limited reporter for OTLP resource and scope attribute expansion.
	otlpAttrReporter *otlpattrs.Reporter

	usageTracker   push.UsageTracker
	ingesterTasks  chan pushIngesterTask
	ingesterTaskWg sync.WaitGroup

	// Will succeed usage tracker in future.
	ingestLimits *ingestLimits

	// kafka
	kafkaWriter   KafkaProducer
	partitionRing ring.PartitionRingReader

	// The number of partitions for the stream metadata topic. Unlike stream
	// records, where entries are sharded over just the active partitions,
	// stream metadata is sharded over all partitions, and all partitions
	// are consumed.
	numMetadataPartitions int

	inflightBytes  atomic.Int64
	circuitBreaker circuitBreaker
}

// New a distributor creates.
func New(
	cfg Config,
	ingesterCfg ingester.Config,
	clientCfg ingester_client.Config,
	configs *runtime.TenantConfigs,
	ingestersRing ring.ReadRing,
	partitionRing ring.PartitionRingReader,
	overrides Limits,
	registerer prometheus.Registerer,
	metricsNamespace string,
	tee Tee,
	usageTracker push.UsageTracker,
	limitsFrontendCfg limits_frontend_client.Config,
	limitsFrontendRing ring.ReadRing,
	numMetadataPartitions int,
	logger log.Logger,
) (*Distributor, error) {
	ingesterClientFactory := cfg.factory
	if ingesterClientFactory == nil {
		ingesterClientFactory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return ingester_client.New(clientCfg, addr)
		})
	}

	internalIngesterClientFactory := func(addr string) (ring_client.PoolClient, error) {
		internalCfg := clientCfg
		internalCfg.Internal = true
		return ingester_client.New(internalCfg, addr)
	}

	validator, err := NewValidator(overrides, usageTracker)
	if err != nil {
		return nil, err
	}

	limitsFrontendClientFactory := limits_frontend_client.NewPoolFactory(limitsFrontendCfg)
	limitsFrontendClientPool := limits_frontend_client.NewPool(
		limits_frontend.RingName,
		limitsFrontendCfg.PoolConfig,
		limitsFrontendRing,
		limitsFrontendClientFactory,
		logger,
	)
	limitsFrontendClient := newIngestLimitsFrontendRingClient(
		limitsFrontendRing,
		limitsFrontendClientPool,
		limitsFrontendCfg.ShuffleShardEnabled,
		limitsFrontendCfg.ShuffleShardSize,
	)

	// Create the configured ingestion rate limit strategy (local or global).
	var ingestionRateStrategy limiter.RateLimiterStrategy
	var distributorsLifecycler *ring.BasicLifecycler
	var distributorsRing *ring.Ring

	var servs []services.Service

	rateLimitStrat := validation.LocalIngestionRateStrategy
	labelCache, err := lru.New[string, labelData](maxLabelCacheSize)
	if err != nil {
		return nil, err
	}

	if partitionRing == nil && cfg.KafkaEnabled {
		return nil, fmt.Errorf("partition ring is required for kafka writes")
	}

	ingestLimits := newIngestLimits(limitsFrontendClient, registerer)

	var kafkaWriter KafkaProducer
	if cfg.KafkaEnabled {
		kafkaClient, err := kafka_client.NewWriterClient("distributor", cfg.KafkaConfig, logger, registerer)
		if err != nil {
			return nil, fmt.Errorf("failed to start kafka client: %w", err)
		}
		kafkaWriter = kafka_client.NewProducer("distributor", kafkaClient, cfg.KafkaConfig.ProducerMaxBufferedBytes,
			prometheus.WrapRegistererWithPrefix("loki_", registerer),
			kafka_client.WithRecordsInterceptor(validation.IngestionPoliciesKafkaProducerInterceptor),
		)
	}

	d := &Distributor{
		cfg:                   cfg,
		ingesterCfg:           ingesterCfg,
		logger:                logger,
		clientCfg:             clientCfg,
		tenantConfigs:         configs,
		tenantsRetention:      retention.NewTenantsRetention(overrides),
		ingestersRing:         ingestersRing,
		validator:             validator,
		ingesterClients:       clientpool.NewPool("ingester", clientCfg.PoolConfig, ingestersRing, ingesterClientFactory, logger, metricsNamespace),
		labelCache:            labelCache,
		shardTracker:          NewShardTracker(),
		healthyInstancesCount: atomic.NewUint32(0),
		rateLimitStrat:        rateLimitStrat,
		tee:                   tee,
		usageTracker:          usageTracker,
		ingesterTasks:         make(chan pushIngesterTask),
		m:                     newMetrics(registerer),
		writeFailuresManager:  writefailures.NewManager(logger, registerer, cfg.WriteFailuresLogging, configs, "distributor"),
		otlpAttrReporter:      otlpattrs.NewReporter(cfg.OTLPAttributeLogging, configs),
		kafkaWriter:           kafkaWriter,
		partitionRing:         partitionRing,
		ingestLimits:          ingestLimits,
		numMetadataPartitions: numMetadataPartitions,
	}

	if cfg.CircuitBreaker.Enabled {
		circuitBreaker := newTrialCircuitBreaker(
			cfg.CircuitBreaker.OpenPeriod,
			cfg.CircuitBreaker.MinFailures,
			cfg.CircuitBreaker.PermittedTrials,
			func(err error) bool {
				return errors.Is(err, kgo.ErrMaxBuffered) || errors.Is(err, errServiceUnavailableMaxLoad)
			},
		)
		registerer.MustRegister(circuitBreaker)
		d.circuitBreaker = circuitBreaker
	}

	if overrides.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		d.rateLimitStrat = validation.GlobalIngestionRateStrategy

		distributorsRing, distributorsLifecycler, err = newRingAndLifecycler(cfg.DistributorRing, d.healthyInstancesCount, logger, registerer, metricsNamespace)
		if err != nil {
			return nil, err
		}

		servs = append(servs, distributorsLifecycler, distributorsRing)

		ingestionRateStrategy = newGlobalIngestionRateStrategy(overrides, d)
	} else {
		ingestionRateStrategy = newLocalIngestionRateStrategy(overrides)
	}

	d.ingestionRateLimiter = limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second)
	d.distributorsRing = distributorsRing
	d.distributorsLifecycler = distributorsLifecycler

	d.m.replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	rfStats.Set(int64(ingestersRing.ReplicationFactor()))

	d.m.maxInflightBytes.Set(float64(d.cfg.MaxInflightBytes))

	rs := NewRateStore(
		d.cfg.RateStore,
		ingestersRing,
		clientpool.NewPool(
			"rate-store",
			clientCfg.PoolConfig,
			ingestersRing,
			ring_client.PoolAddrFunc(internalIngesterClientFactory),
			logger,
			metricsNamespace,
		),
		overrides,
		registerer,
	)
	d.rateStore = rs

	servs = append(servs, d.ingesterClients, rs)
	d.subservices, err = services.NewManager(servs...)
	if err != nil {
		return nil, errors.Wrap(err, "services manager")
	}
	d.subservicesWatcher = services.NewFailureWatcher()
	d.subservicesWatcher.WatchManager(d.subservices)
	d.Service = services.NewBasicService(d.starting, d.running, d.stopping)

	return d, nil
}

func (d *Distributor) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, d.subservices)
}

func (d *Distributor) running(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		d.ingesterTaskWg.Wait()
	}()
	if d.cfg.IngesterEnabled {
		// Spawn workers if ingesters are enabled.
		d.ingesterTaskWg.Add(d.cfg.PushWorkerCount)
		for i := 0; i < d.cfg.PushWorkerCount; i++ {
			go d.pushIngesterWorker(ctx)
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-d.subservicesWatcher.Chan():
		return errors.Wrap(err, "distributor subservice failed")
	}
}

func (d *Distributor) stopping(_ error) error {
	if d.kafkaWriter != nil {
		d.kafkaWriter.Close()
	}
	return services.StopManagerAndAwaitStopped(context.Background(), d.subservices)
}

type KeyedStream struct {
	HashKey        uint32
	HashKeyNoShard uint64
	Stream         logproto.InternalStreamAdapter
	Policy         string
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
type streamTracker struct {
	KeyedStream
	minSuccess  int
	maxFailures int
	succeeded   atomic.Int32
	failed      atomic.Int32
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
type PushTracker struct {
	streamsPending atomic.Int32
	streamsFailed  atomic.Int32
	done           chan struct{}
	err            chan error
}

// doneWithResult records the result of a stream push.
// If err is nil, the stream push is considered successful.
// If err is not nil, the stream push is considered failed.
func (p *PushTracker) doneWithResult(err error) {
	if err == nil {
		if p.streamsPending.Dec() == 0 {
			p.done <- struct{}{}
		}
	} else {
		if p.streamsFailed.Inc() == 1 {
			p.err <- err
		}
	}
}

func (d *Distributor) waitSimulatedLatency(ctx context.Context, tenantID string, start time.Time) {
	latency := d.validator.SimulatedPushLatency(tenantID)
	if latency > 0 {
		// All requests must wait at least the simulated latency. However,
		// we want to avoid adding additional latency on top of slow requests
		// that already took longer then the simulated latency.
		wait := latency - time.Since(start)
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return // The client canceled the request.
			}
		}
	}
}

func (d *Distributor) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	internal := logproto.FromPushRequest(req)
	return d.pushWithResolver(ctx, internal, newRequestScopedStreamResolver(tenantID, d.validator.Limits, d.logger), constants.Loki)
}

// pushWithResolver validates req, enforces the ingestion limits on what is left, and forwards the
// accepted streams. It modifies req in place: validation drops the rejected streams from
// req.Streams and edits the entries of the rest, and time sharding sorts them.
//
// Rejections and the errors they return, in order of precedence:
//   - An error normalizing structured metadata aborts the whole push.
//   - The ingestion rate limit is all-or-nothing: an exceeded bucket rejects the whole push with a
//     429, and any validation error is dropped.
//   - Streams over the limits service's stream limit are dropped with a 429.
//   - Streams that fail validation are dropped with a 400.
//   - Streams of a blocked tenant or policy are dropped with the block status code, or with no
//     error for 200.
//
// For the other rejections, the rest of the push is still written and the error is returned with
// the response. The push is rejected with the error instead when nothing is left to write, or
// when a stream of the push resolves to a policy with reject_partial_writes and the error is a
// 429.
//
// A rejected push, with nothing written, always returns a nil response with its error. A non-nil
// response means the push was written, in part if the error is non-nil.
func (d *Distributor) pushWithResolver(ctx context.Context, req *logproto.InternalPushRequest, streamResolver *requestScopedStreamResolver, format string) (*logproto.PushResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	defer d.waitSimulatedLatency(ctx, tenantID, start)

	if len(req.Streams) == 0 {
		d.m.zeroStreamCount.WithLabelValues(tenantID, "pre-validation").Inc()
		return nil, httpgrpc.Errorf(http.StatusUnprocessableEntity, validation.MissingStreamsErrorMsg)
	}

	now := time.Now()
	validationContext := d.validator.getValidationContextForTime(now, tenantID)

	// Ingestion rate limiting is bucketed by the effective rate-limit target: streams whose
	// resolved policy has a per-policy ingestion rate override are metered against their own
	// per-(tenant,policy) bucket, replacing the tenant-wide limit; all other streams share the
	// tenant-wide bucket (keyed by "").
	rlBuckets := map[string]*rateLimitBucket{}

	sharder := newStreamSharder(d, tenantID, now, len(req.Streams))
	validated, err := d.validateStreams(ctx, req, validationContext, now, streamResolver, format, &sharder, rlBuckets)
	if err != nil {
		return nil, err
	}
	sharder.warnMixedShardStreams()

	// Every stream was empty, rejected or blocked. Live-mode candidates are
	// still to be written: they only lack their shard count.
	if len(sharder.streams) == 0 && len(sharder.liveCandidates) == 0 {
		d.m.zeroStreamCount.WithLabelValues(tenantID, "post-validation").Inc()
		// Nothing was left to write. Without an error, for example when every
		// stream was empty or blocked with status code 200, that is a success.
		if validated.err != nil {
			return nil, validated.err
		}
		return &logproto.PushResponse{}, nil
	}

	// An admitted push keeps its tokens even if a later stage rejects it.
	if err := d.enforceIngestionRateLimits(ctx, now, tenantID, rlBuckets, req.Streams, validationContext, streamResolver, format); err != nil {
		return nil, err
	}

	// These limits are checked after the ingestion rate limit as this
	// is how it works in ingesters.
	streams, limitsErr := d.enforceStreamLimits(ctx, tenantID, &sharder, validationContext, streamResolver, format)
	if limitsErr != nil {
		limitsErr = httpgrpc.Error(http.StatusTooManyRequests, limitsErr.Error())
	}

	// A stream-limit 429 takes precedence over validation and blocked errors.
	finalErr := cmp.Or(limitsErr, validated.err)
	if finalErr != nil {
		// Nothing is left to write: the limits service rejected every
		// stream that passed validation.
		if len(streams) == 0 {
			return nil, finalErr
		}

		// Reject the whole push with a 429 when a policy asks for it,
		// instead of partially writing and sending a 429. The rejected push
		// still consumes its rate-limit tokens and the limits service's
		// stream slots.
		if validated.rejectPartialWrites && (limitsErr != nil || isTooManyRequests(validated.err)) {
			d.m.rejectedPartialWrites.WithLabelValues(tenantID, validated.rejectPartialWritesPolicy).Inc()
			return nil, finalErr
		}
	}

	tracker := PushTracker{
		done: make(chan struct{}, 1), // buffer avoids blocking if caller terminates - doneWithResult only sends once on each
		err:  make(chan error, 1),
	}

	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.InstanceDesc

	streamsToWrite := 0
	if d.cfg.IngesterEnabled {
		streamsToWrite += len(streams)
	}
	if d.cfg.KafkaEnabled {
		// When Kafka is enabled, we track just one stream, instead of len(streams),
		// as all streams are written to Kafka at once.
		streamsToWrite++
	}

	// We must correctly set streamsPending before beginning any writes to ensure we don't have a race between finishing all of one path before starting the other.
	if d.tee != nil {
		// Call register for the tee to allow them to register their pending streams.
		d.tee.Register(ctx, tenantID, streams, &tracker)
	}
	tracker.streamsPending.Add(int32(streamsToWrite))

	// Nil check for performance reasons, to avoid dynamic lookup and/or no-op
	// function calls that cannot be inlined.
	if d.tee != nil {
		d.tee.Duplicate(context.WithoutCancel(ctx), tenantID, streams, &tracker)
	}

	if d.cfg.KafkaEnabled {
		subring := d.partitionRing.PartitionRing()
		shardSize := d.validator.IngestionPartitionsTenantShardSize(tenantID)
		// Do not shuffle shard if the shard size is 0. When the size is 0, it
		// creates a shuffle shard which contains the complete set of partitions,
		// which is the same as not shuffle sharding at all.
		if shardSize > 0 {
			subring, err = subring.ShuffleShard(tenantID, shardSize)
			if err != nil {
				return nil, err
			}
		}
		// Unlike the ingester writes, this write uses ctx, so it stops when ctx is canceled,
		// including after the push returned early on another write's failure.
		go d.sendStreamsToKafka(ctx, tenantID, streams, &tracker, subring)
	}

	if d.cfg.IngesterEnabled {
		streamTrackers := make([]streamTracker, len(streams))
		streamsByIngester := map[string][]*streamTracker{}
		ingesterDescs := map[string]ring.InstanceDesc{}

		if err := func() error {
			sp := trace.SpanFromContext(ctx)
			sp.AddEvent("started to query ingesters ring")
			defer sp.AddEvent("finished to query ingesters ring")

			for i, stream := range streams {
				replicationSet, err := d.ingestersRing.Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
				if err != nil {
					return err
				}

				streamTrackers[i] = streamTracker{
					KeyedStream: stream,
					minSuccess:  len(replicationSet.Instances) - replicationSet.MaxErrors,
					maxFailures: replicationSet.MaxErrors,
				}
				for _, ingester := range replicationSet.Instances {
					streamsByIngester[ingester.Addr] = append(streamsByIngester[ingester.Addr], &streamTrackers[i])
					ingesterDescs[ingester.Addr] = ingester
				}
			}
			return nil
		}(); err != nil {
			return nil, err
		}

		for ingester, streams := range streamsByIngester {
			func(ingester ring.InstanceDesc, samples []*streamTracker) {
				// Clone the context using WithoutCancel, which is not canceled when parent is canceled.
				// This is to make sure all ingesters get samples even if we return early
				localCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.clientCfg.RemoteTimeout)
				sp := trace.SpanFromContext(ctx)
				localCtx = trace.ContextWithSpan(localCtx, sp)

				select {
				case <-ctx.Done():
					cancel()
					return
				case d.ingesterTasks <- pushIngesterTask{
					ingester:      ingester,
					streamTracker: samples,
					pushTracker:   &tracker,
					ctx:           localCtx,
					cancel:        cancel,
				}:
					return
				}
			}(ingesterDescs[ingester], streams)
		}
	}

	select {
	case err := <-tracker.err:
		return nil, err
	case <-tracker.done:
		return &logproto.PushResponse{}, finalErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// validatedPush is what validateStreams leaves for the rest of the push, besides the streams it
// handed to the sharder and the rate-limit buckets it filled.
type validatedPush struct {
	// err is the error for the client from validation: a 400 naming the streams and entries that
	// failed validation or, failing that, the status code of a blocked stream. It is nil if neither
	// happened, which includes every blocked stream having status code 200.
	err error

	// rejectPartialWrites is whether any stream of the push with entries resolved to a policy
	// configured with reject_partial_writes, accepted or not. rejectPartialWritesPolicy is the first
	// such policy. Such pushes are rejected as a whole on a 429 instead of being partially written.
	rejectPartialWrites       bool
	rejectPartialWritesPolicy string
}

// validateStreams validates the streams of req, hands the accepted ones to sharder, and adds their
// bytes and lines to their bucket in rlBuckets. A stream is dropped if it has no entries, fails
// label validation, misses enforced labels, is blocked, or has no entries left once they are
// validated. req.Streams is left with the accepted streams, so that rate-limit discards exclude the
// streams rejected here.
//
// The returned error aborts the push. Validation failures are in validatedPush.err instead. The
// streams accepted before an aborting error have already been sharded, with the side effects
// described on streamSharder.
func (d *Distributor) validateStreams(
	ctx context.Context,
	req *logproto.InternalPushRequest,
	vCtx validationContext,
	now time.Time,
	streamResolver push.StreamResolver,
	format string,
	sharder *streamSharder,
	rlBuckets map[string]*rateLimitBucket,
) (validatedPush, error) {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("start to validate request")
	defer sp.AddEvent("finished to validate request")

	tenantID := vCtx.userID
	fieldDetector := newFieldDetector(vCtx)
	var (
		result                validatedPush
		validationErrors      util.GroupedErrors
		ingestionBlockedError error
	)

	validatedStreams := req.Streams[:0]
	for _, stream := range req.Streams {
		if stream.EntryCount() == 0 {
			continue
		}

		// Truncate first so subsequent steps have consistent line lengths
		d.truncateLines(vCtx, stream)

		parsed, labelsReason, err := d.parseStreamLabels(ctx, vCtx, stream.Labels, stream, streamResolver)
		stream.Labels, stream.Hash = parsed.labelsString, parsed.hash
		if !result.rejectPartialWrites && d.validator.PolicyRejectPartialWrites(tenantID, parsed.policy) {
			result.rejectPartialWrites = true
			result.rejectPartialWritesPolicy = parsed.policy
		}

		// discard logs err to the insight logs and reports the whole stream
		// as discarded for reason.
		discard := func(reason string, err error) {
			d.writeFailuresManager.Log(tenantID, err)
			d.validator.reportDiscardedData(ctx, reason, vCtx, parsed.lbs, parsed.retentionHours, parsed.policy, nestedStreamSize(stream), stream.EntryCount(), format)
		}

		if err != nil {
			validationErrors.Add(err)
			discard(labelsReason, err)
			continue
		}

		if !constants.IsInternalStream(parsed.lbs.Has) {
			if missing, lbsMissing := d.missingEnforcedLabels(parsed.lbs, tenantID, parsed.policy); missing {
				err := fmt.Errorf(validation.MissingEnforcedLabelsErrorMsg, strings.Join(lbsMissing, ","), tenantID, stream.Labels, parsed.policy)
				validationErrors.Add(err)
				discard(validation.MissingEnforcedLabels, err)
				continue
			}
		}

		if block, statusCode, reason, err := d.validator.ShouldBlockIngestion(vCtx, now, parsed.policy); block {
			discard(reason, err)

			// If the status code is 200, return no error.
			// Note that we still log the error and increment the metrics.
			if statusCode == http.StatusOK {
				continue
			}

			// return an error but do not add it to validationErrors
			// otherwise client will get a 400 and will log it.
			ingestionBlockedError = httpgrpc.Errorf(statusCode, "%s", err.Error())
			continue
		}

		stats, err := d.processStreamEntries(
			ctx, vCtx, &stream, parsed.lbs, parsed.retentionHours, parsed.policy, format,
			fieldDetector, &validationErrors,
		)
		if err != nil {
			return validatedPush{}, err
		}
		if stats.entriesKept == 0 {
			continue
		}
		validatedStreams = append(validatedStreams, stream)

		// Attribute this stream's bytes/lines to its rate-limit bucket.
		_, hasRateOverride := d.validator.PolicyIngestionRateBytes(tenantID, parsed.policy)
		bucketKey := ""
		if hasRateOverride {
			bucketKey = parsed.policy
		}
		b := rlBuckets[bucketKey]
		if b == nil {
			b = &rateLimitBucket{policy: parsed.policy, hasOverride: hasRateOverride}
			rlBuckets[bucketKey] = b
		}
		b.bytes += stats.unexpandedSize
		b.lines += stats.entriesKept

		shardCfg, _ := d.validator.PolicyShardStreams(tenantID, parsed.policy)
		sharder.shard(stream, parsed.lbs, stats.expandedSize, parsed.policy, shardCfg)
	}
	// Rate-limit discards must exclude streams already rejected by validation.
	clear(req.Streams[len(validatedStreams):])
	req.Streams = validatedStreams

	if validationErrors.Err() != nil {
		result.err = httpgrpc.Errorf(http.StatusBadRequest, "%s", validationErrors.Error())
	} else if ingestionBlockedError != nil {
		// Any validation error takes precedence over the status code and error message for blocked ingestion.
		result.err = ingestionBlockedError
	}
	return result, nil
}

// enforceStreamLimits enforces the limits service's stream limits on the streams sharder collected
// and returns the streams to write. Live-mode candidates are sharded with the count the service
// returns, and shadow-mode candidates are compared against its answer, see streamSharder. Nothing is
// enforced while the ingest limits are disabled.
//
// Streams over the stream limit are dropped and reported as discarded, and the returned error names
// one of them. It is the only error returned: when the limits service fails, the limits fail open.
// A live-mode rejection takes precedence over one from EnforceLimits, so that its stream is the one
// named in the error.
func (d *Distributor) enforceStreamLimits(
	ctx context.Context,
	tenantID string,
	sharder *streamSharder,
	vCtx validationContext,
	streamResolver push.StreamResolver,
	format string,
) ([]KeyedStream, error) {
	streams := sharder.streams
	// Candidates are only collected while the ingest limits are enabled, see
	// streamSharder.shardByRate.
	if !d.cfg.IngestLimitsEnabled {
		return streams, nil
	}

	// Live-mode streams are sharded with the count the limits service returns,
	// which also decides whether they are written at all. They join streams
	// only after EnforceLimits has checked the remaining streams, as
	// CheckLimitsAndShard has already checked them.
	var (
		liveStreams  []KeyedStream
		liveRejected []logproto.InternalStreamAdapter
	)
	if len(sharder.liveCandidates) > 0 {
		shardCounts := d.limitsServiceShardCounts(ctx, tenantID, shardstreams.LimitsServiceStreamShardingModeLive, sharder.liveCandidates)
		for i, c := range sharder.liveCandidates {
			count := shardCounts[i]
			if count == 0 {
				// The limits service has no stream budget left for this
				// stream, so it is dropped. The rest of the push is still
				// written, unless reject_partial_writes rejects all of it. In
				// dry-run mode the rejection is recorded by the limits service
				// but not acted on here.
				if !d.cfg.IngestLimitsDryRunEnabled {
					liveRejected = append(liveRejected, c.stream)
					continue
				}
				count = 1
			}
			liveStreams = append(liveStreams, d.shardStreamToCount(c.stream, c.labels, tenantID, c.policy, c.shardStreamsCfg, count)...)
		}
	}

	if len(sharder.shadowCandidates) > 0 {
		// The returned counts are discarded: these streams were already
		// sharded with the local rate store's count.
		d.limitsServiceShardCounts(ctx, tenantID, shardstreams.LimitsServiceStreamShardingModeShadow, sharder.shadowCandidates)
	}

	var limitsErr error
	// streams holds every stream but the live-mode ones, so shadow-mode
	// streams are enforced as before. It is empty when the push consists of
	// live-mode streams only.
	if len(streams) > 0 {
		enforceTimer := prometheus.NewTimer(d.m.limitsServiceExceedsLimitsDuration)
		accepted, rejected, err := d.ingestLimits.EnforceLimits(ctx, tenantID, streams)
		enforceTimer.ObserveDuration()
		// When EnforceLimits fails, every stream is written: the limits fail
		// open. Even when it rejects every stream, do not return yet, as the
		// live-mode rejections below still need reporting.
		if err == nil && !d.cfg.IngestLimitsDryRunEnabled {
			if len(rejected) > 0 {
				discarded := make([]logproto.InternalStreamAdapter, 0, len(rejected))
				for _, stream := range rejected {
					discarded = append(discarded, stream.Stream)
				}
				limitsErr = d.rejectOverStreamLimit(ctx, tenantID, discarded, vCtx, streamResolver, format)
			}
			streams = accepted
		}
	}

	streams = append(streams, liveStreams...)
	if len(liveRejected) > 0 {
		limitsErr = d.rejectOverStreamLimit(ctx, tenantID, liveRejected, vCtx, streamResolver, format)
	}
	return streams, limitsErr
}

// rejectOverStreamLimit reports streams the limits service rejected for the stream limit as
// discarded, see trackDiscardedData, and returns an error naming the first of them. rejected must
// not be empty.
func (d *Distributor) rejectOverStreamLimit(
	ctx context.Context,
	tenantID string,
	rejected []logproto.InternalStreamAdapter,
	vCtx validationContext,
	streamResolver push.StreamResolver,
	format string,
) error {
	d.reportDiscardedStreams(ctx, rejected, vCtx, validation.StreamLimit, streamResolver, format)

	// Only the first stream is named, in the insight log and the error. Any stream can hit the
	// limit, so the name rarely points at the cause.
	err := fmt.Errorf(validation.StreamLimitErrorMsg, rejected[0].Labels, tenantID)
	d.writeFailuresManager.Log(tenantID, err)
	return err
}

// isTooManyRequests reports whether err carries an HTTP 429 status.
func isTooManyRequests(err error) bool {
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	return ok && resp.Code == http.StatusTooManyRequests
}

// processStreamEntries validates and enriches entries, then removes empty scopes and resources.
// Entry validation errors are collected; metadata normalization errors abort processing.
func (d *Distributor) processStreamEntries(
	ctx context.Context,
	vContext validationContext,
	stream *logproto.InternalStreamAdapter,
	lbs labels.Labels,
	retentionHours, policy, format string,
	fieldDetector *FieldDetector,
	validationErrors *util.GroupedErrors,
) (streamStats, error) {
	var entriesKept, unexpandedSize, expandedSize int
	tenantID := vContext.userID
	shouldDiscoverLevels := fieldDetector.shouldDiscoverLogLevels()
	var duplicateTimestamps duplicateTimestampTracker

	// The push parsers reserve the backfill label for X-Loki-Backfill-Shard.
	// Backfills bypass the age limit, but future timestamps are still rejected.
	if lbs.Has(constants.BackfillLabel) {
		vContext.rejectOldSample = false
	}

	resourcesKept := 0
	for resourceIdx := range stream.ResourceLogs {
		resource := &stream.ResourceLogs[resourceIdx]
		resourceMetadata := newMetadataGroup(&resource.Attrs)
		scopesKept := 0
		var resourceDiscardReason string
		for scopeIdx := range resource.ScopeLogs {
			scope := &resource.ScopeLogs[scopeIdx]
			scopeMetadata := newMetadataGroup(&scope.Attrs)
			sharedMetadataSize := resourceMetadata.originalSize + scopeMetadata.originalSize
			sharedMetadataCount := resourceMetadata.originalCount + scopeMetadata.originalCount
			var normalizedSharedMetadataSize int
			sharedMetadata := make([]labels.Labels, 0, 2)
			scopeEntriesKept := 0
			var scopeDiscardReason string
			for _, entry := range scope.Entries {
				if reason, err := d.validator.ValidateEntry(vContext, lbs, entry, sharedMetadataSize, sharedMetadataCount); err != nil {
					d.validator.reportDiscardedData(ctx, reason, vContext, lbs, retentionHours, policy, util.EntryTotalSize(&entry), 1, format)
					scopeDiscardReason = reason
					resourceDiscardReason = reason
					d.writeFailuresManager.Log(tenantID, err)
					validationErrors.Add(err)
					continue
				}

				// Normalize shared attributes once, after an entry passes validation.
				if scopeEntriesKept == 0 {
					if err := d.normalizeMetadataGroup(&resourceMetadata, tenantID, format, shouldDiscoverLevels); err != nil {
						return streamStats{}, err
					}
					if err := d.normalizeMetadataGroup(&scopeMetadata, tenantID, format, shouldDiscoverLevels); err != nil {
						return streamStats{}, err
					}
					// Scope attributes take precedence over resource attributes.
					if !scopeMetadata.labels.IsEmpty() {
						sharedMetadata = append(sharedMetadata, scopeMetadata.labels)
					}
					if !resourceMetadata.labels.IsEmpty() {
						sharedMetadata = append(sharedMetadata, resourceMetadata.labels)
					}
					normalizedSharedMetadataSize = resourceMetadata.normalizedSize + scopeMetadata.normalizedSize
				}

				structuredMetadata, _, err := d.normalizeStructuredMetadata(entry.StructuredMetadata, tenantID, format, false)
				if err != nil {
					return streamStats{}, err
				}
				entry.StructuredMetadata = logproto.CopyToLabelAdapters(entry.StructuredMetadata, structuredMetadata)

				fieldDetector.discoverFields(ctx, lbs, &entry, structuredMetadata, sharedMetadata...)
				if vContext.incrementDuplicateTimestamps {
					duplicateTimestamps.increment(&entry)
				}
				scope.Entries[scopeEntriesKept] = entry
				scopeEntriesKept++
				entrySize := util.EntryTotalSize(&entry)
				unexpandedSize += entrySize
				expandedSize += entrySize + normalizedSharedMetadataSize
			}
			scope.Entries = scope.Entries[:scopeEntriesKept]
			if scopeEntriesKept == 0 {
				// Charge shared bytes only when the group is emptied, using its last rejection reason.
				if scopeDiscardReason != "" && scopeMetadata.originalSize > 0 {
					d.validator.reportDiscardedData(ctx, scopeDiscardReason, vContext, lbs, retentionHours, policy, scopeMetadata.originalSize, 0, format)
				}
				continue
			}
			unexpandedSize += scopeMetadata.normalizedSize
			entriesKept += scopeEntriesKept
			resource.ScopeLogs[scopesKept] = *scope
			scopesKept++
		}
		clear(resource.ScopeLogs[scopesKept:])
		resource.ScopeLogs = resource.ScopeLogs[:scopesKept]
		if scopesKept == 0 {
			if resourceDiscardReason != "" && resourceMetadata.originalSize > 0 {
				d.validator.reportDiscardedData(ctx, resourceDiscardReason, vContext, lbs, retentionHours, policy, resourceMetadata.originalSize, 0, format)
			}
			continue
		}
		unexpandedSize += resourceMetadata.normalizedSize
		stream.ResourceLogs[resourcesKept] = *resource
		resourcesKept++
	}
	clear(stream.ResourceLogs[resourcesKept:])
	stream.ResourceLogs = stream.ResourceLogs[:resourcesKept]
	return streamStats{
		entriesKept:    entriesKept,
		unexpandedSize: unexpandedSize,
		expandedSize:   expandedSize,
	}, nil
}

func (d *Distributor) normalizeMetadataGroup(group *metadataGroup, tenantID, format string, normalizeLevel bool) error {
	if group.normalized {
		return nil
	}

	metadata, changed, err := d.normalizeStructuredMetadata(*group.attrs, tenantID, format, normalizeLevel)
	if err != nil {
		return err
	}
	if changed {
		*group.attrs = logproto.CopyToLabelAdapters(nil, metadata)
		group.normalizedSize = util.StructuredMetadataSize(*group.attrs)
	}
	group.labels = metadata
	group.normalized = true
	return nil
}

// normalizeStructuredMetadata normalizes names and values without changing the input.
func (d *Distributor) normalizeStructuredMetadata(metadata []logproto.LabelAdapter, tenantID, format string, normalizeLevel bool) (labels.Labels, bool, error) {
	if len(metadata) == 0 {
		return labels.EmptyLabels(), false, nil
	}
	labelNamer := otlptranslator.LabelNamer{}
	builder := labels.NewBuilder(logproto.FromLabelAdaptersToLabels(metadata))
	changed := false
	for _, lbl := range metadata {
		name, err := labelNamer.Build(lbl.Name)
		if err != nil {
			return labels.EmptyLabels(), false, err
		}
		if name != lbl.Name {
			builder.Del(lbl.Name)
			builder.Set(name, lbl.Value)
			changed = true
			d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID, format).Inc()
		}
		value := lbl.Value
		if value == "" {
			// The labels builder removes empty values.
			changed = true
		}
		if strings.ContainsRune(value, utf8.RuneError) {
			value = strings.Map(removeInvalidUtf, value)
			builder.Set(name, value)
			changed = true
			d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID, format).Inc()
		}
	}
	if normalizeLevel {
		value := builder.Get(constants.LevelLabel)
		if normalized := normalizeLogLevel(value); normalized != value {
			builder.Set(constants.LevelLabel, normalized)
			changed = true
		}
	}
	return builder.Labels(), changed, nil
}

// missingEnforcedLabels returns true if the stream is missing any of the required labels.
//
// It also returns the first label that is missing if any (for the case of multiple labels missing).
func (d *Distributor) missingEnforcedLabels(lbs labels.Labels, tenantID string, policy string) (bool, []string) {
	perPolicyEnforcedLabels := d.validator.PolicyEnforcedLabels(tenantID, policy)
	tenantEnforcedLabels := d.validator.EnforcedLabels(tenantID)

	requiredLbs := append(tenantEnforcedLabels, perPolicyEnforcedLabels...)
	if len(requiredLbs) == 0 {
		// no enforced labels configured.
		return false, []string{}
	}

	// Use a map to deduplicate the required labels. Duplicates may happen if the same label is configured
	// in both the per-tenant and per-policy enforced labels.
	seen := make(map[string]struct{})
	missingLbs := []string{}

	for _, lb := range requiredLbs {
		if _, ok := seen[lb]; ok {
			continue
		}

		seen[lb] = struct{}{}
		if !lbs.Has(lb) {
			missingLbs = append(missingLbs, lb)
		}
	}

	return len(missingLbs) > 0, missingLbs
}

// rateLimitBucket accumulates the bytes and line count of the streams metered against a
// single ingestion rate-limit bucket (either the tenant-wide bucket or a per-policy bucket).
type rateLimitBucket struct {
	policy      string // the name of the policy that owns this bucket.
	hasOverride bool   // true if the policy has its own rate limit, false if it uses the tenant default rate limit.
	bytes       int    // the total number of bytes metered for this policy.
	lines       int    // the total number of log lines metered for this policy.
}

// rateLimitError builds the client-facing 429 error for the rate-limit buckets a request
// exceeded. A single exceeded bucket uses the existing per-tenant or per-policy message; two
// or more are enumerated deterministically (the caller sorts them). All limits are looked up
// at the same "now" used for the reservation decision.
func (d *Distributor) rateLimitError(now time.Time, tenantID string, exceeded []*rateLimitBucket) error {
	limitOf := func(b *rateLimitBucket) int {
		key := tenantID
		if b.hasOverride {
			key = encodeRateLimitKey(tenantID, b.policy)
		}
		return int(d.ingestionRateLimiter.Limit(now, key))
	}

	if len(exceeded) == 1 {
		b := exceeded[0]
		if b.hasOverride {
			return fmt.Errorf(validation.RateLimitedPolicyErrorMsg, tenantID, b.policy, limitOf(b), b.lines, b.bytes)
		}
		return fmt.Errorf(validation.RateLimitedErrorMsg, tenantID, limitOf(b), b.lines, b.bytes)
	}

	clauses := make([]string, 0, len(exceeded))
	for _, b := range exceeded {
		if b.hasOverride {
			clauses = append(clauses, fmt.Sprintf("policy %q (limit: %d bytes/sec) ingesting %d lines totaling %d bytes", b.policy, limitOf(b), b.lines, b.bytes))
		} else {
			clauses = append(clauses, fmt.Sprintf("the tenant default (limit: %d bytes/sec) ingesting %d lines totaling %d bytes", limitOf(b), b.lines, b.bytes))
		}
	}
	return fmt.Errorf(validation.RateLimitedMultiErrorMsg, tenantID, strings.Join(clauses, "; "))
}

// enforceIngestionRateLimits applies the per-bucket ingestion rate limit as an all-or-nothing
// decision. A bucket with a per-policy override is metered independently from the tenant-wide
// bucket. We tentatively reserve each bucket's bytes and, if any bucket can't accept them
// immediately, cancel every reservation (returning the tokens) and reject the whole request.
// Using reservations rather than AllowN keeps the buckets independent: a request rejected
// because one bucket is over its limit does not consume tokens from the others, so an
// over-limit policy can't drain the budgets of unrelated policies across client retries.
//
// It returns a non-nil 429 error if any bucket is over its limit (all reservations are rolled
// back); nil means the request was admitted (the reservations are kept and the tokens consumed).
func (d *Distributor) enforceIngestionRateLimits(
	ctx context.Context,
	now time.Time,
	tenantID string,
	rlBuckets map[string]*rateLimitBucket,
	streams []logproto.InternalStreamAdapter,
	validationContext validationContext,
	streamResolver push.StreamResolver,
	format string,
) error {
	type bucketReservation struct {
		bucket      *rateLimitBucket
		reservation *rate.Reservation
	}
	reservations := make([]bucketReservation, 0, len(rlBuckets))
	var exceeded []*rateLimitBucket
	for _, b := range rlBuckets {
		limiterKey := tenantID
		if b.hasOverride {
			limiterKey = encodeRateLimitKey(tenantID, b.policy)
		}
		r := d.ingestionRateLimiter.ReserveN(now, limiterKey, b.bytes)
		reservations = append(reservations, bucketReservation{bucket: b, reservation: r})
		// A reservation that isn't OK (bytes exceed the burst) or that requires a wait is not
		// immediately allowed, which is equivalent to AllowN returning false. We evaluate every
		// bucket (rather than stopping at the first failure) so the rejection error can
		// deterministically report all exceeded buckets.
		if !r.OK() || r.DelayFrom(now) > 0 {
			exceeded = append(exceeded, b)
		}
	}

	if len(exceeded) == 0 {
		return nil
	}

	// Roll back every reservation so no tokens are consumed for a rejected request.
	for _, br := range reservations {
		br.reservation.CancelAt(now)
	}

	// The whole request is dropped, so attribute every stream as discarded (each under its
	// own policy label, handled by reportDiscardedStreams).
	d.reportDiscardedStreams(ctx, streams, validationContext, validation.RateLimited, streamResolver, format)

	// Sort exceeded buckets deterministically: the tenant-wide bucket first, then policies
	// alphabetically. This keeps the client-facing error (and tests) stable regardless of
	// map iteration order.
	slices.SortFunc(exceeded, func(a, b *rateLimitBucket) int {
		if a.hasOverride != b.hasOverride {
			if !a.hasOverride {
				return -1 // tenant-wide bucket sorts first
			}
			return 1
		}
		return strings.Compare(a.policy, b.policy)
	})

	err := d.rateLimitError(now, tenantID, exceeded)
	d.writeFailuresManager.Log(tenantID, err)
	return httpgrpc.Errorf(http.StatusTooManyRequests, "%s", err.Error())
}

// reportDiscardedStreams reports whole streams that passed validation as discarded for reason,
// resolving each stream's labels, retention and policy for the report. It skips a stream whose
// labels fail validation, as a shard of a stream at max_label_names_per_series does.
func (d *Distributor) reportDiscardedStreams(
	ctx context.Context,
	streams []logproto.InternalStreamAdapter,
	vCtx validationContext,
	reason string,
	streamResolver push.StreamResolver,
	format string,
) {
	for _, stream := range streams {
		parsed, _, err := d.parseStreamLabels(ctx, vCtx, stream.Labels, stream, streamResolver)
		if err != nil {
			level.Warn(d.logger).Log("msg", "failed to parse stream labels when tracking discarded samples and bytes, this data will not be tracked", "error", err, "stream", stream.Labels)
			continue
		}
		d.validator.reportDiscardedData(ctx, reason, vCtx, parsed.lbs, parsed.retentionHours, parsed.policy, nestedStreamSize(stream), stream.EntryCount(), format)
	}
}

// limitsServiceShardCandidate is a logical (pre-shard) stream whose tenant
// asks the ingest-limits service for a shard count. rateStoreShards is
// shardCountFor's recommendation for this push, which is not necessarily the
// number of streams it produces, see shardStream. totalSize is the same push
// size the rate store was given. labels and shardStreamsCfg are what live
// mode needs to shard the stream once the service has answered.
type limitsServiceShardCandidate struct {
	stream          logproto.InternalStreamAdapter
	labels          labels.Labels
	policy          string
	shardStreamsCfg shardstreams.Config
	rateStoreShards int
	rateStoreRate   int64
	totalSize       uint64
}

// TODO(chaudum): Is a 2s timeout too long? Running it in production will tell...
const limitsServiceShardTimeout = 2 * time.Second

// limitsServiceShardCounts asks the ingest-limits service for a shard count
// for each candidate, records how its answer compares to the local rate
// store's, and returns the shard count to use per candidate, in the order the
// candidates were given.
//
// Live mode never falls back to the local rate store's recommendation. A
// candidate the service has no usable answer for -- the whole call failed,
// there was no result for the stream, the service could not check it, or
// the answering instance does not own its partition -- gets a count of 1:
// it is written unsharded rather than rejected. A limits-service outage removes
// sharding until it recovers rather than stopping ingestion.
//
// A candidate the service rejected, for example for exceeding the stream
// limit, gets a count of 0.
func (d *Distributor) limitsServiceShardCounts(ctx context.Context, tenantID, mode string, candidates []limitsServiceShardCandidate) []int {
	// Deferred so that every path, success, failure and Unimplemented alike,
	// records the latency the call adds.
	defer prometheus.NewTimer(d.m.limitsServiceShardDuration).ObserveDuration()

	// Defaults to 1 (written unsharded, not rejected) for every candidate the
	// service ends up with no usable answer for, whether the whole call fails
	// or only a specific candidate does; see the fail-open cases below.
	shardCounts := make([]int, len(candidates))
	for i := range shardCounts {
		shardCounts[i] = 1
	}

	callCtx, cancel := context.WithTimeout(ctx, limitsServiceShardTimeout)
	defer cancel()

	results, err := d.ingestLimits.CheckLimitsAndShard(callCtx, tenantID, candidates)
	if err != nil {
		// None of the candidates were answered, so count them all as failed
		// rather than leaving them out of the coverage metrics.
		d.m.limitsServiceShardShadowFailed.WithLabelValues(tenantID, mode, limits.ReasonFailed.String()).Add(float64(len(candidates)))
		if status.Code(err) == codes.Unimplemented {
			// The service predates the RPC, for instance during a rollout.
			level.Warn(d.logger).Log("msg", "CheckLimitsAndShard call returned Unimplemented; the limits service may predate this RPC", "tenant", tenantID, "mode", mode)
		} else {
			level.Error(d.logger).Log("msg", "failed CheckLimitsAndShard call", "tenant", tenantID, "mode", mode, "err", err)
		}
		return shardCounts
	}

	for i, c := range candidates {
		result, ok := results[c.stream.Hash]
		switch {
		case result.GetRejectReason() != "":
			// A difference in kind rather than in shard count: the local rate
			// store never rejects a stream.
			d.m.limitsServiceShardShadowRejected.WithLabelValues(tenantID, mode, result.GetRejectReason()).Inc()
			shardCounts[i] = 0
		case !ok,
			result.GetStats().GetShardDecisionContext() == uint32(limits.ReasonFailed):
			// No usable answer: there was none, or the service could not check
			// the stream. Comparing it would report agreement or disagreement
			// that does not exist.
			reason := limits.Reason(result.GetStats().GetShardDecisionContext())
			d.m.limitsServiceShardShadowFailed.WithLabelValues(tenantID, mode, reason.String()).Inc()
			// Fail open: there is no decision from the service to enforce, so
			// the stream is written unsharded rather than dropped.
			shardCounts[i] = 1
		case result.GetShards() < 1 && result.GetRejectReason() == "":
			// Should never happen: the frontend's completeShardResults
			// normalizes a zero shard count without a reject reason away
			// before it reaches the distributor. Fail open rather than drop
			// the stream on a contract violation we did not expect.
			level.Error(d.logger).Log("msg", "zero shard count with no reject reason")
			d.m.limitsServiceShardShadowFailed.WithLabelValues(tenantID, mode, limits.ReasonFailed.String()).Inc()
			shardCounts[i] = 1
		default:
			resultShards := int(result.GetShards())
			shardCounts[i] = resultShards
			sharding := shardingState(c.rateStoreShards, resultShards)
			d.m.limitsServiceShardShadowCompared.WithLabelValues(tenantID, mode, sharding).Inc()
			// Record both rates so their distributions can be compared as
			// heatmaps. Both are sustained rates, before this push is
			// amortized on top.
			d.m.limitsServiceShardShadowStreamRate.WithLabelValues(tenantID, mode, "rate_store").Observe(float64(c.rateStoreRate))
			d.m.limitsServiceShardShadowStreamRate.WithLabelValues(tenantID, mode, "limits").Observe(float64(result.GetStats().GetEvaluatedRate()))
			if result.GetStats().GetShardDecisionContext() == uint32(limits.ReasonStreamShardsCapped) {
				d.m.limitsServiceShardShadowCapped.WithLabelValues(tenantID, mode).Inc()
			}
			if resultShards != c.rateStoreShards {
				d.m.limitsServiceShardShadowDivergence.WithLabelValues(tenantID, mode, sharding).Inc()
				// Record the gap as a positive magnitude tagged by direction,
				// so over- and under-sharding are separate distributions.
				direction := "over"
				difference := resultShards - c.rateStoreShards
				if difference < 0 {
					direction = "under"
					difference = -difference
				}
				d.m.limitsServiceShardShadowDivergenceMagnitude.WithLabelValues(tenantID, mode, direction).Observe(float64(difference))
				level.Debug(log.With(util_log.WithUserID(tenantID, d.logger), "stream", c.stream.Labels)).Log(
					"msg", "shard count divergence",
					"mode", mode,
					"rate_store_shards", c.rateStoreShards,
					"limits_service_shards", resultShards,
					"rate_store_rate", c.rateStoreRate,
					"limits_service_rate", result.GetStats().GetEvaluatedRate(),
				)
			}
		}
	}
	return shardCounts
}

// shardingState reports which side's decision shards the stream.
func shardingState(rateStoreShards, limitsShards int) string {
	rateStoreSharded := rateStoreShards > 1
	limitsSharded := limitsShards > 1
	switch {
	case rateStoreSharded && limitsSharded:
		return "both"
	case limitsSharded:
		return "limits_only"
	case rateStoreSharded:
		return "rate_store_only"
	default:
		return "neither"
	}
}

// shardStream shards (divides) the given stream into N smaller streams, where
// N is the sharding size for the given stream. shardSteam returns the smaller
// streams and their associated keys for hashing to ingesters, along with the
// shard count shardCountFor recommended. That count is not necessarily the
// number of streams returned, as shardNested limits the shards to the number
// of entries.
//
// The number of shards is limited by the number of entries.
func (d *Distributor) shardStream(stream logproto.InternalStreamAdapter, lbls labels.Labels, pushSize int, tenantID string, policy string, shardStreamsCfg shardstreams.Config) ([]KeyedStream, int) {
	logger := log.With(util_log.WithUserID(tenantID, d.logger), "stream", stream.Labels)
	shardCount := d.shardCountFor(logger, stream, pushSize, tenantID, shardStreamsCfg)
	return d.shardStreamToCount(stream, lbls, tenantID, policy, shardStreamsCfg, shardCount), shardCount
}

// shardStreamToCount shards stream into shardCount smaller streams, whoever
// decided that count, and returns them with their hash keys. The number of
// streams returned is not necessarily shardCount, as shardNested limits the
// shards to the number of entries.
func (d *Distributor) shardStreamToCount(stream logproto.InternalStreamAdapter, lbls labels.Labels, tenantID string, policy string, shardStreamsCfg shardstreams.Config, shardCount int) []KeyedStream {
	if shardCount <= 1 {
		return []KeyedStream{{HashKey: lokiring.TokenFor(tenantID, stream.Labels), HashKeyNoShard: stream.Hash, Stream: stream, Policy: policy}}
	}

	d.m.streamShardCount.Inc()
	if shardStreamsCfg.LoggingEnabled {
		level.Info(log.With(util_log.WithUserID(tenantID, d.logger), "stream", stream.Labels)).Log("msg", "sharding request", "shard_count", shardCount)
	}

	return d.divideEntriesBetweenShards(tenantID, lbls, shardCount, shardStreamsCfg, stream, policy)
}

func (d *Distributor) divideEntriesBetweenShards(tenantID string, lbls labels.Labels, totalShards int, shardStreamsCfg shardstreams.Config, stream logproto.InternalStreamAdapter, policy string) []KeyedStream {
	if totalShards <= 0 {
		level.Error(d.logger).Log("msg", "attempt to create shard with zeroed total shards", "org_id", tenantID, "stream", stream.Labels, "entries_len", stream.EntryCount())
		return nil
	}

	// Only nonempty shards advance the shard rotation.
	startShard := d.shardTracker.LastShardNum(tenantID, stream.Hash)
	shards := shardNested(stream, lbls, totalShards, startShard)

	derivedStreams := make([]KeyedStream, 0, len(shards))
	for i := range shards {
		derivedStreams = append(derivedStreams, KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, shards[i].Labels),
			HashKeyNoShard: stream.Hash,
			Stream:         shards[i],
			Policy:         policy,
		})

		if shardStreamsCfg.LoggingEnabled {
			level.Info(d.logger).Log("msg", "stream derived from sharding", "src-stream", stream.Labels, "derived-stream", shards[i].Labels)
		}
	}
	d.shardTracker.SetLastShardNum(tenantID, stream.Hash, startShard+len(shards))

	return derivedStreams
}

func (d *Distributor) truncateLines(vContext validationContext, stream logproto.InternalStreamAdapter) {
	if !vContext.maxLineSizeTruncate {
		return
	}

	suffix := vContext.maxLineSizeTruncateIdentifier

	var truncatedSamples, truncatedBytes int
	stream.EachGroup(func(_, _ []logproto.LabelAdapter, entries []logproto.Entry) {
		for i, e := range entries {
			if maxSize := vContext.maxLineSize; maxSize != 0 && len(e.Line) > maxSize {
				truncateTo := maxSize - len(suffix)
				if truncateTo <= 0 {
					continue
				}

				entries[i].Line = e.Line[:truncateTo] + suffix

				truncatedSamples++
				truncatedBytes += len(e.Line) - truncateTo
			}
		}
	})

	if truncatedSamples > 0 {
		validation.MutatedSamples.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedSamples))
		validation.MutatedBytes.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedBytes))

		// Emit a log line so operators can confirm which streams are being
		// truncated when max_line_size_truncate is enabled, complementing the
		// mutated_samples_total / mutated_bytes_total metrics.
		level.Debug(d.logger).Log(
			"msg", "truncated log lines exceeding max_line_size",
			"tenant", vContext.userID,
			"stream", stream.Labels,
			"max_line_size", vContext.maxLineSize,
			"truncated_lines", truncatedSamples,
			"truncated_bytes", truncatedBytes,
		)
	}
}

type pushIngesterTask struct {
	streamTracker []*streamTracker
	pushTracker   *PushTracker
	ingester      ring.InstanceDesc
	ctx           context.Context
	cancel        context.CancelFunc
}

func (d *Distributor) pushIngesterWorker(ctx context.Context) {
	defer d.ingesterTaskWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-d.ingesterTasks:
			d.sendStreams(task)
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendStreams(task pushIngesterTask) {
	defer task.cancel()
	err := d.sendStreamsErr(task.ctx, task.ingester, task.streamTracker)

	// If we succeed, decrement each stream's pending count by one.
	// If we reach the required number of successful puts on this stream, then
	// decrement the number of pending streams by one.
	// If we successfully push all streams to min success ingesters, wake up the
	// waiting rpc so it can return early. Similarly, track the number of errors,
	// and if it exceeds maxFailures shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendStreams
	// goroutine will write to either channel.
	for i := range task.streamTracker {
		if err != nil {
			if task.streamTracker[i].failed.Inc() <= int32(task.streamTracker[i].maxFailures) {
				continue
			}
			task.pushTracker.doneWithResult(err)
		} else {
			if task.streamTracker[i].succeeded.Inc() != int32(task.streamTracker[i].minSuccess) {
				continue
			}
			task.pushTracker.doneWithResult(nil)
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendStreamsErr(ctx context.Context, ingester ring.InstanceDesc, streams []*streamTracker) error {
	c, err := d.ingesterClients.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}

	req := &logproto.PushRequest{
		Streams: make([]logproto.Stream, len(streams)),
	}
	for i, s := range streams {
		// Ingester RPCs serialize the flat view without any modifications.
		req.Streams[i] = s.Stream.FlatView()
	}

	_, err = c.(logproto.PusherClient).Push(ctx, req)
	d.m.ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		if e, ok := status.FromError(err); ok {
			switch e.Code() {
			case codes.DeadlineExceeded:
				d.m.ingesterAppendTimeouts.WithLabelValues(ingester.Addr).Inc()
			}
		}
	}
	return err
}

// sendStreamsToKafka sends all streams to Kafka or returns an error.
func (d *Distributor) sendStreamsToKafka(ctx context.Context, tenant string, streams []KeyedStream, tracker *PushTracker, subring *ring.PartitionRing) {
	records, err := d.recordsForStreams(tenant, streams, subring)
	if err != nil {
		// We need to add len(streams) to the counter as we later count successes and
		// failures per stream too.
		d.m.kafkaAppends.WithLabelValues("kafka", "fail").Add(float64(len(streams)))
		tracker.doneWithResult(err)
		return
	}
	// TODO(grobinson): Check if this is needed, as I would have expected
	// streams without entries to have been removed when the request was
	// validated.
	if len(records) == 0 {
		tracker.doneWithResult(nil)
		return
	}
	d.m.kafkaRecordsPerRequest.Observe(float64(len(records)))
	// Produce the records to Kafka.
	writeLatency := prometheus.NewTimer(d.m.kafkaWriteLatency)
	results := d.kafkaWriter.ProduceSync(ctx, records)
	if count, sizeBytes := successfulProduceRecordsStats(results); count > 0 {
		// TODO(grobinson): We should emit the write latency even when we failed.
		// This has been kept as-is for now to preserve behavior.
		writeLatency.ObserveDuration()
		d.m.kafkaWriteBytesTotal.Add(float64(sizeBytes))
	}
	var finalErr error
	for _, result := range results {
		if result.Err != nil {
			d.m.kafkaAppends.WithLabelValues(fmt.Sprintf("partition_%d", result.Record.Partition), "fail").Inc()
			finalErr = result.Err
		} else {
			d.m.kafkaAppends.WithLabelValues(fmt.Sprintf("partition_%d", result.Record.Partition), "success").Inc()
		}
	}
	tracker.doneWithResult(finalErr)
}

// recordsForStreams returns the Kafka records for the tenant's streams.
// It splits large streams into multiple Kafka records where a stream would
// exceed the maximum record size.
func (d *Distributor) recordsForStreams(
	tenant string,
	streams []KeyedStream,
	subring *ring.PartitionRing,
) ([]*kgo.Record, error) {
	records := make([]*kgo.Record, 0, len(streams))

	for _, stream := range streams {
		// TODO(grobinson): Check if this is still needed, I would have expected
		// streams with no entries to have be removed when the request was validated.
		if stream.Stream.EntryCount() == 0 {
			continue
		}
		partition, err := subring.ActivePartitionForKey(stream.HashKey)
		if err != nil {
			return nil, fmt.Errorf("failed to find partition for stream: %w", err)
		}
		var streamRecords []*kgo.Record
		if d.cfg.OTLPConfig.DeferAttributeExpansion {
			streamRecords, err = kafka.EncodeInternal(partition, tenant, stream.Stream, d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes)
		} else {
			// Encode reads the view synchronously without modifying it.
			streamRecords, err = kafka.Encode(partition, tenant, stream.Stream.FlatView(), d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to marshal streams to records: %w", err)
		}
		records = append(records, streamRecords...)
	}
	return records, nil
}

func successfulProduceRecordsStats(results kgo.ProduceResults) (count, sizeBytes int) {
	for _, res := range results {
		if res.Err == nil && res.Record != nil {
			count++
			sizeBytes += len(res.Record.Value)
		}
	}

	return
}

type labelData struct {
	ls   labels.Labels
	hash uint64
}

// parsedStreamLabels is a stream's labels as parseStreamLabels resolved them.
type parsedStreamLabels struct {
	lbs labels.Labels
	// labelsString is lbs as a string, which replaces the labels the stream was pushed with.
	labelsString   string
	hash           uint64
	retentionHours string
	policy         string
}

// parseStreamLabels parses and validates stream labels using a request-scoped policy resolver. If
// they are invalid, it returns the reason to discard the stream's data for with the error, and
// only lbs (empty), retentionHours and policy are set, as far as they could be resolved, for
// discard reporting.
func (d *Distributor) parseStreamLabels(ctx context.Context, vContext validationContext, key string, stream logproto.InternalStreamAdapter, streamResolver push.StreamResolver) (parsedStreamLabels, string, error) {
	if val, ok := d.labelCache.Get(key); ok {
		retentionHours := streamResolver.RetentionHoursFor(val.ls)
		policy := streamResolver.PolicyFor(ctx, val.ls)
		return parsedStreamLabels{lbs: val.ls, labelsString: val.ls.String(), hash: val.hash, retentionHours: retentionHours, policy: policy}, "", nil
	}

	ls, err := syntax.ParseLabels(key)
	if err != nil {
		retentionHours := d.tenantsRetention.RetentionHoursFor(vContext.userID, labels.EmptyLabels())
		// TODO: check for global policy.
		return parsedStreamLabels{lbs: labels.EmptyLabels(), retentionHours: retentionHours}, validation.InvalidLabels, fmt.Errorf(validation.InvalidLabelsErrorMsg, key, err)
	}

	policy := streamResolver.PolicyFor(ctx, ls)
	retentionHours := d.tenantsRetention.RetentionHoursFor(vContext.userID, ls)

	if reason, err := d.validator.ValidateLabels(vContext, ls, stream); err != nil {
		return parsedStreamLabels{lbs: labels.EmptyLabels(), retentionHours: retentionHours, policy: policy}, reason, err
	}

	lsHash := labels.StableHash(ls)

	d.labelCache.Add(key, labelData{ls, lsHash})
	return parsedStreamLabels{lbs: ls, labelsString: ls.String(), hash: lsHash, retentionHours: retentionHours, policy: policy}, "", nil
}

// shardCountFor returns the right number of shards to be used by the given stream.
//
// It first checks if the number of shards is present in the shard store. If it isn't it will calculate it
// based on the rate stored in the rate store and will store the new evaluated number of shards.
//
// desiredRate is expected to be given in bytes.
func (d *Distributor) shardCountFor(logger log.Logger, stream logproto.InternalStreamAdapter, pushSize int, tenantID string, streamShardcfg shardstreams.Config) int {
	if streamShardcfg.DesiredRate.Val() <= 0 {
		if streamShardcfg.LoggingEnabled {
			level.Error(logger).Log("msg", "invalid desired rate", "desired_rate", streamShardcfg.DesiredRate.String())
		}
		return 1
	}

	rate, pushRate := d.rateStore.RateFor(tenantID, stream.Hash)
	if pushRate == 0 {
		// first push, don't shard until the rate is understood
		return 1
	}

	if pushRate > 1 {
		// High throughput. Let stream sharding do its job and
		// don't attempt to amortize the push size over the
		// real rate
		pushRate = 1
	}

	n, capped := calculateShards(rate, int(float64(pushSize)*pushRate), streamShardcfg.DesiredRate.Val(), streamShardcfg.MaxShardCount)
	if capped {
		d.m.rateStoreMaxShardsLimited.WithLabelValues(tenantID).Inc()
	}
	return n
}

func calculateShards(rate int64, pushSize, desiredRate, maxShardCount int) (n int, capped bool) {
	shards := float64(rate+int64(pushSize)) / float64(desiredRate)
	if shards <= 1 {
		return 1, false
	}
	n = int(math.Ceil(shards))
	if maxShardCount > 0 && n > maxShardCount {
		n = maxShardCount
		capped = true
	}
	n = max(n, 1) // minumum number of shards is 1
	return
}

func nestedStreamSize(s logproto.InternalStreamAdapter) int {
	lines, structuredMetadata := calculateStreamSizes(s)
	return int(lines + structuredMetadata)
}

// calculateStreamSizes counts line and metadata bytes, with shared attributes counted once
// per nonempty resource or scope, without deduplicating names.
func calculateStreamSizes(stream logproto.InternalStreamAdapter) (uint64, uint64) {
	var entriesSize, structuredMetadataSize uint64
	for _, resource := range stream.ResourceLogs {
		hasEntries := false
		for _, scope := range resource.ScopeLogs {
			if len(scope.Entries) == 0 {
				continue
			}
			hasEntries = true
			structuredMetadataSize += uint64(util.StructuredMetadataSize(scope.Attrs))
			for _, entry := range scope.Entries {
				entriesSize += uint64(len(entry.Line))
				structuredMetadataSize += uint64(util.StructuredMetadataSize(entry.StructuredMetadata))
			}
		}
		if hasEntries {
			structuredMetadataSize += uint64(util.StructuredMetadataSize(resource.Attrs))
		}
	}
	return entriesSize, structuredMetadataSize
}

// newRingAndLifecycler creates a new distributor ring and lifecycler with all required lifecycler delegates
func newRingAndLifecycler(cfg RingConfig, instanceCount *atomic.Uint32, logger log.Logger, reg prometheus.Registerer, metricsNamespace string) (*ring.Ring, *ring.BasicLifecycler, error) {
	kvStore, err := kv.NewClient(cfg.KVStore, ring.GetCodec(), kv.RegistererWithKVName(reg, "distributor-lifecycler"), logger)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to initialize distributors' KV store")
	}

	lifecyclerCfg, err := cfg.ToBasicLifecyclerConfig(logger)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build distributors' lifecycler config")
	}

	var delegate ring.BasicLifecyclerDelegate
	delegate = ring.NewInstanceRegisterDelegate(ring.ACTIVE, 1)
	delegate = newHealthyInstanceDelegate(instanceCount, cfg.HeartbeatTimeout, delegate)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, logger)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.HeartbeatTimeout, delegate, logger)

	distributorsLifecycler, err := ring.NewBasicLifecycler(lifecyclerCfg, "distributor", ringKey, kvStore, delegate, logger, prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", reg))
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to initialize distributors' lifecycler")
	}

	distributorsRing, err := ring.New(cfg.ToRingConfig(), "distributor", ringKey, logger, prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", reg))
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to initialize distributors' ring client")
	}

	return distributorsRing, distributorsLifecycler, nil
}

// HealthyInstancesCount implements the ReadLifecycler interface.
//
// We use a ring lifecycler delegate to count the number of members of the
// ring. The count is then used to enforce rate limiting correctly for each
// distributor. $EFFECTIVE_RATE_LIMIT = $GLOBAL_RATE_LIMIT / $NUM_INSTANCES.
func (d *Distributor) HealthyInstancesCount() int {
	return int(d.healthyInstancesCount.Load())
}

type requestScopedStreamResolver struct {
	userID               string
	policyStreamMappings validation.PolicyStreamMapping
	retention            *retention.TenantRetentionSnapshot

	logger log.Logger
}

func newRequestScopedStreamResolver(userID string, overrides Limits, logger log.Logger) *requestScopedStreamResolver {
	return &requestScopedStreamResolver{
		userID:               userID,
		policyStreamMappings: overrides.PoliciesStreamMapping(userID),
		retention:            retention.NewTenantRetentionSnapshot(overrides, userID),
		logger:               logger,
	}
}

func (r requestScopedStreamResolver) RetentionPeriodFor(lbs labels.Labels) time.Duration {
	return r.retention.RetentionPeriodFor(lbs)
}

func (r requestScopedStreamResolver) RetentionHoursFor(lbs labels.Labels) string {
	return r.retention.RetentionHoursFor(lbs)
}

func (r requestScopedStreamResolver) PolicyFor(ctx context.Context, lbs labels.Labels) string {
	policies := r.policyStreamMappings.PolicyFor(ctx, lbs)

	var policy string
	if len(policies) > 0 {
		policy = policies[0]
		if len(policies) > 1 {
			level.Warn(r.logger).Log(
				"msg", "multiple policies matched for the same stream",
				"org_id", r.userID,
				"stream", lbs.String(),
				"policy", policy,
				"policies", strings.Join(policies, ","),
				"insight", "true",
			)
		}
	}

	return policy
}

// streamStats counts retained entries and their sizes after normalization and field discovery.
type streamStats struct {
	entriesKept int
	// Shared attributes counted once per group for ingestion rate limiting.
	unexpandedSize int
	// Shared attributes counted once per entry for rate sharding.
	expandedSize int
}

// metadataGroup keeps validation counts separate from normalized ingestion sizes.
type metadataGroup struct {
	attrs          *[]logproto.LabelAdapter
	originalSize   int
	originalCount  int
	normalizedSize int
	labels         labels.Labels
	normalized     bool
}

func newMetadataGroup(attrs *[]logproto.LabelAdapter) metadataGroup {
	size := util.StructuredMetadataSize(*attrs)
	return metadataGroup{
		attrs:          attrs,
		originalSize:   size,
		originalCount:  len(*attrs),
		normalizedSize: size,
	}
}

// duplicateTimestampTracker tracks consecutive kept entries across a stream's groups.
type duplicateTimestampTracker struct {
	previousLine      string
	previousTimestamp time.Time
	originalTimestamp time.Time
}

func (t *duplicateTimestampTracker) increment(entry *logproto.Entry) {
	// Preserve identical lines' timestamps for downstream deduplication.
	if !t.previousTimestamp.IsZero() && t.previousLine != entry.Line &&
		(entry.Timestamp.Equal(t.originalTimestamp) || entry.Timestamp.Equal(t.previousTimestamp)) {
		entry.Timestamp = t.previousTimestamp.Add(time.Nanosecond)
	} else {
		t.originalTimestamp = entry.Timestamp
	}
	t.previousLine = entry.Line
	t.previousTimestamp = entry.Timestamp
}
