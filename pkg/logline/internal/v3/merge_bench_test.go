package v3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// sampleIndexPath returns the absolute path to a named sample index file
// (small/medium/large). The sample indexes live at the repository root
// testdata/sample-indexes/<name>/index, two levels above pkg/logline/.
// Returns ("", 0) when the file does not exist (CI without testdata).
func sampleIndexPath(name string) (path string, size int64) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", 0
	}
	// thisFile is .../pkg/logline/streaming_merge_bench_test.go
	// Navigate up to repo root: logline/ → pkg/ → repo root
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	p := filepath.Join(root, "testdata", "sample-indexes", name, "index")
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", 0
	}
	return p, info.Size()
}

// requireSampleIndex returns the sample index path or skips the benchmark.
func requireSampleIndex(b *testing.B, name string) (string, int64) {
	b.Helper()
	p, size := sampleIndexPath(name)
	if p == "" {
		b.Skipf("sample index %q not available (run from a checkout with testdata/)", name)
	}
	return p, size
}

func benchmarkMerge(b *testing.B, inputs []string, totalBytes int64) {
	b.Helper()
	b.SetBytes(totalBytes)
	b.ReportAllocs()

	cfg := DefaultFastIndexWriteConfig()
	outDir := b.TempDir()
	outPath := filepath.Join(outDir, "merged.idx")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := os.Create(outPath)
		if err != nil {
			b.Fatal(err)
		}
		_, mergeErr := mergeFilesTo(context.Background(), b, inputs, f, cfg)
		closeErr := f.Close()
		if mergeErr != nil {
			b.Fatal(mergeErr)
		}
		if closeErr != nil {
			b.Fatal(closeErr)
		}
		if err := os.Remove(outPath); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
	}
}

// --- File-backed streaming merge benchmarks ---

func BenchmarkStreamingMerge_SmallSmall(b *testing.B) {
	p, size := requireSampleIndex(b, "small")
	benchmarkMerge(b, []string{p, p}, size*2)
}

func BenchmarkStreamingMerge_SmallMedium(b *testing.B) {
	ps, ss := requireSampleIndex(b, "small")
	pm, sm := requireSampleIndex(b, "medium")
	benchmarkMerge(b, []string{ps, pm}, ss+sm)
}

func BenchmarkStreamingMerge_MediumMedium(b *testing.B) {
	p, size := requireSampleIndex(b, "medium")
	benchmarkMerge(b, []string{p, p}, size*2)
}

func BenchmarkStreamingMerge_MediumLarge(b *testing.B) {
	pm, sm := requireSampleIndex(b, "medium")
	pl, sl := requireSampleIndex(b, "large")
	benchmarkMerge(b, []string{pm, pl}, sm+sl)
}

func BenchmarkStreamingMerge_LargeLarge(b *testing.B) {
	p, size := requireSampleIndex(b, "large")
	benchmarkMerge(b, []string{p, p}, size*2)
}

// --- Synthetic benchmarks (no sample indexes needed) ---

// buildSyntheticIndex creates an index file with the given number of documents
// and terms. Terms are sorted 6-byte ngrams; each term appears in ~10% of docs.
func buildSyntheticIndex(b *testing.B, dir string, fileIdx, numDocs, numTerms int) string {
	b.Helper()
	path := filepath.Join(dir, fmt.Sprintf("input-%d.idx", fileIdx))
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0 // disable for benchmarks
	w, err := newStreamingIndexWriter(path, cfg, uint32(numDocs))
	if err != nil {
		b.Fatal(err)
	}

	baseTime := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	for i := range numDocs {
		w.AddDocument(format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: baseTime.Add(time.Duration(fileIdx*numDocs+i) * time.Minute).UnixMilli(),
			MaxTimeUnix: baseTime.Add(time.Duration(fileIdx*numDocs+i+1) * time.Minute).UnixMilli(),
		})
	}

	// Build sorted terms. The encoding guarantees ascending order.
	type termBM struct {
		term [8]byte
		ids  []uint32
	}
	terms := make([]termBM, numTerms)
	for t := range numTerms {
		var term [8]byte
		term[0] = byte(t >> 16)
		term[1] = byte(t >> 8)
		term[2] = byte(t)
		term[3] = byte('A' + (t % 26))
		term[4] = byte('A' + ((t / 26) % 26))
		term[5] = byte('A' + ((t / 676) % 26))
		var ids []uint32
		for d := t % 10; d < numDocs; d += 10 {
			ids = append(ids, uint32(d))
		}
		terms[t] = termBM{term: term, ids: ids}
	}
	sort.Slice(terms, func(i, j int) bool {
		return compareTerm8(terms[i].term, terms[j].term) < 0
	})

	bm := roaring.New()
	for _, t := range terms {
		bm.Clear()
		bm.AddMany(t.ids)
		if err := w.WriteTermBitmap(t.term, format.Bitmap{Roaring: bm}); err != nil {
			b.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkStreamingMerge_Synthetic exercises the streaming merge with
// generated data at scales that match production compaction workloads.
// The key dimension is input count × term count — the term dictionary is
// walked lazily block-by-block per input, so memory scales with the number
// of active blocks rather than the total term count.
func BenchmarkStreamingMerge_Synthetic(b *testing.B) {
	for _, tc := range []struct {
		name  string
		files int
		docs  int
		terms int
	}{
		{"2x1000doc_10000term", 2, 1000, 10000},
		{"8x1000doc_10000term", 8, 1000, 10000},
		{"16x1000doc_50000term", 16, 1000, 50000},
		{"32x1000doc_50000term", 32, 1000, 50000},
		// Multi-block: 200000 terms spans 2 blocks (TermDictBlockSize=131072).
		// This exercises the lazy iterator's block-crossing and eviction.
		{"4x100doc_200000term", 4, 100, 200000},
		{"8x100doc_200000term", 8, 100, 200000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dir := b.TempDir()
			var inputPaths []string
			var totalBytes int64
			for f := 0; f < tc.files; f++ {
				p := buildSyntheticIndex(b, dir, f, tc.docs, tc.terms)
				info, err := os.Stat(p)
				if err != nil {
					b.Fatal(err)
				}
				totalBytes += info.Size()
				inputPaths = append(inputPaths, p)
			}

			benchmarkMerge(b, inputPaths, totalBytes)
		})
	}
}
