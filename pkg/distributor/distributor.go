package distributor

import (
	"context"
	"flag"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/ingester"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	lru "github.com/hashicorp/golang-lru"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/distributor/shardstreams"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

const (
	ringKey = "distributor"
)

var (
	maxLabelCacheSize = 100000
	rfStats           = usagestats.NewInt("distributor_replication_factor")
)

// Config for a Distributor.
type Config struct {
	// Distributors ring
	DistributorRing RingConfig `yaml:"ring,omitempty"`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`

	RateStore RateStoreConfig `yaml:"rate_store"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.DistributorRing.RegisterFlags(fs)
	cfg.RateStore.RegisterFlagsWithPrefix("distributor.rate-store", fs)
}

// RateStore manages the ingestion rate of streams, populated by data fetched from ingesters.
type RateStore interface {
	RateFor(tenantID string, streamHash uint64) int64
}

// Distributor coordinates replicates and distribution of log streams.
type Distributor struct {
	services.Service

	cfg              Config
	clientCfg        client.Config
	tenantConfigs    *runtime.TenantConfigs
	tenantsRetention *retention.TenantsRetention
	ingestersRing    ring.ReadRing
	validator        *Validator
	pool             *ring_client.Pool

	rateStore    RateStore
	shardTracker *ShardTracker

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances.
	distributorsLifecycler *ring.Lifecycler

	rateLimitStrat string

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	// Per-user rate limiter.
	ingestionRateLimiter *limiter.RateLimiter
	labelCache           *lru.Cache
	// metrics
	ingesterAppends        *prometheus.CounterVec
	ingesterAppendFailures *prometheus.CounterVec
	replicationFactor      prometheus.Gauge
	streamShardCount       prometheus.Counter
}

// New a distributor creates.
func New(
	cfg Config,
	clientCfg client.Config,
	configs *runtime.TenantConfigs,
	ingestersRing ring.ReadRing,
	overrides Limits,
	registerer prometheus.Registerer,
) (*Distributor, error) {
	factory := cfg.factory
	if factory == nil {
		factory = func(addr string) (ring_client.PoolClient, error) {
			return client.New(clientCfg, addr)
		}
	}

	internalFactory := func(addr string) (ring_client.PoolClient, error) {
		internalCfg := clientCfg
		internalCfg.Internal = true
		return client.New(internalCfg, addr)
	}

	validator, err := NewValidator(overrides)
	if err != nil {
		return nil, err
	}

	// Create the configured ingestion rate limit strategy (local or global).
	var ingestionRateStrategy limiter.RateLimiterStrategy
	var distributorsLifecycler *ring.Lifecycler
	rateLimitStrat := validation.LocalIngestionRateStrategy

	var servs []services.Service
	if overrides.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		rateLimitStrat = validation.GlobalIngestionRateStrategy
		if err != nil {
			return nil, errors.Wrap(err, "create distributor KV store client")
		}

		distributorsLifecycler, err = ring.NewLifecycler(cfg.DistributorRing.ToLifecyclerConfig(), nil, "distributor", ringKey, false, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_", registerer))
		if err != nil {
			return nil, errors.Wrap(err, "create distributor lifecycler")
		}

		servs = append(servs, distributorsLifecycler)
		ingestionRateStrategy = newGlobalIngestionRateStrategy(overrides, distributorsLifecycler)
	} else {
		ingestionRateStrategy = newLocalIngestionRateStrategy(overrides)
	}

	labelCache, err := lru.New(maxLabelCacheSize)
	if err != nil {
		return nil, err
	}

	d := Distributor{
		cfg:                    cfg,
		clientCfg:              clientCfg,
		tenantConfigs:          configs,
		tenantsRetention:       retention.NewTenantsRetention(overrides),
		ingestersRing:          ingestersRing,
		distributorsLifecycler: distributorsLifecycler,
		validator:              validator,
		pool:                   clientpool.NewPool(clientCfg.PoolConfig, ingestersRing, factory, util_log.Logger),
		ingestionRateLimiter:   limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second),
		labelCache:             labelCache,
		shardTracker:           NewShardTracker(),
		rateLimitStrat:         rateLimitStrat,
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "distributor_ingester_appends_total",
			Help:      "The total number of batch appends sent to ingesters.",
		}, []string{"ingester"}),
		ingesterAppendFailures: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "distributor_ingester_append_failures_total",
			Help:      "The total number of failed batch appends sent to ingesters.",
		}, []string{"ingester"}),
		replicationFactor: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "distributor_replication_factor",
			Help:      "The configured replication factor.",
		}),
		streamShardCount: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "stream_sharding_count",
			Help:      "Total number of times the distributor has sharded streams",
		}),
	}
	d.replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	rfStats.Set(int64(ingestersRing.ReplicationFactor()))

	rs := NewRateStore(
		d.cfg.RateStore,
		ingestersRing,
		clientpool.NewPool(
			clientCfg.PoolConfig,
			ingestersRing,
			internalFactory,
			util_log.Logger,
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

	return &d, nil
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

// TODO taken from Cortex, see if we can refactor out an usable interface.
type streamTracker struct {
	stream      logproto.Stream
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
	streams := make([]streamTracker, 0, len(req.Streams))
	keys := make([]uint32, 0, len(req.Streams))
	validatedLineSize := 0
	validatedLineCount := 0

	var validationErr error
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

			stream.Labels, stream.Hash, err = d.parseStreamLabels(validationContext, stream.Labels, &stream)
			if err != nil {
				validationErr = err
				validation.DiscardedSamples.WithLabelValues(validation.InvalidLabels, tenantID).Add(float64(len(stream.Entries)))
				bytes := 0
				for _, e := range stream.Entries {
					bytes += len(e.Line)
				}
				validation.DiscardedBytes.WithLabelValues(validation.InvalidLabels, tenantID).Add(float64(bytes))
				continue
			}

			n := 0
			streamSize := 0
			for _, entry := range stream.Entries {
				if err := d.validator.ValidateEntry(validationContext, stream.Labels, entry); err != nil {
					validationErr = err
					continue
				}

				stream.Entries[n] = entry

				// If configured for this tenant, increment duplicate timestamps. Note, this is imperfect
				// since Loki will accept out of order writes it doesn't account for separate
				// pushes with overlapping time ranges having entries with duplicate timestamps
				if validationContext.incrementDuplicateTimestamps && n != 0 {
					// Traditional logic for Loki is that 2 lines with the same timestamp and
					// exact same content will be de-duplicated, (i.e. only one will be stored, others dropped)
					// To maintain this behavior, only increment the timestamp if the log content is different
					if stream.Entries[n-1].Line != entry.Line {
						stream.Entries[n].Timestamp = maxT(entry.Timestamp, stream.Entries[n-1].Timestamp.Add(1*time.Nanosecond))
					}
				}

				n++
				validatedLineSize += len(entry.Line)
				validatedLineCount++
				streamSize += len(entry.Line)
			}
			stream.Entries = stream.Entries[:n]

			shardStreamsCfg := d.validator.Limits.ShardStreams(tenantID)
			if shardStreamsCfg.Enabled {
				derivedKeys, derivedStreams := d.shardStream(stream, streamSize, tenantID)
				keys = append(keys, derivedKeys...)
				streams = append(streams, derivedStreams...)
			} else {
				keys = append(keys, util.TokenFor(tenantID, stream.Labels))
				streams = append(streams, streamTracker{stream: stream})
			}
		}
	}()

	// Return early if none of the streams contained entries
	if len(streams) == 0 {
		return &logproto.PushResponse{}, validationErr
	}

	now := time.Now()
	if !d.ingestionRateLimiter.AllowN(now, tenantID, validatedLineSize) {
		// Return a 429 to indicate to the client they are being rate limited
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, tenantID).Add(float64(validatedLineCount))
		validation.DiscardedBytes.WithLabelValues(validation.RateLimited, tenantID).Add(float64(validatedLineSize))
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, tenantID, int(d.ingestionRateLimiter.Limit(now, tenantID)), validatedLineCount, validatedLineSize)
	}

	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.InstanceDesc

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

		for i, key := range keys {
			replicationSet, err := d.ingestersRing.Get(key, ring.WriteNoExtend, descs[:0], nil, nil)
			if err != nil {
				return err
			}

			streams[i].minSuccess = len(replicationSet.Instances) - replicationSet.MaxErrors
			streams[i].maxFailures = replicationSet.MaxErrors
			for _, ingester := range replicationSet.Instances {
				streamsByIngester[ingester.Addr] = append(streamsByIngester[ingester.Addr], &streams[i])
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

// shardStream shards (divides) the given stream into N smaller streams, where
// N is the sharding size for the given stream. shardSteam returns the smaller
// streams and their associated keys for hashing to ingesters.
//
// The number of shards is limited by the number of entries.
func (d *Distributor) shardStream(stream logproto.Stream, streamSize int, tenantID string) ([]uint32, []streamTracker) {
	shardStreamsCfg := d.validator.Limits.ShardStreams(tenantID)
	logger := log.With(util_log.WithUserID(tenantID, util_log.Logger), "stream", stream.Labels)
	shardCount := d.shardCountFor(logger, &stream, streamSize, tenantID, shardStreamsCfg)

	if shardCount <= 1 {
		return []uint32{util.TokenFor(tenantID, stream.Labels)}, []streamTracker{{stream: stream}}
	}

	d.streamShardCount.Inc()
	if shardStreamsCfg.LoggingEnabled {
		level.Info(logger).Log("msg", "sharding request", "shard_count", shardCount)
	}

	return d.divideEntriesBetweenShards(tenantID, shardCount, shardStreamsCfg, stream)
}

func (d *Distributor) divideEntriesBetweenShards(tenantID string, totalShards int, shardStreamsCfg *shardstreams.Config, stream logproto.Stream) ([]uint32, []streamTracker) {
	derivedKeys, derivedStreams := d.createShards(stream, totalShards, tenantID, shardStreamsCfg)

	for i := 0; i < len(stream.Entries); i++ {
		streamIndex := i % len(derivedStreams)
		entries := append(derivedStreams[streamIndex].stream.Entries, stream.Entries[i])
		derivedStreams[streamIndex].stream.Entries = entries
	}

	return derivedKeys, derivedStreams
}

func (d *Distributor) createShards(stream logproto.Stream, totalShards int, tenantID string, shardStreamsCfg *shardstreams.Config) ([]uint32, []streamTracker) {
	var (
		streamLabels   = labelTemplate(stream.Labels)
		streamPattern  = streamLabels.String()
		derivedKeys    = make([]uint32, 0, totalShards)
		derivedStreams = make([]streamTracker, 0, totalShards)

		streamCount = streamCount(totalShards, stream)
	)

	startShard := d.shardTracker.LastShardNum(tenantID, stream.Hash)
	for i := 0; i < streamCount; i++ {
		shardNum := (startShard + i) % totalShards
		shard := d.createShard(streamLabels, streamPattern, shardNum)

		derivedKeys = append(derivedKeys, util.TokenFor(tenantID, shard.Labels))
		derivedStreams = append(derivedStreams, streamTracker{stream: shard})

		if shardStreamsCfg.LoggingEnabled {
			level.Info(util_log.Logger).Log("msg", "stream derived from sharding", "src-stream", stream.Labels, "derived-stream", shard.Labels)
		}
	}
	d.shardTracker.SetLastShardNum(tenantID, stream.Hash, startShard+streamCount)

	return derivedKeys, derivedStreams
}

func streamCount(totalShards int, stream logproto.Stream) int {
	if len(stream.Entries) < totalShards {
		return len(stream.Entries)
	}
	return totalShards
}

// labelTemplate returns a label set that includes the dummy label to be replaced
// To avoid allocations, this slice is reused when we know the stream value
func labelTemplate(lbls string) labels.Labels {
	baseLbls, err := syntax.ParseLabels(lbls)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "couldn't extract labels from stream", "stream", lbls)
		return nil
	}

	streamLabels := make([]labels.Label, len(baseLbls)+1)
	copy(streamLabels, baseLbls)
	streamLabels[len(baseLbls)] = labels.Label{Name: ingester.ShardLbName, Value: ingester.ShardLbPlaceholder}

	sort.Sort(labels.Labels(streamLabels))

	return streamLabels
}

