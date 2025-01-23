package distributor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"runtime/pprof"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	otlptranslate "github.com/prometheus/prometheus/storage/remote/otlptranslator/prometheus"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/status"
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
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	kafka_client "github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
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

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`

	// RateStore customizes the rate storing used by stream sharding.
	RateStore RateStoreConfig `yaml:"rate_store"`

	// WriteFailuresLoggingCfg customizes write failures logging behavior.
	WriteFailuresLogging writefailures.Cfg `yaml:"write_failures_logging" doc:"description=Customize the logging of write failures."`

	OTLPConfig push.GlobalOTLPConfig `yaml:"otlp_config"`

	KafkaEnabled    bool         `yaml:"kafka_writes_enabled"`
	IngesterEnabled bool         `yaml:"ingester_writes_enabled"`
	KafkaConfig     kafka.Config `yaml:"-"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.OTLPConfig.RegisterFlags(fs)
	cfg.DistributorRing.RegisterFlags(fs)
	cfg.RateStore.RegisterFlagsWithPrefix("distributor.rate-store", fs)
	cfg.WriteFailuresLogging.RegisterFlagsWithPrefix("distributor.write-failures-logging", fs)
	fs.IntVar(&cfg.PushWorkerCount, "distributor.push-worker-count", 256, "Number of workers to push batches to ingesters.")
	fs.BoolVar(&cfg.KafkaEnabled, "distributor.kafka-writes-enabled", false, "Enable writes to Kafka during Push requests.")
	fs.BoolVar(&cfg.IngesterEnabled, "distributor.ingester-writes-enabled", true, "Enable writes to Ingesters during Push requests. Defaults to true.")
}

