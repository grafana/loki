package hintprovider

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestFilterNgramsForShard_Unsharded(t *testing.T) {
	ngrams := []string{"AAAAAA", "BBBBBB"}
	m := store.Meta{ShardCount: 0}
	got := filterNgramsForShard(ngrams, m)
	require.Equal(t, ngrams, got, "unsharded returns all ngrams")
}

func TestFilterNgramsForShard_MatchingShard(t *testing.T) {
	// A=0x41=65, 65%4=1 → shard 1
	// B=0x42=66, 66%4=2 → shard 2
	ngrams := []string{"AAAAAA", "BBBBBB"}
	m := store.Meta{ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1}
	got := filterNgramsForShard(ngrams, m)
	require.Equal(t, []string{"AAAAAA"}, got, "only AAAAAA maps to shard 1")
}

func TestFilterNgramsForShard_NoMatch(t *testing.T) {
	// shard 3: neither A (shard 1) nor B (shard 2) maps here
	ngrams := []string{"AAAAAA", "BBBBBB"}
	m := store.Meta{ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 3}
	got := filterNgramsForShard(ngrams, m)
	require.Empty(t, got)
}

func TestFilterNgramsForShard_UnknownAlgorithm(t *testing.T) {
	// Unknown algorithm returns all ngrams (safe fallback).
	ngrams := []string{"AAAAAA"}
	m := store.Meta{ShardCount: 4, ShardAlgorithm: "future_v99", ShardValue: 0}
	got := filterNgramsForShard(ngrams, m)
	require.Equal(t, ngrams, got)
}

func TestFilterNgramsForShard_PreservesInputOrder(t *testing.T) {
	// A=0x41=65, 65%4=1
	// E=0x45=69, 69%4=1
	ngrams := []string{"CCCCCC", "AAAAAA", "BBBBBB", "EEEEEE"}
	m := store.Meta{ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1}
	got := filterNgramsForShard(ngrams, m)
	require.Equal(t, []string{"AAAAAA", "EEEEEE"}, got, "matching terms should retain original order")
}

