package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedCacheEviction(t *testing.T) {
	const (
		cnt     = 10
		evicted = 5
	)

	// compute value size such that 10 entries account to exactly 1MB.
	// adding one more entry to the cache would result in eviction when MaxSizeMB is configured to a value of 1.
	// value cap = target size of each entry (0.1MB) - size of cache entry with empty value.
	valueCap := (1e6 / cnt) - sizeOf(&Entry[string, []byte]{
		Key: "00",
	})

	itemTemplate := &Entry[string, []byte]{
		Key:   "00",
		Value: make([]byte, 0, valueCap),
	}

	tests := []struct {
		name string
		cfg  EmbeddedCacheConfig
	}{
		{
			name: "test-memory-eviction",
			cfg:  EmbeddedCacheConfig{MaxSizeMB: 1, TTL: 1 * time.Minute},
		},
		{
			name: "test-items-eviction",
			cfg:  EmbeddedCacheConfig{MaxSizeItems: cnt, TTL: 1 * time.Minute},
		},
	}

	for _, test := range tests {
		removedEntriesCount := atomic.NewInt64(0)
		onEntryRemoved := func(_ string, _ []byte) {
			removedEntriesCount.Inc()
		}
		c := NewTypedEmbeddedCache[string, []byte](test.name, test.cfg, nil, log.NewNopLogger(), "test", sizeOf, onEntryRemoved)
		ctx := context.Background()

		// Check put / get works
		keys := []string{}
		values := [][]byte{}
		for i := 0; i < cnt; i++ {
			key := fmt.Sprintf("%02d", i)
			value := make([]byte, len(key), valueCap)
			copy(value, key)
			keys = append(keys, key)
			values = append(values, value)
		}
		err := c.Store(ctx, keys, values)
		require.NoError(t, err)
		require.Len(t, c.entries, cnt)

		reason := fullReason

		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(replacedReason)), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		for i := 0; i < cnt; i++ {
			key := fmt.Sprintf("%02d", i)
			value, ok := c.Get(ctx, key)
			require.True(t, ok)
			require.Equal(t, []byte(key), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(replacedReason)), float64(0))
		assert.Equal(t, int64(0), removedEntriesCount.Load())
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		// Check evictions
		keys = []string{}
		values = [][]byte{}
		for i := cnt - evicted; i < cnt+evicted; i++ {
			key := fmt.Sprintf("%02d", i)
			value := make([]byte, len(key), valueCap)
			copy(value, key)
			keys = append(keys, key)
			values = append(values, value)
		}
		err = c.Store(ctx, keys, values)
		require.NoError(t, err)
		require.Len(t, c.entries, cnt)

		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(replacedReason)), float64(evicted))
		assert.Equalf(t, int64(evicted+evicted), removedEntriesCount.Load(), "%d items were evicted and %d items were replaced", evicted, evicted)
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		for i := 0; i < cnt-evicted; i++ {
			_, ok := c.Get(ctx, fmt.Sprintf("%02d", i))
			require.False(t, ok)
		}
		for i := cnt - evicted; i < cnt+evicted; i++ {
			key := fmt.Sprintf("%02d", i)
			value, ok := c.Get(ctx, key)
			require.True(t, ok)
			require.Equal(t, []byte(key), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(replacedReason)), float64(evicted))
		assert.Equal(t, int64(evicted+evicted), removedEntriesCount.Load(), "During this step the count of the calls must not be changed because we do read-only operations")
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		// Check updates work
		keys = []string{}
		values = [][]byte{}
		for i := cnt; i < cnt+evicted; i++ {
			keys = append(keys, fmt.Sprintf("%02d", i))
			vstr := fmt.Sprintf("%02d", i*2)
			value := make([]byte, len(vstr), valueCap)
			copy(value, vstr)
			values = append(values, value)
		}
		err = c.Store(ctx, keys, values)
		require.NoError(t, err)
		require.Len(t, c.entries, cnt)

		for i := cnt; i < cnt+evicted; i++ {
			value, ok := c.Get(ctx, fmt.Sprintf("%02d", i))
			require.True(t, ok)
			require.Equal(t, []byte(fmt.Sprintf("%02d", i*2)), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equalf(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(replacedReason)), float64(evicted+evicted),
			"During this step we replace %d more items", evicted)
		assert.Equalf(t, int64(evicted+evicted+evicted), removedEntriesCount.Load(), "During this step we replace %d more items", evicted)
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		c.Stop()
		assert.Equal(t, int64(evicted*3), removedEntriesCount.Load(), "onEntryRemoved must not be called for the items during the stop")
	}
}

func TestEmbeddedCacheExpiry(t *testing.T) {
	key1, key2, key3, key4 := "01", "02", "03", "04"
	data1, data2, data3, data4 := genBytes(32), genBytes(64), genBytes(128), genBytes(32)

	memorySz := sizeOf(&Entry[string, []byte]{Key: key1, Value: data1}) +
		sizeOf(&Entry[string, []byte]{Key: key2, Value: data2}) +
		sizeOf(&Entry[string, []byte]{Key: key3, Value: data3})

	cfg := EmbeddedCacheConfig{
		MaxSizeItems:  3,
		TTL:           100 * time.Millisecond,
		PurgeInterval: 50 * time.Millisecond,
	}

	removedEntriesCount := atomic.NewInt64(0)
	onEntryRemoved := func(_ string, _ []byte) {
		removedEntriesCount.Inc()
	}
	c := NewTypedEmbeddedCache[string, []byte]("cache_exprity_test", cfg, nil, log.NewNopLogger(), "test", sizeOf, onEntryRemoved)
	ctx := context.Background()

	err := c.Store(ctx, []string{key1, key2, key3, key4}, [][]byte{data1, data2, data3, data4})
	require.NoError(t, err)

	value, ok := c.Get(ctx, key4)
	require.True(t, ok)
	require.Equal(t, data4, value)

	_, ok = c.Get(ctx, key1)
	require.False(t, ok)

	c.lock.RLock()
	assert.Equal(t, float64(4), testutil.ToFloat64(c.entriesAddedNew))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(expiredReason)))
	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(fullReason)))
	assert.Equal(t, float64(3), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(len(c.entries)), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(c.lru.Len()), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(memorySz), testutil.ToFloat64(c.memoryBytes))
	assert.Equal(t, int64(1), removedEntriesCount.Load(), "on removal callback had to be called for key1")
	c.lock.RUnlock()

	// Expire the item.
	time.Sleep(2 * cfg.TTL)
	_, ok = c.Get(ctx, key4)
	require.False(t, ok)

	c.lock.RLock()
	assert.Equal(t, float64(4), testutil.ToFloat64(c.entriesAddedNew))
	assert.Equal(t, float64(3), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(expiredReason)))
	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(fullReason)))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(len(c.entries)), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(c.lru.Len()), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(memorySz), testutil.ToFloat64(c.memoryBytes))
	assert.Equal(t, int64(4), removedEntriesCount.Load(), "on removal callback had to be called for all 3 expired entries")
	c.lock.RUnlock()

	c.Stop()
}

func genBytes(n uint8) []byte {
	arr := make([]byte, n)
	for i := range arr {
		arr[i] = byte(i)
	}
	return arr
}