func (cfg *Config) Validate() error {
	if !cfg.KafkaEnabled && !cfg.IngesterEnabled {
		return fmt.Errorf("at least one of kafka and ingestor writes must be enabled")
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
	clientCfg        client.Config
	tenantConfigs    *runtime.TenantConfigs
	tenantsRetention *retention.TenantsRetention
	ingestersRing    ring.ReadRing
	validator        *Validator
	pool             *ring_client.Pool
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

	RequestParserWrapper push.RequestParserWrapper

	// metrics
	ingesterAppends                       *prometheus.CounterVec
	ingesterAppendTimeouts                *prometheus.CounterVec
	replicationFactor                     prometheus.Gauge
	streamShardCount                      prometheus.Counter
	tenantPushSanitizedStructuredMetadata *prometheus.CounterVec

	usageTracker   push.UsageTracker
	ingesterTasks  chan pushIngesterTask
	ingesterTaskWg sync.WaitGroup

	// kafka
	kafkaWriter   KafkaProducer
	partitionRing ring.PartitionRingReader

	// kafka metrics
	kafkaAppends           *prometheus.CounterVec
	kafkaWriteBytesTotal   prometheus.Counter
	kafkaWriteLatency      prometheus.Histogram
	kafkaRecordsPerRequest prometheus.Histogram
}

// New a distributor creates.
func New(
	cfg Config,
	ingesterCfg ingester.Config,
	clientCfg client.Config,
	configs *runtime.TenantConfigs,
	ingestersRing ring.ReadRing,
	partitionRing ring.PartitionRingReader,
	overrides Limits,
	registerer prometheus.Registerer,
	metricsNamespace string,
	tee Tee,
	usageTracker push.UsageTracker,
	logger log.Logger,
) (*Distributor, error) {
	factory := cfg.factory
	if factory == nil {
		factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return client.New(clientCfg, addr)
		})
	}

	internalFactory := func(addr string) (ring_client.PoolClient, error) {
		internalCfg := clientCfg
		internalCfg.Internal = true
		return client.New(internalCfg, addr)
	}

	validator, err := NewValidator(overrides, usageTracker)
	if err != nil {
		return nil, err
	}

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

	var kafkaWriter KafkaProducer
	if cfg.KafkaEnabled {
		kafkaClient, err := kafka_client.NewWriterClient(cfg.KafkaConfig, 20, logger, registerer)
		if err != nil {
			return nil, fmt.Errorf("failed to start kafka client: %w", err)
		}
		kafkaWriter = kafka_client.NewProducer(kafkaClient, cfg.KafkaConfig.ProducerMaxBufferedBytes,
			prometheus.WrapRegistererWithPrefix("loki_", registerer))
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
		pool:                  clientpool.NewPool("ingester", clientCfg.PoolConfig, ingestersRing, factory, logger, metricsNamespace),
		labelCache:            labelCache,
		shardTracker:          NewShardTracker(),
		healthyInstancesCount: atomic.NewUint32(0),
		rateLimitStrat:        rateLimitStrat,
		tee:                   tee,
		usageTracker:          usageTracker,
		ingesterTasks:         make(chan pushIngesterTask),
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
		tenantPushSanitizedStructuredMetadata: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_structured_metadata_sanitized_total",
			Help:      "The total number of times we've had to sanitize structured metadata (names or values) at ingestion time per tenant.",
		}, []string{"tenant"}),
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
		writeFailuresManager: writefailures.NewManager(logger, registerer, cfg.WriteFailuresLogging, configs, "distributor"),
		kafkaWriter:          kafkaWriter,
		partitionRing:        partitionRing,
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

	d.replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	rfStats.Set(int64(ingestersRing.ReplicationFactor()))

	rs := NewRateStore(
		d.cfg.RateStore,
		ingestersRing,
		clientpool.NewPool(
			"rate-store",
			clientCfg.PoolConfig,
			ingestersRing,
			ring_client.PoolAddrFunc(internalFactory),
			logger,
			metricsNamespace,
		),
		overrides,
		registerer,
	)
	d.rateStore = rs

	servs = append(servs, d.pool, rs)
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
	d.ingesterTaskWg.Add(d.cfg.PushWorkerCount)
	for i := 0; i < d.cfg.PushWorkerCount; i++ {
		go d.pushIngesterWorker(ctx)
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
	HashKey uint32
	Stream  logproto.Stream
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
type pushTracker struct {
	streamsPending atomic.Int32
	streamsFailed  atomic.Int32
	done           chan struct{}
	err            chan error
}

// doneWithResult records the result of a stream push.
// If err is nil, the stream push is considered successful.
// If err is not nil, the stream push is considered failed.
func (p *pushTracker) doneWithResult(err error) {
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

// Push a set of streams.
// The returned error is the last one seen.
func (d *Distributor) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Return early if request does not contain any streams
	if len(req.Streams) == 0 {
		return &logproto.PushResponse{}, httpgrpc.Errorf(http.StatusUnprocessableEntity, validation.MissingStreamsErrorMsg)
	}

	// First we flatten out the request into a list of samples.
	// We use the heuristic of 1 sample per TS to size the array.
	// We also work out the hash value at the same time.
	streams := make([]KeyedStream, 0, len(req.Streams))
	validatedLineSize := 0
	validatedLineCount := 0

	var validationErrors util.GroupedErrors

	now := time.Now()
	validationContext := d.validator.getValidationContextForTime(now, tenantID)
	fieldDetector := newFieldDetector(validationContext)
	shouldDiscoverLevels := fieldDetector.shouldDiscoverLogLevels()
	shouldDiscoverGenericFields := fieldDetector.shouldDiscoverGenericFields()

	shardStreamsCfg := d.validator.Limits.ShardStreams(tenantID)
	maybeShardByRate := func(stream logproto.Stream, pushSize int) {
		if shardStreamsCfg.Enabled {
			streams = append(streams, d.shardStream(stream, pushSize, tenantID)...)
			return
		}
		streams = append(streams, KeyedStream{
			HashKey: lokiring.TokenFor(tenantID, stream.Labels),
			Stream:  stream,
		})
	}

	maybeShardStreams := func(stream logproto.Stream, labels labels.Labels, pushSize int) {
		if !shardStreamsCfg.TimeShardingEnabled {
			maybeShardByRate(stream, pushSize)
			return
		}

		ignoreRecentFrom := now.Add(-shardStreamsCfg.TimeShardingIgnoreRecent)
		streamsByTime, ok := shardStreamByTime(stream, labels, d.ingesterCfg.MaxChunkAge/2, ignoreRecentFrom)
		if !ok {
			maybeShardByRate(stream, pushSize)
			return
		}

		for _, ts := range streamsByTime {
			maybeShardByRate(ts.Stream, ts.linesTotalLen)
		}
	}

	func() {
		sp := opentracing.SpanFromContext(ctx)
		if sp != nil {
			sp.LogKV("event", "start to validate request")
			defer func() {
				sp.LogKV("event", "finished to validate request")
			}()
		}

		for _, stream := range req.Streams {
			// Return early if stream does not contain any entries
			if len(stream.Entries) == 0 {
				continue
			}

			// Truncate first so subsequent steps have consistent line lengths
			d.truncateLines(validationContext, &stream)

			var lbs labels.Labels
			lbs, stream.Labels, stream.Hash, err = d.parseStreamLabels(validationContext, stream.Labels, stream)
			if err != nil {
				d.writeFailuresManager.Log(tenantID, err)
				validationErrors.Add(err)
				validation.DiscardedSamples.WithLabelValues(validation.InvalidLabels, tenantID).Add(float64(len(stream.Entries)))
				discardedBytes := util.EntriesTotalSize(stream.Entries)
				validation.DiscardedBytes.WithLabelValues(validation.InvalidLabels, tenantID).Add(float64(discardedBytes))
				continue
			}

			if missing, lbsMissing := d.missingEnforcedLabels(lbs, tenantID); missing {
				err := fmt.Errorf(validation.MissingEnforcedLabelsErrorMsg, strings.Join(lbsMissing, ","), tenantID)
				d.writeFailuresManager.Log(tenantID, err)
				validationErrors.Add(err)
				validation.DiscardedSamples.WithLabelValues(validation.MissingEnforcedLabels, tenantID).Add(float64(len(stream.Entries)))
				discardedBytes := util.EntriesTotalSize(stream.Entries)
				validation.DiscardedBytes.WithLabelValues(validation.MissingEnforcedLabels, tenantID).Add(float64(discardedBytes))
				continue
			}

			n := 0
			pushSize := 0
			prevTs := stream.Entries[0].Timestamp

			for _, entry := range stream.Entries {
				if err := d.validator.ValidateEntry(ctx, validationContext, lbs, entry); err != nil {
					d.writeFailuresManager.Log(tenantID, err)
					validationErrors.Add(err)
					continue
				}

				var normalized string
				structuredMetadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)
				for i := range entry.StructuredMetadata {
					normalized = otlptranslate.NormalizeLabel(structuredMetadata[i].Name)
					if normalized != structuredMetadata[i].Name {
						structuredMetadata[i].Name = normalized
						d.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID).Inc()
					}
					if strings.ContainsRune(structuredMetadata[i].Value, utf8.RuneError) {
						structuredMetadata[i].Value = strings.Map(removeInvalidUtf, structuredMetadata[i].Value)
						d.tenantPushSanitizedStructuredMetadata.WithLabelValues(tenantID).Inc()
					}
				}
				if shouldDiscoverLevels {
					pprof.Do(ctx, pprof.Labels("action", "discover_log_level"), func(_ context.Context) {
						logLevel, ok := fieldDetector.extractLogLevel(lbs, structuredMetadata, entry)
						if ok {
							entry.StructuredMetadata = append(entry.StructuredMetadata, logLevel)
						}
					})
				}
				if shouldDiscoverGenericFields {
					pprof.Do(ctx, pprof.Labels("action", "discover_generic_fields"), func(_ context.Context) {
						for field, hints := range fieldDetector.validationContext.discoverGenericFields {
							extracted, ok := fieldDetector.extractGenericField(field, hints, lbs, structuredMetadata, entry)
							if ok {
								entry.StructuredMetadata = append(entry.StructuredMetadata, extracted)
							}
						}
					})
				}
				stream.Entries[n] = entry

				// If configured for this tenant, increment duplicate timestamps. Note, this is imperfect
				// since Loki will accept out of order writes it doesn't account for separate
				// pushes with overlapping time ranges having entries with duplicate timestamps

				if validationContext.incrementDuplicateTimestamps && n != 0 {
					// Traditional logic for Loki is that 2 lines with the same timestamp and
					// exact same content will be de-duplicated, (i.e. only one will be stored, others dropped)
					// To maintain this behavior, only increment the timestamp if the log content is different
					if stream.Entries[n-1].Line != entry.Line && (entry.Timestamp == prevTs || entry.Timestamp == stream.Entries[n-1].Timestamp) {
						stream.Entries[n].Timestamp = stream.Entries[n-1].Timestamp.Add(1 * time.Nanosecond)
					} else {
						prevTs = entry.Timestamp
					}
				}

				n++
				validatedLineSize += util.EntryTotalSize(&entry)
				validatedLineCount++
				pushSize += len(entry.Line)
			}
			stream.Entries = stream.Entries[:n]
			if len(stream.Entries) == 0 {
				// Empty stream after validating all the entries
				continue
			}

			maybeShardStreams(stream, lbs, pushSize)
		}
	}()

	var validationErr error
	if validationErrors.Err() != nil {
		validationErr = httpgrpc.Errorf(http.StatusBadRequest, "%s", validationErrors.Error())
	}

	// Return early if none of the streams contained entries
	if len(streams) == 0 {
		return &logproto.PushResponse{}, validationErr
	}

	if block, until, retStatusCode := d.validator.ShouldBlockIngestion(validationContext, now); block {
		d.trackDiscardedData(ctx, req, validationContext, tenantID, validatedLineCount, validatedLineSize, validation.BlockedIngestion)

		err = fmt.Errorf(validation.BlockedIngestionErrorMsg, tenantID, until.Format(time.RFC3339), retStatusCode)
		d.writeFailuresManager.Log(tenantID, err)

		// If the status code is 200, return success.
		// Note that we still log the error and increment the metrics.
		if retStatusCode == http.StatusOK {
			return &logproto.PushResponse{}, nil
		}

		return nil, httpgrpc.Errorf(retStatusCode, "%s", err.Error())
	}

	if !d.ingestionRateLimiter.AllowN(now, tenantID, validatedLineSize) {
		d.trackDiscardedData(ctx, req, validationContext, tenantID, validatedLineCount, validatedLineSize, validation.RateLimited)

		err = fmt.Errorf(validation.RateLimitedErrorMsg, tenantID, int(d.ingestionRateLimiter.Limit(now, tenantID)), validatedLineCount, validatedLineSize)
		d.writeFailuresManager.Log(tenantID, err)
		// Return a 429 to indicate to the client they are being rate limited
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "%s", err.Error())
	}

	// Nil check for performance reasons, to avoid dynamic lookup and/or no-op
	// function calls that cannot be inlined.
	if d.tee != nil {
		d.tee.Duplicate(tenantID, streams)
	}

	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.InstanceDesc

	tracker := pushTracker{
		done: make(chan struct{}, 1), // buffer avoids blocking if caller terminates - sendSamples() only sends once on each
		err:  make(chan error, 1),
	}
	streamsToWrite := 0
	if d.cfg.IngesterEnabled {
		streamsToWrite += len(streams)
	}
	if d.cfg.KafkaEnabled {
		streamsToWrite += len(streams)
	}
	// We must correctly set streamsPending before beginning any writes to ensure we don't have a race between finishing all of one path before starting the other.
	tracker.streamsPending.Store(int32(streamsToWrite))

	if d.cfg.KafkaEnabled {
		subring, err := d.partitionRing.PartitionRing().ShuffleShard(tenantID, d.validator.IngestionPartitionsTenantShardSize(tenantID))
		if err != nil {
			return nil, err
		}
		// We don't need to create a new context like the ingester writes, because we don't return unless all writes have succeeded.
		d.sendStreamsToKafka(ctx, streams, tenantID, &tracker, subring)
	}

	if d.cfg.IngesterEnabled {
		streamTrackers := make([]streamTracker, len(streams))
		streamsByIngester := map[string][]*streamTracker{}
		ingesterDescs := map[string]ring.InstanceDesc{}

		if err := func() error {
			sp := opentracing.SpanFromContext(ctx)
			if sp != nil {
				sp.LogKV("event", "started to query ingesters ring")
				defer func() {
					sp.LogKV("event", "finished to query ingesters ring")
				}()
			}

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
				if sp := opentracing.SpanFromContext(ctx); sp != nil {
					localCtx = opentracing.ContextWithSpan(localCtx, sp)
				}
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
func (d *Distributor) missingEnforcedLabels(lbs labels.Labels, tenantID string) (bool, []string) {
	requiredLbs := d.validator.Limits.EnforcedLabels(tenantID)
	if len(requiredLbs) == 0 {
		// no enforced labels configured.
		return false, []string{}
	}

	missingLbs := []string{}

	for _, lb := range requiredLbs {
		if !lbs.Has(lb) {
			missingLbs = append(missingLbs, lb)
		}
	}

	return len(missingLbs) > 0, missingLbs
}

func (d *Distributor) trackDiscardedData(
	ctx context.Context,
	req *logproto.PushRequest,
	validationContext validationContext,
	tenantID string,
	validatedLineCount int,
	validatedLineSize int,
	reason string,
) {
	validation.DiscardedSamples.WithLabelValues(reason, tenantID).Add(float64(validatedLineCount))
	validation.DiscardedBytes.WithLabelValues(reason, tenantID).Add(float64(validatedLineSize))

	if d.usageTracker != nil {
		for _, stream := range req.Streams {
			lbs, _, _, err := d.parseStreamLabels(validationContext, stream.Labels, stream)
			if err != nil {
				continue
			}

			discardedStreamBytes := util.EntriesTotalSize(stream.Entries)

			if d.usageTracker != nil {
				d.usageTracker.DiscardedBytesAdd(ctx, tenantID, reason, lbs, float64(discardedStreamBytes))
			}
		}
	}
}

type streamWithTimeShard struct {
	logproto.Stream
	linesTotalLen int
}

// This should shard the stream into multiple sub-streams based on the log
// timestamps, but with no new alocations for the log entries. It will sort them
// in-place in the given stream object (so it may modify it!) and reference
// sub-slices of the same stream.Entries slice.
//
// If the second result is false, it means that either there were no logs in the
// stream, or all of the logs in the stream occurred after the given value of
// ignoreLogsFrom, so there was no need to shard - the original `streams` value
// can be used. However, due to the in-place logs sorting by their timestamp, it
// might still have been reordered.
func shardStreamByTime(stream logproto.Stream, lbls labels.Labels, timeShardLen time.Duration, ignoreLogsFrom time.Time) ([]streamWithTimeShard, bool) {
	entries := stream.Entries
	entriesLen := len(entries)
	if entriesLen == 0 {
		return nil, false
	}

	slices.SortStableFunc(entries, func(a, b logproto.Entry) int { return a.Timestamp.Compare(b.Timestamp) })

	// Shortcut to do no work if all of the logs are recent
	if entries[0].Timestamp.After(ignoreLogsFrom) {
		return nil, false
	}

	result := make([]streamWithTimeShard, 0, (entries[entriesLen-1].Timestamp.Sub(entries[0].Timestamp)/timeShardLen)+1)
	labelBuilder := labels.NewBuilder(lbls)

	startIdx := 0
	for startIdx < entriesLen && entries[startIdx].Timestamp.Before(ignoreLogsFrom) /* the index is changed below */ {
		timeShardStart := entries[startIdx].Timestamp.Truncate(timeShardLen)
		timeShardEnd := timeShardStart.Add(timeShardLen)

		timeShardCutoff := timeShardEnd
		if timeShardCutoff.After(ignoreLogsFrom) {
			// If the time_sharding_ignore_recent is in the middle of this
			// shard, we need to cut off the logs at that point.
			timeShardCutoff = ignoreLogsFrom
		}

		endIdx := startIdx + 1
		linesTotalLen := len(entries[startIdx].Line)
		for ; endIdx < entriesLen && entries[endIdx].Timestamp.Before(timeShardCutoff); endIdx++ {
			linesTotalLen += len(entries[endIdx].Line)
		}

		shardLbls := labelBuilder.Set(timeShardLabel, fmt.Sprintf("%d_%d", timeShardStart.Unix(), timeShardEnd.Unix())).Labels()
		result = append(result, streamWithTimeShard{
			Stream: logproto.Stream{
				Labels:  shardLbls.String(),
				Hash:    shardLbls.Hash(),
				Entries: stream.Entries[startIdx:endIdx],
			},
			linesTotalLen: linesTotalLen,
		})

		startIdx = endIdx
	}

	if startIdx == entriesLen {
		// We do not have any remaining entries
		return result, true
	}

	// Append one last shard with all of the logs without a time shard
	logsWithoutTimeShardLen := 0
	for i := startIdx; i < entriesLen; i++ {
		logsWithoutTimeShardLen += len(entries[i].Line)
	}

	return append(result, streamWithTimeShard{
		Stream: logproto.Stream{
			Labels:  stream.Labels,
			Hash:    stream.Hash,
			Entries: stream.Entries[startIdx:entriesLen],
		},
		linesTotalLen: logsWithoutTimeShardLen,
	}), true
}

// shardStream shards (divides) the given stream into N smaller streams, where
// N is the sharding size for the given stream. shardSteam returns the smaller
// streams and their associated keys for hashing to ingesters.
//
// The number of shards is limited by the number of entries.
func (d *Distributor) shardStream(stream logproto.Stream, pushSize int, tenantID string) []KeyedStream {
	shardStreamsCfg := d.validator.Limits.ShardStreams(tenantID)
	logger := log.With(util_log.WithUserID(tenantID, d.logger), "stream", stream.Labels)
	shardCount := d.shardCountFor(logger, &stream, pushSize, tenantID, shardStreamsCfg)

	if shardCount <= 1 {
		return []KeyedStream{{HashKey: lokiring.TokenFor(tenantID, stream.Labels), Stream: stream}}
	}

	d.streamShardCount.Inc()
	if shardStreamsCfg.LoggingEnabled {
		level.Info(logger).Log("msg", "sharding request", "shard_count", shardCount)
	}

	return d.divideEntriesBetweenShards(tenantID, shardCount, shardStreamsCfg, stream)
}

func (d *Distributor) divideEntriesBetweenShards(tenantID string, totalShards int, shardStreamsCfg shardstreams.Config, stream logproto.Stream) []KeyedStream {
	derivedStreams := d.createShards(stream, totalShards, tenantID, shardStreamsCfg)

	for i := 0; i < len(stream.Entries); i++ {
		streamIndex := i % len(derivedStreams)
		entries := append(derivedStreams[streamIndex].Stream.Entries, stream.Entries[i])
		derivedStreams[streamIndex].Stream.Entries = entries
	}

	return derivedStreams
}

func (d *Distributor) createShards(stream logproto.Stream, totalShards int, tenantID string, shardStreamsCfg shardstreams.Config) []KeyedStream {
	var (
		streamLabels   = labelTemplate(stream.Labels, d.logger)
		streamPattern  = streamLabels.String()
		derivedStreams = make([]KeyedStream, 0, totalShards)

		streamCount = streamCount(totalShards, stream)
	)

	if totalShards <= 0 {
		level.Error(d.logger).Log("msg", "attempt to create shard with zeroed total shards", "org_id", tenantID, "stream", stream.Labels, "entries_len", len(stream.Entries))
		return derivedStreams
	}

	entriesPerShard := int(math.Ceil(float64(len(stream.Entries)) / float64(totalShards)))
	startShard := d.shardTracker.LastShardNum(tenantID, stream.Hash)
	for i := 0; i < streamCount; i++ {
		shardNum := (startShard + i) % totalShards
		shard := d.createShard(streamLabels, streamPattern, shardNum, entriesPerShard)

		derivedStreams = append(derivedStreams, KeyedStream{
			HashKey: lokiring.TokenFor(tenantID, shard.Labels),
			Stream:  shard,
		})

		if shardStreamsCfg.LoggingEnabled {
			level.Info(d.logger).Log("msg", "stream derived from sharding", "src-stream", stream.Labels, "derived-stream", shard.Labels)
		}
	}
	d.shardTracker.SetLastShardNum(tenantID, stream.Hash, startShard+streamCount)

	return derivedStreams
}

func streamCount(totalShards int, stream logproto.Stream) int {
	if len(stream.Entries) < totalShards {
		return len(stream.Entries)
	}
	return totalShards
}

// labelTemplate returns a label set that includes the dummy label to be replaced
// To avoid allocations, this slice is reused when we know the stream value
func labelTemplate(lbls string, logger log.Logger) labels.Labels {
	baseLbls, err := syntax.ParseLabels(lbls)
	if err != nil {
		level.Error(logger).Log("msg", "couldn't extract labels from stream", "stream", lbls)
		return nil
	}

	streamLabels := make([]labels.Label, len(baseLbls)+1)
	copy(streamLabels, baseLbls)
	streamLabels[len(baseLbls)] = labels.Label{Name: ingester.ShardLbName, Value: ingester.ShardLbPlaceholder}

	sort.Sort(labels.Labels(streamLabels))

	return streamLabels
}

func (d *Distributor) createShard(lbls labels.Labels, streamPattern string, shardNumber, numOfEntries int) logproto.Stream {
	shardLabel := strconv.Itoa(shardNumber)
	for i := 0; i < len(lbls); i++ {
		if lbls[i].Name == ingester.ShardLbName {
			lbls[i].Value = shardLabel
			break
		}
	}

	return logproto.Stream{
		Labels:  strings.Replace(streamPattern, ingester.ShardLbPlaceholder, shardLabel, 1),
		Hash:    lbls.Hash(),
		Entries: make([]logproto.Entry, 0, numOfEntries),
	}
}

// maxT returns the highest between two given timestamps.
func maxT(t1, t2 time.Time) time.Time {
	if t1.Before(t2) {
		return t2
	}

	return t1
}

func (d *Distributor) truncateLines(vContext validationContext, stream *logproto.Stream) {
	if !vContext.maxLineSizeTruncate {
		return
	}

	var truncatedSamples, truncatedBytes int
	for i, e := range stream.Entries {
		if maxSize := vContext.maxLineSize; maxSize != 0 && len(e.Line) > maxSize {
			stream.Entries[i].Line = e.Line[:maxSize]

			truncatedSamples++
			truncatedBytes += len(e.Line) - maxSize
		}
	}

	validation.MutatedSamples.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedSamples))
	validation.MutatedBytes.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedBytes))
}

