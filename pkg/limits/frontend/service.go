package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	// RejectedStreamReasonExceedsGlobalLimit is the reason for rejecting a stream
	// because it exceeds the global per tenant limit.
	RejectedStreamReasonExceedsGlobalLimit = "exceeds_global_limit"
)

// Limits is the interface of the limits confgiration
// builder to be passed to the frontend service.
type Limits interface {
	MaxGlobalStreamsPerUser(userID string) int
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

type ringFunc func(context.Context, logproto.IngestLimitsClient) (*logproto.GetStreamUsageResponse, error)

// RingIngestLimitsService is an IngestLimitsService that uses the ring to read the responses
// from all limits backends.
type RingIngestLimitsService struct {
	logger log.Logger

	ring ring.ReadRing
	pool *ring_client.Pool

	limits Limits
}

// NewRingIngestLimitsService returns a new RingIngestLimitsClient.
func NewRingIngestLimitsService(ring ring.ReadRing, pool *ring_client.Pool, limits Limits, logger log.Logger) *RingIngestLimitsService {
	return &RingIngestLimitsService{
		logger: logger,
		ring:   ring,
		pool:   pool,
		limits: limits,
	}
}

func (s *RingIngestLimitsService) forAllBackends(ctx context.Context, f ringFunc) ([]Response, error) {
	replicaSet, err := s.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	return s.forGivenReplicaSet(ctx, replicaSet, f)
}

func (s *RingIngestLimitsService) forGivenReplicaSet(ctx context.Context, replicaSet ring.ReplicationSet, f ringFunc) ([]Response, error) {
	g, ctx := errgroup.WithContext(ctx)
	responses := make([]Response, len(replicaSet.Instances))
	for i, instance := range replicaSet.Instances {
		g.Go(func() error {
			client, err := s.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}
			resp, err := f(ctx, client.(logproto.IngestLimitsClient))
			if err != nil {
				return err
			}
			responses[i] = Response{Addr: instance.Addr, Response: resp}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return responses, nil
}

func (s *RingIngestLimitsService) ExceedsLimits(ctx context.Context, req *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	resps, err := s.forAllBackends(ctx, func(_ context.Context, client logproto.IngestLimitsClient) (*logproto.GetStreamUsageResponse, error) {
		return client.GetStreamUsage(ctx, &logproto.GetStreamUsageRequest{
			Tenant: req.Tenant,
		})
	})
	if err != nil {
		return nil, err
	}

	maxGlobalStreams := s.limits.MaxGlobalStreamsPerUser(req.Tenant)

	var (
		activeStreamsTotal uint64
		uniqueStreamHashes = make(map[uint64]bool)
	)
	for _, resp := range resps {
		var duplicates uint64
		// Record the unique stream hashes
		// and count duplicates active streams
		// to be subtracted from the total
		for _, stream := range resp.Response.RecordedStreams {
			if uniqueStreamHashes[stream.StreamHash] {
				duplicates++
				continue
			}
			uniqueStreamHashes[stream.StreamHash] = true
		}

		activeStreamsTotal += resp.Response.ActiveStreams - duplicates
	}

	if activeStreamsTotal < uint64(maxGlobalStreams) {
		return &logproto.ExceedsLimitsResponse{
			Tenant: req.Tenant,
		}, nil
	}

	var rejectedStreams []*logproto.RejectedStream
	for _, stream := range req.Streams {
		if !uniqueStreamHashes[stream.StreamHash] {
			rejectedStreams = append(rejectedStreams, &logproto.RejectedStream{
				StreamHash: stream.StreamHash,
				Reason:     RejectedStreamReasonExceedsGlobalLimit,
			})
		}
	}

	return &logproto.ExceedsLimitsResponse{
		Tenant:          req.Tenant,
		RejectedStreams: rejectedStreams,
	}, nil
}

type Response struct {
	Addr     string
	Response *logproto.GetStreamUsageResponse
}
