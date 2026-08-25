package cache_test

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/gomemcache/memcache"
)

type mockMemcache struct {
	sync.RWMutex
	contents map[string][]byte
}

func newMockMemcache() *mockMemcache {
	return &mockMemcache{
		contents: map[string][]byte{},
	}
}

func (m *mockMemcache) GetMulti(_ context.Context, keys []string, _ ...memcache.Option) (map[string]*memcache.Item, error) {
	m.RLock()
	defer m.RUnlock()
	result := map[string]*memcache.Item{}
	for _, k := range keys {
		if c, ok := m.contents[k]; ok {
			result[k] = &memcache.Item{
				Value: c,
			}
		}
	}
	return result, nil
}

func (m *mockMemcache) Set(item *memcache.Item) error {
	m.Lock()
	defer m.Unlock()
	m.contents[item.Key] = item.Value
	return nil
}

// delayedMockMemcache introduces a fixed delay before each GetMulti call,
// simulating a slow memcached server. If the context is cancelled during the
// delay it returns immediately with ctx.Err().
type delayedMockMemcache struct {
	mockMemcache
	delay time.Duration
}

func (m *delayedMockMemcache) GetMulti(ctx context.Context, keys []string, _ ...memcache.Option) (map[string]*memcache.Item, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.delay):
		return m.mockMemcache.GetMulti(ctx, keys)
	}
}
