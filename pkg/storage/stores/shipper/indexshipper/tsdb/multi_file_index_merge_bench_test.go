package tsdb

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"testing"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// benchInputs deterministically produces K per-input, fingerprint-sorted
// slices of ChunkRefWithSizingInfo. `overlapPct` (0..100) is the fraction of
// fingerprints in each list that are shared across ALL K lists. The rest are
// unique to that list. Fingerprints are generated with a dithered (strided +
// small jitter) pattern rather than uniform-random, since uniform-random
// input is close to adversarial for the pattern-defeating quicksort that
// backs sort.Slice and overstates the current path's cost.
func benchInputs(k, n, overlapPct int, seed int64) [][]logproto.ChunkRefWithSizingInfo {
	if overlapPct < 0 {
		overlapPct = 0
	}
	if overlapPct > 100 {
		overlapPct = 100
	}
	shared := n * overlapPct / 100
	unique := n - shared

	rng := rand.New(rand.NewSource(seed))

	// Shared fingerprints appear in every list. Give them fingerprints in
	// [0, sharedSpan) with jitter so they're not perfectly uniform.
	sharedFPs := make([]uint64, shared)
	{
		var cur uint64
		for i := 0; i < shared; i++ {
			cur += uint64(1 + rng.Intn(8))
			sharedFPs[i] = cur
		}
	}
	// Ensure per-list unique fingerprints don't collide with shared or each
	// other by placing them well above sharedFPs' max.
	base := uint64(0)
	if shared > 0 {
		base = sharedFPs[shared-1] + 1
	}
	// Give each list its own disjoint band.
	bandStride := uint64(unique*16 + 1)

	out := make([][]logproto.ChunkRefWithSizingInfo, k)
	for listIdx := 0; listIdx < k; listIdx++ {
		list := make([]logproto.ChunkRefWithSizingInfo, 0, n)

		// Shared entries first — each list gets an identical copy so dedup
		// has real work to do. From/Through/Checksum must match too;
		// otherwise the dedup key differs and no dedup happens.
		for _, fp := range sharedFPs {
			list = append(list, logproto.ChunkRefWithSizingInfo{
				ChunkRef: logproto.ChunkRef{
					Fingerprint: fp,
					From:        model.Time(int64(fp) * 100),
					Through:     model.Time(int64(fp)*100 + 60),
					Checksum:    uint32(fp),
				},
				KB:      4,
				Entries: 100,
			})
		}
		// Unique entries live in this list's band.
		bandLo := base + uint64(listIdx)*bandStride
		var cur uint64 = bandLo
		for i := 0; i < unique; i++ {
			cur += uint64(1 + rng.Intn(8))
			list = append(list, logproto.ChunkRefWithSizingInfo{
				ChunkRef: logproto.ChunkRef{
					Fingerprint: cur,
					From:        model.Time(int64(cur) * 100),
					Through:     model.Time(int64(cur)*100 + 60),
					Checksum:    uint32(cur),
				},
				KB:      4,
				Entries: 100,
			})
		}
		// Per-file GetChunkRefs walks TSDB postings in on-disk order — which
		// is fingerprint-sorted. Match that here.
		sort.Slice(list, func(i, j int) bool { return list[i].Less(list[j].ChunkRef) })
		out[listIdx] = list
	}
	return out
}

// cloneInputs makes a deep copy so a merge that consumes/mutates the input
// slices doesn't taint later iterations.
func cloneInputs(src [][]logproto.ChunkRefWithSizingInfo) [][]logproto.ChunkRefWithSizingInfo {
	out := make([][]logproto.ChunkRefWithSizingInfo, len(src))
	for i, s := range src {
		out[i] = append([]logproto.ChunkRefWithSizingInfo(nil), s...)
	}
	return out
}

// benchMatrix defines the parameter sweep from the investigation.
var (
	benchKs        = []int{1, 2, 4, 8, 16}
	benchNs        = []int{1_000, 10_000, 100_000, 1_000_000}
	benchOverlaps  = []int{0, 10, 50}
	benchSeed      = int64(0xC0FFEE)
	skipHugeCells  = true // set false to run the 1M cells too
	skipHugeThresh = 1_000_000
)

func benchName(k, n, overlap int) string {
	return fmt.Sprintf("K=%d/N=%d/overlap=%d", k, n, overlap)
}

func runMergeBench(b *testing.B, merge func([][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo) {
	for _, k := range benchKs {
		for _, n := range benchNs {
			for _, overlap := range benchOverlaps {
				k, n, overlap := k, n, overlap
				if skipHugeCells && n >= skipHugeThresh && k >= 8 {
					// N=1M K=16 is ~1GB of live inputs per iteration; opt out
					// unless the user explicitly enables it.
					continue
				}
				b.Run(benchName(k, n, overlap), func(b *testing.B) {
					src := benchInputs(k, n, overlap, benchSeed)
					b.ResetTimer()
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						xs := cloneInputs(src)
						b.StartTimer()
						out := merge(xs)
						if len(out) == 0 && n > 0 {
							b.Fatalf("expected non-empty merged output")
						}
					}
				})
			}
		}
	}
}

func BenchmarkMergeChunkRefsSort(b *testing.B) {
	runMergeBench(b, func(xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
		return mergeChunkRefsSort(nil, xs)
	})
}

func BenchmarkMergeChunkRefsHeap(b *testing.B) {
	runMergeBench(b, func(xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
		return mergeChunkRefsHeap(nil, xs)
	})
}

// BenchmarkMergeChunkRefsSortPeakMem materialises the largest cell once and
// records peak resident memory delta using runtime.ReadMemStats before and
// after. This is the memory equivalent of the ns/op sweep above — it is not
// something go test can report directly for us. Run with -run='^$'
// -bench=BenchmarkMergeChunkRefsSortPeakMem -benchtime=1x.
func BenchmarkMergeChunkRefsSortPeakMem(b *testing.B) {
	benchPeakMem(b, mergeChunkRefsSort)
}

func BenchmarkMergeChunkRefsHeapPeakMem(b *testing.B) {
	benchPeakMem(b, mergeChunkRefsHeap)
}

func benchPeakMem(
	b *testing.B,
	merge func([]logproto.ChunkRefWithSizingInfo, [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo,
) {
	const (
		k       = 16
		n       = 1_000_000
		overlap = 10
	)
	src := benchInputs(k, n, overlap, benchSeed)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	xs := cloneInputs(src)
	out := merge(nil, xs)
	runtime.KeepAlive(out)

	runtime.ReadMemStats(&after)

	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/(1<<20), "heapAllocMB")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/(1<<20), "totalAllocMB")
	b.ReportMetric(float64(after.Sys-before.Sys)/(1<<20), "sysMB")
	b.ReportMetric(float64(len(out)), "merged")
}
