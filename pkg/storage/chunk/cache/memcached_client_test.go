package cache_test

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/grafana/gomemcache/memcache"
)

// mockMemcache keeps each item under its shard key and its item key, so a lookup
// finds an item only when it uses the shard key of the Set call.
type mockMemcache struct {
	sync.Mutex
	contents      map[mockMemcacheItemKey][]byte
	getMultiCalls [][]string
}

type mockMemcacheItemKey struct {
	shardKey string
	itemKey  string
}

func newMockMemcache() *mockMemcache {
	return &mockMemcache{
		contents: map[mockMemcacheItemKey][]byte{},
	}
}

func (m *mockMemcache) GetMulti(_ context.Context, keys []string, opts ...memcache.Option) (map[string]*memcache.Item, error) {
	var o memcache.Options
	for _, opt := range opts {
		opt(&o)
	}

	m.Lock()
	defer m.Unlock()
	m.getMultiCalls = append(m.getMultiCalls, keys)
	result := map[string]*memcache.Item{}
	for _, k := range keys {
		if c, ok := m.contents[mockMemcacheItemKey{o.ShardKey, k}]; ok {
			result[k] = &memcache.Item{
				Value: c,
			}
		}
	}
	return result, nil
}

func (m *mockMemcache) Set(item *memcache.Item, opts ...memcache.Option) error {
	var o memcache.Options
	for _, opt := range opts {
		opt(&o)
	}

	m.Lock()
	defer m.Unlock()
	m.contents[mockMemcacheItemKey{o.ShardKey, item.Key}] = item.Value
	return nil
}

// getMultiBatchSizes returns the number of keys of each GetMulti batch, in
// ascending order.
func (m *mockMemcache) getMultiBatchSizes() []int {
	m.Lock()
	defer m.Unlock()
	sizes := make([]int, 0, len(m.getMultiCalls))
	for _, keys := range m.getMultiCalls {
		sizes = append(sizes, len(keys))
	}
	slices.Sort(sizes)
	return sizes
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
