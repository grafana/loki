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
	RejectedStreamReasonExceedsGlobalLimit = "exceeds_global_limit"
)

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

type Limits interface {
	MaxGlobalStreamsPerUser(userID string) int
}
type ringFunc func(context.Context, logproto.IngestLimitsClient) (*logproto.GetStreamUsageResponse, error)

// RingIngestLimitsService is an IngestLimitsService that uses the ring to read the responses
// from all limits backends.
type RingIngestLimitsService struct {
	ring   ring.ReadRing
	pool   *ring_client.Pool
	limits Limits
}

// NewRingIngestLimitsService returns a new RingIngestLimitsClient.
func NewRingIngestLimitsService(cfg Config, limits Limits, logger log.Logger, ring ring.ReadRing) *RingIngestLimitsService {
	factory := ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
		return NewIngestLimitsClient(cfg.ClientConfig, addr)
	})
	return &RingIngestLimitsService{
		ring:   ring,
		pool:   NewIngestLimitsClientPool("ingest-limits", cfg.ClientConfig.PoolConfig, ring, factory, logger),
		limits: limits,
	}
}

func (s *RingIngestLimitsService) forAllBackends(ctx context.Context, f ringFunc) ([]Response, error) {
	replicaSet, err := s.ring.GetReplicationSetForOperation(LimitsRead)
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

func (s *RingIngestLimitsService) ExceedsLimits(ctx context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	req := &logproto.GetStreamUsageRequest{
		Tenant: r.Tenant,
	}

	resps, err := s.forAllBackends(ctx, func(_ context.Context, client logproto.IngestLimitsClient) (*logproto.GetStreamUsageResponse, error) {
		return client.GetStreamUsage(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	maxGlobalStreams := s.limits.MaxGlobalStreamsPerUser(r.Tenant)

	var (
		activeStreamsTotal uint64
		uniqueStreamHashes = make(map[uint64]bool)
	)
	for _, resp := range resps {
		if resp.Response == nil {
			continue
		}

		activeStreamsTotal += resp.Response.ActiveStreams

		for _, stream := range resp.Response.RecordedStreams {
			if !uniqueStreamHashes[stream.StreamHash] {
				uniqueStreamHashes[stream.StreamHash] = true
				continue
			}
			activeStreamsTotal--
		}
	}

	if activeStreamsTotal < uint64(maxGlobalStreams) {
		return &logproto.ExceedsLimitsResponse{
			Tenant: r.Tenant,
		}, nil
	}

	var (
		rejectedStreams []*logproto.RejectedStream
	)
	for _, stream := range r.Streams {
		if !uniqueStreamHashes[stream.StreamHash] {
			rejectedStreams = append(rejectedStreams, &logproto.RejectedStream{
				StreamHash: stream.StreamHash,
				Reason:     RejectedStreamReasonExceedsGlobalLimit,
			})
		}
	}

	return &logproto.ExceedsLimitsResponse{
		Tenant:          r.Tenant,
		RejectedStreams: rejectedStreams,
	}, nil
}

type Response struct {
	Addr     string
	Response *logproto.GetStreamUsageResponse
}
