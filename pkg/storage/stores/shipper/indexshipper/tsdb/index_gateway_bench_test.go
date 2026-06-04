package tsdb

// Benchmarks for the underlying index operations performed by each Index Gateway gRPC method.
// Requires real TSDB index files (gzip-compressed) in the directory named by indexGatewayBenchDir.
//
// Mapping to gRPC methods:
//
//	BenchmarkGetChunkRefs  → GetChunkRef
//	BenchmarkSeries        → GetSeries
//	BenchmarkLabelNames    → LabelNamesForMetricName
//	BenchmarkLabelValues   → LabelValuesForMetricName
//	BenchmarkStats         → GetStats
//	BenchmarkVolume        → GetVolume
//	BenchmarkForSeries     → GetShards (which calls ForSeries to enumerate all series + chunks)
//
// Each benchmark opens a sub-benchmark per index file. The query uses a MatchEqual matcher
// constructed from the first lexicographically-sorted label name and value found in that file.
// Decompression and label introspection happen once at startup and are excluded from timing.

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	pgzip "github.com/klauspost/pgzip"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const indexGatewayBenchDir = "/Users/danhopper/Documents/example-index-files"

type igwBenchIndex struct {
	name       string // base filename, used as sub-benchmark label
	idx        *TSDBIndex
	labelName  string // first lexicographic label name present in the index
	labelValue string // first lexicographic value of labelName
}

// igwBenchCacheDir is a stable directory that persists decompressed index files across
// benchmark runs. Delete it manually to force re-decompression.
var igwBenchCacheDir = filepath.Join(indexGatewayBenchDir, "tmp", "tsdb-igw-bench")

var igwBenchIndexes []igwBenchIndex

func TestMain(m *testing.M) {
	igwBenchSetup()
	code := m.Run()
	igwBenchTeardown()
	os.Exit(code)
}

func igwBenchSetup() {
	if _, err := os.Stat(indexGatewayBenchDir); err != nil {
		return
	}

	if err := os.MkdirAll(igwBenchCacheDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "igw bench: create cache dir: %v\n", err)
		return
	}

	entries, err := os.ReadDir(indexGatewayBenchDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "igw bench: read dir: %v\n", err)
		return
	}

	// Pipeline: decompress then open+introspect, all in parallel.
	// Each file gets its own goroutine so I/O and CPU can overlap across files.
	// Opening a file (NewTSDBIndexFromFile) scans the entire symbol table,
	// which is I/O-bound on cold page cache — parallelising it cuts wall time.
	type result struct {
		name       string
		idx        *TSDBIndex
		labelName  string
		labelValue string
		err        error
	}
	ch := make(chan result, len(entries))
	var wg sync.WaitGroup

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		entry := entry
		wg.Add(1)
		go func() {
			defer wg.Done()

			src := filepath.Join(indexGatewayBenchDir, entry.Name())
			dst := filepath.Join(igwBenchCacheDir, strings.TrimSuffix(entry.Name(), ".gz"))

			if err := igwDecompressIfStale(src, dst); err != nil {
				ch <- result{name: entry.Name(), err: fmt.Errorf("decompress: %w", err)}
				return
			}

			idx, _, err := NewTSDBIndexFromFile(dst)
			if err != nil {
				ch <- result{name: entry.Name(), err: fmt.Errorf("open: %w", err)}
				return
			}

			// Introspect: sort all label names, pick the first; sort its values, pick the first.
			// Sorting ensures deterministic selection regardless of index storage order.
			names, err := idx.LabelNames(context.Background(), "", 0, math.MaxInt64)
			if err != nil || len(names) == 0 {
				idx.Close()
				ch <- result{name: entry.Name(), err: fmt.Errorf("label names (empty=%v): %w", len(names) == 0, err)}
				return
			}
			sort.Strings(names)
			labelName := names[0]

			values, err := idx.LabelValues(context.Background(), "", 0, math.MaxInt64, labelName)
			if err != nil || len(values) == 0 {
				idx.Close()
				ch <- result{name: entry.Name(), err: fmt.Errorf("label values for %q (empty=%v): %w", labelName, len(values) == 0, err)}
				return
			}
			sort.Strings(values)

			ch <- result{
				name:       entry.Name(),
				idx:        idx,
				labelName:  labelName,
				labelValue: values[0],
			}
		}()
	}
	wg.Wait()
	close(ch)

	// Collect results, log errors, then sort so igwBenchIndexes has a deterministic order.
	var ready []result
	for r := range ch {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "igw bench: %s: %v\n", r.name, r.err)
			continue
		}
		ready = append(ready, r)
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].name < ready[j].name })

	for _, r := range ready {
		igwBenchIndexes = append(igwBenchIndexes, igwBenchIndex{
			name:       r.name,
			idx:        r.idx,
			labelName:  r.labelName,
			labelValue: r.labelValue,
		})
	}
}

