package limits

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

var (
	streamShardTrackedStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_tracked_streams",
		"The current number of distinct logical (pre-shard) streams tracked by the stream-shard-tracking pipeline per tenant.",
		[]string{"tenant"},
		nil,
	)
	streamShardTotalStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_total_streams",
		"The current predicted total number of physical streams (unsharded plus sharded pieces) per tenant, i.e. the sum of granted shard counts. This is a prediction, not a live count: a stream that shrinks here doesn't retroactively shrink in ingesters, which are still driven by the legacy rate-store path during shadow mode, so there is an inherent lag. Compare against loki_ingester_memory_streams for the real, physical per-tenant stream count.",
		[]string{"tenant"},
		nil,
	)
	streamShardTotalShardsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_total_shards",
		"The current predicted number of physical streams that are shards (pieces of streams sharded into two or more) per tenant. Subtracting this from loki_ingest_limits_stream_shard_total_streams gives the number of unsharded streams. Same shadow-mode prediction caveat as that metric.",
		[]string{"tenant"},
		nil,
	)
)

// streamShardStore is a standalone store dedicated to stream-sharding
// decisions, kept separate from usageStore (its own map, its own eviction)
// so it can't regress the existing stream-count enforcement in
// usageStore/UpdateCond.
//
// It is purely in-memory and per-instance: a restart or partition rebalance
// loses rate history for affected streams, which then warm up again from
// scratch (see checkAndShard's "brand-new or expired" case).
//
// It caps shard growth against max_global_streams_per_user using only the
// streams it has itself evaluated (see room). A tenant whose streams predate
// shadow mode simply warms up until it has seen them, so the cap may be
// looser than reality during that window -- an accepted approximation while
// the feature is observation-only.
type streamShardStore struct {
	activeWindow  time.Duration
	rateWindow    time.Duration
	bucketSize    time.Duration
	numBuckets    int
	numPartitions int

	stripes []map[string]streamShardTenantUsage
	locks   []stripeLock

	limits Limits
}

// streamShardTenantUsage is the per-tenant state: partition -> policy ->
// streamHash -> usage. This mirrors usageStore's tenantUsage shape.
type streamShardTenantUsage map[int32]map[string]map[uint64]streamShardUsage

// streamShardUsage is the state streamShardStore tracks for a single logical (pre-shard)
// stream.
type streamShardUsage struct {
	hash        uint64
	lastSeenAt  int64
	shardCount  uint32
	rateBuckets []rateBucket
	policy      string
}

func newStreamShardStore(
	activeWindow, rateWindow, bucketSize time.Duration,
	numPartitions int,
	limits Limits,
	reg prometheus.Registerer,
) (*streamShardStore, error) {
	s := &streamShardStore{
		activeWindow:  activeWindow,
		rateWindow:    rateWindow,
		bucketSize:    bucketSize,
		numBuckets:    int(rateWindow / bucketSize),
		numPartitions: numPartitions,
		stripes:       make([]map[string]streamShardTenantUsage, numStripes),
		locks:         make([]stripeLock, numStripes),
		limits:        limits,
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]streamShardTenantUsage)
	}
	if err := reg.Register(s); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	return s, nil
}

