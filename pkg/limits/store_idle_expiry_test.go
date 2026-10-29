package limits

import (
	"context"
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
	backfillPolicy = "backfill"
	testIdleExpiry = 2 * time.Minute
)

// mockLimitsWithIdleExpiry extends mockLimits to support per-policy stream
// limits and stream idle expiries.
type mockLimitsWithIdleExpiry struct {
	mockLimits
	policyLimits map[string]int
	idleExpiries map[string]time.Duration
}

func (m *mockLimitsWithIdleExpiry) PolicyMaxGlobalStreamsPerUser(_, policy string) (int, bool) {
	limit, ok := m.policyLimits[policy]
	return limit, ok
}

func (m *mockLimitsWithIdleExpiry) PolicyStreamIdleExpiry(_, policy string) (time.Duration, bool) {
	expiry, ok := m.idleExpiries[policy]
	return expiry, ok
}

// newIdleExpiryLimits returns limits where the backfill policy has its own
// stream limit and an idle expiry, and the "expiry-only" policy has an idle
// expiry but no bucket of its own.
func newIdleExpiryLimits(t *testing.T) *mockLimitsWithIdleExpiry {
	limits := &mockLimitsWithIdleExpiry{
		mockLimits: mockLimits{
			UnlimitedGlobalStreams: true,
			ShardStreamsConfig:     shardstreams.Config{Enabled: true},
		},
		policyLimits: map[string]int{backfillPolicy: 100, "other": 100},
		idleExpiries: map[string]time.Duration{backfillPolicy: testIdleExpiry, "expiry-only": testIdleExpiry},
	}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))
	return limits
}

func TestResolveIdleExpiry(t *testing.T) {
	limits := &mockLimitsWithIdleExpiry{idleExpiries: map[string]time.Duration{
		"short":    time.Minute,
		"zero":     0,
		"negative": -time.Minute,
		noPolicy:   time.Minute,
	}}
	for _, tc := range []struct {
		name     string
		policy   string
		expected time.Duration
	}{
		{name: "short", policy: "short", expected: time.Minute},
		{name: "zero", policy: "zero"},
		{name: "negative", policy: "negative"},
		{name: "unknown", policy: "unknown"},
		// The shared bucket holds streams of every policy without a bucket of
		// its own, so it never has an idle expiry.
		{name: "shared bucket", policy: noPolicy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, resolveIdleExpiry(limits, "tenant", tc.policy))
		})
	}
}

func TestUsageStore_EvictIdle(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, newIdleExpiryLimits(t), prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock

	update := func(hash uint64, policy string) {
		require.NoError(t, s.Update("tenant", &proto.StreamMetadata{StreamHash: hash, IngestionPolicy: policy}, clock.Now()))
	}
	update(0x1, backfillPolicy) // Idle for longer than the expiry.
	update(0x2, "other")        // A policy bucket without an idle expiry.
	update(0x3, noPolicy)       // The shared bucket.
	update(0x4, "expiry-only")  // An idle expiry, but in the shared bucket.
	clock.Advance(testIdleExpiry - time.Second)
	update(0x5, backfillPolicy) // Seen within the expiry.
	clock.Advance(2 * time.Second)

	require.Empty(t, s.Evict())
	require.Equal(t, map[string]int{"tenant": 1}, s.EvictIdle())
	var remaining []uint64
	for _, stream := range s.TenantActiveStreams("tenant") {
		remaining = append(remaining, stream.hash)
	}
	require.ElementsMatch(t, []uint64{0x2, 0x3, 0x4, 0x5}, remaining)
}

func TestStreamShardStore_EvictIdle(t *testing.T) {
	s, err := newStreamShardStore(testActiveWindow, testRateWindow, testBucketSize, 1, testZone, true, newIdleExpiryLimits(t), prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock

	push := func(hash uint64, policy string) {
		results, _ := s.checkAndShard(t.Context(), "test", []*proto.StreamMetadata{{
			StreamHash:      hash,
			TotalSize:       1,
			IngestionPolicy: policy,
		}}, clock.Now())
		require.Len(t, results, 1)
		require.Equal(t, uint32(1), results[0].Shards)
	}
	slots := func(policy string) uint64 {
		var n uint64
		s.withLock("test", func(i int) {
			n = s.stripes[i]["test"][0][policy].slots
		})
		return n
	}
	push(0x1, backfillPolicy)
	push(0x2, "other")
	push(0x3, noPolicy)
	push(0x4, "expiry-only")
	clock.Advance(testIdleExpiry - time.Second)
	push(0x5, backfillPolicy)
	clock.Advance(2 * time.Second)
	require.Equal(t, uint64(2), slots(backfillPolicy))

	require.Equal(t, map[string]int{"test": 1}, s.EvictIdle())
	require.Equal(t, 4, countTrackedStreams(s))
	// The evicted stream's budget is released.
	require.Equal(t, uint64(1), slots(backfillPolicy))
}

func TestService_EvictIdleStreams(t *testing.T) {
	limits := newIdleExpiryLimits(t)
	s, clock := newTestService(t, limits, 1)
	s.partitionManager.Assign([]int32{0})
	s.cfg.EvictionInterval = time.Hour
	s.cfg.IdleEvictionInterval = 10 * time.Millisecond

	metadata := &proto.StreamMetadata{StreamHash: 0x1, TotalSize: 100, IngestionPolicy: backfillPolicy}
	_, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{metadata},
	})
	require.NoError(t, err)
	require.NoError(t, s.usage.Update("test", metadata, clock.Now()))

	// The idle eviction covers the shard store as well as the usage store.
	clock.Advance(testIdleExpiry + time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.evictOldStreamsPeriodic(ctx)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(s.metrics.streamShardEvictionsTotal.WithLabelValues("test", evictionReasonPerPolicyOld)) == 1 &&
			testutil.ToFloat64(s.metrics.streamEvictionsTotal.WithLabelValues("test", evictionReasonPerPolicyOld)) == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, testutil.ToFloat64(s.metrics.streamEvictionsTotal.WithLabelValues("test", evictionReasonOld)))
}