func igwBenchTeardown() {
	for _, x := range igwBenchIndexes {
		x.idx.Close()
	}
	// Cache dir is intentionally left on disk for reuse across runs.
}

// igwDecompressIfStale decompresses src to dst only when dst is absent or older than src.
func igwDecompressIfStale(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.ModTime().After(srcInfo.ModTime()) {
		fmt.Fprintf(os.Stderr, "igw bench: using cached %s\n", filepath.Base(dst))
		return nil
	}
	fmt.Fprintf(os.Stderr, "igw bench: decompressing %s...\n", filepath.Base(src))
	return igwDecompress(src, dst)
}

// igwDecompress decompresses a gzip file to dst using pgzip for parallel CPU utilisation.
// On error the partial destination file is removed so a stale cache entry is never left behind.
func igwDecompress(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	gz, err := pgzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if retErr != nil {
			os.Remove(dst)
		}
	}()

	_, retErr = io.Copy(out, gz)
	return retErr
}

// igwRequireIndexes skips the benchmark if no index files were loaded at startup.
func igwRequireIndexes(b *testing.B) []igwBenchIndex {
	b.Helper()
	if len(igwBenchIndexes) == 0 {
		b.Skipf("no index files found in %s", indexGatewayBenchDir)
	}
	return igwBenchIndexes
}

/*
GOOS=linux GOARCH=arm64 go test -c \
  -o /tmp/tsdb-bench-linux \
  ./pkg/storage/stores/shipper/indexshipper/tsdb/ && \
docker run \
  --memory=2000m --memory-swap=2000m \
  --mount type=bind,src=/Users/danhopper/Documents/example-index-files,dst=/Users/danhopper/Documents/example-index-files \
  --mount type=bind,src=/tmp/tsdb-bench-linux,dst=/tsdb-bench,readonly \
  --name bench-test \
  debian:12-slim \
  /tsdb-bench -test.bench=BenchmarkGetChunkRefs -test.benchtime=10s -test.v; \
docker inspect bench-test --format '{{.State.OOMKilled}} exit={{.State.ExitCode}}'; \
docker rm bench-test
*/
// BenchmarkGetChunkRefs benchmarks TSDBIndex.GetChunkRefs, which backs the gateway's GetChunkRef RPC.
func BenchmarkGetChunkRefs(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := x.idx.GetChunkRefs(ctx, "", 0, math.MaxInt64, nil, nil, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSeries benchmarks TSDBIndex.Series, which backs the gateway's GetSeries RPC.
func BenchmarkSeries(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := x.idx.Series(ctx, "", 0, math.MaxInt64, nil, nil, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLabelNames benchmarks TSDBIndex.LabelNames, which backs the gateway's LabelNamesForMetricName RPC.
func BenchmarkLabelNames(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := x.idx.LabelNames(ctx, "", 0, math.MaxInt64, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLabelValues benchmarks TSDBIndex.LabelValues, which backs the gateway's LabelValuesForMetricName RPC.
func BenchmarkLabelValues(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := x.idx.LabelValues(ctx, "", 0, math.MaxInt64, x.labelName, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStats benchmarks TSDBIndex.Stats, which backs the gateway's GetStats RPC.
func BenchmarkStats(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			acc := new(stats.Stats)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Reset without allocating: zero the struct in place.
				*acc = stats.Stats{}
				if err := x.idx.Stats(ctx, "", 0, math.MaxInt64, acc, nil, nil, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkVolume benchmarks TSDBIndex.Volume, which backs the gateway's GetVolume RPC.
func BenchmarkVolume(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				acc := seriesvolume.NewAccumulator(1000, 1000)
				if err := x.idx.Volume(ctx, "", 0, math.MaxInt64, acc, nil, nil, nil, seriesvolume.DefaultAggregateBy, matcher); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkForSeries benchmarks TSDBIndex.ForSeries, which is the core of the gateway's GetShards RPC.
// GetShards calls ForSeries to enumerate all matching series and their chunk metadata before computing shard boundaries.
func BenchmarkForSeries(b *testing.B) {
	indexes := igwRequireIndexes(b)
	ctx := context.Background()

	for _, x := range indexes {
		x := x
		matcher := labels.MustNewMatcher(labels.MatchEqual, x.labelName, x.labelValue)
		b.Run(x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := x.idx.ForSeries(ctx, "", nil, 0, math.MaxInt64,
					func(_ labels.Labels, _ model.Fingerprint, _ []index.ChunkMeta) (stop bool) {
						return false
					}, matcher)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
