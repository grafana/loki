package limits

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
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
// streams it has itself evaluated (see checkAndShard). A tenant whose streams
// predate shadow mode simply warms up until it has seen them, so the cap may
// be looser than reality during that window -- an accepted approximation
// while the feature is observation-only.
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

	// shardLastUsed tracks the physical shard footprint for stream-count
	// accounting, decoupled from the rate-based shardCount recommendation.
	// Index i holds the UnixNano at which shard i was last covered by a
	// recommendation (i.e. granted > i). Shards the recommendation later stops
	// covering keep their old timestamp and expire individually after
	// activeWindow, mirroring how sharded sub-streams flush their chunks at the
	// ingesters. The slice is non-increasing in i (a higher index is refreshed
	// only when a lower one is), so the live shards are always a prefix.
	shardLastUsed []int64
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
//   - A stream whose policy has sharding disabled returns 1 shard and is
//     not tracked (no rate to record, and the legacy path accounts for it).
//   - An existing stream with sharding enabled has its rate recomputed from
//     the observation in this call and a "desired" shard count derived from
//     rate/desiredRate. The returned recommendation follows the rate: it grows
//     up to this stream's budget ceiling (maxStreams minus the other streams'
//     live shards) and shrinks freely when the rate drops.
//   - The shard-count budget/accounting is decoupled from that recommendation:
//     each granted shard is recorded in shardLastUsed and stays counted until it
//     expires (activeWindow), mirroring how sharded sub-streams persist at the
//     ingesters after the rate drops. So shrinking the recommendation does not
//     immediately free budget; the shards age out individually.
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
			if isNewOrExpired {
				// Drop the stale entry now: it's expired, so if this push ends
				// up rejected it must not linger (and keep counting against
				// other streams) until the next eviction sweep.
				delete(streams, m.StreamHash)
			}

			// budget is the most live shards this stream may hold (only enforced
			// when maxStreams != 0): maxStreams minus the live-shard footprint of
			// every OTHER stream in the bucket. Excluding this stream (rather
			// than counting it and adding it back) means each stream's shards are
			// scanned at most once, and it implicitly lets this stream keep
			// (re-use) the shards it already holds live for free.
			budget := max(0, int64(maxStreams)-int64(othersLiveSlots(streams, m.StreamHash, cutoff)))

			var (
				stream        streamShardUsage
				desired       uint32
				evaluatedRate uint64 // byte/s rate that drove the decision, for shadow debugging; 0 unless computed
			)
			switch {
			case !shardCfg.Enabled:
				// Sharding disabled for this policy: never shard, and drop any
				// tracked state so it stops consuming budget -- a stream whose
				// policy flipped to disabled must not keep its stale shards.
				delete(streams, m.StreamHash)
				results = append(results, &proto.StreamShardResult{
					StreamHash: m.StreamHash,
					Shards:     1,
				})
				continue
			case isNewOrExpired:
				if maxStreams > 0 && budget < 1 {
					// No room even for a single slot: reject outright.
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
					window := clampShardRateWindow(shardCfg.LimitsServiceStreamShardingRateWindow, s.bucketSize, s.rateWindow)
					evaluatedRate = currentRate(stream.rateBuckets, seenAt, window)
					desired = max(1, ceilDivU32(evaluatedRate, uint64(shardCfg.DesiredRate.Val())))
				}
			}

			// Cap the recommendation to this stream's budget ceiling (the
			// shards it already holds live are excluded from budget, so it keeps
			// them for free and grows only into what the other streams leave).
			// Shrinking below that is free, and the shards a lower recommendation
			// stops covering stay counted until they expire (see the
			// shardLastUsed refresh below).
			granted := desired
			if maxStreams != 0 && int64(desired) > budget {
				granted = uint32(budget)
			}

			shardDecisionContext := ReasonUnknown
			if granted < desired {
				shardDecisionContext = ReasonStreamShardsCapped
			}

			stream.hash = m.StreamHash
			stream.policy = policyBucket
			stream.shardCount = granted
			stream.lastSeenAt = max(seenAt.UnixNano(), stream.lastSeenAt)
			// Refresh the shards this recommendation covers; those it no longer
			// covers keep their timestamps and age out of the live count,
			// preserving the physical footprint for stream-count accounting.
			stream.shardLastUsed = refreshLiveShards(stream.shardLastUsed, granted, seenAt.UnixNano())
			streams[m.StreamHash] = stream

			results = append(results, &proto.StreamShardResult{
				StreamHash:           m.StreamHash,
				Shards:               granted,
				ShardDecisionContext: uint32(shardDecisionContext),
				EvaluatedRate:        evaluatedRate,
			})
		}
	})
	return results
}

