package frontend

import (
	"context"
	"slices"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

var (
	LimitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)
)

type getStreamUsageFunc func(context.Context, logproto.IngestLimitsClient, *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error)

// RingStreamUsageGatherer implements StreamUsageGatherer. It uses a ring to find
// limits instances.
type RingStreamUsageGatherer struct {
	logger log.Logger
	ring   ring.ReadRing
	pool   *ring_client.Pool
}

// NewRingStreamUsageGatherer returns a new RingStreamUsageGatherer.
func NewRingStreamUsageGatherer(ring ring.ReadRing, pool *ring_client.Pool, logger log.Logger) *RingStreamUsageGatherer {
	return &RingStreamUsageGatherer{
		logger: logger,
		ring:   ring,
		pool:   pool,
	}
}

// GetStreamUsage implements StreamUsageGatherer.
func (g *RingStreamUsageGatherer) GetStreamUsage(ctx context.Context, r GetStreamUsageRequest) ([]GetStreamUsageResponse, error) {
	if len(r.StreamHashes) == 0 {
		return nil, nil
	}
	return g.forAllBackends(ctx, r, func(
		ctx context.Context,
		client logproto.IngestLimitsClient,
		r *logproto.GetStreamUsageRequest,
	) (*logproto.GetStreamUsageResponse, error) {
		return client.GetStreamUsage(ctx, r)
	})
}

// TODO(grobinson): Need to rename this to something more accurate.
func (g *RingStreamUsageGatherer) forAllBackends(ctx context.Context, r GetStreamUsageRequest, f getStreamUsageFunc) ([]GetStreamUsageResponse, error) {
	replicaSet, err := g.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	return g.forGivenReplicaSet(ctx, replicaSet, r, f)
}

func (g *RingStreamUsageGatherer) forGivenReplicaSet(ctx context.Context, replicaSet ring.ReplicationSet, r GetStreamUsageRequest, f getStreamUsageFunc) ([]GetStreamUsageResponse, error) {
	partitions, err := g.perReplicaSetPartitions(ctx, replicaSet)
	if err != nil {
		return nil, err
	}

	gr, ctx := errgroup.WithContext(ctx)
	responses := make([]GetStreamUsageResponse, len(replicaSet.Instances))

	// TODO: We shouldn't query all instances since we know which instance holds which stream.
	for i, instance := range replicaSet.Instances {
		gr.Go(func() error {
			client, err := g.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}
			protoReq := &logproto.GetStreamUsageRequest{
				Tenant:       r.Tenant,
				StreamHashes: r.StreamHashes,
				Partitions:   partitions[instance.Addr],
			}

			resp, err := f(ctx, client.(logproto.IngestLimitsClient), protoReq)
			if err != nil {
				return err
			}
			responses[i] = GetStreamUsageResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}

	if err := gr.Wait(); err != nil {
		return nil, err
	}

	return responses, nil
}

func (g *RingStreamUsageGatherer) perReplicaSetPartitions(ctx context.Context, replicaSet ring.ReplicationSet) (map[string][]int32, error) {
	type getAssignedPartitionsResponse struct {
		Addr     string
		Response *logproto.GetAssignedPartitionsResponse
	}

	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]getAssignedPartitionsResponse, len(replicaSet.Instances))
	for i, instance := range replicaSet.Instances {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}
			resp, err := client.(logproto.IngestLimitsClient).GetAssignedPartitions(ctx, &logproto.GetAssignedPartitionsRequest{})
			if err != nil {
				return err
			}
			responses[i] = getAssignedPartitionsResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
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
