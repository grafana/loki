package distributor

import (
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// shardCounter is used after all reservations have completed.
func shardCounter(tracker *ShardTracker, tenant string, hash uint64) int {
	return int(tracker.currentShard[hash&uint64(tracker.size-1)][tenant])
}

func TestShardTrackerReserve(t *testing.T) {
	type reservation struct {
		shards, count int
		wantStart     int
		wantCounter   uint64
		tenant        string
		hash          uint64
	}
	cases := []struct {
		name           string
		initialCounter uint64
		reservations   []reservation
	}{
		{
			name: "advance and wrap",
			reservations: []reservation{
				{shards: 10, count: 3, wantStart: 0, wantCounter: 3},
				{shards: 10, count: 8, wantStart: 3, wantCounter: 11},
				{shards: 10, count: 2, wantStart: 1, wantCounter: 13},
			},
		},
		{
			name:           "shared stripes and independent tenants",
			initialCounter: 13,
			reservations: []reservation{
				{hash: defaultStripeSize, shards: 10, count: 1, wantStart: 3, wantCounter: 14},
				{tenant: "another tenant", shards: 10, count: 1, wantStart: 0, wantCounter: 1},
				{hash: 1, shards: 10, count: 1, wantStart: 0, wantCounter: 1},
				{shards: 10, count: 1, wantStart: 4, wantCounter: 15},
			},
		},
		{
			name:           "changing shard count and counter overflow",
			initialCounter: math.MaxUint64 - 1,
			reservations: []reservation{
				{shards: 10, count: 3, wantStart: 4, wantCounter: 1},
				{shards: 3, count: 3, wantStart: 1, wantCounter: 4},
				{shards: 100, count: 3, wantStart: 4, wantCounter: 7},
				{shards: 1, count: 1, wantStart: 0, wantCounter: 8},
				{shards: 7, count: 3, wantStart: 1, wantCounter: 11},
			},
		},
		{name: "zero shards", initialCounter: 7, reservations: []reservation{{shards: 0, count: 1, wantCounter: 7}}},
		{name: "negative shards", initialCounter: 7, reservations: []reservation{{shards: -1, count: 1, wantCounter: 7}}},
		{name: "zero count", initialCounter: 7, reservations: []reservation{{shards: 10, count: 0, wantCounter: 7}}},
		{name: "negative count", initialCounter: 7, reservations: []reservation{{shards: 10, count: -1, wantCounter: 7}}},
		{name: "count exceeds shards", initialCounter: 7, reservations: []reservation{{shards: 10, count: 11, wantCounter: 7}}},
	}
	for _, mode := range []struct {
		name      string
		randomize bool
	}{{"sequential", false}, {"randomized", true}} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					st := NewShardTracker()
					st.seed = 1
					st.currentShard[0]["tenant"] = tc.initialCounter
					for _, r := range tc.reservations {
						tenant := r.tenant
						if tenant == "" {
							tenant = "tenant"
						}
						start := st.Reserve(tenant, r.hash, r.shards, r.count, mode.randomize)
						valid := r.shards > 0 && r.count > 0 && r.count <= r.shards
						if !mode.randomize || !valid {
							require.Equal(t, r.wantStart, start)
						} else {
							require.GreaterOrEqual(t, start, 0)
							require.Less(t, start, r.shards)
						}
						require.Equal(t, r.wantCounter, st.currentShard[r.hash&uint64(st.size-1)][tenant])
					}
				})
			}
		})
	}
}

