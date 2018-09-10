package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

func TestTieredSimple(t *testing.T) {
	for i := 1; i < 10; i++ {
		caches := []cache.Cache{}
		for j := 0; j <= i; j++ {
			caches = append(caches, newMockCache())
		}
		cache := cache.NewTiered(caches)
		testCache(t, cache)
	}
}

func TestTiered(t *testing.T) {
	level1, level2 := newMockCache(), newMockCache()
	cache := cache.NewTiered([]cache.Cache{level1, level2})

	err := level1.Store(context.Background(), "key1", []byte("hello"))
	require.NoError(t, err)

	err = level2.Store(context.Background(), "key2", []byte("world"))
	require.NoError(t, err)

	keys, bufs, missing, err := cache.Fetch(context.Background(), []string{"key1", "key2", "key3"})
	require.NoError(t, err)
	require.Equal(t, []string{"key1", "key2"}, keys)
	require.Equal(t, [][]byte{[]byte("hello"), []byte("world")}, bufs)
	require.Equal(t, []string{"key3"}, missing)
}
