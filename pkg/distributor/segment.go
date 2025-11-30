package distributor

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// A SegmentationKey is a special partition key that attempts to equally
// distribute load while preserving stream locality for tenants.
type SegmentationKey string

// Sum64 returns a 64 bit, non-cryptographic hash of the key.
func (key SegmentationKey) Sum64() uint64 {
	h := fnv.New64a()
	// Use a reserved word here to avoid any possible hash conflicts with
	// streams.
	h.Write([]byte("__loki_segmentation_key__"))
	h.Write([]byte(key))
	return h.Sum64()
}

// GetSegmentationKey returns the segmentation key for the stream or an error.
func GetSegmentationKey(stream KeyedStream) (SegmentationKey, error) {
	labels, err := syntax.ParseLabels(stream.Stream.Labels)
	if err != nil {
		return "", err
	}
	if serviceName := labels.Get("service_name"); serviceName != "" {
		return SegmentationKey(serviceName), nil
	}
	return SegmentationKey("unknown_service"), nil
}

// SegmentationPartitionResolver resolves the partition for a segmentation key.
type SegmentationPartitionResolver struct {
	perPartitionRateBytes uint64
	ringReader            ring.PartitionRingReader
	logger                log.Logger

	// Metrics.
	failed          prometheus.Counter
	randomlySharded prometheus.Counter
	total           prometheus.Counter
}

// NewSegmentationPartitionResolver returns a new SegmentationPartitionResolver.
func NewSegmentationPartitionResolver(perPartitionRateBytes uint64, ringReader ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *SegmentationPartitionResolver {
	return &SegmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
		ringReader:            ringReader,
		failed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_failed_total",
			Help: "Total number of segmentation keys that could not be resolved.",
		}),
		randomlySharded: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total",
			Help: "Total number of segmentation keys that fell back to a randomly choosing an active partition due to absent rate.",
		}),
		total: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_total",
			Help: "Total number of segmentation keys passed to the resolver.",
		}),
		logger: logger,
	}
}

func (r *SegmentationPartitionResolver) Resolve(ctx context.Context, tenant string, key SegmentationKey, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.total.Inc()
	// We use a snapshot of the partition ring to ensure resolving the
	// partition for a segmentation key is determinstic even if the ring
	// changes.
	ring := r.ringReader.PartitionRing()
	if ring.ActivePartitionsCount() == 0 {
		// If there are no active partitions then we cannot write to any
		// partition as we do not know if the partition we chose will have a
		// consumer.
		r.failed.Inc()
		return 0, errors.New("no active partitions")
	}
	// Get a subring for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	subring, err := r.getTenantSubring(ctx, ring, tenant, tenantRateBytes)
	if err != nil {
		r.failed.Inc()
		return 0, fmt.Errorf("failed to get tenant subring: %w", err)
	}
	// If rate bytes is 0, then we were unable to determine the rate (bytes/sec)
	// for the segmentation key. When this happens we fallback to randomly
	// selecting an active partition and incrementing the fallback metric.
	if rateBytes == 0 {
		r.randomlySharded.Inc()
		activePartitionIDs := subring.ActivePartitionIDs()
		rand.Shuffle(len(activePartitionIDs), func(i, j int) {
			activePartitionIDs[i], activePartitionIDs[j] = activePartitionIDs[j], activePartitionIDs[i]
		})
		return activePartitionIDs[0], nil
	}
	subring, err = r.getSegmentationKeySubring(ctx, subring, key, rateBytes)
	if err != nil {
		r.failed.Inc()
		return 0, fmt.Errorf("failed to get segmentation key subring: %w", err)
	}
	// Get a random partition from the subring.
	activePartitionIDs := subring.ActivePartitionIDs()
	idx := rand.Intn(len(activePartitionIDs))
	return activePartitionIDs[idx], nil
}

// getTenantRing returns a subring for the tenant based on their rate limit.
func (r *SegmentationPartitionResolver) getTenantSubring(_ context.Context, ring *ring.PartitionRing, tenant string, tenantRateBytes uint64) (*ring.PartitionRing, error) {
	if tenantRateBytes == 0 {
		// If the tenant has no limit, return the full ring.
		return ring, nil
	}
	// The size of the subring is calculated as the tenant's ingestion rate
	// limit divided by the expected per-tenant rate per partition.
	partitions := tenantRateBytes / r.perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the number of active partitions.
	partitions = min(partitions, uint64(ring.ActivePartitionsCount()))
	return ring.ShuffleShard(tenant, int(partitions))
}

func (r *SegmentationPartitionResolver) getSegmentationKeySubring(_ context.Context, ring *ring.PartitionRing, key SegmentationKey, rateBytes uint64) (*ring.PartitionRing, error) {
	if rateBytes == 0 {
		// If the rate is 0, return the full ring.
		return ring, nil
	}
	// Use the rate of the segmentation key to shuffle shard the active
	// partitions. The number of partitions in the shuffle is calculated as
	// current rate divided by the expected per-tenant rate per partition.
	partitions := rateBytes / r.perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the number of active partitions.
	partitions = min(partitions, uint64(ring.ActivePartitionsCount()))
	// Shuffle shard.
	return ring.ShuffleShard(string(key), int(partitions))
}
