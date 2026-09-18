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

// seedRateBuckets is like warmRateBuckets but also records `size` bytes in the
// current bucket, so currentRate over the whole window is size/rateWindow.
func seedRateBuckets(numBuckets int, bucketSize time.Duration, now time.Time, size uint64) []rateBucket {
	buckets := warmRateBuckets(numBuckets, bucketSize, now)
	idx := int((now.UnixNano() / int64(bucketSize)) % int64(numBuckets))
	buckets[idx].size = size
	return buckets
}

func newTestStreamShardStore(t *testing.T, maxGlobalStreams int, desiredRate string) *streamShardStore {
	t.Helper()
	cfg := shardstreams.Config{Enabled: true}
	require.NoError(t, cfg.DesiredRate.Set(desiredRate))
	l := &mockLimits{
		MaxGlobalStreams:   maxGlobalStreams,
		ShardStreamsConfig: cfg,
	}
	// numPartitions=1 so every test stream hash lands in partition 0,
	// keeping test setup simple.
	s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, prometheus.NewRegistry())
	require.NoError(t, err)
	return s
}

// seedStreamShardStore directly seeds a stream's state, to set up
// capacity/room scenarios precisely without indirectly driving them through
// rate-bucket math. Not goroutine-safe.
//
// When the caller sets shardCount but no explicit shardLastUsed, a matching
// live-shard footprint is synthesized (shardCount shards, all live as of
// lastSeenAt), so a stream seeded at shardCount N consumes N budget slots --
// the behavior these scenarios relied on before per-shard tracking existed.
func seedStreamShardStore(s *streamShardStore, tenant string, partition int32, policyBucket string, stream streamShardUsage) {
	if stream.shardLastUsed == nil && stream.shardCount > 0 {
		stream.shardLastUsed = make([]int64, stream.shardCount)
		for i := range stream.shardLastUsed {
			stream.shardLastUsed[i] = stream.lastSeenAt
		}
	}
	s.withLock(tenant, func(i int) {
		streams := s.checkInitMap(i, tenant, partition, policyBucket)
		streams[stream.hash] = stream
	})
}

