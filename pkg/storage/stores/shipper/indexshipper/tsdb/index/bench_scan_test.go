// This is an in-package Go benchmark for
// github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index
//
// It measures the cost of scanning a TSDB index file's series (the query hot
// path in the index-gateway) comparing:
//   - inmem: an index reader backed by an in-memory byte slice (approximates the
//     previous mmap-backed reader for random access speed)
//   - pool:  the pool/pread-backed reader that replaces mmap (poolByteSlice)
//   - pool_parallel: concurrent series lookups via the pool-backed reader
//
// Copy this file into the index package as bench_scan_test.go to run:
//
//	go test -run '^$' -bench BenchmarkScanIndex -benchmem \
//	  ./pkg/storage/stores/shipper/indexshipper/tsdb/index/...
package index

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

func buildBenchIndex(tb testing.TB, dir string, n int) (path string, size int64) {
	tb.Helper()
	fn := filepath.Join(dir, IndexFilename)
	iw, err := NewWriter(context.Background(), FormatV4, fn)
	require.NoError(tb, err)

	series := make([]labels.Labels, 0, n)
	symbols := map[string]struct{}{}
	for i := 0; i < n; i++ {
		lb := labels.FromStrings(
			"foo", "bar",
			"instance", fmt.Sprintf("inst-%d", i%1000),
			"job", fmt.Sprintf("job-%d", i%50),
			"pod", fmt.Sprintf("pod-%d", i),
		)
		series = append(series, lb)
		lb.Range(func(l labels.Label) {
			symbols[l.Name] = struct{}{}
			symbols[l.Value] = struct{}{}
		})
	}

	syms := make([]string, 0, len(symbols))
	for k := range symbols {
		syms = append(syms, k)
	}
	sort.Strings(syms)
	for _, s := range syms {
		require.NoError(tb, iw.AddSymbol(s))
	}

	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})
	for i, lb := range series {
		require.NoError(tb, iw.AddSeries(storage.SeriesRef(i), lb,
			model.Fingerprint(labels.StableHash(lb)),
			ChunkMeta{MinTime: 0, MaxTime: 1000, Checksum: uint32(i), KB: 16, Entries: 100},
		))
	}
	_, err = iw.Close(false)
	require.NoError(tb, err)

	fi, err := os.Stat(fn)
	require.NoError(tb, err)
	return fn, fi.Size()
}

func allRefs(tb testing.TB, r *Reader) []storage.SeriesRef {
	tb.Helper()
	n, v := AllPostingsKey()
	p, err := r.Postings(n, nil, v)
	require.NoError(tb, err)
	var refs []storage.SeriesRef
	for p.Next() {
		refs = append(refs, p.At())
	}
	require.NoError(tb, p.Err())
	return refs
}

func scanAll(tb testing.TB, r *Reader) int {
	n, v := AllPostingsKey()
	p, err := r.Postings(n, nil, v)
	require.NoError(tb, err)
	var (
		lbls  labels.Labels
		metas []ChunkMeta
		count int
	)
	for p.Next() {
		_, err := r.Series(p.At(), 0, math.MaxInt64, &lbls, &metas)
		require.NoError(tb, err)
		count++
	}
	require.NoError(tb, p.Err())
	return count
}

func BenchmarkScanIndex(b *testing.B) {
	const n = 200000
	dir := b.TempDir()
	path, size := buildBenchIndex(b, dir, n)
	b.Logf("index file size: %.1f MiB (%d series)", float64(size)/(1<<20), n)

	raw, err := os.ReadFile(path)
	require.NoError(b, err)

	b.Run("inmem", func(b *testing.B) {
		r, err := NewReader(RealByteSlice(raw))
		require.NoError(b, err)
		defer r.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = scanAll(b, r)
		}
	})

	b.Run("pool", func(b *testing.B) {
		r, err := NewFileReader(path)
		require.NoError(b, err)
		defer r.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = scanAll(b, r)
		}
	})

	b.Run("pool_parallel", func(b *testing.B) {
		r, err := NewFileReader(path)
		require.NoError(b, err)
		defer r.Close()
		refs := allRefs(b, r)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			var (
				lbls  labels.Labels
				metas []ChunkMeta
				i     int
			)
			for pb.Next() {
				_, err := r.Series(refs[i%len(refs)], 0, math.MaxInt64, &lbls, &metas)
				require.NoError(b, err)
				i++
			}
		})
	})
}
