package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestTieredSimple(t *testing.T) {
	for i := 1; i < 10; i++ {
		caches := []cache.Cache{}
		for j := 0; j <= i; j++ {
			caches = append(caches, cache.NewMockCache())
		}
		cache := cache.NewTiered(caches)
		testCache(t, cache)
	}
}

func TestTiered(t *testing.T) {
	level1, level2 := cache.NewMockCache(), cache.NewMockCache()
	cache := cache.NewTiered([]cache.Cache{level1, level2})

	err := level1.Store(context.Background(), []string{"key1"}, [][]byte{[]byte("hello")})
	require.NoError(t, err)
	err = level2.Store(context.Background(), []string{"key2"}, [][]byte{[]byte("world")})
	require.NoError(t, err)

	keys, bufs, missing, _ := cache.Fetch(context.Background(), []string{"key1", "key2", "key3"})
	require.Equal(t, []string{"key1", "key2"}, keys)
	require.Equal(t, [][]byte{[]byte("hello"), []byte("world")}, bufs)
	require.Equal(t, []string{"key3"}, missing)
}
