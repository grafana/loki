package compute_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func BenchmarkAnd(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			left := makeBoolArray(b, &alloc, benchmarkSize)
			right := makeBoolArray(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.And(benchAlloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, left, right)
		})
	}
}

func BenchmarkOr(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			left := makeBoolArray(b, &alloc, benchmarkSize)
			right := makeBoolArray(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Or(benchAlloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, left, right)
		})
	}
}

func BenchmarkNot(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			input := makeBoolArray(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			benchAlloc := memory.NewAllocator(nil)
			for b.Loop() {
				benchAlloc.Reclaim()
				result, err := compute.Not(benchAlloc, input, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			reportArrayBenchMetrics(b, input)
		})
	}
}

func makeBoolArray(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	for i := 0; i < size; i++ {
		values[i] = i%3 != 0 // ~67% true, ~33% false
	}
	return columnartest.Array(tb, columnar.KindBool, alloc, values...)
}
