package index_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/validation"
)

var ctx = user.InjectOrgID(context.Background(), "1")

const sep = "\xff"

type mockStore struct {
	index.Client
	queries []index.Query
	results index.ReadBatchResult
}

func (m *mockStore) QueryPages(_ context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	for _, query := range queries {
		callback(query, m.results)
	}
	m.queries = append(m.queries, queries...)
	return nil
}

func TestCachingStorageClientBasic(t *testing.T) {
	store := &mockStore{
		results: index.ReadBatch{
			Entries: []index.CacheEntry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	logger := log.NewNopLogger()
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test")
	client := index.NewCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
	queries := []index.Query{{
		TableName: "table",
		HashValue: "baz",
	}}
	err = client.QueryPages(ctx, queries, func(_ index.Query, _ index.ReadBatchResult) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(_ index.Query, _ index.ReadBatchResult) bool {
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))
}

func TestTempCachingStorageClient(t *testing.T) {
	store := &mockStore{
		results: index.ReadBatch{
			Entries: []index.CacheEntry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	logger := log.NewNopLogger()
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test")
	client := index.NewCachingIndexClient(store, cache, 100*time.Millisecond, limits, logger, false)
	queries := []index.Query{
		{TableName: "table", HashValue: "foo"},
		{TableName: "table", HashValue: "bar"},
		{TableName: "table", HashValue: "baz"},
	}
	results := 0
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
		results: index.ReadBatch{
			Entries: []index.CacheEntry{{
				Column: []byte("foo"),
				Value:  []byte("bar"),
			}},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	logger := log.NewNopLogger()
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test")
	client := index.NewCachingIndexClient(store, cache, 100*time.Millisecond, limits, logger, false)
	queries := []index.Query{
		{TableName: "table", HashValue: "foo", Immutable: true},
		{TableName: "table", HashValue: "bar", Immutable: true},
		{TableName: "table", HashValue: "baz", Immutable: true},
	}
	results := 0
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
	store := &mockStore{
		results: index.ReadBatch{
			Entries: []index.CacheEntry{},
		},
	}
	limits, err := defaultLimits()
	require.NoError(t, err)
	logger := log.NewNopLogger()
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test")
	client := index.NewCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
	queries := []index.Query{{TableName: "table", HashValue: "foo"}}
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
		assert.False(t, batch.Iterator().Next())
		return true
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, len(store.queries))

	// If we do the query to the cache again, the underlying store shouldn't see it.
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
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
		results: index.ReadBatch{
			Entries: []index.CacheEntry{
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
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test")
	client := index.NewCachingIndexClient(store, cache, 1*time.Second, limits, logger, false)
	queries := []index.Query{
		{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
		{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
	}

	var results index.ReadBatch
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results.Entries = append(results.Entries, index.CacheEntry{
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
	results = index.ReadBatch{}
	err = client.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results.Entries = append(results.Entries, index.CacheEntry{
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

func (m *mockCache) Store(ctx context.Context, keys []string, buf [][]byte) error {
	m.storedKeys = append(m.storedKeys, keys...)
	return m.Cache.Store(ctx, keys, buf)
}

func buildQueryKey(q index.Query) string {
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
		queries []index.Query

		expectedStoreQueriesWithoutBroadQueriesDisabled []index.Query
		expectedStoreQueriesWithBroadQueriesDisabled    []index.Query
	}{
		{
			name: "TableName-HashValue queries",
			queries: []index.Query{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "bar"},
			},
		},
		{
			name: "TableName-HashValue-RangeValuePrefix queries",
			queries: []index.Query{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz")},
			},
		},
		{
			name: "TableName-HashValue-RangeValuePrefix-ValueEqual queries",
			queries: []index.Query{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz"), ValueEqual: []byte("two")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz"), ValueEqual: []byte("three")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("baz"), ValueEqual: []byte("two")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("taz"), ValueEqual: []byte("three")},
			},
		},
		{
			name: "TableName-HashValue-RangeValueStart queries",
			queries: []index.Query{
				{TableName: "table", HashValue: "foo", RangeValueStart: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValueStart: []byte("baz")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
			},
		},
		{
			name: "Duplicate queries",
			queries: []index.Query{
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "foo"},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
				{TableName: "table", HashValue: "foo", RangeValuePrefix: []byte("bar"), ValueEqual: []byte("one")},
			},
			expectedStoreQueriesWithoutBroadQueriesDisabled: []index.Query{
				{TableName: "table", HashValue: "foo"},
			},
			expectedStoreQueriesWithBroadQueriesDisabled: []index.Query{
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
					results: index.ReadBatch{
						Entries: []index.CacheEntry{
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
					Cache: cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 10, TTL: 10 * time.Second}, nil, logger, "test"),
				}
				client := index.NewCachingIndexClient(store, cache, 1*time.Second, limits, logger, disableBroadQueries)
				var callbackQueries []index.Query

				err = client.QueryPages(ctx, tc.queries, func(query index.Query, _ index.ReadBatchResult) bool {
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
				err = client.QueryPages(ctx, tc.queries, func(query index.Query, _ index.ReadBatchResult) bool {
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

func yoloString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

func defaultLimits() (*validation.Overrides, error) {
	var defaults validation.Limits
	flagext.DefaultValues(&defaults)
	defaults.CardinalityLimit = 5
	return validation.NewOverrides(defaults, nil)
}
