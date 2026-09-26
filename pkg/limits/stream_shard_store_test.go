package limits

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

const (
	testActiveWindow = 5 * time.Minute
	testRateWindow   = time.Minute
	testBucketSize   = 10 * time.Second
	testZone         = "zone1"
)

// newTestStreamShardStore returns a store with a single partition and one
// stripe's worth of tenants, and a mock clock. maxGlobalStreams of 0 means no
// stream limit. desiredRate is a byte size such as "1KB".
func newTestStreamShardStore(t *testing.T, maxGlobalStreams int, desiredRate string) (*streamShardStore, *quartz.Mock) {
	t.Helper()
	limits := &mockLimits{
		MaxGlobalStreams:       maxGlobalStreams,
		UnlimitedGlobalStreams: maxGlobalStreams == 0,
		ShardStreamsConfig:     shardstreams.Config{Enabled: true},
	}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set(desiredRate))
	s, err := newStreamShardStore(testActiveWindow, testRateWindow, testBucketSize, 1, testZone, true, limits, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	return s, clock
}

// track stores a stream directly, as if the store had granted it shards
// earlier, so that a test can start from a given footprint.
func track(t *testing.T, s *streamShardStore, stream streamShardUsage, now time.Time) {
	t.Helper()
	s.withLock("test", func(i int) {
		s.checkInitMap(i, "test", s.getPartitionForHash(stream.hash), noPolicy).
			put(stream, now.Add(-s.activeWindow).UnixNano())
	})
}

// push sends one push of size bytes for streamHash and returns its result.
func push(t *testing.T, s *streamShardStore, streamHash, size uint64, seenAt time.Time) *proto.StreamShardResult {
	t.Helper()
	results, _ := pushWithRecords(t, s, streamHash, size, seenAt)
	return results
}

// pushWithRecords sends one push and returns its result together with the
// records the store wants produced.
func pushWithRecords(t *testing.T, s *streamShardStore, streamHash, size uint64, seenAt time.Time) (*proto.StreamShardResult, []*proto.StreamMetadataRecord) {
	t.Helper()
	results, toProduce := s.checkAndShard(t.Context(), "test", []*proto.StreamMetadata{{
		StreamHash: streamHash,
		TotalSize:  size,
	}}, seenAt)
	require.Len(t, results, 1)
	return results[0], toProduce
}

func TestStreamShardStore_NewStreamGetsOneShard(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// A brand-new stream has no rate history, so it gets one shard however
	// large this push is.
	res := push(t, s, 0x1, 10<<20, clock.Now())
	require.Equal(t, &proto.StreamShardResult{
		StreamHash: 0x1,
		Shards:     1,
		Stats:      &proto.ShardStats{},
	}, res)
}

func TestStreamShardStore_ShardCountFollowsTheRate(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")

	// Push 6KiB per bucket for the whole rate window: 6 buckets of 6KiB over
	// 60s is a sustained 614 B/s, which with this push amortized on top
	// justifies more than one shard at a desired rate of 1KiB/s.
	for range testRateWindow / testBucketSize {
		push(t, s, 0x1, 6<<10, clock.Now())
		clock.Advance(testBucketSize)
	}
	res := push(t, s, 0x1, 6<<10, clock.Now())
	require.Equal(t, uint32(2), res.Shards)
	require.Equal(t, uint64(614), res.Stats.EvaluatedRate)
	require.Equal(t, uint32(ReasonUnknown), res.Stats.ShardDecisionContext)

	// The push being decided on is not part of the rate it is decided
	// against, so a large push reports the same sustained rate. It is
	// amortized on top of that rate once, not twice.
	res = push(t, s, 0x1, 100<<20, clock.Now())
	require.Equal(t, uint64(614), res.Stats.EvaluatedRate)

	// The rate decays once the pushes stop, and so does the shard count. The
	// buckets hold one rate window of history, so skipping two windows leaves
	// nothing inside it.
	clock.Advance(2 * testRateWindow)
	res = push(t, s, 0x1, 1, clock.Now())
	require.Equal(t, uint32(1), res.Shards)
}

