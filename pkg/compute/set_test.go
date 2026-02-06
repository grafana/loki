package compute

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

type isMemberTestCase struct {
	name        string
	searchData  columnar.Datum
	values      *columnar.Set
	expect      columnar.Datum
	expectError bool
}

func TestIsMember(t *testing.T) {
	t.Parallel()
	t.Run("UTF8", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := columnar.NewUTF8Set("test1", "test2", "test3")
		defaultSearchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3")

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: columnar.NewUTF8Set("test1", "test2", "test4"), expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: columnar.NewUTF8Set("test4", "test5", "test6"), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindUTF8, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: columnar.NewUTF8Set(), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, nil), values: columnar.NewUTF8Set(), expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindInt64, alloc, 1, 2, 3), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, tt)
	})

	t.Run("int64", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := columnar.NewNumberSet(int64(1), int64(2), int64(3))
		defaultSearchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(1), int64(2), int64(3))

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: columnar.NewNumberSet(int64(1), int64(2), int64(4)), expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: columnar.NewNumberSet(int64(4), int64(5), int64(6)), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindInt64, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: columnar.NewNumberSet[int64](), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindInt64, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindInt64, alloc, nil), values: columnar.NewNumberSet[int64](), expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3"), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, tt)
	})

	t.Run("Uint64", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := columnar.NewNumberSet(uint64(1), uint64(2), uint64(3))
		defaultSearchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(1), uint64(2), uint64(3))

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: columnar.NewNumberSet(uint64(1), uint64(2), uint64(4)), expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: columnar.NewNumberSet(uint64(4), uint64(5), uint64(6)), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindUint64, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: columnar.NewNumberSet[uint64](), expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindUint64, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindUint64, alloc, nil), values: columnar.NewNumberSet[uint64](), expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3"), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, tt)
	})
}

func runIsMemberTests(t *testing.T, tt []isMemberTestCase) {
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			alloc := memory.NewAllocator(nil)
			result, err := IsMember(alloc, tc.searchData, tc.values, memory.Bitmap{})
			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tc.expect, result)
		})
	}
}

func TestIsMemberWithSelection(t *testing.T) {
	t.Parallel()

	t.Run("UTF8_all_selected", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3", "test4")
		values := columnar.NewUTF8Set("test1", "test3")

		allSelected := memory.Bitmap{}

		result, err := IsMember(alloc, searchData, values, allSelected)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_full_selection", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3", "test4")
		values := columnar.NewUTF8Set("test1", "test3")

		// Explicit full selection (all bits true)
		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_partial_selection_50pct", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3", "test4")
		values := columnar.NewUTF8Set("test1", "test3")

		// Select indices 0 and 2 (alternating pattern)
		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		// Indices 0,2 are evaluated; 1,3 are null
		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_sparse_selection_5pct", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		// Create 20 values for a better 5% test
		data := make([]any, 20)
		for i := range 20 {
			if i%4 == 0 {
				data[i] = "match"
			} else {
				data[i] = "nomatch"
			}
		}
		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, data...)
		values := columnar.NewUTF8Set("match")

		// Select only index 0 (5% of 20)
		sparseSelection := memory.NewBitmap(alloc, 20)
		for i := range 20 {
			sparseSelection.Append(i == 0)
		}

		result, err := IsMember(alloc, searchData, values, sparseSelection)
		require.NoError(t, err)

		// Only index 0 is evaluated (true), rest are null
		expectedData := make([]any, 20)
		expectedData[0] = true
		for i := 1; i < 20; i++ {
			expectedData[i] = nil
		}
		expected := columnartest.Array(t, columnar.KindBool, alloc, expectedData...)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_no_rows_selected", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3")
		values := columnar.NewUTF8Set("test1", "test3")

		// No rows selected (all bits false)
		noSelection := memory.NewBitmap(alloc, 3)
		noSelection.AppendValues(false, false, false)

		result, err := IsMember(alloc, searchData, values, noSelection)
		require.NoError(t, err)

		// All results are null
		expected := columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Int64_all_selected", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(1), int64(2), int64(3), int64(4))
		values := columnar.NewNumberSet(int64(1), int64(3))

		allSelected := memory.Bitmap{}

		result, err := IsMember(alloc, searchData, values, allSelected)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Int64_partial_selection", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(1), int64(2), int64(3), int64(4))
		values := columnar.NewNumberSet(int64(1), int64(3))

		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_all_selected", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(1), uint64(2), uint64(3), uint64(4))
		values := columnar.NewNumberSet(uint64(1), uint64(3))

		allSelected := memory.Bitmap{}

		result, err := IsMember(alloc, searchData, values, allSelected)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_partial_selection", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(1), uint64(2), uint64(3), uint64(4))
		values := columnar.NewNumberSet(uint64(1), uint64(3))

		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})
}