// checkAndShard evaluates each stream in metadata and decides how many
// shards it should use:
//
//   - A brand-new or expired stream always starts at 1 shard (no rate
//     history exists yet to justify more), and is rejected outright (0
//     shards) if there is no room left even for 1.
//   - An existing stream whose policy has sharding disabled is held at 1
//     shard regardless of rate.
//   - An existing stream with sharding enabled has its rate recomputed from
//     the observation in this call, a "desired" shard count derived from
//     rate/desiredRate, and the result capped to fit the tenant's remaining
//     budget: granted = max(1, min(desired, room)). This never forces an
//     active stream down to 0 shards (only a rejection of a brand-new
//     stream can do that), and never shrinks a stream because *other*
//     streams grew -- only because its own rate dropped.
//   - If no rate history has been recorded for this stream yet (its rate
//     buckets are still empty, e.g. this is the first live push since it
//     was granted its initial shard count), the shard count is held steady
//     rather than being recomputed from a rate of zero.
func (s *streamShardStore) checkAndShard(ctx context.Context, tenant string, metadata []*proto.StreamMetadata, seenAt time.Time) []*proto.StreamShardResult {
	var (
		cutoff  = seenAt.Add(-s.activeWindow).UnixNano()
		results = make([]*proto.StreamShardResult, 0, len(metadata))
	)
	s.withLock(tenant, func(i int) {
		for _, m := range metadata {
			if ctx.Err() != nil {
				return
			}

			partition := s.getPartitionForHash(m.StreamHash)
			shardCfg, _ := s.limits.PolicyShardStreams(tenant, m.IngestionPolicy)
			policyBucket, maxStreams := getPolicyBucketAndStreamsLimit(s.limits, s.numPartitions, tenant, m.IngestionPolicy)
			streams := s.checkInitMap(i, tenant, partition, policyBucket)

			existing, wasPresent := streams[m.StreamHash]
			isNewOrExpired := !wasPresent || existing.lastSeenAt < cutoff

			// selfKnown is wasPresent, not "wasPresent and still valid": even
			// an expired entry is still sitting in the map (it isn't
			// deleted/reset until later in this same call), so bucketSlots
			// below would otherwise double-count its own stale allocation as
			// belonging to some other stream, wrongly shrinking room for a
			// reactivating stream evaluating itself.
			room := s.room(streams, maxStreams, existing, wasPresent)

			var (
				stream  streamShardUsage
				desired uint32
			)
			switch {
			case !shardCfg.Enabled:
				stream = existing
				desired = 1
			case isNewOrExpired:
				if maxStreams > 0 && room < 1 {
					// No room even for a single slot: reject outright.
					delete(streams, m.StreamHash)
					results = append(results, &proto.StreamShardResult{
						StreamHash:   m.StreamHash,
						Shards:       0,
						RejectReason: ReasonMaxStreams.String(),
					})
					continue
				}

				// Reset (mirrors usageStore.update's reset-on-expiry
				// semantics). No rate history: never shard on first sight.
				stream = streamShardUsage{hash: m.StreamHash, policy: policyBucket}

				// Seed the first rate bucket with this push's bytes even
				// though the decision itself stays at 1 shard: otherwise
				// this push's bytes are dropped forever (never fed into any
				// rate computation), and only the stream's SECOND push would
				// initialize the buckets, biasing the rate low for bursty or
				// infrequently-pushed streams.
				s.updateRateBucket(&stream, m.TotalSize, seenAt)
				desired = 1
			default:
				stream = existing
				// Must be checked BEFORE updateRateBucket: stream is a
				// shallow copy of existing (same underlying rateBuckets
				// array), so updateRateBucket's in-place mutation would
				// otherwise make every stream look "warm" by the time it's
				// checked, even one that has never seen live traffic before
				// this exact call.
				wasCold := rateBucketsCold(existing.rateBuckets)
				s.updateRateBucket(&stream, m.TotalSize, seenAt)
				if wasCold {
					// This instance hasn't observed live traffic for this
					// stream before now (e.g. it just took over the
					// partition). Hold steady rather than recompute from a
					// rate based on a single, just-started bucket.
					desired = max(1, stream.shardCount)
				} else {
					rate := currentRate(stream.rateBuckets, seenAt, s.rateWindow)
					desired = max(1, ceilDivU32(rate, uint64(shardCfg.DesiredRate.Val())))
				}
			}

			// Cap the rate-justified count to the tenant's remaining budget,
			// but never below what the stream already holds: an active stream
			// only shrinks when its own rate drops, never because other
			// streams grew.
			granted := desired
			if maxStreams != 0 && desired > existing.shardCount {
				granted = max(existing.shardCount, min(desired, uint32(room)))
			}

			shardDecisionContext := ReasonUnknown
			if granted < desired {
				shardDecisionContext = ReasonStreamShardsCapped
			}

			stream.hash = m.StreamHash
			stream.policy = policyBucket
			stream.shardCount = granted
			stream.lastSeenAt = max(seenAt.UnixNano(), stream.lastSeenAt)
			streams[m.StreamHash] = stream

			results = append(results, &proto.StreamShardResult{
				StreamHash:           m.StreamHash,
				Shards:               granted,
				ShardDecisionContext: uint32(shardDecisionContext),
			})
		}
	})
	return results
}

