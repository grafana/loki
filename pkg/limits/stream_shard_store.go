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
	streamShardAllocatedShardsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_allocated_shards",
		"The current sum of shard counts granted by the stream-shard-tracking pipeline per tenant. This is a prediction, not a live count: a stream that shrinks here doesn't retroactively shrink in ingesters, which are still driven by the legacy rate-store path during shadow mode, so there is an inherent lag. Compare against loki_ingester_memory_streams for the real, physical per-tenant stream count.",
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
// streamShardStore reads (never writes) usageStore's stream count via
// streamsUsed, since shard growth must not push a tenant over
// max_global_streams_per_user. See room for how the two counts are combined
// without double-counting streams known to both.
type streamShardStore struct {
	activeWindow  time.Duration
	rateWindow    time.Duration
	bucketSize    time.Duration
	numBuckets    int
	numPartitions int

	stripes []map[string]streamShardTenantUsage
	locks   []stripeLock

	limits Limits
	// streamsUsed returns the number of streams usageStore currently tracks
	// for a tenant/partition/policy bucket. Wired to usageStore.StreamsUsed.
	streamsUsed func(tenant string, partition int32, policyBucket string) uint64
}

// streamShardTenantUsage is the per-tenant state: partition -> policy -> bucket.
type streamShardTenantUsage map[int32]map[string]*streamShardBucket

// streamShardBucket holds all streams tracked by streamShardStore for one
// (tenant, partition, policy bucket).
type streamShardBucket struct {
	streams map[uint64]streamShardUsage
}

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
	streamsUsed func(tenant string, partition int32, policyBucket string) uint64,
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
		streamsUsed:   streamsUsed,
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
			// Stop early if the caller has already given up (deadline
			// exceeded or cancelled) rather than doing abandoned work while
			// holding the stripe lock. Streams not yet processed are simply
			// absent from the response; callers fail open for them.
			if ctx.Err() != nil {
				return
			}
			partition := s.getPartitionForHash(m.StreamHash)
			shardCfg, _ := s.limits.PolicyShardStreams(tenant, m.IngestionPolicy)
			policyBucket, maxStreams := getPolicyBucketAndStreamsLimit(s.limits, s.numPartitions, tenant, m.IngestionPolicy)
			bucket := s.checkInitMap(i, tenant, partition, policyBucket)

			existing, wasPresent := bucket.streams[m.StreamHash]
			isNewOrExpired := !wasPresent || existing.lastSeenAt < cutoff

			// selfKnown is wasPresent, not "wasPresent and still valid": even
			// an expired entry is still sitting in bucket.streams (it isn't
			// deleted/reset until later in this same call), so bucketSlots
			// below would otherwise double-count its own stale allocation as
			// belonging to some other stream, wrongly shrinking room for a
			// reactivating stream evaluating itself.
			room := s.room(bucket, tenant, partition, policyBucket, maxStreams, existing, wasPresent)

			var (
				stream  streamShardUsage
				desired uint32
			)
			switch {
			case isNewOrExpired:
				// Reset (mirrors usageStore.update's reset-on-expiry
				// semantics). No rate history: never shard on first sight.
				stream = streamShardUsage{hash: m.StreamHash, policy: policyBucket}
				if maxStreams > 0 && room < 1 {
					// No room even for a single slot: reject outright.
					delete(bucket.streams, m.StreamHash)
					results = append(results, &proto.StreamShardResult{
						StreamHash:   m.StreamHash,
						Shards:       0,
						RejectReason: ReasonMaxStreams.String(),
					})
					continue
				}
				// Seed the first rate bucket with this push's bytes even
				// though the decision itself stays at 1 shard: otherwise
				// this push's bytes are dropped forever (never fed into any
				// rate computation), and only the stream's SECOND push would
				// initialize the buckets, biasing the rate low for bursty or
				// infrequently-pushed streams.
				s.updateRateBucket(&stream, m.TotalSize, seenAt)
				desired = 1
			case !shardCfg.Enabled:
				stream = existing
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
					desired = maxu32(1, stream.shardCount)
				} else {
					rate := currentRate(stream.rateBuckets, seenAt, s.rateWindow)
					desired = maxu32(1, ceilDivU32(rate, uint64(shardCfg.DesiredRate.Val())))
				}
			}

			// granted combines the rate-justified desired count with the
			// tenant's remaining budget. The two cases are NOT symmetric:
			//
			//   - Shrinking (desired <= existing.shardCount) is always
			//     self-inflicted (this stream's own rate dropped) and always
			//     safe -- it only frees room for others, so it is never
			//     capped by room. Capping it here would double-penalize a
			//     stream that's already giving back capacity.
			//   - Growing (desired > existing.shardCount) is capped by room,
			//     but the floor is existing.shardCount, NOT a flat 1 or 0:
			//     an active stream must never be forced to shrink just
			//     because *other* streams grew and consumed the room it
			//     would have grown into. It can only ever shrink in
			//     response to its own rate (the branch above).
			//
			// A brand-new/expired stream has no "existing" allocation to
			// floor against, so it is simply capped to whatever room
			// remains (already guaranteed >= 1, or rejected above).
			var granted uint32
			switch {
			case maxStreams == 0:
				granted = desired
			case isNewOrExpired:
				granted = minu32(desired, uint32(room))
			case desired <= existing.shardCount:
				granted = desired
			default:
				granted = maxu32(existing.shardCount, minu32(desired, uint32(room)))
			}

			shardDecisionContext := ReasonUnknown
			if granted < desired {
				shardDecisionContext = ReasonStreamShardsCapped
			}

			stream.hash = m.StreamHash
			stream.policy = policyBucket
			stream.shardCount = granted
			seenAtNano := seenAt.UnixNano()
			if seenAtNano > stream.lastSeenAt {
				stream.lastSeenAt = seenAtNano
			}
			bucket.streams[m.StreamHash] = stream

			results = append(results, &proto.StreamShardResult{
				StreamHash:           m.StreamHash,
				Shards:               granted,
				ShardDecisionContext: uint32(shardDecisionContext),
			})
		}
	})
	return results
}