func TestStreamShardStore_ShardCountIsCappedByTheStreamLimit(t *testing.T) {
	// Two streams share a budget of two, so the hot stream can hold a single
	// shard: two minus the one slot the other stream occupies. Its rate
	// justifies two, as in TestStreamShardStore_ShardCountFollowsTheRate.
	s, clock := newTestStreamShardStore(t, 2, "1KB")
	push(t, s, 0x2, 1, clock.Now())

	for range testRateWindow / testBucketSize {
		push(t, s, 0x1, 6<<10, clock.Now())
		clock.Advance(testBucketSize)
	}
	res := push(t, s, 0x1, 6<<10, clock.Now())
	require.Equal(t, uint32(1), res.Shards)
	require.Equal(t, uint32(ReasonStreamShardsCapped), res.Stats.ShardDecisionContext)
}

func TestStreamShardStore_TrackedStreamKeepsOneShardWithoutBudget(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 4, "1KB")
	for range testRateWindow / testBucketSize {
		push(t, s, 0x2, 6<<10, clock.Now())
		clock.Advance(testBucketSize)
	}

	// Another stream takes the whole budget, which is what a runtime decrease
	// of max_global_streams_per_user leaves behind.
	track(t, s, streamShardUsage{
		hash:          0x4,
		shardCount:    4,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 4, clock.Now().UnixNano()),
	}, clock.Now())

	// The tracked stream keeps a shard: it is still being written, and zero
	// shards would report it as rejected.
	res := push(t, s, 0x2, 6<<10, clock.Now())
	require.Equal(t, uint32(1), res.Shards)
	require.Empty(t, res.RejectReason)
	require.Equal(t, uint32(ReasonStreamShardsCapped), res.Stats.ShardDecisionContext)
}

func TestStreamShardStore_NewStreamIsRejectedWithoutBudget(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 1, "1KB")
	push(t, s, 0x1, 1, clock.Now())

	// The budget of one is taken, so a second, brand-new stream is rejected
	// outright rather than granted a shard.
	res := push(t, s, 0x2, 1, clock.Now())
	require.Equal(t, &proto.StreamShardResult{
		StreamHash:   0x2,
		Shards:       0,
		RejectReason: ReasonMaxStreams.String(),
	}, res)

	// A rejected stream is not tracked, so it does not consume budget itself.
	require.Equal(t, 1, countTrackedStreams(s))
}

func TestStreamShardStore_ShardingDisabledForThePolicy(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	for range testRateWindow / testBucketSize {
		push(t, s, 0x1, 6<<10, clock.Now())
		clock.Advance(testBucketSize)
	}
	require.Greater(t, push(t, s, 0x1, 6<<10, clock.Now()).Shards, uint32(1))

	// Disabling sharding drops the tracked state, so the stream stops
	// consuming budget with its stale shards.
	s.limits.(*mockLimits).ShardStreamsConfig.Enabled = false
	res := push(t, s, 0x1, 6<<10, clock.Now())
	require.Equal(t, &proto.StreamShardResult{StreamHash: 0x1, Shards: 1}, res)
	require.Equal(t, 0, countTrackedStreams(s))
}

func TestStreamShardStore_StreamWithoutRecentTrafficGetsOneShard(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	t0 := clock.Now()
	track(t, s, streamShardUsage{
		hash:          0x1,
		shardCount:    4,
		lastSeenAt:    t0.UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 4, t0.UnixNano()),
	}, t0)

	// A tracked stream this instance has observed no traffic for has no rate
	// to shard on, however large the push is.
	res := push(t, s, 0x1, 200<<20, t0)
	require.Equal(t, uint32(1), res.Shards)
	require.Zero(t, res.Stats.EvaluatedRate)

	// Nor has a stream whose traffic has aged out of the rate window while it
	// stayed inside the active window.
	clock.Advance(2 * testRateWindow)
	res = push(t, s, 0x1, 200<<20, clock.Now())
	require.Equal(t, uint32(1), res.Shards)
	require.Zero(t, res.Stats.EvaluatedRate)
}

func TestStreamShardStore_ShrunkShardsKeepCountingUntilTheyExpire(t *testing.T) {
	// The budget is two and the only stream holds both shards, so a second
	// stream has no room even after the first stream's recommendation shrinks
	// back to one shard. The shards it no longer covers keep counting until
	// they age out of the active window.
	s, clock := newTestStreamShardStore(t, 2, "1KB")
	track(t, s, streamShardUsage{
		hash:          0x1,
		shardCount:    2,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 2, clock.Now().UnixNano()),
	}, clock.Now())
	push(t, s, 0x1, 1, clock.Now())
	require.Equal(t, ReasonMaxStreams.String(), push(t, s, 0x2, 1, clock.Now()).RejectReason)

	// Once the second shard has expired, the freed slot lets the new stream in.
	clock.Advance(testActiveWindow + time.Second)
	push(t, s, 0x1, 1, clock.Now())
	require.Empty(t, push(t, s, 0x2, 1, clock.Now()).RejectReason)
}

