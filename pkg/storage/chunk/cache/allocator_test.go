package cache_test

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestWithAllocator(t *testing.T) {
	t.Run("memcached passes the allocator to the client", func(t *testing.T) {
		client := newMockMemcache()
		backend := cache.NewMemcached(cache.MemcachedConfig{}, client, "test", nil, log.NewNopLogger(), "test")
		alloc := &testAllocator{id: 1}

		_, _, _, err := backend.Fetch(t.Context(), []string{"key"}, cache.WithAllocator(alloc))

		require.NoError(t, err)
		require.Same(t, alloc, client.allocator)
	})

	t.Run("memcached passes the allocator to the client when it fetches in batches", func(t *testing.T) {
		client := newMockMemcache()
		backend := cache.NewMemcached(cache.MemcachedConfig{BatchSize: 1, Parallelism: 1}, client, "test", nil, log.NewNopLogger(), "test")
		defer backend.Stop()
		alloc := &testAllocator{id: 1}

		_, _, _, err := backend.Fetch(t.Context(), []string{"key"}, cache.WithAllocator(alloc))

		require.NoError(t, err)
		require.Same(t, alloc, client.allocator)
	})

	t.Run("a tiered cache does not pass the allocator to its tiers", func(t *testing.T) {
		client := newMockMemcache()
		backend := cache.NewMemcached(cache.MemcachedConfig{}, client, "test", nil, log.NewNopLogger(), "test")
		tiered := cache.NewTiered([]cache.Cache{cache.NewMockCache(), backend})

		_, _, _, err := tiered.Fetch(t.Context(), []string{"key"}, cache.WithAllocator(&testAllocator{id: 1}))

		require.NoError(t, err)
		require.Nil(t, client.allocator)
	})
}

type testAllocator struct{ id int }

func (a *testAllocator) Get(size int) *[]byte {
	b := make([]byte, size)
	return &b
}

func (a *testAllocator) Put(*[]byte) {}