func TestIsMemberNumberWithSelection(t *testing.T) {
	t.Parallel()

	t.Run("Int64_all_selected_explicit", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(10), int64(20), int64(30), int64(40))
		values := columnar.NewNumberSet(int64(10), int64(30))

		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Int64_sparse_selection", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		data := make([]any, 20)
		for i := range 20 {
			data[i] = int64(i * 10)
		}
		searchData := columnartest.Array(t, columnar.KindInt64, alloc, data...)
		values := columnar.NewNumberSet(int64(0), int64(100))

		sparseSelection := memory.NewBitmap(alloc, 20)
		for i := range 20 {
			sparseSelection.Append(i == 0 || i == 10)
		}

		result, err := IsMember(alloc, searchData, values, sparseSelection)
		require.NoError(t, err)

		expectedData := make([]any, 20)
		for i := range 20 {
			if i == 0 || i == 10 {
				expectedData[i] = true
			} else {
				expectedData[i] = nil
			}
		}
		expected := columnartest.Array(t, columnar.KindBool, alloc, expectedData...)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Int64_null_in_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(10), nil, int64(30), nil)
		values := columnar.NewNumberSet(int64(10), int64(30))

		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Int64_null_in_non_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(10), nil, int64(30), nil)
		values := columnar.NewNumberSet(int64(10), int64(30))

		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_all_selected_explicit", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(10), uint64(20), uint64(30), uint64(40))
		values := columnar.NewNumberSet(uint64(10), uint64(30))

		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_sparse_selection", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		data := make([]any, 20)
		for i := range 20 {
			data[i] = uint64(i * 10)
		}
		searchData := columnartest.Array(t, columnar.KindUint64, alloc, data...)
		values := columnar.NewNumberSet(uint64(0), uint64(100))

		sparseSelection := memory.NewBitmap(alloc, 20)
		for i := range 20 {
			sparseSelection.Append(i == 0 || i == 10)
		}

		result, err := IsMember(alloc, searchData, values, sparseSelection)
		require.NoError(t, err)

		expectedData := make([]any, 20)
		for i := range 20 {
			if i == 0 || i == 10 {
				expectedData[i] = true
			} else {
				expectedData[i] = nil
			}
		}
		expected := columnartest.Array(t, columnar.KindBool, alloc, expectedData...)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_null_in_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(10), nil, uint64(30), nil)
		values := columnar.NewNumberSet(uint64(10), uint64(30))

		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("Uint64_null_in_non_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(10), nil, uint64(30), nil)
		values := columnar.NewNumberSet(uint64(10), uint64(30))

		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})
}

func TestIsMemberUTF8WithSelection(t *testing.T) {
	t.Parallel()

	t.Run("UTF8_all_selected_explicit", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "apple", "banana", "cherry", "date")
		values := columnar.NewUTF8Set("apple", "cherry")

		// Explicit all-selected bitmap
		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		expected := columnartest.Array(t, columnar.KindBool, alloc, true, false, true, false)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_partial_selection_alternating", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "apple", "banana", "cherry", "date", "elderberry", "fig")
		values := columnar.NewUTF8Set("apple", "cherry", "elderberry")

		// Select indices 0, 2, 4 (alternating)
		partialSelection := memory.NewBitmap(alloc, 6)
		partialSelection.AppendValues(true, false, true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		// Selected indices: 0 (true), 2 (true), 4 (true); Non-selected: 1, 3, 5 (null)
		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_sparse_selection_one_row", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "apple", "banana", "cherry", "date", "elderberry")
		values := columnar.NewUTF8Set("cherry")

		// Select only index 2
		sparseSelection := memory.NewBitmap(alloc, 5)
		sparseSelection.AppendValues(false, false, true, false, false)

		result, err := IsMember(alloc, searchData, values, sparseSelection)
		require.NoError(t, err)

		// Only index 2 is evaluated
		expected := columnartest.Array(t, columnar.KindBool, alloc, nil, nil, true, nil, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_null_in_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		// Data with nulls at indices 1 and 3
		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "apple", nil, "cherry", nil)
		values := columnar.NewUTF8Set("apple", "cherry")

		// Select all rows
		fullSelection := memory.NewBitmap(alloc, 4)
		fullSelection.AppendValues(true, true, true, true)

		result, err := IsMember(alloc, searchData, values, fullSelection)
		require.NoError(t, err)

		// Nulls remain null, others are evaluated
		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_null_in_non_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		// Data with nulls at indices 1 and 3
		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, "apple", nil, "cherry", nil)
		values := columnar.NewUTF8Set("apple", "cherry")

		// Select only indices 0 and 2 (non-null values)
		partialSelection := memory.NewBitmap(alloc, 4)
		partialSelection.AppendValues(true, false, true, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		// Indices 0, 2 are evaluated; 1, 3 are null (from non-selection)
		expected := columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})

	t.Run("UTF8_null_in_selected_and_non_selected_rows", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		// Data with nulls at indices 0, 2, 4
		searchData := columnartest.Array(t, columnar.KindUTF8, alloc, nil, "banana", nil, "date", nil, "fig")
		values := columnar.NewUTF8Set("banana", "fig")

		// Select indices 0, 1, 2, 3 (including some nulls)
		partialSelection := memory.NewBitmap(alloc, 6)
		partialSelection.AppendValues(true, true, true, true, false, false)

		result, err := IsMember(alloc, searchData, values, partialSelection)
		require.NoError(t, err)

		// 0: null (from data), 1: true, 2: null (from data), 3: false, 4: null (not selected), 5: null (not selected)
		expected := columnartest.Array(t, columnar.KindBool, alloc, nil, true, nil, false, nil, nil)
		columnartest.RequireDatumsEqual(t, expected, result)
	})
}