func TestStreamShardStore_SlotsFollowTheTrackedStreams(t *testing.T) {
	// The bucket's slot total is what a push reads instead of scanning the
	// bucket, so it has to stay in step with the streams it tracks.
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	bucketSlots := func() uint64 {
		var slots uint64
		s.withLock("test", func(i int) {
			slots = s.stripes[i]["test"][0][noPolicy].slots
		})
		return slots
	}

	// Two unsharded streams hashing to the same partition, one slot each.
	push(t, s, 0x2, 1, clock.Now())
	push(t, s, 0x4, 1, clock.Now())
	require.Equal(t, uint64(2), bucketSlots())

	// A stream granted more shards claims a slot per shard.
	track(t, s, streamShardUsage{
		hash:          0x6,
		shardCount:    3,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 3, clock.Now().UnixNano()),
	}, clock.Now())
	require.Equal(t, uint64(5), bucketSlots())

	// Disabling sharding for the policy releases that stream's slots.
	s.limits.(*mockLimits).ShardStreamsConfig.Enabled = false
	push(t, s, 0x6, 1, clock.Now())
	require.Equal(t, uint64(2), bucketSlots())

	// So does eviction.
	clock.Advance(testActiveWindow + time.Second)
	require.Equal(t, map[string]int{"test": 2}, s.Evict())
	require.Equal(t, uint64(0), bucketSlots())
}

// mergeRecord merges one record for the test tenant.
func mergeRecord(s *streamShardStore, zone string, streamHash uint64, bucketStart time.Time, size, pushes uint64, shardCount uint32) {
	s.merge("test", &proto.StreamMetadataRecord{
		Zone:     zone,
		Tenant:   "test",
		Metadata: &proto.StreamMetadata{StreamHash: streamHash},
		ShardRateBucket: &proto.ShardRateBucket{
			BucketStart: bucketStart.Truncate(testBucketSize).UnixNano(),
			Size_:       size,
			Pushes:      pushes,
		},
		ShardCount: shardCount,
	})
}

// trackedStream returns the state tracked for streamHash in the test
// tenant's default policy bucket.
func trackedStream(t *testing.T, s *streamShardStore, streamHash uint64) streamShardUsage {
	t.Helper()
	var stream streamShardUsage
	s.withLock("test", func(i int) {
		bucket := s.stripes[i]["test"][s.getPartitionForHash(streamHash)][noPolicy]
		require.NotNil(t, bucket)
		stream = bucket.streams[streamHash]
	})
	return stream
}

func TestStreamShardStore_ProducesOneRecordPerCompleteBucket(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// Nothing to produce while the first bucket is still in flight.
	_, toProduce := pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Empty(t, toProduce)
	_, toProduce = pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Empty(t, toProduce)

	// The first push of the next bucket publishes the complete one.
	bucketStart := clock.Now().Truncate(testBucketSize)
	clock.Advance(testBucketSize)
	_, toProduce = pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Equal(t, []*proto.StreamMetadataRecord{{
		Tenant:   "test",
		Metadata: &proto.StreamMetadata{StreamHash: 0x1},
		ShardRateBucket: &proto.ShardRateBucket{
			BucketStart: bucketStart.UnixNano(),
			Size_:       1200,
			Pushes:      2,
		},
		ShardCount: 1,
	}}, toProduce)

	// The same bucket is not published twice.
	_, toProduce = pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Empty(t, toProduce)
}