// Evict evicts all streams that have not been seen within the active
// window, mirroring usageStore.Evict. For streams that survive, it also trims
// individually-expired shards out of shardLastUsed so the tracked footprint (and
// its memory) tracks the live shards.
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
							continue
						}
						// shardLastUsed is prefix-live, so the live shards are the
						// leading n entries; drop the expired remainder.
						if n := int(liveShardCount(stream.shardLastUsed, cutoff)); n < len(stream.shardLastUsed) {
							trimmed := make([]int64, n)
							copy(trimmed, stream.shardLastUsed)
							stream.shardLastUsed = trimmed
							streams[streamHash] = stream
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
	cutoff := time.Now().Add(-s.activeWindow).UnixNano()
	var (
		// trackedStreams: distinct logical (pre-shard) streams.
		// totalStreams: live physical streams (unsharded + live sharded pieces).
		// totalShards: live physical pieces belonging to sharded streams only
		// (live count >= 2); totalStreams - totalShards = unsharded streams.
		// These use the live-shard footprint (not the instantaneous rate-based
		// recommendation) so they track the ingesters' physical stream count.
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
						live := liveShardCount(stream.shardLastUsed, cutoff)
						totalStreams[tenant] += max(1, uint64(live))
						if live >= 2 {
							totalShards[tenant] += uint64(live)
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

// liveShardCount returns how many of a stream's shards are still live: those
// last covered by a recommendation within the active window (timestamp >=
// cutoff). Because shardLastUsed is non-increasing in index, the live shards are a
// prefix, but a simple scan is used since the slice is short.
func liveShardCount(shardLastUsed []int64, cutoff int64) uint32 {
	var n uint32
	for _, ts := range shardLastUsed {
		if ts >= cutoff {
			n++
		}
	}
	return n
}

// streamLiveSlots returns how many budget slots a stream consumes: one per live
// shard, with a floor of 1 so a tracked (unsharded/just-reset) stream still
// counts as one stream.
func streamLiveSlots(shardLastUsed []int64, cutoff int64) uint64 {
	return max(1, uint64(liveShardCount(shardLastUsed, cutoff)))
}

// othersLiveSlots returns the total live slots consumed by every stream in the
// map except exceptHash.
func othersLiveSlots(streams map[uint64]streamShardUsage, exceptHash uint64, cutoff int64) (n uint64) {
	for hash, stream := range streams {
		if hash == exceptHash {
			continue
		}
		n += streamLiveSlots(stream.shardLastUsed, cutoff)
	}
	return n
}

// refreshLiveShards marks shard indices [0, count) as used at now, growing the
// slice if the recommendation reached a new high, and returns it. Indices at or
// above count are left untouched so they keep aging toward expiry.
func refreshLiveShards(shardLastUsed []int64, count uint32, now int64) []int64 {
	if int(count) > len(shardLastUsed) {
		grown := make([]int64, count)
		copy(grown, shardLastUsed)
		shardLastUsed = grown
	}
	for i := uint32(0); i < count; i++ {
		shardLastUsed[i] = now
	}
	return shardLastUsed
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
	seconds := rateWindow.Seconds()
	if seconds <= 0 {
		return 0
	}
	// Sum only the buckets still inside the rate window. The ring buffer
	// resets a slot lazily, on reuse, so slots not touched this window still
	// hold stale data that must be skipped here rather than counted.
	cutoff := now.Add(-rateWindow).UnixNano()
	var total uint64
	for _, b := range buckets {
		if b.timestamp >= cutoff {
			total += b.size
		}
	}
	return uint64(float64(total) / seconds)
}

// clampShardRateWindow resolves the rate-averaging window for the shard
// decision: the per-tenant/policy override when set (> 0), otherwise the
// store-wide default. The result is clamped to [bucketSize, rateWindow] -- the
// per-stream rate-bucket ring holds only rateWindow of history, and a window
// shorter than one bucket can't be measured. A shorter window reacts to
// shorter bursts, closer to the distributor's local rate store.
func clampShardRateWindow(configured, bucketSize, rateWindow time.Duration) time.Duration {
	if configured <= 0 {
		return rateWindow
	}
	return min(max(configured, bucketSize), rateWindow)
}

func ceilDivU32(a uint64, b uint64) uint32 {
	if b == 0 {
		return 1
	}
	// Clamp to MaxUint32 rather than letting the uint32 conversion wrap: a
	// huge rate over a tiny desired rate could otherwise overflow to a small
	// (or zero) shard count, i.e. under-shard the hottest streams.
	return uint32(min(math.MaxUint32, (a+b-1)/b))
}
