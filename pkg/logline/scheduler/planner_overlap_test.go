package scheduler

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

func makeMeta(date, hash string, minT, maxT time.Time, sizeBytes int64) store.Meta {
	return store.Meta{
		Version:   "v3",
		Date:      date,
		Hash:      hash,
		SizeBytes: sizeBytes,
		MinLogTs:  minT,
		MaxLogTs:  maxT,
	}
}

func TestOverlapPlanner_EmptyInput(t *testing.T) {
	p := newOverlapPlanner(nil, 512*mib)
	require.Nil(t, p.Plan())

	p2 := newOverlapPlanner([]store.Meta{}, 512*mib)
	require.Nil(t, p2.Plan())
}

func TestOverlapPlanner_SingleIndex(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)
	require.Nil(t, p.Plan())
	require.Equal(t, 1, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_TwoDisjointIndexes(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A ends at epoch+4min; B starts at epoch+10min — clear gap.
	// No time overlap, but the fallback pass groups them to reduce total count.
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(4*time.Minute), 100*mib),
		makeMeta("2026-01-01", "bbb", epoch.Add(10*time.Minute), epoch.Add(14*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.Len(t, group, 2, "disjoint indexes should be grouped in fallback pass")
	require.Nil(t, p.Plan())
}

func TestOverlapPlanner_TwoOverlappingIndexes(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A=[0,10min], B=[5min,15min] — overlapping.
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-01", "bbb", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.Len(t, group, 2)
	require.Nil(t, p.Plan())
}

func TestOverlapPlanner_DisjointClustersIsolated(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Cluster 1: A=[0,10], B=[5,15]. Cluster 2: C=[30,40], D=[35,45].
	metaA := makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 100*mib)
	metaB := makeMeta("2026-01-01", "bbb", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib)
	metaC := makeMeta("2026-01-01", "ccc", epoch.Add(30*time.Minute), epoch.Add(40*time.Minute), 100*mib)
	metaD := makeMeta("2026-01-01", "ddd", epoch.Add(35*time.Minute), epoch.Add(45*time.Minute), 100*mib)

	cluster1IDs := map[string]bool{metaA.ID(): true, metaB.ID(): true}
	cluster2IDs := map[string]bool{metaC.ID(): true, metaD.ID(): true}

	p := newOverlapPlanner([]store.Meta{metaA, metaB, metaC, metaD}, 512*mib)

	group1 := p.Plan()
	require.Len(t, group1, 2)

	group2 := p.Plan()
	require.Len(t, group2, 2)

	require.Nil(t, p.Plan())

	// Verify no cross-cluster mixing: each group must be entirely within one cluster.
	checkGroupBelongsToOneCluster := func(group []string) {
		inCluster1 := cluster1IDs[group[0]]
		inCluster2 := cluster2IDs[group[0]]
		require.True(t, inCluster1 || inCluster2, "unknown ID in group: %s", group[0])
		for _, id := range group[1:] {
			if inCluster1 {
				require.True(t, cluster1IDs[id], "cross-cluster mixing: %s belongs to cluster 2", id)
			} else {
				require.True(t, cluster2IDs[id], "cross-cluster mixing: %s belongs to cluster 1", id)
			}
		}
	}
	checkGroupBelongsToOneCluster(group1)
	checkGroupBelongsToOneCluster(group2)
}

func TestOverlapPlanner_SizeConstraintSkipsOversized(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// All three overlap. One is 600 MiB (oversized), two are 100 MiB each.
	metas := []store.Meta{
		makeMeta("2026-01-01", "big", epoch, epoch.Add(10*time.Minute), 600*mib),
		makeMeta("2026-01-01", "sm1", epoch.Add(2*time.Minute), epoch.Add(12*time.Minute), 100*mib),
		makeMeta("2026-01-01", "sm2", epoch.Add(4*time.Minute), epoch.Add(14*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.Len(t, group, 2)
	// Oversized is skipped; two small ones form the group.
	for _, id := range group {
		require.NotEqual(t, "2026-01-01/big", id, "oversized index must not appear in group")
	}

	require.Nil(t, p.Plan())
	require.Equal(t, 1, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_SizeConstraintSplitsCluster(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 5 indexes each at 150 MiB in overlapping time ranges. maxJobSizeBytes=512 MiB.
	// 3*150=450 fits; 4*150=600 exceeds. Expect multiple groups, no group > 512 MiB.
	metas := []store.Meta{
		makeMeta("2026-01-01", "a01", epoch, epoch.Add(10*time.Minute), 150*mib),
		makeMeta("2026-01-01", "a02", epoch.Add(2*time.Minute), epoch.Add(12*time.Minute), 150*mib),
		makeMeta("2026-01-01", "a03", epoch.Add(4*time.Minute), epoch.Add(14*time.Minute), 150*mib),
		makeMeta("2026-01-01", "a04", epoch.Add(6*time.Minute), epoch.Add(16*time.Minute), 150*mib),
		makeMeta("2026-01-01", "a05", epoch.Add(8*time.Minute), epoch.Add(18*time.Minute), 150*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	var groups [][]string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		require.GreaterOrEqual(t, len(g), 2, "group must have at least 2 sources")
		// Verify no group exceeds size limit.
		var total int64
		for _, id := range g {
			for _, m := range metas {
				if m.ID() == id {
					total += m.SizeBytes
				}
			}
		}
		require.LessOrEqual(t, total, int64(512*mib), "group total size must not exceed maxJobSizeBytes")
		groups = append(groups, g)
	}
	require.NotEmpty(t, groups)
}

func TestOverlapPlanner_AllOversized(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Both indexes are 600 MiB, max is 512 MiB. They overlap in time.
	metas := []store.Meta{
		makeMeta("2026-01-01", "big1", epoch, epoch.Add(10*time.Minute), 600*mib),
		makeMeta("2026-01-01", "big2", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 600*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)
	require.Nil(t, p.Plan())
	require.Equal(t, 2, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_Determinism(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 80*mib),
		makeMeta("2026-01-01", "bbb", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 90*mib),
		makeMeta("2026-01-01", "ccc", epoch.Add(8*time.Minute), epoch.Add(18*time.Minute), 70*mib),
		makeMeta("2026-01-01", "ddd", epoch.Add(12*time.Minute), epoch.Add(22*time.Minute), 110*mib),
		makeMeta("2026-01-01", "eee", epoch.Add(16*time.Minute), epoch.Add(26*time.Minute), 95*mib),
		makeMeta("2026-01-01", "fff", epoch.Add(20*time.Minute), epoch.Add(30*time.Minute), 85*mib),
	}

	// Collect groups from planner with original order.
	p1 := newOverlapPlanner(metas, 512*mib)
	var groups1 [][]string
	for {
		g := p1.Plan()
		if g == nil {
			break
		}
		groups1 = append(groups1, g)
	}

	// Shuffle and collect from second planner.
	shuffled := append([]store.Meta(nil), metas...)
	rng := rand.New(rand.NewSource(42))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	p2 := newOverlapPlanner(shuffled, 512*mib)
	var groups2 [][]string
	for {
		g := p2.Plan()
		if g == nil {
			break
		}
		groups2 = append(groups2, g)
	}

	require.Equal(t, groups1, groups2, "planner output must be deterministic regardless of input order")
}

func TestOverlapPlanner_NewestDateNotStarved(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Simulate the production scenario: 4 dates, each with multiple groups.
	// The newest date (01-04) must appear in the first batch of planned groups,
	// not be starved behind older dates.
	var metas []store.Meta
	for day := 1; day <= 4; day++ {
		date := time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		// 6 overlapping indexes per date → multiple groups per date.
		for i := range 6 {
			h := string(rune('a'+i)) + string(rune('0'+day))
			metas = append(metas, makeMeta(date, h+"0000000000000000"[:14],
				epoch.Add(time.Duration(i)*2*time.Minute),
				epoch.Add(time.Duration(i)*2*time.Minute+10*time.Minute),
				100*mib))
		}
	}

	p := newOverlapPlanner(metas, 512*mib)

	// Pull the first 4 groups (one round-robin pass across 4 dates).
	// The newest date must appear within this first batch.
	firstBatch := make(map[string]bool)
	for range 4 {
		g := p.Plan()
		require.NotNil(t, g)
		firstBatch[g[0][:10]] = true
	}

	require.True(t, firstBatch["2026-01-04"],
		"newest date must appear in first round-robin pass, got dates: %v", firstBatch)
	require.Len(t, firstBatch, 4,
		"all 4 dates should be represented in first pass")
}

func TestOverlapPlanner_InterleavesDatesRoundRobin(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two dates, 4 overlapping indexes each → 2 groups per date (at 150 MiB each, 512 budget).
	// Plan order should interleave: newest, oldest, newest, oldest.
	var metas []store.Meta
	for _, day := range []int{1, 2} {
		date := time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		for i := range 4 {
			h := string(rune('a'+i)) + string(rune('0'+day))
			metas = append(metas, makeMeta(date, h+"0000000000000000"[:14],
				epoch.Add(time.Duration(i)*time.Minute),
				epoch.Add(time.Duration(i)*time.Minute+10*time.Minute),
				150*mib))
		}
	}

	p := newOverlapPlanner(metas, 512*mib)

	var dates []string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		dates = append(dates, g[0][:10])
	}

	require.GreaterOrEqual(t, len(dates), 2)
	// First group must be from the newest date.
	require.Equal(t, "2026-01-02", dates[0], "newest date should be planned first")
	// Second group must be from the oldest date (interleaved, not batched).
	if len(dates) >= 2 {
		require.Equal(t, "2026-01-01", dates[1], "second group should be from older date (round-robin)")
	}
}

func TestOverlapPlanner_MultiDateGrouping(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Indexes from two dates with overlapping time ranges — must never be mixed.
	metas := []store.Meta{
		makeMeta("2026-01-01", "d1a", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-01", "d1b", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib),
		makeMeta("2026-01-02", "d2a", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-02", "d2b", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	var groups [][]string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		groups = append(groups, g)
	}

	require.Len(t, groups, 2)
	for _, group := range groups {
		// All IDs in a group must share the same date prefix.
		firstDate := group[0][:10] // "YYYY-MM-DD"
		for _, id := range group[1:] {
			require.Equal(t, firstDate, id[:10], "cross-date mixing detected in group %v", group)
		}
	}
}

func TestOverlapPlanner_ZeroTimestamps(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Mix of zero-timestamp indexes and normal overlapping indexes.
	metas := []store.Meta{
		makeMeta("2026-01-01", "z01", time.Time{}, time.Time{}, 50*mib),              // zero timestamps
		makeMeta("2026-01-01", "z02", time.Time{}, epoch.Add(5*time.Minute), 50*mib), // partial zero
		makeMeta("2026-01-01", "nrm1", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-01", "nrm2", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib),
	}

	// Must not panic.
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	// The two normal overlapping indexes should form a group.
	require.NotNil(t, group)
	require.Len(t, group, 2)
	for _, id := range group {
		require.NotContains(t, []string{"2026-01-01/z01", "2026-01-01/z02"}, id,
			"zero-timestamp indexes must not appear in compaction groups")
	}

	require.Nil(t, p.Plan())
}

func TestOverlapPlanner_OverlapPriorityOrdering(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Cluster A: high overlap — both cover almost the same 4-minute window.
	metaA1 := makeMeta("2026-01-01", "ha1", epoch, epoch.Add(4*time.Minute), 100*mib)
	metaA2 := makeMeta("2026-01-01", "ha2", epoch.Add(1*time.Minute), epoch.Add(4*time.Minute), 100*mib)

	// Cluster B: low overlap — time ranges barely touch (B1 ends exactly where B2 starts).
	// Use a strict non-strict check: MinLogTs <= clusterMaxLogTs means they cluster together.
	// B1=[20min,30min], B2=[30min,40min]: B2.MinLogTs == B1.MaxLogTs → they cluster.
	// Overlap fraction ≈ 0 (single point intersection / wide union).
	metaB1 := makeMeta("2026-01-01", "lb1", epoch.Add(20*time.Minute), epoch.Add(30*time.Minute), 100*mib)
	metaB2 := makeMeta("2026-01-01", "lb2", epoch.Add(30*time.Minute), epoch.Add(40*time.Minute), 100*mib)

	clusterAIDs := map[string]bool{metaA1.ID(): true, metaA2.ID(): true}

	p := newOverlapPlanner([]store.Meta{metaA1, metaA2, metaB1, metaB2}, 512*mib)

	// First group must come from cluster A (high overlap).
	group1 := p.Plan()
	require.NotNil(t, group1)
	require.Len(t, group1, 2)
	for _, id := range group1 {
		require.True(t, clusterAIDs[id], "first group must be from high-overlap cluster A, got %s", id)
	}

	// Second group comes from cluster B.
	group2 := p.Plan()
	require.NotNil(t, group2)
	require.Len(t, group2, 2)

	require.Nil(t, p.Plan())
}

func TestOverlapPlanner_MultiSourceGroups(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 8 small overlapping indexes. Budget 512 MiB → all 8 fit in one group (8*50=400).
	var metas []store.Meta
	for i := range 8 {
		h := string(rune('a'+i)) + "0"
		metas = append(metas, makeMeta("2026-01-01", h+"0000000000000000"[:14],
			epoch.Add(time.Duration(i)*2*time.Minute),
			epoch.Add(time.Duration(i)*2*time.Minute+10*time.Minute),
			50*mib))
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.NotNil(t, group)
	require.Len(t, group, 8, "all 8 overlapping small indexes should be in one group")
	require.Nil(t, p.Plan())
}

func TestOverlapPlanner_MultiSourceSizeBoundary(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 6 indexes at 100 MiB each, all overlapping. Budget 512 MiB.
	// 5*100=500 fits, 6*100=600 doesn't. Expect group of 5 + singleton skipped.
	var metas []store.Meta
	for i := range 6 {
		h := string(rune('a'+i)) + "0"
		metas = append(metas, makeMeta("2026-01-01", h+"0000000000000000"[:14],
			epoch.Add(time.Duration(i)*time.Minute),
			epoch.Add(time.Duration(i)*time.Minute+10*time.Minute),
			100*mib))
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.NotNil(t, group)
	require.Len(t, group, 5, "5 fit under 512 MiB budget")

	// 6th is a trailing singleton — no second group.
	require.Nil(t, p.Plan())
	require.Equal(t, 1, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_DisjointFallbackGrouping(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 4 completely disjoint indexes on a settled date — no time overlap at all.
	// The fallback pass should still group them to reduce total index count.
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-01", "bbb", epoch.Add(20*time.Minute), epoch.Add(30*time.Minute), 100*mib),
		makeMeta("2026-01-01", "ccc", epoch.Add(40*time.Minute), epoch.Add(50*time.Minute), 100*mib),
		makeMeta("2026-01-01", "ddd", epoch.Add(60*time.Minute), epoch.Add(70*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	group := p.Plan()
	require.NotNil(t, group)
	require.Len(t, group, 4, "all 4 disjoint indexes should be packed in fallback pass")
	require.Nil(t, p.Plan())
	require.Equal(t, 0, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_MixedOverlapAndDisjoint(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Cluster 1: A and B overlap (0-10, 5-15).
	// C is disjoint (30-40).
	// D is disjoint (50-60).
	// Overlap pass groups {A,B}. Fallback pass groups {C,D}.
	metas := []store.Meta{
		makeMeta("2026-01-01", "aaa", epoch, epoch.Add(10*time.Minute), 100*mib),
		makeMeta("2026-01-01", "bbb", epoch.Add(5*time.Minute), epoch.Add(15*time.Minute), 100*mib),
		makeMeta("2026-01-01", "ccc", epoch.Add(30*time.Minute), epoch.Add(40*time.Minute), 100*mib),
		makeMeta("2026-01-01", "ddd", epoch.Add(50*time.Minute), epoch.Add(60*time.Minute), 100*mib),
	}
	p := newOverlapPlanner(metas, 512*mib)

	var groups [][]string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		groups = append(groups, g)
	}
	require.Len(t, groups, 2, "one overlap group + one fallback group")

	// All 4 indexes should be accounted for across both groups.
	allIDs := make(map[string]bool)
	for _, g := range groups {
		for _, id := range g {
			allIDs[id] = true
		}
	}
	require.Len(t, allIDs, 4)
	require.Equal(t, 0, p.(*overlapPlanner).skipped)
}

func TestOverlapPlanner_ShardedIndexesGroupByShardKey(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 4 indexes: 2 for shard 0 and 2 for shard 1, same date, overlapping time.
	// They must NOT be grouped together — different shard values are different compaction groups.
	shard0a := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "s0a", MinLogTs: epoch, MaxLogTs: epoch.Add(10 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 2, ShardAlgorithm: "first_byte", ShardValue: 0,
	}
	shard0b := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "s0b", MinLogTs: epoch.Add(5 * time.Minute), MaxLogTs: epoch.Add(15 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 2, ShardAlgorithm: "first_byte", ShardValue: 0,
	}
	shard1a := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "s1a", MinLogTs: epoch, MaxLogTs: epoch.Add(10 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 2, ShardAlgorithm: "first_byte", ShardValue: 1,
	}
	shard1b := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "s1b", MinLogTs: epoch.Add(5 * time.Minute), MaxLogTs: epoch.Add(15 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 2, ShardAlgorithm: "first_byte", ShardValue: 1,
	}

	p := newOverlapPlanner([]store.Meta{shard0a, shard0b, shard1a, shard1b}, 512*mib)

	var groups [][]string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		groups = append(groups, g)
	}

	require.Len(t, groups, 2, "shard 0 and shard 1 must form separate groups")

	for _, g := range groups {
		firstID := g[0]
		for _, id := range g[1:] {
			firstShard := shardValueForID(firstID, shard0a, shard0b, shard1a, shard1b)
			thisShard := shardValueForID(id, shard0a, shard0b, shard1a, shard1b)
			require.Equal(t, firstShard, thisShard, "cross-shard mixing in group %v", g)
		}
	}
}

func shardValueForID(id string, metas ...store.Meta) int {
	for _, m := range metas {
		if m.ID() == id {
			return m.ShardValue
		}
	}
	return -1
}

// TestOverlapPlanner_DocumentShardedIndexes checks that v5 indexes with
// document shard bits compact with indexes of the same bits, and that v5
// indexes written before day-relative cells (footer version 4) are never
// planned: the v5 merge refuses them, so their jobs would fail on every pass.
func TestOverlapPlanner_DocumentShardedIndexes(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v5Meta := func(hash string, shardBits int, footerVersion uint32) store.Meta {
		m := makeMeta("2026-01-01", hash, epoch, epoch.Add(10*time.Minute), 100*mib)
		m.Version = "v5"
		m.DocumentInterval = 16 * time.Second
		m.DocumentShardBits = shardBits
		m.IndexHeader = &format.HeaderInfo{Version: footerVersion}
		return m
	}
	timeOnly1, timeOnly2 := v5Meta("plain1", 0, 5), v5Meta("plain2", 0, 5)
	sharded1, sharded2 := v5Meta("shard1", 5, 5), v5Meta("shard2", 5, 5)
	old1, old2 := v5Meta("old1", 5, 4), v5Meta("old2", 5, 4)

	p := newOverlapPlanner([]store.Meta{timeOnly1, sharded1, old1, timeOnly2, sharded2, old2}, 512*mib)

	var groups [][]string
	for g := p.Plan(); g != nil; g = p.Plan() {
		slices.Sort(g)
		groups = append(groups, g)
	}
	require.ElementsMatch(t, [][]string{
		{timeOnly1.ID(), timeOnly2.ID()},
		{sharded1.ID(), sharded2.ID()},
	}, groups)
}

func TestOverlapPlanner_MixedShardConfigsDontMix(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Old unsharded index (ShardCount=0) and new sharded (ShardCount=4) for same date.
	// They must never compact together.
	unsharded := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "old1", MinLogTs: epoch, MaxLogTs: epoch.Add(10 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 0, ShardAlgorithm: "", ShardValue: 0,
	}
	unsharded2 := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "old2", MinLogTs: epoch.Add(5 * time.Minute), MaxLogTs: epoch.Add(15 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 0, ShardAlgorithm: "", ShardValue: 0,
	}
	sharded := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "new1", MinLogTs: epoch, MaxLogTs: epoch.Add(10 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 0,
	}
	sharded2 := store.Meta{
		Version: "v3", Date: "2026-01-01", Hash: "new2", MinLogTs: epoch.Add(5 * time.Minute), MaxLogTs: epoch.Add(15 * time.Minute),
		SizeBytes: 100 * mib, ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 0,
	}

	p := newOverlapPlanner([]store.Meta{unsharded, unsharded2, sharded, sharded2}, 512*mib)

	var groups [][]string
	for {
		g := p.Plan()
		if g == nil {
			break
		}
		groups = append(groups, g)
	}

	require.Len(t, groups, 2)
	for _, g := range groups {
		ids := make(map[string]bool)
		for _, id := range g {
			ids[id] = true
		}
		hasOld := ids[unsharded.ID()] || ids[unsharded2.ID()]
		hasNew := ids[sharded.ID()] || ids[sharded2.ID()]
		require.False(t, hasOld && hasNew, "unsharded and sharded indexes must not compact together: group %v", g)
	}
}