func TestStreamShardStore_ProducesTheBucketsOfAStreamThatSkipsBuckets(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// A stream pushed to less often than once per bucket has no traffic in
	// the bucket before the push, but the bucket holding its previous push is
	// still complete and must be published.
	first := clock.Now().Truncate(testBucketSize)
	push(t, s, 0x1, 600, clock.Now())

	clock.Advance(2 * testBucketSize)
	second := clock.Now().Truncate(testBucketSize)
	_, toProduce := pushWithRecords(t, s, 0x1, 900, clock.Now())
	require.Equal(t, []*proto.StreamMetadataRecord{{
		Tenant:   "test",
		Metadata: &proto.StreamMetadata{StreamHash: 0x1},
		ShardRateBucket: &proto.ShardRateBucket{
			BucketStart: first.UnixNano(),
			Size_:       600,
			Pushes:      1,
		},
		ShardCount: 1,
	}}, toProduce)

	// The bucket just published is not published again by the next gap.
	clock.Advance(2 * testBucketSize)
	_, toProduce = pushWithRecords(t, s, 0x1, 300, clock.Now())
	require.Len(t, toProduce, 1)
	require.Equal(t, &proto.ShardRateBucket{
		BucketStart: second.UnixNano(),
		Size_:       900,
		Pushes:      1,
	}, toProduce[0].ShardRateBucket)

	// After a gap longer than the rate window the earlier buckets have fallen
	// out of it, and a record for them would be dropped by the consumer, so
	// nothing is published until the stream has a complete bucket again.
	clock.Advance(testRateWindow + testBucketSize)
	late := clock.Now().Truncate(testBucketSize)
	_, toProduce = pushWithRecords(t, s, 0x1, 300, clock.Now())
	require.Empty(t, toProduce)
	clock.Advance(testBucketSize)
	_, toProduce = pushWithRecords(t, s, 0x1, 300, clock.Now())
	require.Len(t, toProduce, 1)
	require.Equal(t, late.UnixNano(), toProduce[0].ShardRateBucket.BucketStart)
}

func TestStreamShardStore_ProducesNothingWithoutDurability(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	s.durabilityEnabled = false
	push(t, s, 0x1, 600, clock.Now())
	clock.Advance(testBucketSize)
	_, toProduce := pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Empty(t, toProduce)
}

func TestStreamShardStore_ProducedRecordRestoresTheRate(t *testing.T) {
	// What one store produces is what another store needs to reach the same
	// decision, so feed the records of a warm store into a cold one.
	warm, clock := newTestStreamShardStore(t, 0, "1KB")
	cold, _ := newTestStreamShardStore(t, 0, "1KB")
	cold.clock = clock
	// A zone only merges the other zones' records while it is serving, so the
	// cold store sees these as another zone's.
	cold.zone = "zone2"

	for range testRateWindow / testBucketSize {
		_, toProduce := pushWithRecords(t, warm, 0x1, 6<<10, clock.Now())
		for _, rec := range toProduce {
			rec.Zone = testZone
			cold.merge("test", rec)
		}
		clock.Advance(testBucketSize)
	}

	// The rate the cold store evaluates is the warm store's rate less the
	// bucket that was still in flight when the last record was produced.
	require.Equal(t, uint64(614), push(t, warm, 0x1, 6<<10, clock.Now()).Stats.EvaluatedRate)
	require.Equal(t, uint64(512), push(t, cold, 0x1, 6<<10, clock.Now()).Stats.EvaluatedRate)
	require.Equal(t, uint32(2), push(t, cold, 0x1, 6<<10, clock.Now()).Shards)
}

func TestStreamShardStore_MergeRestoresTheRateHistory(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// Replay a rate window's worth of another zone's buckets, 6KiB per bucket,
	// which is the history TestStreamShardStore_ShardCountFollowsTheRate
	// builds up by pushing.
	for range testRateWindow / testBucketSize {
		mergeRecord(s, "zone2", 0x1, clock.Now(), 6<<10, 1, 1)
		clock.Advance(testBucketSize)
	}
	// The first push after the merge is decided on that history instead of
	// looking brand new.
	res := push(t, s, 0x1, 6<<10, clock.Now())
	require.Equal(t, uint32(2), res.Shards)
	require.Equal(t, uint64(614), res.Stats.EvaluatedRate)
}

func TestStreamShardStore_MergeIsIdempotent(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	for range 3 {
		mergeRecord(s, "zone2", 0x1, clock.Now(), 6<<10, 3, 2)
	}
	stream := trackedStream(t, s, 0x1)
	bytes, pushes := sumRateBuckets(stream.remoteBuckets["zone2"], 0)
	require.Equal(t, uint64(6<<10), bytes)
	require.Equal(t, uint64(3), pushes)
	require.Equal(t, uint64(2), stream.slots)
}