func (d *Distributor) createShard(lbls labels.Labels, streamPattern string, shardNumber int) logproto.Stream {
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
		Entries: make([]logproto.Entry, 0, 1024),
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
		req.Streams[i] = s.stream
	}

	_, err = c.(logproto.PusherClient).Push(ctx, req)
	d.ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		d.ingesterAppendFailures.WithLabelValues(ingester.Addr).Inc()
	}
	return err
}

type labelData struct {
	labels string
	hash   uint64
}

func (d *Distributor) parseStreamLabels(vContext validationContext, key string, stream *logproto.Stream) (string, uint64, error) {
	if val, ok := d.labelCache.Get(key); ok {
		labelVal := val.(labelData)
		return labelVal.labels, labelVal.hash, nil
	}

	ls, err := syntax.ParseLabels(key)
	if err != nil {
		return "", 0, httpgrpc.Errorf(http.StatusBadRequest, validation.InvalidLabelsErrorMsg, key, err)
	}

	if err := d.validator.ValidateLabels(vContext, ls, *stream); err != nil {
		return "", 0, err
	}

	lsVal := ls.String()
	lsHash := ls.Hash()

	d.labelCache.Add(key, labelData{lsVal, lsHash})
	return lsVal, lsHash, nil
}

// shardCountFor returns the right number of shards to be used by the given stream.
//
// It first checks if the number of shards is present in the shard store. If it isn't it will calculate it
// based on the rate stored in the rate store and will store the new evaluated number of shards.
//
// desiredRate is expected to be given in bytes.
func (d *Distributor) shardCountFor(logger log.Logger, stream *logproto.Stream, streamSize int, tenantID string, streamShardcfg *shardstreams.Config) int {
	if streamShardcfg.DesiredRate.Val() <= 0 {
		if streamShardcfg.LoggingEnabled {
			level.Error(logger).Log("msg", "invalid desired rate", "desired_rate", streamShardcfg.DesiredRate.String())
		}
		return 1
	}

	rate := d.rateStore.RateFor(tenantID, stream.Hash)
	shards := calculateShards(rate, streamSize, streamShardcfg.DesiredRate.Val())
	if shards == 0 {
		// 1 shard is enough for the given stream.
		return 1
	}

	return shards
}

func calculateShards(rate int64, streamSize, desiredRate int) int {
	shards := float64(rate+int64(streamSize)) / float64(desiredRate)
	if shards <= 1 {
		return 1
	}
	return int(math.Ceil(shards))
}
