package compute_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Benchmark And operation with various selection patterns

func BenchmarkAnd_FullSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.And(&alloc, left, right, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkAnd_50PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeAlternatingSelection(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.And(&alloc, left, right, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkAnd_5PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeSparseSelection(b, &alloc, benchmarkSize, 0.05)

	for b.Loop() {
		result, err := compute.And(&alloc, left, right, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

// Benchmark Or operation with various selection patterns

func BenchmarkOr_FullSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.Or(&alloc, left, right, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkOr_50PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeAlternatingSelection(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.Or(&alloc, left, right, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkOr_5PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	left := makeBoolArray(b, &alloc, benchmarkSize)
	right := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeSparseSelection(b, &alloc, benchmarkSize, 0.05)

	for b.Loop() {
		result, err := compute.Or(&alloc, left, right, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

// Benchmark Not operation with various selection patterns

func BenchmarkNot_FullSelection(b *testing.B) {
	var alloc memory.Allocator
	input := makeBoolArray(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.Not(&alloc, input, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkNot_50PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	input := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeAlternatingSelection(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.Not(&alloc, input, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkNot_5PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	input := makeBoolArray(b, &alloc, benchmarkSize)
	selection := makeSparseSelection(b, &alloc, benchmarkSize, 0.05)

	for b.Loop() {
		result, err := compute.Not(&alloc, input, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

// Helper function to create bool arrays

func makeBoolArray(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	for i := 0; i < size; i++ {
		values[i] = i%3 != 0 // ~67% true, ~33% false
	}
	return columnartest.Array(tb, columnar.KindBool, alloc, values...)
}
