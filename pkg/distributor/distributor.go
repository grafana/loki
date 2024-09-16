package distributor

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"github.com/buger/jsonparser"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/grpc/codes"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	lru "github.com/hashicorp/golang-lru"
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
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
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
)

var allowedLabelsForLevel = map[string]struct{}{
	"level": {}, "LEVEL": {}, "Level": {},
	"severity": {}, "SEVERITY": {}, "Severity": {},
	"lvl": {}, "LVL": {}, "Lvl": {},
}

// Config for a Distributor.
type Config struct {
	// Distributors ring
	DistributorRing RingConfig `yaml:"ring,omitempty"`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`

	// RateStore customizes the rate storing used by stream sharding.
	RateStore RateStoreConfig `yaml:"rate_store"`

	// WriteFailuresLoggingCfg customizes write failures logging behavior.
	WriteFailuresLogging writefailures.Cfg `yaml:"write_failures_logging" doc:"description=Customize the logging of write failures."`

	OTLPConfig push.GlobalOTLPConfig `yaml:"otlp_config"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.OTLPConfig.RegisterFlags(fs)
	cfg.DistributorRing.RegisterFlags(fs)
	cfg.RateStore.RegisterFlagsWithPrefix("distributor.rate-store", fs)
	cfg.WriteFailuresLogging.RegisterFlagsWithPrefix("distributor.write-failures-logging", fs)
}

// RateStore manages the ingestion rate of streams, populated by data fetched from ingesters.
type RateStore interface {
	RateFor(tenantID string, streamHash uint64) (int64, float64)
}

// Distributor coordinates replicates and distribution of log streams.
type Distributor struct {
	services.Service

	cfg              Config
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
	labelCache           *lru.Cache

	// Push failures rate limiter.
	writeFailuresManager *writefailures.Manager

	RequestParserWrapper push.RequestParserWrapper

	// metrics
	ingesterAppends        *prometheus.CounterVec
	ingesterAppendTimeouts *prometheus.CounterVec
	replicationFactor      prometheus.Gauge
	streamShardCount       prometheus.Counter

	usageTracker push.UsageTracker
}

// New a distributor creates.
func New(
	cfg Config,
	clientCfg client.Config,
	configs *runtime.TenantConfigs,
	ingestersRing ring.ReadRing,
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
	labelCache, err := lru.New(maxLabelCacheSize)
	if err != nil {
		return nil, err
	}

	d := &Distributor{
		cfg:                   cfg,
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
		writeFailuresManager: writefailures.NewManager(logger, registerer, cfg.WriteFailuresLogging, configs, "distributor"),
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
	select {
	case <-ctx.Done():
		return nil
	case err := <-d.subservicesWatcher.Chan():
		return errors.Wrap(err, "distributor subservice failed")
	}
}

func (d *Distributor) stopping(_ error) error {
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

// Push a set of streams.
// The returned error is the last one seen.
func (d *Distributor) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Return early if request does not contain any streams
	if len(req.Streams) == 0 {
		return &logproto.PushResponse{}, nil
	}

	// First we flatten out the request into a list of samples.
	// We use the heuristic of 1 sample per TS to size the array.
	// We also work out the hash value at the same time.
	streams := make([]KeyedStream, 0, len(req.Streams))
	validatedLineSize := 0
	validatedLineCount := 0

	var validationErrors util.GroupedErrors
	validationContext := d.validator.getValidationContextForTime(time.Now(), tenantID)

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
				bytes := 0
				for _, e := range stream.Entries {
					bytes += len(e.Line)
				}
				validation.DiscardedBytes.WithLabelValues(validation.InvalidLabels, tenantID).Add(float64(bytes))
				continue
			}

			n := 0
			pushSize := 0
			prevTs := stream.Entries[0].Timestamp

			shouldDiscoverLevels := validationContext.allowStructuredMetadata && validationContext.discoverLogLevels
			levelFromLabel, hasLevelLabel := hasAnyLevelLabels(lbs)
			for _, entry := range stream.Entries {
				if err := d.validator.ValidateEntry(ctx, validationContext, lbs, entry); err != nil {
					d.writeFailuresManager.Log(tenantID, err)
					validationErrors.Add(err)
					continue
				}

				structuredMetadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)
				if shouldDiscoverLevels {
					var logLevel string
					if hasLevelLabel {
						logLevel = levelFromLabel
					} else if levelFromMetadata, ok := hasAnyLevelLabels(structuredMetadata); ok {
						logLevel = levelFromMetadata
					} else {
						logLevel = detectLogLevelFromLogEntry(entry, structuredMetadata)
					}
					if logLevel != constants.LogLevelUnknown && logLevel != "" {
						entry.StructuredMetadata = append(entry.StructuredMetadata, logproto.LabelAdapter{
							Name:  constants.LevelLabel,
							Value: logLevel,
						})
					}
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
				validatedLineSize += len(entry.Line)
				validatedLineCount++
				pushSize += len(entry.Line)
			}
			stream.Entries = stream.Entries[:n]
			if len(stream.Entries) == 0 {
				// Empty stream after validating all the entries
				continue
			}

			shardStreamsCfg := d.validator.Limits.ShardStreams(tenantID)
			if shardStreamsCfg.Enabled {
				streams = append(streams, d.shardStream(stream, pushSize, tenantID)...)
			} else {
				streams = append(streams, KeyedStream{
					HashKey: lokiring.TokenFor(tenantID, stream.Labels),
					Stream:  stream,
				})
			}
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

	now := time.Now()

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

	tracker := pushTracker{
		done: make(chan struct{}, 1), // buffer avoids blocking if caller terminates - sendSamples() only sends once on each
		err:  make(chan error, 1),
	}
	tracker.streamsPending.Store(int32(len(streams)))
	for ingester, streams := range streamsByIngester {
		go func(ingester ring.InstanceDesc, samples []*streamTracker) {
			// Use a background context to make sure all ingesters get samples even if we return early
			localCtx, cancel := context.WithTimeout(context.Background(), d.clientCfg.RemoteTimeout)
			defer cancel()
			localCtx = user.InjectOrgID(localCtx, tenantID)
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				localCtx = opentracing.ContextWithSpan(localCtx, sp)
			}
			d.sendStreams(localCtx, ingester, samples, &tracker)
		}(ingesterDescs[ingester], streams)
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

			discardedStreamBytes := 0
			for _, e := range stream.Entries {
				discardedStreamBytes += len(e.Line)
			}

			if d.usageTracker != nil {
				d.usageTracker.DiscardedBytesAdd(ctx, tenantID, reason, lbs, float64(discardedStreamBytes))
			}
		}
	}
}

