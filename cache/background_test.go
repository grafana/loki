package cache_test

import (
	"testing"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
)

func TestBackground(t *testing.T) {
	c := cache.NewBackground("mock", cache.BackgroundConfig{
		WriteBackGoroutines: 1,
		WriteBackBuffer:     100,
	}, cache.NewMockCache())

	keys, chunks := fillCache(t, c)
	cache.Flush(c)

	testCacheSingle(t, c, keys, chunks)
	testCacheMultiple(t, c, keys, chunks)
	testCacheMiss(t, c)
}
