package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

func TestMemcached(t *testing.T) {
	t.Run("unbatched", func(t *testing.T) {
		client := newMockMemcache()
		memcache := cache.NewMemcached(cache.MemcachedConfig{}, client)

		testMemcache(t, memcache)
	})

	t.Run("batched", func(t *testing.T) {
		client := newMockMemcache()
		memcache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client)

		testMemcache(t, memcache)
	})
}

func testMemcache(t *testing.T, memcache *cache.Memcached) {
	numKeys := 1000

	ctx := context.Background()
	keys := make([]string, 0, numKeys)
	// Insert 1000 keys skipping all multiples of 5.
	for i := 0; i < numKeys; i++ {
		keys = append(keys, string(i))
		if i%5 == 0 {
			continue
		}

		require.NoError(t, memcache.Store(ctx, string(i), []byte(string(i))))
	}

	found, bufs, missing, err := memcache.Fetch(ctx, keys)
	require.NoError(t, err)
	for i := 0; i < numKeys; i++ {
		if i%5 == 0 {
			require.Equal(t, string(i), missing[0])
			missing = missing[1:]
			continue
		}

		require.Equal(t, string(i), found[0])
		require.Equal(t, string(i), string(bufs[0]))
		found = found[1:]
		bufs = bufs[1:]
	}
}
