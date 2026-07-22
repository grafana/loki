package tsdb

import (
	"container/heap"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// mergeChunkRefsHeap performs a K-way streaming merge of already-sorted
// per-file chunk-ref lists using a binary min-heap keyed on
// (Fingerprint, From, Through, Checksum). Dedup happens inline against the
// most-recently-emitted ChunkRef — the same-file compaction transition can
// produce duplicate refs in adjacent lists, and in a merged sorted stream
// duplicates are always adjacent.
//
// Inputs must be sorted by ChunkRef.Less. Empty inputs are skipped. Callers
// retain ownership of the input slices and must not return them to
// ChunkRefsPool until after this function returns.
func mergeChunkRefsHeap(res []logproto.ChunkRefWithSizingInfo, xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	switch len(xs) {
	case 0:
		return res
	case 1:
		// Single list is already sorted; still need intra-list dedup in case
		// a single index somehow emits a duplicate (defensive; matches the
		// sort.Slice path's dedup guarantee).
		return dedupSortedInto(res, xs[0])
	}

	h := chunkRefHeap{cursors: make([]chunkRefCursor, 0, len(xs))}
	for i, list := range xs {
		if len(list) == 0 {
			continue
		}
		h.cursors = append(h.cursors, chunkRefCursor{listIdx: i, pos: 0})
	}
	h.xs = xs
	heap.Init(&h)

	var (
		haveLast bool
		last     logproto.ChunkRef
	)
	for h.Len() > 0 {
		top := h.cursors[0]
		ref := xs[top.listIdx][top.pos]
		if !haveLast || ref.ChunkRef != last {
			res = append(res, ref)
			last = ref.ChunkRef
			haveLast = true
		}
		// Advance cursor.
		next := top.pos + 1
		if next >= len(xs[top.listIdx]) {
			heap.Pop(&h)
		} else {
			h.cursors[0].pos = next
			heap.Fix(&h, 0)
		}
	}

	return res
}

// dedupSortedInto appends src to dst, dropping consecutive duplicates on
// ChunkRef. Assumes src is sorted by ChunkRef.Less.
func dedupSortedInto(dst []logproto.ChunkRefWithSizingInfo, src []logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
	var (
		haveLast bool
		last     logproto.ChunkRef
	)
	for _, ref := range src {
		if !haveLast || ref.ChunkRef != last {
			dst = append(dst, ref)
			last = ref.ChunkRef
			haveLast = true
		}
	}
	return dst
}

type chunkRefCursor struct {
	listIdx int
	pos     int
}

// chunkRefHeap is a container/heap adapter over a slice of cursors into a
// slice-of-slices `xs`. The `xs` reference is stored so Less can compare the
// actual ChunkRef values without allocating.
type chunkRefHeap struct {
	xs      [][]logproto.ChunkRefWithSizingInfo
	cursors []chunkRefCursor
}

func (h *chunkRefHeap) Len() int { return len(h.cursors) }

func (h *chunkRefHeap) Less(i, j int) bool {
	ci, cj := h.cursors[i], h.cursors[j]
	ri := &h.xs[ci.listIdx][ci.pos].ChunkRef
	rj := &h.xs[cj.listIdx][cj.pos].ChunkRef
	return ri.Less(*rj)
}

func (h *chunkRefHeap) Swap(i, j int) { h.cursors[i], h.cursors[j] = h.cursors[j], h.cursors[i] }

func (h *chunkRefHeap) Push(x any) { h.cursors = append(h.cursors, x.(chunkRefCursor)) }

func (h *chunkRefHeap) Pop() any {
	old := h.cursors
	n := len(old)
	x := old[n-1]
	h.cursors = old[:n-1]
	return x
}
