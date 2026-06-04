package tsdb

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// This file benchmarks the three perf changes on this branch so we can validate
// (and regression-track) them:
//
//  1. SymbolCache — per-query memoization of symbol-ref -> string. Validated by
//     comparing cache=false vs cache=true: label values repeat heavily across
//     series, so the cache should cut both time and (on pread) bytes read.
//  2. Bounded symbol reads — Lookup/ReverseLookup read one sampled block instead
//     of the whole file. Validated by BenchmarkTSDBIndexReadAmplification: the
//     bytes-read-per-series under pread must stay small and roughly flat as the
//     index grows. Without bounding, each symbol lookup would read ~the whole
//     file and amplification would explode with cardinality.
//  3. mmap -> pread (io.ReaderAt) — the production swap. Validated by comparing
//     the mmap and pread backings on the same on-disk index, both single-threaded
//     (BenchmarkTSDBIndexRead) and under concurrency (BenchmarkTSDBIndexReadConcurrent).
//
// IMPORTANT caveat on interpreting mmap vs pread here: a microbenchmark runs
// against a warm page cache with no memory pressure, which is mmap's best case
// (faulted pages stay resident, no scheduler stalls). The motivation for the
// swap — mmap minor-fault scheduler stalls on the index-gateway — only shows up
// under real memory pressure and concurrent load. So treat "pread is within Nx
// of mmap on latency, with bounded read amplification" as the success bar here;
// the stall win must be confirmed on a loaded host (or with cgroup memory limits
// + concurrent queries), not from these numbers alone.
//
// Run examples:
//
//	go test -run='^$' -bench=BenchmarkTSDBIndexRead$ -benchmem ./pkg/storage/stores/shipper/indexshipper/tsdb/
//	go test -run='^$' -bench=BenchmarkTSDBIndexReadAmplification -benchmem ./pkg/.../tsdb/
//	go test -run='^$' -bench=BenchmarkTSDBIndexReadConcurrent -cpu=1,8,32 ./pkg/.../tsdb/

// benchScales models a few realistic index sizes (k8s logging cardinality, see
// genSeries). Kept well under the 500k series that would OOM a normal test run.
var benchScales = []struct {
	name      string
	nSeries   int
	chunksPer int
}{
	{"1k_x6", 1_000, 6},
	{"10k_x12", 10_000, 12},
	{"50k_x12", 50_000, 12},
}

// allSeriesMatcher matches every generated series: every stream is stdout or
// stderr, and "std.+" matches both. Using a low-cardinality label keeps the
// postings construction cheap so the benchmark measures the per-series read
// path, not postings merging.
var allSeriesMatcher = labels.MustNewMatcher(labels.MatchRegexp, "stream", "std.+")

// writeIndexFile builds an in-memory TSDB index and writes it to a temp file,
// returning the path and on-disk size. We build in memory and write the bytes
// ourselves rather than using Builder.Build, whose Identifier.Path() semantics
// are about object-store layout, not a local file we can reopen.
func writeIndexFile(tb testing.TB, nSeries, chunksPer int) (path string, size int64) {
	tb.Helper()
	bld := NewBuilder(index.FormatV3)
	for _, s := range genSeries(nSeries, chunksPer) {
		bld.AddSeries(s.Labels, model.Fingerprint(labels.StableHash(s.Labels)), s.Chunks)
	}
	_, data, err := bld.BuildInMemory(context.Background(), func(from, through model.Time, checksum uint32) Identifier {
		return SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: from, Through: through, Checksum: checksum}
	})
	if err != nil {
		tb.Fatalf("build index: %v", err)
	}
	path = filepath.Join(tb.TempDir(), "index.tsdb")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		tb.Fatalf("write index: %v", err)
	}
	return path, int64(len(data))
}

// openReader opens the on-disk index with the requested backing.
func openReader(tb testing.TB, backing, path string) (*index.Reader, func()) {
	tb.Helper()
	var (
		r   *index.Reader
		err error
	)
	switch backing {
	case "mmap":
		r, err = index.NewFileReader(path)
	case "pread":
		r, err = index.NewFileReaderAt(path)
	default:
		tb.Fatalf("unknown backing %q", backing)
	}
	if err != nil {
		tb.Fatalf("open %s reader: %v", backing, err)
	}
	return r, func() { _ = r.Close() }
}

