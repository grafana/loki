// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/inmemory_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

// Tests out the LRU cache implementation.
package cache

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/util/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRUCacheEviction(t *testing.T) {
	const (
		cnt     = 10
		evicted = 5
	)
	itemTemplate := &cacheEntry{
		key:   "00",
		value: []byte("00"),
	}
	entrySize := entryMemoryUsage(itemTemplate.key, itemTemplate.value)

	cfg := LRUCacheConfig{MaxSizeBytes: flagext.ByteSize(entrySize * cnt), MaxItems: cnt, MaxItemSizeBytes: flagext.ByteSize(entrySize + 1), Enabled: true}

	c, err := NewLRUCache("test-cache", cfg, nil, log.NewNopLogger(), "test")
	require.NoError(t, err)
	defer c.Stop()
	ctx := context.Background()

	// Check put / get works. Put/get 10 different items.
	keys := []string{}
	values := [][]byte{}
	for i := 0; i < cnt; i++ {
		key := fmt.Sprintf("%02d", i)
		value := make([]byte, len(key))
		copy(value, key)
		keys = append(keys, key)
		values = append(values, value)
	}
	require.NoError(t, c.Store(ctx, keys, values))
	require.Equal(t, cnt, c.lru.Len())

	assert.Equal(t, float64(10), testutil.ToFloat64(c.added), float64(10))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.evicted), float64(0))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.current), float64(cnt))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.requests), float64(0))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.totalMisses), float64(0))
	assert.Equal(t, float64(cnt*entryMemoryUsage(itemTemplate.key, itemTemplate.value)), testutil.ToFloat64(c.bytesInUse))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.overflow))

	for i := 0; i < cnt; i++ {
		key := fmt.Sprintf("%02d", i)
		value, ok := c.get(key)
		require.True(t, ok)
		require.Equal(t, []byte(key), value)
	}

	assert.Equal(t, float64(10), testutil.ToFloat64(c.added))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.evicted))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.current))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.requests))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(cnt*entrySize), testutil.ToFloat64(c.bytesInUse))

	// Check evictions
	keys = []string{}
	values = [][]byte{}
	for i := cnt - evicted; i < cnt+evicted; i++ {
		key := fmt.Sprintf("%02d", i)
		value := make([]byte, len(key))
		copy(value, key)
		keys = append(keys, key)
		values = append(values, value)
	}
	require.NoError(t, c.Store(ctx, keys, values))
	require.Equal(t, cnt, c.lru.Len())

	assert.Equal(t, float64(15), testutil.ToFloat64(c.added))
	assert.Equal(t, float64(evicted), testutil.ToFloat64(c.evicted))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.current))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.requests))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(cnt*entrySize), testutil.ToFloat64(c.bytesInUse))

	for i := 0; i < cnt-evicted; i++ {
		_, ok := c.get(fmt.Sprintf("%02d", i))
		require.False(t, ok)
	}
	for i := cnt - evicted; i < cnt+evicted; i++ {
		key := fmt.Sprintf("%02d", i)
		value, ok := c.get(key)
		require.True(t, ok)
		require.Equal(t, []byte(key), value)
	}

	assert.Equal(t, float64(15), testutil.ToFloat64(c.added))
	assert.Equal(t, float64(evicted), testutil.ToFloat64(c.evicted))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.current))
	assert.Equal(t, float64(cnt*2+evicted), testutil.ToFloat64(c.requests))
	assert.Equal(t, float64(cnt-evicted), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(cnt*entrySize), testutil.ToFloat64(c.bytesInUse))

	// Check updates work
	keys = []string{}
	values = [][]byte{}
	for i := cnt; i < cnt+evicted; i++ {
		keys = append(keys, fmt.Sprintf("%02d", i))
		vstr := fmt.Sprintf("%02d", i*2)
		value := make([]byte, len(vstr))
		copy(value, vstr)
		values = append(values, value)
	}
	assert.Equal(t, cnt, c.lru.Len())
	require.NoError(t, c.Store(ctx, keys, values))
	assert.Equal(t, cnt, c.lru.Len())

	assert.Equal(t, float64(0), testutil.ToFloat64(c.overflow))
	assert.Equal(t, float64(15), testutil.ToFloat64(c.added))
	assert.Equal(t, float64(evicted), testutil.ToFloat64(c.evicted))
	assert.Equal(t, float64(cnt), testutil.ToFloat64(c.current))
	assert.Equal(t, float64(cnt*2+evicted), testutil.ToFloat64(c.requests))
	assert.Equal(t, float64(cnt-evicted), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(cnt*entrySize), testutil.ToFloat64(c.bytesInUse))

	for i := cnt; i < cnt+evicted; i++ {
		value, ok := c.get(fmt.Sprintf("%02d", i))
		require.True(t, ok)
		require.Equal(t, []byte(fmt.Sprintf("%02d", i*2)), value)
	}

}