// room returns how many additional slots are available in bucket for
// maxStreams, excluding whatever the stream identified by existing/selfKnown
// already holds. maxStreams == 0 (unlimited) is handled by callers, not
// here.
//
// The tenant's real usage is the union of two, mostly-disjoint views:
//   - usageStore's count of logical streams it knows about (accurate for
//     streams this pipeline hasn't evaluated yet, e.g. because "shadow"
//     mode was only just enabled for the tenant/policy).
//   - streamShardStore's own bucket, which is accurate (including shard
//     multiplicity) for every stream this pipeline has already evaluated at
//     least once.
//
// Rather than determine the exact overlap between the two sets (which would
// require exposing usageStore's actual stream-hash keys), this estimates the
// count of "not yet known to streamShardStore" streams as
// max(0, usageStoreCount - knownToStreamShardStore). This is exact in both
// steady states (all streams known to one store or the other) and
// self-corrects as a tenant/policy migrates between modes.
func (s *streamShardStore) room(bucket *streamShardBucket, tenant string, partition int32, policyBucket string, maxStreams uint64, existing streamShardUsage, selfKnown bool) int64 {
	if maxStreams == 0 {
		return 1<<63 - 1
	}
	knownToStreamShardStore := uint64(len(bucket.streams))
	usageStoreCount := s.streamsUsed(tenant, partition, policyBucket)
	var notYetMigrated uint64
	if usageStoreCount > knownToStreamShardStore {
		notYetMigrated = usageStoreCount - knownToStreamShardStore
	}
	othersSlots := bucketSlots(bucket)
	if selfKnown {
		othersSlots -= streamShardSlots(existing.shardCount)
	}
	room := int64(maxStreams) - int64(notYetMigrated) - int64(othersSlots)
	if room < 0 {
		room = 0
	}
	return room
}

// Evict evicts all streams that have not been seen within the active
// window, mirroring usageStore.Evict.
func (s *streamShardStore) Evict() map[string]int {
	cutoff := time.Now().Add(-s.activeWindow).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, bucket := range policies {
					for streamHash, stream := range bucket.streams {
						if stream.lastSeenAt < cutoff {
							delete(bucket.streams, streamHash)
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
	descs <- streamShardAllocatedShardsDesc
}

// Collect implements [prometheus.Collector].
func (s *streamShardStore) Collect(metrics chan<- prometheus.Metric) {
	var (
		trackedStreams  = make(map[string]int)
		allocatedShards = make(map[string]uint64)
	)
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, bucket := range policies {
					for _, stream := range bucket.streams {
						trackedStreams[tenant]++
						allocatedShards[tenant] += streamShardSlots(stream.shardCount)
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
	for tenant, n := range allocatedShards {
		metrics <- prometheus.MustNewConstMetric(
			streamShardAllocatedShardsDesc,
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
		bucket := s.checkInitMap(i, tenant, partition, policyBucket)
		bucket.streams[stream.hash] = stream
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
// and returns the bucket. It must not be called without the stripe lock.
func (s *streamShardStore) checkInitMap(i int, tenant string, partition int32, policy string) *streamShardBucket {
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(streamShardTenantUsage)
	}
	if _, ok := s.stripes[i][tenant][partition]; !ok {
		s.stripes[i][tenant][partition] = make(map[string]*streamShardBucket)
	}
	if _, ok := s.stripes[i][tenant][partition][policy]; !ok {
		s.stripes[i][tenant][partition][policy] = &streamShardBucket{streams: make(map[uint64]streamShardUsage)}
	}
	return s.stripes[i][tenant][partition][policy]
}

// streamShardSlots returns how many budget slots a stream with the given shard
// count consumes: 0 (unsharded/unknown) and 1 both consume exactly 1 slot.
func streamShardSlots(shardCount uint32) uint64 {
	if shardCount == 0 {
		return 1
	}
	return uint64(shardCount)
}

// bucketSlots returns the total slots consumed by all streams in bucket.
func bucketSlots(bucket *streamShardBucket) uint64 {
	var n uint64
	for _, stream := range bucket.streams {
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
	if a == 0 {
		return 0
	}
	return uint32((a + b - 1) / b)
}

func minu32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func maxu32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
