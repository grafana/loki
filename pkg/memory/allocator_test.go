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

func TestAllocator_AllocateFromParent(t *testing.T) {
	parent := memory.NewAllocator(nil)
	child := memory.NewAllocator(parent)

	defer child.Reset()
	defer parent.Reset()

	_ = child.Allocate(5)
	_ = parent.Allocate(3)

	require.Equal(t, 5, child.AllocatedBytes())
	require.Equal(t, 8, parent.AllocatedBytes())

	// An initial trim should not change the number of allocated bytes, as
	// the buffer is still in use.
	childAllocated := child.AllocatedBytes()
	parentAllocated := parent.AllocatedBytes()
	child.Trim()
	require.Equal(t, childAllocated, child.AllocatedBytes(), "should not have trimmed in-use memory")
	require.Equal(t, parentAllocated, parent.AllocatedBytes(), "should not have trimmed in-use memory")
	parent.Trim()
	require.Equal(t, childAllocated, child.AllocatedBytes(), "should not have trimmed in-use memory")
	require.Equal(t, parentAllocated, parent.AllocatedBytes(), "should not have trimmed in-use memory")

	// Reclaiming & trimming memory from the child should also trim child's memory from the parent.
	child.Reclaim()
	child.Trim()
	require.Equal(t, 0, child.AllocatedBytes(), "all memory should have been trimmed")
	require.Equal(t, parentAllocated, parent.AllocatedBytes(), "parent allocated memory should not have been changed because the parent was not Trimmed.")
	require.Equal(t, 0, child.FreeBytes(), "all child memory should have been freed")
	require.Equal(t, 5, parent.FreeBytes(), "parent memory should have the child's bytes available for allocation")

	// Creating a new buffer should allocate new memory from the parent.
	_ = child.Allocate(5)
	require.Equal(t, childAllocated, child.AllocatedBytes())
	require.Equal(t, parentAllocated, parent.AllocatedBytes(), "parent memory should have been reassigned to the child")

	// Reclaiming & trimming memory from the parent should reclaim all memory, but the child is not notified to avoid tracking children in the parent.
	// Child memory is also invalidated, however, so this scenario a memory re-use bug. All children should be reclaimed before the parent.
	parent.Reclaim()
	parent.Trim()
	require.Equal(t, 0, parent.AllocatedBytes(), "all memory should have been trimmed")
	require.Equal(t, 5, child.AllocatedBytes(), "child memory should not have been trimmed")
}

func TestAllocator_reuse_returned_child_memory(t *testing.T) {
	var (
		parent = memory.NewAllocator(nil)
		child  = memory.NewAllocator(parent)
	)

	// Have the child allocator pull some memory from its parent and immediately
	// give it back.
	expect := child.Allocate(64)
	child.Free()

	// Reset memory on the parent; this should keep reg alive since it was just
	// used by the child since the previous reset.
	parent.Reset()

	child = memory.NewAllocator(parent)
	actual := child.Allocate(64)

	// NOTE(rfratto): We can't use require.Equal here since that doesn't care
	// about pointer equality.
	require.True(t, expect == actual, "Parent should have returned original memory region (%p), got %p", expect, actual)
}
