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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

const (
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

var (
	LimitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

	// defaultZoneCmp compares two zones using [strings.Compare].
	defaultZoneCmp = func(a, b string) int {
		return strings.Compare(a, b)
	}
)

// ringGatherer uses a ring to find limits instances.
type ringGatherer struct {
	logger                  log.Logger
	ring                    ring.ReadRing
	pool                    *ring_client.Pool
	numPartitions           int
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse]
	zoneCmp                 func(a, b string) int

	// Metrics.
	streams       prometheus.Counter
	streamsFailed prometheus.Counter
}

// newRingGatherer returns a new ringGatherer.
func newRingGatherer(
	ring ring.ReadRing,
	pool *ring_client.Pool,
	numPartitions int,
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse],
	logger log.Logger,
	reg prometheus.Registerer,
) *ringGatherer {
	return &ringGatherer{
		logger:                  logger,
		ring:                    ring,
		pool:                    pool,
		numPartitions:           numPartitions,
		assignedPartitionsCache: assignedPartitionsCache,
		zoneCmp:                 defaultZoneCmp,
		streams: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_streams_total",
				Help: "The total number of received streams.",
			},
		),
		streamsFailed: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_streams_failed_total",
				Help: "The total number of received streams that could not be checked.",
			},
		),
	}
}

// ExceedsLimits implements the [exceedsLimitsGatherer] interface.
func (g *ringGatherer) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	if len(req.Streams) == 0 {
		return nil, nil
	}
	rs, err := g.ring.GetAllHealthy(LimitsRead)
	if err != nil {
		return nil, err
	}
	// Get the partition consumers for each zone.
	zonesPartitions, err := g.getZoneAwarePartitionConsumers(ctx, rs.Instances)
	if err != nil {
		return nil, err
	}
	// In practice we want zones to be queried in random order to spread
	// reads. However, in tests we want a deterministic order so test cases
	// are stable and reproducible. Having a custom sort func supports both
	// use cases as zoneCmp can be switched out in tests.
	zonesToQuery := make([]string, 0, len(zonesPartitions))
	for zone := range zonesPartitions {
		zonesToQuery = append(zonesToQuery, zone)
	}
	slices.SortFunc(zonesToQuery, g.zoneCmp)
	// Make a copy of the streams from the request. We will prune this slice
	// each time we receive the responses from a zone.
	streams := make([]*proto.StreamMetadata, 0, len(req.Streams))
	streams = append(streams, req.Streams...)
	g.streams.Add(float64(len(streams)))
	// Query each zone as ordered in zonesToQuery. If a zone answers all
	// streams, the request is satisfied and there is no need to query
	// subsequent zones. If a zone answers just a subset of streams
	// (i.e. the instance that is consuming a partition is unavailable or the
	// partition that owns one or more streams does not have a consumer)
	// then query the next zone for the remaining streams. We repeat this
	// process until all streams have been queried or we have exhausted all
	// zones.
	responses := make([]*proto.ExceedsLimitsResponse, 0)
	for _, zone := range zonesToQuery {
		resps, answered, err := g.doExceedsLimitsRPCs(ctx, req.Tenant, streams, zonesPartitions[zone], zone)
		if err != nil {
			continue
		}
		responses = append(responses, resps...)
		// Remove the answered streams from the slice. The slice of answered
		// streams must be sorted so we can use sort.Search to subtract the
		// two slices.
		slices.Sort(answered)
		streams = slices.DeleteFunc(streams, func(stream *proto.StreamMetadata) bool {
			// see https://pkg.go.dev/sort#Search
			i := sort.Search(len(answered), func(i int) bool {
				return answered[i] >= stream.StreamHash
			})
			return i < len(answered) && answered[i] == stream.StreamHash
		})
		// All streams been checked against per-tenant limits.
		if len(streams) == 0 {
			break
		}
	}
	g.streamsFailed.Add(float64(len(streams)))
	return responses, nil
}

func (g *ringGatherer) doExceedsLimitsRPCs(ctx context.Context, tenant string, streams []*proto.StreamMetadata, partitions map[int32]string, zone string) ([]*proto.ExceedsLimitsResponse, []uint64, error) {
	// For each stream, figure out which instance consume its partition.
	instancesForStreams := make(map[string][]*proto.StreamMetadata)
	for _, stream := range streams {
		partition := int32(stream.StreamHash % uint64(g.numPartitions))
		addr, ok := partitions[partition]
		if !ok {
			level.Warn(g.logger).Log("msg", "no instance found for partition", "partition", partition, "zone", zone)
			continue
		}
		instancesForStreams[addr] = append(instancesForStreams[addr], stream)
	}
	errg, ctx := errgroup.WithContext(ctx)
	responseCh := make(chan *proto.ExceedsLimitsResponse, len(instancesForStreams))
	answeredCh := make(chan uint64, len(streams))
	for addr, streams := range instancesForStreams {
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(addr)
			if err != nil {
				level.Error(g.logger).Log("msg", "failed to get client for instance", "instance", addr, "err", err.Error())
				return nil
			}
			resp, err := client.(proto.IngestLimitsClient).ExceedsLimits(ctx, &proto.ExceedsLimitsRequest{
				Tenant:  tenant,
				Streams: streams,
			})
			if err != nil {
				level.Error(g.logger).Log("failed check execeed limits for instance", "instance", addr, "err", err.Error())
				return nil
			}
			responseCh <- resp
			for _, stream := range streams {
				answeredCh <- stream.StreamHash
			}
			return nil
		})
	}
	_ = errg.Wait()
	close(responseCh)
	close(answeredCh)
	responses := make([]*proto.ExceedsLimitsResponse, 0, len(instancesForStreams))
	for r := range responseCh {
		responses = append(responses, r)
	}
	answered := make([]uint64, 0, len(streams))
	for streamHash := range answeredCh {
		answered = append(answered, streamHash)
	}
	return responses, answered, nil
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
func (g *ringGatherer) getZoneAwarePartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[string]map[int32]string, error) {
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
func (g *ringGatherer) getPartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[int32]string, error) {
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
			resp, err := client.(proto.IngestLimitsClient).GetAssignedPartitions(ctx, &proto.GetAssignedPartitionsRequest{})
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
