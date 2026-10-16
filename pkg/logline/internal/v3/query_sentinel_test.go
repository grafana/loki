package v3

import (
	"path/filepath"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestGetBitmapAllSentinels(t *testing.T) {
	// If every query term is a sentinel, GetBitmap returns nil for each.
	dir := t.TempDir()
	path := filepath.Join(dir, "q.lidx")

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0

	w, err := newStreamingIndexWriter(path, cfg, 3)
	require.NoError(t, err)
	w.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
		{ID: 2, MinTimeUnix: 2, MaxTimeUnix: 3},
	})
	// Write two sentinel terms
	require.NoError(t, w.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{MatchesAll: true}))
	require.NoError(t, w.WriteTermBitmap([8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, format.Bitmap{MatchesAll: true}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	for _, term := range []string{"aaaaaa", "bbbbbb"} {
		idx, err := r.FindTerm(term)
		require.NoError(t, err)
		require.GreaterOrEqual(t, idx, 0)

		res, err := r.GetBitmap(idx)
		require.NoError(t, err)
		require.True(t, res.MatchesAll, "sentinel term should have MatchesAll set")
		require.Nil(t, res.Roaring, "sentinel term should have nil bitmap")
	}

	// Documents() should return all 3
	require.Len(t, r.Documents(), 3)
}

func TestGetBitmapMixedSentinel(t *testing.T) {
	// Sentinel term is transparent — nil bitmap means "matches all".
	dir := t.TempDir()
	path := filepath.Join(dir, "q2.lidx")

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0

	w, err := newStreamingIndexWriter(path, cfg, 4)
	require.NoError(t, err)
	w.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
		{ID: 2, MinTimeUnix: 2, MaxTimeUnix: 3},
		{ID: 3, MinTimeUnix: 3, MaxTimeUnix: 4},
	})

	// "aaaaaa": sentinel (matches all)
	require.NoError(t, w.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{MatchesAll: true}))
	// "bbbbbb": real bitmap — docs 1 and 2
	bm := roaring.New()
	bm.AddMany([]uint32{1, 2})
	require.NoError(t, w.WriteTermBitmap([8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, format.Bitmap{Roaring: bm}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	// Sentinel term
	idx, err := r.FindTerm("aaaaaa")
	require.NoError(t, err)
	require.GreaterOrEqual(t, idx, 0)
	sentinelRes, err := r.GetBitmap(idx)
	require.NoError(t, err)
	require.True(t, sentinelRes.MatchesAll)
	require.Nil(t, sentinelRes.Roaring)

	// Real term
	idx, err = r.FindTerm("bbbbbb")
	require.NoError(t, err)
	require.GreaterOrEqual(t, idx, 0)
	realRes, err := r.GetBitmap(idx)
	require.NoError(t, err)
	require.False(t, realRes.MatchesAll)
	require.NotNil(t, realRes.Roaring)
	require.Equal(t, []uint32{1, 2}, realRes.Roaring.ToArray())
}