func TestStreamShardStore_MergeKeepsTheLargerBucketTotals(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	mergeRecord(s, "zone2", 0x1, clock.Now(), 600, 3, 1)
	// A later record for the same bucket carries the zone's grown totals.
	mergeRecord(s, "zone2", 0x1, clock.Now(), 900, 4, 1)
	// An out-of-order record, such as one produced by the previous owner of
	// the partition, must not shrink them back.
	mergeRecord(s, "zone2", 0x1, clock.Now(), 300, 1, 1)
	stream := trackedStream(t, s, 0x1)
	bytes, pushes := sumRateBuckets(stream.remoteBuckets["zone2"], 0)
	require.Equal(t, uint64(900), bytes)
	require.Equal(t, uint64(4), pushes)

	// A record for the next bucket replaces the slot rather than merging into
	// it, as the ring reuses slots.
	clock.Advance(testRateWindow)
	mergeRecord(s, "zone2", 0x1, clock.Now(), 100, 1, 1)
	stream = trackedStream(t, s, 0x1)
	bytes, pushes = sumRateBuckets(stream.remoteBuckets["zone2"], 0)
	require.Equal(t, uint64(100), bytes)
	require.Equal(t, uint64(1), pushes)
}

func TestStreamShardStore_MergeOwnZoneDoesNotRegressLocalBuckets(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// A push, then our own record for the same bucket as it was when the
	// record was produced: replaying it must neither drop the bytes pushed
	// after the flush nor count them twice.
	push(t, s, 0x1, 600, clock.Now())
	push(t, s, 0x1, 600, clock.Now())
	mergeRecord(s, testZone, 0x1, clock.Now(), 600, 1, 1)
	stream := trackedStream(t, s, 0x1)
	require.Empty(t, stream.remoteBuckets)
	bytes, pushes := sumRateBuckets(stream.rateBuckets, 0)
	require.Equal(t, uint64(1200), bytes)
	require.Equal(t, uint64(2), pushes)
}

func TestStreamShardStore_MergeDoesNotReproduceTheRestoredBucket(t *testing.T) {
	// The topic already holds the record a store replays for its own zone, so
	// the replaying store must not write it a second time.
	warm, clock := newTestStreamShardStore(t, 0, "1KB")
	cold, _ := newTestStreamShardStore(t, 0, "1KB")
	cold.clock = clock

	push(t, warm, 0x1, 600, clock.Now())
	clock.Advance(testBucketSize)
	_, toProduce := pushWithRecords(t, warm, 0x1, 600, clock.Now())
	require.Len(t, toProduce, 1)
	produced := toProduce[0].ShardRateBucket.BucketStart
	for _, rec := range toProduce {
		rec.Zone = testZone
		cold.merge("test", rec)
	}
	require.Equal(t, produced, trackedStream(t, cold, 0x1).lastProducedBucket)

	// The replaying store is pushed to within the same bucket the record was
	// written in, which is the only window in which the cursor decides
	// anything: a later push finds the ring slot reused.
	_, toProduce = pushWithRecords(t, cold, 0x1, 600, clock.Now())
	require.Empty(t, toProduce)

	// Buckets that complete after the merge are still produced.
	clock.Advance(testBucketSize)
	_, toProduce = pushWithRecords(t, cold, 0x1, 600, clock.Now())
	require.Len(t, toProduce, 1)
	require.Equal(t, produced+int64(testBucketSize), toProduce[0].ShardRateBucket.BucketStart)
}

func TestStreamShardStore_MergeOtherZoneKeepsTheProduceCursor(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	push(t, s, 0x1, 600, clock.Now())
	bucketStart := clock.Now().Truncate(testBucketSize).UnixNano()
	// Another zone's record says nothing about whether this zone has written
	// its own record for that bucket, so it must not hold ours back.
	mergeRecord(s, "zone2", 0x1, clock.Now(), 600, 1, 1)
	require.Zero(t, trackedStream(t, s, 0x1).lastProducedBucket)

	clock.Advance(testBucketSize)
	_, toProduce := pushWithRecords(t, s, 0x1, 600, clock.Now())
	require.Len(t, toProduce, 1)
	require.Equal(t, bucketStart, toProduce[0].ShardRateBucket.BucketStart)
}

