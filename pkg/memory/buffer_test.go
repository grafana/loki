package memory_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBuffer_Push(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	buf := memory.NewBuffer[int32](&alloc, 10)
	require.Equal(t, (64 / 4), buf.Cap(), "capacity should be 64-byte aligned") // Capacity should be padded to 64-byte alignment.

	require.NotPanics(t, func() {
		for range int32(buf.Cap()) {
			buf.Push(5)
		}
	})

	require.Panics(t, func() {
		buf.Push(99)
	}, "should not be able to push past capacity")

	// Grow the capacity and try some more.
	buf.Grow(5)
	require.NotPanics(t, func() {
		for range int32(buf.Cap() - buf.Len()) {
			buf.Push(5)
		}
	})

	require.Panics(t, func() {
		buf.Push(99)
	}, "should not be able to push past capacity")

	expect := slices.Repeat([]int32{5}, buf.Cap())
	actual := buf.Data()[:buf.Len()]
	require.Equal(t, expect, actual)
}

func TestBuffer_Append(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	buf := memory.NewBuffer[int32](&alloc, 10)

	require.NotPanics(t, func() {
		buf.Append(slices.Repeat([]int32{5}, buf.Cap()-buf.Len())...)
	})

	require.Panics(t, func() {
		buf.Append(make([]int32, 1)...)
	}, "should not be able to push past capacity")

	// Grow the capacity and try some more.
	buf.Grow(5)
	require.NotPanics(t, func() {
		buf.Append(slices.Repeat([]int32{5}, buf.Cap()-buf.Len())...)
	})

	require.Panics(t, func() {
		buf.Append(make([]int32, 1)...)
	}, "should not be able to push past capacity")

	expect := slices.Repeat([]int32{5}, buf.Cap())
	actual := buf.Data()[:buf.Len()]
	require.Equal(t, expect, actual)
}

func TestBuffer_Set_Get(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	buf := memory.NewBuffer[int32](&alloc, 10)
	require.Equal(t, 0, buf.Len())
	require.Panics(t, func() { buf.Set(0, 15) }, "cannot call set past length")

	buf.Resize(5)
	require.Equal(t, 5, buf.Len())
	require.Equal(t, (64 / 4), buf.Cap(), "capacity should be 64-byte aligned")
	require.NotPanics(t, func() { buf.Set(3, 15) }, "cannot call set past length")
	require.Panics(t, func() { buf.Set(5, 15) }, "cannot call set past length")

	require.Equal(t, int32(15), buf.Get(3))
}

func TestBuffer_Grow(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	buf := memory.NewBuffer[int32](&alloc, 10)
	buf.Grow(buf.Cap() * 2) // Double the initial capacity

	// Our allocator should be tracking both the original memory and the
	// expanded memory. However, it should not consider the expanded memory as
	// free, since there may be copies of the original memory still being used.
	require.Equal(t, 192, alloc.AllocatedBytes(), "expected allocated bytes to be 64-byte aligned")
	require.Equal(t, 0, alloc.FreeBytes(), "recently expanded memory should not be marked free")

	alloc.Reset()

	buf = memory.NewBuffer[int32](&alloc, 20)
	require.Equal(t, 192, alloc.AllocatedBytes(), "no additional memory should be allocated")
	require.Equal(t, 64, alloc.FreeBytes(), "recently expanded memory should be free")

	// Resetting one more time should cause the unused free memory to be released.
	alloc.Reset()

	buf = memory.NewBuffer[int32](&alloc, 20)
	require.Equal(t, 128, alloc.AllocatedBytes(), "expected unused memory to be released")
	require.Equal(t, 0, alloc.FreeBytes(), "expected unused memory to be released")
}

func TestBuffer_Slice(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	buf := memory.NewBuffer[int32](&alloc, 10)
	buf.Append(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)

	slice := buf.Slice(3, 7)
	require.Equal(t, 4, slice.Len())
	require.Equal(t, 4, slice.Cap())

	for i := 0; i < slice.Len(); i++ {
		require.Equal(t, int32(i+4), slice.Get(i), "unexpected slice value at index %d", i)
	}
}
