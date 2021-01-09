package distributor

import (
	"context"
	"flag"
	"net/http"
	"time"

	cortex_distributor "github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	cortex_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/cortexproject/cortex/pkg/util/services"
	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"go.uber.org/atomic"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	ingesterAppends = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_ingester_appends_total",
		Help:      "The total number of batch appends sent to ingesters.",
	}, []string{"ingester"})
	ingesterAppendFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_ingester_append_failures_total",
		Help:      "The total number of failed batch appends sent to ingesters.",
	}, []string{"ingester"})

	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant",
	}, []string{"tenant"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})

	maxLabelCacheSize = 100000
)

// Config for a Distributor.
type Config struct {
	// Distributors ring
	DistributorRing cortex_distributor.RingConfig `yaml:"ring,omitempty"`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.DistributorRing.RegisterFlags(f)
}

// Distributor coordinates replicates and distribution of log streams.
type Distributor struct {
	services.Service

	cfg           Config
	clientCfg     client.Config
	ingestersRing ring.ReadRing
	validator     *Validator
	pool          *ring_client.Pool

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances.
	distributorsRing *ring.Lifecycler

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Per-user rate limiter.
	ingestionRateLimiter *limiter.RateLimiter
	labelCache           *lru.Cache
}

// New a distributor creates.
func New(cfg Config, clientCfg client.Config, ingestersRing ring.ReadRing, overrides *validation.Overrides, registerer prometheus.Registerer) (*Distributor, error) {
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
	var distributorsRing *ring.Lifecycler

	var servs []services.Service

	if overrides.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		var err error
		distributorsRing, err = ring.NewLifecycler(cfg.DistributorRing.ToLifecyclerConfig(), nil, "distributor", ring.DistributorRingKey, false, registerer)
		if err != nil {
			return nil, err
		}

		servs = append(servs, distributorsRing)
		ingestionRateStrategy = newGlobalIngestionRateStrategy(overrides, distributorsRing)
	} else {
		ingestionRateStrategy = newLocalIngestionRateStrategy(overrides)
	}

	labelCache, err := lru.New(maxLabelCacheSize)
	if err != nil {
		return nil, err
	}
	d := Distributor{
		cfg:                  cfg,
		clientCfg:            clientCfg,
		ingestersRing:        ingestersRing,
		distributorsRing:     distributorsRing,
		validator:            validator,
		pool:                 cortex_distributor.NewPool(clientCfg.PoolConfig, ingestersRing, factory, cortex_util.Logger),
		ingestionRateLimiter: limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second),
		labelCache:           labelCache,
	}

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
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// Track metrics.
	bytesCount := 0
	lineCount := 0
	for _, stream := range req.Streams {
		for _, entry := range stream.Entries {
			bytesCount += len(entry.Line)
			lineCount++
		}
	}
	bytesIngested.WithLabelValues(userID).Add(float64(bytesCount))
	linesIngested.WithLabelValues(userID).Add(float64(lineCount))

	// First we flatten out the request into a list of samples.
	// We use the heuristic of 1 sample per TS to size the array.
	// We also work out the hash value at the same time.
	streams := make([]streamTracker, 0, len(req.Streams))
	keys := make([]uint32, 0, len(req.Streams))
	var validationErr error
	validatedSamplesSize := 0
	validatedSamplesCount := 0

	validationContext := d.validator.getValidationContextFor(userID)

	for _, stream := range req.Streams {
		stream.Labels, err = d.parseStreamLabels(validationContext, stream.Labels, &stream)
		if err != nil {
			validationErr = err
			continue
		}
		n := 0
		for _, entry := range stream.Entries {
			if err := d.validator.ValidateEntry(validationContext, stream.Labels, entry); err != nil {
				validationErr = err
				continue
			}
			stream.Entries[n] = entry
			n++
			validatedSamplesSize += len(entry.Line)
			validatedSamplesCount++
		}
		stream.Entries = stream.Entries[:n]

		if len(stream.Entries) == 0 {
			continue
		}
		keys = append(keys, util.TokenFor(userID, stream.Labels))
		streams = append(streams, streamTracker{
			stream: stream,
		})
	}

	if len(streams) == 0 {
		return &logproto.PushResponse{}, validationErr
	}

	now := time.Now()
	if !d.ingestionRateLimiter.AllowN(now, userID, validatedSamplesSize) {
		// Return a 429 to indicate to the client they are being rate limited
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamplesCount))
		validation.DiscardedBytes.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamplesSize))
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(int(d.ingestionRateLimiter.Limit(now, userID)), validatedSamplesCount, validatedSamplesSize))
	}

	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.IngesterDesc

	samplesByIngester := map[string][]*streamTracker{}
	ingesterDescs := map[string]ring.IngesterDesc{}
	for i, key := range keys {
		replicationSet, err := d.ingestersRing.Get(key, ring.Write, descs[:0])
		if err != nil {
			return nil, err
		}

		streams[i].minSuccess = len(replicationSet.Ingesters) - replicationSet.MaxErrors
		streams[i].maxFailures = replicationSet.MaxErrors
		for _, ingester := range replicationSet.Ingesters {
			samplesByIngester[ingester.Addr] = append(samplesByIngester[ingester.Addr], &streams[i])
			ingesterDescs[ingester.Addr] = ingester
		}
	}

	tracker := pushTracker{
		done: make(chan struct{}),
		err:  make(chan error),
	}
	tracker.samplesPending.Store(int32(len(streams)))
	for ingester, samples := range samplesByIngester {
		go func(ingester ring.IngesterDesc, samples []*streamTracker) {
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

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendSamples(ctx context.Context, ingester ring.IngesterDesc, streamTrackers []*streamTracker, pushTracker *pushTracker) {
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
func (d *Distributor) sendSamplesErr(ctx context.Context, ingester ring.IngesterDesc, streams []*streamTracker) error {
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
	ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		ingesterAppendFailures.WithLabelValues(ingester.Addr).Inc()
	}
	return err
}

// Check implements the grpc healthcheck
func (*Distributor) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (d *Distributor) parseStreamLabels(vContext validationContext, key string, stream *logproto.Stream) (string, error) {
	labelVal, ok := d.labelCache.Get(key)
	if ok {
		return labelVal.(string), nil
	}
	ls, err := logql.ParseLabels(key)
	if err != nil {
		return "", httpgrpc.Errorf(http.StatusBadRequest, "error parsing labels: %v", err)
	}
	// ensure labels are correctly sorted.
	if err := d.validator.ValidateLabels(vContext, ls, *stream); err != nil {
		return "", err
	}
	lsVal := ls.String()
	d.labelCache.Add(key, lsVal)
	return lsVal, nil
}