func TestStreamShardStore_CheckAndShard(t *testing.T) {
	// seedStream pre-populates a stream via seedStreamShardStore before the push
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
			// Five other single-shard streams already fill the whole budget
			// (5 slots), so a brand-new stream has no room and is rejected.
			// Distinct from "existing stream's granted shards fully consume
			// the budget", which fills the same budget with one 5-shard stream.
			name:                "new stream rejected when no room",
			maxGlobalStreams:    5,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 100, shardCount: 1},
				{hash: 101, shardCount: 1},
				{hash: 102, shardCount: 1},
				{hash: 103, shardCount: 1},
				{hash: 104, shardCount: 1},
			},
			pushHash:         1,
			pushTotalSize:    100,
			wantShards:       0,
			wantRejectReason: ReasonMaxStreams.String(),
		},
		{
			// Three other streams hold 6 live shards total (2 each), so the
			// budget for the target is maxStreams(9) - others(6) = 3. The warm
			// target's rate (1500B / 300s = 5B/s) plus the amortized current
			// push (1500 * pushRate(1/300s) ~= 5) gives desired ~= 10, which is
			// capped to the budget of 3.
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
			// The target stream already holds 5 live shards and is warm;
			// another stream holds the other 5, so the budget for the target is
			// maxStreams(10) - others(5) = 5. Its rate (1800B/300s = 6B/s) plus
			// the amortized push (~6) gives desired ~= 12, but it is capped to
			// the budget of 5: it keeps its own 5 live shards and can't grow
			// past the budget, and is never force-shrunk below them.
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
			// The target holds 8 live shards and is warm; another stream holds
			// 2, so the tenant is at its budget of 10 live shards. The target's
			// budget excludes its own shards: maxStreams(10) - others(2) = 8.
			// Its rate has dropped (900B/300s = 3B/s) and with the amortized
			// push (~3) desired ~= 6, so it shrinks from 8 to 6 -- shrinking is
			// never blocked by a full budget, since it only frees capacity.
			name:                "self shrink ignores room",
			maxGlobalStreams:    10,
			shardStreamsEnabled: true,
			seed: []seedStream{
				{hash: 1, shardCount: 8, warm: true},
				{hash: 2, shardCount: 2},
			},
			pushHash:                 1,
			pushTotalSize:            900,
			wantShards:               6,
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
			pushTotalSize:            1500, // rate 5B/s + amortized push ~5 -> desired ~10, uncapped
			wantShards:               10,
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
				l := &mockLimits{MaxGlobalStreams: test.maxGlobalStreams, ShardStreamsConfig: cfg}
				// numPartitions=1 so every test stream hash lands in partition 0.
				s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, prometheus.NewRegistry())
				require.NoError(t, err)
				now := time.Now()

				for _, seed := range test.seed {
					usage := streamShardUsage{hash: seed.hash, lastSeenAt: now.UnixNano(), shardCount: seed.shardCount, policy: noPolicy}
					if seed.warm {
						usage.rateBuckets = warmRateBuckets(s.numBuckets, s.bucketSize, now)
					}
					seedStreamShardStore(s, "tenant1", 0, noPolicy, usage)
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

	// These scenarios need multiple sequential pushes or gauge inspection,
	// which the single-push table above can't express, so they run as
	// subtests of this test rather than table rows.
	t.Run("new stream seeds its first rate bucket", func(t *testing.T) {
		// Regression guard: a brand-new stream's first push must still start at
		// 1 shard (no rate history to justify more), but its bytes must be
		// recorded into the rate buckets rather than dropped -- otherwise only
		// the SECOND push would initialize the buckets, and a real rate
		// computation wouldn't happen until the third push, permanently biasing
		// bursty/infrequent streams low.
		synctest.Test(t, func(t *testing.T) {
			s := newTestStreamShardStore(t, 100, "1B")

			// First push: brand new stream. Must start at 1 shard.
			results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
				{StreamHash: 1, TotalSize: 1500},
			}, time.Now())
			require.Len(t, results, 1)
			require.Equal(t, uint32(1), results[0].Shards)

			// Second push, same instant (same 1-minute bucket, per
			// newTestStreamShardStore's bucketSize): if the first push's bytes
			// were recorded, the rate is already computable from BOTH pushes
			// combined -- 3000 bytes / 300s rate window = 10 B/s -- instead of
			// being held steady at 1 while "cold" (which would only stop being
			// true on the THIRD push without this fix). With the push-size
			// amortization (rate 10 + 1500*pushRate(2/300) ~= 10) desired ~= 20.
			results = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
				{StreamHash: 1, TotalSize: 1500},
			}, time.Now())
			require.Len(t, results, 1)
			require.Equal(t, uint32(20), results[0].Shards)
		})
	})

	t.Run("shorter per-tenant rate window shards more", func(t *testing.T) {
		// The same traffic yields more shards under a shorter rate-averaging
		// window, because the bytes are divided by a smaller window. Two 1500B
		// pushes in the same 1-minute bucket = 3000B: over the default 5m (300s)
		// window that is 10 B/s (desired ~20 with the push-size amortization,
		// see the subtest above), but over a 1m (60s) per-tenant window it is
		// 50 B/s -> desired ~100 (rate 50 + 1500*pushRate(2/60) ~= 50).
		synctest.Test(t, func(t *testing.T) {
			cfg := shardstreams.Config{Enabled: true, LimitsServiceStreamShardingRateWindow: time.Minute}
			require.NoError(t, cfg.DesiredRate.Set("1B"))
			l := &mockLimits{MaxGlobalStreams: 1000, ShardStreamsConfig: cfg}
			s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, prometheus.NewRegistry())
			require.NoError(t, err)

			// First push initializes the buckets (held at 1 shard while cold).
			results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
				{StreamHash: 1, TotalSize: 1500},
			}, time.Now())
			require.Len(t, results, 1)
			require.Equal(t, uint32(1), results[0].Shards)

			// Second push, same 1-minute bucket: 3000B / 60s window = 50 B/s,
			// plus the amortized push -> desired ~= 100.
			results = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
				{StreamHash: 1, TotalSize: 1500},
			}, time.Now())
			require.Len(t, results, 1)
			require.Equal(t, uint32(100), results[0].Shards)
		})
	})

	t.Run("disabled stream is untracked", func(t *testing.T) {
		// A stream that was tracked while its policy had sharding enabled must be
		// dropped from the store once sharding is disabled for it, so its stale
		// shards stop consuming budget and stop showing in the gauges.
		synctest.Test(t, func(t *testing.T) {
			cfg := shardstreams.Config{Enabled: false}
			require.NoError(t, cfg.DesiredRate.Set("1B"))
			l := &mockLimits{MaxGlobalStreams: 100, ShardStreamsConfig: cfg}
			s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, prometheus.NewRegistry())
			require.NoError(t, err)

			seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
				hash: 1, lastSeenAt: time.Now().UnixNano(), shardCount: 5, policy: noPolicy,
			})
			tracked, total, _ := collectStreamShardStoreGauges(t, s, "tenant1")
			require.Equal(t, float64(1), tracked)
			require.Equal(t, float64(5), total)

			results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
				{StreamHash: 1, TotalSize: 100},
			}, time.Now())
			require.Len(t, results, 1)
			require.Equal(t, uint32(1), results[0].Shards)

			tracked, total, _ = collectStreamShardStoreGauges(t, s, "tenant1")
			require.Equal(t, float64(0), tracked, "disabled stream must be untracked")
			require.Equal(t, float64(0), total)
		})
	})
}

