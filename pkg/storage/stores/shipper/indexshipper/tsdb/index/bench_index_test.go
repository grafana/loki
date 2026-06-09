package index

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// Benchmark parameters
const (
	benchNumStreams = 1_000_000
	benchNumChunks  = 100
	benchIndexFile  = "bench_1m_streams.index"
	benchNumCluster = 5
	benchNumNS      = 20
	benchNumPods    = 100
	benchNumConts   = 100 // clusters * namespaces * pods * containers = 5*20*100*100 = 1_000_000
)

// benchIndexPath returns the path of the persistent fixture index, stored
// under the OS user-cache directory so it survives between benchmark runs.
// The directory is created on first use; the file itself is created by
// buildBenchIndex when absent.
func benchIndexPath(t testing.TB) string {
	t.Helper()
	cacheDir, err := os.UserCacheDir()
	require.NoError(t, err)
	dir := filepath.Join(cacheDir, "loki-bench-tsdb-index")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return filepath.Join(dir, benchIndexFile)
}

// benchLabelSets generates a deterministic slice of 1 M Loki-like label
// sets covering a realistic cardinality distribution:
//
//	cluster (5) × namespace (20) × pod (100) × container (100) = 1 000 000
//
// The slice is sorted by labels.StableHash so it can be fed directly to the
// index writer, which requires series in that order.
func benchLabelSets() []labels.Labels {
	lbls := make([]labels.Labels, 0, benchNumStreams)
	for c := 0; c < benchNumCluster; c++ {
		for n := 0; n < benchNumNS; n++ {
			for p := 0; p < benchNumPods; p++ {
				for ct := 0; ct < benchNumConts; ct++ {
					lbls = append(lbls, labels.FromStrings(
						"cluster", fmt.Sprintf("cluster-%02d", c),
						"container", fmt.Sprintf("container-%02d", ct),
						"namespace", fmt.Sprintf("namespace-%02d", n),
						"pod", fmt.Sprintf("pod-%03d", p),
					))
				}
			}
		}
	}
	sort.Slice(lbls, func(i, j int) bool {
		return labels.StableHash(lbls[i]) < labels.StableHash(lbls[j])
	})
	return lbls
}

// buildBenchIndex creates the index at path.  Each series has benchNumChunks
// non-overlapping chunks evenly distributed across a 24 h window.
func buildBenchIndex(t testing.TB, path string) {
	t.Helper()
	t.Log("building benchmark index, this may take a while …")

	const (
		windowMs  = int64(24 * 60 * 60 * 1000) // 24 h in milliseconds
		chunkSize = windowMs / benchNumChunks
	)

	rng := rand.New(rand.NewSource(42))
	lbls := benchLabelSets()

	// Collect all unique symbols first — the writer requires them sorted.
	symSet := make(map[string]struct{})
	for _, lset := range lbls {
		lset.Range(func(l labels.Label) {
			symSet[l.Name] = struct{}{}
			symSet[l.Value] = struct{}{}
		})
	}
	syms := make([]string, 0, len(symSet))
	for s := range symSet {
		syms = append(syms, s)
	}
	sort.Strings(syms)

	iw, err := NewWriter(context.Background(), FormatV3, path)
	require.NoError(t, err)

	for _, s := range syms {
		require.NoError(t, iw.AddSymbol(s))
	}

	chunks := make([]ChunkMeta, benchNumChunks)
	for i, lset := range lbls {
		for j := range chunks {
			mint := int64(j) * chunkSize
			chunks[j] = ChunkMeta{
				MinTime:  mint,
				MaxTime:  mint + chunkSize - 1,
				Checksum: rng.Uint32(),
				KB:       uint32(rng.Intn(512) + 1),
				Entries:  uint32(rng.Intn(10000) + 100),
			}
		}
		fp := model.Fingerprint(labels.StableHash(lset))
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i), lset, fp, chunks...))
	}

	_, err = iw.Close(false)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	t.Logf("benchmark index written: %s (%.1f MB)", path, float64(info.Size())/(1<<20))
}

// openBenchIndex returns a ready-to-use *Reader backed by the persistent
// fixture.  It builds the fixture on first call.
func openBenchIndex(t testing.TB) *Reader {
	t.Helper()
	path := benchIndexPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		buildBenchIndex(t, path)
	}
	r, err := NewFileReader(path)
	require.NoError(t, err)
	return r
}

// timeRangeBounds returns [from, through) in milliseconds for a query window
// centred on the 24 h period (00:00 → 24:00).
//
//	3 h  → [10.5 h, 13.5 h]
//	6 h  → [9 h, 15 h]
//	12 h → [6 h, 18 h]
//	24 h → [0, 86400000]
func timeRangeBounds(hours int) (int64, int64) {
	const totalMs = int64(24 * 60 * 60 * 1000)
	rangeMs := int64(hours) * 60 * 60 * 1000
	mid := totalMs / 2
	from := mid - rangeMs/2
	through := mid + rangeMs/2
	if from < 0 {
		from = 0
	}
	if through > totalMs {
		through = totalMs
	}
	return from, through
}

