package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

type mockStore struct {
	chunk.StorageClient
	queries int
}

func (m *mockStore) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	for _, query := range queries {
		m.queries++
		callback(query, mockReadBatch{
			rangeValue: []byte(query.HashValue),
			value:      []byte(query.HashValue),
		})
	}
	return nil
}

type mockReadBatch struct {
	rangeValue, value []byte
}

func (m mockReadBatch) Iterator() chunk.ReadBatchIterator {
	return &mockReadBatchIterator{
		mockReadBatch: m,
	}
}

type mockReadBatchIterator struct {
	consumed bool
	mockReadBatch
}

func (m *mockReadBatchIterator) Next() bool {
	if m.consumed {
		return false
	}
	m.consumed = true
	return true
}

func (m *mockReadBatchIterator) RangeValue() []byte {
	return m.mockReadBatch.rangeValue
}

func (m *mockReadBatchIterator) Value() []byte {
	return m.mockReadBatch.value
}

func TestCachingStorageClientBasic(t *testing.T) {
	store := &mockStore{}
	cache := cache.NewFifoCache("test", 10, 10*time.Second)
	client := newCachingStorageClient(store, cache, 1*time.Second)
	queries := []chunk.IndexQuery{{
		TableName: "table",
		HashValue: "baz",
	}}
	err := client.QueryPages(context.Background(), queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(context.Background(), queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)
}

func TestCachingStorageClient(t *testing.T) {
	store := &mockStore{}
	cache := cache.NewFifoCache("test", 10, 10*time.Second)
	client := newCachingStorageClient(store, cache, 1*time.Second)
	queries := []chunk.IndexQuery{
		{TableName: "table", HashValue: "foo"},
		{TableName: "table", HashValue: "bar"},
		{TableName: "table", HashValue: "baz"},
	}
	results := 0
	err := client.QueryPages(context.Background(), queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			assert.Equal(t, query.HashValue, string(iter.RangeValue()))
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	results = 0
	err = client.QueryPages(context.Background(), queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			assert.Equal(t, query.HashValue, string(iter.RangeValue()))
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)
}
