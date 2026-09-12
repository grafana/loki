package limits

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// warmRateBuckets returns a rateBuckets slice with the current bucket
// already populated (non-zero timestamp, zero size), so a seeded stream is
// treated as "warm" (rateBucketsCold returns false) and the next
// checkAndShard call computes its rate from that call's own push, instead
// of holding steady as if this instance had just taken ownership of the
// stream's partition with no live traffic observed yet.
func warmRateBuckets(numBuckets int, bucketSize time.Duration, now time.Time) []rateBucket {
	buckets := make([]rateBucket, numBuckets)
	bucketNum := now.UnixNano() / int64(bucketSize)
	idx := int(bucketNum % int64(numBuckets))
	buckets[idx] = rateBucket{timestamp: now.Truncate(bucketSize).UnixNano()}
	return buckets
}

// newShardingEnabledMockLimits returns a mockLimits with sharding enabled
// and a permissive desired_rate. mockLimits{}'s zero value has sharding
// *disabled* (Go's zero value for shardstreams.Config.Enabled is false),
// which would make every checkAndShard call collapse to 1 shard -- easy to
// trip over when a test only cares about, e.g., replay/eviction behavior
// and doesn't intend to exercise the sharding-disabled code path at all.
func newShardingEnabledMockLimits() *mockLimits {
	cfg := shardstreams.Config{Enabled: true}
	cfg.DesiredRate.Set("1B") //nolint:errcheck
	return &mockLimits{ShardStreamsConfig: cfg}
}

func newTestStreamShardStore(t *testing.T, maxGlobalStreams int, desiredRate string, streamsUsed func(string, int32, string) uint64) *streamShardStore {
	t.Helper()
	cfg := shardstreams.Config{Enabled: true}
	require.NoError(t, cfg.DesiredRate.Set(desiredRate))
	if streamsUsed == nil {
		streamsUsed = func(string, int32, string) uint64 { return 0 }
	}
	l := &mockLimits{
		MaxGlobalStreams:   maxGlobalStreams,
		ShardStreamsConfig: cfg,
	}
	// numPartitions=1 so every test stream hash lands in partition 0,
	// keeping test setup simple.
	s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, streamsUsed, prometheus.NewRegistry())
	require.NoError(t, err)
	return s
}

