package tsdb

import (
	"context"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type MultiIndex struct {
	iter        IndexIter
	filterer    chunk.RequestChunkFilterer
	maxParallel int
}

type IndexIter interface {
	// For may be executed concurrently,
	// but all work must complete before
	// it returns.
	// TODO(owen-d|sandeepsukhani):
	// Lazy iteration may touch different index files within the same index query.
	// `For` e.g, Bounds and GetChunkRefs might go through different index files
	// if a sync happened between the calls.
	// The second parameter sets a limit on the number of indexes iterated concurrently.
	For(context.Context, int, func(context.Context, Index) error) error
}

type IndexSlice []Index

func (xs IndexSlice) For(ctx context.Context, maxConcurrent int, fn func(context.Context, Index) error) error {
	if len(xs) == 0 {
		return nil
	}

	// shortcut; if there's only one slice, there's no need for bounded concurrency
	if len(xs) == 1 {
		return fn(ctx, xs[0])
	}

	g, ctx := errgroup.WithContext(ctx)
	if maxConcurrent == 0 {
		panic("maxConcurrent cannot be 0, IndexIter is being called with a maxConcurrent of 0")
	}
	g.SetLimit(maxConcurrent)
	for i := range xs {
		x := xs[i]
		g.Go(func() error {
			return fn(ctx, x)
		})
	}
	return g.Wait()
}

func NewMultiIndex(i IndexIter) *MultiIndex {
	maxConcurrent := runtime.GOMAXPROCS(0) / 2
	if maxConcurrent == 0 {
		maxConcurrent = 1
	}
	return &MultiIndex{
		iter:        i,
		maxParallel: maxConcurrent,
	}
}

func (i *MultiIndex) Bounds() (model.Time, model.Time) {
	var lowest, highest model.Time
	var mtx sync.Mutex

	_ = i.forMatchingIndices(
		context.Background(),
		0, math.MaxInt64,
		func(_ context.Context, idx Index) error {
			from, through := idx.Bounds()

			mtx.Lock()
			defer mtx.Unlock()

			if lowest == 0 || from < lowest {
				lowest = from
			}

			if highest == 0 || through > highest {
				highest = through
			}
			return nil
		},
	)

	return lowest, highest
}

func (i *MultiIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.filterer = chunkFilter
}

func (i *MultiIndex) Close() error {
	return i.forMatchingIndices(
		context.Background(),
		0, math.MaxInt64,
		func(_ context.Context, idx Index) error {
			return idx.Close()
		},
	)
}

func (i *MultiIndex) forMatchingIndices(ctx context.Context, from, through model.Time, f func(context.Context, Index) error) error {
	queryBounds := newBounds(from, through)

	return i.iter.For(ctx, i.maxParallel, func(ctx context.Context, idx Index) error {
		if Overlap(idx, queryBounds) {

			if i.filterer != nil {
				// TODO(owen-d): Find a nicer way
				// to handle filterer passing. Doing it
				// in the read path rather than during instantiation
				// feels bad :(
				idx.SetChunkFilterer(i.filterer)
			}

			return f(ctx, idx)
		}
		return nil
	})

}

func (i *MultiIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	acc := newResultAccumulator(func(xs [][]ChunkRef) ([]ChunkRef, error) {
		if res == nil {
			res = ChunkRefsPool.Get()
		}
		res = res[:0]

		// keep track of duplicates
		seen := make(map[ChunkRef]struct{})

		// TODO(owen-d): Do this more efficiently,
		// not all indices overlap each other
		// TODO(owen-d): loser-tree or some other heap?

		for _, group := range xs {
			g := group
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

		sort.Slice(res, func(i, j int) bool { return res[i].Less(res[j]) })

		return res, nil
	})

	if err := i.forMatchingIndices(
		ctx,
		from,
		through,
		func(ctx context.Context, idx Index) error {
			got, err := idx.GetChunkRefs(ctx, userID, from, through, nil, fpFilter, matchers...)
			if err != nil {
				return err
			}
			acc.Add(got)
			return nil
		},
	); err != nil {
		return nil, err
	}

	merged, err := acc.Merge()
	if err != nil {
		if err == ErrEmptyAccumulator {
			return nil, nil
		}
		return nil, err
	}
	return merged, nil

}

func (i *MultiIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	acc := newResultAccumulator(func(xs [][]Series) ([]Series, error) {
		if res == nil {
			res = SeriesPool.Get()
		}
		res = res[:0]

		seen := make(map[model.Fingerprint]struct{})

		for _, x := range xs {
			seriesSet := x
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
	})

	if err := i.forMatchingIndices(
		ctx,
		from,
		through,
		func(ctx context.Context, idx Index) error {
			got, err := idx.Series(ctx, userID, from, through, nil, fpFilter, matchers...)
			if err != nil {
				return err
			}
			acc.Add(got)
			return nil
		},
	); err != nil {
		return nil, err
	}

	merged, err := acc.Merge()
	if err != nil {
		if err == ErrEmptyAccumulator {
			return nil, nil
		}
		return nil, err
	}
	return merged, nil
}

func (i *MultiIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	acc := newResultAccumulator(func(xs [][]string) ([]string, error) {
		var (
			maxLn int // maximum number of lNames, assuming no duplicates
			lists [][]string
		)
		for _, group := range xs {
			x := group
			maxLn += len(x)
			lists = append(lists, x)
		}

		// optimistically allocate the maximum length slice
		// to avoid growing incrementally
		// TODO(owen-d): use pool
		results := make([]string, 0, maxLn)
		seen := make(map[string]struct{})

		for _, ls := range lists {
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
	})

	if err := i.forMatchingIndices(
		ctx,
		from,
		through,
		func(ctx context.Context, idx Index) error {
			got, err := idx.LabelNames(ctx, userID, from, through, matchers...)
			if err != nil {
				return err
			}
			acc.Add(got)
			return nil
		},
	); err != nil {
		return nil, err
	}

	merged, err := acc.Merge()
	if err != nil {
		if err == ErrEmptyAccumulator {
			return nil, nil
		}
		return nil, err
	}
	return merged, nil
}

func (i *MultiIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	acc := newResultAccumulator(func(xs [][]string) ([]string, error) {
		var (
			maxLn int // maximum number of lValues, assuming no duplicates
			lists [][]string
		)
		for _, group := range xs {
			x := group
			maxLn += len(x)
			lists = append(lists, x)
		}

		// optimistically allocate the maximum length slice
		// to avoid growing incrementally
		// TODO(owen-d): use pool
		results := make([]string, 0, maxLn)
		seen := make(map[string]struct{})

		for _, ls := range lists {
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
	})

	if err := i.forMatchingIndices(
		ctx,
		from,
		through,
		func(ctx context.Context, idx Index) error {
			got, err := idx.LabelValues(ctx, userID, from, through, name, matchers...)
			if err != nil {
				return err
			}
			acc.Add(got)
			return nil
		},
	); err != nil {
		return nil, err
	}

	merged, err := acc.Merge()
	if err != nil {
		if err == ErrEmptyAccumulator {
			return nil, nil
		}
		return nil, err
	}
	return merged, nil
}

func (i *MultiIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	return i.forMatchingIndices(ctx, from, through, func(ctx context.Context, idx Index) error {
		return idx.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, matchers...)
	})
}

func (i *MultiIndex) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	return i.forMatchingIndices(ctx, from, through, func(ctx context.Context, idx Index) error {
		return idx.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, matchers...)
	})
}

func (i MultiIndex) ForSeries(ctx context.Context, userID string, fpFilter index.FingerprintFilter, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	return i.forMatchingIndices(ctx, from, through, func(ctx context.Context, idx Index) error {
		return idx.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
	})
}
