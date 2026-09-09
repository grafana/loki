package v3

import (
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestDensityFilterSentinel(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/density.lidx"

	// With a 5s document interval there are 17280 docs/day.
	// Threshold 0.5 → sentinel cutoff = 8640 docs.
	docCount := uint32(10)
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0.5
	cfg.DocumentInterval = 5 * time.Second // 17280 docs/day, cutoff = 8640

	w, err := newStreamingIndexWriter(path, cfg, docCount)
	require.NoError(t, err)
	for i := range docCount {
		w.AddDocument(format.DocumentMetadata{ID: i, MinTimeUnix: int64(i), MaxTimeUnix: int64(i + 1)})
	}

	// "aaaaaa": 6 docs, well below the 8640 cutoff → should NOT be sentinel
	denseTerm := [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}
	denseBM := roaring.New()
	denseBM.AddMany([]uint32{0, 1, 2, 3, 4, 5})
	require.NoError(t, w.WriteTermBitmap(denseTerm, format.Bitmap{Roaring: denseBM}))

	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)

	require.True(t, it.Next())
	require.Equal(t, denseTerm, it.Term())
	require.False(t, it.Bitmap().MatchesAll, "6 docs should not exceed day-based cutoff of 8640")
	require.Equal(t, uint64(6), it.Bitmap().Roaring.GetCardinality())

	require.False(t, it.Next())
}

func TestDensityFilterSentinel_ExceedsDayCutoff(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/density_day.lidx"

	// 1ms document interval → 86_400_000 docs/day.
	// Threshold 0.20 → cutoff = 17_280_000.
	// Write an index with 20_000_000 docs where one term covers them all.
	// That exceeds the cutoff → sentinel.
	docCount := uint32(20_000_000)
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0.20
	cfg.DocumentInterval = time.Millisecond // 86_400_000 docs/day, cutoff = 17_280_000

	w, err := newStreamingIndexWriter(path, cfg, docCount)
	require.NoError(t, err)
	for i := range docCount {
		w.AddDocument(format.DocumentMetadata{ID: i, MinTimeUnix: int64(i), MaxTimeUnix: int64(i + 1)})
	}

	// Term covering all 20M docs exceeds the 17.28M cutoff → sentinel
	term := [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}
	bm := roaring.New()
	bm.AddRange(0, uint64(docCount))
	require.NoError(t, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))

	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.True(t, it.Bitmap().MatchesAll, "20M docs should exceed day-based cutoff of 17.28M")
	require.False(t, it.Next())
}

func TestDensityFilterDisabled(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nodensity.lidx"

	docCount := uint32(5)
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0 // disabled

	w, err := newStreamingIndexWriter(path, cfg, docCount)
	require.NoError(t, err)
	for i := range docCount {
		w.AddDocument(format.DocumentMetadata{ID: i, MinTimeUnix: int64(i), MaxTimeUnix: int64(i + 1)})
	}

	// Dense term, but threshold is disabled → should be kept
	term := [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}
	bm := roaring.New()
	bm.AddMany([]uint32{0, 1, 2, 3, 4}) // 100% of docs
	require.NoError(t, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.False(t, it.Bitmap().MatchesAll, "threshold=0 should not filter anything")
	require.Equal(t, uint64(5), it.Bitmap().Roaring.GetCardinality())
}

func TestDensityFilterDisabled_NoDocumentInterval(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nointerval.lidx"

	docCount := uint32(5)
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0.20
	cfg.DocumentInterval = 0 // zero interval disables density filter

	w, err := newStreamingIndexWriter(path, cfg, docCount)
	require.NoError(t, err)
	for i := range docCount {
		w.AddDocument(format.DocumentMetadata{ID: i, MinTimeUnix: int64(i), MaxTimeUnix: int64(i + 1)})
	}

	term := [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}
	bm := roaring.New()
	bm.AddMany([]uint32{0, 1, 2, 3, 4})
	require.NoError(t, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.False(t, it.Bitmap().MatchesAll, "zero document interval should disable density filter")
	require.Equal(t, uint64(5), it.Bitmap().Roaring.GetCardinality())
}

func TestExplicitSentinelWrite(t *testing.T) {
	// Test that WriteTermBitmap(term, nil) always writes a sentinel,
	// regardless of DensityThreshold setting.
	dir := t.TempDir()
	path := dir + "/sentinel.lidx"

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0

	w, err := newStreamingIndexWriter(path, cfg, 3)
	require.NoError(t, err)
	w.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
		{ID: 2, MinTimeUnix: 2, MaxTimeUnix: 3},
	})

	// Write explicit sentinel (nil bitmap)
	sentinelTerm := [8]byte{'s', 'e', 'n', 't', 'i', 'n', 0, 0}
	require.NoError(t, w.WriteTermBitmap(sentinelTerm, format.Bitmap{MatchesAll: true}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.True(t, it.Bitmap().MatchesAll, "explicitly written nil bitmap must read back as sentinel")
}
