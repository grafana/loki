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
		_, _ = IsMember(benchAlloc, searchData, valuesSet, memory.Bitmap{})
	}

	b.SetBytes(int64(searchData.Size()))
	b.ReportMetric(float64(searchData.Len()*b.N)/b.Elapsed().Seconds(), "values/s")
}

func TestIsMember_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name       string
		searchData columnar.Datum
		values     *columnar.Set
	}{
		{
			name:       "UTF8 search data with int64 set",
			searchData: columnartest.Array(t, columnar.KindUTF8, &alloc, "test1", "test2", "test3"),
			values:     columnar.NewNumberSet(int64(1), int64(2), int64(3)),
		},
		{
			name:       "int64 search data with UTF8 set",
			searchData: columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			values:     columnar.NewUTF8Set("test1", "test2", "test3"),
		},
		{
			name:       "uint64 search data with UTF8 set",
			searchData: columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(2), uint64(3)),
			values:     columnar.NewUTF8Set("test1", "test2", "test3"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := IsMember(&alloc, tc.searchData, tc.values, memory.Bitmap{})
			require.Error(t, err, "mismatched types should result in an error")
		})
	}
}
