package distributor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"runtime/pprof"
	"slices"
	"strconv"
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
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/distributor/rendezvous"
	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	kafka_client "github.com/grafana/loki/v3/pkg/kafka/client"
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

	timeShardLabel = "__time_shard__"
)

var (
	maxLabelCacheSize = 100000
	rfStats           = analytics.NewInt("distributor_replication_factor")

	errServiceUnavailableMaxLoad = httpgrpc.Error(503, "The server cannot accept more requests at this time.")

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
	MaxRecvMsgSize      int   `yaml:"max_recv_msg_size"`
	MaxDecompressedSize int64 `yaml:"max_decompressed_size"`
	MaxInflightBytes    int   `yaml:"max_inflight_bytes"`

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

	// DeferOTLPAttributeExpansion keeps a stream's OTLP resource and scope attributes
	// stored once per group on the wire, rather than expanded onto every entry beneath
	// them. The distributor works on the nested representation either way; this decides
	// only what it encodes, and therefore what every consumer downstream must be able to
	// read. Turning it on before the ingesters and every Kafka consumer understand the
	// internal push format will drop data.
	DeferOTLPAttributeExpansion bool `yaml:"defer_otlp_attribute_expansion"`

	KafkaEnabled              bool `yaml:"kafka_writes_enabled"`
	IngesterEnabled           bool `yaml:"ingester_writes_enabled"`
	IngestLimitsEnabled       bool `yaml:"ingest_limits_enabled"`
	IngestLimitsDryRunEnabled bool `yaml:"ingest_limits_dry_run_enabled"`

	KafkaConfig kafka.Config `yaml:"-"`

	DataObjTeeConfig DataObjTeeConfig     `yaml:"dataobj_tee"`
	CircuitBreaker   CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.OTLPConfig.RegisterFlags(fs)
	cfg.DistributorRing.RegisterFlags(fs)
	cfg.DataObjTeeConfig.RegisterFlags(fs)
	cfg.CircuitBreaker.RegisterFlags(fs)
	cfg.RateStore.RegisterFlagsWithPrefix("distributor.rate-store", fs)
	cfg.WriteFailuresLogging.RegisterFlagsWithPrefix("distributor.write-failures-logging", fs)
	cfg.OTLPAttributeLogging.RegisterFlagsWithPrefix("distributor.otlp-attribute-logging", fs)
	fs.IntVar(&cfg.MaxRecvMsgSize, "distributor.max-recv-msg-size", 100<<20, "The maximum size of a received message.")
	fs.Int64Var(&cfg.MaxDecompressedSize, "distributor.max-decompressed-size", 5000<<20, "The maximum size of a decompressed message. Defaults to 50x max-recv-msg-size.")
	fs.IntVar(&cfg.MaxInflightBytes, "distributor.max-inflight-bytes", 0, "The maximum number of inflight bytes at a time. 0 means disabled.")
	fs.IntVar(&cfg.PushWorkerCount, "distributor.push-worker-count", 256, "Number of workers to push batches to ingesters.")
	fs.BoolVar(&cfg.KafkaEnabled, "distributor.kafka-writes-enabled", false, "Enable writes to Kafka during Push requests.")
	fs.BoolVar(&cfg.IngesterEnabled, "distributor.ingester-writes-enabled", true, "Enable writes to Ingesters during Push requests. Defaults to true.")
	fs.BoolVar(&cfg.IngestLimitsEnabled, "distributor.ingest-limits-enabled", false, "Enable checking limits against the ingest-limits service. Defaults to false.")
	fs.BoolVar(&cfg.IngestLimitsDryRunEnabled, "distributor.ingest-limits-dry-run-enabled", false, "Enable dry-run mode where limits are checked the ingest-limits service, but not enforced. Defaults to false.")
	fs.BoolVar(&cfg.DeferOTLPAttributeExpansion, "distributor.defer-otlp-attribute-expansion", false, "Encode push requests with OTLP resource and scope attributes stored once per group instead of expanded onto every entry. Every ingester and Kafka consumer must understand the internal push format before this is enabled. Defaults to false.")
}

// sanitizeStructuredMetadata normalises attribute names and strips invalid UTF-8 from
// values, returning the result. It never writes through attrs, so it is safe to call on a
// set shared by several streams.
func (d *Distributor) sanitizeStructuredMetadata(tenantID, format string, attrs []logproto.LabelAdapter, labelNamer otlptranslator.LabelNamer) ([]logproto.LabelAdapter, error) {
	structuredMetadata := logproto.FromLabelAdaptersToLabels(attrs)
	normalizedBuilder := labels.NewBuilder(structuredMetadata)

	for _, lbl := range attrs {
		normalized, err := labelNamer.Build(lbl.Name)
		if err != nil {
			return nil, err
		}
		if normalized != lbl.Name {
			// Swap the name with the normalized one.
			normalizedBuilder.Del(lbl.Name)
			normalizedBuilder.Set(normalized, lbl.Value)

			d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID, format).Inc()
		}
		if strings.ContainsRune(lbl.Value, utf8.RuneError) {
			normalizedBuilder.Set(normalized, strings.Map(removeInvalidUtf, lbl.Value))
			d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID, format).Inc()
		}
	}

	return logproto.CopyToLabelAdapters(nil, normalizedBuilder.Labels()), nil
}