func hasAnyLevelLabels(l labels.Labels) (string, bool) {
	for lbl := range allowedLabelsForLevel {
		if l.Has(lbl) {
			return l.Get(lbl), true
		}
	}
	return "", false
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

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendStreams(ctx context.Context, ingester ring.InstanceDesc, streamTrackers []*streamTracker, pushTracker *pushTracker) {
	err := d.sendStreamsErr(ctx, ingester, streamTrackers)

	// If we succeed, decrement each stream's pending count by one.
	// If we reach the required number of successful puts on this stream, then
	// decrement the number of pending streams by one.
	// If we successfully push all streams to min success ingesters, wake up the
	// waiting rpc so it can return early. Similarly, track the number of errors,
	// and if it exceeds maxFailures shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendStreams
	// goroutine will write to either channel.
	for i := range streamTrackers {
		if err != nil {
			if streamTrackers[i].failed.Inc() <= int32(streamTrackers[i].maxFailures) {
				continue
			}
			if pushTracker.streamsFailed.Inc() == 1 {
				pushTracker.err <- err
			}
		} else {
			if streamTrackers[i].succeeded.Inc() != int32(streamTrackers[i].minSuccess) {
				continue
			}
			if pushTracker.streamsPending.Dec() == 0 {
				pushTracker.done <- struct{}{}
			}
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

type labelData struct {
	ls   labels.Labels
	hash uint64
}

func (d *Distributor) parseStreamLabels(vContext validationContext, key string, stream logproto.Stream) (labels.Labels, string, uint64, error) {
	if val, ok := d.labelCache.Get(key); ok {
		labelVal := val.(labelData)
		return labelVal.ls, labelVal.ls.String(), labelVal.hash, nil
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

func detectLogLevelFromLogEntry(entry logproto.Entry, structuredMetadata labels.Labels) string {
	// otlp logs have a severity number, using which we are defining the log levels.
	// Significance of severity number is explained in otel docs here https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-severitynumber
	if otlpSeverityNumberTxt := structuredMetadata.Get(push.OTLPSeverityNumber); otlpSeverityNumberTxt != "" {
		otlpSeverityNumber, err := strconv.Atoi(otlpSeverityNumberTxt)
		if err != nil {
			return constants.LogLevelInfo
		}
		if otlpSeverityNumber == int(plog.SeverityNumberUnspecified) {
			return constants.LogLevelUnknown
		} else if otlpSeverityNumber <= int(plog.SeverityNumberTrace4) {
			return constants.LogLevelTrace
		} else if otlpSeverityNumber <= int(plog.SeverityNumberDebug4) {
			return constants.LogLevelDebug
		} else if otlpSeverityNumber <= int(plog.SeverityNumberInfo4) {
			return constants.LogLevelInfo
		} else if otlpSeverityNumber <= int(plog.SeverityNumberWarn4) {
			return constants.LogLevelWarn
		} else if otlpSeverityNumber <= int(plog.SeverityNumberError4) {
			return constants.LogLevelError
		} else if otlpSeverityNumber <= int(plog.SeverityNumberFatal4) {
			return constants.LogLevelFatal
		}
		return constants.LogLevelUnknown
	}

	return extractLogLevelFromLogLine(entry.Line)
}

func extractLogLevelFromLogLine(log string) string {
	logSlice := unsafe.Slice(unsafe.StringData(log), len(log))
	var v []byte
	if isJSON(log) {
		v = getValueUsingJSONParser(logSlice)
	} else {
		v = getValueUsingLogfmtParser(logSlice)
	}

	switch {
	case bytes.EqualFold(v, []byte("trace")), bytes.EqualFold(v, []byte("trc")):
		return constants.LogLevelTrace
	case bytes.EqualFold(v, []byte("debug")), bytes.EqualFold(v, []byte("dbg")):
		return constants.LogLevelDebug
	case bytes.EqualFold(v, []byte("info")), bytes.EqualFold(v, []byte("inf")):
		return constants.LogLevelInfo
	case bytes.EqualFold(v, []byte("warn")), bytes.EqualFold(v, []byte("wrn")):
		return constants.LogLevelWarn
	case bytes.EqualFold(v, []byte("error")), bytes.EqualFold(v, []byte("err")):
		return constants.LogLevelError
	case bytes.EqualFold(v, []byte("critical")):
		return constants.LogLevelCritical
	case bytes.EqualFold(v, []byte("fatal")):
		return constants.LogLevelFatal
	default:
		return detectLevelFromLogLine(log)
	}
}

func getValueUsingLogfmtParser(line []byte) []byte {
	equalIndex := bytes.Index(line, []byte("="))
	if len(line) == 0 || equalIndex == -1 {
		return nil
	}

	d := logfmt.NewDecoder(line)
	for !d.EOL() && d.ScanKeyval() {
		if _, ok := allowedLabelsForLevel[string(d.Key())]; ok {
			return (d.Value())
		}
	}
	return nil
}

func getValueUsingJSONParser(log []byte) []byte {
	for allowedLabel := range allowedLabelsForLevel {
		l, _, _, err := jsonparser.Get(log, allowedLabel)
		if err == nil {
			return l
		}
	}
	return nil
}

func isJSON(line string) bool {
	var firstNonSpaceChar rune
	for _, char := range line {
		if !unicode.IsSpace(char) {
			firstNonSpaceChar = char
			break
		}
	}

	var lastNonSpaceChar rune
	for i := len(line) - 1; i >= 0; i-- {
		char := rune(line[i])
		if !unicode.IsSpace(char) {
			lastNonSpaceChar = char
			break
		}
	}

	return firstNonSpaceChar == '{' && lastNonSpaceChar == '}'
}

func detectLevelFromLogLine(log string) string {
	if strings.Contains(log, "info:") || strings.Contains(log, "INFO:") ||
		strings.Contains(log, "info") || strings.Contains(log, "INFO") {
		return constants.LogLevelInfo
	}
	if strings.Contains(log, "err:") || strings.Contains(log, "ERR:") ||
		strings.Contains(log, "error") || strings.Contains(log, "ERROR") {
		return constants.LogLevelError
	}
	if strings.Contains(log, "warn:") || strings.Contains(log, "WARN:") ||
		strings.Contains(log, "warning") || strings.Contains(log, "WARNING") {
		return constants.LogLevelWarn
	}
	if strings.Contains(log, "CRITICAL:") || strings.Contains(log, "critical:") {
		return constants.LogLevelCritical
	}
	if strings.Contains(log, "debug:") || strings.Contains(log, "DEBUG:") {
		return constants.LogLevelDebug
	}
	return constants.LogLevelUnknown
}