type pushIngesterTask struct {
	streamTracker []*streamTracker
	pushTracker   *pushTracker
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
	c, err := d.pool.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}

	req := &logproto.PushRequest{
		Streams: make([]logproto.Stream, len(streams)),
	}
	for i, s := range streams {
		req.Streams[i] = s.Stream
	}

	_, err = c.(logproto.PusherClient).Push(ctx, req)
	d.ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		if e, ok := status.FromError(err); ok {
			switch e.Code() {
			case codes.DeadlineExceeded:
				d.ingesterAppendTimeouts.WithLabelValues(ingester.Addr).Inc()
			}
		}
	}
	return err
}

func (d *Distributor) sendStreamsToKafka(ctx context.Context, streams []KeyedStream, tenant string, tracker *pushTracker, subring *ring.PartitionRing) {
	for _, s := range streams {
		go func(s KeyedStream) {
			err := d.sendStreamToKafka(ctx, s, tenant, subring)
			if err != nil {
				err = fmt.Errorf("failed to write stream to kafka: %w", err)
			}
			tracker.doneWithResult(err)
		}(s)
	}
}

func (d *Distributor) sendStreamToKafka(ctx context.Context, stream KeyedStream, tenant string, subring *ring.PartitionRing) error {
	if len(stream.Stream.Entries) == 0 {
		return nil
	}
	partitionID, err := subring.ActivePartitionForKey(stream.HashKey)
	if err != nil {
		d.kafkaAppends.WithLabelValues("kafka", "fail").Inc()
		return fmt.Errorf("failed to find active partition for stream: %w", err)
	}

	startTime := time.Now()

	records, err := kafka.Encode(partitionID, tenant, stream.Stream, d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes)
	if err != nil {
		d.kafkaAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
		return fmt.Errorf("failed to marshal write request to records: %w", err)
	}

	d.kafkaRecordsPerRequest.Observe(float64(len(records)))

	produceResults := d.kafkaWriter.ProduceSync(ctx, records)

	if count, sizeBytes := successfulProduceRecordsStats(produceResults); count > 0 {
		d.kafkaWriteLatency.Observe(time.Since(startTime).Seconds())
		d.kafkaWriteBytesTotal.Add(float64(sizeBytes))
	}

	var finalErr error
	for _, result := range produceResults {
		if result.Err != nil {
			d.kafkaAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
			finalErr = result.Err
		} else {
			d.kafkaAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "success").Inc()
		}
	}

	return finalErr
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

func (d *Distributor) parseStreamLabels(vContext validationContext, key string, stream logproto.Stream) (labels.Labels, string, uint64, error) {
	if val, ok := d.labelCache.Get(key); ok {
		return val.ls, val.ls.String(), val.hash, nil
	}

	ls, err := syntax.ParseLabels(key)
	if err != nil {
		return nil, "", 0, fmt.Errorf(validation.InvalidLabelsErrorMsg, key, err)
	}

	if err := d.validator.ValidateLabels(vContext, ls, stream); err != nil {
		return nil, "", 0, err
	}

	lsHash := ls.Hash()

	d.labelCache.Add(key, labelData{ls, lsHash})
	return ls, ls.String(), lsHash, nil
}

// shardCountFor returns the right number of shards to be used by the given stream.
//
// It first checks if the number of shards is present in the shard store. If it isn't it will calculate it
// based on the rate stored in the rate store and will store the new evaluated number of shards.
//
// desiredRate is expected to be given in bytes.
func (d *Distributor) shardCountFor(logger log.Logger, stream *logproto.Stream, pushSize int, tenantID string, streamShardcfg shardstreams.Config) int {
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
