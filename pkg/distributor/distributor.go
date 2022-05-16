package distributor

import (
	"context"
	"flag"
	"net/http"
	"time"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	lru "github.com/hashicorp/golang-lru"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
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
}

// RegisterFlags registers distributor-related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.DistributorRing.RegisterFlags(fs)
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

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances.
	distributorsRing       *ring.Ring
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
}

// New a distributor creates.
func New(cfg Config, clientCfg client.Config, configs *runtime.TenantConfigs, ingestersRing ring.ReadRing, overrides *validation.Overrides, registerer prometheus.Registerer) (*Distributor, error) {
	factory := cfg.factory
	if factory == nil {
		factory = func(addr string) (ring_client.PoolClient, error) {
			return client.New(clientCfg, addr)
		}
	}

	validator, err := NewValidator(overrides)
	if err != nil {
		return nil, err
	}

	// Create the configured ingestion rate limit strategy (local or global).
	var ingestionRateStrategy limiter.RateLimiterStrategy
	var distributorsLifecycler *ring.Lifecycler
	var distributorsRing *ring.Ring
	rateLimitStrat := validation.LocalIngestionRateStrategy

	var servs []services.Service
	if overrides.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		rateLimitStrat = validation.GlobalIngestionRateStrategy
		ringStore, err := kv.NewClient(
			cfg.DistributorRing.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "distributor"),
			util_log.Logger)
		if err != nil {
			return nil, errors.Wrap(err, "create distributor KV store client")
		}

		distributorsLifecycler, err = ring.NewLifecycler(cfg.DistributorRing.ToLifecyclerConfig(), nil, "distributor", ringKey, false, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_", registerer))
		if err != nil {
			return nil, errors.Wrap(err, "create distributor lifecycler")
		}

		distributorsRing, err = ring.NewWithStoreClientAndStrategy(cfg.DistributorRing.ToRingConfig(),
			"distributor", "distributor", ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("cortex_", registerer), util_log.Logger)
		if err != nil {
			return nil, errors.Wrap(err, "create distributor ring client")
		}

		servs = append(servs, distributorsLifecycler, distributorsRing)
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
		distributorsRing:       distributorsRing,
		distributorsLifecycler: distributorsLifecycler,
		validator:              validator,
		pool:                   clientpool.NewPool(clientCfg.PoolConfig, ingestersRing, factory, util_log.Logger),
		ingestionRateLimiter:   limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second),
		labelCache:             labelCache,
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
	}
	d.replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	rfStats.Set(int64(ingestersRing.ReplicationFactor()))

	servs = append(servs, d.pool)
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
	samplesPending atomic.Int32
	samplesFailed  atomic.Int32
	done           chan struct{}
	err            chan error
}

