package lazyquery

import (
	"context"
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/chunkstore"
)

// LazyQueryable wraps a storage.Queryable
type LazyQueryable struct {
	q storage.Queryable
}

// Querier implements storage.Queryable
func (lq LazyQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	q, err := lq.q.Querier(ctx, mint, maxt)
	if err != nil {
		return nil, err
	}

	return NewLazyQuerier(q), nil
}

// NewLazyQueryable returns a lazily wrapped queryable
func NewLazyQueryable(q storage.Queryable) storage.Queryable {
	return LazyQueryable{q}
}

// LazyQuerier is a lazy-loaded adapter for a storage.Querier
type LazyQuerier struct {
	next storage.Querier
}

// NewLazyQuerier wraps a storage.Querier, does the Select in the background.
// Return value cannot be used from more than one goroutine simultaneously.
func NewLazyQuerier(next storage.Querier) storage.Querier {
	return LazyQuerier{next}
}

// Select implements Storage.Querier
func (l LazyQuerier) Select(selectSorted bool, params *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	// make sure there is space in the buffer, to unblock the goroutine and let it die even if nobody is
	// waiting for the result yet (or anymore).
	future := make(chan storage.SeriesSet, 1)
	go func() {
		future <- l.next.Select(selectSorted, params, matchers...)
	}()

	return &lazySeriesSet{
		future: future,
	}
}

// LabelValues implements Storage.Querier
func (l LazyQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return l.next.LabelValues(name, matchers...)
}

// LabelNames implements Storage.Querier
func (l LazyQuerier) LabelNames() ([]string, storage.Warnings, error) {
	return l.next.LabelNames()
}

// Close implements Storage.Querier
func (l LazyQuerier) Close() error {
	return l.next.Close()
}

// Get implements chunk.Store for the chunk tar HTTP handler.
func (l LazyQuerier) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	store, ok := l.next.(chunkstore.ChunkStore)
	if !ok {
		return nil, fmt.Errorf("not supported")
	}

	return store.Get(ctx, userID, from, through, matchers...)
}

type lazySeriesSet struct {
	next   storage.SeriesSet
	future chan storage.SeriesSet
}

// Next implements storage.SeriesSet.  NB not thread safe!
func (s *lazySeriesSet) Next() bool {
	if s.next == nil {
		s.next = <-s.future
	}
	return s.next.Next()
}

// At implements storage.SeriesSet.
func (s *lazySeriesSet) At() storage.Series {
	if s.next == nil {
		s.next = <-s.future
	}
	return s.next.At()
}

// Err implements storage.SeriesSet.
func (s *lazySeriesSet) Err() error {
	if s.next == nil {
		s.next = <-s.future
	}
	return s.next.Err()
}

// Warnings implements storage.SeriesSet.
func (s *lazySeriesSet) Warnings() storage.Warnings {
	return nil
}
