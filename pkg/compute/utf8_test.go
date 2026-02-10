package compute

import (
	"strings"
	"testing"

	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func BenchmarkRegexpMatch(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	regexp := regexp.MustCompile("A{100}targetB{100}")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = RegexpMatch(benchAlloc, haystack, regexp, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}

func BenchmarkSubstrInsensitive(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	needle := columnartest.Scalar(b, columnar.KindUTF8, "target")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = SubstrInsensitive(benchAlloc, haystack, needle, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}

func BenchmarkSubstr(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	needle := columnartest.Scalar(b, columnar.KindUTF8, "TaRgEt")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = Substr(benchAlloc, haystack, needle, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}
