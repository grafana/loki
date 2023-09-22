package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFifoCacheEviction(t *testing.T) {
	const (
		cnt     = 10
		evicted = 5
	)

	// compute value size such that 10 entries account to exactly 1MB.
	// adding one more entry to the cache would result in eviction when MaxSizeMB is configured to a value of 1.
	// value cap = target size of each entry (0.1MB) - size of cache entry with empty value.
	valueCap := (1e6 / cnt) - sizeOf(&cacheEntry{
		key: "00",
	})

	itemTemplate := &cacheEntry{
		key:   "00",
		value: make([]byte, 0, valueCap),
	}

	tests := []struct {
		name string
		cfg  FifoCacheConfig
	}{
		{
			name: "test-memory-eviction",
			cfg:  FifoCacheConfig{MaxSizeMB: 1, TTL: 1 * time.Minute},
		},
		{
			name: "test-items-eviction",
			cfg:  FifoCacheConfig{MaxSizeItems: cnt, TTL: 1 * time.Minute},
		},
	}

	for _, test := range tests {
		c := NewFifoCache(test.name, test.cfg, nil, log.NewNopLogger(), "test")
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

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		for i := 0; i < cnt; i++ {
			key := fmt.Sprintf("%02d", i)
			value, ok := c.Get(ctx, key)
			require.True(t, ok)
			require.Equal(t, []byte(key), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
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

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
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

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt*2+evicted))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
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

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted.WithLabelValues(reason)), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt*2+evicted*2))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		c.Stop()
	}
}

func TestFifoCacheExpiry(t *testing.T) {
	key1, key2, key3, key4 := "01", "02", "03", "04"
	data1, data2, data3, data4 := genBytes(32), genBytes(64), genBytes(128), genBytes(32)

	memorySz := sizeOf(&cacheEntry{key: key1, value: data1}) +
		sizeOf(&cacheEntry{key: key2, value: data2}) +
		sizeOf(&cacheEntry{key: key3, value: data3})

	cfg := FifoCacheConfig{
		MaxSizeItems:  3,
		TTL:           100 * time.Millisecond,
		PurgeInterval: 50 * time.Millisecond,
	}

	c := NewFifoCache("cache_exprity_test", cfg, nil, log.NewNopLogger(), "test")
	ctx := context.Background()

	err := c.Store(ctx, []string{key1, key2, key3, key4}, [][]byte{data1, data2, data3, data4})
	require.NoError(t, err)

	value, ok := c.Get(ctx, key4)
	require.True(t, ok)
	require.Equal(t, data4, value)

	_, ok = c.Get(ctx, key1)
	require.False(t, ok)

	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesAdded))
	assert.Equal(t, float64(4), testutil.ToFloat64(c.entriesAddedNew))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(expiredReason)))
	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(fullReason)))
	assert.Equal(t, float64(3), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(len(c.entries)), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(c.lru.Len()), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(2), testutil.ToFloat64(c.totalGets))
	assert.Equal(t, float64(1), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(memorySz), testutil.ToFloat64(c.memoryBytes))

	// Expire the item.
	time.Sleep(2 * cfg.TTL)
	_, ok = c.Get(ctx, key4)
	require.False(t, ok)

	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesAdded))
	assert.Equal(t, float64(4), testutil.ToFloat64(c.entriesAddedNew))
	assert.Equal(t, float64(3), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(expiredReason)))
	assert.Equal(t, float64(1), testutil.ToFloat64(c.entriesEvicted.WithLabelValues(fullReason)))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(len(c.entries)), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(c.lru.Len()), testutil.ToFloat64(c.entriesCurrent))
	assert.Equal(t, float64(3), testutil.ToFloat64(c.totalGets))
	assert.Equal(t, float64(2), testutil.ToFloat64(c.totalMisses))
	assert.Equal(t, float64(memorySz), testutil.ToFloat64(c.memoryBytes))

	c.Stop()
}

func genBytes(n uint8) []byte {
	arr := make([]byte, n)
	for i := range arr {
		arr[i] = byte(i)
	}
	return arr
}

func TestBytesParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{input: "", expected: 0},
		{input: "123", expected: 123},
		{input: "1234567890", expected: 1234567890},
		{input: "25k", expected: 25000},
		{input: "25K", expected: 25000},
		{input: "25kb", expected: 25000},
		{input: "25kB", expected: 25000},
		{input: "25Kb", expected: 25000},
		{input: "25KB", expected: 25000},
		{input: "25kib", expected: 25600},
		{input: "25KiB", expected: 25600},
		{input: "25m", expected: 25000000},
		{input: "25M", expected: 25000000},
		{input: "25mB", expected: 25000000},
		{input: "25MB", expected: 25000000},
		{input: "2.5MB", expected: 2500000},
		{input: "25MiB", expected: 26214400},
		{input: "25mib", expected: 26214400},
		{input: "2.5mib", expected: 2621440},
		{input: "25g", expected: 25000000000},
		{input: "25G", expected: 25000000000},
		{input: "25gB", expected: 25000000000},
		{input: "25Gb", expected: 25000000000},
		{input: "25GiB", expected: 26843545600},
		{input: "25gib", expected: 26843545600},
	}
	for _, test := range tests {
		output, err := parsebytes(test.input)
		assert.Nil(t, err)
		assert.Equal(t, test.expected, output)
	}
}
