package tsdb

import (
	"container/heap"
	"context"
	"errors"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
)

type MultiIndex struct {
	indices []Index
}

// bounded is optional, but if supplied, it will wrap each passed index in NewParallelIndex
func NewBoundedMultiIndex(bounded *BoundedParallelism, indices ...Index) (Index, error) {
	if len(indices) == 0 {
		return nil, errors.New("must supply at least one index")
	}

	if len(indices) == 1 {
		if bounded == nil {
			return indices[0], nil
		}
		return NewParallelIndex(bounded, indices[0]), nil
	}

	sort.Slice(indices, func(i, j int) bool {
		aFrom, aThrough := indices[i].Bounds()
		bFrom, bThrough := indices[j].Bounds()

		if aFrom != bFrom {
			return aFrom < bFrom
		}
		// tiebreaker uses through
		return aThrough <= bThrough
	})

	if bounded == nil {
		return &MultiIndex{indices: indices}, nil
	}

	limited := make([]Index, 0, len(indices))
	for _, idx := range indices {
		limited = append(limited, NewParallelIndex(bounded, idx))
	}
	return &MultiIndex{indices: limited}, nil
}

func (i *MultiIndex) Bounds() (model.Time, model.Time) {
	var lowest, highest model.Time
	for _, idx := range i.indices {
		from, through := idx.Bounds()
		if lowest == 0 || from < lowest {
			lowest = from
		}

		if highest == 0 || through > highest {
			highest = through
		}
	}

	return lowest, highest
}

func (i *MultiIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	queryBounds := newBounds(from, through)
	g, ctx := errgroup.WithContext(ctx)

	// ensure we prebuffer the channel by the maximum possible
	// return length so our goroutines don't block
	ch := make(chan []ChunkRef, len(i.indices))

	for _, idx := range i.indices {
		// ignore indices which can't match this query
		if Overlap(queryBounds, idx) {
			// run all queries in linked goroutines (cancel after first err),
			// bounded by parallelism controls if applicable.
			g.Go(func() error {
				refs, err := idx.GetChunkRefs(ctx, userID, from, through, matchers...)
				if err != nil {
					return err
				}
				ch <- refs
				return nil
			})
		}
	}

	// wait for work to finish or error|ctx expiration
	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(ch)

	refHeap := &ChunkRefHeap{}
	var maxLn int // maximum number of chunk refs, assuming no duplicates
	for refs := range ch {
		maxLn += len(refs)
		if len(refs) > 0 {
			// TODO(owen-d): There is a lot of potentially unnecessary sorting
			// we're doing. Here, as well as the heapsort later on to dedupe.
			// It may be better to just use a map for deduping and ignore ordering.
			// Additionally, if we store series in TSDB by sorted fingerprints
			// instead of lexicographically by labels, we could skip
			// this current sort step and use the heapsort for deduping
			// by using a (fingerprint > from > through) ordering scheme therein.
			// I want to try that out as a later optimization, so I'm leaving
			// this comment as a breadcrumb
			sort.Slice(refs, func(i, j int) bool {
				return refs[i].Less(refs[j])
			})
			heap.Push(refHeap, refs)
		}
	}

	// optimistically allocate the maximum length slice
	// to avoid growing incrementally
	results := make([]ChunkRef, 0, maxLn)
	var last ChunkRef
	for refHeap.Len() > 0 {
		next := heap.Pop(refHeap).(ChunkRef)

		// duplicate, ignore
		if last == next {
			continue
		}

		results = append(results, next)
		last = next
	}

	return results, nil

}

func (i *MultiIndex) Series(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]Series, error) {
	panic("unimplemented!")
}

func (i *MultiIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	panic("unimplemented!")
}

func (i *MultiIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	panic("unimplemented!")
}
