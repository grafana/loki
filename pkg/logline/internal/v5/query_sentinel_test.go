package v5

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

	cfg := testWriteConfig()
	cfg.DensityThreshold = 0

	w, err := newStreamingIndexWriter(path, cfg)
	require.NoError(t, err)
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

	require.Nil(t, r.Documents(), "v5 indexes have no document table")
}

func TestGetBitmapMixedSentinel(t *testing.T) {
	// Sentinel term is transparent — nil bitmap means "matches all".
	dir := t.TempDir()
	path := filepath.Join(dir, "q2.lidx")

	cfg := testWriteConfig()
	cfg.DensityThreshold = 0

	w, err := newStreamingIndexWriter(path, cfg)
	require.NoError(t, err)

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