func TestStreamShardStore_CheckAndShard(t *testing.T) {
	// seedStream pre-populates a stream via setForTests before the push
	// being asserted on. warm controls whether it has an already-populated
	// rate bucket (rateBucketsCold == false, so the push's rate is computed
	// normally) or not (cold: the shard count is held steady instead).
	type seedStream struct {
		hash       uint64
		shardCount uint32
		warm       bool
	}
	tests := []struct {
		name                     string
		maxGlobalStreams         int
		shardStreamsEnabled      bool
		streamsUsed              func(tenant string, partition int32, policyBucket string) uint64
		seed                     []seedStream
		advanceBeforeCall        time.Duration
		pushHash                 uint64
		pushTotalSize            uint64
		wantShards               uint32
		wantShardDecisionContext Reason
		wantRejectReason         string
	}{
		{
			// A huge first push must still never shard on the very first
			// sighting: there is no rate history yet to justify more than 1
			// shard.
			name:                     "new stream starts at one shard",
			maxGlobalStreams:         100,
			shardStreamsEnabled:      true,
			pushHash:                 1,
			pushTotalSize:            1_000_000,
			wantShards:               1,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// usageStore (via streamsUsed) reports the bucket is already
			// fully at the tenant's real budget.
			name:                "new stream rejected when no room",
			maxGlobalStreams:    5,
			shardStreamsEnabled: true,
			streamsUsed:         func(string, int32, string) uint64 { return 5 },
			pushHash:            1,
			pushTotalSize:       100,
			wantShards:          0,
			wantRejectReason:    ReasonMaxStreams.String(),
		},
		{
			// Three other streams occupy 6 slots total (2 each); the target
			// stream currently holds 1 shard and is warm, so its rate is
			// computed (rateWindow=5m=300s, desiredRate=1B/s: TotalSize=1500
			// -> rate=5B/s -> desired=5), then capped to fit the remaining
			// budget: room = maxStreams(9) - others(6) = 3; granted =
			// max(current=1, min(desired=5, room=3)) = 3.
			name:                "growth capped to fit budget",
			maxGlobalStreams:    9,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 100, shardCount: 2},
				{hash: 101, shardCount: 2},
				{hash: 102, shardCount: 2},
				{hash: 1, shardCount: 1, warm: true},
			},
			pushHash:                 1,
			pushTotalSize:            1500,
			wantShards:               3,
			wantShardDecisionContext: ReasonStreamShardsCapped,
		},
		{
			// The target stream already holds 5 shards and is warm; another
			// stream has grown to consume the rest of the budget. The
			// target's rate still justifies growth to 6 shards (rate=6B/s):
			// room = maxStreams(10) - others(5) = 5; desired(6) >
			// current(5), so granted = max(current=5, min(6,5)) = 5 -- held
			// steady, neither force-shrunk to make room for the other
			// stream nor grown further since there's no room left to grow
			// into.
			name:                "never force shrink from others' growth",
			maxGlobalStreams:    10,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 5, warm: true},
				{hash: 2, shardCount: 5},
			},
			pushHash:                 1,
			pushTotalSize:            1800,
			wantShards:               5,
			wantShardDecisionContext: ReasonStreamShardsCapped,
		},
		{
			// The target stream currently holds 8 shards and is warm;
			// another stream consumes the entire remaining budget (room=0:
			// maxStreams(10) - others(2) - self(8) = 0). The target's rate
			// has dropped to justify only 3 shards (rate=3B/s): a
			// self-inflicted shrink is never capped by room, since it only
			// frees capacity.
			name:                "self shrink ignores room",
			maxGlobalStreams:    10,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 8, warm: true},
				{hash: 2, shardCount: 2},
			},
			pushHash:                 1,
			pushTotalSize:            900,
			wantShards:               3,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// A push that would otherwise justify many shards must still be
			// held at 1 while sharding is disabled for this policy.
			name:                "sharding disabled never exceeds one shard",
			maxGlobalStreams:    100,
			shardStreamsEnabled: false,
			seed: []seedStream{
				{hash: 1, shardCount: 1},
			},
			pushHash:                 1,
			pushTotalSize:            1_000_000,
			wantShards:               1,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// Simulates a stream restored from a replayed snapshot (e.g.
			// after a partition rebalance): it has a durable shardCount,
			// but no rate history has been observed yet by this instance
			// (not warm). The very next live push, even a tiny one, must
			// not collapse the shard count to whatever a fresh,
			// mostly-empty rate bucket implies.
			name:                "cold rate buckets hold steady",
			maxGlobalStreams:    100,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 7},
			},
			pushHash:                 1,
			pushTotalSize:            1,
			wantShards:               7,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// Advancing past the active window means the stream is now
			// expired and must be treated as brand new (no shard-count
			// memory carried over).
			name:                "expired stream resets to one shard",
			maxGlobalStreams:    100,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 7},
			},
			advanceBeforeCall:        15*time.Minute + time.Second,
			pushHash:                 1,
			pushTotalSize:            1_000_000,
			wantShards:               1,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// Regression guard: a reactivating (expired-then-reseen) stream
			// must not have its own stale shard allocation double-counted
			// against itself when room() decides whether it can reset to 1
			// shard.
			name:                "reactivating stream's own stale allocation doesn't count against itself",
			maxGlobalStreams:    5,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 5},
			},
			advanceBeforeCall:        15*time.Minute + time.Second,
			pushHash:                 1,
			pushTotalSize:            100,
			wantShards:               1,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			name:                "unlimited when maxStreams is zero",
			maxGlobalStreams:    0,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 1, warm: true},
			},
			pushHash:                 1,
			pushTotalSize:            1500, // rate=5B/s -> desired=5
			wantShards:               5,
			wantShardDecisionContext: ReasonUnknown,
		},
		{
			// Regression guard: a tracked stream holding 5 shards must
			// consume 5 budget slots, not 1. maxGlobalStreams here exactly
			// equals that shard count, so a new stream must be rejected
			// outright.
			name:                "existing stream's granted shards fully consume the budget",
			maxGlobalStreams:    5,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 100, shardCount: 5},
			},
			pushHash:         1,
			pushTotalSize:    100,
			wantShards:       0,
			wantRejectReason: ReasonMaxStreams.String(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := shardstreams.Config{Enabled: test.shardStreamsEnabled}
				require.NoError(t, cfg.DesiredRate.Set("1B"))
				streamsUsed := test.streamsUsed
				if streamsUsed == nil {
					streamsUsed = func(string, int32, string) uint64 { return 0 }
				}
				l := &mockLimits{MaxGlobalStreams: test.maxGlobalStreams, ShardStreamsConfig: cfg}
				// numPartitions=1 so every test stream hash lands in partition 0.
				s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, streamsUsed, prometheus.NewRegistry())
				require.NoError(t, err)
				now := time.Now()

				for _, seed := range test.seed {
					usage := streamShardUsage{hash: seed.hash, lastSeenAt: now.UnixNano(), shardCount: seed.shardCount, policy: noPolicy}
					if seed.warm {
						usage.rateBuckets = warmRateBuckets(s.numBuckets, s.bucketSize, now)
					}
					s.setForTests("tenant1", 0, noPolicy, usage)
				}

				if test.advanceBeforeCall > 0 {
					time.Sleep(test.advanceBeforeCall)
				}

				results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
					{StreamHash: test.pushHash, TotalSize: test.pushTotalSize},
				}, time.Now())
				require.Len(t, results, 1)
				require.Equal(t, test.wantShards, results[0].Shards)
				require.Equal(t, uint32(test.wantShardDecisionContext), results[0].ShardDecisionContext)
				require.Equal(t, test.wantRejectReason, results[0].RejectReason)
			})
		})
	}
}

