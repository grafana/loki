package distributor

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
	logger                log.Logger

	// We divide the partition ring into two smaller rings called oldEntries
	// and newEntries. oldEntries contains the partitions that receive entries
	// more than 1 hour old, while newEntries contains the partitions that
	// receive recent entries less than 1 hour old. The mtx must be used to
	// access each pointer as it can be swapped concurrently via the delegate.
	oldEntries *ring.PartitionRing
	newEntries *ring.PartitionRing
	mtx        sync.RWMutex

	// Metrics.
	failed          prometheus.Counter
	randomlySharded prometheus.Counter
	total           prometheus.Counter
}

// NewSegmentationPartitionResolver returns a new SegmentationPartitionResolver.
func NewSegmentationPartitionResolver(
	perPartitionRateBytes uint64,
	reg prometheus.Registerer,
	logger log.Logger,
) *SegmentationPartitionResolver {
	return &SegmentationPartitionResolver{
		perPartitionRateBytes: perPartitionRateBytes,
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

func (r *SegmentationPartitionResolver) Resolve(
	ctx context.Context,
	tenant string,
	key SegmentationKey,
	rateBytes,
	tenantRateBytes uint64,
	since time.Duration,
) (int32, error) {
	r.total.Inc()
	if since > 4*time.Hour {
		return r.resolveOldEntries(ctx, since)
	}
	return r.resolve(ctx, tenant, key, rateBytes, tenantRateBytes)
}

func (r *SegmentationPartitionResolver) resolveOldEntries(
	ctx context.Context,
	since time.Duration,
) (int32, error) {
	r.mtx.RLock()
	oldEntries := r.oldEntries
	r.mtx.RUnlock()
	// Write old logs to dedicated "old" partitions.
	if oldEntries == nil || oldEntries.ActivePartitionsCount() == 0 {
		// If there are no active partitions we must return an error,
		// otherwise we would write to a partition without knowing if the
		// partition has a consumer.
		r.failed.Inc()
		return 0, errors.New("no active partitions")
	}
	n := oldEntries.ActivePartitionsCount()
	if since > 24*time.Hour {
		return int32(min(1, n)), nil
	}
	return 0, nil
}

func (r *SegmentationPartitionResolver) resolve(
	ctx context.Context,
	tenant string,
	key SegmentationKey,
	rateBytes,
	tenantRateBytes uint64,
) (int32, error) {
	r.mtx.RLock()
	newEntries := r.newEntries
	r.mtx.RUnlock()
	if newEntries == nil || newEntries.ActivePartitionsCount() == 0 {
		// If there are no active partitions we must return an error,
		// otherwise we would write to a partition without knowing if the
		// partition has a consumer.
		r.failed.Inc()
		return 0, errors.New("no active partitions")
	}
	// Get a subring for the tenant based on their ingestion rate limit.
	// This ensures that streams are not only co-located within the same
	// segmentation key, but also segmentation keys for a tenant are as
	// co-located as possible.
	subring, err := r.getTenantSubring(ctx, newEntries, tenant, tenantRateBytes)
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
func (r *SegmentationPartitionResolver) getTenantSubring(
	_ context.Context,
	ring *ring.PartitionRing,
	tenant string,
	tenantRateBytes uint64,
) (*ring.PartitionRing, error) {
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

func (r *SegmentationPartitionResolver) getSegmentationKeySubring(
	_ context.Context,
	ring *ring.PartitionRing,
	key SegmentationKey,
	rateBytes uint64,
) (*ring.PartitionRing, error) {
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

// OnPartitionRingChanged implements [ring.PartitionRingWatcherDelegate].
func (r *SegmentationPartitionResolver) OnPartitionRingChanged(_, newDesc *ring.PartitionRingDesc) {
	// Select just the active partitions from the desc.
	active := make([]int32, 0, len(newDesc.Partitions))
	for partition, desc := range newDesc.Partitions {
		if desc.IsActive() {
			active = append(active, partition)
		}
	}
	slices.Sort(active)
	// Sort the active partitions into old and new entries partitions.
	partitionsOldEntries := make(map[int32]struct{}, len(newDesc.Partitions))
	partitionsNewEntries := make(map[int32]struct{}, len(newDesc.Partitions))
	if len(active) < 3 {
		for _, partition := range active {
			partitionsOldEntries[partition] = struct{}{}
			partitionsNewEntries[partition] = struct{}{}
		}
	} else {
		var n int
		for _, partition := range active {
			if n < 2 {
				partitionsOldEntries[partition] = struct{}{}
			} else {
				partitionsNewEntries[partition] = struct{}{}
			}
			n++
		}
	}
	oldEntries, err := ring.NewPartitionRing(newDesc.WithPartitions(partitionsOldEntries))
	if err != nil {
		level.Error(r.logger).Log("msg", "failed to update old entries ring", "err", err)
		return
	}
	newEntries, err := ring.NewPartitionRing(newDesc.WithPartitions(partitionsNewEntries))
	if err != nil {
		level.Error(r.logger).Log("msg", "failed to update new entries ring", "err", err)
		return
	}
	level.Debug(r.logger).Log("msg", "partition ring change", "old_entries", len(partitionsOldEntries), "new_entries", len(partitionsNewEntries))
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.oldEntries = oldEntries
	r.newEntries = newEntries
}
