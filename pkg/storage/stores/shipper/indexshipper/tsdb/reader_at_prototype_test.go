package tsdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// countingReaderAt wraps an io.ReaderAt and tallies read calls + bytes, so we can
// measure the read amplification of driving the index reader off mmap.
type countingReaderAt struct {
	r     io.ReaderAt
	calls atomic.Int64
	bytes atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls.Add(1)
	c.bytes.Add(int64(len(p)))
	return c.r.ReadAt(p, off)
}
func (c *countingReaderAt) reset()       { c.calls.Store(0); c.bytes.Store(0) }
func (c *countingReaderAt) callN() int64 { return c.calls.Load() }
func (c *countingReaderAt) byteN() int64 { return c.bytes.Load() }

// TestReaderAtPrototype swaps the index reader's mmap backing for an io.ReaderAt
// and (a) verifies it returns identical results, (b) measures how many bytes the
// pread path actually reads vs the file size.
func TestReaderAtPrototype(t *testing.T) {
	const nSeries, chunksPer = 5000, 6

	b := NewBuilder(index.FormatV3)
	for _, s := range genSeries(nSeries, chunksPer) {
		b.AddSeries(s.Labels, model.Fingerprint(labels.StableHash(s.Labels)), s.Chunks)
	}
	_, data, err := b.BuildInMemory(context.Background(), func(from, through model.Time, checksum uint32) Identifier {
		return SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: from, Through: through, Checksum: checksum}
	})
	require.NoError(t, err)
	fileSize := int64(len(data))

	// Baseline: whole-buffer backing (Range is a free subslice — same access cost
	// profile as mmap, which only pages in what's touched).
	base, err := index.NewReader(index.RealByteSlice(data))
	require.NoError(t, err)

	// Prototype: io.ReaderAt backing, instrumented.
	cr := &countingReaderAt{r: bytes.NewReader(data)}
	openCalls0, openBytes0 := cr.callN(), cr.byteN()
	bs := index.NewReaderAtByteSlice(cr, int64(len(data)))
	rat, err := index.NewReader(bs)
	require.NoError(t, err)
	openCalls := cr.callN() - openCalls0
	openBytes := cr.byteN() - openBytes0

	// RawFileReader (used by the upload path) must stream the exact file bytes.
	// On the pread backing it returns a SectionReader rather than buffering the
	// whole index, so verify it is byte-for-byte identical to the source.
	raw, err := rat.RawFileReader()
	require.NoError(t, err)
	gotRaw, err := io.ReadAll(raw)
	require.NoError(t, err)
	require.Equal(t, data, gotRaw, "RawFileReader stream must equal the original index bytes")

	// Collect a sample of real series IDs from the baseline reader.
	p, err := base.Postings("stream", nil, "stdout")
	require.NoError(t, err)
	var ids []storage.SeriesRef
	for p.Next() && len(ids) < 200 {
		ids = append(ids, p.At())
	}
	require.NoError(t, p.Err())
	require.NotEmpty(t, ids)

	// Read each sampled series via the ReaderAt path; verify equality vs baseline.
	cr.reset()
	for _, id := range ids {
		var lblsB, lblsR labels.Labels
		var chksB, chksR []index.ChunkMeta
		_, err := base.Series(id, 0, math.MaxInt64, &lblsB, &chksB)
		require.NoError(t, err)
		_, err = rat.Series(id, 0, math.MaxInt64, &lblsR, &chksR)
		require.NoError(t, err)
		require.Equal(t, lblsB.String(), lblsR.String(), "labels mismatch for id %d", id)
		require.Equal(t, len(chksB), len(chksR), "chunk count mismatch for id %d", id)
	}
	seriesCalls, seriesBytes := cr.callN(), cr.byteN()

	// Same scan, but with a per-query symbol cache shared across all series.
	cache := index.NewSymbolCache()
	cr.reset()
	for _, id := range ids {
		var lbls labels.Labels
		var chks []index.ChunkMeta
		_, err := rat.Series(id, 0, math.MaxInt64, &lbls, &chks, cache)
		require.NoError(t, err)
	}
	cachedCalls, cachedBytes := cr.callN(), cr.byteN()

	// One postings query via the ReaderAt path.
	cr.reset()
	pq, err := rat.Postings("service_name", nil, "svc-5")
	require.NoError(t, err)
	cnt := 0
	for pq.Next() {
		cnt++
	}
	require.NoError(t, pq.Err())
	postCalls, postBytes := cr.callN(), cr.byteN()

	require.NoError(t, bs.Err()) // no read errors occurred during open + scan

	fmt.Printf("\n=== io.ReaderAt prototype: %d series, %d chunks/series, file=%.2f MB ===\n",
		nSeries, chunksPer, float64(fileSize)/(1<<20))
	fmt.Printf("OPEN (NewReader):        %6d reads, %10d bytes  (%.1fx file)\n",
		openCalls, openBytes, float64(openBytes)/float64(fileSize))
	fmt.Printf("READ %d series:          %6d reads, %10d bytes  (%.1fx file, %d B/series, %.1f reads/series)\n",
		len(ids), seriesCalls, seriesBytes, float64(seriesBytes)/float64(fileSize),
		seriesBytes/int64(len(ids)), float64(seriesCalls)/float64(len(ids)))
	fmt.Printf("  + per-query cache:     %6d reads, %10d bytes  (%.1fx file, %d B/series, %.1f reads/series)\n",
		cachedCalls, cachedBytes, float64(cachedBytes)/float64(fileSize),
		cachedBytes/int64(len(ids)), float64(cachedCalls)/float64(len(ids)))
	fmt.Printf("POSTINGS query (%d hits): %5d reads, %10d bytes  (%.1fx file)\n",
		cnt, postCalls, postBytes, float64(postBytes)/float64(fileSize))
}