func TestIntersectRanges(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := time.Minute

	tests := []struct {
		name     string
		a, b     []HintTimeRange
		expected []HintTimeRange
	}{
		{
			name:     "both empty",
			a:        nil,
			b:        nil,
			expected: []HintTimeRange{},
		},
		{
			name:     "one empty",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(5 * m)}},
			b:        nil,
			expected: []HintTimeRange{},
		},
		{
			name:     "no overlap",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(5 * m)}},
			b:        []HintTimeRange{{Start: t0.Add(10 * m), End: t0.Add(15 * m)}},
			expected: []HintTimeRange{},
		},
		{
			name:     "partial overlap",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(10 * m)}},
			b:        []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(15 * m)}},
			expected: []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(10 * m)}},
		},
		{
			name:     "one contained in other",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(20 * m)}},
			b:        []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(10 * m)}},
			expected: []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(10 * m)}},
		},
		{
			name:     "identical ranges",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(10 * m)}},
			b:        []HintTimeRange{{Start: t0, End: t0.Add(10 * m)}},
			expected: []HintTimeRange{{Start: t0, End: t0.Add(10 * m)}},
		},
		{
			// Adjacent [start, end) ranges share a boundary but have empty
			// intersection. Emitting an instant [t,t] used to inflate
			// hint_ranges with zero-duration windows (see production traces
			// with thousands of instants and ~half the total seconds).
			name:     "touching at endpoint",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(5 * m)}},
			b:        []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(10 * m)}},
			expected: []HintTimeRange{},
		},
		{
			name: "multiple ranges with multiple overlaps",
			a: []HintTimeRange{
				{Start: t0, End: t0.Add(10 * m)},
				{Start: t0.Add(20 * m), End: t0.Add(30 * m)},
			},
			b: []HintTimeRange{
				{Start: t0.Add(5 * m), End: t0.Add(25 * m)},
			},
			expected: []HintTimeRange{
				{Start: t0.Add(5 * m), End: t0.Add(10 * m)},
				{Start: t0.Add(20 * m), End: t0.Add(25 * m)},
			},
		},
		{
			name:     "sources merged",
			a:        []HintTimeRange{{Start: t0, End: t0.Add(10 * m), Source: "shard=0"}},
			b:        []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(15 * m), Source: "shard=1"}},
			expected: []HintTimeRange{{Start: t0.Add(5 * m), End: t0.Add(10 * m), Source: "shard=0;shard=1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := intersectRanges(tc.a, tc.b)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestAggregateShardRanges(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := time.Minute

	unshardedKey := shardKey{} // ShardCount=0
	shard0 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 0}
	shard1 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 1}

	t.Run("unsharded only unions", func(t *testing.T) {
		byKey := map[shardKey][]HintTimeRange{
			unshardedKey: {
				{Start: t0, End: t0.Add(10 * m)},
				{Start: t0.Add(5 * m), End: t0.Add(15 * m)},
			},
		}
		got := aggregateShardRanges(byKey)
		require.Equal(t, []HintTimeRange{{Start: t0, End: t0.Add(15 * m)}}, got)
	})

	t.Run("two shards intersected", func(t *testing.T) {
		byKey := map[shardKey][]HintTimeRange{
			shard0: {{Start: t0, End: t0.Add(20 * m)}},
			shard1: {{Start: t0.Add(10 * m), End: t0.Add(30 * m)}},
		}
		got := aggregateShardRanges(byKey)
		// Intersection: [t0+10m, t0+20m]
		require.Equal(t, []HintTimeRange{{Start: t0.Add(10 * m), End: t0.Add(20 * m)}}, got)
	})

	t.Run("sharded intersect plus unsharded union", func(t *testing.T) {
		byKey := map[shardKey][]HintTimeRange{
			shard0:       {{Start: t0, End: t0.Add(20 * m)}},
			shard1:       {{Start: t0.Add(10 * m), End: t0.Add(30 * m)}},
			unshardedKey: {{Start: t0.Add(50 * m), End: t0.Add(60 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Len(t, got, 2)
		// Sharded intersection: [t0+10m, t0+20m]
		require.Equal(t, t0.Add(10*m), got[0].Start)
		require.Equal(t, t0.Add(20*m), got[0].End)
		// Unsharded union: [t0+50m, t0+60m]
		require.Equal(t, t0.Add(50*m), got[1].Start)
		require.Equal(t, t0.Add(60*m), got[1].End)
	})

	t.Run("disjoint shards produce empty result", func(t *testing.T) {
		byKey := map[shardKey][]HintTimeRange{
			shard0: {{Start: t0, End: t0.Add(5 * m)}},
			shard1: {{Start: t0.Add(10 * m), End: t0.Add(15 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Nil(t, got)
	})

	t.Run("adjacent shard ranges produce empty intersection not instants", func(t *testing.T) {
		// Cross-shard INTERSECT of abutting [start, end) docs, e.g.
		// ngram A → shard0 [t0, t0+1s), ngram B → shard1 [t0+1s, t0+2s).
		byKey := map[shardKey][]HintTimeRange{
			shard0: {{Start: t0, End: t0.Add(time.Second)}},
			shard1: {{Start: t0.Add(time.Second), End: t0.Add(2 * time.Second)}},
		}
		got := aggregateShardRanges(byKey)
		require.Nil(t, got)
	})

	t.Run("different shard groups are unioned not intersected", func(t *testing.T) {
		// Two different shard groups (count=4 vs count=8) at disjoint times.
		// They should be unioned, not intersected.
		group4shard0 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		group8shard0 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		byKey := map[shardKey][]HintTimeRange{
			group4shard0: {{Start: t0, End: t0.Add(10 * m)}},
			group8shard0: {{Start: t0.Add(20 * m), End: t0.Add(30 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Len(t, got, 2)
		require.Equal(t, t0, got[0].Start)
		require.Equal(t, t0.Add(10*m), got[0].End)
		require.Equal(t, t0.Add(20*m), got[1].Start)
		require.Equal(t, t0.Add(30*m), got[1].End)
	})

	t.Run("empty map", func(t *testing.T) {
		got := aggregateShardRanges(nil)
		require.Nil(t, got)
	})

	t.Run("three disjoint shards consistently empty across iteration orders", func(t *testing.T) {
		// Regression: a mid-loop nil intersection used to reset groupResult
		// to the next shard's ranges (via the `if groupResult == nil` branch),
		// so this map's output was non-deterministic under Go's randomized
		// iteration. Three shards is the minimum to trigger — the reset
		// requires at least one iteration AFTER the nil intersection.
		shard2 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		for i := range 200 {
			byKey := map[shardKey][]HintTimeRange{
				shard0: {{Start: t0, End: t0.Add(10 * m)}},
				shard1: {{Start: t0.Add(20 * m), End: t0.Add(30 * m)}},
				// shard2 covers BOTH shard0 and shard1's windows. A buggy
				// reset would surface shard2's ranges as the final result.
				shard2: {
					{Start: t0, End: t0.Add(10 * m)},
					{Start: t0.Add(20 * m), End: t0.Add(30 * m)},
				},
			}
			got := aggregateShardRanges(byKey)
			require.Nil(t, got, "trial %d: 0 ∩ 1 ∩ 2 is empty regardless of iteration order", i)
		}
	})

	t.Run("three overlapping shards consistently intersect across iteration orders", func(t *testing.T) {
		// Same regression test, happy path: all three shards overlap on
		// [20m, 30m]. Output must be stable across map iteration orders.
		shard2 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		want := []HintTimeRange{{Start: t0.Add(20 * m), End: t0.Add(30 * m)}}
		for i := range 200 {
			byKey := map[shardKey][]HintTimeRange{
				shard0: {{Start: t0.Add(10 * m), End: t0.Add(30 * m)}},
				shard1: {{Start: t0.Add(20 * m), End: t0.Add(40 * m)}},
				shard2: {{Start: t0.Add(15 * m), End: t0.Add(35 * m)}},
			}
			got := aggregateShardRanges(byKey)
			require.Len(t, got, 1, "trial %d", i)
			require.Equal(t, want[0].Start, got[0].Start, "trial %d", i)
			require.Equal(t, want[0].End, got[0].End, "trial %d", i)
		}
	})

	t.Run("two groups: one collapses to empty, other intersects to a range", func(t *testing.T) {
		// Group A (count=4): three disjoint shards → empty intersection.
		// Group B (count=8): three overlapping shards → non-empty intersection.
		// The empty group must not poison the non-empty group's result.
		a0 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		a1 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 1}
		a2 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		b0 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		b1 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 1}
		b2 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		byKey := map[shardKey][]HintTimeRange{
			a0: {{Start: t0, End: t0.Add(5 * m)}},
			a1: {{Start: t0.Add(10 * m), End: t0.Add(15 * m)}},
			a2: {{Start: t0.Add(20 * m), End: t0.Add(25 * m)}},
			b0: {{Start: t0.Add(100 * m), End: t0.Add(200 * m)}},
			b1: {{Start: t0.Add(150 * m), End: t0.Add(180 * m)}},
			b2: {{Start: t0.Add(160 * m), End: t0.Add(170 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Len(t, got, 1)
		require.Equal(t, t0.Add(160*m), got[0].Start)
		require.Equal(t, t0.Add(170*m), got[0].End)
	})

	t.Run("two groups both collapse to empty", func(t *testing.T) {
		a0 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		a1 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 1}
		a2 := shardKey{shardGroup: shardGroup{ShardCount: 4, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		b0 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 0}
		b1 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 1}
		b2 := shardKey{shardGroup: shardGroup{ShardCount: 8, ShardAlgorithm: "first_byte"}, ShardValue: 2}
		byKey := map[shardKey][]HintTimeRange{
			a0: {{Start: t0, End: t0.Add(5 * m)}},
			a1: {{Start: t0.Add(10 * m), End: t0.Add(15 * m)}},
			a2: {{Start: t0.Add(20 * m), End: t0.Add(25 * m)}},
			b0: {{Start: t0.Add(100 * m), End: t0.Add(110 * m)}},
			b1: {{Start: t0.Add(200 * m), End: t0.Add(210 * m)}},
			b2: {{Start: t0.Add(300 * m), End: t0.Add(310 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Nil(t, got)
	})

	t.Run("two non-empty groups union into stable result", func(t *testing.T) {
		// Group A (count=4): four shards all overlapping on [20m, 30m].
		// Group B (count=8): four shards all overlapping on [60m, 70m].
		// Final UNION across groups should yield two ranges regardless of order.
		mkGroup := func(count int, mins ...int) map[shardKey][]HintTimeRange {
			out := make(map[shardKey][]HintTimeRange)
			for i, base := range mins {
				k := shardKey{shardGroup: shardGroup{ShardCount: count, ShardAlgorithm: "first_byte"}, ShardValue: i}
				out[k] = []HintTimeRange{{Start: t0.Add(time.Duration(base) * m), End: t0.Add(time.Duration(base+20) * m)}}
			}
			return out
		}
		byKey := map[shardKey][]HintTimeRange{}
		// intersect → [19m, 30m]
		maps.Copy(byKey, mkGroup(4, 10, 15, 18, 19))
		// intersect → [59m, 70m]
		maps.Copy(byKey, mkGroup(8, 50, 55, 58, 59))
		got := aggregateShardRanges(byKey)
		require.Len(t, got, 2)
		require.Equal(t, t0.Add(19*m), got[0].Start)
		require.Equal(t, t0.Add(30*m), got[0].End)
		require.Equal(t, t0.Add(59*m), got[1].Start)
		require.Equal(t, t0.Add(70*m), got[1].End)
	})

	t.Run("five disjoint shards collapse to empty regardless of where the break fires", func(t *testing.T) {
		// More shards → more possible iteration orders → bail can fire at
		// iter 2, 3, 4, or 5 depending on which two disjoint shards land
		// adjacent in the iteration. Result must be empty in all cases.
		mkKey := func(v int) shardKey {
			return shardKey{shardGroup: shardGroup{ShardCount: 16, ShardAlgorithm: "first_byte"}, ShardValue: v}
		}
		byKey := map[shardKey][]HintTimeRange{
			mkKey(0): {{Start: t0, End: t0.Add(5 * m)}},
			mkKey(1): {{Start: t0.Add(10 * m), End: t0.Add(15 * m)}},
			mkKey(2): {{Start: t0.Add(20 * m), End: t0.Add(25 * m)}},
			mkKey(3): {{Start: t0.Add(30 * m), End: t0.Add(35 * m)}},
			mkKey(4): {{Start: t0.Add(40 * m), End: t0.Add(45 * m)}},
		}
		got := aggregateShardRanges(byKey)
		require.Nil(t, got)
	})
}

func TestMergeSources_Truncation(t *testing.T) {
	// Build a source string that exceeds the cap.
	segment := "index=2026-03-30/abc123def456,doc=42,min=2026-03-30T00:00:00Z,max=2026-03-30T12:00:00Z"
	var left string
	for len(left) < maxMergedSourceLen {
		if left != "" {
			left += ";"
		}
		left += segment
	}
	right := "index=2026-03-30/zzz,doc=0,min=2026-03-30T00:00:00Z,max=2026-03-30T01:00:00Z"

	merged := mergeSources(left, right)

	require.LessOrEqual(t, len(merged), maxMergedSourceLen)
	require.Contains(t, merged, "...(truncated)")

	// Subsequent merges must not grow past the cap or append another marker.
	again := mergeSources(merged, right+"x")
	require.Equal(t, merged, again)
	require.LessOrEqual(t, len(again), maxMergedSourceLen)
	require.Equal(t, 1, strings.Count(again, "...(truncated)"))
}

func TestMergeSources_SmallStringsUnchanged(t *testing.T) {
	require.Equal(t, "a;b", mergeSources("a", "b"))
	require.Equal(t, "a", mergeSources("a", ""))
	require.Equal(t, "b", mergeSources("", "b"))
	require.Equal(t, "a", mergeSources("a", "a"))
}

func TestMergeSources_OversizedSingleSource(t *testing.T) {
	huge := strings.Repeat("x", maxMergedSourceLen+100)
	got := mergeSources("", huge)
	require.LessOrEqual(t, len(got), maxMergedSourceLen)
	require.Contains(t, got, "...(truncated)")

	got = mergeSources(huge, "y")
	require.LessOrEqual(t, len(got), maxMergedSourceLen)
	require.Contains(t, got, "...(truncated)")
}

func TestMergeSources_NormalizeRanges_BoundedGrowth(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Simulate 1000 overlapping ranges from different indexes, all merging
	// into one range. Without truncation this would produce a ~90 KB source
	// string; with truncation it stays under the cap.
	ranges := make([]HintTimeRange, 1000)
	for i := range ranges {
		ranges[i] = HintTimeRange{
			Start:  t0,
			End:    t0.Add(time.Hour),
			Source: "index=2026-03-30/idx" + string(rune('A'+i%26)) + ",doc=0,min=x,max=y",
		}
	}
	out := normalizeRanges(ranges)
	require.Len(t, out, 1)
	require.LessOrEqual(t, len(out[0].Source), maxMergedSourceLen)
}
