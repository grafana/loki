package compute_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

const benchmarkSize = 10000

var selections = map[string]func(*testing.B, *memory.Allocator) memory.Bitmap{
	"selection_pct=100": func(*testing.B, *memory.Allocator) memory.Bitmap { return memory.Bitmap{} },
	"selection_pct=99": func(b *testing.B, alloc *memory.Allocator) memory.Bitmap {
		return makeSparseSelection(b, alloc, benchmarkSize, 0.99)
	},
	"selection_pct=50": func(b *testing.B, alloc *memory.Allocator) memory.Bitmap {
		return makeAlternatingSelection(b, alloc, benchmarkSize)
	},
	"selection_pct=05": func(b *testing.B, alloc *memory.Allocator) memory.Bitmap {
		return makeSparseSelection(b, alloc, benchmarkSize, 0.05)
	},
}

func BenchmarkEquals_Int64(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			left := makeInt64Array(b, &alloc, benchmarkSize)
			right := makeInt64Array(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Equals(benchAlloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, left, right)
		})
	}
}

func BenchmarkEquals_UTF8(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			left := makeUTF8Array(b, &alloc, benchmarkSize)
			right := makeUTF8Array(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Equals(benchAlloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, left, right)
		})
	}
}

func makeInt64Array(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	for i := 0; i < size; i++ {
		values[i] = int64(i % 1000)
	}
	return columnartest.Array(tb, columnar.KindInt64, alloc, values...)
}

func makeUTF8Array(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	strings := []string{"foo", "bar", "baz", "qux", "quux", "corge", "grault", "garply"}
	for i := 0; i < size; i++ {
		values[i] = strings[i%len(strings)]
	}
	return columnartest.Array(tb, columnar.KindUTF8, alloc, values...)
}

func makeAlternatingSelection(_ testing.TB, alloc *memory.Allocator, size int) memory.Bitmap {
	bm := memory.NewBitmap(alloc, size)
	for i := 0; i < size; i++ {
		bm.Append(i%2 == 0)
	}
	return bm
}

func makeSparseSelection(_ testing.TB, alloc *memory.Allocator, size int, selectivity float64) memory.Bitmap {
	bm := memory.NewBitmap(alloc, size)
	step := int(1.0 / selectivity)
	for i := 0; i < size; i++ {
		bm.Append(i%step == 0)
	}
	return bm
}