func (cfg *Config) Validate() error {
	if !cfg.KafkaEnabled && !cfg.IngesterEnabled {
		return errors.New("at least one of kafka and ingestor writes must be enabled")
	}
	if err := cfg.DataObjTeeConfig.Validate(); err != nil {
		return err
	}
	if err := cfg.CircuitBreaker.Validate(); err != nil {
		return err
	}
	// Set default maxDecompressedSize if not configured (50x maxRecvMsgSize)
	if cfg.MaxDecompressedSize == 0 && cfg.MaxRecvMsgSize > 0 {
		cfg.MaxDecompressedSize = int64(cfg.MaxRecvMsgSize) * 50
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

type metrics struct {
	// distributor metrics
	ingesterAppends                       *prometheus.CounterVec
	ingesterAppendTimeouts                *prometheus.CounterVec
	replicationFactor                     prometheus.Gauge
	streamShardCount                      prometheus.Counter
	zeroStreamCount                       *prometheus.CounterVec
	pushStatsCount                        *prometheus.CounterVec
	tenantPushSanitizedStructuredMetadata *prometheus.CounterVec

	// kafka metrics
	kafkaAppends           *prometheus.CounterVec
	kafkaWriteBytesTotal   prometheus.Counter
	kafkaWriteLatency      prometheus.Histogram
	kafkaRecordsPerRequest prometheus.Histogram
}

func newMetrics(registerer prometheus.Registerer) *metrics {
	return &metrics{
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_ingester_appends_total",
			Help:      "The total number of batch appends sent to ingesters.",
		}, []string{"ingester"}),
		ingesterAppendTimeouts: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_ingester_append_timeouts_total",
			Help:      "The total number of failed batch appends sent to ingesters due to timeouts.",
		}, []string{"ingester"}),
		replicationFactor: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "distributor_replication_factor",
			Help:      "The configured replication factor.",
		}),
		streamShardCount: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "stream_sharding_count",
			Help:      "Total number of times the distributor has sharded streams",
		}),
		zeroStreamCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_zero_streams_count",
			Help:      "Total number of push requests with 0 streams",
		}, []string{"tenant", "stage"}),
		pushStatsCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_stats_count",
			Help:      "Total number of successfully parsed push requests aggregated by tenant, content-type, encoding, version, format",
		}, []string{"tenant", "content_type", "content_encoding", "content_version", "format"}),
		tenantPushSanitizedStructuredMetadata: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_structured_metadata_sanitized_total",
			Help:      "The total number of times we've had to sanitize structured metadata (names or values) at ingestion time per tenant.",
		}, []string{"tenant", "format"}),

		kafkaAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_appends_total",
			Help:      "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		kafkaWriteLatency: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "distributor_kafka_latency_seconds",
			Help:                            "Latency to write an incoming request to the ingest storage.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		kafkaWriteBytesTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_sent_bytes_total",
			Help:      "Total number of bytes sent to the ingest storage.",
		}),
		kafkaRecordsPerRequest: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_records_per_write_request",
			Help:      "The number of records a single per-partition write request has been split into.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
		}),
	}
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

	// Track the max inflight bytes in the last 1 minute.
	inflightBytesHighWatermark prometheus.Summary
	inflightBytes              atomic.Int64
	circuitBreaker             circuitBreaker
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
	dataObjConsumerPartitionRing ring.PartitionRingReader,
	dataObjConsumerPartitionKVClient kv.Client,
	dataObjConsumerPartitionRingKey string,
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

	ingestLimits := newIngestLimits(limitsFrontendClient, cfg.DeferOTLPAttributeExpansion, registerer)

	var kafkaWriter KafkaProducer
	if cfg.KafkaEnabled {
		kafkaClient, err := kafka_client.NewWriterClient("distributor", cfg.KafkaConfig, 20, logger, registerer)
		if err != nil {
			return nil, fmt.Errorf("failed to start kafka client: %w", err)
		}
		kafkaWriter = kafka_client.NewProducer("distributor", kafkaClient, cfg.KafkaConfig.ProducerMaxBufferedBytes,
			prometheus.WrapRegistererWithPrefix("loki_", registerer),
			kafka_client.WithRecordsInterceptor(validation.IngestionPoliciesKafkaProducerInterceptor),
		)

		if cfg.DataObjTeeConfig.Enabled {
			var rendezvousPartitionWatcher *rendezvous.PartitionRingWatcher
			if cfg.DataObjTeeConfig.UseRendezvousHashing {
				rendezvousPartitionWatcher = rendezvous.New(
					rendezvous.Config{Key: dataObjConsumerPartitionRingKey},
					dataObjConsumerPartitionKVClient,
					logger,
				)
				servs = append(servs, rendezvousPartitionWatcher)
			}
			resolver := newSegmentationPartitionResolver(
				uint64(cfg.DataObjTeeConfig.PerPartitionRateBytes),
				cfg.DataObjTeeConfig.UseRendezvousHashing,
				dataObjConsumerPartitionRing,
				rendezvousPartitionWatcher,
				registerer,
				logger,
			)
			dataObjTee, err := NewDataObjTee(
				&cfg.DataObjTeeConfig,
				resolver,
				ingestLimits,
				overrides,
				kafkaWriter,
				logger,
				registerer,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create data object tee: %w", err)
			}
			tee = WrapTee(tee, dataObjTee)

			if rateBatcher := dataObjTee.RateBatcher(); rateBatcher != nil {
				servs = append(servs, rateBatcher)
			}
		}
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
		inflightBytesHighWatermark: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Name:       "loki_distributor_inflight_bytes_high_watermark",
			Help:       "The max inflight bytes in the last 1 minute.",
			Objectives: map[float64]float64{1.0: 0.1},
			MaxAge:     time.Minute,
		}),
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

	// A request arriving on this RPC is in the flat form by definition, so it is wrapped
	// into the internal one here. Every attribute is already on every entry, so each
	// stream becomes a single group and scope with nothing shared.
	internal := &logproto.InternalPushRequest{
		Streams: make([]logproto.InternalStreamAdapter, 0, len(req.Streams)),
	}
	for i := range req.Streams {
		internal.Streams = append(internal.Streams, logproto.FromStream(req.Streams[i]))
	}

	return d.pushWithResolver(ctx, internal, newRequestScopedStreamResolver(tenantID, d.validator.Limits, d.logger), constants.Loki)
}

