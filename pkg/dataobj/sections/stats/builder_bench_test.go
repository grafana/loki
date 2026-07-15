package stats

import (
	"fmt"
	"testing"
)

// sizeSink prevents the compiler from eliminating the EstimatedSize calls.
var sizeSink int

// BenchmarkBuilderAppend reproduces how index compaction drives the stats
// builder. indexobj.MergeBuilder.AppendStat queries EstimatedSize before and
// after every Append to decide when to cut a section, so the merge performs two
// EstimatedSize calls per row.
//
// EstimatedSize used to recompute its result by walking every accumulated row
// and every label in each row, making this loop O(n²) in the number of rows —
// which is why merging a tenant with many stats rows "took ages". It is now
// maintained incrementally in Append, so each call is O(1). Run across branches
// with benchstat to see the difference; the per-row-count sub-benchmarks make
// the quadratic-vs-linear scaling visible on its own.
func BenchmarkBuilderAppend(b *testing.B) {
	for _, n := range []int{1_000, 5_000, 20_000} {
		rows := makeBenchStats(n)
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			var sink int
			for range b.N {
				bld := NewBuilder(nil, ColumnarSectionEncoder(2048, 0))
				bld.SetTenant("tenant")
				for _, r := range rows {
					// Mirror indexobj.MergeBuilder.AppendStat's pre/post size checks.
					sink += bld.EstimatedSize()
					bld.Append(r)
					sink += bld.EstimatedSize()
				}
			}
			sizeSink = sink
		})
	}
}

// makeBenchStats builds n stats rows with a realistic multi-label sort schema so
// EstimatedSize's per-row label walk is exercised at a representative width.
func makeBenchStats(n int) []Stat {
	rows := make([]Stat, n)
	for i := range rows {
		rows[i] = Stat{
			ObjectPath:   fmt.Sprintf("logs/tenant/obj-%06d", i),
			SectionIndex: int64(i % 8),
			SortSchema:   "service_name,namespace,pod",
			Labels: map[string]string{
				"service_name": fmt.Sprintf("svc-%d", i%512),
				"namespace":    fmt.Sprintf("ns-%d", i%32),
				"pod":          fmt.Sprintf("pod-%d", i),
			},
			MinTimestamp:     int64(i),
			MaxTimestamp:     int64(i) + 1,
			RowCount:         int64(i),
			UncompressedSize: int64(i) * 10,
		}
	}
	return rows
}