// Push a set of streams.
func (d *Distributor) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	userID, err := tenant.TenantID(ctx)
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
	validatedSamplesSize := 0
	validatedSamplesCount := 0

	var validationErr error
	validationContext := d.validator.getValidationContextForTime(time.Now(), userID)

	for _, stream := range req.Streams {
		// Return early if stream does not contain any entries
		if len(stream.Entries) == 0 {
			continue
		}

		// Truncate first so subsequent steps have consistent line lengths
		d.truncateLines(validationContext, &stream)

		stream.Labels, err = d.parseStreamLabels(validationContext, stream.Labels, &stream)
		if err != nil {
			validationErr = err
			validation.DiscardedSamples.WithLabelValues(validation.InvalidLabels, userID).Add(float64(len(stream.Entries)))
			bytes := 0
			for _, e := range stream.Entries {
				bytes += len(e.Line)
			}
			validation.DiscardedBytes.WithLabelValues(validation.InvalidLabels, userID).Add(float64(bytes))
			continue
		}

		n := 0
		for _, entry := range stream.Entries {
			if err := d.validator.ValidateEntry(validationContext, stream.Labels, entry); err != nil {
				validationErr = err
				continue
			}

			stream.Entries[n] = entry

			// If configured for this tenant, fudge duplicate timestamps. Note, this is imperfect
			// since Loki will accept out of order writes it doesn't account for separate
			// pushes with overlapping time ranges having entries with duplicate timestamps
			if validationContext.fudgeDuplicateTimestamps && n != 0 && stream.Entries[n-1].Timestamp.Equal(entry.Timestamp) {
				// Traditional logic for Loki is that 2 lines with the same timestamp and
				// exact same content will be de-duplicated, (i.e. only one will be stored, others dropped)
				// To maintain this behavior, only fudge the timestamp if the log content is different
				if stream.Entries[n-1].Line != entry.Line {
					stream.Entries[n].Timestamp = entry.Timestamp.Add(1 * time.Nanosecond)
				}
			}

			n++
			validatedSamplesSize += len(entry.Line)
			validatedSamplesCount++
		}
		stream.Entries = stream.Entries[:n]

		keys = append(keys, util.TokenFor(userID, stream.Labels))
		streams = append(streams, streamTracker{stream: stream})
	}

	// Return early if none of the streams contained entries
	if len(streams) == 0 {
		return &logproto.PushResponse{}, validationErr
	}

	now := time.Now()
	if !d.ingestionRateLimiter.AllowN(now, userID, validatedSamplesSize) {
		// Return a 429 to indicate to the client they are being rate limited
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamplesCount))
		validation.DiscardedBytes.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamplesSize))
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, userID, int(d.ingestionRateLimiter.Limit(now, userID)), validatedSamplesCount, validatedSamplesSize)
	}

	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.InstanceDesc

	samplesByIngester := map[string][]*streamTracker{}
	ingesterDescs := map[string]ring.InstanceDesc{}
	for i, key := range keys {
		replicationSet, err := d.ingestersRing.Get(key, ring.Write, descs[:0], nil, nil)
		if err != nil {
			return nil, err
		}

		streams[i].minSuccess = len(replicationSet.Instances) - replicationSet.MaxErrors
		streams[i].maxFailures = replicationSet.MaxErrors
		for _, ingester := range replicationSet.Instances {
			samplesByIngester[ingester.Addr] = append(samplesByIngester[ingester.Addr], &streams[i])
			ingesterDescs[ingester.Addr] = ingester
		}
	}

	tracker := pushTracker{
		done: make(chan struct{}, 1), // buffer avoids blocking if caller terminates - sendSamples() only sends once on each
		err:  make(chan error, 1),
	}
	tracker.samplesPending.Store(int32(len(streams)))
	for ingester, samples := range samplesByIngester {
		go func(ingester ring.InstanceDesc, samples []*streamTracker) {
			// Use a background context to make sure all ingesters get samples even if we return early
			localCtx, cancel := context.WithTimeout(context.Background(), d.clientCfg.RemoteTimeout)
			defer cancel()
			localCtx = user.InjectOrgID(localCtx, userID)
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				localCtx = opentracing.ContextWithSpan(localCtx, sp)
			}
			d.sendSamples(localCtx, ingester, samples, &tracker)
		}(ingesterDescs[ingester], samples)
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

func (d *Distributor) truncateLines(vContext validationContext, stream *logproto.Stream) {
	if !vContext.maxLineSizeTruncate {
		return
	}

	var truncatedSamples, truncatedBytes int
	for i, e := range stream.Entries {
		if maxSize := vContext.maxLineSize; maxSize != 0 && len(e.Line) > maxSize {
			stream.Entries[i].Line = e.Line[:maxSize]

			truncatedSamples++
			truncatedBytes = len(e.Line) - maxSize
		}
	}

	validation.MutatedSamples.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedSamples))
	validation.MutatedBytes.WithLabelValues(validation.LineTooLong, vContext.userID).Add(float64(truncatedBytes))
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendSamples(ctx context.Context, ingester ring.InstanceDesc, streamTrackers []*streamTracker, pushTracker *pushTracker) {
	err := d.sendSamplesErr(ctx, ingester, streamTrackers)

	// If we succeed, decrement each sample's pending count by one.  If we reach
	// the required number of successful puts on this sample, then decrement the
	// number of pending samples by one.  If we successfully push all samples to
	// min success ingesters, wake up the waiting rpc so it can return early.
	// Similarly, track the number of errors, and if it exceeds maxFailures
	// shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendSamples
	// goroutine will write to either channel.
	for i := range streamTrackers {
		if err != nil {
			if streamTrackers[i].failed.Inc() <= int32(streamTrackers[i].maxFailures) {
				continue
			}
			if pushTracker.samplesFailed.Inc() == 1 {
				pushTracker.err <- err
			}
		} else {
			if streamTrackers[i].succeeded.Inc() != int32(streamTrackers[i].minSuccess) {
				continue
			}
			if pushTracker.samplesPending.Dec() == 0 {
				pushTracker.done <- struct{}{}
			}
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendSamplesErr(ctx context.Context, ingester ring.InstanceDesc, streams []*streamTracker) error {
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

func (d *Distributor) parseStreamLabels(vContext validationContext, key string, stream *logproto.Stream) (string, error) {
	labelVal, ok := d.labelCache.Get(key)
	if ok {
		return labelVal.(string), nil
	}
	ls, err := syntax.ParseLabels(key)
	if err != nil {
		return "", httpgrpc.Errorf(http.StatusBadRequest, validation.InvalidLabelsErrorMsg, key, err)
	}
	// ensure labels are correctly sorted.
	if err := d.validator.ValidateLabels(vContext, ls, *stream); err != nil {
		return "", err
	}
	lsVal := ls.String()
	d.labelCache.Add(key, lsVal)
	return lsVal, nil
}
