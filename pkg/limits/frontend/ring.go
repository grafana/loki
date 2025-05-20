package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

const (
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

var (
	LimitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)
)

// RingGatherer uses a ring to find limits instances.
type RingGatherer struct {
	logger                  log.Logger
	ring                    ring.ReadRing
	pool                    *ring_client.Pool
	numPartitions           int
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse]
}

// NewRingGatherer returns a new RingGatherer.
func NewRingGatherer(
	ring ring.ReadRing,
	pool *ring_client.Pool,
	numPartitions int,
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse],
	logger log.Logger,
) *RingGatherer {
	return &RingGatherer{
		logger:                  logger,
		ring:                    ring,
		pool:                    pool,
		numPartitions:           numPartitions,
		assignedPartitionsCache: assignedPartitionsCache,
	}
}

// ExceedsLimits implements ExceedsLimitsGatherer.
func (g *RingGatherer) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	if len(req.Streams) == 0 {
		return nil, nil
	}
	rs, err := g.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	partitionConsumers, err := g.getPartitionConsumers(ctx, rs.Instances)
	if err != nil {
		return nil, err
	}
	ownedStreams := make(map[string][]*proto.StreamMetadata)
	for _, s := range req.Streams {
		partition := int32(s.StreamHash % uint64(g.numPartitions))
		addr, ok := partitionConsumers[partition]
		if !ok {
			// TODO(grobinson): Drop streams when ok is false.
			level.Warn(g.logger).Log("msg", "no instance found for partition", "partition", partition)
			continue
		}
		ownedStreams[addr] = append(ownedStreams[addr], s)
	}
	errg, ctx := errgroup.WithContext(ctx)
	responseCh := make(chan *proto.ExceedsLimitsResponse, len(ownedStreams))
	for addr, streams := range ownedStreams {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(addr)
			if err != nil {
				level.Error(g.logger).Log("msg", "failed to get client for instance", "instance", addr, "err", err.Error())
				return err
			}
			resp, err := client.(proto.IngestLimitsClient).ExceedsLimits(ctx, &proto.ExceedsLimitsRequest{
				Tenant:  req.Tenant,
				Streams: streams,
			})
			if err != nil {
				return err
			}
			responseCh <- resp
			return nil
		})
	}
	if err = errg.Wait(); err != nil {
		return nil, err
	}
	close(responseCh)
	responses := make([]*proto.ExceedsLimitsResponse, 0, len(rs.Instances))
	for resp := range responseCh {
		responses = append(responses, resp)
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
func (g *RingGatherer) getZoneAwarePartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[string]map[int32]string, error) {
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
	response *proto.GetAssignedPartitionsResponse
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
func (g *RingGatherer) getPartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[int32]string, error) {
	errg, ctx := errgroup.WithContext(ctx)
	responseCh := make(chan getAssignedPartitionsResponse, len(instances))
	for _, instance := range instances {
		errg.Go(func() error {
			// We use a cache to eliminate redundant gRPC requests for
			// GetAssignedPartitions as the set of assigned partitions is
			// expected to be stable outside consumer rebalances.
			if resp, ok := g.assignedPartitionsCache.get(instance.Addr); ok {
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
			resp, err := client.(proto.IngestLimitsClient).GetAssignedPartitions(ctx, &proto.GetAssignedPartitionsRequest{})
			if err != nil {
				level.Error(g.logger).Log("failed to get assigned partitions for instance", "instance", instance.Addr, "err", err.Error())
				return nil
			}
			g.assignedPartitionsCache.set(instance.Addr, resp)
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