func TestShardTrackerReserveConcurrent(t *testing.T) {
	const pushes = 1000
	for _, tc := range []struct {
		name                        string
		randomize                   bool
		shards, count, wantPerShard int
	}{
		{"sequential reservations do not overlap", false, pushes * 3, 3, 1},
		{"randomized cycles remain balanced", true, 10, 1, pushes / 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := NewShardTracker()
			st.seed = 1
			starts := make(chan int, pushes)
			var wg sync.WaitGroup
			for range pushes {
				wg.Go(func() { starts <- st.Reserve("tenant", 0, tc.shards, tc.count, tc.randomize) })
			}
			wg.Wait()
			close(starts)

			counts := make([]int, tc.shards)
			for start := range starts {
				require.GreaterOrEqual(t, start, 0)
				require.Less(t, start, tc.shards)
				for offset := range tc.count {
					counts[(start+offset)%tc.shards]++
				}
			}
			for shard, count := range counts {
				require.Equal(t, tc.wantPerShard, count, "shard %d", shard)
			}
			require.Equal(t, uint64(pushes*tc.count), st.currentShard[0]["tenant"])
		})
	}
}

func TestShardTrackerReserveDistribution(t *testing.T) {
	const shards, pushes = 10, 1000
	for _, tc := range []struct {
		name            string
		competingStream bool
		tolerance       float64
	}{
		{"complete cycles", false, 0},
		{"stripe collisions", true, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := NewShardTracker()
			st.seed = 1
			counts := make([]int, shards)
			seen := make(map[int]bool, shards)
			for push := range pushes {
				start := st.Reserve("tenant", 0, shards, 1, true)
				require.GreaterOrEqual(t, start, 0)
				require.Less(t, start, shards)
				counts[start]++
				if tc.competingStream {
					// A fixed phase would strand our stream on one shard.
					st.Reserve("tenant", defaultStripeSize, shards, shards-1, true)
				} else {
					if push%shards == 0 {
						clear(seen)
					}
					require.False(t, seen[start], "cycle %d repeats shard %d", push/shards, start)
					seen[start] = true
				}
			}
			for shard, count := range counts {
				require.InDelta(t, pushes/shards, count, tc.tolerance, "shard %d", shard)
			}
		})
	}
}

func TestShardTrackerReserveIndependentPhases(t *testing.T) {
	const shards = 10
	for _, tc := range []struct {
		name     string
		varySeed bool
	}{
		{"distributor seeds", true},
		{"stream hashes in the same stripe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			starts := make(map[int]bool)
			for i := range uint64(100) {
				st := NewShardTracker()
				st.seed = 1
				hash := uint64(7)
				if tc.varySeed {
					st.seed = i
				} else {
					hash = i * defaultStripeSize
				}
				starts[st.Reserve("tenant", hash, shards, 1, true)] = true
			}
			require.Len(t, starts, shards)
		})
	}
}

func TestShardTrackerReserveVariableFanout(t *testing.T) {
	const shards, pushes = 17, 10000
	for _, tc := range []struct {
		name         string
		distributors int
		collide      bool
	}{
		{"one distributor", 1, false},
		{"four distributors", 4, false},
		{"one distributor with stripe collisions", 1, true},
		{"four distributors with stripe collisions", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trackers := make([]*ShardTracker, tc.distributors)
			for i := range trackers {
				trackers[i] = NewShardTracker()
				trackers[i].seed = uint64(i + 1)
			}
			bytes := make([]int, shards)
			total := 0
			fanouts := []int{1, 3, 8, 2, 17, 5}
			for push := range pushes {
				for _, tracker := range trackers {
					count := fanouts[push%len(fanouts)]
					start := tracker.Reserve("tenant", 0, shards, count, true)
					require.GreaterOrEqual(t, start, 0)
					require.Less(t, start, shards)
					for output := range count {
						// Alternate front-heavy and back-heavy pushes of different sizes.
						size := 1 + push%3
						if output == (push%2)*(count-1) {
							size *= 100
						}
						bytes[(start+output)%shards] += size
						total += size
					}
					if tc.collide {
						tracker.Reserve("tenant", defaultStripeSize, 23, 1+push%23, true)
					}
				}
			}
			// Check long-run skew for this deterministic mix, not a per-window guarantee.
			mean := float64(total) / shards
			for shard, size := range bytes {
				require.InDelta(t, mean, size, mean*0.15, "shard %d", shard)
			}
		})
	}
}
