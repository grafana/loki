package metadata

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefetchedRangesID_cacheKey(t *testing.T) {
	t.Run("the hashed key has the same length for a short and a long path, within the memcached limit", func(t *testing.T) {
		const memcachedMaxKeyBytes = 250
		id := prefetchedRangesID{Kind: kindColumn, ColumnName: "trace_id"}
		longPath := strings.Repeat("p", 400)

		_, short := id.cacheKey("objects/ab/cd")
		_, long := id.cacheKey(longPath)

		require.Len(t, long, len(short))
		require.LessOrEqual(t, len(long), memcachedMaxKeyBytes)
		require.NotContains(t, long, longPath)
		require.NotContains(t, short, "objects/ab/cd")
	})

	t.Run("the unhashed key holds the whole path and the whole column name", func(t *testing.T) {
		path := strings.Repeat("p", 400)

		unhashed, _ := prefetchedRangesID{Kind: kindColumn, ColumnName: "trace_id"}.cacheKey(path)

		require.Contains(t, unhashed, path)
		require.Contains(t, unhashed, "trace_id")
	})

	t.Run("the groups of one object have distinct unhashed and hashed keys", func(t *testing.T) {
		ids := []prefetchedRangesID{
			{Kind: kindFileMetadata},
			{Kind: kindCore},
			{Kind: kindMessage},
			{Kind: kindRegion},
			{Kind: kindColumn, ColumnName: "trace_id"},
			{Kind: kindColumn, ColumnName: "level"},
		}

		unhashedKeys, hashedKeys := map[string]struct{}{}, map[string]struct{}{}
		for _, id := range ids {
			unhashed, hashed := id.cacheKey("objects/ab/cd")
			unhashedKeys[unhashed], hashedKeys[hashed] = struct{}{}, struct{}{}
		}
		require.Len(t, unhashedKeys, len(ids))
		require.Len(t, hashedKeys, len(ids))
	})

	t.Run("groups of different objects have distinct unhashed keys when a name holds the separator", func(t *testing.T) {
		columnKey, _ := prefetchedRangesID{Kind: kindColumn, ColumnName: "b#core"}.cacheKey("a")
		coreKey, _ := prefetchedRangesID{Kind: kindCore}.cacheKey("a#col:b")

		require.NotEqual(t, columnKey, coreKey)
	})
}
