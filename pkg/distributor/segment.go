package distributor

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/distributor/rendezvous"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// A segmentationKey is a special partition key that attempts to equally
// distribute load while preserving stream locality for tenants.
type segmentationKey string

// Sum64 returns a 64 bit, non-cryptographic hash of the key.
func (key segmentationKey) Sum64() uint64 {
	h := fnv.New64a()
	// Use a reserved word here to avoid any possible hash conflicts with
	// streams.
	h.Write([]byte("__loki_segmentation_key__"))
	h.Write([]byte(key))
	return h.Sum64()
}

// getSegmentationKey returns the segmentation key for the stream or an error.
func getSegmentationKey(stream KeyedStream) (segmentationKey, error) {
	labels, err := syntax.ParseLabels(stream.Stream.Labels)
	if err != nil {
		return "", err
	}
	if serviceName := labels.Get("service_name"); serviceName != "" {
		return segmentationKey(serviceName), nil
	}
	return "unknown_service", nil
}

// segmentationPartitionResolver resolves the partition for a segmentation key.
type segmentationPartitionResolver struct {
	perPartitionRateBytes uint64
	useRendezvousHashing  bool
	ringReader            ring.PartitionRingReader
	partitionRingWatcher  *rendezvous.PartitionRingWatcher
	logger                log.Logger

	// Metrics.
	resolveFailed                   prometheus.Counter
	resolveTotal                    prometheus.Counter
	tenantShuffleShardSize          prometheus.Histogram
	segmentationKeyShuffleShardSize prometheus.Histogram
}

// newSegmentationPartitionResolver returns a new segmentationPartitionResolver.
func newSegmentationPartitionResolver(
	perPartitionRateBytes uint64,
	useRendezvousHashing bool,
	ringReader ring.PartitionRingReader,
	partitionWatcher *rendezvous.PartitionRingWatcher,
	reg prometheus.Registerer,
	logger log.Logger) *segmentationPartitionResolver {
	return &segmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
		useRendezvousHashing:  useRendezvousHashing,
		ringReader:            ringReader,
		partitionRingWatcher:  partitionWatcher,
		resolveFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_failed_total",
			Help: "Total number of segmentation keys that could not be resolved.",
		}),
		resolveTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_total",
			Help: "Total number of segmentation keys passed to the resolver.",
		}),
		tenantShuffleShardSize: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_distributor_segmentation_partition_resolver_tenant_shuffle_shard_size",
			Help:                            "The size of the shuffle shard created by sharding on tenant ID, observed per request",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		segmentationKeyShuffleShardSize: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_distributor_segmentation_partition_resolver_segmentation_key_shuffle_shard_size",
			Help:                            "The size of the shuffle shard created by sharding on segmentation key, observed per request",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		logger: logger,
	}
}

func (r *segmentationPartitionResolver) Resolve(tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	if r.useRendezvousHashing {
		return r.resolveRendezvousHashing(tenant, key, hashKey, rateBytes, tenantRateBytes)
	}

	// TODO(danhopper): remove consistent hashing once we have confidence in rendezvous hashing
	return r.resolveConsistentHashing(tenant, key, hashKey, rateBytes, tenantRateBytes)
}

func (r *segmentationPartitionResolver) resolveRendezvousHashing(tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.resolveTotal.Inc()

	shuffleSharder := r.partitionRingWatcher.ShuffleSharder()
	if shuffleSharder == nil {
		r.resolveFailed.Inc()
		return 0, errors.New("partition ring watcher not initialised")
	}

	// Shuffle shard for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	numTenantShuffleShardPartitions := numPartitionsForRateRendezvousHashing(tenantRateBytes, r.perPartitionRateBytes, shuffleSharder.Size())
	shuffleSharder = shuffleSharder.ShuffleShard(tenant, numTenantShuffleShardPartitions)

	// Shuffle shard for the segmentation key.
	numSegKeyShuffleShardPartitions := numPartitionsForRateRendezvousHashing(rateBytes, r.perPartitionRateBytes, shuffleSharder.Size())
	// If the segmentation key is small enough that it does not need to be sharded,
	// we can avoid doing a shuffle shard.
	if numSegKeyShuffleShardPartitions > 1 {
		shuffleSharder = shuffleSharder.ShuffleShard(string(key), numSegKeyShuffleShardPartitions)
	}

	r.tenantShuffleShardSize.Observe(float64(numTenantShuffleShardPartitions))
	r.segmentationKeyShuffleShardSize.Observe(float64(numSegKeyShuffleShardPartitions))

	// Finally, shard based on the hash key.
	partition, err := shuffleSharder.Shard(hashKey)
	if err != nil {
		r.resolveFailed.Inc()
	}
	return partition, err
}

