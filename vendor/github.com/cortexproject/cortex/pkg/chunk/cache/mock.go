package cache

import (
	"context"
	"sync"
)

type mockCache struct {
	sync.Mutex
	cache map[string][]byte
}

func (m *mockCache) Store(_ context.Context, keys []string, bufs [][]byte) {
	m.Lock()
	defer m.Unlock()
	for i := range keys {
		m.cache[keys[i]] = bufs[i]
	}
}

func (m *mockCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string) {
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

func (m *mockCache) Stop() {
}

// NewMockCache makes a new MockCache.
func NewMockCache() Cache {
	return &mockCache{
		cache: map[string][]byte{},
	}
}

// NewNoopCache returns a no-op cache.
func NewNoopCache() Cache {
	return NewTiered(nil)
}
