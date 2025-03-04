package frontend

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// RejectedStreamReasonExceedsGlobalLimit is the reason for rejecting a stream
	// because it exceeds the global per tenant limit.
	RejectedStreamReasonExceedsGlobalLimit = "exceeds_global_limit"

	// RejectedStreamReasonRateLimited is the reason for rejecting a stream
	// because it is rate limited.
	RejectedStreamReasonRateLimited = "rate_limited"
)

// Limits is the interface of the limits configuration
// builder to be passed to the frontend service.
type Limits interface {
	MaxGlobalStreamsPerUser(userID string) int
	IngestionRateBytes(userID string) float64
	IngestionBurstSizeBytes(userID string) int
}

type ingestionRateStrategy struct {
	limits Limits
}

func newIngestionRateStrategy(limits Limits) *ingestionRateStrategy {
	return &ingestionRateStrategy{limits: limits}
}

func (s *ingestionRateStrategy) Limit(tenantID string) float64 {
	return s.limits.IngestionRateBytes(tenantID)
}

func (s *ingestionRateStrategy) Burst(tenantID string) int {
	return s.limits.IngestionBurstSizeBytes(tenantID)
}

// IngestLimitsService is responsible for receiving, processing and
// validating requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
type IngestLimitsService interface {
	// ExceedsLimits checks if the request would exceed the current tenants
	// limits.
	ExceedsLimits(ctx context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error)
}

var (
	LimitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)
)

type metrics struct {
	tenantExceedsLimits   *prometheus.CounterVec
	tenantActiveStreams   *prometheus.GaugeVec
	tenantRejectedStreams *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		tenantExceedsLimits: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_frontend_exceeds_limits_total",
			Help:      "The total number of requests that exceeded limits per tenant.",
		}, []string{"tenant"}),
		tenantActiveStreams: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_frontend_streams_active",
			Help:      "The current number of active streams (seen within the window) per tenant.",
		}, []string{"tenant"}),
		tenantRejectedStreams: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_frontend_streams_rejected_total",
			Help:      "The total number of rejected streams per tenant when the global limit is exceeded.",
		}, []string{"tenant", "reason"}),
	}
}

type ringGetUsageFunc func(context.Context, logproto.IngestLimitsClient, []int32) (*logproto.GetStreamUsageResponse, error)

// RingIngestLimitsService is an IngestLimitsService that uses the ring to read the responses
// from all limits backends.
type RingIngestLimitsService struct {
	logger log.Logger

	ring ring.ReadRing
	pool *ring_client.Pool

	limits      Limits
	rateLimiter *limiter.RateLimiter

	metrics *metrics
}

// NewRingIngestLimitsService returns a new RingIngestLimitsClient.
func NewRingIngestLimitsService(ring ring.ReadRing, pool *ring_client.Pool, limits Limits, rateLimiter *limiter.RateLimiter, logger log.Logger, reg prometheus.Registerer) *RingIngestLimitsService {
	return &RingIngestLimitsService{
		logger:      logger,
		ring:        ring,
		pool:        pool,
		limits:      limits,
		rateLimiter: rateLimiter,
		metrics:     newMetrics(reg),
	}
}

func (s *RingIngestLimitsService) forAllBackends(ctx context.Context, f ringGetUsageFunc) ([]GetStreamUsageResponse, error) {
	replicaSet, err := s.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	return s.forGivenReplicaSet(ctx, replicaSet, f)
}

