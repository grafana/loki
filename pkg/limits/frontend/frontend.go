// Package frontend contains provides a frontend service for ingest limits.
// It is responsible for receiving and answering gRPC requests from distributors,
// such as exceeds limits requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
package frontend

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	limits_client "github.com/grafana/loki/v3/pkg/limits/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	// ReasonExceedsMaxStreams is returned when a tenant exceeds the maximum
	// number of active streams as per their per-tenant limit.
	ReasonExceedsMaxStreams = "exceeds_max_streams"

	// ReasonExceedsRateLimit is returned when a tenant exceeds their maximum
	// rate limit as per their per-tenant limit.
	ReasonExceedsRateLimit = "exceeds_rate_limit"
)

type metrics struct {
	activeStreams          *prometheus.GaugeVec
	streamsExceedingLimits *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		activeStreams: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_limits_frontend_streams_active",
			Help: "The current number of active streams (seen within the window) per tenant.",
		}, []string{"tenant"}),
		streamsExceedingLimits: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingest_limits_frontend_streams_exceeding_limits_total",
			Help: "The total number of streams exceeding limits per tenant.",
		}, []string{"tenant", "reason"}),
	}
}

// Frontend is the limits-frontend service, and acts a service wrapper for
// all components needed to run the limits-frontend.
type Frontend struct {
	services.Service

	cfg    Config
	logger log.Logger

	limits                  Limits
	rateLimiter             *limiter.RateLimiter
	streamUsage             StreamUsageGatherer
	assignedPartitionsCache Cache[string, *logproto.GetAssignedPartitionsResponse]
	metrics                 *metrics

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher
}

// New returns a new Frontend.
func New(cfg Config, ringName string, limitsRing ring.ReadRing, limits Limits, logger log.Logger, reg prometheus.Registerer) (*Frontend, error) {
	// Set up a client pool for the limits service. The frontend will use this
	// to make RPCs that get the current stream usage to checks per-tenant limits.
	clientPoolFactory := limits_client.NewPoolFactory(cfg.ClientConfig)
	clientPool := limits_client.NewPool(
		ringName,
		cfg.ClientConfig.PoolConfig,
		limitsRing,
		clientPoolFactory,
		logger,
	)

	var assignedPartitionsCache Cache[string, *logproto.GetAssignedPartitionsResponse]
	if cfg.AssignedPartitionsCacheTTL == 0 {
		// When the TTL is 0, the cache is disabled.
		assignedPartitionsCache = NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()
	} else {
		assignedPartitionsCache = NewTTLCache[string, *logproto.GetAssignedPartitionsResponse](cfg.AssignedPartitionsCacheTTL)
	}

	rateLimiter := limiter.NewRateLimiter(newRateLimitsAdapter(limits), cfg.RecheckPeriod)
	streamUsage := NewRingStreamUsageGatherer(limitsRing, clientPool, cfg.NumPartitions, assignedPartitionsCache, logger)

	f := &Frontend{
		cfg:                     cfg,
		logger:                  logger,
		limits:                  limits,
		rateLimiter:             rateLimiter,
		streamUsage:             streamUsage,
		assignedPartitionsCache: assignedPartitionsCache,
		metrics:                 newMetrics(reg),
	}

	lifecycler, err := ring.NewLifecycler(cfg.LifecyclerConfig, f, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	f.lifecycler = lifecycler
	// Watch the lifecycler
	f.lifecyclerWatcher = services.NewFailureWatcher()
	f.lifecyclerWatcher.WatchService(f.lifecycler)

	servs := []services.Service{lifecycler, clientPool}
	mgr, err := services.NewManager(servs...)
	if err != nil {
		return nil, err
	}

	f.subservices = mgr
	f.subservicesWatcher = services.NewFailureWatcher()
	f.subservicesWatcher.WatchManager(f.subservices)
	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)

	return f, nil
}