// Push a set of streams.
// Can modify the input req parameter.
// The returned error is the last one seen.
func (d *Distributor) pushWithResolver(ctx context.Context, internalReq *logproto.InternalPushRequest, streamResolver *requestScopedStreamResolver, format string) (*logproto.PushResponse, error) {
	requestSize := int64(internalReq.Size())
	newInflightBytes := d.inflightBytes.Add(requestSize)
	d.inflightBytesHighWatermark.Observe(float64(newInflightBytes))
	defer d.inflightBytes.Add(-requestSize)

	maxInflightBytes := int64(d.cfg.MaxInflightBytes)
	if maxInflightBytes > 0 && newInflightBytes > maxInflightBytes {
		return nil, errServiceUnavailableMaxLoad
	}

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	defer d.waitSimulatedLatency(ctx, tenantID, start)

	// Return early if request does not contain any streams
	if len(internalReq.Streams) == 0 {
		d.m.zeroStreamCount.WithLabelValues(tenantID, "pre-validation").Inc()
		return &logproto.PushResponse{}, httpgrpc.Errorf(http.StatusUnprocessableEntity, validation.MissingStreamsErrorMsg)
	}

	// First we flatten out the request into a list of samples.
	// We use the heuristic of 1 sample per TS to size the array.
	// We also work out the hash value at the same time.
	streams := make([]KeyedStream, 0, len(internalReq.Streams))

	var validationErrors util.GroupedErrors

	now := time.Now()
	validationContext := d.validator.getValidationContextForTime(now, tenantID)
	validationContext.deferOTLPExpansion = d.cfg.DeferOTLPAttributeExpansion
	fieldDetector := newFieldDetector(validationContext)
	shouldDiscoverLevels := fieldDetector.shouldDiscoverLogLevels()
	shouldDiscoverGenericFields := fieldDetector.shouldDiscoverGenericFields()

	// The shard-streams config is resolved per stream from its policy (see PolicyShardStreams)
	// and threaded into these closures, so a policy can override the tenant sharding behavior
	// (e.g. toggle time sharding or use a different desired_rate).
	maybeShardByRate := func(stream logproto.InternalStreamAdapter, pushSize int, policy string, shardStreamsCfg shardstreams.Config) {
		if shardStreamsCfg.Enabled {
			streams = append(streams, d.shardStream(stream, pushSize, tenantID, policy, shardStreamsCfg)...)
			return
		}
		streams = append(streams, KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, stream.Labels),
			HashKeyNoShard: stream.Hash,
			Stream:         stream,
			Policy:         policy,
		})
	}

	maybeShardStreams := func(stream logproto.InternalStreamAdapter, labels labels.Labels, pushSize int, policy string, shardStreamsCfg shardstreams.Config) {
		// Backfill streams implement time sharding on the client side (via the
		// constants.BackfillShardLabel), so Loki's own time sharding is disabled for them to avoid
		// exploding stream cardinality. Rate-based sharding still applies.
		if !shardStreamsCfg.TimeShardingEnabled || labels.Has(constants.BackfillLabel) {
			maybeShardByRate(stream, pushSize, policy, shardStreamsCfg)
			return
		}

		ignoreRecentFrom := now.Add(-shardStreamsCfg.TimeShardingIgnoreRecent)
		streamsByTime, ok := shardStreamByTime(stream, labels, d.ingesterCfg.MaxChunkAge/2, ignoreRecentFrom)
		if !ok {
			maybeShardByRate(stream, pushSize, policy, shardStreamsCfg)
			return
		}

		for _, ts := range streamsByTime {
			maybeShardByRate(ts.Stream, ts.linesTotalLen, policy, shardStreamsCfg)
		}
	}

	var ingestionBlockedError error

	// Ingestion rate limiting is bucketed by the effective rate-limit target: streams whose
	// resolved policy has a per-policy ingestion rate override are metered against their own
	// per-(tenant,policy) bucket, replacing the tenant-wide limit; all other streams share the
	// tenant-wide bucket (keyed by "").
	rlBuckets := map[string]*rateLimitBucket{}

	err = func() error {
		sp := trace.SpanFromContext(ctx)
		sp.AddEvent("start to validate request")
		defer sp.AddEvent("finished to validate request")

		for streamIdx := range internalReq.Streams {
			stream := &internalReq.Streams[streamIdx]

			// Return early if stream does not contain any entries
			if stream.EntryCount() == 0 {
				continue
			}

			// Truncate first so subsequent steps have consistent line lengths
			d.truncateLines(validationContext, stream)

			var lbs labels.Labels
			var retentionHours, policy string
			lbs, stream.Labels, stream.Hash, retentionHours, policy, err = d.parseStreamLabels(ctx, validationContext, stream.Labels, stream, streamResolver, format)
			if err != nil {
				d.writeFailuresManager.Log(tenantID, err)
				validationErrors.Add(err)
				d.validator.reportDiscardedDataWithTracker(ctx, validation.InvalidLabels, validationContext, lbs, retentionHours, policy, stream.AccountedSize(d.cfg.DeferOTLPAttributeExpansion), stream.EntryCount(), format)
				continue
			}

			if !d.validator.IsInternalStream(lbs) {
				if missing, lbsMissing := d.missingEnforcedLabels(lbs, tenantID, policy); missing {
					err := fmt.Errorf(validation.MissingEnforcedLabelsErrorMsg, strings.Join(lbsMissing, ","), tenantID, stream.Labels, policy)
					d.writeFailuresManager.Log(tenantID, err)
					validationErrors.Add(err)
					d.validator.reportDiscardedDataWithTracker(ctx, validation.MissingEnforcedLabels, validationContext, lbs, retentionHours, policy, stream.AccountedSize(d.cfg.DeferOTLPAttributeExpansion), stream.EntryCount(), format)
					continue
				}
			}

			if block, statusCode, reason, err := d.validator.ShouldBlockIngestion(validationContext, now, policy); block {
				d.writeFailuresManager.Log(tenantID, err)
				d.validator.reportDiscardedDataWithTracker(ctx, reason, validationContext, lbs, retentionHours, policy, stream.AccountedSize(d.cfg.DeferOTLPAttributeExpansion), stream.EntryCount(), format)

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

			// Backfilled data is expected to be older than reject_old_samples_max_age. The backfill
			// label is reserved: the push parsers reject streams that already carry it, so it can
			// only originate from the X-Loki-Backfill-Shard header and cannot be spoofed to bypass
			// validation. Entries too far in the future are still rejected.
			entryValidationContext := validationContext
			if lbs.Has(constants.BackfillLabel) {
				entryValidationContext.rejectOldSample = false
			}

			labelNamer := otlptranslator.LabelNamer{}

			// Sanitise each shared attribute set once for the group that owns it, instead of
			// once for every entry beneath it. Only when the attributes will stay on the
			// group: otherwise they are merged onto each entry below and sanitised there,
			// which is what preserves today's behaviour exactly — one builder over the
			// combined set, so a name normalising into a collision still collapses.
			var sanitizeErr error
			if d.cfg.DeferOTLPAttributeExpansion {
				stream.RewriteSharedAttrs(func(attrs []logproto.LabelAdapter) []logproto.LabelAdapter {
					if sanitizeErr != nil || len(attrs) == 0 {
						return attrs
					}
					sanitized, err := d.sanitizeStructuredMetadata(tenantID, format, attrs, labelNamer)
					if err != nil {
						sanitizeErr = err
						return attrs
					}
					return sanitized
				})
			}
			if sanitizeErr != nil {
				return sanitizeErr
			}

			var mdBuf []logproto.LabelAdapter
			dropped := stream.Filter(func(res *logproto.ResourceLogs, scope *logproto.ScopeLogs, entry *logproto.Entry) bool {
				if sanitizeErr != nil {
					return false
				}

				if err := d.validator.ValidateEntry(ctx, entryValidationContext, lbs, *entry, retentionHours, policy, format); err != nil {
					d.writeFailuresManager.Log(tenantID, err)
					validationErrors.Add(err)
					return false
				}

				// With the attributes staying on the group, only the entry's own metadata is
				// sanitised here. Otherwise the effective set is merged and sanitised as one,
				// which is what the flattened form has always done.
				toSanitize := entry.StructuredMetadata
				if !d.cfg.DeferOTLPAttributeExpansion {
					mdBuf = logproto.AppendEffectiveMetadata(mdBuf[:0], res.Attrs, scope.Attrs, entry)
					toSanitize = mdBuf
				}
				sanitized, err := d.sanitizeStructuredMetadata(tenantID, format, toSanitize, labelNamer)
				if err != nil {
					sanitizeErr = err
					return false
				}
				entry.StructuredMetadata = sanitized

				if shouldDiscoverLevels || shouldDiscoverGenericFields {
					// Detection reads the entry's effective metadata, so an attribute that
					// arrived on the resource or the scope is visible to it — which is what
					// it saw when those attributes were copied onto the entry. When they were
					// just merged above, the entry's own set is already that.
					if d.cfg.DeferOTLPAttributeExpansion {
						mdBuf = logproto.AppendEffectiveMetadata(mdBuf[:0], res.Attrs, scope.Attrs, entry)
					} else {
						mdBuf = append(mdBuf[:0], entry.StructuredMetadata...)
					}
					effective := logproto.FromLabelAdaptersToLabels(mdBuf)

					if shouldDiscoverLevels {
						pprof.Do(ctx, pprof.Labels("action", "discover_log_level"), func(_ context.Context) {
							logLevel, ok := fieldDetector.extractLogLevel(lbs, effective, *entry)
							if ok {
								entry.StructuredMetadata = append(entry.StructuredMetadata, logLevel)
							}
						})
					}
					if shouldDiscoverGenericFields {
						pprof.Do(ctx, pprof.Labels("action", "discover_generic_fields"), func(_ context.Context) {
							for field, hints := range fieldDetector.validationContext.discoverGenericFields {
								extracted, ok := fieldDetector.extractGenericField(field, hints, lbs, effective, *entry)
								if ok {
									entry.StructuredMetadata = append(entry.StructuredMetadata, extracted)
								}
							}
						})
					}
				}

				return true
			})
			_ = dropped
			if sanitizeErr != nil {
				return sanitizeErr
			}

			if !d.cfg.DeferOTLPAttributeExpansion {
				// Every entry now carries what applied to it, so leaving the group's copies in
				// place would count and write them twice.
				stream.ClearSharedAttrs()
			}

			// If configured for this tenant, increment duplicate timestamps. Note, this is imperfect
			// since Loki will accept out of order writes it doesn't account for separate
			// pushes with overlapping time ranges having entries with duplicate timestamps
			if validationContext.incrementDuplicateTimestamps {
				stream.EnforceTimestampOrder()
			}

			// The tenant-facing number: what they sent, with a shared attribute set counted
			// once rather than once for every entry beneath it.
			streamEntriesSize := stream.AccountedSize(d.cfg.DeferOTLPAttributeExpansion)
			n := stream.EntryCount()
			if n == 0 {
				// Empty stream after validating all the entries
				continue
			}

			// Attribute this stream's bytes/lines to its rate-limit bucket.
			_, hasRateOverride := d.validator.PolicyIngestionRateBytes(tenantID, policy)
			bucketKey := ""
			if hasRateOverride {
				bucketKey = policy
			}
			b := rlBuckets[bucketKey]
			if b == nil {
				b = &rateLimitBucket{policy: policy, hasOverride: hasRateOverride}
				rlBuckets[bucketKey] = b
			}
			b.bytes += streamEntriesSize
			b.lines += n

			shardCfg, _ := d.validator.PolicyShardStreams(tenantID, policy)
			// Sharding is sized on the expanded measure whatever the config says, so that
			// deferring the expansion does not change how traffic spreads across ingesters.
			maybeShardStreams(*stream, lbs, stream.ExpandedSize(), policy, shardCfg)
		}
		return nil
	}()
	if err != nil {
		return nil, err
	}

	var validationErr error
	if validationErrors.Err() != nil {
		validationErr = httpgrpc.Errorf(http.StatusBadRequest, "%s", validationErrors.Error())
	} else if ingestionBlockedError != nil {
		// Any validation error takes precedence over the status code and error message for blocked ingestion.
		validationErr = ingestionBlockedError
	}

	// Return early if none of the streams contained entries
	if len(streams) == 0 {
		d.m.zeroStreamCount.WithLabelValues(tenantID, "post-validation").Inc()
		return &logproto.PushResponse{}, validationErr
	}

	if err := d.enforceIngestionRateLimits(ctx, now, tenantID, rlBuckets, internalReq.Streams, validationContext, streamResolver, format); err != nil {
		return nil, err
	}

	// These limits are checked after the ingestion rate limit as this
	// is how it works in ingesters.
	if d.cfg.IngestLimitsEnabled {
		accepted, rejected, err := d.ingestLimits.EnforceLimits(ctx, tenantID, streams)
		if err == nil && !d.cfg.IngestLimitsDryRunEnabled {
			if len(rejected) > 0 {
				discardedStreams := make([]logproto.InternalStreamAdapter, 0, len(rejected))
				for _, stream := range rejected {
					discardedStreams = append(discardedStreams, stream.Stream)
				}
				d.trackDiscardedData(ctx, discardedStreams, validationContext, tenantID, validation.StreamLimit, streamResolver, format, nil)

				// While many streams may have failed we only log the error for one stream in the insight logs and in the error message.
				// It's generally not useful to know the stream labels for a stream that is hitting the stream limit as it could be any
				// stream and isn't necessarily a stream with high cardinality. However, it might also be a high cardinality stream so returning
				// something here still may be useful. We used to return nothing with this limit and people requested that something is better than nothing.
				err = fmt.Errorf(validation.StreamLimitErrorMsg, rejected[0].Stream.Labels, tenantID)
				d.writeFailuresManager.Log(tenantID, err)
				// Set the validation error to the stream limit error so it is returned to the client.
				validationErr = httpgrpc.Error(http.StatusTooManyRequests, err.Error())
				// If none of the streams were accepted, return early.
				if len(accepted) == 0 {
					return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "%s", err.Error())
				}
			}
			streams = accepted
		}
	}

	tracker := PushTracker{
		done: make(chan struct{}, 1), // buffer avoids blocking if caller terminates - sendSamples() only sends once on each
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
		// We don't need to create a new context like the ingester writes, because we don't return unless all writes have succeeded.
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
		return &logproto.PushResponse{}, validationErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	policy      string
	hasOverride bool
	bytes       int
	lines       int
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
	// own policy label, handled by trackDiscardedData).
	d.trackDiscardedData(ctx, streams, validationContext, tenantID, validation.RateLimited, streamResolver, format, nil)

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
	// Return a 429 to indicate to the client they are being rate limited
	return httpgrpc.Errorf(http.StatusTooManyRequests, "%s", err.Error())
}

