package storage

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

var ctx = user.InjectOrgID(context.Background(), "1")

type mockStore struct {
	chunk.IndexClient
	queries []chunk.IndexQuery
	results ReadBatch
}

func (m *mockStore) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	for _, query := range queries {
		callback(query, m.results)
	}
	m.queries = append(m.queries, queries...)
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
	logger := log.NewNopLogger()
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger)
	client := newCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
	queries := []chunk.IndexQuery{{
		TableName: "table",
		HashValue: "baz",
	}}
	err = client.QueryPages(ctx, queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(_ chunk.IndexQuery, _ chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))
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
	logger := log.NewNopLogger()
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger)
	client := newCachingIndexClient(store, cache, 100*time.Millisecond, limits, logger, false)
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
	assert.EqualValues(t, len(queries), len(store.queries))
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
	assert.EqualValues(t, len(queries), len(store.queries))
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
	assert.EqualValues(t, 2*len(queries), len(store.queries))
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
	logger := log.NewNopLogger()
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger)
	client := newCachingIndexClient(store, cache, 100*time.Millisecond, limits, logger, false)
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
	assert.EqualValues(t, len(queries), len(store.queries))
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
	assert.EqualValues(t, len(queries), len(store.queries))
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
	assert.EqualValues(t, len(queries), len(store.queries))
	assert.EqualValues(t, len(queries), results)
}

func TestCachingStorageClientEmptyResponse(t *testing.T) {
	store := &mockStore{}
	limits, err := defaultLimits()
	require.NoError(t, err)
	logger := log.NewNopLogger()
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger)
	client := newCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
	queries := []chunk.IndexQuery{{TableName: "table", HashValue: "foo"}}
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		assert.False(t, batch.Iterator().Next())
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		assert.False(t, batch.Iterator().Next())
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))
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
	logger := log.NewNopLogger()
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger)
	client := newCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
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
	assert.EqualValues(t, 1, len(store.queries))
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
	assert.EqualValues(t, 1, len(store.queries))
	assert.EqualValues(t, store.results, results)
}

type mockCache struct {
	storedKeys []string
	cache.Cache
}

func (m *mockCache) Store(ctx context.Context, keys []string, buf [][]byte) {
	m.storedKeys = append(m.storedKeys, keys...)
	m.Cache.Store(ctx, keys, buf)
}

func buildQueryKey(q chunk.IndexQuery) string {
	ret := q.TableName + sep + q.HashValue

	if len(q.RangeValuePrefix) != 0 {
		ret += sep + yoloString(q.RangeValuePrefix)
	}

	if len(q.ValueEqual) != 0 {
		ret += sep + yoloString(q.ValueEqual)
	}

	return ret
}

func TestCachingStorageClientStoreQueries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		queries []chunk.IndexQuery

		expectedStoreQueriesWithoutBroadQueriesDisabled []chunk.IndexQuery
		expectedStoreQueriesWithBroadQueriesDisabled    []chunk.IndexQuery
	}{
		{
			name: "TableName-HashValue queries",
			queries: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
		},
		{
			name: "TableName-HashValue-RangeValuePrefix queries",
			queries: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz")},
			},
		},
		{
			name: "TableName-HashValue-RangeValuePrefix-ValueEqual queries",
			queries: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz"), ValueEqual: []byte("two")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz"), ValueEqual: []byte("three")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz"), ValueEqual: []byte("two")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz"), ValueEqual: []byte("three")},
			},
		},
		{
			name: "TableName-HashValue-RangeValueStart queries",
			queries: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo", RangeValueStart: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValueStart: []byte("baz")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
			},
		},
		{
			name: "Duplicate queries",
			queries: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []chunk.IndexQuery{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
			},
		},
	} {
		for _, disableBroadQueries := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-%v", tc.name, disableBroadQueries), func(t *testing.T) {
				expectedStoreQueries := tc.expectedStoreQueriesWithoutBroadQueriesDisabled
				if disableBroadQueries {
					expectedStoreQueries = tc.expectedStoreQueriesWithBroadQueriesDisabled
				}
				expectedQueryKeysInCache := make([]string, 0, len(expectedStoreQueries))
				for _, query := range expectedStoreQueries {
					expectedQueryKeysInCache = append(expectedQueryKeysInCache, cache.HashKey(buildQueryKey(query)))
				}

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
				logger := log.NewNopLogger()
				cache := &mockCache{
					Cache: cache.NewFifoCache("test", cache.FifoCacheConfig{MaxSizeItems: 10, Validity: 10 * time.Second}, nil, logger),
				}
				client := newCachingIndexClient(store, cache, 1*time.Second, limits, logger, disableBroadQueries)
				var callbackQueries []chunk.IndexQuery

				err = client.QueryPages(ctx, tc.queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
					callbackQueries = append(callbackQueries, query)
					return true
				})
				require.NoError(t, err)

				// we do a callback per query sent not per query done to the index store. See if we got as many callbacks as the number of actual queries.
				sort.Slice(tc.queries, func(i, j int) bool {
					return buildQueryKey(tc.queries[i]) < buildQueryKey(tc.queries[j])
				})
				sort.Slice(callbackQueries, func(i, j int) bool {
					return buildQueryKey(callbackQueries[i]) < buildQueryKey(callbackQueries[j])
				})
				assert.EqualValues(t, tc.queries, callbackQueries)

				// sort the expected and actual queries before comparing
				sort.Slice(expectedStoreQueries, func(i, j int) bool {
					return buildQueryKey(expectedStoreQueries[i]) < buildQueryKey(expectedStoreQueries[j])
				})
				sort.Slice(store.queries, func(i, j int) bool {
					return buildQueryKey(store.queries[i]) < buildQueryKey(store.queries[j])
				})
				assert.EqualValues(t, expectedStoreQueries, store.queries)

				callbackQueries = callbackQueries[:0]
				// If we do the query to the cache again, the underlying store shouldn't see it.
				err = client.QueryPages(ctx, tc.queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
					callbackQueries = append(callbackQueries, query)
					return true
				})
				require.NoError(t, err)

				// verify the callback queries again
				sort.Slice(callbackQueries, func(i, j int) bool {
					return buildQueryKey(callbackQueries[i]) < buildQueryKey(callbackQueries[j])
				})
				assert.EqualValues(t, tc.queries, callbackQueries)

				assert.EqualValues(t, expectedStoreQueries, store.queries)

				// sort the expected and actual query keys in cache before comparing
				sort.Strings(expectedQueryKeysInCache)
				sort.Strings(cache.storedKeys)
				assert.EqualValues(t, expectedQueryKeysInCache, cache.storedKeys)
			})

		}
	}
}
