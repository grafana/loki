package compute

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/stretchr/testify/require"
)

func BenchmarkIsMember(b *testing.B) {
	var alloc memory.Allocator

	// 1000 values
	data := make([]any, 0, 1000)
	for i := range 1000 {
		data = append(data, fmt.Sprintf("test%d", i))
	}
	searchData := columnartest.Array(b, columnar.KindUTF8, &alloc, data...)

	// 1000 keys
	values := make(map[any]struct{}, 1000)
	for i := range 1000 {
		values[fmt.Sprintf("notpresent%d", i)] = struct{}{}
	}

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = IsMember(benchAlloc, searchData, values)
	}

	b.SetBytes(int64(searchData.Size()))
	b.ReportMetric(float64(searchData.Len()*b.N)/b.Elapsed().Seconds(), "values/s")
}

func TestIsMember(t *testing.T) {
	t.Parallel()
	t.Run("UTF8", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := map[any]struct{}{"test1": {}, "test2": {}, "test3": {}}
		defaultSearchData := columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3")

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: map[any]struct{}{"test1": {}, "test2": {}, "test4": {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: map[any]struct{}{"test4": {}, "test5": {}, "test6": {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindUTF8, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, nil), values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindInt64, alloc, 1, 2, 3), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, alloc, tt)
	})

	t.Run("Int64", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := map[any]struct{}{int64(1): {}, int64(2): {}, int64(3): {}}
		defaultSearchData := columnartest.Array(t, columnar.KindInt64, alloc, int64(1), int64(2), int64(3))

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: map[any]struct{}{int64(1): {}, int64(2): {}, int64(4): {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: map[any]struct{}{int64(4): {}, int64(5): {}, int64(6): {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindInt64, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindInt64, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindInt64, alloc, nil), values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3"), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, alloc, tt)
	})

	t.Run("Uint64", func(t *testing.T) {
		t.Parallel()
		alloc := memory.NewAllocator(nil)

		defaultSearchValues := map[any]struct{}{uint64(1): {}, uint64(2): {}, uint64(3): {}}
		defaultSearchData := columnartest.Array(t, columnar.KindUint64, alloc, uint64(1), uint64(2), uint64(3))

		tt := []isMemberTestCase{
			{name: "all present", searchData: defaultSearchData, values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, true)},
			{name: "some present", searchData: defaultSearchData, values: map[any]struct{}{uint64(1): {}, uint64(2): {}, uint64(4): {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, true, true, false)},
			{name: "none present", searchData: defaultSearchData, values: map[any]struct{}{uint64(4): {}, uint64(5): {}, uint64(6): {}}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "empty search data", searchData: columnartest.Array(t, columnar.KindUint64, alloc), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc)},
			{name: "empty values", searchData: defaultSearchData, values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, false, false, false)},
			{name: "null search data", searchData: columnartest.Array(t, columnar.KindUint64, alloc, nil), values: defaultSearchValues, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "null search data and empty values", searchData: columnartest.Array(t, columnar.KindUint64, alloc, nil), values: map[any]struct{}{}, expect: columnartest.Array(t, columnar.KindBool, alloc, nil)},
			{name: "mismatched types", searchData: columnartest.Array(t, columnar.KindUTF8, alloc, "test1", "test2", "test3"), values: defaultSearchValues, expect: nil, expectError: true},
		}

		runIsMemberTests(t, alloc, tt)
	})
}

type isMemberTestCase struct {
	name        string
	searchData  columnar.Datum
	values      map[any]struct{}
	expect      columnar.Datum
	expectError bool
}

func runIsMemberTests(t *testing.T, alloc *memory.Allocator, tt []isMemberTestCase) {
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := IsMember(alloc, tc.searchData, tc.values)
			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tc.expect, result)
		})
	}
}
