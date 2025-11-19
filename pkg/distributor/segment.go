package distributor

import (
	"context"
	"errors"
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

<<<<<<< HEAD
// Sum64 returns a 64 bit, non-cryptographic hash of the key.
=======
// Sum64 returns a 64 bit, non-crytogrpahic hash of the key.
>>>>>>> 5ef092522a (feat: add segmentation keys and resolver)
func (key SegmentationKey) Sum64() uint64 {
	h := fnv.New64a()
	// Use a reserved word here to avoid any possible hash conflicts with
	// streams.
<<<<<<< HEAD
	h.Write([]byte("__loki_segmentation_key__"))
=======
	h.Write([]byte("__loki__segmentation_key__"))
>>>>>>> 5ef092522a (feat: add segmentation keys and resolver)
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
<<<<<<< HEAD
	logger                log.Logger

	// Metrics.
	failed          prometheus.Counter
	randomlySharded prometheus.Counter
	total           prometheus.Counter
=======
	fallback              prometheus.Counter
	failed                prometheus.Counter
	total                 prometheus.Counter
	logger                log.Logger
>>>>>>> 5ef092522a (feat: add segmentation keys and resolver)
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

func (r *SegmentationPartitionResolver) Resolve(_ context.Context, key SegmentationKey, rateBytes uint64) (int32, error) {
	r.total.Inc()
	// We work with a snapshot of the partition ring to ensure resolving the
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
	// If rate bytes is 0, then we were unable to determine the rate (bytes/sec)
	// for the segmentation key. When this happens we fallback to randomly
	// selecting an active partition and incrementing the fallback metric.
	if rateBytes == 0 {
		r.randomlySharded.Inc()
		activePartitionIDs := ring.ActivePartitionIDs()
		rand.Shuffle(len(activePartitionIDs), func(i, j int) {
			activePartitionIDs[i], activePartitionIDs[j] = activePartitionIDs[j], activePartitionIDs[i]
		})
		return activePartitionIDs[0], nil
	}
	// Use the rate of the segmentation key to shuffle shard the active
	// partitions. The number of partitions in the shuffle is calculated as
	// current rate / the expected rate per partition.
	partitions := rateBytes / r.perPartitionRateBytes
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the number of active partitions.
	partitions = min(partitions, uint64(ring.ActivePartitionsCount()))
	// Shuffle shard.
	subring, err := ring.ShuffleShard(string(key), int(partitions))
	if err != nil {
		r.failed.Inc()
		return 0, err
	}
	// Get a random partition from the subring.
	activePartitionIDs := subring.ActivePartitionIDs()
	idx := rand.Intn(len(activePartitionIDs))
	return activePartitionIDs[idx], nil
}
