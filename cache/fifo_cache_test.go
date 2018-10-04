package cache

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const size = 10
const overwrite = 5

func TestFifoCache(t *testing.T) {
	c := NewFifoCache("test", FifoCacheConfig{Size: size, Validity: 1 * time.Minute})
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

func TestFifoCacheExpiry(t *testing.T) {
	c := NewFifoCache("test", FifoCacheConfig{Size: size, Validity: 5 * time.Millisecond})
	ctx := context.Background()

	c.Put(ctx, []string{"0"}, []interface{}{0})

	value, ok := c.Get(ctx, "0")
	require.True(t, ok)
	require.Equal(t, 0, value.(int))

	// Expire the entry.
	time.Sleep(5 * time.Millisecond)
	_, ok = c.Get(ctx, strconv.Itoa(0))
	require.False(t, ok)
}

func (c *FifoCache) print() {
	fmt.Println("first", c.first, "last", c.last)
	for i, entry := range c.entries {
		fmt.Printf("  %d -> key: %s, value: %v, next: %d, prev: %d\n", i, entry.key, entry.value, entry.next, entry.prev)
	}
}
