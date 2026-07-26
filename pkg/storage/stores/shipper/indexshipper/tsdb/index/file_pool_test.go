package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestFilePool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	t.Run("reuses idle handles", func(t *testing.T) {
		p := newFilePool(path, 2)
		defer func() { _ = p.stop() }()

		f1, err := p.get()
		require.NoError(t, err)
		require.NoError(t, p.put(f1))

		// The next get should return the same, still-open handle.
		f2, err := p.get()
		require.NoError(t, err)
		require.Same(t, f1, f2)
		require.NoError(t, p.put(f2))
	})

	t.Run("closes handles over capacity", func(t *testing.T) {
		p := newFilePool(path, 1)
		defer func() { _ = p.stop() }()

		f1, err := p.get()
		require.NoError(t, err)
		f2, err := p.get()
		require.NoError(t, err)
		require.NotSame(t, f1, f2)

		require.NoError(t, p.put(f1)) // retained
		require.NoError(t, p.put(f2)) // over capacity -> closed
	})

	t.Run("stop closes idle handles and rejects gets", func(t *testing.T) {
		p := newFilePool(path, 2)
		f, err := p.get()
		require.NoError(t, err)
		require.NoError(t, p.put(f))

		require.NoError(t, p.stop())
		require.NoError(t, p.stop()) // idempotent

		_, err = p.get()
		require.ErrorIs(t, err, errPoolStopped)
	})
}

func TestPoolByteSliceErr(t *testing.T) {
	b := &poolByteSlice{}
	require.NoError(t, b.Err())

	first := errors.New("first")
	b.setErr(first)
	require.ErrorIs(t, b.Err(), first)

	// The first error wins; subsequent errors and nil are ignored.
	b.setErr(errors.New("second"))
	require.ErrorIs(t, b.Err(), first)
	b.setErr(nil)
	require.ErrorIs(t, b.Err(), first)
}

func TestPoolByteSliceParity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	raw := make([]byte, 64<<10)
	_, _ = rand.New(rand.NewSource(1)).Read(raw)
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	b := newPoolByteSlice(path, len(raw), 4)
	defer b.Close()

	require.Equal(t, len(raw), b.Len())

	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 1000; i++ {
		start := rng.Intn(len(raw) - 1)
		end := start + 1 + rng.Intn(len(raw)-start-1)

		require.Equal(t, raw[start:end], b.Range(start, end))

		buf, release, err := b.readRange(start, end)
		require.NoError(t, err)
		require.True(t, bytes.Equal(raw[start:end], buf))
		release()
	}
	require.NoError(t, b.Err())
}

// buildParityIndex builds an on-disk index and returns its path plus the raw
// bytes for constructing an in-memory reference reader.
func buildParityIndex(t testing.TB, n int) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)
	iw, err := NewWriter(context.Background(), FormatV4, fn)
	require.NoError(t, err)

	series := make([]labels.Labels, 0, n)
	symbols := map[string]struct{}{}
	for i := 0; i < n; i++ {
		lb := labels.FromStrings(
			"foo", "bar",
			"instance", fmt.Sprintf("inst-%d", i%100),
			"job", fmt.Sprintf("job-%d", i%7),
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
		require.NoError(t, iw.AddSymbol(s))
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})
	for i, lb := range series {
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i), lb,
			model.Fingerprint(labels.StableHash(lb)),
			ChunkMeta{MinTime: 0, MaxTime: 1000, Checksum: uint32(i), KB: 16, Entries: 100},
		))
	}
	_, err = iw.Close(false)
	require.NoError(t, err)

	raw, err := os.ReadFile(fn)
	require.NoError(t, err)
	return fn, raw
}

