package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
	logger                  log.Logger
	ring                    ring.ReadRing
	pool                    *ring_client.Pool
	numPartitions           int
	assignedPartitionsCache Cache[string, *logproto.GetAssignedPartitionsResponse]
}

// NewRingStreamUsageGatherer returns a new RingStreamUsageGatherer.
func NewRingStreamUsageGatherer(
	ring ring.ReadRing,
	pool *ring_client.Pool,
	numPartitions int,
	assignedPartitionsCache Cache[string, *logproto.GetAssignedPartitionsResponse],
	logger log.Logger,
) *RingStreamUsageGatherer {
	return &RingStreamUsageGatherer{
		logger:                  logger,
		ring:                    ring,
		pool:                    pool,
		numPartitions:           numPartitions,
		assignedPartitionsCache: assignedPartitionsCache,
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
	partitionConsumers, err := g.getPartitionConsumers(ctx, rs.Instances)
	if err != nil {
		return nil, err
	}

	ownedStreamHeashes := make(map[string][]uint64)
	for _, hash := range r.StreamHashes {
		partitionID := int32(hash % uint64(g.numPartitions))
		addr, ok := partitionConsumers[partitionID]
		if !ok {
			// TODO Replace with a metric for partitions missing owners.
			level.Warn(g.logger).Log("msg", "no instance found for partition", "partition", partitionID)
			continue
		}
		ownedStreamHeashes[addr] = append(ownedStreamHeashes[addr], hash)
	}

	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]GetStreamUsageResponse, len(rs.Instances))

	// Query all healthy instances in parallel,
	// send requested stream hahes only to owning instances.
	for i, instance := range rs.Instances {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(instance.Addr)
			if err != nil {
				return err
			}

			protoReq := &logproto.GetStreamUsageRequest{Tenant: r.Tenant}

			// Only send stream hashes to the instance that owns them.
			// This eliminates the need in downstream callers to filter
			// duplicate unknown streams.
			if hashes, ok := ownedStreamHeashes[instance.Addr]; ok {
				protoReq.StreamHashes = hashes
			}

			resp, err := client.(logproto.IngestLimitsClient).GetStreamUsage(ctx, protoReq)
			if err != nil {
				return err
			}

			responses[i] = GetStreamUsageResponse{Addr: instance.Addr, Response: resp}
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return nil, err
	}

	return responses, nil
}

type zonePartitionConsumersResult struct {
	zone       string
	partitions map[int32]string
}

// getZoneAwarePartitionConsumers returns partition consumers for each zone
// in the replication set. If a zone has no active partition consumers, the
// zone will still be returned but its partition consumers will be nil.
// If ZoneAwarenessEnabled is false, it returns all partition consumers under
// a pseudo-zone ("").
func (g *RingStreamUsageGatherer) getZoneAwarePartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[string]map[int32]string, error) {
	zoneDescs := make(map[string][]ring.InstanceDesc)
	for _, instance := range instances {
		zoneDescs[instance.Zone] = append(zoneDescs[instance.Zone], instance)
	}
	// Get the partition consumers for each zone.
	resultsCh := make(chan zonePartitionConsumersResult, len(zoneDescs))
	errg, ctx := errgroup.WithContext(ctx)
	for zone, instances := range zoneDescs {
		errg.Go(func() error {
			res, err := g.getPartitionConsumers(ctx, instances)
			if err != nil {
				level.Error(g.logger).Log("msg", "failed to get partition consumers for zone", "zone", zone, "err", err.Error())
			}
			// Even if the consumers could not be fetched for a zone, we
			// should still return the zone.
			resultsCh <- zonePartitionConsumersResult{
				zone:       zone,
				partitions: res,
			}
			return nil
		})
	}
	_ = errg.Wait()
	close(resultsCh)
	results := make(map[string]map[int32]string)
	for result := range resultsCh {
		results[result.zone] = result.partitions
	}
	return results, nil
}

type getAssignedPartitionsResponse struct {
	addr     string
	response *logproto.GetAssignedPartitionsResponse
}

// getPartitionConsumers returns the consumer for each partition.

// In some cases, it might not be possible to know the consumer for a
// partition. If this happens, it returns the consumers for a subset of
// partitions that it does know about.
//
// For example, if a partition does not have a consumer then the partition
// will be absent from the result. Likewise, if an instance does not respond,
// the partition that it consumes will be absent from the result too. This
// also means that if no partitions are assigned consumers, or if no instances
// respond, the result will be empty.
//
// This method is not zone-aware, so if ZoneAwarenessEnabled is true, it
// should be called once for each zone, and instances should be filtered to
// the respective zone. Alternatively, you can pass all instances for all zones
// to find the most up to date consumer for each partition across all zones.
func (g *RingStreamUsageGatherer) getPartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[int32]string, error) {
	errg, ctx := errgroup.WithContext(ctx)
	responseCh := make(chan getAssignedPartitionsResponse, len(instances))
	for _, instance := range instances {
		errg.Go(func() error {
			// We use a cache to eliminate redundant gRPC requests for
			// GetAssignedPartitions as the set of assigned partitions is
			// expected to be stable outside consumer rebalances.
			if resp, ok := g.assignedPartitionsCache.Get(instance.Addr); ok {
				responseCh <- getAssignedPartitionsResponse{
					addr:     instance.Addr,
					response: resp,
				}
				return nil
			}
			client, err := g.pool.GetClientFor(instance.Addr)
			if err != nil {
				level.Error(g.logger).Log("failed to get client for instance", "instance", instance.Addr, "err", err.Error())
				return nil
			}
			resp, err := client.(logproto.IngestLimitsClient).GetAssignedPartitions(ctx, &logproto.GetAssignedPartitionsRequest{})
			if err != nil {
				level.Error(g.logger).Log("failed to get assigned partitions for instance", "instance", instance.Addr, "err", err.Error())
				return nil
			}
			g.assignedPartitionsCache.Set(instance.Addr, resp)
			responseCh <- getAssignedPartitionsResponse{
				addr:     instance.Addr,
				response: resp,
			}
			return nil
		})
	}
	_ = errg.Wait()
	close(responseCh)
	highestTimestamp := make(map[int32]int64)
	assigned := make(map[int32]string)
	for resp := range responseCh {
		for partition, assignedAt := range resp.response.AssignedPartitions {
			if t := highestTimestamp[partition]; t < assignedAt {
				highestTimestamp[partition] = assignedAt
				assigned[partition] = resp.addr
			}
		}
	}
	return assigned, nil
}