func TestStreamShardStore_CheckAndShard_NewStreamSeedsItsFirstRateBucket(t *testing.T) {
	// Regression guard: a brand-new stream's first push must still start at
	// 1 shard (no rate history to justify more), but its bytes must be
	// recorded into the rate buckets rather than dropped -- otherwise only
	// the SECOND push would initialize the buckets, and a real rate
	// computation wouldn't happen until the third push, permanently biasing
	// bursty/infrequent streams low.
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 100, "1B", nil)

		// First push: brand new stream. Must start at 1 shard.
		results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(1), results[0].Shards)

		// Second push, same instant (same 1-minute bucket, per
		// newTestStreamShardStore's bucketSize): if the first push's bytes
		// were recorded, the rate is already computable from BOTH pushes
		// combined -- 3000 bytes / 300s rate window = 10 B/s, desired 10
		// shards at desiredRate=1B/s -- instead of being held steady at 1
		// while "cold" (which would only stop being true on the THIRD push
		// without this fix).
		results = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(10), results[0].Shards)
	})
}

// collectStreamShardStoreGauges reads streamShardStore's own Collector
// output directly (bypassing a full registry Gather), returning the
// per-tenant tracked-streams/allocated-shards gauge values it currently
// reports.
func collectStreamShardStoreGauges(t *testing.T, s *streamShardStore, tenant string) (trackedStreams, allocatedShards float64) {
	t.Helper()
	ch := make(chan prometheus.Metric, 16)
	go func() {
		s.Collect(ch)
		close(ch)
	}()
	for m := range ch {
		var pb dto.Metric
		require.NoError(t, m.Write(&pb))
		require.Len(t, pb.Label, 1)
		require.Equal(t, "tenant", pb.Label[0].GetName())
		if pb.Label[0].GetValue() != tenant {
			continue
		}
		switch m.Desc() {
		case streamShardTrackedStreamsDesc:
			trackedStreams = pb.GetGauge().GetValue()
		case streamShardAllocatedShardsDesc:
			allocatedShards = pb.GetGauge().GetValue()
		}
	}
	return trackedStreams, allocatedShards
}

func TestStreamShardStore_Collect_ReflectsCurrentStateNotCumulative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 100, "1B", nil)

		// Nothing tracked yet.
		trackedStreams, allocatedShards := collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(0), trackedStreams)
		require.Equal(t, float64(0), allocatedShards)

		// First push: brand new stream, always starts at 1 shard.
		results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(1), results[0].Shards)

		trackedStreams, allocatedShards = collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(1), trackedStreams)
		require.Equal(t, float64(1), allocatedShards)

		// Second push, same stream: its rate now justifies 10 shards. Collect
		// must reflect the CURRENT allocation (10), not accumulate the two
		// pushes' shard counts (1 + 10 = 11) -- these are lazily-aggregated
		// gauges over live state, not counters.
		results = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(10), results[0].Shards)

		trackedStreams, allocatedShards = collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(1), trackedStreams, "still exactly one distinct logical stream")
		require.Equal(t, float64(10), allocatedShards, "reflects the current allocation, not the sum across pushes")
	})
}

func TestStreamShardStore_Evict_FreesAllOfAStreamsSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 5, "1B", nil)
		now := time.Now()
		s.setForTests("tenant1", 0, noPolicy, streamShardUsage{
			hash: 1, lastSeenAt: now.UnixNano(), shardCount: 5, policy: noPolicy,
		})
		time.Sleep(15*time.Minute + time.Second)
		evicted := s.Evict()
		require.Equal(t, 1, evicted["tenant1"])
		// All 5 of the evicted stream's slots must be freed: a brand-new stream
		// should now fit within the (otherwise fully consumed) budget.
		results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 2, TotalSize: 1},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(1), results[0].Shards)
		require.Empty(t, results[0].RejectReason)
	})
}

func TestStreamShardStore_EvictPartitions(t *testing.T) {
	s := newTestStreamShardStore(t, 5, "1B", nil)
	now := time.Now()
	s.setForTests("tenant1", 0, noPolicy, streamShardUsage{
		hash: 1, lastSeenAt: now.UnixNano(), shardCount: 5, policy: noPolicy,
	})
	s.EvictPartitions([]int32{0})
	// Slots must be freed for the evicted partition, same as a normal Evict.
	results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
		{StreamHash: 2, TotalSize: 1},
	}, now)
	require.Len(t, results, 1)
	require.Equal(t, uint32(1), results[0].Shards)
	require.Empty(t, results[0].RejectReason)
}
