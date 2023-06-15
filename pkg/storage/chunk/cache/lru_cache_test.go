// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/inmemory_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

// Tests out the LRU cache implementation.
package cache

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/go-kit/log"
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

	tests := []struct {
		name string
		cfg  LRUCacheConfig
	}{
		{
			name: "test-memory-eviction",
			cfg:  LRUCacheConfig{MaxSizeBytes: strconv.FormatInt(int64(cnt*sizeOf(itemTemplate)), 10)},
		},
	}

	for _, test := range tests {
		c, err := NewLRUCache(test.name, test.cfg, nil, log.NewNopLogger(), "test")
		require.NoError(t, err)
		ctx := context.Background()

		// Check put / get works
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
		require.Len(t, c.lru.Len(), cnt)

		assert.Equal(t, testutil.ToFloat64(c.added), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.evicted), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.current), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.requests), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.bytesInUse), float64(cnt*sizeOf(itemTemplate)))

		for i := 0; i < cnt; i++ {
			key := fmt.Sprintf("%02d", i)
			value, ok := c.get(key)
			require.True(t, ok)
			require.Equal(t, []byte(key), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.added), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.evicted), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.current), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.requests), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.bytesInUse), float64(cnt*sizeOf(itemTemplate)))

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
		err = c.Store(ctx, keys, values)
		require.NoError(t, err)
		require.Len(t, c.lru.Len(), cnt)

		assert.Equal(t, testutil.ToFloat64(c.added), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.evicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.current), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.requests), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.bytesInUse), float64(cnt*sizeOf(itemTemplate)))

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

		assert.Equal(t, testutil.ToFloat64(c.added), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.evicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.current), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.requests), float64(cnt*2+evicted))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
		assert.Equal(t, testutil.ToFloat64(c.bytesInUse), float64(cnt*sizeOf(itemTemplate)))

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
		err = c.Store(ctx, keys, values)
		require.NoError(t, err)
		require.Len(t, c.lru.Len(), cnt)

		for i := cnt; i < cnt+evicted; i++ {
			value, ok := c.get(fmt.Sprintf("%02d", i))
			require.True(t, ok)
			require.Equal(t, []byte(fmt.Sprintf("%02d", i*2)), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.added), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.evicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.current), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.requests), float64(cnt*2+evicted*2))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
		assert.Equal(t, testutil.ToFloat64(c.bytesInUse), float64(cnt*sizeOf(itemTemplate)))

		c.Stop()
	}
}
