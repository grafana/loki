package util

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/stretchr/testify/require"
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
