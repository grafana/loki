package frontend

import (
	"context"
	"slices"
	"sort"
	"strings"

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

var (
	// lexicoCmp compares two strings lexicographically.
	lexicoCmp = func(a, b string) int {
		return strings.Compare(a, b)
	}
)

// RingStreamUsageGatherer implements StreamUsageGatherer. It uses a ring to find
// limits instances.
type RingStreamUsageGatherer struct {
	logger                  log.Logger
	ring                    ring.ReadRing
	pool                    *ring_client.Pool
	numPartitions           int
	assignedPartitionsCache Cache[string, *logproto.GetAssignedPartitionsResponse]
	zoneCmp                 func(a, b string) int
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
		zoneCmp:                 lexicoCmp,
	}
}

// GetStreamUsage implements StreamUsageGatherer.
func (g *RingStreamUsageGatherer) GetStreamUsage(ctx context.Context, r GetStreamUsageRequest) ([]GetStreamUsageResponse, error) {
	if len(r.StreamHashes) == 0 {
		return nil, nil
	}
	// Get all healthy instances across all zones.
	rs, err := g.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	// Get the partition consumers for each zone.
	zonesPartitions, err := g.getZoneAwarePartitionConsumers(ctx, rs.Instances)
	if err != nil {
		return nil, err
	}
	// In practice, we want zones to be queried in random order to spread
	// reads. However, in tests we want a deterministic order so test cases
	// are stable and reproducible. When compared to just iterating over
	// a map, this allows us to achieve both.
	zonesToQuery := make([]string, len(zonesPartitions))
	for zone := range zonesPartitions {
		zonesToQuery = append(zonesToQuery, zone)
	}
	slices.SortFunc(zonesToQuery, g.zoneCmp)
	// Make a copy of the stream hashes as we will prune this slice each time
	// we receive the responses from a zone.
	streamHashesToQuery := make([]uint64, len(r.StreamHashes))
	copy(streamHashesToQuery, r.StreamHashes)
	// Query each zone as ordered in zonesToQuery. If a zone answers all
	// stream hashes, the request is satisifed and there is no need to query
	// subsequent zones. If a zone answers just a subset of stream hashes
	// (i.e. the instance that is consuming a partition is unavailable or the
	// partition that owns one or more stream hashes does not have a consumer)
	// then query the next zone for the remaining stream hashes. We repeat
	// this process until all stream hashes have been queried or we have
	// exhausted all zones.
	responses := make([]GetStreamUsageResponse, 0)
	for _, zone := range zonesToQuery {
		result, streamHashesToDelete, err := g.getStreamUsage(ctx, r.Tenant, streamHashesToQuery, zonesPartitions[zone])
		if err != nil {
			continue
		}
		responses = append(responses, result...)
		// Delete the queried stream hashes from the slice of stream hashes
		// to query. The slice of queried stream hashes must be sorted so we
		// can use sort.Search to subtract the two slices.
		slices.Sort(streamHashesToDelete)
		streamHashesToQuery = slices.DeleteFunc(streamHashesToQuery, func(streamHashToQuery uint64) bool {
			// see https://pkg.go.dev/sort#Search
			i := sort.Search(len(streamHashesToDelete), func(i int) bool {
				return streamHashesToDelete[i] >= streamHashToQuery
			})
			return i < len(streamHashesToDelete) && streamHashesToDelete[i] == streamHashToQuery
		})
		// All stream hashes have been queried.
		if len(streamHashesToQuery) == 0 {
			break
		}
	}
	// Treat remaining stream hashes as unknown streams.
	if len(streamHashesToQuery) > 0 {
		responses = append(responses, GetStreamUsageResponse{
			Response: &logproto.GetStreamUsageResponse{
				Tenant:         "test",
				UnknownStreams: streamHashesToQuery,
			},
		})
	}
	return responses, nil
}

type getStreamUsageResponse struct {
	addr         string
	response     *logproto.GetStreamUsageResponse
	streamHashes []uint64
}

func (g *RingStreamUsageGatherer) getStreamUsage(ctx context.Context, tenant string, streamHashes []uint64, partitions map[int32]string) ([]GetStreamUsageResponse, []uint64, error) {
	instancesToQuery := make(map[string][]uint64)
	for _, streamHash := range streamHashes {
		partitionID := int32(streamHash % uint64(g.numPartitions))
		addr, ok := partitions[partitionID]
		if !ok {
			// TODO Replace with a metric for partitions missing owners.
			level.Warn(g.logger).Log("msg", "no instance found for partition", "partition", partitionID)
			continue
		}
		instancesToQuery[addr] = append(instancesToQuery[addr], streamHash)
	}
	// Get the stream usage from each instance.
	responseCh := make(chan getStreamUsageResponse, len(instancesToQuery))
	errg, ctx := errgroup.WithContext(ctx)
	for addr, streamHashes := range instancesToQuery {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(addr)
			if err != nil {
				level.Error(g.logger).Log("failed to get client for instance", "instance", addr, "err", err.Error())
				return nil
			}
			protoReq := &logproto.GetStreamUsageRequest{
				Tenant:       tenant,
				StreamHashes: streamHashes,
			}
			resp, err := client.(logproto.IngestLimitsClient).GetStreamUsage(ctx, protoReq)
			if err != nil {
				level.Error(g.logger).Log("failed to get stream usage for instance", "instance", addr, "err", err.Error())
				return nil
			}
			responseCh <- getStreamUsageResponse{
				addr:         addr,
				response:     resp,
				streamHashes: streamHashes,
			}
			return nil
		})
	}
	errg.Wait() //nolint
	close(responseCh)
	responses := make([]GetStreamUsageResponse, 0, len(instancesToQuery))
	queriedStreamHashes := make([]uint64, 0, len(streamHashes))
	for r := range responseCh {
		responses = append(responses, GetStreamUsageResponse{
			Addr:     r.addr,
			Response: r.response,
		})
		queriedStreamHashes = append(queriedStreamHashes, r.streamHashes...)
	}
	return responses, queriedStreamHashes, nil
}

type zonePartitionConsumersResult struct {
	zone       string
	partitions map[int32]string
}

// getZoneAwarePartitionConsumers returns partition consumers for each zone
// in the replication set. If a zone has no active partition consumers, the
// zone will still be returned but its partition consumers will be nil.
// If ZoneAwarenessEnabled is false, it returns all partition consumers under
// a psuedo-zone ("").
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