// BenchmarkLabelNames measures how long it takes to list all label names from
// the index (no time-range filtering — LabelNames operates on the posting
// table, not on chunk times).
func BenchmarkLabelNames(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		names, err := r.LabelNames()
		require.NoError(b, err)
		_ = names
	}
}

// BenchmarkLabelValues measures how long it takes to list all values for a
// given high-cardinality label key ("pod", 100 unique values).
func BenchmarkLabelValues(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	for _, key := range []string{"cluster", "namespace", "pod", "container"} {
		key := key
		b.Run(key, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				vals, err := r.LabelValues(key)
				require.NoError(b, err)
				_ = vals
			}
		})
	}
}

// BenchmarkLabelValuesWithTimeRange iterates the postings for a label key and
// then fetches each matching series to filter by chunk time range, mirroring
// what a label-values-for-matchers query would do.
func BenchmarkLabelValuesWithTimeRange(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	for _, hours := range []int{3, 6, 12, 24} {
		hours := hours
		from, through := timeRangeBounds(hours)
		b.Run(fmt.Sprintf("%dh", hours), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				p, err := r.Postings(allPostingsKey.Name, nil, allPostingsKey.Value)
				require.NoError(b, err)

				seen := make(map[string]struct{})
				var lset labels.Labels
				var chks []ChunkMeta
				for p.Next() {
					_, err := r.Series(p.At(), from, through, &lset, &chks)
					require.NoError(b, err)
					if len(chks) == 0 {
						continue
					}
					lset.Range(func(l labels.Label) {
						seen[l.Name] = struct{}{}
					})
					chks = chks[:0]
				}
				require.NoError(b, p.Err())
				_ = seen
			}
		})
	}
}

// BenchmarkSeriesAllPostings iterates all series in the index for different
// time-range windows, measuring how many series entries are decoded per second.
func BenchmarkSeriesAllPostings(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	for _, hours := range []int{3, 6, 12, 24} {
		hours := hours
		from, through := timeRangeBounds(hours)
		b.Run(fmt.Sprintf("%dh", hours), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				p, err := r.Postings(allPostingsKey.Name, nil, allPostingsKey.Value)
				require.NoError(b, err)

				var lset labels.Labels
				var chks []ChunkMeta
				n := 0
				for p.Next() {
					_, err := r.Series(p.At(), from, through, &lset, &chks)
					require.NoError(b, err)
					n++
					chks = chks[:0]
				}
				require.NoError(b, p.Err())
				b.ReportMetric(float64(n), "series/op")
			}
		})
	}
}

// BenchmarkSeriesByLabelMatcher benchmarks resolving a single label-value
// matcher (e.g. {namespace="namespace-03"}) to postings, then reading all
// matching series for different time-range windows.  This is the canonical
// hot path for most Loki label-selector queries.
func BenchmarkSeriesByLabelMatcher(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	// Pick a matcher that matches 1/benchNumNS of all streams (50 000 series).
	const matcherValue = "namespace-03"

	for _, hours := range []int{3, 6, 12, 24} {
		hours := hours
		from, through := timeRangeBounds(hours)
		b.Run(fmt.Sprintf("%dh", hours), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				p, err := r.Postings("namespace", nil, matcherValue)
				require.NoError(b, err)

				var lset labels.Labels
				var chks []ChunkMeta
				n := 0
				for p.Next() {
					_, err := r.Series(p.At(), from, through, &lset, &chks)
					require.NoError(b, err)
					n++
					chks = chks[:0]
				}
				require.NoError(b, p.Err())
				b.ReportMetric(float64(n), "series/op")
			}
		})
	}
}

// BenchmarkSeriesByMultipleMatchers benchmarks narrowing postings via two
// label keys (e.g. {namespace="namespace-03", cluster="cluster-02"}) and then
// reading all matching series.  This exercises posting list intersection via
// Merge + filtering.
func BenchmarkSeriesByMultipleMatchers(b *testing.B) {
	r := openBenchIndex(b)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	// namespace-03 × cluster-02 → 100 pods × 100 containers = 10 000 series.
	const (
		nsValue      = "namespace-03"
		clusterValue = "cluster-02"
	)

	for _, hours := range []int{3, 6, 12, 24} {
		hours := hours
		from, through := timeRangeBounds(hours)
		b.Run(fmt.Sprintf("%dh", hours), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				pNS, err := r.Postings("namespace", nil, nsValue)
				require.NoError(b, err)
				pCL, err := r.Postings("cluster", nil, clusterValue)
				require.NoError(b, err)
				p := Intersect(pNS, pCL)

				var lset labels.Labels
				var chks []ChunkMeta
				n := 0
				for p.Next() {
					_, err := r.Series(p.At(), from, through, &lset, &chks)
					require.NoError(b, err)
					n++
					chks = chks[:0]
				}
				require.NoError(b, p.Err())
				b.ReportMetric(float64(n), "series/op")
			}
		})
	}
}
