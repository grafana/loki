package distributor

import (
	"context"
	"hash/fnv"
	"math/rand"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	return SegmentationKey("unknown_service"), nil
}

// SegmentationPartitionResolver resolves the partition for a segmentation key.
type SegmentationPartitionResolver struct {
	cfg          *DataObjTeeConfig
	limits       Limits
	ringReader   ring.PartitionRingReader
	limitsClient *ingestLimits
	fallback     *prometheus.CounterVec
	logger       log.Logger
}

// NewSegmentationPartitionResolver returns a new SegmentationPartitionResolver.
func NewSegmentationPartitionResolver(cfg *DataObjTeeConfig, limits Limits, ringReader ring.PartitionRingReader, limitsClient *ingestLimits, reg prometheus.Registerer, logger log.Logger) *SegmentationPartitionResolver {
	return &SegmentationPartitionResolver{
		cfg:          cfg,
		limits:       limits,
		ringReader:   ringReader,
		limitsClient: limitsClient,
		fallback: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_fallback_total",
			Help: "Total number of streams that could not be duplicated.",
		}, []string{"tenant", "segmentation_key", "reason"}),
		logger: logger,
	}
}

func (r *SegmentationPartitionResolver) Resolve(ctx context.Context, tenant string, key SegmentationKey, stream KeyedStream) (int32, error) {
	ring := r.ringReader.PartitionRing()

	// Make an fnv32 hash of the segmentation key.
	hash := fnv.New32()
	hash.Write([]byte("__loki_service_name__"))
	hash.Write([]byte(tenant))
	hash.Write([]byte(key))
	segmentationKeyHash := hash.Sum32()
	segmentationKeyedStream := KeyedStream{
		HashKey:        segmentationKeyHash,
		HashKeyNoShard: uint64(segmentationKeyHash),
		Stream:         stream.Stream,
	}

	// Update the rates for the segmentation key.
	results, err := r.limitsClient.UpdateRates(ctx, tenant, []KeyedStream{segmentationKeyedStream})
	if err != nil || len(results) == 0 {
		if err != nil {
			r.fallback.WithLabelValues(tenant, string(key), "err").Inc()
			level.Debug(r.logger).Log("msg", "failed to update rates", "err", err)
		} else {
			r.fallback.WithLabelValues(tenant, string(key), "no_results").Inc()
		}
		// Fall back to naiive shard on the hash key.
		partition, err := ring.ActivePartitionForKey(stream.HashKey)
		if err != nil {
			return 0, err
		}
		return partition, nil
	}

	// Use the rate of the segmentation key to shuffle shard.
	res := results[0]
	// The partitions is the current rate / the expected rate per partition.
	partitions := res.Rate / uint64(r.cfg.PerPartitionRateBytes)
	// Must be at least 1 partition.
	partitions = max(partitions, 1)
	// Must not exceed the number of active partitions.
	partitions = min(partitions, uint64(len(ring.ActivePartitionIDs())))
	// Shuffle shard.
	subring, err := ring.ShuffleShard(string(key), int(partitions))
	if err != nil {
		return 0, err
	}
	// Get the partition from this shard.
	if r.cfg.RandomWithinShard {
		activePartitionIDs := subring.ActivePartitionIDs()
		idx := rand.Intn(len(activePartitionIDs))
		return activePartitionIDs[idx], nil
	}
	partition, err := subring.ActivePartitionForKey(uint32(stream.HashKeyNoShard))
	return partition, err
}