// trackDiscardedData tracks discarded samples and bytes. When policyMatch is non-nil, only
// streams whose resolved policy satisfies it are tracked (used to attribute discards to a
// single ingestion rate-limit bucket); a nil policyMatch tracks all streams.
func (d *Distributor) trackDiscardedData(
	ctx context.Context,
	streams []logproto.InternalStreamAdapter,
	validationContext validationContext,
	tenantID string,
	reason string,
	streamResolver push.StreamResolver,
	format string,
	policyMatch func(policy string) bool,
) {
	for _, stream := range streams {
		lbs, _, _, retentionHours, policy, err := d.parseStreamLabels(ctx, validationContext, stream.Labels, &stream, streamResolver, format)
		if err != nil {
			level.Warn(d.logger).Log("msg", "failed to parse stream labels when tracking discarded samples and bytes, this data will not be tracked", "error", err, "stream", stream.Labels)
			continue
		}
		if policyMatch != nil && !policyMatch(policy) {
			continue
		}
		discardedStreamBytes := stream.AccountedSize(d.cfg.DeferOTLPAttributeExpansion)
		validation.DiscardedSamples.WithLabelValues(reason, tenantID, retentionHours, policy, format).Add(float64(stream.EntryCount()))
		validation.DiscardedBytes.WithLabelValues(reason, tenantID, retentionHours, policy, format).Add(float64(discardedStreamBytes))
		if d.usageTracker != nil {
			d.usageTracker.DiscardedBytesAdd(ctx, tenantID, reason, lbs, float64(discardedStreamBytes), format)
		}
	}
}

