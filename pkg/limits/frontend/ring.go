package frontend

import (
	"context"
	"iter"
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

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

const (
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

var (
	limitsRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

	// defaultZoneCmp compares two zones using [strings.Compare].
	defaultZoneCmp = func(a, b string) int {
		return strings.Compare(a, b)
	}
)

// ringLimitsClient uses a ring to find limits instances.
type ringLimitsClient struct {
	logger                  log.Logger
	ring                    ring.ReadRing
	pool                    *ring_client.Pool
	numPartitions           int
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse]
	zoneCmp                 func(a, b string) int

	// Metrics.
	partitionsMissing *prometheus.CounterVec
}

// newRingLimitsClient returns a new ringLimitsClient.
func newRingLimitsClient(
	ring ring.ReadRing,
	pool *ring_client.Pool,
	numPartitions int,
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse],
	logger log.Logger,
	reg prometheus.Registerer,
) *ringLimitsClient {
	return &ringLimitsClient{
		logger:                  logger,
		ring:                    ring,
		pool:                    pool,
		numPartitions:           numPartitions,
		assignedPartitionsCache: assignedPartitionsCache,
		zoneCmp:                 defaultZoneCmp,
		partitionsMissing: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_partitions_missing_total",
				Help: "The total number of times an instance was missing for a requested partition.",
			},
			[]string{"zone"},
		),
	}
}

// ExceedsLimits implements the [exceedsLimitsGatherer] interface.
func (r *ringLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	var resp proto.ExceedsLimitsResponse
	if len(req.Streams) == 0 {
		return &resp, nil
	}
	unanswered, err := r.exhaustAllZones(ctx, req.Tenant, req.Streams, r.newExceedsLimitsRPCsFunc(&resp))
	if err != nil {
		return nil, err
	}
	// Any unanswered streams after exhausting all zones must be failed.
	if len(unanswered) > 0 {
		failed := make([]*proto.ExceedsLimitsResult, 0, len(unanswered))
		for _, stream := range unanswered {
			failed = append(failed, &proto.ExceedsLimitsResult{
				StreamHash: stream.StreamHash,
				Reason:     uint32(limits.ReasonFailed),
			})
		}
		resp.Results = append(resp.Results, failed...)
	}
	return &resp, nil
}

// UpdateRates implements the [exceedsLimitsGatherer] interface.
func (r *ringLimitsClient) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) (*proto.UpdateRatesResponse, error) {
	var resp proto.UpdateRatesResponse
	if len(req.Streams) == 0 {
		return &resp, nil
	}
	unanswered, err := r.exhaustAllZones(ctx, req.Tenant, req.Streams, r.newUpdateRatesRPCsFunc(&resp))
	if err != nil {
		return nil, err
	}
	// Any unanswered streams after exhausting all zones have a rate of 0.
	if len(unanswered) > 0 {
		failed := make([]*proto.UpdateRatesResult, 0, len(unanswered))
		for _, stream := range unanswered {
			failed = append(failed, &proto.UpdateRatesResult{
				StreamHash: stream.StreamHash,
				Rate:       0,
			})
		}
		resp.Results = append(resp.Results, failed...)
	}
	return &resp, nil
}

