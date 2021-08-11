package cache

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFifoCacheEviction(t *testing.T) {
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
		cfg  FifoCacheConfig
	}{
		{
			name: "test-memory-eviction",
			cfg:  FifoCacheConfig{MaxSizeBytes: strconv.FormatInt(int64(cnt*sizeOf(itemTemplate)), 10), Validity: 1 * time.Minute},
		},
		{
			name: "test-items-eviction",
			cfg:  FifoCacheConfig{MaxSizeItems: cnt, Validity: 1 * time.Minute},
		},
	}

	for _, test := range tests {
		c := NewFifoCache(test.name, test.cfg, nil, log.NewNopLogger())
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
		c.Store(ctx, keys, values)
		require.Len(t, c.entries, cnt)

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
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
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

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
		c.Store(ctx, keys, values)
		require.Len(t, c.entries, cnt)

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
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
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt*2+evicted))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

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
		c.Store(ctx, keys, values)
		require.Len(t, c.entries, cnt)

		for i := cnt; i < cnt+evicted; i++ {
			value, ok := c.Get(ctx, fmt.Sprintf("%02d", i))
			require.True(t, ok)
			require.Equal(t, []byte(fmt.Sprintf("%02d", i*2)), value)
		}

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(cnt+evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(evicted))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(cnt))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(cnt*2+evicted*2))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(cnt-evicted))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(cnt*sizeOf(itemTemplate)))

		c.Stop()
	}
}

func TestFifoCacheExpiry(t *testing.T) {
	key1, key2, key3, key4 := "01", "02", "03", "04"
	data1, data2, data3 := genBytes(24), []byte("testdata"), genBytes(8)

	memorySz := sizeOf(&cacheEntry{key: key1, value: data1}) +
		sizeOf(&cacheEntry{key: key2, value: data2}) +
		sizeOf(&cacheEntry{key: key3, value: data3})

	tests := []struct {
		name string
		cfg  FifoCacheConfig
	}{
		{
			name: "test-memory-expiry",
			cfg:  FifoCacheConfig{MaxSizeBytes: strconv.FormatInt(int64(memorySz), 10), Validity: 5 * time.Millisecond},
		},
		{
			name: "test-items-expiry",
			cfg:  FifoCacheConfig{MaxSizeItems: 3, Validity: 5 * time.Millisecond},
		},
	}

	for _, test := range tests {
		c := NewFifoCache(test.name, test.cfg, nil, log.NewNopLogger())
		ctx := context.Background()

		c.Store(ctx,
			[]string{key1, key2, key4, key3, key2, key1},
			[][]byte{genBytes(16), []byte("dummy"), genBytes(20), data3, data2, data1})

		value, ok := c.Get(ctx, key1)
		require.True(t, ok)
		require.Equal(t, data1, value)

		_, ok = c.Get(ctx, key4)
		require.False(t, ok)

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(5))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(memorySz))

		// Expire the item.
		time.Sleep(5 * time.Millisecond)
		_, ok = c.Get(ctx, key1)
		require.False(t, ok)

		assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(5))
		assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(len(c.entries)))
		assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(c.lru.Len()))
		assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(3))
		assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(2))
		assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(1))
		assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(memorySz))

		c.Stop()
	}
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