type streamWithTimeShard struct {
	Stream        logproto.InternalStreamAdapter
	linesTotalLen int
}

// shardIdentity is the labels and hash a shard will carry. The entries arrive separately from
// Divide, so the identity has to be held while the shards are being worked out.
type shardIdentity struct {
	labels string
	hash   uint64
}

// This shards the stream into multiple sub-streams based on the log timestamps.
//
// Which shard an entry lands in is decided from a sorted view of the entries rather than by
// sorting the stream: an entry cannot leave the resource and scope whose attributes apply to
// it. Each shard is then ordered within itself, which for a stream of one group and scope —
// every native push — is the same total order the entries used to be put in.
//
// If the second result is false, it means that either there were no logs in the
// stream, or all of the logs in the stream occurred after the given value of
// ignoreLogsFrom, so there was no need to shard - the original `streams` value
// can be used.
func shardStreamByTime(stream logproto.InternalStreamAdapter, lbls labels.Labels, timeShardLen time.Duration, ignoreLogsFrom time.Time) ([]streamWithTimeShard, bool) {
	entriesLen := stream.EntryCount()
	if entriesLen == 0 {
		return nil, false
	}

	// One record per entry: where it sits in containment order, when it happened, and how
	// long its line is. Sorting these leaves the entries themselves untouched.
	type positioned struct {
		pos      int
		ts       time.Time
		lineSize int
	}
	order := make([]positioned, 0, entriesLen)
	pos := 0
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			entries := stream.ResourceLogs[i].ScopeLogs[j].Entries
			for k := range entries {
				order = append(order, positioned{pos: pos, ts: entries[k].Timestamp, lineSize: len(entries[k].Line)})
				pos++
			}
		}
	}
	slices.SortStableFunc(order, func(a, b positioned) int { return a.ts.Compare(b.ts) })

	// Shortcut to do no work if all of the logs are recent
	if order[0].ts.After(ignoreLogsFrom) {
		return nil, false
	}

	shardCap := int((order[entriesLen-1].ts.Sub(order[0].ts) / timeShardLen) + 1)
	result := make([]streamWithTimeShard, 0, shardCap)
	identities := make([]shardIdentity, 0, shardCap)
	labelBuilder := labels.NewBuilder(lbls)

	// shardOf[pos] is the shard the entry at that position belongs to.
	shardOf := make([]int, entriesLen)

	startIdx := 0
	for startIdx < entriesLen && order[startIdx].ts.Before(ignoreLogsFrom) /* the index is changed below */ {
		timeShardStart := order[startIdx].ts.Truncate(timeShardLen)
		timeShardEnd := timeShardStart.Add(timeShardLen)

		timeShardCutoff := timeShardEnd
		if timeShardCutoff.After(ignoreLogsFrom) {
			// If the time_sharding_ignore_recent is in the middle of this
			// shard, we need to cut off the logs at that point.
			timeShardCutoff = ignoreLogsFrom
		}

		shard := len(result)
		endIdx := startIdx + 1
		linesTotalLen := order[startIdx].lineSize
		shardOf[order[startIdx].pos] = shard
		for ; endIdx < entriesLen && order[endIdx].ts.Before(timeShardCutoff); endIdx++ {
			linesTotalLen += order[endIdx].lineSize
			shardOf[order[endIdx].pos] = shard
		}

		shardLbls := labelBuilder.Set(timeShardLabel, fmt.Sprintf("%d_%d", timeShardStart.Unix(), timeShardEnd.Unix())).Labels()
		result = append(result, streamWithTimeShard{linesTotalLen: linesTotalLen})
		identities = append(identities, shardIdentity{labels: shardLbls.String(), hash: labels.StableHash(shardLbls)})

		startIdx = endIdx
	}

	// Everything at or after ignoreLogsFrom goes into one last shard, which keeps the
	// stream's own labels rather than gaining a time shard label.
	if startIdx < entriesLen {
		shard := len(result)
		logsWithoutTimeShardLen := 0
		for i := startIdx; i < entriesLen; i++ {
			logsWithoutTimeShardLen += order[i].lineSize
			shardOf[order[i].pos] = shard
		}
		result = append(result, streamWithTimeShard{linesTotalLen: logsWithoutTimeShardLen})
		identities = append(identities, shardIdentity{labels: stream.Labels, hash: stream.Hash})
	}

	divided := stream.Divide(len(result), func(idx int, _ *logproto.Entry) int { return shardOf[idx] })
	for i := range result {
		divided[i].Labels, divided[i].Hash = identities[i].labels, identities[i].hash
		divided[i].SortByTimestamp()
		result[i].Stream = divided[i]
	}

	return result, true
}