func TestStreamShardStore_MergeKeepsTheFresherFootprint(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	track(t, s, streamShardUsage{
		hash:          0x1,
		shardCount:    3,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 3, clock.Now().UnixNano()),
	}, clock.Now())

	// A record older than what we know about the stream contributes its rate
	// bucket but must not shrink the footprint back to one shard.
	mergeRecord(s, "zone2", 0x1, clock.Now().Add(-testBucketSize), 600, 1, 1)
	stream := trackedStream(t, s, 0x1)
	require.Equal(t, uint32(3), stream.shardCount)
	require.Equal(t, uint64(3), stream.slots)
	require.NotEmpty(t, stream.remoteBuckets["zone2"])
}

func TestStreamShardStore_MergeIgnoresRecordsOutsideTheRateWindow(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	mergeRecord(s, "zone2", 0x1, clock.Now().Add(-testRateWindow-testBucketSize), 600, 1, 1)
	require.Equal(t, 0, countTrackedStreams(s))
}

func TestStreamShardStore_Evict(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	push(t, s, 0x1, 1, clock.Now())

	require.Empty(t, s.Evict())
	require.Equal(t, 1, countTrackedStreams(s))

	clock.Advance(testActiveWindow + time.Second)
	require.Equal(t, map[string]int{"test": 1}, s.Evict())
	require.Equal(t, 0, countTrackedStreams(s))
}

func TestStreamShardStore_EvictTrimsExpiredShards(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	t0 := clock.Now()
	stream := streamShardUsage{
		hash:          0x1,
		shardCount:    4,
		lastSeenAt:    t0.UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 4, t0.UnixNano()),
	}
	s.updateRateBucket(&stream, 1, t0)
	track(t, s, stream, t0)

	// The stream's rate no longer justifies four shards, so from here on only
	// its first shard keeps being covered.
	clock.Advance(time.Second)
	require.Equal(t, uint32(1), push(t, s, 0x1, 1, clock.Now()).Shards)

	// The stream itself survives eviction, but the three shards last covered
	// before the active window are trimmed away.
	clock.Advance(testActiveWindow)
	push(t, s, 0x1, 1, clock.Now())
	require.Empty(t, s.Evict())
	s.withLock("test", func(i int) {
		bucket := s.stripes[i]["test"][0][noPolicy]
		require.Len(t, bucket.streams[0x1].shardLastUsed, 1)
		require.Equal(t, uint64(1), bucket.slots)
	})
}

func TestStreamShardStore_EvictPartitions(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	push(t, s, 0x1, 1, clock.Now())

	s.EvictPartitions([]int32{1})
	require.Equal(t, 1, countTrackedStreams(s))

	s.EvictPartitions([]int32{0})
	require.Equal(t, 0, countTrackedStreams(s))
	// The tenant itself is dropped once it has no partitions left.
	s.withLock("test", func(i int) {
		require.NotContains(t, s.stripes[i], "test")
	})
}

func TestStreamShardStore_Collect(t *testing.T) {
	s, clock := newTestStreamShardStore(t, 0, "1KB")
	// One unsharded stream and one stream holding three shards: four physical
	// streams in total, three of which belong to a sharded stream.
	track(t, s, streamShardUsage{
		hash:          0x1,
		shardCount:    1,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 1, clock.Now().UnixNano()),
	}, clock.Now())
	track(t, s, streamShardUsage{
		hash:          0x2,
		shardCount:    3,
		lastSeenAt:    clock.Now().UnixNano(),
		shardLastUsed: refreshLiveShards(nil, 3, clock.Now().UnixNano()),
	}, clock.Now())

	require.NoError(t, testutil.CollectAndCompare(s, strings.NewReader(`
# HELP loki_ingest_limits_stream_shard_tracked_streams The current number of logical (pre-shard) streams tracked for stream sharding per tenant.
# TYPE loki_ingest_limits_stream_shard_tracked_streams gauge
loki_ingest_limits_stream_shard_tracked_streams{tenant="test"} 2
# HELP loki_ingest_limits_stream_shard_total_shards The current number of physical streams that belong to a sharded stream per tenant. Subtracting this from loki_ingest_limits_stream_shard_total_streams gives the number of unsharded streams.
# TYPE loki_ingest_limits_stream_shard_total_shards gauge
loki_ingest_limits_stream_shard_total_shards{tenant="test"} 3
# HELP loki_ingest_limits_stream_shard_total_streams The current number of physical streams (unsharded streams plus the shards of sharded streams) per tenant implied by the granted shard counts. Compare against loki_ingester_memory_streams for the physical stream count the ingesters see.
# TYPE loki_ingest_limits_stream_shard_total_streams gauge
loki_ingest_limits_stream_shard_total_streams{tenant="test"} 4
`),
		"loki_ingest_limits_stream_shard_tracked_streams",
		"loki_ingest_limits_stream_shard_total_streams",
		"loki_ingest_limits_stream_shard_total_shards",
	))
}

