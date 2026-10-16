package logline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// latencyReaderAt wraps an io.ReaderAt and injects a configurable latency on
// every ReadAt call to simulate object-storage round trips.
type latencyReaderAt struct {
	inner   io.ReaderAt
	latency time.Duration
}

func (r *latencyReaderAt) ReadAt(p []byte, off int64) (int, error) {
	time.Sleep(r.latency)
	return r.inner.ReadAt(p, off)
}

// benchIndex holds a pre-built index on disk for repeated benchmark opens.
type benchIndex struct {
	path   string
	size   int64
	header format.HeaderInfo
	cached any // opaque state from first open
}

// buildBenchIndex creates a deterministic index with the given doc/term counts.
func buildBenchIndex(b *testing.B, version string, docCount, termCount int) benchIndex {
	b.Helper()

	dir := b.TempDir()
	path := filepath.Join(dir, "bench.lidx")

	docs := make([]format.DocumentMetadata, docCount)
	for i := range docs {
		docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: int64(i) * 1000,
			MaxTimeUnix: int64(i)*1000 + 999,
		}
	}

	w, err := NewWriter(version, path, docs, nil)
	require.NoError(b, err)

	bm := roaring.New()
	for i := range termCount {
		bm.Clear()
		// Each term covers a sliding window of 10% of docs (at least 1).
		windowSize := max(docCount/10, 1)
		start := (i * windowSize / 2) % docCount
		for j := range windowSize {
			bm.Add(uint32((start + j) % docCount))
		}
		var term [8]byte
		copy(term[:], fmt.Sprintf("T%05d", i))
		require.NoError(b, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
	}
	require.NoError(b, w.Close())

	fi, err := os.Stat(path)
	require.NoError(b, err)

	f, err := os.Open(path)
	require.NoError(b, err)
	defer f.Close()

	reader, _, cached, err := OpenReaderAt(f, 0, fi.Size())
	require.NoError(b, err)
	header := reader.ReadHeader()
	require.NoError(b, reader.Close())

	return benchIndex{path: path, size: fi.Size(), header: header, cached: cached}
}

// --- Write benchmarks ---

