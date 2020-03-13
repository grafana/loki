package cache

import (
	"context"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const size = 10
const overwrite = 5

func TestFifoCache(t *testing.T) {
	c := NewFifoCache("test1", FifoCacheConfig{Size: size, Validity: 1 * time.Minute})
	ctx := context.Background()

	// Check put / get works
	keys := []string{}
	values := []interface{}{}
	for i := 0; i < size; i++ {
		keys = append(keys, strconv.Itoa(i))
		values = append(values, i)
	}
	c.Put(ctx, keys, values)
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := 0; i < size; i++ {
		value, ok := c.Get(ctx, strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i, value.(int))
	}

	// Check evictions
	keys = []string{}
	values = []interface{}{}
	for i := size; i < size+overwrite; i++ {
		keys = append(keys, strconv.Itoa(i))
		values = append(values, i)
	}
	c.Put(ctx, keys, values)
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := 0; i < size-overwrite; i++ {
		_, ok := c.Get(ctx, strconv.Itoa(i))
		require.False(t, ok)
	}
	for i := size; i < size+overwrite; i++ {
		value, ok := c.Get(ctx, strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i, value.(int))
	}

	// Check updates work
	keys = []string{}
	values = []interface{}{}
	for i := size; i < size+overwrite; i++ {
		keys = append(keys, strconv.Itoa(i))
		values = append(values, i*2)
	}
	c.Put(ctx, keys, values)
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := size; i < size+overwrite; i++ {
		value, ok := c.Get(ctx, strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i*2, value.(int))
	}
}

func TestFifoCacheEvictionExpiry(t *testing.T) {
	c := NewFifoCache("test2", FifoCacheConfig{Size: 3, Validity: 5 * time.Millisecond})
	ctx := context.Background()
	memorySz := int(unsafe.Sizeof(cacheEntry{})) * 3

	assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(memorySz))

	key1, key2, key3, key4 := "key1", "key23", "key345", "key45676789"
	data1, data2, data3 := []float64{1.0, 2.0, 3.0}, "testdata", []byte{1, 2, 3, 4, 5, 6, 7, 8}
	memorySz += len(key1) + len(key2) + len(key3) + len(data1)*8 + len(data2) + len(data3)

	c.Put(ctx,
		[]string{key1, key2, key4, key3, key2, key1},
		[]interface{}{[]int32{1, 2, 3, 4}, "dummy", []int{5, 4, 3, 2, 1}, data3, data2, data1})

	value, ok := c.Get(ctx, key1)
	require.True(t, ok)
	require.Equal(t, data1, value.([]float64))

	_, ok = c.Get(ctx, key4)
	require.False(t, ok)

	assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
	assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(5))
	assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(2))
	assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(3))
	assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(2))
	assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(1))
	assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(0))
	assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(memorySz))

	// Expire the entry.
	time.Sleep(5 * time.Millisecond)
	_, ok = c.Get(ctx, key1)
	require.False(t, ok)

	assert.Equal(t, testutil.ToFloat64(c.entriesAdded), float64(1))
	assert.Equal(t, testutil.ToFloat64(c.entriesAddedNew), float64(5))
	assert.Equal(t, testutil.ToFloat64(c.entriesEvicted), float64(2))
	assert.Equal(t, testutil.ToFloat64(c.entriesCurrent), float64(3))
	assert.Equal(t, testutil.ToFloat64(c.totalGets), float64(3))
	assert.Equal(t, testutil.ToFloat64(c.totalMisses), float64(2))
	assert.Equal(t, testutil.ToFloat64(c.staleGets), float64(1))
	assert.Equal(t, testutil.ToFloat64(c.memoryBytes), float64(memorySz))
}