// shardStream shards (divides) the given stream into N smaller streams, where
// N is the sharding size for the given stream. shardSteam returns the smaller
// streams and their associated keys for hashing to ingesters.
//
// The number of shards is limited by the number of entries.
func (d *Distributor) shardStream(stream logproto.InternalStreamAdapter, pushSize int, tenantID string, policy string, shardStreamsCfg shardstreams.Config) []KeyedStream {
	logger := log.With(util_log.WithUserID(tenantID, d.logger), "stream", stream.Labels)
	shardCount := d.shardCountFor(logger, &stream, pushSize, tenantID, shardStreamsCfg)

	if shardCount <= 1 {
		return []KeyedStream{{HashKey: lokiring.TokenFor(tenantID, stream.Labels), HashKeyNoShard: stream.Hash, Stream: stream, Policy: policy}}
	}

	d.m.streamShardCount.Inc()
	if shardStreamsCfg.LoggingEnabled {
		level.Info(logger).Log("msg", "sharding request", "shard_count", shardCount)
	}

	return d.divideEntriesBetweenShards(tenantID, shardCount, shardStreamsCfg, stream, policy)
}

func (d *Distributor) divideEntriesBetweenShards(tenantID string, totalShards int, shardStreamsCfg shardstreams.Config, stream logproto.InternalStreamAdapter, policy string) []KeyedStream {
	derivedStreams, identities := d.createShards(stream, totalShards, tenantID, shardStreamsCfg, policy)
	if len(derivedStreams) == 0 {
		return derivedStreams
	}

	// Round robin, as before: entry i goes to shard i%N.
	parts := stream.Divide(len(derivedStreams), func(idx int, _ *logproto.Entry) int {
		return idx % len(derivedStreams)
	})
	for i := range derivedStreams {
		parts[i].Labels, parts[i].Hash = identities[i].labels, identities[i].hash
		derivedStreams[i].Stream = parts[i]
	}

	return derivedStreams
}

