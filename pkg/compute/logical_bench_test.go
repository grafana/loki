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

			for b.Loop() {
				result, err := compute.And(&alloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			arr := left.(columnar.Array)
			b.SetBytes(int64(arr.Size()))
			b.ReportMetric(float64(b.N*arr.Len()), "values/s")
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

			for b.Loop() {
				result, err := compute.Or(&alloc, left, right, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			arr := left.(columnar.Array)
			b.SetBytes(int64(arr.Size()))
			b.ReportMetric(float64(b.N*arr.Len()), "values/s")
		})
	}
}

func BenchmarkNot(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			input := makeBoolArray(b, &alloc, benchmarkSize)
			selection := selectionFunc(b, &alloc)

			for b.Loop() {
				result, err := compute.Not(&alloc, input, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			arr := input.(columnar.Array)
			b.SetBytes(int64(arr.Size()))
			b.ReportMetric(float64(b.N*arr.Len()), "values/s")
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