func TestClampShardRateWindow(t *testing.T) {
	const bucketSize = time.Minute
	const rateWindow = 5 * time.Minute
	for _, tc := range []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{"zero falls back to store default", 0, rateWindow},
		{"negative falls back to store default", -time.Second, rateWindow},
		{"in range kept as-is", 2 * time.Minute, 2 * time.Minute},
		{"below one bucket clamped up to bucket size", 10 * time.Second, bucketSize},
		{"above window clamped down to window", 10 * time.Minute, rateWindow},
		{"equal to bucket size kept", bucketSize, bucketSize},
		{"equal to window kept", rateWindow, rateWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clampShardRateWindow(tc.configured, bucketSize, rateWindow))
		})
	}
}

// collectStreamShardStoreGauges reads streamShardStore's own Collector
// output directly (bypassing a full registry Gather), returning the
// per-tenant tracked-streams / total-streams / total-shards gauge values it
// currently reports.
func collectStreamShardStoreGauges(t *testing.T, s *streamShardStore, tenant string) (trackedStreams, totalStreams, totalShards float64) {
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
		case streamShardTotalStreamsDesc:
			totalStreams = pb.GetGauge().GetValue()
		case streamShardTotalShardsDesc:
			totalShards = pb.GetGauge().GetValue()
		}
	}
	return trackedStreams, totalStreams, totalShards
}

