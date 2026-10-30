package compactor

// Benchmarks for compaction performance. These measure the three bottleneck phases:
// download, merge, and upload. In production, download from remote object storage
// dominates wall-clock time; the merge is CPU-bound.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/logline"
	"github.com/grafana/loki/v4/pkg/logline/format"
	schedulerapi "github.com/grafana/loki/v4/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v4/pkg/logline/store"
)

// buildBenchIndex creates a realistic index with the given number of documents and terms.
// Each document gets unique terms to create non-trivial bitmaps for the merge.
func buildBenchIndex(b *testing.B, s *store.Store, date, hash string, numDocs, numTerms int) store.Meta {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, hash+".idx")

	// Create documents with realistic time ranges
	baseTime := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	docs := make([]format.DocumentMetadata, numDocs)
	for i := 0; i < numDocs; i++ {
		docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: baseTime.Add(time.Duration(i) * time.Minute).UnixMilli(),
			MaxTimeUnix: baseTime.Add(time.Duration(i+1) * time.Minute).UnixMilli(),
		}
	}

	w, err := logline.NewWriter("v3", path, docs, nil)
	require.NoError(b, err)

	// Create terms with bitmaps spanning multiple documents
	for t := 0; t < numTerms; t++ {
		var term [8]byte
		term[0] = byte(t >> 16)
		term[1] = byte(t >> 8)
		term[2] = byte(t)
		term[3] = byte('A' + (t % 26))
		term[4] = byte('A' + ((t / 26) % 26))
		term[5] = byte('A' + ((t / 676) % 26))

		bm := roaring.New()
		// Each term appears in ~10% of documents
		for d := t % 10; d < numDocs; d += 10 {
			bm.Add(uint32(d))
		}
		require.NoError(b, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
	}
	require.NoError(b, w.Close())

	f, err := os.Open(path)
	require.NoError(b, err)
	defer f.Close()

	meta := store.Meta{
		Date: date, StorageID: hash, Version: "v3",
		MinLogTs:    baseTime,
		MaxLogTs:    baseTime.Add(time.Duration(numDocs) * time.Minute),
		MinRecordTs: baseTime,
		MaxRecordTs: baseTime.Add(time.Duration(numDocs) * time.Minute),
	}
	require.NoError(b, populateFileInfo(f, &meta))
	err = s.PutIndex(context.Background(), f, meta)
	require.NoError(b, err)
	return meta
}

func newBenchCompactor(b *testing.B, s *store.Store) *Service {
	b.Helper()
	cfg := Config{Compaction: logline.CompactionConfig{
		SchedulerAddress: "http://localhost:9999",
		CompactorID:      "bench-compactor",
	}}
	require.NoError(b, cfg.Validate())
	svc, err := New(s, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(b, err)
	return svc
}

// BenchmarkCompact_EndToEnd measures total compaction time including
// download from in-memory bucket, merge, hash, and upload.
// In production, download from remote storage dominates; this benchmark
// measures the CPU-bound merge + hash + local I/O portion.
func BenchmarkCompact_EndToEnd(b *testing.B) {
	for _, tc := range []struct {
		name    string
		sources int
		docs    int
		terms   int
	}{
		{"2x100doc_1000term", 2, 100, 1000},
		{"4x100doc_1000term", 4, 100, 1000},
		{"4x1000doc_10000term", 4, 1000, 10000},
		{"8x1000doc_10000term", 8, 1000, 10000},
		{"4x5000doc_50000term", 4, 5000, 50000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			bucket := objstore.NewInMemBucket()
			cfg := store.Config{MinDate: "0001-01-01"}
			require.NoError(b, cfg.Validate())
			// Wrap the bucket so the streaming upload in compact() doesn't hold
			// InMemBucket's write lock while source range-reads are in flight
			// (see streamingInMemBucket in compactor_test.go).
			s, err := store.NewStore(&streamingInMemBucket{Bucket: bucket}, cfg, log.NewNopLogger(), nil)
			require.NoError(b, err)
			svc := newBenchCompactor(b, s)

			// Pre-create source indexes
			var sourceIDs []string
			for i := 0; i < tc.sources; i++ {
				hash := fmt.Sprintf("%016x", i)
				meta := buildBenchIndex(b, s, "2026-02-25", hash, tc.docs, tc.terms)
				sourceIDs = append(sourceIDs, meta.ID())
			}

			job := &schedulerapi.JobResponse{
				ID:        "benchjob",
				SourceIDs: sourceIDs,
				ExpiresAt: time.Now().Add(10 * time.Minute),
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				err := svc.compact(context.Background(), job)
				require.NoError(b, err)
			}
		})
	}
}

