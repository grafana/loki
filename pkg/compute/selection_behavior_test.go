package compute_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestSelectionMasking(t *testing.T) {
	var alloc memory.Allocator

	selection := selectionMask(&alloc, true, false, true, false)

	t.Run("equals", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 2, 3, 4)
		right := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 0, 3, 0)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		actual, err := compute.Equals(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("not", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, nil, false, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("and", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("substr-insensitive", func(t *testing.T) {
		haystack := columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "A", "c")
		needle := columnartest.Scalar(t, columnar.KindUTF8, "a")

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		actual, err := compute.SubstrInsensitive(&alloc, haystack, needle, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

func TestSelectionLengthMismatch(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 2, 3, 4)
	right := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 2, 3, 4)
	selection := selectionMask(&alloc, true, false)

	_, err := compute.Equals(&alloc, left, right, selection)
	require.Error(t, err)
}

func TestAllSelected(t *testing.T) {
	var alloc memory.Allocator

	t.Run("equals-with-all-selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 2, 3, 4)
		right := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 0, 3, 0)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		actual, err := compute.Equals(&alloc, left, right, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("not-with-all-selected", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true)

		actual, err := compute.Not(&alloc, input, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("and-with-all-selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, false)

		actual, err := compute.And(&alloc, left, right, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("substr-insensitive-with-all-selected", func(t *testing.T) {
		haystack := columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "A", "c")
		needle := columnartest.Scalar(t, columnar.KindUTF8, "a")

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		actual, err := compute.SubstrInsensitive(&alloc, haystack, needle, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

func TestSingleElementArray(t *testing.T) {
	var alloc memory.Allocator

	t.Run("single-element-with-true-selection", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindUint64, &alloc, 42)
		right := columnartest.Array(t, columnar.KindUint64, &alloc, 42)
		selection := selectionMask(&alloc, true)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true)

		actual, err := compute.Equals(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("single-element-with-false-selection", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindUint64, &alloc, 42)
		right := columnartest.Array(t, columnar.KindUint64, &alloc, 42)
		selection := selectionMask(&alloc, false)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil)

		actual, err := compute.Equals(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("single-element-not-operation", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true)
		selection := selectionMask(&alloc, true)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

func TestAllFalseSelection(t *testing.T) {
	var alloc memory.Allocator

	selection := selectionMask(&alloc, false, false, false, false)

	t.Run("equals-with-all-false-selection", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 2, 3, 4)
		right := columnartest.Array(t, columnar.KindUint64, &alloc, 1, 0, 3, 0)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.Equals(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("not-with-all-false-selection", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("and-with-all-false-selection", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("or-with-all-false-selection", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("substr-insensitive-with-all-false-selection", func(t *testing.T) {
		haystack := columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "A", "c")
		needle := columnartest.Scalar(t, columnar.KindUTF8, "a")

		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.SubstrInsensitive(&alloc, haystack, needle, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}
