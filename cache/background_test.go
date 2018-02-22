package cache_test

import (
	"context"
	"sync"
	"testing"

	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

type mockCache struct {
	sync.Mutex
	cache map[string][]byte
}

func (m *mockCache) StoreChunk(_ context.Context, key string, buf []byte) error {
	m.Lock()
	defer m.Unlock()
	m.cache[key] = buf
	return nil
}

func (m *mockCache) FetchChunkData(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	m.Lock()
	defer m.Unlock()
	for _, key := range keys {
		buf, ok := m.cache[key]
		if ok {
			found = append(found, key)
			bufs = append(bufs, buf)
		} else {
			missing = append(missing, key)
		}
	}
	return
}

func (m *mockCache) Stop() error {
	return nil
}

func newMockCache() cache.Cache {
	return &mockCache{
		cache: map[string][]byte{},
	}
}

func TestBackground(t *testing.T) {
	c := cache.NewBackground(cache.BackgroundConfig{
		WriteBackGoroutines: 1,
		WriteBackBuffer:     100,
	}, newMockCache())

	keys, chunks := fillCache(t, c)
	cache.Flush(c)

	testCacheSingle(t, c, keys, chunks)
	testCacheMultiple(t, c, keys, chunks)
	testCacheMiss(t, c)
}
