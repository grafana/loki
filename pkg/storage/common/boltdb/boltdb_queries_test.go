package boltdb

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockTableQuerier struct {
	sync.Mutex
	queries map[string]Query
}

func (m *mockTableQuerier) MultiQueries(_ context.Context, queries []Query, _ QueryPagesCallback) error {
	m.Lock()
	defer m.Unlock()

	for _, query := range queries {
		m.queries[query.HashValue] = query
	}

	return nil
}

func (m *mockTableQuerier) hasQueries(t *testing.T, count int) {
	require.Len(t, m.queries, count)
	for i := range count {
		idx := strconv.Itoa(i)

		require.Equal(t, m.queries[idx], Query{
			HashValue:  idx,
			ValueEqual: []byte(idx),
		})
	}
}

func TestDoParallelQueries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		queryCount int
	}{
		{
			name:       "queries < maxQueriesPerGoroutine",
			queryCount: maxQueriesBatch / 2,
		},
		{
			name:       "queries = maxQueriesPerGoroutine",
			queryCount: maxQueriesBatch,
		},
		{
			name:       "queries > maxQueriesPerGoroutine",
			queryCount: maxQueriesBatch * 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := buildQueries(tc.queryCount)

			tableQuerier := mockTableQuerier{
				queries: map[string]Query{},
			}

			err := doParallelQueries(context.Background(), tableQuerier.MultiQueries, queries, func(_ Query, _ ReadBatchResult) bool {
				return false
			})
			require.NoError(t, err)

			tableQuerier.hasQueries(t, tc.queryCount)
		})
	}
}

func buildQueries(n int) []Query {
	queries := make([]Query, 0, n)
	for i := range n {
		idx := strconv.Itoa(i)
		queries = append(queries, Query{
			HashValue:  idx,
			ValueEqual: []byte(idx),
		})
	}

	return queries
}

func TestIndexDeduper(t *testing.T) {
	for _, tc := range []struct {
		name           string
		batches        []batch
		expectedValues map[string][][]byte
	}{
		{
			name: "single batch",
			batches: []batch{
				{
					hashValue:   "1",
					rangeValues: [][]byte{[]byte("a"), []byte("b")},
				},
			},
			expectedValues: map[string][][]byte{
				"1": {[]byte("a"), []byte("b")},
			},
		},
		{
			name: "multiple batches, no duplicates",
			batches: []batch{
				{
					hashValue:   "1",
					rangeValues: [][]byte{[]byte("a"), []byte("b")},
				},
				{
					hashValue:   "2",
					rangeValues: [][]byte{[]byte("c"), []byte("d")},
				},
			},
			expectedValues: map[string][][]byte{
				"1": {[]byte("a"), []byte("b")},
				"2": {[]byte("c"), []byte("d")},
			},
		},
		{
			name: "duplicate rangeValues but different hashValues",
			batches: []batch{
				{
					hashValue:   "1",
					rangeValues: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
				},
				{
					hashValue:   "2",
					rangeValues: [][]byte{[]byte("a"), []byte("b")},
				},
			},
			expectedValues: map[string][][]byte{
				"1": {[]byte("a"), []byte("b"), []byte("c")},
				"2": {[]byte("a"), []byte("b")},
			},
		},
		{
			name: "duplicate rangeValues in same hashValues",
			batches: []batch{
				{
					hashValue:   "1",
					rangeValues: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
				},
				{
					hashValue:   "1",
					rangeValues: [][]byte{[]byte("a"), []byte("b"), []byte("d")},
				},
			},
			expectedValues: map[string][][]byte{
				"1": {[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("sync", func(t *testing.T) {
				actualValues := map[string][][]byte{}
				deduper := NewSyncCallbackDeduper(func(query Query, readBatch ReadBatchResult) bool {
					itr := readBatch.Iterator()
					for itr.Next() {
						actualValues[query.HashValue] = append(actualValues[query.HashValue], itr.RangeValue())
					}
					return true
				}, 0)

				for _, batch := range tc.batches {
					deduper(Query{HashValue: batch.hashValue}, batch)
				}

				require.Equal(t, tc.expectedValues, actualValues)
			})

			t.Run("nosync", func(t *testing.T) {
				actualValues := map[string][][]byte{}
				deduper := NewCallbackDeduper(func(query Query, readBatch ReadBatchResult) bool {
					itr := readBatch.Iterator()
					for itr.Next() {
						actualValues[query.HashValue] = append(actualValues[query.HashValue], itr.RangeValue())
					}
					return true
				}, 0)

				for _, batch := range tc.batches {
					deduper(Query{HashValue: batch.hashValue}, batch)
				}

				require.Equal(t, tc.expectedValues, actualValues)
			})
		})
	}
}

type batch struct {
	hashValue   string
	rangeValues [][]byte
}

func (b batch) Iterator() ReadBatchIterator {
	return &batchIterator{
		rangeValues: b.rangeValues,
	}
}

type batchIterator struct {
	rangeValues [][]byte
	idx         int
}

func (b *batchIterator) Next() bool {
	if b.idx >= len(b.rangeValues) {
		return false
	}

	b.idx++
	return true
}

func (b batchIterator) RangeValue() []byte {
	return b.rangeValues[b.idx-1]
}

func (b batchIterator) Value() []byte {
	panic("implement me")
}

func Benchmark_DedupeCallback(b *testing.B) {
	deduper := NewCallbackDeduper(func(_ Query, readBatch ReadBatchResult) bool {
		itr := readBatch.Iterator()
		for itr.Next() {
			_ = itr.RangeValue()
		}
		return true
	}, 1)
	q := Query{HashValue: "1"}
	batch1 := batch{
		hashValue:   "1",
		rangeValues: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deduper(q, batch1)
	}
}

type TableQuerierFunc func(ctx context.Context, queries []Query, callback QueryPagesCallback) error

func (f TableQuerierFunc) MultiQueries(ctx context.Context, queries []Query, callback QueryPagesCallback) error {
	return f(ctx, queries, callback)
}

func Benchmark_MultiQueries(b *testing.B) {
	benchmarkMultiQueries(b, 50)
	benchmarkMultiQueries(b, 100)
	benchmarkMultiQueries(b, 1000)
	benchmarkMultiQueries(b, 10000)
	benchmarkMultiQueries(b, 50000)
}

func benchmarkMultiQueries(b *testing.B, n int) {
	b.Run(strconv.Itoa(n), func(b *testing.B) {
		callback := QueryPagesCallback(func(_ Query, readBatch ReadBatchResult) bool {
			itr := readBatch.Iterator()
			for itr.Next() {
				_ = itr.RangeValue()
			}
			return true
		})
		queries := make([]Query, n)
		for i := range queries {
			queries[i] = Query{HashValue: strconv.Itoa(i)}
		}
		ranges := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = doParallelQueries(ctx, func(_ context.Context, queries []Query, callback QueryPagesCallback) error {
				for _, query := range queries {
					callback(query, batch{
						hashValue:   query.HashValue,
						rangeValues: ranges,
					})
					callback(query, batch{
						hashValue:   query.HashValue,
						rangeValues: ranges,
					})
					callback(query, batch{
						hashValue:   query.HashValue,
						rangeValues: ranges,
					})
					callback(query, batch{
						hashValue:   query.HashValue,
						rangeValues: ranges,
					})
				}
				return nil
			}, queries, callback)
		}
	})
}
