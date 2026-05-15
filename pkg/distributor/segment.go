package distributor

import (
	"context"
	"errors"
	"hash/fnv"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/loki/v3/pkg/distributor/rendezvous"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	ringReader            ring.PartitionRingReader
	logger                log.Logger

	// Metrics.
	resolveFailed prometheus.Counter
	resolveTotal  prometheus.Counter
}

// newSegmentationPartitionResolver returns a new segmentationPartitionResolver.
func newSegmentationPartitionResolver(perPartitionRateBytes uint64, ringReader ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *segmentationPartitionResolver {
	return &segmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
		ringReader:            ringReader,
		resolveFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_failed_total",
			Help: "Total number of segmentation keys that could not be resolved.",
		}),
		resolveTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_total",
			Help: "Total number of segmentation keys passed to the resolver.",
		}),
		logger: logger,
	}
}

func (r *segmentationPartitionResolver) Resolve(ctx context.Context, tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.resolveTotal.Inc()
	// We use a snapshot of the partition partitionRing to ensure resolving the
	// partition for a segmentation key is determinstic even if the partitionRing
	// changes.
	partitionRing := r.ringReader.PartitionRing()
	if partitionRing.ActivePartitionsCount() == 0 {
		// If there are no active partitions then we cannot write to any
		// partition as we do not know if the partition we chose will have a
		// consumer.
		r.resolveFailed.Inc()
		return 0, errors.New("no active partitions")
	}

	// Construct rendezvous shuffle sharder
	activePartitions := make([]ring.PartitionDesc, partitionRing.ActivePartitionsCount())
	for i, partition := range partitionRing.Partitions() {
		activePartitions[i] = partition
	}
	shuffleSharder := rendezvous.NewShuffleSharder(activePartitions)

	// Shuffle shard for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	numTenantShuffleShardPartitions := numPartitionsForRate(tenantRateBytes, r.perPartitionRateBytes, partitionRing.ActivePartitionsCount())
	shuffleSharder = shuffleSharder.ShuffleShard(tenant, numTenantShuffleShardPartitions)

	// Shuffle shard for the segmentation key.
	numSegKeyShuffleShardPartitions := numPartitionsForRate(rateBytes, r.perPartitionRateBytes, partitionRing.ActivePartitionsCount())
	// If the segmentation key is small enough that it does not need to be sharded,
	// we can avoid doing a shuffle shard.
	if numSegKeyShuffleShardPartitions > 1 {
		shuffleSharder = shuffleSharder.ShuffleShard(string(key), numSegKeyShuffleShardPartitions)
	}

	return shuffleSharder.Shard(hashKey).Id, nil
}

// numPartitionsForRate returns the number of partitions needed to keep within
// perPartitionRateBytes. It cannot exceed the total number of partitions.
func numPartitionsForRate(rateBytes, perPartitionRateBytes uint64, numPartitions int) int {
	partitions := rateBytes / perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the total number of partitions.
	partitions = min(partitions, uint64(numPartitions))
	// We can convert back to int here because partitions is guaranteed to be less
	// than or equal to the number of partitions, which is an int.
	return int(partitions)
}