// room returns how many additional slots are available in the bucket for
// maxStreams, excluding whatever the stream identified by existing/selfKnown
// already holds. maxStreams == 0 (unlimited) is handled by callers, not
// here.
//
// It counts only the streams streamShardStore has itself evaluated, weighted
// by their granted shard counts (bucketSlots). Streams it hasn't seen yet
// (e.g. because shadow mode was only just enabled for the tenant, or a stream
// hasn't pushed since) don't count against the budget, so the cap can be
// looser than reality until the store warms up -- an accepted approximation
// while the feature is observation-only.
func (s *streamShardStore) room(streams map[uint64]streamShardUsage, maxStreams uint64, existing streamShardUsage, selfKnown bool) int64 {
	if maxStreams == 0 {
		return 1<<63 - 1
	}
	othersSlots := bucketSlots(streams)
	if selfKnown {
		othersSlots -= streamShardSlots(existing.shardCount)
	}

	return max(0, int64(maxStreams)-int64(othersSlots))
}

// Evict evicts all streams that have not been seen within the active
// window, mirroring usageStore.Evict.
func (s *streamShardStore) Evict() map[string]int {
	cutoff := time.Now().Add(-s.activeWindow).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, streams := range policies {
					for streamHash, stream := range streams {
						if stream.lastSeenAt < cutoff {
							delete(streams, streamHash)
							evicted[tenant]++
						}
					}
				}
			}
		}
	})
	return evicted
}

// Describe implements [prometheus.Collector].
func (s *streamShardStore) Describe(descs chan<- *prometheus.Desc) {
	descs <- streamShardTrackedStreamsDesc
	descs <- streamShardTotalStreamsDesc
	descs <- streamShardTotalShardsDesc
}

// Collect implements [prometheus.Collector].
func (s *streamShardStore) Collect(metrics chan<- prometheus.Metric) {
	var (
		// trackedStreams: distinct logical (pre-shard) streams.
		// totalStreams: physical streams (unsharded + sharded pieces).
		// totalShards: physical pieces belonging to sharded streams only
		// (shardCount >= 2); totalStreams - totalShards = unsharded streams.
		trackedStreams = make(map[string]int)
		totalStreams   = make(map[string]uint64)
		totalShards    = make(map[string]uint64)
	)
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, streams := range policies {
					for _, stream := range streams {
						trackedStreams[tenant]++
						totalStreams[tenant] += streamShardSlots(stream.shardCount)
						if stream.shardCount >= 2 {
							totalShards[tenant] += uint64(stream.shardCount)
						}
					}
				}
			}
		}
	})
	for tenant, n := range trackedStreams {
		metrics <- prometheus.MustNewConstMetric(
			streamShardTrackedStreamsDesc,
			prometheus.GaugeValue,
			float64(n),
			tenant,
		)
	}
	for tenant, n := range totalStreams {
		metrics <- prometheus.MustNewConstMetric(
			streamShardTotalStreamsDesc,
			prometheus.GaugeValue,
			float64(n),
			tenant,
		)
	}
	for tenant, n := range totalShards {
		metrics <- prometheus.MustNewConstMetric(
			streamShardTotalShardsDesc,
			prometheus.GaugeValue,
			float64(n),
			tenant,
		)
	}
}

// EvictPartitions evicts all streams for the specified partitions, mirroring
// usageStore.EvictPartitions. Called when this instance is no longer
// assigned those partitions (see partition_lifecycler.go).
func (s *streamShardStore) EvictPartitions(partitionsToEvict []int32) {
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, p := range partitionsToEvict {
				delete(partitions, p)
			}
			if len(partitions) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
	})
}

