package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

type mockStore struct {
	chunk.StorageClient
	queries int
}

func (m *mockStore) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	m.queries++
	for _, query := range queries {
		callback(query, mockReadBatch{})
	}
	return nil
}

type mockReadBatch struct{}

func (mockReadBatch) Iterator() chunk.ReadBatchIterator {
	return &mockReadBatchIterator{}
}

type mockReadBatchIterator struct {
	consumed bool
}

func (m *mockReadBatchIterator) Next() bool {
	if m.consumed {
		return false
	}
	m.consumed = true
	return true
}

func (mockReadBatchIterator) RangeValue() []byte {
	return []byte("foo")
}

func (mockReadBatchIterator) Value() []byte {
	return []byte("bar")
}

func TestCachingStorageClient(t *testing.T) {
	mock := &mockStore{}
	cache := cache.NewFifoCache("test", 10, 10*time.Second)
	client := newCachingStorageClient(mock, cache, 1*time.Second)
	queries := []chunk.IndexQuery{{
		TableName: "table",
		HashValue: "baz",
	}}
	err := client.QueryPages(context.Background(), queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, mock.queries)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(context.Background(), queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, mock.queries)
}