func (s *RingIngestLimitsService) forGivenReplicaSet(ctx context.Context, replicaSet ring.ReplicationSet, f ringGetUsageFunc) ([]GetStreamUsageResponse, error) {
	partitions, err := s.perReplicaSetPartitions(ctx, replicaSet)
	if err != nil {
		return nil, err
	}

	g, ctx := errgroup.WithContext(ctx)
	responses := make([]GetStreamUsageResponse, len(replicaSet.Instances))

	for i, instance := range replicaSet.Instances {
		g.Go(func() error {
			client, err := s.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}

			var partitionStr strings.Builder
			for _, partition := range partitions[instance.Addr] {
				partitionStr.WriteString(fmt.Sprintf("%d,", partition))
			}

			resp, err := f(ctx, client.(logproto.IngestLimitsClient), partitions[instance.Addr])
			if err != nil {
				return err
			}
			responses[i] = GetStreamUsageResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return responses, nil
}

func (s *RingIngestLimitsService) perReplicaSetPartitions(ctx context.Context, replicaSet ring.ReplicationSet) (map[string][]int32, error) {
	g, ctx := errgroup.WithContext(ctx)
	responses := make([]GetAssignedPartitionsResponse, len(replicaSet.Instances))
	for i, instance := range replicaSet.Instances {
		g.Go(func() error {
			client, err := s.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}
			resp, err := client.(logproto.IngestLimitsClient).GetAssignedPartitions(ctx, &logproto.GetAssignedPartitionsRequest{})
			if err != nil {
				return err
			}
			responses[i] = GetAssignedPartitionsResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	partitions := make(map[string][]int32)
	// Track highest value seen for each partition
	highestValues := make(map[int32]int64)
	// Track which addr has the highest value for each partition
	highestAddr := make(map[int32]string)

	// First pass - find highest values for each partition
	for _, resp := range responses {
		for partition, value := range resp.Response.AssignedPartitions {
			if currentHighest, exists := highestValues[partition]; !exists || value > currentHighest {
				highestValues[partition] = value
				highestAddr[partition] = resp.Addr
			}
		}
	}

	// Second pass - assign partitions to addrs that have the highest values
	for partition, addr := range highestAddr {
		partitions[addr] = append(partitions[addr], partition)
	}

	// Sort partition IDs for each address for consistent ordering
	for addr := range partitions {
		slices.Sort(partitions[addr])
	}

	return partitions, nil
}

func (s *RingIngestLimitsService) ExceedsLimits(ctx context.Context, req *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	streamHashes := make([]uint64, 0, len(req.Streams))
	for _, stream := range req.Streams {
		streamHashes = append(streamHashes, stream.StreamHash)
	}

	resps, err := s.forAllBackends(ctx, func(_ context.Context, client logproto.IngestLimitsClient, partitions []int32) (*logproto.GetStreamUsageResponse, error) {
		return client.GetStreamUsage(ctx, &logproto.GetStreamUsageRequest{
			Tenant:       req.Tenant,
			Partitions:   partitions,
			StreamHashes: streamHashes,
		})
	})
	if err != nil {
		return nil, err
	}

	maxGlobalStreams := s.limits.MaxGlobalStreamsPerUser(req.Tenant)

	var (
		activeStreamsTotal uint64
		tenantRate         int
	)
	for _, resp := range resps {
		activeStreamsTotal += resp.Response.ActiveStreams
		tenantRate += int(resp.Response.Rate)
	}

	s.metrics.tenantActiveStreams.WithLabelValues(req.Tenant).Set(float64(activeStreamsTotal))

	var (
		rejectedStreams    []*logproto.RejectedStream
		uniqueStreamHashes = make(map[uint64]bool)
	)

	tenantRateLimit := s.rateLimiter.Limit(time.Now(), req.Tenant)
	if float64(tenantRate) > tenantRateLimit {
		rateLimitedStreams := make([]*logproto.RejectedStream, 0, len(streamHashes))
		for _, streamHash := range streamHashes {
			rateLimitedStreams = append(rateLimitedStreams, &logproto.RejectedStream{
				StreamHash: streamHash,
				Reason:     RejectedStreamReasonRateLimited,
			})
		}

		// Count rejections by reason
		s.metrics.tenantExceedsLimits.WithLabelValues(req.Tenant).Inc()
		s.metrics.tenantRejectedStreams.WithLabelValues(req.Tenant, RejectedStreamReasonRateLimited).Add(float64(len(rateLimitedStreams)))

		return &logproto.ExceedsLimitsResponse{
			Tenant:          req.Tenant,
			RejectedStreams: rateLimitedStreams,
		}, nil
	}

	// Only process global limit if we're exceeding it
	if activeStreamsTotal >= uint64(maxGlobalStreams) {
		for _, resp := range resps {
			for _, unknownStream := range resp.Response.UnknownStreams {
				if !uniqueStreamHashes[unknownStream] {
					uniqueStreamHashes[unknownStream] = true
					rejectedStreams = append(rejectedStreams, &logproto.RejectedStream{
						StreamHash: unknownStream,
						Reason:     RejectedStreamReasonExceedsGlobalLimit,
					})
				}
			}
		}
	}

	if len(rejectedStreams) > 0 {
		s.metrics.tenantExceedsLimits.WithLabelValues(req.Tenant).Inc()

		// Count rejections by reason
		exceedsLimitCount := 0
		for _, rejected := range rejectedStreams {
			if rejected.Reason == RejectedStreamReasonExceedsGlobalLimit {
				exceedsLimitCount++
			}
		}

		if exceedsLimitCount > 0 {
			s.metrics.tenantRejectedStreams.WithLabelValues(req.Tenant, RejectedStreamReasonExceedsGlobalLimit).Add(float64(exceedsLimitCount))
		}
	}

	return &logproto.ExceedsLimitsResponse{
		Tenant:          req.Tenant,
		RejectedStreams: rejectedStreams,
	}, nil
}

type GetStreamUsageResponse struct {
	Addr     string
	Response *logproto.GetStreamUsageResponse
}

type GetAssignedPartitionsResponse struct {
	Addr     string
	Response *logproto.GetAssignedPartitionsResponse
}