// numPartitionsForRateRendezvousHashing returns the number of partitions needed to keep within
// perPartitionRateBytes. It cannot exceed the total number of partitions.
func numPartitionsForRateRendezvousHashing(rateBytes, perPartitionRateBytes uint64, numPartitions int) int {
	partitions := (rateBytes + perPartitionRateBytes - 1) / perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the total number of partitions.
	partitions = min(partitions, uint64(numPartitions))
	// We can convert back to int here because partitions is guaranteed to be less
	// than or equal to the number of partitions, which is an int.
	return int(partitions)
}

func (r *segmentationPartitionResolver) resolveConsistentHashing(tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.resolveTotal.Inc()
	// We use a snapshot of the partition ring to ensure resolving the
	// partition for a segmentation key is determinstic even if the ring
	// changes.
	ring := r.ringReader.PartitionRing()
	if ring.ActivePartitionsCount() == 0 {
		// If there are no active partitions then we cannot write to any
		// partition as we do not know if the partition we chose will have a
		// consumer.
		r.resolveFailed.Inc()
		return 0, errors.New("no active partitions")
	}
	// Get a subring for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	subring, err := r.tenantShuffleShardConsistentHashing(ring, tenant, tenantRateBytes)
	if err != nil {
		r.resolveFailed.Inc()
		return 0, fmt.Errorf("failed to shuffle shard tenant: %w", err)
	}
	// If the rate is 0, we cannot make a decision to shuffle shard the segmentation
	// key. We fallback to choosing a partition for the hash key.
	if rateBytes == 0 {
		return subring.ActivePartitionForKey(hashKey)
	}
	numShuffleShardPartitions := numPartitionsForRateConsistentHashing(rateBytes, r.perPartitionRateBytes, subring.ActivePartitionsCount())
	// If the segmentation key is small enough that it does not need to be sharded,
	// we can avoid doing an expensive shuffle shard.
	if numShuffleShardPartitions == 1 {
		return subring.ActivePartitionForKey(uint32(key.Sum64()))
	}
	subring, err = subring.ShuffleShard(string(key), numShuffleShardPartitions)
	if err != nil {
		r.resolveFailed.Inc()
		return 0, fmt.Errorf("failed to get segmentation key subring: %w", err)
	}
	// TODO(grobinson): We need to use a different method that does not depend on
	// stream sharding, as this information comes from the ingesters.
	return subring.ActivePartitionForKey(hashKey)
}

// tenantShuffleShardConsistentHashing returns a subring for the tenant based on their rate limit.
func (r *segmentationPartitionResolver) tenantShuffleShardConsistentHashing(ring *ring.PartitionRing, tenant string, tenantRateBytes uint64) (*ring.PartitionRing, error) {
	// If the tenant has no limit, return the full ring.
	if tenantRateBytes == 0 {
		return ring, nil
	}
	numShuffleShardPartitions := numPartitionsForRateConsistentHashing(tenantRateBytes, r.perPartitionRateBytes, ring.ActivePartitionsCount())
	return ring.ShuffleShard(tenant, numShuffleShardPartitions)
}

// numPartitionsForRateRendezvousHashing returns the number of partitions needed to keep within
// perPartitionRateBytes. It cannot exceed the total number of partitions.
func numPartitionsForRateConsistentHashing(rateBytes, perPartitionRateBytes uint64, numPartitions int) int {
	partitions := rateBytes / perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the total number of partitions.
	partitions = min(partitions, uint64(numPartitions))
	// We can convert back to int here because partitions is guaranteed to be less
	// than or equal to the number of partitions, which is an int.
	return int(partitions)
}
