package tsdb

import (
	"context"
	"errors"

	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type MultiIndex struct {
	indices []Index
}

func NewMultiIndex(indices ...Index) (*MultiIndex, error) {
	if len(indices) == 0 {
		return nil, errors.New("must supply at least one index")
	}

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

func (i *MultiIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	for _, x := range i.indices {
		x.SetChunkFilterer(chunkFilter)
	}
}

func (i *MultiIndex) Close() error {
	var errs multierror.MultiError
	for _, idx := range i.indices {
		if err := idx.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs.Err()

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

			// must wrap g.Go in anonymous function to capture
			// idx variable during iteration
			func(idx Index) {
				g.Go(func() error {
					got, err := fn(ctx, idx)
					if err != nil {
						return err
					}
					ch <- got
					return nil
				})
			}(idx)

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

func (i *MultiIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	groups, err := i.forIndices(ctx, from, through, func(ctx context.Context, idx Index) (interface{}, error) {
		return idx.GetChunkRefs(ctx, userID, from, through, nil, shard, matchers...)
	})
	if err != nil {
		return nil, err
	}

	// keep track of duplicates
	seen := make(map[ChunkRef]struct{})

	// TODO(owen-d): Do this more efficiently,
	// not all indices overlap each other
	for _, group := range groups {
		g := group.([]ChunkRef)
		for _, ref := range g {
			_, ok := seen[ref]
			if ok {
				continue
			}
			seen[ref] = struct{}{}
			res = append(res, ref)
		}
		ChunkRefsPool.Put(g)
	}

	return res, nil
}

func (i *MultiIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	groups, err := i.forIndices(ctx, from, through, func(ctx context.Context, idx Index) (interface{}, error) {
		return idx.Series(ctx, userID, from, through, nil, shard, matchers...)
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[model.Fingerprint]struct{})

	for _, x := range groups {
		seriesSet := x.([]Series)
		for _, s := range seriesSet {
			_, ok := seen[s.Fingerprint]
			if ok {
				continue
			}
			seen[s.Fingerprint] = struct{}{}
			res = append(res, s)
		}
		SeriesPool.Put(seriesSet)
	}

	return res, nil
}

func (i *MultiIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	groups, err := i.forIndices(ctx, from, through, func(ctx context.Context, idx Index) (interface{}, error) {
		return idx.LabelNames(ctx, userID, from, through, matchers...)
	})
	if err != nil {
		return nil, err
	}

	var maxLn int // maximum number of chunk refs, assuming no duplicates
	xs := make([][]string, 0, len(i.indices))
	for _, group := range groups {
		x := group.([]string)
		maxLn += len(x)
		xs = append(xs, x)
	}

	// optimistically allocate the maximum length slice
	// to avoid growing incrementally
	results := make([]string, 0, maxLn)
	seen := make(map[string]struct{})

	for _, ls := range xs {
		for _, l := range ls {
			_, ok := seen[l]
			if ok {
				continue
			}
			seen[l] = struct{}{}
			results = append(results, l)
		}
	}

	return results, nil
}

func (i *MultiIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	groups, err := i.forIndices(ctx, from, through, func(ctx context.Context, idx Index) (interface{}, error) {
		return idx.LabelValues(ctx, userID, from, through, name, matchers...)
	})
	if err != nil {
		return nil, err
	}

	var maxLn int // maximum number of chunk refs, assuming no duplicates
	xs := make([][]string, 0, len(i.indices))
	for _, group := range groups {
		x := group.([]string)
		maxLn += len(x)
		xs = append(xs, x)
	}

	// optimistically allocate the maximum length slice
	// to avoid growing incrementally
	results := make([]string, 0, maxLn)
	seen := make(map[string]struct{})

	for _, ls := range xs {
		for _, l := range ls {
			_, ok := seen[l]
			if ok {
				continue
			}
			seen[l] = struct{}{}
			results = append(results, l)
		}
	}

	return results, nil
}
