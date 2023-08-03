package cache

import (
	"context"
	"sync"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

type MockCache interface {
	Cache
	NumKeyUpdates() int
	GetInternal() map[string][]byte
	KeysRequested() int
}

type mockCache struct {
	numKeyUpdates int
	keysRequested int
	sync.Mutex
	cache map[string][]byte
}

func (m *mockCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	m.Lock()
	defer m.Unlock()
	for i := range keys {
		m.cache[keys[i]] = bufs[i]
		m.numKeyUpdates++
	}
	return nil
}

func (m *mockCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	m.Lock()
	defer m.Unlock()
	for _, key := range keys {
		m.keysRequested++
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

func (m *mockCache) GetCacheType() stats.CacheType {
	return "mock"
}

func (m *mockCache) NumKeyUpdates() int {
	return m.numKeyUpdates
}

func (m *mockCache) GetInternal() map[string][]byte {
	return m.cache
}

func (m *mockCache) KeysRequested() int {
	return m.keysRequested
}

// NewMockCache makes a new MockCache.
func NewMockCache() MockCache {
	return &mockCache{
		cache: map[string][]byte{},
	}
}

// NewNoopCache returns a no-op cache.
func NewNoopCache() Cache {
	return NewTiered(nil)
}
