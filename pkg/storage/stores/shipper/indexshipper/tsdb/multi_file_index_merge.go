package tsdb

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// mergeChunkRefsSort is the historical concat-then-sort merge for
// MultiIndex.GetChunkRefs. It deduplicates via a map keyed on
// logproto.ChunkRef and then sorts the merged slice with sort.Slice.
//
// It does NOT return the input slices to ChunkRefsPool; callers own that.
// If res is nil, one is fetched from ChunkRefsPool.
func mergeChunkRefsSort(res []logproto.ChunkRefWithSizingInfo, xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	seen := make(map[logproto.ChunkRef]struct{})

	for _, group := range xs {
		for _, ref := range group {
			if _, ok := seen[ref.ChunkRef]; ok {
				continue
			}
			seen[ref.ChunkRef] = struct{}{}
			res = append(res, ref)
		}
	}

	sort.Slice(res, func(i, j int) bool { return res[i].Less(res[j].ChunkRef) })

	return res
}
