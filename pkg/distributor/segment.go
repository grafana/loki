package distributor

import (
	"hash/fnv"

	"github.com/go-kit/log"
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
	return segmentationKey("unknown_service"), nil
}

// segmentationPartitionResolver resolves the partition for a segmentation key.
type segmentationPartitionResolver struct {
	perPartitionRateBytes uint64
	partitionWatcher      *rendezvous.PartitionWatcher
	logger                log.Logger

	// Metrics.
	resolveFailed                   prometheus.Counter
	resolveTotal                    prometheus.Counter
	numPartitions                   prometheus.Histogram
	tenantShuffleShardSize          prometheus.Histogram
	segmentationKeyShuffleShardSize prometheus.Histogram
}

// newSegmentationPartitionResolver returns a new segmentationPartitionResolver.
func newSegmentationPartitionResolver(
	perPartitionRateBytes uint64,
	partitionWatcher *rendezvous.PartitionWatcher,
	reg prometheus.Registerer,
	logger log.Logger) *segmentationPartitionResolver {
	return &segmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
		partitionWatcher:      partitionWatcher,
		resolveFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_failed_total",
			Help: "Total number of segmentation keys that could not be resolved.",
		}),
		resolveTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_total",
			Help: "Total number of segmentation keys passed to the resolver.",
		}),
		numPartitions: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_distributor_segmentation_partition_resolver_partition_count",
			Help:                            "The number of partitions that a key could resolve to",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tenantShuffleShardSize: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_distributor_segmentation_partition_resolver_tenant_shuffle_shard_size",
			Help:                            "The size of the shuffle shard created by sharding on tenant ID",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		segmentationKeyShuffleShardSize: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_distributor_segmentation_partition_resolver_segmentation_key_shuffle_shard_size",
			Help:                            "The size of the shuffle shard created by sharding on segmentation key",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		logger: logger,
	}
}

func (r *segmentationPartitionResolver) Resolve(tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.resolveTotal.Inc()
	shuffleSharder := *r.partitionWatcher.Sharder()

	// Shuffle shard for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	numTenantShuffleShardPartitions := numPartitionsForRate(tenantRateBytes, r.perPartitionRateBytes, shuffleSharder.Size())
	shuffleSharder = shuffleSharder.ShuffleShard(tenant, numTenantShuffleShardPartitions)

	// Shuffle shard for the segmentation key.
	numSegKeyShuffleShardPartitions := numPartitionsForRate(rateBytes, r.perPartitionRateBytes, shuffleSharder.Size())
	// If the segmentation key is small enough that it does not need to be sharded,
	// we can avoid doing a shuffle shard.
	if numSegKeyShuffleShardPartitions > 1 {
		shuffleSharder = shuffleSharder.ShuffleShard(string(key), numSegKeyShuffleShardPartitions)
	}

	r.numPartitions.Observe(float64(shuffleSharder.Size()))
	r.tenantShuffleShardSize.Observe(float64(numTenantShuffleShardPartitions))
	r.segmentationKeyShuffleShardSize.Observe(float64(numSegKeyShuffleShardPartitions))

	// Finally, shard based on the hash key.
	partition, err := shuffleSharder.Shard(hashKey)
	if err != nil {
		r.resolveFailed.Inc()
	}
	return partition, err
}

// numPartitionsForRate returns the number of partitions needed to keep within
// perPartitionRateBytes. It cannot exceed the total number of partitions.
func numPartitionsForRate(rateBytes, perPartitionRateBytes uint64, numPartitions int) int {
	partitions := (rateBytes + perPartitionRateBytes - 1) / perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the total number of partitions.
	partitions = min(partitions, uint64(numPartitions))
	// We can convert back to int here because partitions is guaranteed to be less
	// than or equal to the number of partitions, which is an int.
	return int(partitions)
}
