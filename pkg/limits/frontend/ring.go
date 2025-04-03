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
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

var (
	LimitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)
)

// RingStreamUsageGatherer implements StreamUsageGatherer. It uses a ring to find
// limits instances.
type RingStreamUsageGatherer struct {
	logger        log.Logger
	ring          ring.ReadRing
	pool          *ring_client.Pool
	numPartitions int
}

// NewRingStreamUsageGatherer returns a new RingStreamUsageGatherer.
func NewRingStreamUsageGatherer(ring ring.ReadRing, pool *ring_client.Pool, logger log.Logger, numPartitions int) *RingStreamUsageGatherer {
	return &RingStreamUsageGatherer{
		logger:        logger,
		ring:          ring,
		pool:          pool,
		numPartitions: numPartitions,
	}
}

// GetStreamUsage implements StreamUsageGatherer.
func (g *RingStreamUsageGatherer) GetStreamUsage(ctx context.Context, r GetStreamUsageRequest) ([]GetStreamUsageResponse, error) {
	if len(r.StreamHashes) == 0 {
		return nil, nil
	}
	return g.forAllBackends(ctx, r)
}

// TODO(grobinson): Need to rename this to something more accurate.
func (g *RingStreamUsageGatherer) forAllBackends(ctx context.Context, r GetStreamUsageRequest) ([]GetStreamUsageResponse, error) {
	rs, err := g.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	return g.forGivenReplicaSet(ctx, rs, r)
}

func (g *RingStreamUsageGatherer) forGivenReplicaSet(ctx context.Context, rs ring.ReplicationSet, r GetStreamUsageRequest) ([]GetStreamUsageResponse, error) {
	partitionConsumers, err := g.getPartitionConsumers(ctx, rs)
	if err != nil {
		return nil, err
	}

	type consumerReq struct {
		addr     string
		protoReq *logproto.GetStreamUsageRequest
	}

	requests := make([]consumerReq, 0, len(rs.Instances))

outer:
	for _, hash := range r.StreamHashes {
		partitionID := int32(hash % uint64(g.numPartitions))
		addr, ok := partitionConsumers[partitionID]
		if !ok {
			continue
		}

		for _, req := range requests {
			if req.addr == addr {
				req.protoReq.StreamHashes = append(req.protoReq.StreamHashes, hash)
				continue outer
			}
		}

		c := consumerReq{
			addr: addr,
			protoReq: &logproto.GetStreamUsageRequest{
				Tenant:       r.Tenant,
				StreamHashes: []uint64{hash},
			},
		}

		requests = append(requests, c)
	}

	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]GetStreamUsageResponse, len(requests))

	// Query each instance for stream usage
	for i, req := range requests {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(req.addr)
			if err != nil {
				return err
			}

			resp, err := client.(logproto.IngestLimitsClient).GetStreamUsage(ctx, req.protoReq)
			if err != nil {
				return err
			}

			responses[i] = GetStreamUsageResponse{Addr: req.addr, Response: resp}
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return nil, err
	}

	return responses, nil
}

type getAssignedPartitionsResponse struct {
	Addr     string
	Response *logproto.GetAssignedPartitionsResponse
}

func (g *RingStreamUsageGatherer) getPartitionConsumers(ctx context.Context, rs ring.ReplicationSet) (map[int32]string, error) {
	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]getAssignedPartitionsResponse, len(rs.Instances))

	// Get the partitions assigned to each instance.
	for i, instance := range rs.Instances {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}
			resp, err := client.(logproto.IngestLimitsClient).GetAssignedPartitions(ctx, &logproto.GetAssignedPartitionsRequest{})
			if err != nil {
				return err
			}
			// No need for a mutex here as responses is a "Structured variable"
			// as described in https://go.dev/ref/spec#Variables.
			responses[i] = getAssignedPartitionsResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}
	if err := errg.Wait(); err != nil {
		return nil, err
	}

	// Deduplicate the partitions. This can happen if the call to
	// GetAssignedPartitions is interleaved with a partition rebalance, such
	// that two or more instances claim to be the consumer of the same
	// partition at the same time. In case of conflicts, choose the instance
	// with the latest timestamp.
	highestTimestamp := make(map[int32]int64)
	assigned := make(map[int32]string)
	for _, resp := range responses {
		for partition, assignedAt := range resp.Response.AssignedPartitions {
			if t := highestTimestamp[partition]; t < assignedAt {
				highestTimestamp[partition] = assignedAt
				assigned[partition] = resp.Addr
			}
		}
	}

	return assigned, nil
}
