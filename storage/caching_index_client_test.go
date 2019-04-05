package storage

import (
	"context"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

var ctx = user.InjectOrgID(context.Background(), "1")

type mockStore struct {
	chunk.IndexClient
	queries int
	results ReadBatch
}

func (m *mockStore) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	for _, query := range queries {
		m.queries++
		callback(query, m.results)
	}
	return nil
}

func TestCachingStorageClientBasic(t *testing.T) {
	store := &mockStore{
		results: ReadBatch{
			Entries: []Entry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 10, Validity: 10 * time.Second})
	client := newCachingIndexClient(store, cache, 1*time.Second, limits)
	queries := []chunk.IndexQuery{{
		TableName: "table",
		HashValue: "baz",
	}}
	err = client.QueryPages(ctx, queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)
}

func TestTempCachingStorageClient(t *testing.T) {
	store := &mockStore{
		results: ReadBatch{
			Entries: []Entry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 10, Validity: 10 * time.Second})
	client := newCachingIndexClient(store, cache, 100*time.Millisecond, limits)
	queries := []chunk.IndexQuery{
		{TableName: "table", HashValue: "foo"},
		{TableName: "table", HashValue: "bar"},
		{TableName: "table", HashValue: "baz"},
	}
	results := 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	results = 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)

	// If we do the query after validity, it should see the queries.
	time.Sleep(100 * time.Millisecond)
	results = 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2*len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)
}

func TestPermCachingStorageClient(t *testing.T) {
	store := &mockStore{
		results: ReadBatch{
			Entries: []Entry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 10, Validity: 10 * time.Second})
	client := newCachingIndexClient(store, cache, 100*time.Millisecond, limits)
	queries := []chunk.IndexQuery{
		{TableName: "table", HashValue: "foo", Immutable: true},
		{TableName: "table", HashValue: "bar", Immutable: true},
		{TableName: "table", HashValue: "baz", Immutable: true},
	}
	results := 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	results = 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)

	// If we do the query after validity, it still shouldn't see the queries.
	time.Sleep(200 * time.Millisecond)
	results = 0
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, len(queries), store.queries)
	assert.EqualValues(t, len(queries), results)
}

func TestCachingStorageClientEmptyResponse(t *testing.T) {
	store := &mockStore{}
	limits, err := defaultLimits()
	require.NoError(t, err)
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 10, Validity: 10 * time.Second})
	client := newCachingIndexClient(store, cache, 1*time.Second, limits)
	queries := []chunk.IndexQuery{{TableName: "table", HashValue: "foo"}}
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		assert.False(t, batch.Iterator().Next())
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		assert.False(t, batch.Iterator().Next())
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)
}

func TestCachingStorageClientCollision(t *testing.T) {
	// These two queries should result in one query to the cache & index, but
	// two results, as we cache entire rows.
	store := &mockStore{
		results: ReadBatch{
			Entries: []Entry{
				{
					Column: []byte("bar"),
					Value:  []byte("bar"),
				},
				{
					Column: []byte("baz"),
					Value:  []byte("baz"),
				},
			},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 10, Validity: 10 * time.Second})
	client := newCachingIndexClient(store, cache, 1*time.Second, limits)
	queries := []chunk.IndexQuery{
		{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
		{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
	}

	var results ReadBatch
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results.Entries = append(results.Entries, Entry{
				Column: iter.RangeValue(),
				Value:  iter.Value(),
			})
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)
	assert.EqualValues(t, store.results, results)

	// If we do the query to the cache again, the underlying store shouldn't see it.
	results = ReadBatch{}
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results.Entries = append(results.Entries, Entry{
				Column: iter.RangeValue(),
				Value:  iter.Value(),
			})
		}
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, store.queries)
	assert.EqualValues(t, store.results, results)
}