// newExceedsLimitsRPCsFunc returns a doRPCsFunc that executes the ExceedsLimits
// RPCs for the instances in a zone.
func (r *ringLimitsClient) newExceedsLimitsRPCsFunc(resp *proto.ExceedsLimitsResponse) doRPCsFunc {
	return func(
		ctx context.Context,
		tenant string,
		streams []*proto.StreamMetadata,
		zone string,
		consumers map[int32]string,
	) ([]uint64, error) {
		errg, ctx := errgroup.WithContext(ctx)
		instancesForStreams := r.instancesForStreams(streams, zone, consumers)
		responseCh := make(chan *proto.ExceedsLimitsResponse, len(instancesForStreams))
		answeredCh := make(chan uint64, len(streams))
		for addr, streams := range instancesForStreams {
			errg.Go(func() error {
				client, err := r.pool.GetClientFor(addr)
				if err != nil {
					level.Error(r.logger).Log("msg", "failed to get client for instance", "instance", addr, "err", err.Error())
					return nil
				}
				resp, err := client.(proto.IngestLimitsClient).ExceedsLimits(ctx, &proto.ExceedsLimitsRequest{
					Tenant:  tenant,
					Streams: streams,
				})
				if err != nil {
					level.Error(r.logger).Log("failed check execeed limits for instance", "instance", addr, "err", err.Error())
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
		for r := range responseCh {
			resp.Results = append(resp.Results, r.Results...)
		}
		answered := make([]uint64, 0, len(streams))
		for streamHash := range answeredCh {
			answered = append(answered, streamHash)
		}
		return answered, nil
	}
}

// newUpdateRatesRPCsFunc returns a doRPCsFunc that executes the UpdateRates
// RPCs for the instances in a zone.
func (r *ringLimitsClient) newUpdateRatesRPCsFunc(resp *proto.UpdateRatesResponse) doRPCsFunc {
	return func(
		ctx context.Context,
		tenant string,
		streams []*proto.StreamMetadata,
		zone string,
		consumers map[int32]string,
	) ([]uint64, error) {
		errg, ctx := errgroup.WithContext(ctx)
		instancesForStreams := r.instancesForStreams(streams, zone, consumers)
		responseCh := make(chan *proto.UpdateRatesResponse, len(instancesForStreams))
		answeredCh := make(chan uint64, len(streams))
		for addr, streams := range instancesForStreams {
			errg.Go(func() error {
				client, err := r.pool.GetClientFor(addr)
				if err != nil {
					level.Error(r.logger).Log("msg", "failed to get client for instance", "instance", addr, "err", err.Error())
					return nil
				}
				resp, err := client.(proto.IngestLimitsClient).UpdateRates(ctx, &proto.UpdateRatesRequest{
					Tenant:  tenant,
					Streams: streams,
				})
				if err != nil {
					level.Error(r.logger).Log("failed check execeed limits for instance", "instance", addr, "err", err.Error())
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
		for r := range responseCh {
			resp.Results = append(resp.Results, r.Results...)
		}
		answered := make([]uint64, 0, len(streams))
		for streamHash := range answeredCh {
			answered = append(answered, streamHash)
		}
		return answered, nil
	}
}

type doRPCsFunc func(
	ctx context.Context,
	tenant string,
	streams []*proto.StreamMetadata,
	zone string,
	consumers map[int32]string,
) ([]uint64, error)

// exhaustAllZones queries all zones, one at a time, until either all streams
// have been answered or all zones have been exhausted.
func (r *ringLimitsClient) exhaustAllZones(ctx context.Context, tenant string, streams []*proto.StreamMetadata, doRPCs doRPCsFunc) ([]*proto.StreamMetadata, error) {
	zonesIter, err := r.allZones(ctx)
	if err != nil {
		return nil, err
	}
	// Make a copy of the stream. We will prune this slice each time we receive
	// the responses from a zone.
	unanswered := make([]*proto.StreamMetadata, 0, len(streams))
	unanswered = append(unanswered, streams...)
	// Query each zone as ordered in zonesToQuery. If a zone answers all
	// streams, the request is satisfied and there is no need to query
	// subsequent zones. If a zone answers just a subset of streams
	// (i.e. the instance that is consuming a partition is unavailable or the
	// partition that owns one or more streams does not have a consumer)
	// then query the next zone for the remaining streams. We repeat this
	// process until all streams have been queried or we have exhausted all
	// zones.
	for zone, consumers := range zonesIter {
		// All streams been answered.
		if len(unanswered) == 0 {
			break
		}
		answered, err := doRPCs(ctx, tenant, unanswered, zone, consumers)
		if err != nil {
			continue
		}
		// Remove the answered streams from the slice. The slice of answered
		// streams must be sorted so we can use sort.Search to subtract the
		// two slices.
		slices.Sort(answered)
		unanswered = slices.DeleteFunc(unanswered, func(stream *proto.StreamMetadata) bool {
			// see https://pkg.go.dev/sort#Search
			i := sort.Search(len(answered), func(i int) bool {
				return answered[i] >= stream.StreamHash
			})
			return i < len(answered) && answered[i] == stream.StreamHash
		})
	}
	return unanswered, nil
}

// allZones returns an iterator over all zones and the consumers for each
// partition in each zone. If a zone has no active partition consumers, the
// zone will still be returned but its partition consumers will be nil.
// If ZoneAwarenessEnabled is false, it returns all partition consumers under
// a pseudo-zone ("").
func (r *ringLimitsClient) allZones(ctx context.Context) (iter.Seq2[string, map[int32]string], error) {
	rs, err := r.ring.GetAllHealthy(limitsRead)
	if err != nil {
		return nil, err
	}
	// Get the partition consumers for each zone.
	zonesPartitions, err := r.getZoneAwarePartitionConsumers(ctx, rs.Instances)
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
	slices.SortFunc(zonesToQuery, r.zoneCmp)
	return func(yield func(string, map[int32]string) bool) {
		for _, zone := range zonesToQuery {
			if !yield(zone, zonesPartitions[zone]) {
				return
			}
		}
	}, nil
}

// getZoneAwarePartitionConsumers returns partition consumers for each zone
// in the replication set. If a zone has no active partition consumers, the
// zone will still be returned but its partition consumers will be nil.
// If ZoneAwarenessEnabled is false, it returns all partition consumers under
// a pseudo-zone ("").
func (r *ringLimitsClient) getZoneAwarePartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[string]map[int32]string, error) {
	zoneDescs := make(map[string][]ring.InstanceDesc)
	for _, instance := range instances {
		zoneDescs[instance.Zone] = append(zoneDescs[instance.Zone], instance)
	}
	// Get the partition consumers for each zone.
	type zonePartitionConsumersResult struct {
		zone       string
		partitions map[int32]string
	}
	resultsCh := make(chan zonePartitionConsumersResult, len(zoneDescs))
	errg, ctx := errgroup.WithContext(ctx)
	for zone, instances := range zoneDescs {
		errg.Go(func() error {
			res, err := r.getPartitionConsumers(ctx, instances)
			if err != nil {
				level.Error(r.logger).Log("msg", "failed to get partition consumers for zone", "zone", zone, "err", err.Error())
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
func (r *ringLimitsClient) getPartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[int32]string, error) {
	errg, ctx := errgroup.WithContext(ctx)
	type getAssignedPartitionsResponse struct {
		addr     string
		response *proto.GetAssignedPartitionsResponse
	}
	responseCh := make(chan getAssignedPartitionsResponse, len(instances))
	for _, instance := range instances {
		errg.Go(func() error {
			// We use a cache to eliminate redundant gRPC requests for
			// GetAssignedPartitions as the set of assigned partitions is
			// expected to be stable outside consumer rebalances.
			if resp, ok := r.assignedPartitionsCache.Get(instance.Addr); ok {
				responseCh <- getAssignedPartitionsResponse{
					addr:     instance.Addr,
					response: resp,
				}
				return nil
			}
			client, err := r.pool.GetClientFor(instance.Addr)
			if err != nil {
				level.Error(r.logger).Log("failed to get client for instance", "instance", instance.Addr, "err", err.Error())
				return nil
			}
			resp, err := client.(proto.IngestLimitsClient).GetAssignedPartitions(ctx, &proto.GetAssignedPartitionsRequest{})
			if err != nil {
				level.Error(r.logger).Log("failed to get assigned partitions for instance", "instance", instance.Addr, "err", err.Error())
				return nil
			}
			r.assignedPartitionsCache.Set(instance.Addr, resp)
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
	partitionConsumers := make(map[int32]string)
	for resp := range responseCh {
		for partition, assignedAt := range resp.response.AssignedPartitions {
			if t := highestTimestamp[partition]; t < assignedAt {
				highestTimestamp[partition] = assignedAt
				partitionConsumers[partition] = resp.addr
			}
		}
	}
	return partitionConsumers, nil
}

func (r *ringLimitsClient) instancesForStreams(streams []*proto.StreamMetadata, zone string, instances map[int32]string) map[string][]*proto.StreamMetadata {
	// For each stream, figure out which instance consume its partition.
	instancesForStreams := make(map[string][]*proto.StreamMetadata)
	for _, stream := range streams {
		partition := int32(stream.StreamHash % uint64(r.numPartitions))
		addr, ok := instances[partition]
		if !ok {
			r.partitionsMissing.WithLabelValues(zone).Inc()
			continue
		}
		instancesForStreams[addr] = append(instancesForStreams[addr], stream)
	}
	return instancesForStreams
}