func BenchmarkWriter(b *testing.B) {
	for _, version := range AllVersions() {
		for _, tc := range []struct {
			name      string
			docCount  int
			termCount int
		}{
			{"small_10d_100t", 10, 100},
			{"medium_100d_1000t", 100, 1000},
			{"large_1000d_10000t", 1000, 10000},
		} {
			b.Run(version+"/"+tc.name, func(b *testing.B) {
				docs := make([]format.DocumentMetadata, tc.docCount)
				for i := range docs {
					docs[i] = format.DocumentMetadata{
						ID:          uint32(i),
						MinTimeUnix: int64(i) * 1000,
						MaxTimeUnix: int64(i)*1000 + 999,
					}
				}

				// Pre-build sorted terms and bitmaps.
				type termEntry struct {
					key [8]byte
					bm  *roaring.Bitmap
				}
				terms := make([]termEntry, tc.termCount)
				for i := range terms {
					copy(terms[i].key[:], fmt.Sprintf("T%05d", i))
					terms[i].bm = roaring.New()
					windowSize := max(tc.docCount/10, 1)
					start := (i * windowSize / 2) % tc.docCount
					for j := range windowSize {
						terms[i].bm.Add(uint32((start + j) % tc.docCount))
					}
				}

				dir := b.TempDir()
				b.ResetTimer()

				for b.Loop() {
					path := filepath.Join(dir, fmt.Sprintf("bench-%d.lidx", b.N))
					w, err := NewWriter(version, path, docs, nil)
					if err != nil {
						b.Fatal(err)
					}
					for _, te := range terms {
						if err := w.WriteTermBitmap(te.key, format.Bitmap{Roaring: te.bm}); err != nil {
							b.Fatal(err)
						}
					}
					if err := w.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// --- Read benchmarks (with simulated object-storage latency) ---

func BenchmarkReader_Query(b *testing.B) {
	const objStoreLatency = 250 * time.Millisecond

	for _, version := range AllVersions() {
		for _, tc := range []struct {
			name      string
			docCount  int
			termCount int
			querySize int // number of terms per query
		}{
			{"small_1term", 100, 100, 1},
			{"small_3terms", 100, 100, 3},
			{"medium_1term", 1000, 1000, 1},
			{"medium_3terms", 1000, 1000, 3},
		} {
			b.Run(version+"/"+tc.name, func(b *testing.B) {
				idx := buildBenchIndex(b, version, tc.docCount, tc.termCount)

				// Build query terms from the middle of the term space.
				queryTerms := make([]string, tc.querySize)
				mid := tc.termCount / 2
				for i := range queryTerms {
					queryTerms[i] = fmt.Sprintf("T%05d", mid+i)
				}

				b.ResetTimer()

				for b.Loop() {
					f, err := os.Open(idx.path)
					if err != nil {
						b.Fatal(err)
					}
					slow := &latencyReaderAt{inner: f, latency: objStoreLatency}

					reader, _, err := OpenReader(version, slow, 0, idx.size, idx.header)
					if err != nil {
						f.Close()
						b.Fatal(err)
					}
					if err := benchQueryTerms(reader, queryTerms); err != nil {
						reader.Close()
						f.Close()
						b.Fatal(err)
					}
					reader.Close()
					f.Close()
				}
			})
		}
	}
}

func BenchmarkReader_CachedOpen(b *testing.B) {
	const objStoreLatency = 250 * time.Millisecond

	for _, version := range AllVersions() {
		b.Run(version, func(b *testing.B) {
			idx := buildBenchIndex(b, version, 100, 100)

			queryTerms := []string{"T00050"}

			b.ResetTimer()

			for b.Loop() {
				f, err := os.Open(idx.path)
				if err != nil {
					b.Fatal(err)
				}
				slow := &latencyReaderAt{inner: f, latency: objStoreLatency}

				reader, err := OpenReaderCached(version, slow, 0, idx.size, idx.cached)
				if err != nil {
					f.Close()
					b.Fatal(err)
				}
				if err := benchQueryTerms(reader, queryTerms); err != nil {
					reader.Close()
					f.Close()
					b.Fatal(err)
				}
				reader.Close()
				f.Close()
			}
		})
	}
}

func BenchmarkReader_ConcurrentQueries(b *testing.B) {
	const objStoreLatency = 250 * time.Millisecond
	const concurrency = 8

	for _, version := range AllVersions() {
		b.Run(version, func(b *testing.B) {
			idx := buildBenchIndex(b, version, 1000, 1000)

			b.ResetTimer()

			for b.Loop() {
				var wg sync.WaitGroup
				wg.Add(concurrency)
				for g := range concurrency {
					go func() {
						defer wg.Done()
						f, err := os.Open(idx.path)
						if err != nil {
							b.Error(err)
							return
						}
						defer f.Close()

						slow := &latencyReaderAt{inner: f, latency: objStoreLatency}
						reader, err := OpenReaderCached(version, slow, 0, idx.size, idx.cached)
						if err != nil {
							b.Error(err)
							return
						}
						defer reader.Close()

						term := fmt.Sprintf("T%05d", g*100)
						if err := benchQueryTerms(reader, []string{term}); err != nil {
							b.Error(err)
						}
					}()
				}
				wg.Wait()
			}
		})
	}
}

func BenchmarkOpenReaderAt(b *testing.B) {
	const objStoreLatency = 250 * time.Millisecond

	for _, version := range AllVersions() {
		b.Run(version, func(b *testing.B) {
			idx := buildBenchIndex(b, version, 100, 100)

			b.ResetTimer()

			for b.Loop() {
				f, err := os.Open(idx.path)
				if err != nil {
					b.Fatal(err)
				}
				slow := &latencyReaderAt{inner: f, latency: objStoreLatency}

				reader, _, _, err := OpenReaderAt(slow, 0, idx.size)
				if err == nil {
					reader.Close()
				}
				f.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchQueryTerms(reader Reader, terms []string) error {
	result := format.Bitmap{MatchesAll: true}
	for _, term := range terms {
		idx, err := reader.FindTerm(term)
		if err != nil {
			return err
		}
		if idx < 0 {
			return nil
		}
		res, err := reader.GetBitmap(idx)
		if err != nil {
			return err
		}
		result = result.And(res)
		if result.IsEmpty() {
			return nil
		}
	}
	return nil
}
