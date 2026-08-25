package compute_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

var filterSelectivities = map[string]func(*testing.B, *memory.Allocator) memory.Bitmap{
	"selectivity=100": func(*testing.B, *memory.Allocator) memory.Bitmap { return memory.Bitmap{} },
	"selectivity=50": func(b *testing.B, alloc *memory.Allocator) memory.Bitmap {
		return makeAlternatingSelection(b, alloc, benchmarkSize)
	},
	"selectivity=05": func(b *testing.B, alloc *memory.Allocator) memory.Bitmap {
		return makeSparseSelection(b, alloc, benchmarkSize, 0.05)
	},
}

func BenchmarkFilter_Bool(b *testing.B) {
	for name, maskFunc := range filterSelectivities {
		b.Run(name, func(b *testing.B) {
			var alloc memory.Allocator
			input := makeBoolArray(b, &alloc, benchmarkSize)
			mask := maskFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Filter(benchAlloc, input, mask)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, input)
		})
	}
}

func BenchmarkFilter_Int64(b *testing.B) {
	for name, maskFunc := range filterSelectivities {
		b.Run(name, func(b *testing.B) {
			var alloc memory.Allocator
			input := makeInt64Array(b, &alloc, benchmarkSize)
			mask := maskFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Filter(benchAlloc, input, mask)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, input)
		})
	}
}

func BenchmarkFilter_UTF8(b *testing.B) {
	for name, maskFunc := range filterSelectivities {
		b.Run(name, func(b *testing.B) {
			var alloc memory.Allocator
			input := makeUTF8Array(b, &alloc, benchmarkSize)
			mask := maskFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Filter(benchAlloc, input, mask)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, input)
		})
	}
}
