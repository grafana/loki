package compute_test

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
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
		_, _ = compute.IsMember(benchAlloc, searchData, valuesSet, memory.Bitmap{})
	}

	reportArrayBenchMetrics(b, searchData)
}

func BenchmarkIsMember_UTF8(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator

			// Create test data
			data := make([]any, benchmarkSize)
			for i := 0; i < benchmarkSize; i++ {
				data[i] = fmt.Sprintf("value%d", i%100)
			}
			searchData := columnartest.Array(b, columnar.KindUTF8, &alloc, data...)

			// Create search set with 50% of values present
			values := make([]string, 50)
			for i := 0; i < 50; i++ {
				values[i] = fmt.Sprintf("value%d", i)
			}
			valuesSet := columnar.NewUTF8Set(values...)

			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.IsMember(benchAlloc, searchData, valuesSet, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, searchData)
		})
	}
}

func BenchmarkIsMember_Int64(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator

			// Create test data
			data := make([]any, benchmarkSize)
			for i := 0; i < benchmarkSize; i++ {
				data[i] = int64(i % 100)
			}
			searchData := columnartest.Array(b, columnar.KindInt64, &alloc, data...)

			// Create search set with 50% of values present
			values := make([]int64, 50)
			for i := 0; i < 50; i++ {
				values[i] = int64(i)
			}
			valuesSet := columnar.NewNumberSet(values...)

			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.IsMember(benchAlloc, searchData, valuesSet, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, searchData)
		})
	}
}

func BenchmarkIsMember_Uint64(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator

			// Create test data
			data := make([]any, benchmarkSize)
			for i := 0; i < benchmarkSize; i++ {
				data[i] = uint64(i % 100)
			}
			searchData := columnartest.Array(b, columnar.KindUint64, &alloc, data...)

			// Create search set with 50% of values present
			values := make([]uint64, 50)
			for i := 0; i < 50; i++ {
				values[i] = uint64(i)
			}
			valuesSet := columnar.NewNumberSet(values...)

			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.IsMember(benchAlloc, searchData, valuesSet, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, searchData)
		})
	}
}
