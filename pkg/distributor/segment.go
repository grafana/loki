package distributor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// A segmentationKey is a special partition key that attempts to equally
// distribute load while preserving stream locality for tenants.
type segmentationKey string

// Sum64 returns a 64 bit, non-cryptographic hash of the key.
func (key segmentationKey) Sum64() uint64 {
	h := xxhash.New()
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
	sb := strings.Builder{}
	if cluster := labels.Get("cluster"); cluster != "" {
		sb.WriteString(cluster)
	} else {
		sb.WriteString("unknown_cluster")
	}
	sb.WriteString("/")
	if namespace := labels.Get("namespace"); namespace != "" {
		sb.WriteString(namespace)
	} else {
		sb.WriteString("unknown_namespace")
	}
	sb.WriteString("/")
	if serviceName := labels.Get("service_name"); serviceName != "" {
		sb.WriteString(serviceName)
	} else {
		sb.WriteString("unknown_service")
	}
	return segmentationKey(sb.String()), nil
}

// segmentationPartitionResolver resolves the partition for a segmentation key.
type segmentationPartitionResolver struct {
	perPartitionRateBytes uint64
	ringReader            ring.PartitionRingReader
	logger                log.Logger

	// Metrics.
	failed prometheus.Counter
	total  prometheus.Counter
}

// newSegmentationPartitionResolver returns a new segmentationPartitionResolver.
func newSegmentationPartitionResolver(perPartitionRateBytes uint64, ringReader ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *segmentationPartitionResolver {
	return &segmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
		ringReader:            ringReader,
		failed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_failed_total",
			Help: "Total number of segmentation keys that could not be resolved.",
		}),
		total: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_segmentation_partition_resolver_keys_total",
			Help: "Total number of segmentation keys passed to the resolver.",
		}),
		logger: logger,
	}
}

func (r *segmentationPartitionResolver) Resolve(ctx context.Context, tenant string, key segmentationKey, hashKey uint32, rateBytes, tenantRateBytes uint64) (int32, error) {
	r.total.Inc()
	// We use a snapshot of the partition ring to ensure resolving the
	// partition for a segmentation key is determinstic even if the ring
	// changes.
	snapshot := r.ringReader.PartitionRing()
	if snapshot.ActivePartitionsCount() == 0 {
		// If there are no active partitions then we cannot write to any
		// partition as we do not know if the partition we chose will have a
		// consumer.
		r.failed.Inc()
		return 0, errors.New("no active partitions")
	}
	// Get a subring for the tenant based on their rate limit. This ensures
	// that segmentation keys for a tenant are as co-located as possible.
	subring, err := r.getTenantSubring(ctx, snapshot, tenant, tenantRateBytes)
	if err != nil {
		r.failed.Inc()
		return 0, fmt.Errorf("failed to get tenant subring: %w", err)
	}
	// If rate bytes is 0, then we were unable to determine the rate (bytes/sec)
	// for the segmentation key. When this happens we can write the entire
	// segmentation key to an active partition.
	if rateBytes == 0 {
		return subring.ActivePartitionForKey(uint32(key.Sum64()))
	}
	// Calculate the number of partitions needed for the current rate.
	partitions := numPartitionsForRate(rateBytes, r.perPartitionRateBytes, subring.ActivePartitionsCount())
	if partitions == 1 {
		// This is an optimization that reduces the number of expensive shuffle shards
		// that need to be done.
		return subring.ActivePartitionForKey(uint32(key.Sum64()))
	}
	subring, err = subring.ShuffleShard(string(key), partitions)
	if err != nil {
		r.failed.Inc()
		return 0, fmt.Errorf("failed to get segmentation key subring: %w", err)
	}
	// TODO(grobinson): We need to use a different method that does not depend on
	// stream sharding, as this information comes from the ingesters.
	return subring.ActivePartitionForKey(hashKey)
}

// getTenantRing returns a subring for the tenant based on their rate limit.
func (r *segmentationPartitionResolver) getTenantSubring(_ context.Context, ring *ring.PartitionRing, tenant string, tenantRateBytes uint64) (*ring.PartitionRing, error) {
	if tenantRateBytes == 0 {
		return ring, nil
	}
	numActivePartitions := ring.ActivePartitionsCount()
	numShardPartitions := numPartitionsForRate(tenantRateBytes, r.perPartitionRateBytes, numActivePartitions)
	return ring.ShuffleShard(tenant, numShardPartitions)
}

// numPartitionsForRate returns the number of partitions needed to keep within
// perPartitionRateBytes. It cannot exceed the total number of partitions.
func numPartitionsForRate(rateBytes, perPartitionRateBytes uint64, numPartitions int) int {
	partitions := rateBytes / perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the total number of partitions.
	partitions = min(partitions, uint64(numPartitions))
	return int(partitions)
}
