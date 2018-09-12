package cache_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
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

// mockMemcache whose calls fail 1/3rd of the time.
type mockMemcacheFailing struct {
	*mockMemcache
	calls uint64
}

func newMockMemcacheFailing() *mockMemcacheFailing {
	return &mockMemcacheFailing{
		mockMemcache: newMockMemcache(),
	}
}

func (c *mockMemcacheFailing) GetMulti(keys []string) (map[string]*memcache.Item, error) {
	calls := atomic.AddUint64(&c.calls, 1)
	if calls%3 == 0 {
		return nil, errors.New("fail")
	}

	return c.mockMemcache.GetMulti(keys)
}

func TestMemcacheFailure(t *testing.T) {
	t.Run("unbatched", func(t *testing.T) {
		client := newMockMemcacheFailing()
		memcache := cache.NewMemcached(cache.MemcachedConfig{}, client)

		testMemcacheFailing(t, memcache)
	})

	t.Run("batched", func(t *testing.T) {
		client := newMockMemcacheFailing()
		memcache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client)

		testMemcacheFailing(t, memcache)
	})
}

func testMemcacheFailing(t *testing.T, memcache *cache.Memcached) {
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

	for i := 0; i < 10; i++ {
		found, bufs, missing, _ := memcache.Fetch(ctx, keys)

		require.Equal(t, len(found), len(bufs))
		for i := range found {
			require.Equal(t, found[i], string(bufs[i]))
		}

		keysReturned := make(map[string]struct{})
		for _, key := range found {
			_, ok := keysReturned[key]
			require.False(t, ok, "duplicate key returned")

			keysReturned[key] = struct{}{}
		}
		for _, key := range missing {
			_, ok := keysReturned[key]
			require.False(t, ok, "duplicate key returned")

			keysReturned[key] = struct{}{}
		}

		for _, key := range keys {
			_, ok := keysReturned[key]
			require.True(t, ok, "key missing %s", key)
		}
	}
}
