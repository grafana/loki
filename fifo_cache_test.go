package chunk

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const size = 10
const overwrite = 5

func TestFifoCache(t *testing.T) {
	c := NewFifoCache(size)

	// Check put / get works
	for i := 0; i < size; i++ {
		c.Put(strconv.Itoa(i), i)
		//c.print()
	}
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := 0; i < size; i++ {
		value, _, ok := c.Get(strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i, value.(int))
	}

	// Check evictions
	for i := size; i < size+overwrite; i++ {
		c.Put(strconv.Itoa(i), i)
		//c.print()
	}
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := 0; i < size-overwrite; i++ {
		_, _, ok := c.Get(strconv.Itoa(i))
		require.False(t, ok)
	}
	for i := size; i < size+overwrite; i++ {
		value, _, ok := c.Get(strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i, value.(int))
	}

	// Check updates work
	for i := size; i < size+overwrite; i++ {
		c.Put(strconv.Itoa(i), i*2)
		//c.print()
	}
	require.Len(t, c.index, size)
	require.Len(t, c.entries, size)

	for i := size; i < size+overwrite; i++ {
		value, _, ok := c.Get(strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, i*2, value.(int))
	}
}

func (c *FifoCache) print() {
	fmt.Println("first", c.first, "last", c.last)
	for i, entry := range c.entries {
		fmt.Printf("  %d -> key: %s, value: %v, next: %d, prev: %d\n", i, entry.key, entry.value, entry.next, entry.prev)
	}
}
