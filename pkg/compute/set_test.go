package compute

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
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
	values := make([]string, 1000)
	for i := range 1000 {
		values[i] = fmt.Sprintf("notpresent%d", i)
	}
	valuesSet := columnar.NewUTF8Set(values...)

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = IsMember(benchAlloc, searchData, valuesSet)
	}

	b.SetBytes(int64(searchData.Size()))
	b.ReportMetric(float64(searchData.Len()*b.N)/b.Elapsed().Seconds(), "values/s")
}

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