func TestStreamShardStore_Collect_ReflectsCurrentStateNotCumulative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 100, "1B")

		// Nothing tracked yet.
		trackedStreams, totalStreams, totalShards := collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(0), trackedStreams)
		require.Equal(t, float64(0), totalStreams)
		require.Equal(t, float64(0), totalShards)

		// First push: brand new stream, always starts at 1 shard. Unsharded
		// (shardCount 1): it counts as 1 logical and 1 physical stream, but
		// contributes 0 to total_shards.
		results := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(1), results[0].Shards)

		trackedStreams, totalStreams, totalShards = collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(1), trackedStreams)
		require.Equal(t, float64(1), totalStreams)
		require.Equal(t, float64(0), totalShards, "an unsharded stream is not a shard")

		// Second push, same stream: its rate (3000B/300s = 10 B/s) plus the
		// amortized push justifies 20 shards. Collect must reflect the
		// live-shard footprint (20 here, since all 20 were just used), not
		// accumulate the two pushes' shard counts -- these are lazily-aggregated
		// gauges over live state, not counters. Now sharded (20 live shards):
		// still 1 logical stream, 20 physical streams, all 20 of them shards.
		// (How the footprint holds after the recommendation later shrinks is
		// covered by TestStreamShardStore_ShardFootprintHoldsThenExpires.)
		results = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{
			{StreamHash: 1, TotalSize: 1500},
		}, time.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(20), results[0].Shards)

		trackedStreams, totalStreams, totalShards = collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(1), trackedStreams, "still exactly one distinct logical stream")
		require.Equal(t, float64(20), totalStreams, "reflects the live-shard footprint, not the sum across pushes")
		require.Equal(t, float64(20), totalShards, "all 20 physical streams are shards of the sharded stream")
	})
}

func TestStreamShardStore_ShardFootprintHoldsThenExpires(t *testing.T) {
	// The recommendation shrinks with the rate, but the shards already created
	// stay accounted for (like the ingesters' sub-streams persist until their
	// chunks flush) until they individually expire out of the active window.
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 100, "1B")

		// Grow to 20 shards: two 1500B pushes in the same bucket = 3000B / 300s
		// rate window = 10 B/s, plus the amortized push -> desired ~20.
		s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 1500}}, time.Now())
		res := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 1500}}, time.Now())
		require.Equal(t, uint32(20), res[0].Shards)
		_, _, totalShards := collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(20), totalShards)

		// Advance past the 5m rate window so the burst's bytes age out, then push
		// a trickle: the rate now justifies only 1 shard, so the recommendation
		// shrinks -- but the other 19 shards are still live (well within the 15m
		// active window), so the accounted footprint holds at 20.
		time.Sleep(6 * time.Minute)
		res = s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 60}}, time.Now())
		require.Equal(t, uint32(1), res[0].Shards, "recommendation shrinks with the rate")
		_, _, totalShards = collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(20), totalShards, "footprint holds while the shards are still live")

		// Advance until the burst is older than the active window but the trickle
		// isn't: shards 1..19 (last used at the burst) expire, only shard 0 (kept
		// alive by the trickle) survives, so the footprint decays to 1.
		time.Sleep(10 * time.Minute) // burst+16m, trickle+10m; cutoff falls between them
		_, totalStreams, totalShards := collectStreamShardStoreGauges(t, s, "tenant1")
		require.Equal(t, float64(1), totalStreams, "expired shards drop out of the footprint")
		require.Equal(t, float64(0), totalShards, "no longer a sharded stream")
	})
}

func TestStreamShardStore_RegrowWithinLiveFootprintNotCapped(t *testing.T) {
	// A stream re-growing into shards it still holds live is not budget-capped,
	// even when the tenant budget is otherwise full -- those shards are already
	// counted, so no new budget is consumed.
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 10, "1B") // budget = 10 shards
		now := time.Now()

		// Stream 1 holds 6 live shards; its rate still justifies 6 (1800B/300s).
		seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
			hash: 1, lastSeenAt: now.UnixNano(), shardCount: 6, policy: noPolicy,
			rateBuckets: seedRateBuckets(s.numBuckets, s.bucketSize, now, 1800),
		})
		// Stream 2 holds 4 live shards, filling the rest of the budget (6+4=10).
		seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
			hash: 2, lastSeenAt: now.UnixNano(), shardCount: 4, policy: noPolicy,
		})

		// Stream 1 pushes (adding no new bytes): rate wants 6, and all 6 are
		// already its own live shards, so it is granted 6 and NOT capped even
		// though the tenant budget is full.
		res := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 0}}, now)
		require.Len(t, res, 1)
		require.Equal(t, uint32(6), res[0].Shards)
		require.Equal(t, uint32(ReasonUnknown), res[0].ShardDecisionContext, "reusing its own live shards is not a cap")
	})
}

