package util

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
)

type mockTableQuerier struct {
	sync.Mutex
	queries map[string]chunk.IndexQuery
}

func (m *mockTableQuerier) MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	m.Lock()
	defer m.Unlock()

	for _, query := range queries {
		m.queries[query.HashValue] = query
	}

	return nil
}

func (m *mockTableQuerier) hasQueries(t *testing.T, count int) {
	require.Len(t, m.queries, count)
	for i := 0; i < count; i++ {
		idx := strconv.Itoa(i)

		require.Equal(t, m.queries[idx], chunk.IndexQuery{
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
			queryCount: maxQueriesPerGoroutine / 2,
		},
		{
			name:       "queries = maxQueriesPerGoroutine",
			queryCount: maxQueriesPerGoroutine,
		},
		{
			name:       "queries > maxQueriesPerGoroutine",
			queryCount: maxQueriesPerGoroutine * 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := buildQueries(tc.queryCount)

			tableQuerier := mockTableQuerier{
				queries: map[string]chunk.IndexQuery{},
			}

			err := DoParallelQueries(context.Background(), &tableQuerier, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
				return false
			})
			require.NoError(t, err)

			tableQuerier.hasQueries(t, tc.queryCount)
		})
	}
}

func buildQueries(n int) []chunk.IndexQuery {
	queries := make([]chunk.IndexQuery, 0, n)
	for i := 0; i < n; i++ {
		idx := strconv.Itoa(i)
		queries = append(queries, chunk.IndexQuery{
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
			actualValues := map[string][][]byte{}
			deduper := NewIndexDeduper(func(query chunk.IndexQuery, readBatch chunk.ReadBatch) bool {
				itr := readBatch.Iterator()
				for itr.Next() {
					actualValues[query.HashValue] = append(actualValues[query.HashValue], itr.RangeValue())
				}
				return true
			})

			for _, batch := range tc.batches {
				deduper.Callback(chunk.IndexQuery{HashValue: batch.hashValue}, batch)
			}

			require.Equal(t, tc.expectedValues, actualValues)
		})
	}
}

type batch struct {
	hashValue   string
	rangeValues [][]byte
}

func (b batch) Iterator() chunk.ReadBatchIterator {
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
