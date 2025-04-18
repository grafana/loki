package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/jellydator/ttlcache/v3"
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
	cache         PartitionConsumersCache
}

// NewRingStreamUsageGatherer returns a new RingStreamUsageGatherer.
func NewRingStreamUsageGatherer(ring ring.ReadRing, pool *ring_client.Pool, logger log.Logger, cache PartitionConsumersCache, numPartitions int) *RingStreamUsageGatherer {
	return &RingStreamUsageGatherer{
		logger:        logger,
		ring:          ring,
		pool:          pool,
		numPartitions: numPartitions,
		cache:         cache,
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

	instancesToQuery := make(map[string][]uint64)
	for _, hash := range r.StreamHashes {
		partitionID := int32(hash % uint64(g.numPartitions))
		addr, ok := partitionConsumers[partitionID]
		if !ok {
			// TODO Replace with a metric for partitions missing owners.
			level.Warn(g.logger).Log("msg", "no instance found for partition", "partition", partitionID)
			continue
		}
		instancesToQuery[addr] = append(instancesToQuery[addr], hash)
	}

	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]GetStreamUsageResponse, len(instancesToQuery))

	// Query each instance for stream usage
	i := 0
	for addr, hashes := range instancesToQuery {
		j := i
		i++
		errg.Go(func() error {
			client, err := g.pool.GetClientFor(addr)
			if err != nil {
				return err
			}

			protoReq := &logproto.GetStreamUsageRequest{
				Tenant:       r.Tenant,
				StreamHashes: hashes,
			}

			resp, err := client.(logproto.IngestLimitsClient).GetStreamUsage(ctx, protoReq)
			if err != nil {
				return err
			}

			responses[j] = GetStreamUsageResponse{Addr: addr, Response: resp}
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

func (g *RingStreamUsageGatherer) getPartitionConsumers(ctx context.Context, instances []ring.InstanceDesc) (map[int32]string, error) {
	// Initialize result maps
	highestTimestamp := make(map[int32]int64)
	assigned := make(map[int32]string)

	errg, ctx := errgroup.WithContext(ctx)
	responses := make([]getAssignedPartitionsResponse, len(instances))

	for i, instance := range instances {
		errg.Go(func() error {
			errChan := make(chan error, 1)
			defer close(errChan)

			loaderFunc := ttlcache.LoaderFunc[string, logproto.GetAssignedPartitionsResponse](
				func(c *ttlcache.Cache[string, logproto.GetAssignedPartitionsResponse], key string) *ttlcache.Item[string, logproto.GetAssignedPartitionsResponse] {
					client, err := g.pool.GetClientFor(instance.Addr)
					if err != nil {
						errChan <- err
						return nil
					}

					resp, err := client.(logproto.IngestLimitsClient).GetAssignedPartitions(ctx, &logproto.GetAssignedPartitionsRequest{})
					if err != nil {
						errChan <- err
						return nil
					}
					return c.Set(key, *resp, ttlcache.DefaultTTL)
				},
			)

			cached := g.cache.Get(instance.Addr, ttlcache.WithLoader(loaderFunc))

			select {
			case err := <-errChan:
				return err
			default:
			}

			protoResp := cached.Value()
			responses[i] = getAssignedPartitionsResponse{Addr: instance.Addr, Response: &protoResp}
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return nil, err
	}

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
