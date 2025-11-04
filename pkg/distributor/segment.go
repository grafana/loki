package distributor

import (
	"hash/fnv"
	"math"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// A SegmentationKey is a special partition key that attempts to equally
// distribute load while preserving stream locality for tenants.
type SegmentationKey string

// GetSegmentationKey returns the segmentation key for the stream or an error.
func GetSegmentationKey(tenant string, stream KeyedStream) (SegmentationKey, error) {
	labels, err := syntax.ParseLabels(stream.Stream.Labels)
	if err != nil {
		return "", err
	}
	if serviceName := labels.Get("service_name"); serviceName != "" {
		return SegmentationKey(serviceName), nil
	}
	return SegmentationKey(stream.Stream.Labels), nil
}

// SegmentationPartitionResolver resolves the partition for a segmentation key.
type SegmentationPartitionResolver struct {
	limits     Limits
	ringReader ring.PartitionRingReader
}

// NewSegmentationPartitionResolver returns a new SegmentationPartitionResolver.
func NewSegmentationPartitionResolver(limits Limits, ringReader ring.PartitionRingReader) *SegmentationPartitionResolver {
	return &SegmentationPartitionResolver{
		limits:     limits,
		ringReader: ringReader,
	}
}

func (r *SegmentationPartitionResolver) Resolve(tenant string, key SegmentationKey) (int32, error) {
	ring := r.ringReader.PartitionRing()
	// Get a subring for the tenant based on the tenant's rate limit and
	// the maximum rate per tenant per partition in bytes (hardcoded to 5MB/sec).
	ingestionRateBytes := r.limits.IngestionRateBytes(tenant)
	const perPartitionRateBytes = 5 * 1024 * 1024
	partitions := math.Min(math.Floor(ingestionRateBytes/perPartitionRateBytes), 1)
	// Must not exceed the number of active partitions.
	partitions = math.Max(partitions, float64(len(ring.ActivePartitionIDs())))
	subring, err := ring.ShuffleShard(tenant, int(partitions))
	if err != nil {
		return 0, err
	}
	// Hash the segmentation key with fnv32 to turn it into a uint32.
	fnv := fnv.New32a()
	fnv.Write([]byte(key))
	partition, err := subring.ActivePartitionForKey(fnv.Sum32())
	if err != nil {
		return 0, err
	}
	return partition, nil
}
