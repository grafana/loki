package hintprovider

import (
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// shardGroup identifies a partitioning scheme. Indexes in the same group
// split the ngram space the same way — their per-value results must be
// intersected. Different groups (e.g., a re-shard from 4→8) cover different
// time periods and are unioned.
type shardGroup struct {
	ShardCount     int
	ShardAlgorithm string
}

// shardKey identifies a specific shard within a group.
type shardKey struct {
	shardGroup
	ShardValue int
}

func shardKeyOf(m store.Meta) shardKey {
	return shardKey{
		shardGroup: shardGroup{
			ShardCount:     m.ShardCount,
			ShardAlgorithm: m.ShardAlgorithm,
		},
		ShardValue: m.ShardValue,
	}
}

func (g shardGroup) isSharded() bool { return g.ShardCount > 1 }

func (k shardKey) String() string {
	return fmt.Sprintf("%s/%d/%d", k.ShardAlgorithm, k.ShardCount, k.ShardValue)
}

// intersectRanges returns the overlapping portions of two sorted, normalized
// range sets under [start, end) semantics. Both inputs must already be sorted
// by Start and non-overlapping (as produced by normalizeRanges). The output is
// also sorted and non-overlapping. Adjacent ranges that only touch at a
// boundary (a.End == b.Start) have empty intersection and are omitted.
func intersectRanges(a, b []HintTimeRange) []HintTimeRange {
	if len(a) == 0 || len(b) == 0 {
		return []HintTimeRange{}
	}

	// Defensive: sort inputs if caller didn't.
	sort.Slice(a, func(i, j int) bool { return a[i].Start.Before(a[j].Start) })
	sort.Slice(b, func(i, j int) bool { return b[i].Start.Before(b[j].Start) })

	out := []HintTimeRange{}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		// Compute overlap.
		start := a[i].Start
		if b[j].Start.After(start) {
			start = b[j].Start
		}
		end := a[i].End
		if b[j].End.Before(end) {
			end = b[j].End
		}

		// Half-open: start == end is empty, not an instant match.
		if start.Before(end) {
			out = append(out, HintTimeRange{
				Start:  start,
				End:    end,
				Source: mergeSources(a[i].Source, b[j].Source),
			})
		}

		// Advance the pointer whose range ends first.
		if a[i].End.Before(b[j].End) {
			i++
		} else {
			j++
		}
	}
	return out
}

// filterNgramsForShard returns only the ngrams that map to meta's shard.
// For unsharded indexes (ShardCount <= 1) or unknown algorithms, returns
// the full list unchanged.
func filterNgramsForShard(ngrams []string, meta store.Meta) []string {
	if meta.ShardCount <= 1 {
		return ngrams
	}
	fn, err := shard.New(meta.ShardAlgorithm)
	if err != nil {
		// for an unknown alg it feels safe enough to simply return all ngrams. essentially the filter "fails open"
		// by looking for everything
		return ngrams
	}
	filtered := make([]string, 0, len(ngrams))
	for _, ng := range ngrams {
		var key [8]byte
		copy(key[:], ng)
		if fn(key, meta.ShardCount) == meta.ShardValue {
			filtered = append(filtered, ng)
		}
	}
	return filtered
}