// BenchmarkCompact_MergeOnly isolates the merge phase to measure CPU cost
// independent of I/O. Creates local files directly and merges them.
func BenchmarkCompact_MergeOnly(b *testing.B) {
	for _, tc := range []struct {
		name    string
		files   int
		docs    int
		terms   int
		overlap float64 // fraction of docs that share time bounds across files (0.0 = none, 1.0 = all)
	}{
		{"2x1000doc_10000term_noOverlap", 2, 1000, 10000, 0.0},
		{"4x1000doc_10000term_noOverlap", 4, 1000, 10000, 0.0},
		{"4x5000doc_50000term_noOverlap", 4, 5000, 50000, 0.0},
		{"8x5000doc_50000term_noOverlap", 8, 5000, 50000, 0.0},
		{"4x1000doc_10000term_80pctOverlap", 4, 1000, 10000, 0.8},
		{"8x5000doc_50000term_80pctOverlap", 8, 5000, 50000, 0.8},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dir := b.TempDir()
			var inputPaths []string
			baseTime := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
			overlapDocs := int(float64(tc.docs) * tc.overlap)

			for f := 0; f < tc.files; f++ {
				path := filepath.Join(dir, fmt.Sprintf("input-%d.idx", f))

				// First overlapDocs documents share time bounds across all files.
				// Remaining documents are unique per file.
				fileDocs := make([]format.DocumentMetadata, tc.docs)
				for i := 0; i < tc.docs; i++ {
					var minT, maxT int64
					if i < overlapDocs {
						minT = baseTime.Add(time.Duration(i) * time.Minute).UnixMilli()
						maxT = baseTime.Add(time.Duration(i+1) * time.Minute).UnixMilli()
					} else {
						offset := time.Duration(f*tc.docs+i) * time.Minute
						minT = baseTime.Add(offset).UnixMilli()
						maxT = baseTime.Add(offset + time.Minute).UnixMilli()
					}
					fileDocs[i] = format.DocumentMetadata{
						ID:          uint32(i),
						MinTimeUnix: minT,
						MaxTimeUnix: maxT,
					}
				}
				w, err := logline.NewWriter("v3", path, fileDocs, nil)
				require.NoError(b, err)
				for t := 0; t < tc.terms; t++ {
					var term [8]byte
					term[0] = byte(t >> 16)
					term[1] = byte(t >> 8)
					term[2] = byte(t)
					term[3] = byte('A' + (t % 26))
					term[4] = byte('A' + ((t / 26) % 26))
					term[5] = byte('A' + ((t / 676) % 26))
					bm := roaring.New()
					for d := t % 10; d < tc.docs; d += 10 {
						bm.Add(uint32(d))
					}
					require.NoError(b, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
				}
				require.NoError(b, w.Close())
				inputPaths = append(inputPaths, path)
			}

			merger, err := logline.NewMerger("v3", nil)
			require.NoError(b, err)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				readers := make([]io.ReaderAt, len(inputPaths))
				sizes := make([]int64, len(inputPaths))
				files := make([]*os.File, len(inputPaths))
				for j, p := range inputPaths {
					f, err := os.Open(p)
					require.NoError(b, err)
					fi, err := f.Stat()
					require.NoError(b, err)
					readers[j] = f
					sizes[j] = fi.Size()
					files[j] = f
				}
				// Discard the merged bytes — we only care about merge CPU cost.
				_, err := merger.Merge(context.Background(), readers, sizes, io.Discard)
				require.NoError(b, err)
				for _, f := range files {
					f.Close()
				}
			}
		})
	}
}