func TestStreamShardStore_PushSizeAmortization(t *testing.T) {
	// Mirrors the local rate store (shardCountFor): the shard count is driven
	// by the windowed rate PLUS the current push amortized by its push rate, so
	// a large push nudges the count up before it's reflected in the sustained
	// rate. Chosen so the rate and push terms differ (not the coincidental 2x
	// of the same-push tests).
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 1000, "1B") // desiredRate 1 B/s, window 300s
		now := time.Now()
		// Warm stream that already accumulated 27000B this window.
		seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
			hash: 1, lastSeenAt: now.UnixNano(), shardCount: 1, policy: noPolicy,
			rateBuckets: seedRateBuckets(s.numBuckets, s.bucketSize, now, 27000),
		})
		// A 3000B push: windowed rate = (27000+3000)/300 = 100 B/s; the push is
		// amortized at pushRate 1/300 -> +3000/300 = 10, so desired = 110.
		res := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 3000}}, now)
		require.Len(t, res, 1)
		require.Equal(t, uint32(110), res[0].Shards)
		require.Equal(t, uint64(100), res[0].EvaluatedRate, "evaluatedRate reports the pre-amortization windowed rate")
	})
}

func TestStreamShardStore_UnlimitedTenantNeverCaps(t *testing.T) {
	// A tenant with no stream limit (MaxGlobalStreamsPerUser == 0) has no budget
	// ceiling: the recommendation follows the rate and is never capped. Guards
	// the maxStreams==0 path, where budget is the MaxInt64 sentinel.
	synctest.Test(t, func(t *testing.T) {
		cfg := shardstreams.Config{Enabled: true}
		require.NoError(t, cfg.DesiredRate.Set("1B"))
		l := &mockLimits{UnlimitedGlobalStreams: true, ShardStreamsConfig: cfg}
		s, err := newStreamShardStore(15*time.Minute, 5*time.Minute, time.Minute, 1, l, prometheus.NewRegistry())
		require.NoError(t, err)

		// Two 1500B pushes = 3000B / 300s = 10 B/s, plus the amortized push
		// -> desired ~20; with no limit it's granted in full, uncapped.
		s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 1500}}, time.Now())
		res := s.checkAndShard(context.Background(), "tenant1", []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 1500}}, time.Now())
		require.Len(t, res, 1)
		require.Equal(t, uint32(20), res[0].Shards)
		require.Equal(t, uint32(ReasonUnknown), res[0].ShardDecisionContext)
	})
}

func TestStreamShardStore_Evict_FreesAllOfAStreamsSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestStreamShardStore(t, 5, "1B")
		now := time.Now()
		seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
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
	s := newTestStreamShardStore(t, 5, "1B")
	now := time.Now()
	seedStreamShardStore(s, "tenant1", 0, noPolicy, streamShardUsage{
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

func TestService_CheckLimitsAndShard_UnownedPartitionStreamsGetAnExplicitNotOwnedResult(t *testing.T) {
	// Regression guard: a stream whose partition isn't owned by this
	// instance must not silently vanish from the response -- it must come
	// back with an explicit ShardDecisionContext=ReasonNotOwned entry, so the
	// frontend's fail-open handling can engage for it instead of the caller
	// getting no answer at all for that stream.
	store := newTestStreamShardStore(t, 100, "1B")
	pm, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	// No partitions assigned to pm, so every stream below is "unowned".
	s := &Service{
		cfg:              Config{NumPartitions: 1},
		partitionManager: pm,
		streamShardStore: store,
		streamShardStreamsDiscardedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_streams_discarded_total",
		}, []string{"partition"}),
	}

	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant:  "tenant1",
		Streams: []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 100}},
	})
	require.NoError(t, err)
	require.Equal(t, []*proto.StreamShardResult{{
		StreamHash:           1,
		Shards:               1,
		ShardDecisionContext: uint32(ReasonNotOwned),
	}}, resp.Results)
}
