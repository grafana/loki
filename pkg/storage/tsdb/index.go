package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type Series struct {
	Labels labels.Labels
}

type ChunkRef struct {
	User        string
	Fingerprint model.Fingerprint
	Start, End  model.Time
	Checksum    uint32
}

// Compares by (Start, End)
// Assumes User is equivalent
func (r ChunkRef) Less(x ChunkRef) bool {
	if r.Start != x.Start {
		return r.Start < x.Start
	}
	return r.End <= x.End
}

type Index interface {
	Bounded
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error)
	Series(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]Series, error)
	LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
	LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error)
}

// utility for heapsort
type ChunkRefHeap [][]ChunkRef

func (h ChunkRefHeap) Len() int { return len(h) }
func (h ChunkRefHeap) Less(i, j int) bool {
	return h[i][0].Less(h[j][0])
}
func (h ChunkRefHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push a []ChunkRef to the heap
func (h *ChunkRefHeap) Push(x interface{}) {
	*h = append(*h, x.([]ChunkRef))
}

// Pop a single ChunkRef from the heap
func (h *ChunkRefHeap) Pop() interface{} {
	old := *h
	n := h.Len()
	x := old[n-1]
	*h = old[:n-1]

	result := x[0]
	rem := x[1:]
	if len(rem) > 0 {
		h.Push(rem)
	}

	return result
}
