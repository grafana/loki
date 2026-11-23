package hintprovider

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// shardGroup identifies a partitioning scheme. Indexes in the same group
// split the ngram space the same way — their per-value results must be
// intersected. Different groups (e.g., a re-shard from 4→8) cover different
// time periods and are unioned.
//
// Version is part of the group because the extractor belongs to the index
// version, so the same needle can decompose into different terms per version
// and those terms route to different shard values. Intersecting across versions
// would drop every range of a version that has no term on a shard the other
// version queried.
type shardGroup struct {
	Version        string
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
			Version:        m.Version,
			ShardCount:     m.ShardCount,
			ShardAlgorithm: m.ShardAlgorithm,
		},
		ShardValue: m.ShardValue,
	}
}

func (g shardGroup) isSharded() bool { return g.ShardCount > 1 }

func (k shardKey) String() string {
	return fmt.Sprintf("%s/%s/%d/%d", k.Version, k.ShardAlgorithm, k.ShardCount, k.ShardValue)
}

// intersectRanges returns the overlapping portions of two range sets under
// [start, end) semantics, per document shard. Ranges of two document shards
// intersect only if one shard holds the other, and the result keeps the
// narrower one; a range with zero shard bits holds every shard. The output may
// hold overlapping ranges of one shard, which NormalizeRanges merges.
//
// When neither set has a document shard, both must be normalized
// (NormalizeRanges), and so is the output.
func intersectRanges(a, b []HintTimeRange) []HintTimeRange {
	if !hasDocumentShards(a) && !hasDocumentShards(b) {
		return intersectTimeRanges(a, b)
	}
	out := []HintTimeRange{}
	bShards := byDocumentShard(b)
	for _, as := range byDocumentShard(a) {
		for _, bs := range bShards {
			shard, ok := narrowerDocumentShard(as[0], bs[0])
			if !ok {
				continue
			}
			for _, r := range intersectTimeRanges(as, bs) {
				r.DocumentShard, r.DocumentShardBits = shard.DocumentShard, shard.DocumentShardBits
				out = append(out, r)
			}
		}
	}
	return out
}

// byDocumentShard normalizes ranges and splits them into one subslice per
// document shard.
func byDocumentShard(ranges []HintTimeRange) [][]HintTimeRange {
	var out [][]HintTimeRange
	normalized := NormalizeRanges(ranges)
	for start, i := 0, 1; i <= len(normalized); i++ {
		if i == len(normalized) || normalized[i].documentShardKey() != normalized[start].documentShardKey() {
			out = append(out, normalized[start:i])
			start = i
		}
	}
	return out
}

// narrowerDocumentShard returns whichever of a and b has more shard bits, if
// the other's shard holds it: a shard's top bits name each coarser shard
// holding it.
func narrowerDocumentShard(a, b HintTimeRange) (HintTimeRange, bool) {
	if a.DocumentShardBits > b.DocumentShardBits {
		a, b = b, a
	}
	return b, b.DocumentShard>>(b.DocumentShardBits-a.DocumentShardBits) == a.DocumentShard
}

// hasDocumentShards reports whether any range is limited to a document shard.
func hasDocumentShards(ranges []HintTimeRange) bool {
	return slices.ContainsFunc(ranges, func(r HintTimeRange) bool { return r.DocumentShardBits > 0 })
}

// intersectTimeRanges intersects two sorted, non-overlapping range sets by
// time alone. Adjacent ranges that only touch at a boundary (a.End ==
// b.Start) have empty intersection and are omitted.
func intersectTimeRanges(a, b []HintTimeRange) []HintTimeRange {
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