func (d *Distributor) createShards(stream logproto.InternalStreamAdapter, totalShards int, tenantID string, shardStreamsCfg shardstreams.Config, policy string) ([]KeyedStream, []shardIdentity) {
	var (
		streamLabels   = labelTemplate(stream.Labels, d.logger)
		streamPattern  = streamLabels.String()
		derivedStreams = make([]KeyedStream, 0, totalShards)
		identities     = make([]shardIdentity, 0, totalShards)

		streamCount = streamCount(totalShards, stream)
	)

	if totalShards <= 0 {
		level.Error(d.logger).Log("msg", "attempt to create shard with zeroed total shards", "org_id", tenantID, "stream", stream.Labels, "entries_len", stream.EntryCount())
		return derivedStreams, identities
	}

	startShard := d.shardTracker.LastShardNum(tenantID, stream.Hash)
	for i := 0; i < streamCount; i++ {
		shardNum := (startShard + i) % totalShards
		shard := d.createShard(streamLabels, streamPattern, shardNum)

		derivedStreams = append(derivedStreams, KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, shard.labels),
			HashKeyNoShard: stream.Hash,
			Policy:         policy,
		})
		identities = append(identities, shard)

		if shardStreamsCfg.LoggingEnabled {
			level.Info(d.logger).Log("msg", "stream derived from sharding", "src-stream", stream.Labels, "derived-stream", shard.labels)
		}
	}
	d.shardTracker.SetLastShardNum(tenantID, stream.Hash, startShard+streamCount)

	return derivedStreams, identities
}

func streamCount(totalShards int, stream logproto.InternalStreamAdapter) int {
	if stream.EntryCount() < totalShards {
		return stream.EntryCount()
	}
	return totalShards
}

// labelTemplate returns a label set that includes the dummy label to be replaced
// To avoid allocations, this slice is reused when we know the stream value
func labelTemplate(lbls string, logger log.Logger) labels.Labels {
	baseLbls, err := syntax.ParseLabels(lbls)
	if err != nil {
		level.Error(logger).Log("msg", "couldn't extract labels from stream", "stream", lbls)
		return labels.EmptyLabels()
	}

	builder := labels.NewBuilder(baseLbls)
	builder.Set(ingester.ShardLbName, ingester.ShardLbPlaceholder)
	return builder.Labels()
}