// setForTests directly seeds a stream's state. Used in tests only, to set
// up capacity/room scenarios precisely without indirectly driving them
// through rate-bucket math. Not goroutine-safe.
func (s *streamShardStore) setForTests(tenant string, partition int32, policyBucket string, stream streamShardUsage) {
	s.withLock(tenant, func(i int) {
		streams := s.checkInitMap(i, tenant, partition, policyBucket)
		streams[stream.hash] = stream
	})
}

func (s *streamShardStore) updateRateBucket(stream *streamShardUsage, sizeDelta uint64, seenAt time.Time) {
	if len(stream.rateBuckets) == 0 {
		stream.rateBuckets = make([]rateBucket, s.numBuckets)
	}
	seenAtUnixNano := seenAt.UnixNano()
	bucketNum := seenAtUnixNano / int64(s.bucketSize)
	bucketIdx := int(bucketNum % int64(s.numBuckets))
	b := stream.rateBuckets[bucketIdx]
	bucketStart := seenAt.Truncate(s.bucketSize).UnixNano()
	if b.timestamp < bucketStart {
		b.timestamp = bucketStart
		b.size = 0
	}
	b.size += sizeDelta
	stream.rateBuckets[bucketIdx] = b
}

func (s *streamShardStore) getPartitionForHash(hash uint64) int32 {
	return int32(hash % uint64(s.numPartitions))
}

func (s *streamShardStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

func (s *streamShardStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

func (s *streamShardStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

// forEachRLock executes fn with a shared lock for each stripe.
func (s *streamShardStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// checkInitMap initializes the maps for tenant/partition/policy if needed
// and returns the stream map. It must not be called without the stripe lock.
func (s *streamShardStore) checkInitMap(i int, tenant string, partition int32, policy string) map[uint64]streamShardUsage {
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(streamShardTenantUsage)
	}
	if _, ok := s.stripes[i][tenant][partition]; !ok {
		s.stripes[i][tenant][partition] = make(map[string]map[uint64]streamShardUsage)
	}
	if _, ok := s.stripes[i][tenant][partition][policy]; !ok {
		s.stripes[i][tenant][partition][policy] = make(map[uint64]streamShardUsage)
	}
	return s.stripes[i][tenant][partition][policy]
}

// streamShardSlots returns how many budget slots a stream with the given shard
// count consumes: a sharded stream consumes one slot per shard, while 0
// (unsharded/unknown) and 1 both consume exactly 1 slot.
func streamShardSlots(shardCount uint32) uint64 {
	return max(1, uint64(shardCount))
}

// bucketSlots returns the total slots consumed by all streams in the map.
func bucketSlots(streams map[uint64]streamShardUsage) (n uint64) {
	for _, stream := range streams {
		n += streamShardSlots(stream.shardCount)
	}
	return n
}

// rateBucketsCold returns true if none of the buckets have ever been
// written to, i.e. this instance has not observed any live traffic for the
// stream since the buckets were last (re)initialized (e.g. right after this
// instance took ownership of the stream's partition).
func rateBucketsCold(buckets []rateBucket) bool {
	for _, b := range buckets {
		if b.timestamp != 0 {
			return false
		}
	}
	return true
}

// currentRate computes a windowed-average byte rate from buckets, using the
// same technique as usageStore/Service.UpdateRates: sum the bytes in
// buckets that fall within the rate window, divided by the rate window
// duration.
func currentRate(buckets []rateBucket, now time.Time, rateWindow time.Duration) uint64 {
	withinWindow := func(t int64) bool {
		return now.Add(-rateWindow).UnixNano() <= t
	}
	active := getActiveRateBuckets(buckets, withinWindow)
	if len(active) == 0 {
		return 0
	}
	var total uint64
	for _, b := range active {
		total += b.size
	}
	seconds := rateWindow.Seconds()
	if seconds <= 0 {
		return 0
	}
	return uint64(float64(total) / seconds)
}

func ceilDivU32(a uint64, b uint64) uint32 {
	if b == 0 {
		return 1
	}
	return uint32((a + b - 1) / b)
}
