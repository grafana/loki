package memory_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/memory"
)

func TestAllocator_Reclaim(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	// We want to try to create a buffer with exactly the same capacity twice.
	// On the second loop, the allocated/free bytes should be exactly the same
	// if the buffer was reassigned.
	for range 2 {
		_ = alloc.Allocate(5)
		require.Equal(t, 5, alloc.AllocatedBytes(), "there should be no additional memory allocated")
		require.Equal(t, 0, alloc.FreeBytes(), "all allocated memory should be in use")

		alloc.Reclaim()

		require.Equal(t, 5, alloc.AllocatedBytes(), "there should be no additional memory allocated")
		require.Equal(t, alloc.AllocatedBytes(), alloc.FreeBytes(), "memory should have been marked as free")
	}
}

func TestAllocator_Trim(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	_ = alloc.Allocate(5)

	// An initial trim should not change the number of allocated bytes, as
	// the buffer is still in use.
	origAllocated := alloc.AllocatedBytes()
	alloc.Trim()
	require.Equal(t, origAllocated, alloc.AllocatedBytes(), "should not have trimmed in-use memory")

	alloc.Reclaim()
	alloc.Trim()
	require.Equal(t, 0, alloc.AllocatedBytes(), "all memory should have been trimmed")

	// Creating a new buffer should allocate new memory.
	_ = alloc.Allocate(5)
	require.Equal(t, origAllocated, alloc.AllocatedBytes())
}
