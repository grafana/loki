package compute_test

import (
	"testing"

	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

var selections = map[string]func(*testing.B, *memory.Allocator) memory.Bitmap{
	"selection_pct=100": func(b *testing.B, _ *memory.Allocator) memory.Bitmap { return memory.Bitmap{} },
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

func BenchmarkRegexpMatch(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			haystack := makeUTF8ArrayForRegexp(b, &alloc, benchmarkSize)
			pattern := regexp.MustCompile(`ba[rz]`)
			selection := selectionFunc(b, &alloc)

			for b.Loop() {
				result, err := compute.RegexpMatch(&alloc, haystack, pattern, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			utf8 := haystack.(*columnar.UTF8)
			b.SetBytes(int64(utf8.Size()))
			b.ReportMetric(float64(b.N*utf8.Len()), "values/s")
		})
	}
}

func BenchmarkSubstrInsensitive(b *testing.B) {
	for selectionName, selectionFunc := range selections {
		b.Run(selectionName, func(b *testing.B) {
			var alloc memory.Allocator
			haystack := makeUTF8ArrayForSubstr(b, &alloc, benchmarkSize)
			needle := &columnar.UTF8Scalar{Value: []byte("TEST")}
			selection := selectionFunc(b, &alloc)

			for b.Loop() {
				result, err := compute.SubstrInsensitive(&alloc, haystack, needle, selection)
				if err != nil {
					b.Fatal(err)
				}
				_ = result
			}

			utf8 := haystack.(*columnar.UTF8)
			b.SetBytes(int64(utf8.Size()))
			b.ReportMetric(float64(b.N*utf8.Len()), "values/s")
		})
	}

}

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