// starting implements services.Service.
func (f *Frontend) starting(ctx context.Context) (err error) {
	defer func() {
		if err == nil {
			return
		}
		stopErr := services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
		if stopErr != nil {
			level.Error(f.logger).Log("msg", "failed to stop subservices", "err", stopErr)
		}
	}()

	level.Info(f.logger).Log("msg", "starting subservices")
	if err := services.StartManagerAndAwaitHealthy(ctx, f.subservices); err != nil {
		return fmt.Errorf("failed to start subservices: %w", err)
	}

	return nil
}

// running implements services.Service.
func (f *Frontend) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-f.subservicesWatcher.Chan():
		return fmt.Errorf("ingest limits frontend subservice failed: %w", err)
	}
}

// stopping implements services.Service.
func (f *Frontend) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
}

// ExceedsLimits implements logproto.IngestLimitsFrontendClient.
func (f *Frontend) ExceedsLimits(ctx context.Context, req *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	streamHashes := make([]uint64, 0, len(req.Streams))
	for _, stream := range req.Streams {
		streamHashes = append(streamHashes, stream.StreamHash)
	}
	resps, err := f.streamUsage.GetStreamUsage(ctx, GetStreamUsageRequest{
		Tenant:       req.Tenant,
		StreamHashes: streamHashes,
	})
	if err != nil {
		return nil, err
	}

	var (
		activeStreamsTotal uint64
		rateTotal          float64
	)
	// Sum the number of active streams and rates of all responses.
	for _, resp := range resps {
		activeStreamsTotal += resp.Response.ActiveStreams
		rateTotal += float64(resp.Response.Rate)
	}
	f.metrics.activeStreams.WithLabelValues(req.Tenant).Set(float64(activeStreamsTotal))

	// A slice containing the results returned to the caller.
	// If len(results) == 0 then the request does not exceed limits.
	var results []*logproto.ExceedsLimitsResult

	// Check if max streams limit would be exceeded.
	maxGlobalStreams := f.limits.MaxGlobalStreamsPerUser(req.Tenant)
	if activeStreamsTotal >= uint64(maxGlobalStreams) {
		for _, resp := range resps {
			for _, unknownStream := range resp.Response.UnknownStreams {
				results = append(results, &logproto.ExceedsLimitsResult{
					StreamHash: unknownStream,
					Reason:     ReasonExceedsMaxStreams,
				})
			}
		}
	}
	f.metrics.streamsExceedingLimits.WithLabelValues(
		req.Tenant,
		ReasonExceedsMaxStreams,
	).Add(float64(len(results)))

	// Check if rate limits would be exceeded.
	tenantRateLimit := f.rateLimiter.Limit(time.Now(), req.Tenant)
	if rateTotal > tenantRateLimit {
		// Rate limit would be exceeded, all streams must be reported
		// as exceeding limits.
		for _, streamHash := range streamHashes {
			results = append(results, &logproto.ExceedsLimitsResult{
				StreamHash: streamHash,
				Reason:     ReasonExceedsRateLimit,
			})
		}
		f.metrics.streamsExceedingLimits.WithLabelValues(
			req.Tenant,
			ReasonExceedsRateLimit,
		).Add(float64(len(streamHashes)))
	}

	return &logproto.ExceedsLimitsResponse{
		Tenant:  req.Tenant,
		Results: results,
	}, nil
}

func (f *Frontend) CheckReady(ctx context.Context) error {
	if f.State() != services.Running && f.State() != services.Stopping {
		return fmt.Errorf("ingest limits frontend not ready: %v", f.State())
	}

	err := f.lifecycler.CheckReady(ctx)
	if err != nil {
		level.Error(f.logger).Log("msg", "ingest limits frontend not ready", "err", err)
		return err
	}

	return nil
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest
// limits frontend instance.
func (f *Frontend) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another
// ingest limits frontend instance.
func (f *Frontend) TransferOut(_ context.Context) error {
	return nil
}