// createShard works out the labels and hash one shard will carry. Divide sizes and fills the
// entries, so nothing is pre-allocated here.
func (d *Distributor) createShard(lbls labels.Labels, streamPattern string, shardNumber int) shardIdentity {
	shardLabel := strconv.Itoa(shardNumber)

	builder := labels.NewBuilder(lbls)
	if lbls.Has(ingester.ShardLbName) {
		builder.Set(ingester.ShardLbName, shardLabel)
	}
	lbls = builder.Labels()

	return shardIdentity{
		labels: strings.Replace(streamPattern, ingester.ShardLbPlaceholder, shardLabel, 1),
		hash:   labels.StableHash(lbls),
	}
}

func (d *Distributor) truncateLines(vContext validationContext, stream *logproto.InternalStreamAdapter) {
	if !vContext.maxLineSizeTruncate {
		return
	}

	truncatedSamples, truncatedBytes := stream.TruncateLines(vContext.maxLineSize, vContext.maxLineSizeTruncateIdentifier)

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
		// The ingester's push RPC takes flat entries, so the attributes are expanded here.
		// A second RPC carrying the nested form is what removes this.
		req.Streams[i] = s.Stream.ToStream()
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

		// The one decision the config makes: keep the attributes stored once per group, or
		// expand them onto every entry so that consumers reading only the old format still
		// understand the record.
		var streamRecords []*kgo.Record
		if d.cfg.DeferOTLPAttributeExpansion {
			streamRecords, err = kafka.EncodeInternal(partition, tenant, stream.Stream, d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes)
		} else {
			streamRecords, err = kafka.Encode(partition, tenant, stream.Stream.ToStream(), d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes)
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

// parseStreamLabels parses stream labels using a request-scoped policy resolver
func (d *Distributor) parseStreamLabels(ctx context.Context, vContext validationContext, key string, stream *logproto.InternalStreamAdapter, streamResolver push.StreamResolver, format string) (labels.Labels, string, uint64, string, string, error) {
	if val, ok := d.labelCache.Get(key); ok {
		retentionHours := streamResolver.RetentionHoursFor(val.ls)
		policy := streamResolver.PolicyFor(ctx, val.ls)
		return val.ls, val.ls.String(), val.hash, retentionHours, policy, nil
	}

	ls, err := syntax.ParseLabels(key)
	if err != nil {
		retentionHours := d.tenantsRetention.RetentionHoursFor(vContext.userID, labels.EmptyLabels())
		// TODO: check for global policy.
		return labels.EmptyLabels(), "", 0, retentionHours, "", fmt.Errorf(validation.InvalidLabelsErrorMsg, key, err)
	}

	policy := streamResolver.PolicyFor(ctx, ls)
	retentionHours := d.tenantsRetention.RetentionHoursFor(vContext.userID, ls)

	if err := d.validator.ValidateLabels(vContext, ls, stream, retentionHours, policy, format); err != nil {
		return labels.EmptyLabels(), "", 0, retentionHours, policy, err
	}

	lsHash := labels.StableHash(ls)

	d.labelCache.Add(key, labelData{ls, lsHash})
	return ls, ls.String(), lsHash, retentionHours, policy, nil
}

// shardCountFor returns the right number of shards to be used by the given stream.
//
// It first checks if the number of shards is present in the shard store. If it isn't it will calculate it
// based on the rate stored in the rate store and will store the new evaluated number of shards.
//
// desiredRate is expected to be given in bytes.
func (d *Distributor) shardCountFor(logger log.Logger, stream *logproto.InternalStreamAdapter, pushSize int, tenantID string, streamShardcfg shardstreams.Config) int {
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

	shards := calculateShards(rate, int(float64(pushSize)*pushRate), streamShardcfg.DesiredRate.Val())
	if shards == 0 {
		// 1 shard is enough for the given stream.
		return 1
	}

	return shards
}

func calculateShards(rate int64, pushSize, desiredRate int) int {
	shards := float64(rate+int64(pushSize)) / float64(desiredRate)
	if shards <= 1 {
		return 1
	}
	return int(math.Ceil(shards))
}

// calculateStreamSizes reports the line bytes and the metadata bytes the limits service should
// meter. The metadata figure counts each shared attribute set the way it will be written, so
// that what the service records matches what leaves the distributor.
func calculateStreamSizes(stream logproto.InternalStreamAdapter, deferExpansion bool) (uint64, uint64) {
	var entriesSize, structuredMetadataSize uint64
	for i := range stream.ResourceLogs {
		res := &stream.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			for k := range scope.Entries {
				entry := &scope.Entries[k]
				entriesSize += uint64(len(entry.Line))
				// Everything that applies to the entry, so a resource or scope attribute
				// counts here just as it did when it sat on the entry itself.
				if deferExpansion {
					structuredMetadataSize += uint64(logproto.EffectiveMetadataSize(nil, nil, entry))
				} else {
					structuredMetadataSize += uint64(logproto.EffectiveMetadataSize(res.Attrs, scope.Attrs, entry))
				}
			}
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
