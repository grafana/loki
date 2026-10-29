package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline"
)

// radixDatasets are the generated captures the sorter benchmarks run on; see
// TestGenerateSyntheticRandom and TestGenerateLoghubCapture.
var radixDatasets = []string{
	"testdata/kafka_records_random20.bin",
	"testdata/kafka_records_loghub20.bin",
}

const maxDatasetPairs = 20_000_000

type datasetPairs struct {
	keys [][8]byte
	docs []uint32
}

var (
	datasetPairsMu    sync.Mutex
	datasetPairsCache = map[string]datasetPairs{}
)

// loadDatasetPairs returns the (ngram, docID) pairs the builder appends for
// the capture at path, in append order and capped at maxDatasetPairs. The
// pairs come from the real ingest path (decode → processStream) with a buffer
// large enough that it never fills, so they are exactly what the first fill
// would sort.
func loadDatasetPairs(tb testing.TB, path string) datasetPairs {
	tb.Helper()
	datasetPairsMu.Lock()
	defer datasetPairsMu.Unlock()
	if p, ok := datasetPairsCache[path]; ok {
		return p
	}
	records, err := loadCapturedRecords(path)
	if err != nil {
		tb.Skipf("no dataset at %s (generate it first): %v", path, err)
	}
	decoder, err := kafka.NewDecoder()
	require.NoError(tb, err)
	cfg := Config{
		Index: logline.IndexConfig{
			Version:          "v3",
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
		},
		PostingsBufferPairs:    maxDatasetPairs + 1<<20,
		PostingsSpillWatermark: DefaultPostingsSpillWatermark,
		ScratchDir:             tb.TempDir(),
	}
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(tb, err)
	pb := b.ing.postings
	for _, rec := range records {
		if len(pb.keys) >= maxDatasetPairs {
			break
		}
		stream, lbls, err := decoder.Decode(rec.value)
		require.NoError(tb, err)
		require.NoError(tb, b.processStream(&stream, &lbls, rec.timestamp, recordRef{}))
	}
	require.Zero(tb, pb.runSeq, "the extraction buffer must not fill")
	n := min(len(pb.keys), maxDatasetPairs)
	p := datasetPairs{keys: slices.Clone(pb.keys[:n]), docs: slices.Clone(pb.docs[:n])}
	datasetPairsCache[path] = p
	return p
}

func datasetTag(path string) string {
	return strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), ".bin"), "kafka_records_")
}

// TestNgramSortVariantsRealData checks every sorter against the baseline on
// the real pairs of each generated dataset (first 2M pairs and all pairs),
// comparing the sorted+deduped output exactly. Skipped when the datasets have
// not been generated.
func TestNgramSortVariantsRealData(t *testing.T) {
	if testing.Short() {
		t.Skip("large real-data comparison")
	}
	for _, path := range radixDatasets {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("no dataset at %s", path)
		}
		p := loadDatasetPairs(t, path)
		for _, n := range []int{2_000_000, len(p.keys)} {
			n = min(n, len(p.keys))
			wk, wd := referenceSort(p.keys[:n], p.docs[:n])
			wm := sortAndDedupeGroups(wk, wd)
			for _, v := range ngramSortVariants[1:] {
				t.Run(fmt.Sprintf("%s/n=%d/%s", datasetTag(path), n, v.name), func(t *testing.T) {
					keys, docs := slices.Clone(p.keys[:n]), slices.Clone(p.docs[:n])
					sk, sd := v.newSorter().sort(keys, docs, make([][8]byte, n), make([]uint32, n))
					for i := 1; i < n; i++ {
						if keyU64(sk[i-1]) > keyU64(sk[i]) {
							t.Fatalf("keys out of order at %d", i)
						}
					}
					m := sortAndDedupeGroups(sk, sd)
					require.Equal(t, wm, m, "deduped length differs")
					require.True(t, slices.Equal(wk[:wm], sk[:m]), "deduped keys differ")
					require.True(t, slices.Equal(wd[:wm], sd[:m]), "deduped docs differ")
				})
			}
		}
	}
}

// BenchmarkNgramSort times each sorter alone on real pairs of each dataset:
// the first 2M pairs (one fill of a 2M-pair buffer) and all pairs (capped at
// 20M, the single sort a default-sized buffer performs at flush). Each
// iteration restores the unsorted input outside the timer.
//
//	go test ./pkg/logline/builder/ -run '^$' -bench BenchmarkNgramSort -count 8 -timeout 2h
//
// BENCH_RADIX (comma list) restricts the variants.
func BenchmarkNgramSort(b *testing.B) {
	variants := ngramSortVariants
	if names := os.Getenv("BENCH_RADIX"); names != "" {
		variants = nil
		for _, name := range strings.Split(names, ",") {
			v, ok := lookupNgramSortVariant(strings.TrimSpace(name))
			if !ok {
				b.Fatalf("unknown variant %q", name)
			}
			variants = append(variants, v)
		}
	}
	for _, path := range radixDatasets {
		p := loadDatasetPairs(b, path)
		for _, n := range []int{2_000_000, len(p.keys)} {
			n = min(n, len(p.keys))
			size := "2M"
			if n != 2_000_000 {
				size = "all"
			}
			for _, v := range variants {
				b.Run(fmt.Sprintf("data=%s/n=%s/sorter=%s", datasetTag(path), size, v.name), func(b *testing.B) {
					keys := make([][8]byte, n)
					docs := make([]uint32, n)
					kb := make([][8]byte, n)
					db := make([]uint32, n)
					if v.hugePages {
						adviseHugePages(keys, docs, kb, db)
					}
					s := v.newSorter()
					copy(keys, p.keys[:n])
					copy(docs, p.docs[:n])
					s.sort(keys, docs, kb, db) // fault in buffers and scratch
					b.ResetTimer()
					for range b.N {
						b.StopTimer()
						copy(keys, p.keys[:n])
						copy(docs, p.docs[:n])
						b.StartTimer()
						s.sort(keys, docs, kb, db)
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/pair")
				})
			}
		}
	}
}