func TestStreamShardUsage_CurrentRate(t *testing.T) {
	now := time.Unix(1000, 0)
	stream := streamShardUsage{
		rateBuckets: []shardRateBucket{
			{timestamp: now.Add(-30 * time.Second).UnixNano(), size: 600, pushes: 3},
			// Outside the rate window: a slot the ring buffer has not reused yet
			// still holds stale data, which must not be counted.
			{timestamp: now.Add(-2 * time.Minute).UnixNano(), size: 6000, pushes: 30},
			{},
		},
	}
	bytesRate, pushRate := stream.currentRate(now, time.Minute)
	require.Equal(t, uint64(10), bytesRate)
	require.InDelta(t, 0.05, pushRate, 0.001)

	bytesRate, pushRate = stream.currentRate(now, 0)
	require.Zero(t, bytesRate)
	require.Zero(t, pushRate)
}

func TestStreamShardUsage_CurrentRate_AllZones(t *testing.T) {
	now := time.Unix(1000, 0)
	stream := streamShardUsage{
		rateBuckets: []shardRateBucket{
			{timestamp: now.Add(-30 * time.Second).UnixNano(), size: 600, pushes: 3},
		},
		remoteBuckets: map[string][]shardRateBucket{
			"zone2": {
				{timestamp: now.Add(-30 * time.Second).UnixNano(), size: 300, pushes: 2},
				{timestamp: now.Add(-2 * time.Minute).UnixNano(), size: 6000, pushes: 30},
			},
			"zone3": {
				{timestamp: now.Add(-10 * time.Second).UnixNano(), size: 300, pushes: 1},
			},
		},
	}
	// 1200 bytes and 6 pushes over a minute, the stale bucket excluded.
	bytesRate, pushRate := stream.currentRate(now, time.Minute)
	require.Equal(t, uint64(20), bytesRate)
	require.InDelta(t, 0.1, pushRate, 0.001)
}

func TestCeilDivU32(t *testing.T) {
	require.Equal(t, uint32(1), ceilDivU32(1, 0))
	require.Equal(t, uint32(0), ceilDivU32(0, 10))
	require.Equal(t, uint32(1), ceilDivU32(1, 10))
	require.Equal(t, uint32(2), ceilDivU32(11, 10))
	// Clamped rather than wrapped to a small shard count.
	require.Equal(t, uint32(math.MaxUint32), ceilDivU32(math.MaxUint64, 1))
}

// The budget check must not depend on the number of streams already tracked
// in the bucket: a tenant near its stream limit pushes into a bucket holding
// tens of thousands of streams.
func BenchmarkStreamShardStore_CheckAndShard(b *testing.B) {
	for _, streamsInBucket := range []int{100, 10_000, 100_000} {
		b.Run(fmt.Sprintf("%d_streams_in_bucket", streamsInBucket), func(b *testing.B) {
			limits := &mockLimits{
				MaxGlobalStreams:   2 * streamsInBucket,
				ShardStreamsConfig: shardstreams.Config{Enabled: true},
			}
			require.NoError(b, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))
			s, err := newStreamShardStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, testZone, true, limits, prometheus.NewRegistry())
			require.NoError(b, err)

			now := time.Now()
			metadata := make([]*proto.StreamMetadata, 0, streamsInBucket)
			for i := range streamsInBucket {
				metadata = append(metadata, &proto.StreamMetadata{
					StreamHash: uint64(i),
					TotalSize:  1024,
				})
			}
			results, _ := s.checkAndShard(b.Context(), "test", metadata, now)
			require.Len(b, results, streamsInBucket)

			one := metadata[:1]
			b.ResetTimer()
			for i := range b.N {
				s.checkAndShard(b.Context(), "test", one, now.Add(time.Duration(i)*time.Second))
			}
		})
	}
}

func countTrackedStreams(s *streamShardStore) int {
	var n int
	s.forEachRLock(func(i int) {
		for _, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, bucket := range policies {
					n += len(bucket.streams)
				}
			}
		}
	})
	return n
}