// scanOnce performs one full scan: resolve all postings for the matcher, then
// read every series via the op ("series" => Series, anything else => ChunkStats),
// optionally sharing a per-scan SymbolCache. This mirrors TSDBIndex.forSeriesAndLabels
// / Stats / Volume. It takes no *testing.B so it is safe to call from RunParallel
// goroutines. Returns the number of series scanned.
func scanOnce(r *index.Reader, op string, useCache bool) (int, error) {
	p, err := PostingsForMatchers(r, nil, allSeriesMatcher)
	if err != nil {
		return 0, err
	}
	var cache *index.SymbolCache
	if useCache {
		cache = index.NewSymbolCache()
	}
	var (
		ls   labels.Labels
		chks []index.ChunkMeta
	)
	const from, through = int64(0), int64(math.MaxInt64)
	n := 0
	for p.Next() {
		ref := p.At()
		if op == "series" {
			if _, err = r.Series(ref, from, through, &ls, &chks, cache); err != nil {
				return n, err
			}
		} else {
			if _, _, err = r.ChunkStats(ref, from, through, &ls, nil, cache); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, p.Err()
}

// BenchmarkTSDBIndexRead is the primary matrix: scale x op x backing x cache,
// single-threaded, reporting ns/op + B/op + allocs/op + series/op.
func BenchmarkTSDBIndexRead(b *testing.B) {
	for _, sc := range benchScales {
		b.Run(sc.name, func(b *testing.B) {
			path, size := writeIndexFile(b, sc.nSeries, sc.chunksPer)
			b.Logf("index: %.2f MB (%d series x %d chunks)", float64(size)/(1<<20), sc.nSeries, sc.chunksPer)

			for _, op := range []string{"series", "chunkstats"} {
				for _, backing := range []string{"mmap", "pread"} {
					for _, useCache := range []bool{false, true} {
						name := fmt.Sprintf("%s/%s/cache=%t", op, backing, useCache)
						b.Run(name, func(b *testing.B) {
							r, closeFn := openReader(b, backing, path)
							defer closeFn()

							b.ReportAllocs()
							b.ResetTimer()
							var series int
							for i := 0; i < b.N; i++ {
								n, err := scanOnce(r, op, useCache)
								if err != nil {
									b.Fatal(err)
								}
								series = n
							}
							b.StopTimer()
							if series > 0 {
								b.ReportMetric(float64(series), "series/op")
							}
						})
					}
				}
			}
		})
	}
}

// BenchmarkTSDBIndexReadConcurrent stresses the backings under concurrent scans,
// which is where mmap page-fault contention and pread syscall overhead diverge.
// Run with -cpu=1,8,32 to vary parallelism. Cache is on (production behavior).
func BenchmarkTSDBIndexReadConcurrent(b *testing.B) {
	for _, sc := range benchScales {
		b.Run(sc.name, func(b *testing.B) {
			path, _ := writeIndexFile(b, sc.nSeries, sc.chunksPer)
			for _, backing := range []string{"mmap", "pread"} {
				b.Run(backing, func(b *testing.B) {
					r, closeFn := openReader(b, backing, path)
					defer closeFn()

					b.ReportAllocs()
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if _, err := scanOnce(r, "series", true); err != nil {
								// b.Fatal is not safe off the benchmark goroutine.
								panic(err)
							}
						}
					})
				})
			}
		})
	}
}

// BenchmarkTSDBIndexReadAmplification quantifies read amplification on the pread
// backing: bytes and read calls per series, and bytes-per-scan as a multiple of
// the file size. These are the numbers that prove the bounded-read change works
// (small, cardinality-flat per-series reads) and that the SymbolCache pays off
// (cache=true should sharply reduce both reads/series and readB/series).
func BenchmarkTSDBIndexReadAmplification(b *testing.B) {
	for _, sc := range benchScales {
		b.Run(sc.name, func(b *testing.B) {
			path, size := writeIndexFile(b, sc.nSeries, sc.chunksPer)
			for _, useCache := range []bool{false, true} {
				b.Run(fmt.Sprintf("series/cache=%t", useCache), func(b *testing.B) {
					f, err := os.Open(path)
					if err != nil {
						b.Fatal(err)
					}
					defer f.Close()
					fi, err := f.Stat()
					if err != nil {
						b.Fatal(err)
					}
					// Wrap the file so we can count reads, then build the reader
					// on the pread byte slice exactly as NewFileReaderAt would.
					cr := &countingReaderAt{r: f}
					r, err := index.NewReader(index.NewReaderAtByteSlice(cr, fi.Size()))
					if err != nil {
						b.Fatal(err)
					}

					cr.reset() // exclude open/TOC/symbol-table parsing; measure scans only
					b.ResetTimer()
					total := 0
					for i := 0; i < b.N; i++ {
						n, err := scanOnce(r, "series", useCache)
						if err != nil {
							b.Fatal(err)
						}
						total += n
					}
					b.StopTimer()

					if total > 0 {
						b.ReportMetric(float64(cr.byteN())/float64(total), "readB/series")
						b.ReportMetric(float64(cr.callN())/float64(total), "reads/series")
					}
					if b.N > 0 && size > 0 {
						b.ReportMetric(float64(cr.byteN())/float64(b.N)/float64(size), "xfile/scan")
					}
				})
			}
		})
	}
}
