package tsdb

import (
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

func NewMultiIndex(indices ...Index) (Index, error) {
	if len(indices) == 0 {
		return nil, errors.New("must supply at least one index")
	}

	if len(indices) == 1 {
		return indices[0], nil
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

	return &MultiIndex{indices: indices}, nil
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

func (i *MultiIndex) forIndices(ctx context.Context, from, through model.Time, fn func(context.Context, Index) (interface{}, error)) ([]interface{}, error) {
	queryBounds := newBounds(from, through)
	g, ctx := errgroup.WithContext(ctx)

	// ensure we prebuffer the channel by the maximum possible
	// return length so our goroutines don't block
	ch := make(chan interface{}, len(i.indices))

	for _, idx := range i.indices {
		// ignore indices which can't match this query
		if Overlap(queryBounds, idx) {
			// run all queries in linked goroutines (cancel after first err),
			// bounded by parallelism controls if applicable.
			g.Go(func() error {
				got, err := fn(ctx, idx)
				if err != nil {
					return err
				}
				ch <- got
				return nil
			})
		}
	}

	// wait for work to finish or error|ctx expiration
	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(ch)

	results := make([]interface{}, 0, len(i.indices))
	for x := range ch {
		results = append(results, x)
	}
	return results, nil
}

func (i *MultiIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	groups, err := i.forIndices(ctx, from, through, func(ctx context.Context, idx Index) (interface{}, error) {
		return idx.GetChunkRefs(ctx, userID, from, through, matchers...)
	})

	if err != nil {
		return nil, err
	}

	var maxLn int // maximum number of chunk refs, assuming no duplicates
	refGroups := make([][]ChunkRef, 0, len(i.indices))
	for _, group := range groups {
		rg := group.([]ChunkRef)
		maxLn += len(rg)
		refGroups = append(refGroups, rg)
	}
	// optimistically allocate the maximum length slice
	// to avoid growing incrementally
	results := make([]ChunkRef, 0, maxLn)

	// keep track of duplicates
	seen := make(map[ChunkRef]struct{})

	// TODO(owen-d): Do this more efficiently,
	// not all indices overlap each other
	for _, group := range refGroups {
		for _, ref := range group {
			_, ok := seen[ref]
			if ok {
				continue
			}
			seen[ref] = struct{}{}
			results = append(results, ref)
		}
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
