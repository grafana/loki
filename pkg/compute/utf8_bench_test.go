package compute_test

import (
	"testing"

	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Benchmark RegexpMatch with various selection patterns

func BenchmarkRegexpMatch_FullSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForRegexp(b, &alloc, benchmarkSize)
	pattern := regexp.MustCompile(`ba[rz]`)

	for b.Loop() {
		result, err := compute.RegexpMatch(&alloc, haystack, pattern, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkRegexpMatch_50PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForRegexp(b, &alloc, benchmarkSize)
	pattern := regexp.MustCompile(`ba[rz]`)
	selection := makeAlternatingSelection(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.RegexpMatch(&alloc, haystack, pattern, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkRegexpMatch_5PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForRegexp(b, &alloc, benchmarkSize)
	pattern := regexp.MustCompile(`ba[rz]`)
	selection := makeSparseSelection(b, &alloc, benchmarkSize, 0.05)

	for b.Loop() {
		result, err := compute.RegexpMatch(&alloc, haystack, pattern, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

// Benchmark SubstrInsensitive with various selection patterns

func BenchmarkSubstrInsensitive_FullSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForSubstr(b, &alloc, benchmarkSize)
	needle := &columnar.UTF8Scalar{Value: []byte("TEST")}

	for b.Loop() {
		result, err := compute.SubstrInsensitive(&alloc, haystack, needle, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkSubstrInsensitive_50PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForSubstr(b, &alloc, benchmarkSize)
	needle := &columnar.UTF8Scalar{Value: []byte("TEST")}
	selection := makeAlternatingSelection(b, &alloc, benchmarkSize)

	for b.Loop() {
		result, err := compute.SubstrInsensitive(&alloc, haystack, needle, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkSubstrInsensitive_5PercentSelection(b *testing.B) {
	var alloc memory.Allocator
	haystack := makeUTF8ArrayForSubstr(b, &alloc, benchmarkSize)
	needle := &columnar.UTF8Scalar{Value: []byte("TEST")}
	selection := makeSparseSelection(b, &alloc, benchmarkSize, 0.05)

	for b.Loop() {
		result, err := compute.SubstrInsensitive(&alloc, haystack, needle, selection)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

// Helper functions to create UTF8 test data for benchmarks

func makeUTF8ArrayForRegexp(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	strings := []string{"foo", "bar", "baz", "qux", "quux", "corge", "grault", "garply"}
	for i := 0; i < size; i++ {
		values[i] = strings[i%len(strings)]
	}
	return columnartest.Array(tb, columnar.KindUTF8, alloc, values...)
}

func makeUTF8ArrayForSubstr(tb testing.TB, alloc *memory.Allocator, size int) columnar.Datum {
	values := make([]interface{}, size)
	strings := []string{
		"this is a test string",
		"another test value",
		"no match here",
		"testing 123",
		"test",
		"final test entry",
	}
	for i := 0; i < size; i++ {
		values[i] = strings[i%len(strings)]
	}
	return columnartest.Array(tb, columnar.KindUTF8, alloc, values...)
}