// TestReaderPoolParity ensures the pool-backed (pread) reader returns exactly the
// same results as an in-memory (mmap-equivalent) reader across the read paths
// that were refactored to bounded reads.
func TestReaderPoolParity(t *testing.T) {
	const n = 3000
	path, raw := buildParityIndex(t, n)

	ref, err := NewReader(RealByteSlice(raw))
	require.NoError(t, err)
	defer ref.Close()

	pool, err := NewFileReader(path)
	require.NoError(t, err)
	defer pool.Close()

	// LabelNames / LabelValues parity.
	refNames, err := ref.LabelNames()
	require.NoError(t, err)
	poolNames, err := pool.LabelNames()
	require.NoError(t, err)
	require.Equal(t, refNames, poolNames)

	for _, name := range refNames {
		refVals, err := ref.LabelValues(name)
		require.NoError(t, err)
		poolVals, err := pool.LabelValues(name)
		require.NoError(t, err)
		require.Equal(t, refVals, poolVals, "label values mismatch for %q", name)
	}

	// Series parity across all series (exercises symbol lookups on both readers).
	pn, pv := AllPostingsKey()
	refPostings, err := ref.Postings(pn, nil, pv)
	require.NoError(t, err)

	var refIDs []storage.SeriesRef
	for refPostings.Next() {
		refIDs = append(refIDs, refPostings.At())
	}
	require.NoError(t, refPostings.Err())
	require.Len(t, refIDs, n)

	for _, id := range refIDs {
		var (
			lblsA, lblsB labels.Labels
			chksA, chksB []ChunkMeta
		)
		fpA, err := ref.Series(id, 0, math.MaxInt64, &lblsA, &chksA)
		require.NoError(t, err)
		fpB, err := pool.Series(id, 0, math.MaxInt64, &lblsB, &chksB)
		require.NoError(t, err)

		require.Equal(t, fpA, fpB)
		require.Equal(t, lblsA, lblsB)
		require.Equal(t, chksA, chksB)
	}

	// Postings parity for a specific multi-value lookup.
	refP, err := ref.Postings("job", nil, "job-1", "job-3", "job-5")
	require.NoError(t, err)
	poolP, err := pool.Postings("job", nil, "job-1", "job-3", "job-5")
	require.NoError(t, err)
	require.Equal(t, drainPostings(t, refP), drainPostings(t, poolP))
}

func drainPostings(t testing.TB, p Postings) []storage.SeriesRef {
	t.Helper()
	var out []storage.SeriesRef
	for p.Next() {
		out = append(out, p.At())
	}
	require.NoError(t, p.Err())
	return out
}

// TestReaderPoolConcurrentSeries exercises the pool-backed reader from many
// goroutines using valid series references and asserts results match a
// single-threaded in-memory reader.
func TestReaderPoolConcurrentSeries(t *testing.T) {
	const n = 4000
	path, raw := buildParityIndex(t, n)

	ref, err := NewReader(RealByteSlice(raw))
	require.NoError(t, err)
	defer ref.Close()

	pool, err := NewFileReader(path)
	require.NoError(t, err)
	defer pool.Close()

	pn, pv := AllPostingsKey()
	p, err := ref.Postings(pn, nil, pv)
	require.NoError(t, err)
	var ids []storage.SeriesRef
	for p.Next() {
		ids = append(ids, p.At())
	}
	require.NoError(t, p.Err())

	// Expected labels per ref from the reference reader.
	want := make([]labels.Labels, len(ids))
	for i, id := range ids {
		var lbls labels.Labels
		var chks []ChunkMeta
		_, err := ref.Series(id, 0, math.MaxInt64, &lbls, &chks)
		require.NoError(t, err)
		want[i] = lbls.Copy()
	}

	var (
		wg       sync.WaitGroup
		failures atomic.Int64
	)
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			var lbls labels.Labels
			var chks []ChunkMeta
			for i := 0; i < 2000; i++ {
				idx := rng.Intn(len(ids))
				_, err := pool.Series(ids[idx], 0, math.MaxInt64, &lbls, &chks)
				if err != nil || !labels.Equal(want[idx], lbls) {
					failures.Add(1)
				}
			}
		}(int64(g))
	}
	wg.Wait()

	require.Zero(t, failures.Load())
	require.NoError(t, byteSliceErr(pool.b))
}
